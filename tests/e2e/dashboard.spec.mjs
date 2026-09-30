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
let administratorUsername = 'admin';
let administratorPassword = 'correct horse battery staple';
let integrationInstances = [];
let integrationMutationRequests = 0;
let overviewRequests = 0;
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
    {key: 'call_timeout', kind: 'duration', label: '请求超时', help: '限制等待时间。', default: '5s', advanced: true},
    {key: 'token', kind: 'secret', label: '访问令牌', help: '登录 Plex Web 后复制 X-Plex-Token。', required: true},
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
      administratorUsername = lastSetupComplete.administrator_username;
      administratorPassword = lastSetupComplete.password;
      setupRequired = false;
      sessionValid = false;
      response.writeHead(201, {'content-type': 'application/json'});
      response.end(JSON.stringify({configured: true}));
      return;
    }
    if (request.url.startsWith('/api/auth/session')) {
      response.writeHead(sessionValid ? 200 : 401, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify(sessionValid ? {authenticated: true, csrf: testCSRF, username: administratorUsername} : {error: 'unauthorized'}));
      return;
    }
    if (request.url.startsWith('/api/auth/login')) {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const input = JSON.parse(raw);
      if (String(input.username ?? '').trim().toLocaleLowerCase() !== administratorUsername.toLocaleLowerCase()
          || input.password !== administratorPassword) {
        response.writeHead(401, {'content-type': 'application/json'});
        response.end(JSON.stringify({error: 'invalid_credentials'}));
        return;
      }
      sessionValid = true;
      response.writeHead(200, {'content-type': 'application/json'});
      response.end(JSON.stringify({authenticated: true, csrf: testCSRF, username: administratorUsername}));
      return;
    }
    if (request.url.startsWith('/api/auth/logout')) {
      sessionValid = false;
      response.writeHead(204);
      response.end();
      return;
    }
    if (request.url.startsWith('/api/manage/overview')) {
      overviewRequests += 1;
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({version:'test',application_uptime_seconds:120,nas:{version:'25.10.1',uptime_seconds:90000,connected:true,last_update:'2026-09-28T00:00:00Z'},integrations:[{instance_id:'truenas-main',type:'truenas',running:true,healthy:true,message:'运行中'}],migration:{imported:false,warning_count:0}}));
      return;
    }
    if (request.url.startsWith('/api/manage/update')) {
      response.writeHead(200, {'content-type': 'application/json', 'cache-control': 'no-store'});
      response.end(JSON.stringify({current:'test',available:false,disabled:true}));
      return;
    }
    if (request.url.startsWith('/api/manage/username')) {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const input = JSON.parse(raw);
      if (input.current_password !== administratorPassword) {
        response.writeHead(401, {'content-type': 'application/json'});
        response.end(JSON.stringify({error: 'invalid_credentials'}));
        return;
      }
      administratorUsername = input.username;
      sessionValid = false;
      response.writeHead(200, {'content-type': 'application/json'});
      response.end(JSON.stringify({username: administratorUsername}));
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
        integrationMutationRequests += 1;
        const candidate = JSON.parse(raw); integrationInstances = [{id: 'plex-main', type: 'plex', enabled: true, config: candidate.config, secrets: {token: true}}]; mockPlexVisible = true;
        response.writeHead(201, {'content-type': 'application/json'}); response.end(JSON.stringify({instance: integrationInstances[0], probe: {ok:true}})); return;
      }
      if (request.method === 'PUT' && parts.length === 1) {
        integrationMutationRequests += 1;
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
    await expect(page.locator('[data-key="weather"] [data-field="detail"]')).toHaveText('体感31° · 湿度64%');
    const weather = page.locator('[data-key="weather"]');
    await expect(weather).toHaveAttribute('data-icon', 'weather-sunny');
    await expect(weather.locator('[data-field="icon"]')).toHaveAttribute('href', '#icon-weather-sunny');
    await expect(page.locator('[data-key="weather:forecast:0"]')).toHaveClass(/activity-row--forecast/);
    await expect(page.locator('[data-key="weather:forecast:0"] [data-field="value"]')).toHaveText('多云');
    await expect(page.locator('[data-key="weather:forecast:0"] [data-field="detail"]')).toHaveText('26°–33° · 雨35%');
    await expect(page.locator('[data-key="weather:forecast:1"] [data-field="value"]')).toHaveText('阵雨');
    await expect(page.locator('[data-key="weather:forecast:1"] [data-field="detail"]')).toHaveText('25°–31° · 雨80%');
    await expect(page.getByText('明天', {exact: true})).toHaveCount(0);
    await expect(page.getByText('后天', {exact: true})).toHaveCount(0);
    const sunnyMotion = await weather.locator('.activity-icon svg').evaluate((node) => {
      const style = getComputedStyle(node);
      return {animationName: style.animationName, color: style.color, width: Number.parseFloat(style.width)};
    });
    expect(sunnyMotion.animationName).toContain('weather-sun-turn');
    expect(sunnyMotion.color).toBe('rgb(255, 204, 76)');
    expect(sunnyMotion.width).toBeGreaterThanOrEqual(22);
    const forecastIconWidths = await page.locator('.activity-row--forecast .activity-icon svg').evaluateAll((icons) => icons.map((icon) => Number.parseFloat(getComputedStyle(icon).width)));
    expect(forecastIconWidths).toEqual([sunnyMotion.width, sunnyMotion.width]);
    await expect(page.getByText('健康', {exact: true})).toHaveCount(0);
    await expect(page.getByText('和风天气', {exact: true})).toHaveCount(0);
    await expect(page.getByText('Plex', {exact: true})).toBeVisible();
    await expect(page.getByText('SMART', {exact: true})).toBeVisible();
    await expect(page.locator('[data-key="fan"]')).toHaveClass(/activity-row--fan/);
    expect(await page.locator('.activity-row').evaluateAll((rows) => rows.map((row) => row.dataset.key))).toEqual(['weather', 'weather:forecast:0', 'weather:forecast:1', 'fan', 'plex', 'alert:a1']);

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
            valueLeft: Math.round(value.left),
            valueRight: Math.round(value.right),
            top: Math.round(child.getBoundingClientRect().top),
          };
        });
        return {
          labelSpread: Math.max(...rows.map((row) => row.labelLeft)) - Math.min(...rows.map((row) => row.labelLeft)),
          valueLeftSpread: Math.max(...rows.map((row) => row.valueLeft)) - Math.min(...rows.map((row) => row.valueLeft)),
          valueRightSpread: Math.max(...rows.map((row) => row.valueRight)) - Math.min(...rows.map((row) => row.valueRight)),
          contentRight: Math.round(node.getBoundingClientRect().right),
          valueLeft: rows[0].valueLeft,
          valueRight: rows[0].valueRight,
          rowGaps: rows.slice(1).map((row, index) => row.top - rows[index].top),
        };
      });
      expect(metricLayout.labelSpread).toBeLessThanOrEqual(1);
      expect(metricLayout.valueLeftSpread).toBeLessThanOrEqual(1);
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
      expect(weatherAlignment.detailFontSize).toBeGreaterThanOrEqual(12);
      const fanLayout = await page.locator('[data-key="fan"]').evaluate((node) => {
        const previous = node.previousElementSibling.getBoundingClientRect();
        const row = node.getBoundingClientRect();
        const icon = node.querySelector('.activity-icon svg').getBoundingClientRect();
        const value = node.querySelector('[data-field="value"]').getBoundingClientRect();
        return {
          gap: Math.round(row.top - previous.bottom),
          height: Math.round(row.height),
          iconWidth: Math.round(icon.width),
          valueRight: Math.round(value.right),
          contentRight: Math.round(row.right),
        };
      });
      expect(fanLayout.gap).toBeGreaterThanOrEqual(6);
      expect(fanLayout.gap).toBeLessThanOrEqual(18);
      expect(fanLayout.height).toBeLessThanOrEqual(46);
      expect(fanLayout.iconWidth).toBeGreaterThanOrEqual(20);
      const sharedValueColumn = await page.locator('.glass-panel').evaluate((panel) => {
        const left = (selector) => Math.round(panel.querySelector(selector).getBoundingClientRect().left);
        const starts = [
          left('[data-bind="uptime"]'),
          left('[data-key="cpu"] [data-field="value"]'),
          left('[data-key="disk_temperature"] [data-field="value"]'),
          left('[data-key="network"] [data-field="rates"]'),
          left('[data-key="weather"] [data-field="detail"]'),
          left('[data-key="weather:forecast:0"] [data-field="detail"]'),
          left('[data-key="weather:forecast:1"] [data-field="detail"]'),
          left('[data-key="fan"] [data-field="value"]'),
          left('[data-key="plex"] [data-field="value"]'),
        ];
        return {starts, spread: Math.max(...starts) - Math.min(...starts)};
      });
      expect(sharedValueColumn.spread, sharedValueColumn.starts.join(', ')).toBeLessThanOrEqual(2);
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
    await expect(page.locator('[data-key="weather"] [data-field="detail"]')).toHaveText('体感31° · 湿度64%');
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
  administratorUsername = 'admin';
  await page.setViewportSize({width: 1200, height: 900});
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  await expect(page.getByRole('heading', {name: '管理中心'})).toBeVisible();
  await page.getByRole('tab', {name: '桌面内容'}).click();
  await expect(page.locator('#desktop-page').getByRole('heading', {name: '桌面内容'})).toBeVisible();
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
  await expect(page.frameLocator('#layout-preview').locator('.glass-panel')).toBeVisible();
  const measurePreviewFit = () => page.locator('.preview-stage').evaluate((stage) => {
    const iframe = stage.querySelector('iframe');
    const panel = iframe.contentDocument.querySelector('.glass-panel');
    const matrix = new DOMMatrixReadOnly(getComputedStyle(iframe).transform);
    return {
      stageHeight: stage.getBoundingClientRect().height,
      expectedHeight: panel.getBoundingClientRect().height * matrix.a + 36,
      sharedPanels: iframe.contentDocument.querySelectorAll('.glass-panel').length,
    };
  });
  expect((await measurePreviewFit()).sharedPanels).toBe(1);
  // ResizeObserver and the stage's height transition settle asynchronously.
  await expect.poll(async () => {
    const fit = await measurePreviewFit();
    return Math.abs(fit.stageHeight - fit.expectedHeight);
  }).toBeLessThanOrEqual(32);
  await page.screenshot({path: testInfo.outputPath('layout-editor.jpg'), type: 'jpeg', quality: 88, fullPage: true});
});

test('management overview and recovery settings are clear without exposing secret values', async ({page}, testInfo) => {
  sessionValid = true;
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  await expect(page.getByText('25.10.1')).toBeVisible();
  await expect(page.getByText('最近刷新', {exact: false})).toHaveCount(0);
  await page.getByRole('tab', {name: '设置与恢复'}).click();
  await expect(page.getByRole('heading', {name: '完整加密备份'})).toBeVisible();
  await expect(page.getByRole('heading', {name: '恢复加密备份'})).toBeVisible();
  await page.getByText('从完整备份恢复设置与凭据', {exact:true}).click();
  await expect(page.locator('#restore-form input[name="file"]')).toBeVisible();
  await page.getByText('删除 Wallboard 配置', {exact: true}).first().click();
  await expect(page.getByText(/不会删除 TrueNAS 数据/)).toBeVisible();
  expect(await page.locator('body').textContent()).not.toContain('correct horse battery staple');
  await page.screenshot({path: testInfo.outputPath('overview-settings.jpg'), type: 'jpeg', quality: 88, fullPage: true});
});

test('management layout stays bounded and aligned on wide screens', async ({page}) => {
  sessionValid = true;
  administratorUsername = 'admin';
  await page.setViewportSize({width: 2560, height: 1400});
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});

  const shell = await page.locator('.management-shell').boundingBox();
  expect(shell).not.toBeNull();
  expect(shell.width).toBeLessThanOrEqual(1041);

  const firstTab = await page.getByRole('tab', {name: '概览'}).boundingBox();
  expect(firstTab).not.toBeNull();
  expect(firstTab.width).toBeGreaterThan(0);

  for (const tabName of ['概览', '桌面内容', '集成', '设置与恢复']) {
    await page.getByRole('tab', {name: tabName}).click();
    const starts = await page.locator('.manage-page:not([hidden]) .settings-section .section-content').evaluateAll((nodes) => nodes.filter((node) => node.getClientRects().length > 0).map((node) => Math.round(node.getBoundingClientRect().left)));
    expect(starts.length).toBeGreaterThan(0);
    expect(Math.max(...starts) - Math.min(...starts), `${tabName}: ${starts.join(', ')}`).toBeLessThanOrEqual(1);
  }

  await page.getByRole('tab', {name: '集成'}).click();
  await expect(page.locator('[data-integration] .section-label p')).toHaveCount(0);
  const integrationGeometry = await page.locator('[data-integration="plex"]').evaluate((row) => {
    const content = row.querySelector('.section-content').getBoundingClientRect();
    const secondary = row.querySelector('.integration-secondary').getBoundingClientRect();
    return {contentRight: content.right, actionRight: secondary.right, gap: secondary.left - row.querySelector('.integration-status').getBoundingClientRect().right};
  });
  expect(integrationGeometry.actionRight).toBeLessThanOrEqual(integrationGeometry.contentRight + 1);
  expect(integrationGeometry.gap).toBeLessThanOrEqual(40);

  await page.getByRole('tab', {name: '设置与恢复'}).click();
  await page.getByText('修改登录用户名', {exact: true}).click();
  const form = await page.locator('#username-form').boundingBox();
  expect(form).not.toBeNull();
  expect(form.width).toBeLessThanOrEqual(640);
});

test('management mobile contains long content without overflow', async ({page}) => {
  sessionValid = true;
  administratorUsername = '管理员_这是一个较长的用户名';
  integrationInstances = [{id: 'plex-main', type: 'plex', enabled: true, config: {url: 'http://plex-with-an-extremely-long-local-hostname-that-must-not-expand-the-page.local:32400/library'}, secrets: {token: true}}];
  await page.setViewportSize({width: 390, height: 844});
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});

  for (const tabName of ['概览', '桌面内容', '集成', '设置与恢复']) {
    await page.getByRole('tab', {name: tabName}).click();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
  }

  await page.getByRole('tab', {name: '集成'}).click();
  const integration = page.locator('[data-integration="plex"]');
  const wrapping = await page.evaluate(() => {
    const username = document.querySelector('#current-username');
    const endpoint = document.querySelector('[data-integration="plex"] .integration-endpoint');
    return {
      usernameWhiteSpace: getComputedStyle(username).whiteSpace,
      usernameFits: username.scrollWidth <= username.clientWidth,
      endpointWhiteSpace: getComputedStyle(endpoint).whiteSpace,
      endpointFits: endpoint.scrollWidth <= endpoint.clientWidth,
    };
  });
  expect(wrapping).toEqual({usernameWhiteSpace: 'normal', usernameFits: true, endpointWhiteSpace: 'normal', endpointFits: true});
  await integration.locator('.integration-status').evaluate((node) => {
    const error = document.createElement('p');
    error.className = 'exception-message';
    error.textContent = '这是一段没有空格而且非常长的中文集成错误信息用于确认内容不会撑出手机页面';
    node.append(error);
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  await integration.getByRole('button', {name: '设置'}).click();
  const dialog = await page.locator('#integration-dialog').boundingBox();
  expect(dialog).not.toBeNull();
  expect(dialog.x).toBeGreaterThanOrEqual(0);
  expect(dialog.x + dialog.width).toBeLessThanOrEqual(390);
  await page.getByRole('button', {name: '取消'}).click();

  await page.getByRole('tab', {name: '设置与恢复'}).click();
  await page.getByText('修改管理密码', {exact: true}).click();
  const order = await page.locator('#password-form').evaluate((form) => {
    const details = form.closest('details');
    return {summaryTop: details.querySelector('summary').getBoundingClientRect().top, formTop: form.getBoundingClientRect().top};
  });
  expect(order.formTop).toBeGreaterThan(order.summaryTop);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
});

test('management lifecycle stops overview polling and detaches inactive preview', async ({page}) => {
  sessionValid = true;
  overviewRequests = 0;
  await page.addInitScript(() => {
    const nativeSetTimeout = window.setTimeout.bind(window);
    window.setTimeout = (callback, delay, ...args) => nativeSetTimeout(callback, delay === 60000 ? 40 : delay, ...args);
  });
  await page.goto(`${baseURL}/manage`, {waitUntil: 'domcontentloaded'});
  await expect.poll(() => overviewRequests).toBeGreaterThan(1);

  await page.getByRole('tab', {name: '桌面内容'}).click();
  const stoppedAt = overviewRequests;
  await page.waitForTimeout(150);
  expect(overviewRequests).toBe(stoppedAt);
  await expect(page.locator('#layout-preview')).toHaveAttribute('src', /desktop=1&preview=/);

  await page.getByRole('tab', {name: '集成'}).click();
  await expect(page.locator('#layout-preview')).toHaveAttribute('src', 'about:blank');
  await page.getByRole('tab', {name: '桌面内容'}).click();
  await expect(page.locator('#layout-preview')).toHaveAttribute('src', /desktop=1&preview=/);

  await page.getByRole('tab', {name: '设置与恢复'}).click();
  for (const selector of ['#username-form', '#password-form', '#download-support', '#backup-form', '#reset-form']) await expect(page.locator(selector)).toHaveCount(1);
});

for (const viewport of [{name: 'desktop', width: 1200, height: 900}, {name: 'mobile', width: 390, height: 844}]) {
  test(`${viewport.name} management console keeps compact rows readable and keyboard-visible`, async ({page}, testInfo) => {
    setupRequired = false;
    sessionValid = true;
    administratorUsername = 'admin';
    await page.setViewportSize({width: viewport.width, height: viewport.height});
    await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
    await expect(page.locator('.product-identity small')).toHaveCount(0);
    if (viewport.name === 'desktop') {
      await page.screenshot({path: testInfo.outputPath('overview-clean.jpg'), type: 'jpeg', quality: 88, fullPage: true});
    }

    const tabs = ['概览', '桌面内容', '集成', '设置与恢复'];
    for (const tabName of tabs) {
      const tab = page.getByRole('tab', {name: tabName});
      await tab.click();
      const current = page.locator('.manage-page:not([hidden])');
      await expect(current.locator('.settings-section').first()).toBeVisible();
      await expect(current.locator('.settings-section').first().locator('.section-label')).toBeVisible();
      await expect(current.locator('.settings-section').first().locator('.section-content')).toBeVisible();
      await expect(current.locator('.page-intro .eyebrow, .page-intro p')).toHaveCount(0);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(1);
    }

    await page.getByRole('tab', {name: '概览'}).click();
    const wrapped = await page.locator('[data-section="collectors"] .section-main').evaluate((node) => {
      const message = document.createElement('p');
      message.className = 'exception-message';
      message.textContent = '采集器连接失败：这是一段没有空格而且非常长的中文诊断信息用于确认内容会在栏目内部自然换行而不会把整个管理页面撑出横向滚动条';
      node.append(message);
      return {
        inside: message.scrollWidth <= message.clientWidth,
      pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      };
    });
    expect(wrapped.inside).toBe(true);
    expect(wrapped.pageOverflow).toBeLessThanOrEqual(1);

    const focusedTab = page.getByRole('tab', {name: '桌面内容'});
    await page.getByRole('tab', {name: '概览'}).focus();
    await page.keyboard.press('Tab');
    await expect(focusedTab).toBeFocused();
    const focusStyle = await focusedTab.evaluate((node) => {
      const style = getComputedStyle(node);
      return {outlineStyle: style.outlineStyle, outlineWidth: Number.parseFloat(style.outlineWidth)};
    });
    expect(focusStyle.outlineStyle).not.toBe('none');
    expect(focusStyle.outlineWidth).toBeGreaterThanOrEqual(2);
    await page.screenshot({path: testInfo.outputPath(`${viewport.name}-management.jpg`), type: 'jpeg', quality: 88, fullPage: true});
  });
}

test('browser and desktop modes render identical activity IDs, order, and text including multiple media sessions', async ({context}) => {
  const twoSessions = {
    ...fixture,
    activities: [
      ...fixture.activities.filter((item) => item.id !== 'plex'),
      {id:'plex:0',icon:'play',tone:'active',title:'Plex',value:'歌曲 A',detail:'播放 · 电视'},
      {id:'plex:1',icon:'play',tone:'active',title:'Plex',value:'歌曲 B',detail:'暂停 · 手机'},
    ],
  };
  const capture = async (target) => {
    const targetPage = await context.newPage();
    await targetPage.goto(target, {waitUntil:'networkidle'});
    await targetPage.evaluate(async (data) => { const {renderDashboard} = await import('/view.js'); renderDashboard(document, data); }, twoSessions);
    const rows = await targetPage.locator('.activity-row').evaluateAll((items) => items.map((row) => ({id:row.dataset.key, text:row.textContent.replace(/\s+/g,' ').trim()})));
    const mediaClass = await targetPage.locator('[data-key="plex:0"]').getAttribute('class');
    await targetPage.close();
    return {rows, mediaClass};
  };
  const browser = await capture(baseURL);
  const desktop = await capture(`${baseURL}/?desktop=1`);
  expect(desktop.rows).toEqual(browser.rows);
  expect(browser.rows.filter((item) => item.id.startsWith('plex:')).map((item) => item.id)).toEqual(['plex:0','plex:1']);
  expect(desktop.mediaClass).toContain('activity-row--media');
});

test('download activity hides names, aligns its icon, and renders every task progress', async ({page}) => {
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await page.evaluate(async (base) => {
    const {renderDashboard} = await import('/view.js');
    renderDashboard(document, {
      ...base,
      activities: [
        ...base.activities,
        {id:'downloads',icon:'download',tone:'active',title:'下载',value:'3 个 · 6.7 MB/s',detail:'Private.Movie.mkv · 68%',progress:[68,20,5]},
      ],
    });
  }, fixture);

  const download = page.locator('[data-key="downloads"]');
  await expect(download).toHaveClass(/activity-row--download/);
  await expect(page.getByText('Private.Movie.mkv', {exact: false})).toHaveCount(0);
  await expect(download.locator('[data-field="detail"]')).toBeHidden();
  await expect(download.locator('.download-progress')).toHaveCount(3);
  await expect(download.locator('.download-progress-value')).toHaveText(['68%', '20%', '5%']);
  await expect(download.locator('.download-progress-label')).toHaveText(['任务 1', '任务 2', '任务 3']);
  const iconOffset = await page.locator('.glass-panel').evaluate((panel) => {
    const weather = panel.querySelector('[data-key="weather"] .activity-icon svg').getBoundingClientRect();
    const downloadIcon = panel.querySelector('[data-key="downloads"] .activity-icon svg').getBoundingClientRect();
    return Math.abs(weather.left - downloadIcon.left);
  });
  expect(iconOffset).toBeLessThanOrEqual(2);
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
  await page.getByLabel('管理员用户名').fill('Owner');
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
  expect(lastSetupComplete.administrator_username).toBe('Owner');
  expect(await page.locator('body').textContent()).not.toContain('temporary-api-secret');
});

test('legacy administrator login and username change preserve the password but revoke sessions', async ({page}) => {
  setupRequired = false;
  sessionValid = false;
  administratorUsername = 'admin';
  administratorPassword = 'correct horse battery staple';
  await page.goto(`${baseURL}/login`, {waitUntil: 'domcontentloaded'});
  const username = page.getByLabel('管理员用户名');
  const password = page.getByLabel('管理员密码');
  await username.fill('somebody');
  await password.fill('correct horse battery staple');
  await page.getByRole('button', {name: '登录'}).click();
  await expect(page.getByRole('alert')).toHaveText('用户名或密码不正确');
  await expect(username).toHaveValue('somebody');
  await expect(password).toHaveValue('');
  await username.fill('admin');
  await password.fill('correct horse battery staple');
  await page.getByRole('button', {name: '登录'}).click();
  await page.waitForURL('**/manage');
  await expect(page.getByRole('heading', {name: '管理中心'})).toBeVisible();
  await expect(page.locator('#current-username')).toHaveText('admin');

  await page.getByRole('tab', {name: '设置与恢复'}).click();
  await page.getByText('修改登录用户名', {exact: true}).click();
  const usernameForm = page.locator('#username-form');
  await usernameForm.getByLabel('新用户名').fill('Owner');
  await usernameForm.getByLabel('当前密码').fill('correct horse battery staple');
  await usernameForm.getByRole('button', {name: '修改用户名'}).click();
  await page.waitForURL('**/login');
  expect(sessionValid).toBe(false);

  await username.fill('admin');
  await password.fill('correct horse battery staple');
  await page.getByRole('button', {name: '登录'}).click();
  await expect(page.getByRole('alert')).toHaveText('用户名或密码不正确');
  await expect(password).toHaveValue('');
  await username.fill('owner');
  await password.fill('correct horse battery staple');
  await page.getByRole('button', {name: '登录'}).click();
  await page.waitForURL('**/manage');
  await expect(page.locator('#current-username')).toHaveText('Owner');
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
  sessionValid = true; integrationInstances = []; integrationMutationRequests = 0; mockPlexVisible = false;
  await page.goto(`${baseURL}/manage`, {waitUntil: 'networkidle'});
  const integrationAPI = await page.evaluate(async () => { const response = await fetch('/api/manage/integrations'); return {status: response.status, body: await response.text()}; });
  expect(integrationAPI.status, integrationAPI.body).toBe(200);
  await page.waitForTimeout(100);
  expect(pageErrors).toEqual([]);
  await page.getByRole('tab', {name: '集成'}).click();
  const card = page.locator('[data-integration="plex"]');
  await expect(card).toBeVisible();
  await page.screenshot({path: testInfo.outputPath('integration-center.jpg'), type: 'jpeg', quality: 88, fullPage: true});
  await card.getByRole('button', {name: '添加'}).click();
  const dialog = page.locator('#integration-dialog');
  await expect(dialog.getByText('为每个播放终端分别显示当前播放会话。')).toBeVisible();
  await expect(dialog.getByText('登录 Plex Web 后复制 X-Plex-Token。')).toBeVisible();
  await expect(page.locator('[name="call_timeout"]')).not.toBeVisible();
  await page.getByText('高级设置', {exact: true}).click();
  await expect(page.locator('[name="call_timeout"]')).toBeVisible();
  await page.screenshot({path: testInfo.outputPath('integration-dialog-guidance.jpg'), type: 'jpeg', quality: 88});
  await page.locator('[name="url"]').fill('http://plex.local:32400');
  await page.locator('[name="token"]').fill('plex-secret');
  await page.getByRole('button', {name: '测试连接'}).click();
  await expect(page.getByText('Plex 连接成功')).toBeVisible();
  await page.getByRole('button', {name: '保存'}).click();
  await expect(card.getByText('http://plex.local:32400')).toBeVisible();
  await expect(card.getByText('已启用')).toBeVisible();
  expect(integrationMutationRequests).toBe(1);
  await card.getByRole('button', {name: '设置'}).click();
  await expect(page.locator('[name="token"]')).toHaveValue('********');
  await page.getByRole('button', {name: '取消'}).click();
  await card.getByRole('button', {name: '设置'}).click();
  await page.getByRole('button', {name: '保存'}).click();
  await page.waitForTimeout(100);
  expect(integrationMutationRequests).toBe(2);
  const enabledDashboard = await page.evaluate(async () => (await fetch('/api/dashboard')).json());
  expect(enabledDashboard.activities.some((item) => item.id === 'plex')).toBe(true);
  await page.locator('[data-integration="plex"]').getByRole('button', {name: '停用'}).click();
  const disabledDashboard = await page.evaluate(async () => (await fetch('/api/dashboard')).json());
  expect(disabledDashboard.activities.some((item) => item.id === 'plex')).toBe(false);
  mockPlexVisible = true;
});

test('Home Assistant discovery lists separate fan and wattage choices without requiring a selected fan', async ({page}) => {
  sessionValid = true; setupRequired = false;
  const definition = {id:'home_assistant',metadata:{name:'Home Assistant 设备',category:'devices',description:'选择家中的设备'},fields:[
    {key:'url',kind:'url',label:'服务地址',required:true},
    {key:'entity_id',kind:'entity_id',label:'风扇实体',required:true},
    {key:'power_entity_id',kind:'entity_id',label:'NAS 功耗实体'},
    {key:'token',kind:'secret',label:'长期令牌',required:true},
  ]};
  await page.route('**/api/manage/integrations',route=>route.fulfill({json:{catalog:[definition],instances:[],health:[]}}));
  await page.route('**/api/manage/integrations/entities',async route=>{
    const request=route.request().postDataJSON();
    expect(request.config.entity_id).toBeUndefined();
    expect(request.secrets.token).toBe('local-test-token');
    await route.fulfill({json:{entities:[{id:'fan.room',name:'客厅风扇',kind:'fan'},{id:'sensor.nas_power',name:'NAS功耗',kind:'power',unit:'W'}]}});
  });
  await page.goto(`${baseURL}/manage`,{waitUntil:'networkidle'});
  await page.getByRole('tab',{name:'集成'}).click();
  await page.locator('[data-integration="home_assistant"]').getByRole('button',{name:'添加'}).click();
  await page.locator('[name="url"]').fill('http://ha.local:8123');
  await page.locator('[name="token"]').fill('local-test-token');
  await page.getByRole('button',{name:'读取风扇与功耗设备'}).click();
  await expect(page.locator('#ha-options-entity_id option')).toHaveCount(1);
  await expect(page.locator('#ha-options-entity_id option')).toHaveAttribute('value','fan.room');
  await expect(page.locator('#ha-options-power_entity_id option')).toHaveAttribute('value','sensor.nas_power');
  await page.locator('[name="entity_id"]').fill('fan.manual');
  await expect(page.locator('[name="entity_id"]')).toHaveValue('fan.manual');
});
