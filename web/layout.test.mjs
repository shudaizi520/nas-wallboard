import assert from 'node:assert/strict';
import test from 'node:test';

import {
  createPreviewController,
  isLayoutDirty,
  reorderWidgets,
  resetWidget,
  toggleDefinition,
  updateWidget,
  validateLayout,
} from './layout.js';

const catalog = [
  {id: 'cpu', integration_type: 'truenas', placement: 'metric', label: '处理器', visibility: 'always', defaults: {}},
  {id: 'pool_capacity', integration_type: 'truenas', placement: 'metric', label: '存储池容量', visibility: 'always', defaults: {warning: 80, critical: 92}},
  {id: 'plex', integration_type: 'plex', placement: 'activity', label: 'Plex 播放', visibility: 'non_empty', defaults: {limit: 3}},
];
const sources = [{id: 'truenas-main', type: 'truenas'}, {id: 'plex-main', type: 'plex'}];

function layout() {
  return {width: 360, widgets: [
    {id: 'cpu-1', definition_id: 'cpu', integration_id: 'truenas-main', enabled: true, order: 0, config: {}},
    {id: 'plex-1', definition_id: 'plex', integration_id: 'plex-main', enabled: true, order: 1, config: {limit: 3}},
  ]};
}

test('toggle adds a stable sourced instance and later only changes enabled state', () => {
  const added = toggleDefinition(layout(), 'pool_capacity', true, catalog, sources);
  assert.deepEqual(added.widgets[2], {
    id: 'pool_capacity-1', definition_id: 'pool_capacity', integration_id: 'truenas-main',
    enabled: true, order: 2, config: {warning: 80, critical: 92},
  });
  const disabled = toggleDefinition(added, 'pool_capacity', false, catalog, sources);
  assert.equal(disabled.widgets[2].enabled, false);
  assert.equal(disabled.widgets[2].id, 'pool_capacity-1');
});

test('drag and keyboard reorder use the same immutable ordering operation', () => {
  const original = layout();
  const moved = reorderWidgets(original, 'plex-1', 0);
  assert.deepEqual(moved.widgets.map((item) => [item.id, item.order]), [['plex-1', 0], ['cpu-1', 1]]);
  assert.deepEqual(original.widgets.map((item) => item.id), ['cpu-1', 'plex-1']);
});

test('source, thresholds, activity limits, reset, and width validation are deterministic', () => {
  let changed = toggleDefinition(layout(), 'pool_capacity', true, catalog, sources);
  changed = updateWidget(changed, 'pool_capacity-1', {integration_id: 'truenas-main', config: {warning: 75, critical: 90}});
  changed = updateWidget(changed, 'plex-1', {config: {limit: 5}});
  assert.equal(changed.widgets[2].config.warning, 75);
  assert.equal(changed.widgets[1].config.limit, 5);
  assert.deepEqual(resetWidget(changed, 'pool_capacity-1', catalog).widgets[2].config, {warning: 80, critical: 92});
  assert.deepEqual(validateLayout({...changed, width: 299}, catalog, sources), ['面板宽度必须在 300px 到 720px 之间']);
  assert.deepEqual(validateLayout(changed, catalog, sources), []);
});

test('dirty state detects unsaved changes without depending on object identity', () => {
  const saved = layout();
  assert.equal(isLayoutDirty(saved, structuredClone(saved)), false);
  assert.equal(isLayoutDirty(saved, {...saved, width: 500}), true);
});

test('exact desktop preview is detached while hidden and refreshed when visible', () => {
  const iframe = {src: ''};
  let visible = true;
  let listener = () => {};
  const environment = {
    isVisible: () => visible,
    onVisibilityChange: (callback) => { listener = callback; return () => { listener = () => {}; }; },
  };
  const preview = createPreviewController(iframe, environment);
  preview.start();
  assert.match(iframe.src, /^\/?\?desktop=1&preview=/);
  const first = iframe.src;
  visible = false; listener(false);
  assert.equal(iframe.src, 'about:blank');
  visible = true; listener(true);
  assert.notEqual(iframe.src, first);
  preview.stop();
  assert.equal(iframe.src, 'about:blank');
});
