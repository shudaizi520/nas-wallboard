import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const fixture = JSON.parse(await readFile(join(root, 'tests/e2e/fixture.json'), 'utf8'));

test('desktop marks loading dimensions provisional until configured render', async ({page}) => {
  let releaseDashboard;
  const dashboardGate = new Promise((resolve) => { releaseDashboard = resolve; });
  await page.addInitScript(() => {
    window.hostSizes = [];
    window.chrome = {webview: {postMessage(message) { window.hostSizes.push(message); }, addEventListener() {}}};
  });
  await page.route('http://wallboard.test/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/dashboard') {
      await dashboardGate;
      await route.fulfill({json: {...fixture, width: 360}});
    } else if (path === '/api/setup/status') {
      await route.fulfill({json: {setup_required: false}});
    } else {
      const file = path === '/' ? 'index.html' : path.slice(1);
      const body = await readFile(join(root, 'web', file));
      const contentType = file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html';
      await route.fulfill({body, contentType});
    }
  });
  await page.goto('http://wallboard.test/?desktop=1');
  await expect.poll(() => page.evaluate(() => window.hostSizes.length)).toBeGreaterThan(0);
  const provisional = await page.evaluate(() => window.hostSizes);
  expect(provisional.every((message) => message.ready === false)).toBe(true);
  expect(provisional.at(-1).width).toBe(560);
  releaseDashboard();
  await expect.poll(() => page.evaluate(() => window.hostSizes.findLast((message) => message.ready)?.width)).toBe(360);
  const sizes = await page.evaluate(() => window.hostSizes);
  expect(sizes.find((message) => message.ready).width).toBe(360);
});
