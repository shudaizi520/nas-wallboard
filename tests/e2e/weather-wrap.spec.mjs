import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const fixture = JSON.parse(await readFile(join(root, 'tests/e2e/fixture.json'), 'utf8'));

for (const width of [300, 360]) {
  test(`weather at ${width}px renders complete multi-line labels and warnings and resizes desktop`, async ({page}) => {
    await page.setViewportSize({width: 600, height: 700});
    let dashboard = {...fixture, width};
    await page.addInitScript(() => {
      window.hostSizes = [];
      window.chrome = {webview: {postMessage(message) { window.hostSizes.push(message); }, addEventListener() {}}};
    });
    await page.route('http://wallboard.test/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/dashboard') {
        await route.fulfill({json: dashboard});
      } else if (path === '/api/setup/status') {
        await route.fulfill({json: {setup_required: false}});
      } else {
        const file = path === '/' ? 'index.html' : path.slice(1);
        const contentType = file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : file.endsWith('.png') ? 'image/png' : 'text/html';
        await route.fulfill({body: await readFile(join(root, 'web', file)), contentType});
      }
    });
    await page.goto('http://wallboard.test/?desktop=1');
    await expect.poll(() => page.evaluate(() => window.hostSizes.findLast((message) => message.ready)?.width)).toBe(width);
    const shortHeight = await page.evaluate(() => window.hostSizes.findLast((message) => message.ready).height);
    const short = dashboard;
    dashboard = {...dashboard, activities: [
      {id: 'weather', icon: 'weather-cloudy', tone: 'bad', value: '28° · 晴间多云', detail: '暴雨橙色预警\n雷雨大风黄色预警\n雷电黄色预警'},
      ...dashboard.activities.slice(1),
    ]};
    await page.evaluate(async (value) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, value);
    }, dashboard);
    const row = page.locator('[data-key="weather"]');
    const dimensions = await row.evaluate((node) => {
      const measure = (element) => {
        const rect = element.getBoundingClientRect();
        return {height: rect.height, width: rect.width, top: rect.top, bottom: rect.bottom, left: rect.left, scrollWidth: element.scrollWidth, scrollHeight: element.scrollHeight, lineHeight: parseFloat(getComputedStyle(element).lineHeight)};
      };
      return {
        value: measure(node.querySelector('[data-field="value"]')),
        conditionLines: (() => {
          const value = node.querySelector('[data-field="value"]');
          const range = document.createRange();
          range.setStart(value.firstChild, value.textContent.indexOf('晴'));
          range.setEnd(value.firstChild, value.textContent.length);
          return range.getClientRects().length;
        })(),
        detail: measure(node.querySelector('[data-field="detail"]')),
        row: measure(node),
        next: measure(node.nextElementSibling),
        metric: measure(document.querySelector('[data-key="cpu"] [data-field="value"]')),
      };
    });
    expect(dimensions.value.height).toBeGreaterThan(dimensions.value.lineHeight + 1);
    expect(dimensions.conditionLines).toBe(1);
    for (const bounds of [dimensions.value, dimensions.detail]) {
      expect(bounds.scrollWidth).toBeLessThanOrEqual(bounds.width + 1);
      expect(bounds.scrollHeight).toBeLessThanOrEqual(bounds.height + 1);
      expect(bounds.bottom).toBeLessThanOrEqual(dimensions.row.bottom + 1);
    }
    expect(dimensions.detail.height).toBeGreaterThanOrEqual(dimensions.detail.lineHeight * 3 - 1);
    expect(Math.abs(dimensions.detail.left - dimensions.metric.left)).toBeLessThanOrEqual(1);
    expect(dimensions.next.top).toBeGreaterThanOrEqual(dimensions.row.bottom - 1);
    await expect.poll(() => page.evaluate(() => window.hostSizes.findLast((message) => message.ready).height)).toBeGreaterThan(shortHeight);
    expect(await page.evaluate(() => window.hostSizes.findLast((message) => message.ready).width)).toBe(width);
    if (width === 360) {
      await page.locator('.glass-panel').screenshot({path: test.info().outputPath('weather-three-warnings.png')});
    }
    dashboard = short;
    await page.evaluate(async (value) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, value);
    }, dashboard);
    await expect.poll(() => page.evaluate(() => window.hostSizes.findLast((message) => message.ready).height)).toBe(shortHeight);
    const forecast = page.locator('[data-key="weather:forecast:0"] [data-field="value"]');
    const originalForecastHeight = (await forecast.boundingBox()).height;
    dashboard = {...short, activities: short.activities.map((activity) => activity.id === 'weather:forecast:0'
      ? {...activity, value: '雷阵雨伴有冰雹', detail: '25°–30° · 雨88%\n请留意雷雨变化'} : activity)};
    await page.evaluate(async (value) => {
      const {renderDashboard} = await import('/view.js');
      renderDashboard(document, value);
    }, dashboard);
    const future = await forecast.evaluate((node) => {
      const row = node.closest('.activity-row');
      const detail = row.querySelector('[data-field="detail"]');
      return {height: node.getBoundingClientRect().height, width: node.getBoundingClientRect().width, scrollWidth: node.scrollWidth,
        detailHeight: detail.getBoundingClientRect().height, detailScrollHeight: detail.scrollHeight,
        bottom: row.getBoundingClientRect().bottom, nextTop: row.nextElementSibling.getBoundingClientRect().top};
    });
    expect(future.height).toBeGreaterThan(originalForecastHeight + 1);
    expect(future.scrollWidth).toBeLessThanOrEqual(future.width + 1);
    expect(future.detailScrollHeight).toBeLessThanOrEqual(future.detailHeight + 1);
    expect(future.nextTop).toBeGreaterThanOrEqual(future.bottom - 1);
  });
}
