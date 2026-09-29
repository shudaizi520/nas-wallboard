import assert from 'node:assert/strict';
import test from 'node:test';

import {
  createPreviewController,
  layoutPresentation,
  isLayoutDirty,
  reorderWidgets,
  resetWidget,
  toggleDefinition,
  updateWidget,
  validateLayout,
} from './layout.js';

const catalog = [
  {id: 'cpu', integration_type: 'truenas', placement: 'metric', label: '处理器', visibility: 'always', defaults: {}},
  {id: 'pool_capacity', integration_type: 'truenas', placement: 'metric', label: '存储池容量', visibility: 'always', defaults: {warning: 80, critical: 92}, fields: [{key: 'warning', kind: 'integer', label: '警告阈值'}]},
  {id: 'plex', integration_type: 'plex', placement: 'activity', label: 'Plex 播放', visibility: 'non_empty', defaults: {limit: 3}, fields: [{key: 'limit', kind: 'integer', label: '最多显示'}]},
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

test('layout presentation separates enabled rows, advanced controls, grouped additions, and dirty actions', () => {
  const saved = layout();
  const clean = layoutPresentation(saved, saved, catalog, sources);
  assert.deepEqual(clean.visible.map((row) => row.id), ['cpu-1', 'plex-1']);
  assert.deepEqual(clean.visible[0].controls, []);
  assert.deepEqual(clean.visible[1].controls.map((control) => control.key), ['limit']);
  assert.deepEqual(clean.visible.map((row) => row.controlLayout), ['none', 'single']);
  assert.deepEqual(clean.availableGroups.map((group) => [group.source, group.items.map((item) => item.id)]), [['truenas', ['pool_capacity']]]);
  assert.equal(clean.actionBarHidden, true);
  assert.equal(layoutPresentation(saved, {...saved, width: 500}, catalog, sources).actionBarHidden, false);
});

test('exact desktop preview is detached while hidden and refreshed when visible', () => {
  const iframe = {src: '', style: {}, parentElement: {clientWidth: 388, style: {}}};
  let visible = true;
  let listener = () => {};
  const environment = {
    isVisible: () => visible,
    onVisibilityChange: (callback) => { listener = callback; return () => { listener = () => {}; }; },
    onPanelSize: () => () => {},
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

test('preview stage follows reported panel height with bounded padding', () => {
  const stage = {clientWidth: 388, style: {}};
  const iframe = {src: '', style: {}, parentElement: stage};
  let reportSize = () => {};
  const environment = {
    isVisible: () => true,
    onVisibilityChange: () => () => {},
    onPanelSize: (_iframe, callback) => { reportSize = callback; return () => { reportSize = () => {}; }; },
  };
  const preview = createPreviewController(iframe, environment);
  preview.start();
  reportSize({width: 360, height: 244});
  assert.equal(iframe.style.height, '244px');
  assert.equal(stage.style.height, '280px');
  reportSize({width: 360, height: 420});
  assert.equal(stage.style.height, '456px');
  preview.stop();
});
