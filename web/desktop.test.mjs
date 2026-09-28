import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

import {startDesktopMode} from './desktop.js';

function harness(search = '?desktop=1', withBridge = true) {
  const classes = new Set();
  const messages = [];
  const listeners = new Map();
  let observerCallback = () => {};
  let disconnected = false;
  const panel = {
    rect: {width: 300.4, height: 184.6},
    getBoundingClientRect() { return this.rect; },
    addEventListener(type, listener) { listeners.set(type, listener); },
    removeEventListener(type) { listeners.delete(type); },
  };
  const document = {
    documentElement: {classList: {
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
      toggle: (name, enabled) => enabled ? classes.add(name) : classes.delete(name),
    }},
    querySelector: (selector) => selector === '.glass-panel' ? panel : null,
  };
  const window = {location: {search}};
  const bridgeListeners = new Map();
  if (withBridge) window.chrome = {webview: {
    postMessage: (message) => messages.push(message),
    addEventListener: (type, listener) => bridgeListeners.set(type, listener),
    removeEventListener: (type) => bridgeListeners.delete(type),
  }};
  class ResizeObserver {
    constructor(callback) { observerCallback = callback; }
    observe() {}
    disconnect() { disconnected = true; }
  }
  return {
    classes, messages, listeners, bridgeListeners, panel, document, window, ResizeObserver,
    resize: () => observerCallback(),
    disconnected: () => disconnected,
  };
}

test('normal browser mode does not alter or message the page', () => {
  const fake = harness('');
  const stop = startDesktopMode(fake);
  assert.equal(typeof stop, 'function');
  assert.deepEqual([...fake.classes], []);
  assert.deepEqual(fake.messages, []);
  assert.equal(fake.listeners.size, 0);
});

test('desktop mode marks the page and reports rounded bounded panel size', () => {
  const fake = harness();
  const stop = startDesktopMode(fake);
  assert.equal(fake.classes.has('desktop-mode'), true);
  assert.deepEqual(fake.messages, [{type: 'resize', width: 300, height: 185}]);

  fake.panel.rect = {width: 4000, height: -12};
  fake.resize();
  assert.deepEqual(fake.messages.at(-1), {type: 'resize', width: 800, height: 80});

  stop();
  assert.equal(fake.disconnected(), true);
  assert.equal(fake.listeners.size, 0);
});

test('desktop host can expose and clear movable mode', () => {
  const fake = harness();
  const stop = startDesktopMode(fake);
  const hostMessage = fake.bridgeListeners.get('message');

  hostMessage({data: {type: 'movable', enabled: true}});
  assert.equal(fake.classes.has('desktop-movable'), true);

  hostMessage({data: {type: 'movable', enabled: false}});
  assert.equal(fake.classes.has('desktop-movable'), false);

  stop();
  assert.equal(fake.bridgeListeners.size, 0);
});

test('desktop preview is harmless without the WebView2 bridge', () => {
  const fake = harness('?desktop=1', false);
  assert.doesNotThrow(() => startDesktopMode(fake));
  assert.equal(fake.classes.has('desktop-mode'), true);
  assert.deepEqual(fake.messages, []);
});

test('desktop bridge contains no duplicate dashboard copy or markup', async () => {
  const source = await readFile(new URL('./desktop.js', import.meta.url), 'utf8');
  for (const forbidden of ['处理器', '机械硬盘', '实时网速', '天气', '<section', 'innerHTML']) {
    assert.equal(source.includes(forbidden), false, `desktop.js duplicates ${forbidden}`);
  }
});
