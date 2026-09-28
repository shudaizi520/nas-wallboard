import {test, expect} from '@playwright/test';
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {extname, join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const fixture = JSON.parse(await readFile(join(root, 'tests/e2e/fixture.json'), 'utf8'));
let server;
let baseURL;
let setupRequired = false;
let setupMigration = false;
let sessionValid = true;
let lastSetupComplete = null;
let integrationInstances = [];
let mockPlexVisible = true;
const testCSRF = 'e2e-csrf-token';
const widgetSources = [
  {id: 'truenas-main', type: 'truenas', enabled: true},
  {id: 'qweather-main', type: 'qweather', enabled: true},
  {id: 'plex-main', type: 'plex', enabled: true},
  {id: 'uptime-main', type: 'uptime_kuma', enabled: true},
];
const widgetCatalog = [
  {id: 'cpu', integration_type: 'truenas', placement: 'metric', label: '处理器', visibility: 'always', defaults: {}},
  {id: 'cpu_temperature', integration_type: 'truenas', placement: 'metric', label: '处理器温度', visibility: 'always', defaults: {}},
  {id: 'network', integration_type: 'truenas', placement: 'metric', label: '实时网速', visibility: 'always', defaults: {}},
  {id: 'disk_temperature', integration_type: 'truenas', placement: 'metric', label: '机械硬盘', visibility: 'always', defaults: {}, fields: [{key:'name',kind:'text',label:'设备名称'}]},
  {id: 'weather', integration_type: 'qweather', placement: 'activity', label: '天气', visibility: 'always', defaults: {}},
  {id: 'plex', integration_type: 'plex', placement: 'activity', label: 'Plex 播放', visibility: 'non_empty', defaults: {}},
  {id: 'uptime_kuma', integration_type: 'uptime_kuma', placement: 'activity', label: '网站状态', visibility: 'warning_only', defaults: {}},
];
let managedLayout = {
  width: 300,
  widgets: [
    {id:'cpu-1',definition_id:'cpu',integration_id:'truenas-main',enabled:true,order:0,config:{}},
    {id:'cpu-temperature-1',definition_id:'cpu_temperature',integration_id:'truenas-main',enabled:true,order:1,config:{}},
    {id:'network-1',definition_id:'network',integration_id:'truenas-main',enabled:true,order:2,config:{}},
    {id:'disk-1',definition_id:'disk_temperature',integration_id:'truenas-main',enabled:true,order:3,config:{name:'sda'}},
    {id:'weather-1',definition_id:'weather',integration_id:'qweather-main',enabled:true,order:4,config:{}},
    {id:'plex-1',definition_id:'plex',integration_id:'plex-main',enabled:true,order:5,config:{}},
  ],
};
const integrationCatalog = [{
  id: 'plex', detected: true, configured: false,
  metadata: {name: 'Plex 媒体', description: '为每个播放终端分别显示当前播放会话。', icon: 'play', category: '媒体', required: false},
  fields: [
    {key: 'url', kind: 'url', label: '服务地址', help: '填写 Plex 服务的局域网地址。', required: true},
    {key: 'call_timeout', kind: 'duration', label: '请求超时', help: '限制等待时间。', default: '5s'},
    {key: 'token', kind: 'secret', label: '访问令牌', help: '填写只读令牌。', required: true},
  ],
}];
let managed = {
  width: 300,
  min_width: 300,
  max_width: 720,
  metrics: [
    {id: 'cpu', label: 'CPU', enabled: true},
    {id: 'cpu_temperature', label: 'CPU 温度', enabled: true},
    {id: 'network', label: '网速', enabled: true},
    {id: 'disk_temperature', label: '机械硬盘', enabled: true},
  ],
  activities: [
    {id: 'weather', label: '天气', enabled: true},
    {id: 'plex', label: 'Plex', enabled: true},
    {id: 'uptime_kuma', label: '网站状态', enabled: false},
  ],
};

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    if (request.url.startsWith('/api/setup/status')) {
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({setup_required: setupRequired, migration: setupMigration, configured: !setupRequired}));
      return;
    }
    if (request.url.startsWith('/api/setup/probe')) {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const candidate = JSON.parse(raw);
      if (!candidate.api_key) {
        response.writeHead(400, {'content-type': 'application/json'});
        response.end(JSON.stringify({error: 'invalid_candidate'}));
        return;
      }
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({
        ok: true, compatible: true, version: '25.10.1',
        permissions: [{feature: '系统信息', ok: true}, {feature: '存储池', ok: true}, {feature: '硬盘', ok: true}],
        pools: [{id: '1', name: 'tank'}], disks: [{id: 'sda', name: 'sda'}],
        interfaces: [{id: 'eno1', name: 'eno1'}], apps: [{id: 'plex', name: 'Plex'}],
      }));
      return;
    }
    if (request.url.startsWith('/api/setup/complete')) {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      lastSetupComplete = JSON.parse(raw);
      setupRequired = false;
      sessionValid = false;
      response.writeHead(201, {'content-type': 'application/json'});
      response.end(JSON.stringify({configured: true}));
      return;
    }
    if (request.url.startsWith('/api/auth/session')) {
      response.writeHead(sessionValid ? 200 : 401, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify(sessionValid ? {authenticated: true, csrf: testCSRF} : {error: 'unauthorized'}));
      return;
    }
    if (request.url.startsWith('/api/auth/login')) {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const input = JSON.parse(raw);
      if (input.password !== 'correct horse battery staple') {
        response.writeHead(401, {'content-type': 'application/json'});
        response.end(JSON.stringify({error: 'invalid_credentials'}));
        return;
      }
      sessionValid = true;
      response.writeHead(200, {'content-type': 'application/json'});
      response.end(JSON.stringify({authenticated: true, csrf: testCSRF}));
      return;
    }
    if (request.url.startsWith('/api/auth/logout')) {
      sessionValid = false;
      response.writeHead(204);
      response.end();
      return;
    }
    if (request.url.startsWith('/api/manage/overview')) {
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({version:'test',application_uptime_seconds:120,nas:{version:'25.10.1',uptime_seconds:90000,connected:true,last_update:'2026-09-28T00:00:00Z'},integrations:[{instance_id:'truenas-main',type:'truenas',running:true,healthy:true,message:'运行中'}],migration:{imported:false,warning_count:0}}));
      return;
    }
    if (request.url.startsWith('/api/manage/update')) {
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({current:'test',available:false,disabled:true}));
      return;
    }
    if (request.url.startsWith('/api/manage/dashboard')) {
      if (request.method === 'PUT') {
        let raw = '';
        for await (const chunk of request) raw += chunk;
        const next = JSON.parse(raw);
        managed = {
          ...managed,
          width: next.width,
          metrics: managed.metrics.map((item) => ({...item, enabled: next.metrics.includes(item.id)})),
          activities: managed.activities.map((item) => ({...item, enabled: next.activities.includes(item.id)})),
        };
      }
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify(managed));
      return;
    }
    if (request.url.startsWith('/api/manage/layout')) {
      if (request.method === 'PUT') {
        let raw = '';
        for await (const chunk of request) raw += chunk;
        managedLayout = JSON.parse(raw);
      }
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({catalog: widgetCatalog, sources: widgetSources, layout: managedLayout}));
      return;
    }
    if (request.url.startsWith('/api/manage/integrations')) {
      const path = new URL(request.url, 'http://test.local').pathname;
      const parts = path.replace('/api/manage/integrations', '').split('/').filter(Boolean);
      let raw = '';
      for await (const chunk of request) raw += chunk;
      if (request.method === 'GET' && parts.length === 0) {
        response.writeHead(200, {'content-type': 'application/json'});
        response.end(JSON.stringify({catalog: integrationCatalog.map((item) => ({...item, configured: integrationInstances.some((instance) => instance.type === item.id)})), instances: integrationInstances, health: []}));
        return;
      }
      if (request.method === 'POST' && parts[0] === 'probe') {
        response.writeHead(200, {'content-type': 'application/json'}); response.end(JSON.stringify({ok: true, stage: 'feature', message: 'Plex 连接成功'})); return;
      }
      if (request.method === 'POST' && parts.length === 0) {
        const candidate = JSON.parse(raw); integrationInstances = [{id: 'plex-main', type: 'plex', enabled: true, config: candidate.config, secrets: {token: true}}]; mockPlexVisible = true;
        response.writeHead(201, {'content-type': 'application/json'}); response.end(JSON.stringify({instance: integrationInstances[0], probe: {ok:true}})); return;
      }
      if (request.method === 'PUT' && parts.length === 1) {
        const candidate = JSON.parse(raw); integrationInstances[0] = {...integrationInstances[0], config: candidate.config};
        response.writeHead(200, {'content-type': 'application/json'}); response.end(JSON.stringify({instance: integrationInstances[0], probe:{ok:true}})); return;
      }
      if (request.method === 'POST' && parts[1] === 'disable') { integrationInstances[0].enabled = false; mockPlexVisible = false; response.writeHead(200, {'content-type':'application/json'}); response.end('{"enabled":false}'); return; }
      if (request.method === 'POST' && parts[1] === 'enable') { integrationInstances[0].enabled = true; mockPlexVisible = true; response.writeHead(200, {'content-type':'application/json'}); response.end('{"enabled":true}'); return; }
    }
    if (request.url.startsWith('/api/dashboard')) {
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({...fixture, width: managedLayout.width, activities: mockPlexVisible ? fixture.activities : fixture.activities.filter((item) => item.id !== 'plex')}));
      return;
    }
    const requestedPath = new URL(request.url, 'http://test.local').pathname;
    const pathname = requestedPath === '/' ? '/index.html'
      : requestedPath === '/manage' ? '/manage.html'
      : requestedPath === '/setup' ? '/setup.html'
      : requestedPath === '/login' ? '/login.html'
      : requestedPath;
    try {
      const data = await readFile(join(root, 'web', pathname));
      const types = {'.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.png': 'image/png', '.svg': 'image/svg+xml'};
      response.writeHead(200, {'content-type': types[extname(pathname)] ?? 'application/octet-stream'});
      response.end(data);
    } catch {
      response.writeHead(404);
      response.end('not found');
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  baseURL = `http://127.0.0.1:${server.address().port}`;
});

test.afterAll(async () => {
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
});

for (const viewport of [
  {name: '1080p', width: 1920, height: 1080},
  {name: '1440p', width: 2560, height: 1440},
  {name: 'narrow', width: 390, height: 844},
]) {
  test(`${viewport.name} compact wallpaper stays readable`, async ({page}, testInfo) => {
    const consoleErrors = [];
    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    await page.setViewportSize({width: viewport.width, height: viewport.height});
    await page.goto(baseURL, {waitUntil: 'networkidle'});

    await expect(page.getByRole('main')).toHaveCount(1);
    const panel = page.getByRole('region', {name: 'NAS 状态'});
    await expect(panel).toBeVisible();
    await expect(page.locator('[data-bind="connection"]')).toHaveAttribute('data-tone', 'good');
    await expect(page.locator('[data-bind="uptime"]')).toHaveText('5天 6小时');
    const uptimeSeparator = await page.locator('[data-bind="uptime"]').evaluate((node) => getComputedStyle(node, '::before').content);
    expect(uptimeSeparator).toBe('none');
    await expect(page.getByText('运行时间', {exact: true})).toHaveCount(0);
    await expect(page.getByText('截至', {exact: false})).toHaveCount(0);
    await expect(page.locator('[data-key="cpu"] [data-field="value"]')).toHaveText('18.4% · 56°');
    await expect(page.locator('[data-key="cpu"] [data-field="label"]')).toHaveText('处理器');
    await expect(page.locator('[data-key="cpu_temperature"]')).toHaveCount(0);
    await expect(page.locator('[data-key="disk_temperature"] [data-field="label"]')).toHaveText('机械硬盘');
    await expect(page.locator('[data-key="disk_temperature"] [data-field="value"]')).toHaveText('40°');
    await expect(page.locator('[data-key="network"] [data-field="label"]')).toHaveText('实时网速');
    await expect(page.locator('[data-key="network"] [data-field="value"]')).toBeHidden();
    await expect(page.locator('[data-key="network"] [data-field="down"]')).toHaveText('↓ 1 MB/s');
    await expect(page.locator('[data-key="network"] [data-field="up"]')).toHaveText('↑ 1 MB/s');
    const networkColors = await page.locator('[data-key="network"]').evaluate((node) => ({
      down: getComputedStyle(node.querySelector('[data-field="down"]')).color,
      up: getComputedStyle(node.querySelector('[data-field="up"]')).color,
    }));
    expect.soft(networkColors.up).toBe(networkColors.down);
    await expect(page.getByText('天气', {exact: true})).toHaveCount(0);
    await expect(page.locator('[data-key="weather"] [data-field="detail"]')).toHaveText('深圳 · 两小时无雨');
    const weather = page.locator('[data-key="weather"]');
    await expect(weather).toHaveAttribute('data-icon', 'weather-sunny');
    await expect(weather.locator('[data-field="icon"]')).toHaveAttribute('href', '#icon-weather-sunny');
    const sunnyMotion = await weather.locator('.activity-icon svg').evaluate((node) => {
      const style = getComputedStyle(node);
      return {animationName: style.animationName, color: style.color, width: Number.parseFloat(style.width)};
    });
    expect(sunnyMotion.animationName).toContain('weather-sun-turn');
    expect(sunnyMotion.color).toBe('rgb(255, 204, 76)');
    expect(sunnyMotion.width).toBeGreaterThanOrEqual(22);
    await expect(page.getByText('健康', {exact: true})).toHaveCount(0);
    await expect(page.getByText('和风天气', {exact: true})).toHaveCount(0);
    await expect(page.getByText('Plex', {exact: true})).toBeVisible();
    await expect(page.getByText('SMART', {exact: true})).toBeVisible();
    expect(await page.locator('.activity-row').evaluateAll((rows) => rows.map((row) => row.dataset.key))).toEqual(['weather', 'plex', 'alert:a1']);

    for (const removed of ['实时概览', '存储空间', '应用服务', '本地天气', '已连接', '刚刚更新', '可能忘记关了', '只显示需要关注的信息', '应用更新', 'Jellyfin', 'qBittorrent', 'ReplicationSuccess']) {
      await expect(page.getByText(removed, {exact: true})).toHaveCount(0);
    }
    await expect(page.locator('[data-bind="clock"]')).toHaveCount(0);

    await page.screenshot({path: testInfo.outputPath(`${viewport.name}.jpg`), type: 'jpeg', quality: 85, fullPage: true});

    const bounds = await panel.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.y).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(viewport.height);
    if (viewport.name !== 'narrow') {
      expect(bounds.width).toBe(300);
      expect(bounds.x).toBeGreaterThan(viewport.width * 0.65);
      expect(bounds.y).toBeLessThan(viewport.height * 0.15);
      expect(bounds.height).toBeLessThanOrEqual(370);
      const metricLayout = await page.locator('.health-metrics').evaluate((node) => {
        const rows = [...node.children].map((child) => {
          const label = child.querySelector('[data-field="label"]').getBoundingClientRect();
          const value = child.querySelector('[data-field="value"]:not([hidden]), [data-field="rates"]:not([hidden])').getBoundingClientRect();
          return {
            labelLeft: Math.round(label.left),
            valueRight: Math.round(value.right),
            top: Math.round(child.getBoundingClientRect().top),
          };
        });
        return {
          labelSpread: Math.max(...rows.map((row) => row.labelLeft)) - Math.min(...rows.map((row) => row.labelLeft)),
          valueRightSpread: Math.max(...rows.map((row) => row.valueRight)) - Math.min(...rows.map((row) => row.valueRight)),
          contentRight: Math.round(node.getBoundingClientRect().right),
          valueRight: rows[0].valueRight,
          rowGaps: rows.slice(1).map((row, index) => row.top - rows[index].top),
        };
      });
      expect(metricLayout.labelSpread).toBeLessThanOrEqual(1);
      expect(metricLayout.valueRightSpread).toBeLessThanOrEqual(2);
      expect(Math.abs(metricLayout.contentRight - metricLayout.valueRight)).toBeLessThanOrEqual(1);
      expect(await page.locator('.health-value').count()).toBe(3);
      expect(metricLayout.rowGaps.every((gap) => gap >= 24 && gap <= 36)).toBe(true);
      const metricBottom = await page.locator('.health-metrics').evaluate((node) => node.getBoundingClientRect().bottom);
      const weatherTop = await page.locator('[data-key="weather"]').evaluate((node) => node.getBoundingClientRect().top);
      expect(weatherTop - metricBottom).toBeLessThanOrEqual(20);
      const weatherAlignment = await page.locator('[data-key="weather"]').evaluate((node) => {
        const value = node.querySelector('[data-field="value"]').getBoundingClientRect();
        const detail = node.querySelector('[data-field="detail"]').getBoundingClientRect();
        return {
          lineCenterDelta: Math.round(Math.abs((detail.top + detail.height / 2) - (value.top + value.height / 2))),
          detailGap: Math.round(detail.left - value.right),
          detailFontSize: Number.parseFloat(getComputedStyle(node.querySelector('[data-field="detail"]')).fontSize),
          detailRight: Math.round(detail.right),
          contentRight: Math.round(node.getBoundingClientRect().right),
        };
      });
      expect(weatherAlignment.lineCenterDelta).toBeLessThanOrEqual(2);
      expect.soft(weatherAlignment.detailGap).toBeGreaterThanOrEqual(12);
      expect(weatherAlignment.detailFontSize).toBeGreaterThanOrEqual(12);
      expect(Math.abs(weatherAlignment.contentRight - weatherAlignment.detailRight)).toBeLessThanOrEqual(1);
      const typography = await page.locator('.glass-panel').evaluate((panel) => {
        const nodes = [
          panel.querySelector('.nas-mark b'),
          panel.querySelector('[data-bind="uptime"]'),
          ...panel.querySelectorAll('.health-value small, .health-value b, .network-rates i'),
          panel.querySelector('[data-key="weather"] [data-field="value"]'),
          panel.querySelector('[data-key="weather"] [data-field="detail"]'),
        ];
        return nodes.map((node) => {
          const style = getComputedStyle(node);
          return {color: style.color, fontSize: style.fontSize, fontWeight: style.fontWeight};
        });
      });
      expect(new Set(typography.map((item) => item.color)).size).toBe(1);
      expect(new Set(typography.map((item) => item.fontSize)).size).toBe(1);
      expect(new Set(typography.map((item) => item.fontWeight)).size).toBe(1);
    }

    const overflow = await page.evaluate(() => ({
      pageX: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      pageY: document.documentElement.scrollHeight - document.documentElement.clientHeight,
      panelX: document.querySelector('.glass-panel').scrollWidth - document.querySelector('.glass-panel').clientWidth,
      metricsFit: [...document.querySelectorAll('.health-value [data-field="value"]:not([hidden])')].every((value) => value.scrollWidth <= value.clientWidth),
      rowsInside: [...document.querySelectorAll('.activity-row')].every((row) => row.scrollWidth <= row.clientWidth),
    }));
    expect(overflow).toEqual({pageX: 0, pageY: 0, panelX: 0, metricsFit: true, rowsInside: true});

    await page.evaluate(async (data) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, {
        ...data,
        activities: data.activities.map((activity) => activity.id === 'weather'
          ? {...activity, value: '31° · 晴', detail: '平湖 · 深圳市气象台发布高温黄色预警'}
          : activity),
      });
    }, fixture);
    const longWeather = await page.locator('[data-key="weather"]').evaluate((node) => {
      const value = node.querySelector('[data-field="value"]').getBoundingClientRect();
      const detailNode = node.querySelector('[data-field="detail"]');
      const detail = detailNode.getBoundingClientRect();
      return {
        gap: Math.round(detail.left - value.right),
        inside: detail.right <= node.getBoundingClientRect().right + 1,
      };
    });
    expect(longWeather.gap).toBeGreaterThanOrEqual(12);
    expect(longWeather.inside).toBe(true);

    await page.evaluate(async (data) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, data);
    }, fixture);

    await page.evaluate(async () => {
      const {renderFetchError} = await import('/view.js');
      renderFetchError(document);
    });
    await expect(page.locator('[data-bind="connection"]')).toHaveAttribute('data-tone', 'bad');
    await expect(page.locator('[data-key="cpu"] [data-field="value"]')).toHaveText('18.4% · 56°');
    await expect(page.locator('[data-key="weather"] [data-field="detail"]')).toHaveText('深圳 · 两小时无雨');
    expect(consoleErrors).toEqual([]);

    await page.evaluate(async (data) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, data);
    }, fixture);
    await expect(page.locator('[data-bind="connection"]')).toHaveAttribute('data-tone', 'good');
  });
}

test('desktop mode exposes only the transparent shared panel', async ({page}) => {
  await page.setViewportSize({width: 1200, height: 800});
  await page.goto(`${baseURL}/?desktop=1`, {waitUntil: 'networkidle'});
  const layout = await page.evaluate(() => {
    const panel = document.querySelector('.glass-panel').getBoundingClientRect();
    const body = document.body.getBoundingClientRect();
    const wallpaper = getComputedStyle(document.body).backgroundImage;
    const weatherIcon = document.querySelector('[data-key="weather"] .activity-icon svg');
    const weatherGlow = document.querySelector('[data-key="weather"] .activity-icon');
    return {
      desktop: document.documentElement.classList.contains('desktop-mode'),
      background: getComputedStyle(document.body).backgroundColor,
      wallpaper,
      panel: {x: panel.x, y: panel.y, width: panel.width, height: panel.height},
      body: {width: body.width, height: body.height},
      panels: document.querySelectorAll('.glass-panel').length,
      panelBackdrop: getComputedStyle(document.querySelector('.glass-panel')).backdropFilter,
      weatherAnimation: getComputedStyle(weatherIcon).animationName,
      weatherGlowAnimation: getComputedStyle(weatherGlow, '::before').animationName,
    };
  });
  expect(layout.desktop).toBe(true);
  expect(layout.background).toBe('rgba(0, 0, 0, 0)');
  expect(layout.wallpaper).toBe('none');
  expect(layout.panel.x).toBe(0);
  expect(layout.panel.y).toBe(0);
  expect(Math.abs(layout.body.width - layout.panel.width)).toBeLessThanOrEqual(2);
  expect(Math.abs(layout.body.height - layout.panel.height)).toBeLessThanOrEqual(2);
  expect(layout.panels).toBe(1);
  expect(layout.panelBackdrop).toBe('none');
  expect(layout.weatherAnimation).toBe('none');
  expect(layout.weatherGlowAnimation).toBe('none');
});

test('desktop panel width stays stable when the host follows its measured size', async ({page}) => {
  await page.setViewportSize({width: 420, height: 500});
  await page.goto(`${baseURL}/?desktop=1`, {waitUntil: 'networkidle'});
  const panel = page.getByRole('region', {name: 'NAS 状态'});

  for (let iteration = 0; iteration < 6; iteration += 1) {
    const bounds = await panel.boundingBox();
    expect(bounds).not.toBeNull();
    await page.setViewportSize({
      width: Math.round(bounds.width),
      height: Math.round(bounds.height),
    });
  }

  const settled = await panel.boundingBox();
  expect(settled).not.toBeNull();
  expect(settled.width).toBe(300);
});

test('management page adds, keyboard reorders, configures, previews, and saves stable widgets', async ({page}, testInfo) => {
  sessionValid = true;
  await page.setViewportSize({width: 1200, height: 900});
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  await expect(page.getByRole('heading', {name: '管理中心'})).toBeVisible();
  await page.getByRole('tab', {name: '桌面内容'}).click();
  await expect(page.getByRole('heading', {name: '桌面内容'})).toBeVisible();
  await page.getByRole('button', {name: '＋ 网站状态'}).click();
  const website = page.locator('[data-id="uptime_kuma-1"]');
  await expect(website).toBeVisible();
  await website.focus();
  await page.keyboard.press('Alt+ArrowUp');
  await page.locator('#width').fill('500');
  await expect(page.locator('#status')).toHaveText('有未保存的更改');
  await page.getByRole('button', {name: '保存布局'}).click();
  await expect(page.locator('#status')).toHaveText('已保存');
  expect(managedLayout.width).toBe(500);
  expect(managedLayout.widgets.find((item) => item.id === 'uptime_kuma-1').enabled).toBe(true);
  await expect(page.locator('#layout-preview')).toHaveAttribute('src', /desktop=1&preview=/);
  await page.screenshot({path: testInfo.outputPath('layout-editor.jpg'), type: 'jpeg', quality: 88, fullPage: true});
});

test('management overview and recovery settings are clear without exposing secret values', async ({page}, testInfo) => {
  sessionValid = true;
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  await expect(page.getByText('25.10.1')).toBeVisible();
  await expect(page.getByText('数据连接正常')).toBeVisible();
  await page.getByRole('tab', {name: '设置与恢复'}).click();
  await expect(page.getByRole('heading', {name: '完整加密备份'})).toBeVisible();
  await expect(page.getByText(/不会删除 TrueNAS 上的任何数据/)).toBeVisible();
  expect(await page.locator('body').textContent()).not.toContain('correct horse battery staple');
  await page.screenshot({path: testInfo.outputPath('overview-settings.jpg'), type: 'jpeg', quality: 88, fullPage: true});
});

test('browser and desktop modes render identical activity IDs, order, and text including multiple media sessions', async ({page}) => {
  const twoSessions = {
    ...fixture,
    activities: [
      ...fixture.activities.filter((item) => item.id !== 'plex'),
      {id:'plex:0',icon:'play',tone:'active',title:'Plex',value:'播放',detail:'歌曲 A · 电视'},
      {id:'plex:1',icon:'play',tone:'active',title:'Plex',value:'暂停',detail:'歌曲 B · 手机'},
    ],
  };
  const capture = async (target) => {
    await page.goto(target, {waitUntil:'networkidle'});
    await page.evaluate(async (data) => { const {renderDashboard} = await import('/view.js'); renderDashboard(document, data); }, twoSessions);
    return page.locator('.activity-row').evaluateAll((rows) => rows.map((row) => ({id:row.dataset.key, text:row.textContent.replace(/\s+/g,' ').trim()})));
  };
  const browser = await capture(baseURL);
  const desktop = await capture(`${baseURL}/?desktop=1`);
  expect(desktop).toEqual(browser);
  expect(browser.filter((item) => item.id.startsWith('plex:')).map((item) => item.id)).toEqual(['plex:0','plex:1']);
});

test('setup-required dashboard links to the first-run wizard', async ({page}) => {
  setupRequired = true;
  setupMigration = false;
  sessionValid = false;
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await expect(page.getByRole('link', {name: /完成初始设置/})).toBeVisible();
  await expect(page.locator('.glass-panel')).toHaveClass(/setup-pending/);
  setupRequired = false;
});

test('legacy migration keeps the existing dashboard visible until an administrator password is chosen', async ({page}) => {
  setupRequired = true;
  setupMigration = true;
  sessionValid = false;
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await expect(page.getByRole('link', {name: /完成初始设置/})).toBeHidden();
  await expect(page.locator('.glass-panel')).not.toHaveClass(/setup-pending/);
  await expect(page.locator('[data-key="cpu"] [data-field="value"]')).toHaveText('18.4% · 56°');
  setupRequired = false;
  setupMigration = false;
});

test('six-step setup completes without retaining submitted secrets', async ({page}, testInfo) => {
  setupRequired = true;
  sessionValid = false;
  lastSetupComplete = null;
  await page.goto(`${baseURL}/setup`, {waitUntil: 'networkidle'});
  await expect(page.locator('.brand img')).toHaveJSProperty('complete', true);
  expect(await page.locator('.brand img').evaluate((image) => image.naturalWidth)).toBeGreaterThan(0);
  await expect(page.getByRole('heading', {name: '几分钟完成设置'})).toBeVisible();
  await page.getByLabel(/我使用的是 TrueNAS SCALE/).check();
  await page.getByRole('button', {name: '继续'}).click();
  await page.getByLabel('TrueNAS 地址').fill('https://nas.local');
  await page.getByLabel('专用账户名').fill('nas_wallboard');
  await page.getByLabel('API Key').fill('temporary-api-secret');
  await page.getByRole('button', {name: '继续'}).click();
  await page.getByRole('button', {name: '测试 TrueNAS 连接'}).click();
  await expect(page.getByText('已连接 TrueNAS 25.10.1')).toBeVisible();
  await page.getByRole('button', {name: '继续'}).click();
  await page.getByLabel('管理员密码', {exact: true}).fill('correct horse battery staple');
  await page.getByLabel('再次输入').fill('correct horse battery staple');
  await page.getByRole('button', {name: '继续'}).click();
  await expect(page.getByRole('heading', {name: '这些资源已经就绪'})).toBeVisible();
  await page.getByRole('button', {name: '继续'}).click();
  await expect(page.getByRole('heading', {name: '可以开始了'})).toBeVisible();
  await page.screenshot({path: testInfo.outputPath('setup-review.jpg'), type: 'jpeg', quality: 88, fullPage: true});
  await page.getByRole('button', {name: '完成设置'}).click();
  await page.waitForURL('**/login');
  expect(lastSetupComplete.api_key).toBe('temporary-api-secret');
  expect(lastSetupComplete.url).toBe('wss://nas.local/api/current');
  expect(await page.locator('body').textContent()).not.toContain('temporary-api-secret');
});

test('login failure clears the password, success opens management, and logout ends the session', async ({page}) => {
  setupRequired = false;
  sessionValid = false;
  await page.goto(`${baseURL}/login`, {waitUntil: 'domcontentloaded'});
  const password = page.getByLabel('管理员密码');
  await password.fill('wrong password value');
  await page.getByRole('button', {name: '登录'}).click();
  await expect(page.getByRole('alert')).toHaveText('密码不正确');
  await expect(password).toHaveValue('');
  await password.fill('correct horse battery staple');
  await page.getByRole('button', {name: '登录'}).click();
  await page.waitForURL('**/manage');
  await expect(page.getByRole('heading', {name: '管理中心'})).toBeVisible();
  await page.getByRole('button', {name: '退出'}).click();
  await page.waitForURL('**/login');
  expect(sessionValid).toBe(false);
});

test('setup is mobile-safe and a configured reload never asks for secrets again', async ({page}) => {
  setupRequired = true;
  sessionValid = false;
  await page.setViewportSize({width: 390, height: 844});
  await page.goto(`${baseURL}/setup`, {waitUntil: 'networkidle'});
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);

  setupRequired = false;
  sessionValid = true;
  await page.reload({waitUntil: 'networkidle'});
  await page.waitForURL('**/manage');
  await expect(page.getByLabel('API Key')).toHaveCount(0);
  await expect(page.locator('input[name="administrator"]:visible')).toHaveCount(0);
});

test('integration center adds, retains masked token, and disables Plex without restart', async ({page}, testInfo) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  sessionValid = true; integrationInstances = []; mockPlexVisible = false;
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  const integrationAPI = await page.evaluate(async () => { const response = await fetch('/api/manage/integrations'); return {status: response.status, body: await response.text()}; });
  expect(integrationAPI.status, integrationAPI.body).toBe(200);
  await page.waitForTimeout(100);
  expect(pageErrors).toEqual([]);
  await page.getByRole('tab', {name: '集成中心'}).click();
  const card = page.locator('[data-integration="plex"]');
  await expect(card).toBeVisible();
  await page.screenshot({path: testInfo.outputPath('integration-center.jpg'), type: 'jpeg', quality: 88, fullPage: true});
  await card.getByRole('button', {name: '添加'}).click();
  await page.locator('[name="url"]').fill('http://plex.local:32400');
  await page.locator('[name="token"]').fill('plex-secret');
  await page.getByRole('button', {name: '测试连接'}).click();
  await expect(page.getByText('Plex 连接成功')).toBeVisible();
  await page.getByRole('button', {name: '保存'}).click();
  await expect(card.getByText('已配置')).toBeVisible();
  await card.getByRole('button', {name: '设置'}).click();
  await expect(page.locator('[name="token"]')).toHaveValue('********');
  await page.getByRole('button', {name: '取消'}).click();
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await expect(page.getByText('Plex', {exact: true})).toBeVisible();
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  await page.getByRole('tab', {name: '集成中心'}).click();
  await page.locator('[data-integration="plex"]').getByRole('button', {name: '停用'}).click();
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await expect(page.getByText('Plex', {exact: true})).toHaveCount(0);
  mockPlexVisible = true;
});
