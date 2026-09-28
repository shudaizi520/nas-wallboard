import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

import {normalizeDashboard, renderFetchError} from './view.js';

test('generic dashboard keeps configured metric and activity order', () => {
  const view = normalizeDashboard({
    width: 560,
    connection_tone: 'good',
    uptime: '5天 6小时',
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

test('fetch error changes only the connection indicator', () => {
  const connection = {dataset: {tone: 'good'}};
  const cpu = {textContent: '18.4%'};
  const root = {querySelector(selector) {
    if (selector === '[data-bind="connection"]') return connection;
    if (selector === '[data-key="cpu"]') return cpu;
    return null;
  }};

  renderFetchError(root);

  assert.equal(connection.dataset.tone, 'bad');
  assert.equal(cpu.textContent, '18.4%');
});
