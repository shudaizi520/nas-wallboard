import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root = new URL('../..', import.meta.url).pathname;
const css = (await readFile(join(root, 'web', 'styles.css'), 'utf8'))
  .replaceAll(/(?:-webkit-)?backdrop-filter:[^;]+;/g, '');

test.use({launchOptions: {args: ['--disable-gpu']}});

test('dashboard values share one left-aligned column', async ({page}) => {
  await page.setViewportSize({width: 600, height: 700});
  await page.setContent(`
    <style>${css}</style>
    <section class="glass-panel" style="position:relative;inset:auto;width:300px">
      <header class="health-strip">
        <div class="nas-summary">
          <span class="nas-mark"><i class="connection-dot"></i><b>NAS</b></span>
          <span class="nas-meta"><span class="nas-uptime" data-bind="uptime">7天</span><span class="nas-power" data-bind="nas-power">75W</span></span>
        </div>
        <div class="health-metrics">
          <span class="health-value" data-key="cpu"><small>处理器</small><b data-field="value">8% · 60°</b></span>
          <span class="health-value" data-key="disk_temperature"><small>机械硬盘</small><b data-field="value">43°</b></span>
          <span class="health-value" data-key="network"><small>实时网速</small><span class="network-rates" data-field="rates"><i>↓ 49.2 KB/s</i><i>↑ 528 KB/s</i></span></span>
        </div>
      </header>
      <div class="activity-list">
        <article class="activity-row activity-row--weather" data-key="weather"><span class="activity-icon"></span><div class="activity-copy"><div class="activity-line"><strong></strong><span data-field="value">34° 多云</span></div><p data-field="detail">10分钟后开始下小雨</p></div><i class="activity-signal"></i></article>
        <article class="activity-row activity-row--forecast" data-key="weather:forecast:0"><span class="activity-icon"></span><div class="activity-copy"><div class="activity-line"><strong></strong><span data-field="value">少云</span></div><p data-field="detail">27°–35° · 雨28%</p></div><i class="activity-signal"></i></article>
        <article class="activity-row activity-row--fan" data-key="fan"><span class="activity-icon"></span><div class="activity-copy"><div class="activity-line"><strong>风扇</strong><span data-field="value">66% · 直吹风</span></div><p></p></div><i class="activity-signal"></i></article>
        <article class="activity-row activity-row--media" data-key="plex"><span class="activity-icon"><svg></svg></span><div class="activity-copy"><div class="activity-line"><strong>Plex</strong><span data-field="value">江峰 · 一起摇摆</span></div><p data-field="detail">暂停 · Shudaizi</p></div><i class="activity-signal"></i></article>
      </div>
    </section>
  `);

  const alignment = await page.locator('.glass-panel').evaluate((panel) => {
    const left = (selector) => Math.round(panel.querySelector(selector).getBoundingClientRect().left);
    const starts = [
      left('[data-bind="uptime"]'),
      left('[data-key="cpu"] [data-field="value"]'),
      left('[data-key="disk_temperature"] [data-field="value"]'),
      left('[data-key="network"] [data-field="rates"]'),
      left('[data-key="weather"] [data-field="detail"]'),
      left('[data-key="weather:forecast:0"] [data-field="detail"]'),
      left('[data-key="fan"] [data-field="value"]'),
      left('[data-key="plex"] [data-field="value"]'),
    ];
    return {starts, mediaDetail: left('[data-key="plex"] [data-field="detail"]'), spread: Math.max(...starts) - Math.min(...starts)};
  });

  expect(alignment.spread, alignment.starts.join(', ')).toBeLessThanOrEqual(2);
  expect(alignment.mediaDetail).toBe(alignment.starts.at(-1));

  const labelStarts = await page.locator('.glass-panel').evaluate((panel) => {
    const left = (selector) => Math.round(panel.querySelector(selector).getBoundingClientRect().left);
    return [
      left('[data-key="weather:forecast:0"] [data-field="value"]'),
      left('[data-key="fan"] strong'),
      left('[data-key="plex"] strong'),
    ];
  });
  expect(Math.max(...labelStarts) - Math.min(...labelStarts), labelStarts.join(', ')).toBeLessThanOrEqual(1);

  const compactRows = await page.locator('.activity-list').evaluate((list) => ({
    media: list.querySelector('[data-key="plex"]').getBoundingClientRect().height,
    fan: list.querySelector('[data-key="fan"]').getBoundingClientRect().height,
    mediaLabelCenter: (() => {
      const bounds = list.querySelector('[data-key="plex"] strong').getBoundingClientRect();
      return bounds.top + bounds.height / 2;
    })(),
    mediaCopyCenter: (() => {
      const title = list.querySelector('[data-key="plex"] [data-field="value"]').getBoundingClientRect();
      const detail = list.querySelector('[data-key="plex"] [data-field="detail"]').getBoundingClientRect();
      return (title.top + detail.bottom) / 2;
    })(),
    bottomGap: (() => {
      const panel = list.closest('.glass-panel').getBoundingClientRect();
      const last = list.lastElementChild.getBoundingClientRect();
      return panel.bottom - last.bottom;
    })(),
    mediaInsets: (() => {
      const row = list.querySelector('[data-key="plex"]').getBoundingClientRect();
      const title = list.querySelector('[data-key="plex"] [data-field="value"]').getBoundingClientRect();
      const detail = list.querySelector('[data-key="plex"] [data-field="detail"]').getBoundingClientRect();
      return [title.top - row.top, row.bottom - detail.bottom];
    })(),
    fanCenterDelta: (() => {
      const row = list.querySelector('[data-key="fan"]').getBoundingClientRect();
      const label = list.querySelector('[data-key="fan"] strong').getBoundingClientRect();
      return Math.abs((row.top + row.height / 2) - (label.top + label.height / 2));
    })(),
  }));
  expect(compactRows.media).toBeLessThanOrEqual(44);
  expect(compactRows.fan).toBeLessThanOrEqual(32);
  expect(Math.abs(compactRows.mediaLabelCenter - compactRows.mediaCopyCenter)).toBeLessThanOrEqual(1);
  expect(compactRows.bottomGap).toBeLessThanOrEqual(18);
  expect(Math.abs(compactRows.mediaInsets[0] - compactRows.mediaInsets[1])).toBeLessThanOrEqual(2);
  expect(compactRows.fanCenterDelta).toBeLessThanOrEqual(1);
});

test('current weather detail grows to fit all lines and shrinks back when short', async ({page}) => {
  await page.setViewportSize({width: 600, height: 700});
  await page.setContent(`
    <style>${css}</style>
    <section class="glass-panel" style="position:relative;inset:auto;width:300px">
      <div class="activity-list">
        <article class="activity-row activity-row--weather" data-key="weather">
          <span class="activity-icon"></span>
          <div class="activity-copy">
            <div class="activity-line"><strong></strong><span data-field="value">33° 多云</span></div>
            <p data-field="detail">体感31° · 湿度64%</p>
          </div>
          <i class="activity-signal"></i>
        </article>
      </div>
    </section>
  `);

  const detail = page.locator('[data-key="weather"] [data-field="detail"]');
  const measure = () => detail.evaluate((node) => {
    const style = getComputedStyle(node);
    return {
      height: node.getBoundingClientRect().height,
      width: node.getBoundingClientRect().width,
      lineHeight: Number.parseFloat(style.lineHeight),
      overflow: style.overflow,
      display: style.display,
      whiteSpace: style.whiteSpace,
      lineClamp: style.webkitLineClamp,
      scrollWidth: node.scrollWidth,
      scrollHeight: node.scrollHeight,
    };
  });

  const short = await measure();
  await detail.evaluate((node) => { node.textContent = '强风6级 · 紫外线8'; });
  const hazard = await measure();
  await detail.evaluate((node) => { node.textContent = '25分钟后开始下大雨，45分钟后雨势逐渐减弱'; });
  const long = await measure();
  await detail.evaluate((node) => { node.textContent = '25分钟后开始下大雨，45分钟后雨势逐渐减弱，随后可能再次出现强降雨，请注意关窗'; });
  const veryLong = await measure();

  expect(short.height).toBeLessThanOrEqual(short.lineHeight + 1);
  expect(hazard.height).toBeLessThanOrEqual(hazard.lineHeight + 1);
  expect(long.height, JSON.stringify({short, long, veryLong})).toBeGreaterThan(short.height + 1);
  expect(veryLong.height).toBeGreaterThan(short.lineHeight * 2 + 1);
  expect(veryLong.height).toBeGreaterThanOrEqual(veryLong.scrollHeight - 1);
  expect(veryLong.scrollWidth).toBeLessThanOrEqual(veryLong.width + 1);
  await detail.evaluate((node) => { node.textContent = '体感31° · 湿度64%'; });
  expect((await measure()).height).toBeCloseTo(short.height, 0);
});
