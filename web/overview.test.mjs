import test from 'node:test';
import assert from 'node:assert/strict';
import {healthCards, createOverviewPoller} from './overview.js';

test('overview health cards turn failures into actionable Chinese status', () => {
  const cards = healthCards({nas: {connected: false, version: '25.10', last_update: '2026-09-28T00:00:00Z'}, integrations: [
    {instance_id: 'plex-main', type: 'plex', running: false, healthy: false, message: '采集器运行失败'},
  ]});
  assert.equal(cards[0].tone, 'bad');
  assert.match(cards[0].detail, /检查/);
  assert.equal(cards[1].tone, 'bad');
  assert.match(cards[1].detail, /集成中心/);
});

test('overview poller stops while hidden and resumes with one immediate refresh', async () => {
  const listeners = new Map();
  const documentRef = {hidden: false, addEventListener(name, listener) { listeners.set(name, listener); }, removeEventListener() {}};
  const timers = [];
  let calls = 0;
  const poller = createOverviewPoller(documentRef, async () => { calls += 1; }, (callback) => { timers.push(callback); return timers.length; }, () => {});
  await poller.start();
  assert.equal(calls, 1);
  documentRef.hidden = true;
  listeners.get('visibilitychange')();
  await timers.at(-1)?.();
  assert.equal(calls, 1);
  documentRef.hidden = false;
  await listeners.get('visibilitychange')();
  assert.equal(calls, 2);
  poller.stop();
  await poller.start();
  assert.equal(calls, 3);
  poller.stop();
});
