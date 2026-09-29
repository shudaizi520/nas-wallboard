import test from 'node:test';
import assert from 'node:assert/strict';
import {overviewRows, overviewSummary, createOverviewPoller} from './overview.js';

test('overview rows keep ordered product status and isolate failure tone', () => {
  const payload = {
    version: 'v1.0.0-beta.1', application_uptime_seconds: 3661,
    nas: {connected: false, version: '25.10.7', last_update: '2026-09-28T00:00:00Z'},
    integrations: [
      {instance_id: 'truenas-main', type: 'truenas', running: true, healthy: true, message: '运行中'},
      {instance_id: 'plex-main', type: 'plex', running: false, healthy: false, message: '<img src=x onerror=alert(1)> 采集器运行失败'},
    ],
  };
  const rows = overviewRows(payload);
  assert.deepEqual(rows.map((row) => row.id), ['application', 'truenas', 'collectors', 'desktop-client']);
  assert.match(rows[0].value, /v1\.0\.0-beta\.1 · 1小时 1分/);
  assert.equal(rows[0].tone, 'neutral');
  assert.equal(rows[1].value, '25.10.7');
  assert.equal(rows[1].tone, 'bad');
  assert.equal(rows[2].value, '1 正常 · 1 异常');
  assert.equal(rows[2].tone, 'bad');
  assert.equal(rows[2].detail, '<img src=x onerror=alert(1)> 采集器运行失败');
  assert.deepEqual(rows[3].actions, [
    {label: '安装桌面小组件', href: '/download/nas-wallboard-desktop.zip'},
    {label: '网页预览', href: '/', external: true},
  ]);
  assert.deepEqual(overviewSummary(payload), [
    {label: '运行状态', value: '需要关注', tone: 'bad'},
    {label: '正常采集', value: '1', tone: 'good'},
    {label: '异常采集', value: '1', tone: 'bad'},
    {label: '运行时间', value: '1小时 1分', tone: 'neutral'},
  ]);
  assert.equal(overviewSummary(payload).some((item) => item.value.includes('采集器运行失败')), false);
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
