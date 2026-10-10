import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const fixture = JSON.parse(await readFile(join(root, 'tests/e2e/fixture.json'), 'utf8'));
const dashboard = {
  ...fixture,
  activities: [
    ...fixture.activities.map(item => item.id === 'alert:a1' ? {...item, title: '提醒'} : item),
    {id: 'downloads', icon: 'download', tone: 'active', title: '下载', value: '1 个 · 6.7 MB/s', progress: [68]},
    {id: 'custom-device', icon: 'app', tone: 'neutral', title: '设备', value: '正常'},
  ],
};

for (const viewportWidth of [600, 1920]) {
  test(`all activity labels match metric text at ${viewportWidth}px viewport`, async ({page}) => {
    await page.setViewportSize({width: viewportWidth, height: 1000});
    await page.route('http://wallboard.test/**', async route => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/dashboard') return route.fulfill({json: dashboard});
      if (path === '/api/setup/status') return route.fulfill({json: {setup_required: false}});
      const file = path === '/' ? 'index.html' : path.slice(1);
      return route.fulfill({
        body: await readFile(join(root, 'web', file)),
        contentType: file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html',
      });
    });
    await page.goto('http://wallboard.test/?desktop=1');
    const metric = page.locator('[data-key="cpu"] [data-field="label"]');
    await expect(metric).toHaveCSS('font-size', '12.8px');
    for (const id of ['fan', 'plex', 'downloads', 'alert:a1', 'custom-device']) {
      const row = page.locator(`[data-key="${id}"]`);
      const title = row.locator('[data-field="title"]');
      await expect(title).toHaveCSS('font-size', '12.8px');
      await expect(title).toHaveCSS('font-weight', '500');
      const bounds = await title.evaluate(node => {
        const r = node.getBoundingClientRect(), parent = node.closest('.activity-row').getBoundingClientRect();
        return {top: r.top, bottom: r.bottom, rowTop: parent.top, rowBottom: parent.bottom, overflow: node.scrollWidth > node.clientWidth + 1};
      });
      expect(bounds.top).toBeGreaterThanOrEqual(bounds.rowTop);
      expect(bounds.bottom).toBeLessThanOrEqual(bounds.rowBottom);
      expect(bounds.overflow).toBe(false);
    }
  });
}
