import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

import {activityVariant, normalizeDashboard, reconcile, renderFetchError} from './view.js';

function fakeContainer(keys) {
  const container = {
    children: [],
    ownerDocument: {},
    append(node) {
      const index = this.children.indexOf(node);
      if (index >= 0) this.children.splice(index, 1);
      this.children.push(node);
      node.parent = this;
    },
    insertBefore(node, reference) {
      const index = this.children.indexOf(node);
      if (index >= 0) this.children.splice(index, 1);
      const target = reference ? this.children.indexOf(reference) : this.children.length;
      this.children.splice(target < 0 ? this.children.length : target, 0, node);
      node.parent = this;
    },
  };
  for (const key of keys) {
    const node = {dataset: {key}, remove() {
      const index = this.parent.children.indexOf(this);
      if (index >= 0) this.parent.children.splice(index, 1);
    }};
    container.append(node);
  }
  return container;
}

test('reconciliation restores configured order after conditional rows reappear', () => {
  const container = fakeContainer(['fan', 'weather', 'plex']);
  reconcile(container, [{id: 'weather'}, {id: 'plex'}, {id: 'fan'}], () => {
    throw new Error('all rows already exist');
  }, () => {});
  assert.deepEqual(container.children.map((node) => node.dataset.key), ['weather', 'plex', 'fan']);
});

test('forecast rows use a compact weather variant without day labels', () => {
  assert.equal(activityVariant('weather'), 'weather');
  assert.equal(activityVariant('weather:forecast:0'), 'weather-forecast');
  assert.equal(activityVariant('fan'), 'fan');
  assert.equal(activityVariant('plex'), 'media');
  assert.equal(activityVariant('plex:0'), 'media');
  assert.equal(activityVariant('jellyfin:1'), 'media');
  assert.equal(activityVariant('downloads'), 'download');
});

test('download activity hides source names and preserves every progress value', () => {
  const view = normalizeDashboard({
    activities: [{
      id: 'downloads', icon: 'download', tone: 'active', title: '下载',
      value: '2 个 · 12.5 MB/s', detail: 'Ubuntu.iso · 68%', progress: [68, 20],
    }],
  });

  assert.deepEqual(view.activities[0], {
    id: 'downloads', icon: 'download', tone: 'active', title: '下载',
    value: '2 个 · 12.5 MB/s', detail: '', progress: [68, 20],
  });
});

test('generic dashboard keeps configured metric and activity order', () => {
  const view = normalizeDashboard({
    width: 560,
    connection_tone: 'good',
    uptime: '5天 6小时',
    nas_power: '38W',
    metrics: [
      {id: 'custom-first', icon: 'disk', value: '40°C', tone: 'neutral'},
      {id: 'custom-second', icon: 'not-installed', value: '11%', tone: 'active'},
    ],
    activities: [
      {id: 'source:a', icon: 'updates', title: '第一项', value: '1', detail: '', tone: 'active'},
      {id: 'source:b', icon: 'download', title: '第二项', value: '', detail: '详情', tone: 'warn'},
      {id: 'source:c', icon: 'play', title: '第三项', value: '', detail: '', tone: 'active'},
      {id: 'source:d', icon: 'uptime', title: '第四项', value: '', detail: '', tone: 'bad'},
      {id: 'source:e', icon: 'future-icon', title: '第五项', value: '', detail: '', tone: 'neutral'},
    ],
  });

  assert.equal(view.width, 560);
  assert.equal(view.connectionTone, 'good');
  assert.equal(view.uptime, '5天 6小时');
  assert.equal(view.nasPower, '38W');
  assert.deepEqual(view.metrics.map(({id, icon}) => ({id, icon})), [
    {id: 'custom-first', icon: 'disk'},
    {id: 'custom-second', icon: 'app'},
  ]);
  assert.deepEqual(view.activities.map(({id, icon}) => ({id, icon})), [
    {id: 'source:a', icon: 'updates'},
    {id: 'source:b', icon: 'download'},
    {id: 'source:c', icon: 'play'},
    {id: 'source:d', icon: 'uptime'},
    {id: 'source:e', icon: 'app'},
  ]);
});

test('invalid dashboard values get safe display fallbacks', () => {
  const view = normalizeDashboard({width: 9999, connection_tone: 'unexpected', metrics: null, activities: 'bad'});
  assert.equal(view.width, 360);
  assert.equal(view.connectionTone, 'bad');
  assert.equal(view.uptime, '');
  assert.equal(view.nasPower, '');
  assert.deepEqual(view.metrics, []);
  assert.deepEqual(view.activities, []);
});

test('processor usage and temperature share one compact display row', () => {
  const view = normalizeDashboard({
    metrics: [
      {id: 'cpu', icon: 'cpu', value: '18.4%', tone: 'neutral'},
      {id: 'cpu_temperature', icon: 'cpu', value: '56°', tone: 'warn'},
      {id: 'disk_temperature', icon: 'disk', value: '40°', tone: 'neutral'},
    ],
  });

  assert.deepEqual(view.metrics, [
    {id: 'cpu', icon: 'cpu', value: '18.4% · 56°', tone: 'warn'},
    {id: 'disk_temperature', icon: 'disk', value: '40°', tone: 'neutral'},
  ]);
});

test('every external activity icon has an embedded symbol', async () => {
  const markup = await readFile(new URL('./index.html', import.meta.url), 'utf8');
  for (const icon of ['download', 'play', 'uptime', 'network', 'weather-sunny', 'weather-cloudy', 'weather-rain', 'weather-storm', 'weather-snow', 'weather-fog']) {
    assert.match(markup, new RegExp(`id="icon-${icon}"`));
  }
});

test('fetch error marks retained values as expired rather than current', () => {
  const connection = {dataset: {tone: 'good'}};
  const freshness = {textContent: '', hidden: true};
  const cpu = {textContent: '18.4%'};
  const root = {querySelector(selector) {
    if (selector === '[data-bind="connection"]') return connection;
    if (selector === '[data-bind="freshness"]') return freshness;
    if (selector === '[data-key="cpu"]') return cpu;
    return null;
  }};

  renderFetchError(root);

  assert.equal(connection.dataset.tone, 'bad');
  assert.equal(cpu.textContent, '18.4%');
  assert.equal(freshness.textContent, '数据过期');
  assert.equal(freshness.hidden, false);
});

test('dashboard carries source freshness without modifying metrics', () => {
  const stale = normalizeDashboard({connection_tone: 'bad', data_status: '数据过期', metrics: [{id: 'cpu', value: '18%'}]});
  assert.equal(stale.dataStatus, '数据过期');
  assert.equal(stale.metrics[0].value, '18%');
  assert.equal(normalizeDashboard({connection_tone: 'good'}).dataStatus, '');
});
