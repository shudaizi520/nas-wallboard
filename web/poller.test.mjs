import assert from 'node:assert/strict';
import test from 'node:test';

import { createPoller, visibleRefreshInterval } from './poller.js';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function fakeEnvironment(initialVisible = true) {
  let visible = initialVisible;
  let visibilityHandler = () => {};
  let nextID = 1;
  const timers = new Map();
  const delays = [];
  return {
    setTimeout(callback, delay) {
      const id = nextID++;
      timers.set(id, callback);
      delays.push(delay);
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
    isVisible: () => visible,
    onVisibilityChange(handler) {
      visibilityHandler = handler;
      return () => { visibilityHandler = () => {}; };
    },
    setVisible(value) {
      visible = value;
      visibilityHandler(value);
    },
    runNext() {
      const entry = timers.entries().next().value;
      assert.ok(entry, 'expected a scheduled timer');
      const [id, callback] = entry;
      timers.delete(id);
      callback();
    },
    timerCount: () => timers.size,
    delays,
  };
}

async function settle() {
  await Promise.resolve();
  await Promise.resolve();
}

test('desktop mode refreshes less often than the interactive page', () => {
  assert.equal(visibleRefreshInterval('?desktop=1'), 15_000);
  assert.equal(visibleRefreshInterval(''), 5_000);
  assert.equal(visibleRefreshInterval('?desktop=0'), 5_000);
});

test('failed fetch keeps the last good data', async () => {
  const environment = fakeEnvironment();
  const responses = [{ hostname: 'atlas' }, new Error('offline')];
  const received = [];
  const errors = [];
  const poller = createPoller({
    fetchStatus: async () => {
      const value = responses.shift();
      if (value instanceof Error) throw value;
      return value;
    },
    onData: (value) => received.push(value),
    onError: (error) => errors.push(error.message),
    visibleEvery: 5_000,
    hiddenEvery: 60_000,
    environment,
  });
  poller.start();
  await settle();
  environment.runNext();
  await settle();
  assert.deepEqual(received, [{ hostname: 'atlas' }]);
  assert.deepEqual(errors, ['offline']);
  poller.stop();
});

test('requests never overlap', async () => {
  const environment = fakeEnvironment();
  const pending = deferred();
  let calls = 0;
  const poller = createPoller({
    fetchStatus: () => { calls += 1; return pending.promise; },
    onData: () => {},
    onError: () => {},
    visibleEvery: 5_000,
    hiddenEvery: 60_000,
    environment,
  });
  poller.start();
  poller.refresh();
  poller.refresh();
  assert.equal(calls, 1);
  pending.resolve({ ok: true });
  await settle();
  assert.equal(calls, 1);
  poller.stop();
});

test('hidden pages use the slower interval', async () => {
  const environment = fakeEnvironment(false);
  const poller = createPoller({
    fetchStatus: async () => ({}),
    onData: () => {},
    onError: () => {},
    visibleEvery: 5_000,
    hiddenEvery: 60_000,
    environment,
  });
  poller.start();
  await settle();
  assert.equal(environment.delays.at(-1), 60_000);
  poller.stop();
});

test('visibility restore causes exactly one immediate refresh', async () => {
  const environment = fakeEnvironment(false);
  let calls = 0;
  const poller = createPoller({
    fetchStatus: async () => { calls += 1; return {}; },
    onData: () => {},
    onError: () => {},
    visibleEvery: 5_000,
    hiddenEvery: 60_000,
    environment,
  });
  poller.start();
  await settle();
  assert.equal(calls, 1);
  environment.setVisible(true);
  environment.setVisible(true);
  await settle();
  assert.equal(calls, 2);
  assert.equal(environment.timerCount(), 1);
  poller.stop();
});
