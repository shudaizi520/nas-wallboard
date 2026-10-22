import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const fixture = JSON.parse(await readFile(join(root, 'tests/e2e/fixture.json'), 'utf8'));

// Removing the hourly temperature's own color must fail this test: a parent
// warning must not recolor ordinary temperatures or obscure warning severity.
for (const width of [300, 360]) test(`hourly temperatures stay neutral through warning changes at ${width}px`, async ({page}) => {
  await page.setViewportSize({width: 620, height: 1000});
  await page.route('http://wallboard.test/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/dashboard') return route.fulfill({json: {...fixture, width}});
    if (path === '/api/setup/status') return route.fulfill({json: {setup_required: false}});
    const file = path === '/' ? 'index.html' : path.slice(1);
    return route.fulfill({body: await readFile(join(root, 'web', file)), contentType: file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  await expect(page.locator('.glass-panel')).toHaveAttribute('data-ready', 'true');
  const forecast_hours = [29, 30, 30, 29, 28, 27].map((temperature, i) => ({
    at: `2026-10-22T${12 + i * 2}:00:00+08:00`, label: `${12 + i * 2}时`,
    icon: i < 3 ? 'weather-sunny' : 'weather-moon', condition: '晴', temperature: `${temperature}°`, rain: i === 0 ? '65%' : '',
  }));
  const weather = {...fixture.activities.find(a => a.id === 'weather'), value: '26° · 晴', value_tone: 'neutral', forecast_hours};
  const row = page.locator('[data-key="weather"]');
  let originalHeight;
  for (const [color, tone, warningColor] of [
    ['orange', 'bad', 'rgb(255, 173, 85)'], ['red', 'bad', 'rgb(255, 125, 135)'],
    ['yellow', 'warn', 'rgb(245, 211, 93)'], ['blue', 'warn', 'rgb(131, 187, 255)'],
    ['purple', 'bad', 'rgb(198, 165, 231)'], ['black', 'bad', 'rgb(198, 165, 231)'],
    ['', 'active', null],
  ]) {
    const detail = color ? '森林火险预警' : '';
    const activity = {...weather, tone, detail, detail_parts: color ? [{text: detail, color}] : undefined};
    await page.evaluate(async raw => {
      const {renderDashboard} = await import('/view.js'); renderDashboard(document, raw);
    }, {...fixture, width, activities: [activity]});
    await expect(row).toHaveAttribute('data-tone', tone);
    await expect(row.locator('.weather-hour [data-field="temperature"]')).toHaveText(['29°', '30°', '30°', '29°', '28°', '27°']);
    for (const temperature of await row.locator('.weather-hour [data-field="temperature"]').all()) {
      await expect(temperature).toHaveCSS('color', 'rgba(247, 250, 255, 0.96)');
      await expect(temperature).toHaveCSS('font-size', '13.6px');
    }
    await expect(row.locator('.activity-line [data-field="value"]')).toHaveCSS('color', 'rgba(235, 243, 255, 0.82)');
    await expect(row.locator('.weather-hour [data-field="rain"]').first()).toHaveCSS('color', 'rgb(105, 217, 237)');
    if (color) await expect(row.locator(`[data-warning-color="${color}"]`)).toHaveCSS('color', warningColor);
    else await expect(row.locator('[data-warning-color]')).toHaveCount(0);
    const layout = await row.locator('.weather-hourly').evaluate(n => ({height: n.getBoundingClientRect().height, overflow: n.scrollWidth > n.clientWidth + 1}));
    originalHeight ??= layout.height;
    expect(layout.height).toBe(originalHeight);
    expect(layout.overflow).toBe(false);
  }
});

test('weather keeps current text neutral and independently colors warning details', async ({page}) => {
  await page.setViewportSize({width: 600, height: 700});
  await page.route('http://wallboard.test/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/dashboard') return route.fulfill({json: fixture});
    if (path === '/api/setup/status') return route.fulfill({json: {setup_required: false}});
    const file = path === '/' ? 'index.html' : path.slice(1);
    const contentType = file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html';
    return route.fulfill({body: await readFile(join(root, 'web', file)), contentType});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  await expect(page.locator('.glass-panel')).toHaveAttribute('data-ready', 'true');
  const weather = {id: 'weather', icon: 'weather-cloudy', tone: 'bad', value_tone: 'neutral', value: '27° · 晴间多云', detail: '暴雨橙色预警\n雷电黄色预警', detail_parts: [
    {text: '暴雨橙色预警', color: 'orange'}, {text: '\n', color: ''}, {text: '雷电黄色预警', color: 'yellow'},
  ]};
  const render = async (activity) => page.evaluate(async (dashboard) => {
    const {renderDashboard} = await import('/view.js');
    renderDashboard(document, dashboard);
  }, {...fixture, activities: [activity, ...fixture.activities.slice(1)]});
  await render(weather);
  const row = page.locator('[data-key="weather"]');
  const value = row.locator('[data-field="value"]');
  const detail = row.locator('[data-field="detail"]');
  await expect(value).toHaveCSS('color', 'rgba(235, 243, 255, 0.82)');
  await expect(detail.locator('[data-warning-color="orange"]')).toHaveCSS('color', 'rgb(255, 173, 85)');
  await expect(detail.locator('[data-warning-color="yellow"]')).toHaveCSS('color', 'rgb(245, 211, 93)');
  await expect(detail).toHaveText(weather.detail);
  // Same plain text after warning removal must clear colored descendants too.
  await render({...weather, tone: 'active', value_tone: undefined, detail_parts: undefined});
  await expect(detail.locator('[data-warning-color]')).toHaveCount(0);
  await expect(value).toHaveCSS('color', 'rgba(235, 243, 255, 0.82)');
  // Failure text is still visibly bad; neutralizing real values must not hide an outage.
  await render({...weather, value: '不可用', value_tone: undefined});
  await expect(value).toHaveCSS('color', 'rgb(255, 125, 135)');
  await render({...weather, detail: '<img src=x onerror="window.warningInjected=true">', detail_parts: [{text: '<img src=x onerror="window.warningInjected=true">', color: 'red'}]});
  await expect(detail.locator('img')).toHaveCount(0);
  expect(await page.evaluate(() => window.warningInjected)).toBeUndefined();
});
