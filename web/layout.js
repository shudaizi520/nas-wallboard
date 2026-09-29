function copy(value) {
  return structuredClone(value);
}

function normalizeOrders(layout) {
  layout.widgets.forEach((item, index) => { item.order = index; });
  return layout;
}

export function reorderWidgets(layout, instanceID, targetIndex) {
  const next = copy(layout);
  const from = next.widgets.findIndex((item) => item.id === instanceID);
  if (from < 0) return next;
  const [item] = next.widgets.splice(from, 1);
  const target = Math.max(0, Math.min(Number(targetIndex), next.widgets.length));
  next.widgets.splice(target, 0, item);
  return normalizeOrders(next);
}

function nextInstanceID(definitionID, widgets) {
  const used = new Set(widgets.map((item) => item.id));
  let sequence = 1;
  while (used.has(`${definitionID}-${sequence}`)) sequence += 1;
  return `${definitionID}-${sequence}`;
}

export function toggleDefinition(layout, definitionID, enabled, catalog, sources) {
  const next = copy(layout);
  const current = next.widgets.find((item) => item.definition_id === definitionID);
  if (current) {
    current.enabled = Boolean(enabled);
    return next;
  }
  if (!enabled) return next;
  const definition = catalog.find((item) => item.id === definitionID);
  if (!definition) return next;
  const source = sources.find((item) => item.type === definition.integration_type);
  next.widgets.push({
    id: nextInstanceID(definitionID, next.widgets),
    definition_id: definitionID,
    integration_id: source?.id ?? '',
    enabled: true,
    order: next.widgets.length,
    config: copy(definition.defaults ?? {}),
  });
  return next;
}

export function updateWidget(layout, instanceID, change) {
  const next = copy(layout);
  const item = next.widgets.find((candidate) => candidate.id === instanceID);
  if (!item) return next;
  if (Object.hasOwn(change, 'integration_id')) item.integration_id = change.integration_id;
  if (Object.hasOwn(change, 'enabled')) item.enabled = Boolean(change.enabled);
  if (change.config) item.config = {...item.config, ...copy(change.config)};
  return next;
}

export function resetWidget(layout, instanceID, catalog) {
  const next = copy(layout);
  const item = next.widgets.find((candidate) => candidate.id === instanceID);
  const definition = item && catalog.find((candidate) => candidate.id === item.definition_id);
  if (item && definition) item.config = copy(definition.defaults ?? {});
  return next;
}

export function validateLayout(layout, catalog, sources) {
  if (!Number.isInteger(layout.width) || layout.width < 300 || layout.width > 720) {
    return ['面板宽度必须在 300px 到 720px 之间'];
  }
  const definitions = new Map(catalog.map((item) => [item.id, item]));
  const sourceTypes = new Map(sources.map((item) => [item.id, item.type]));
  const errors = [];
  const ids = new Set();
  layout.widgets.forEach((item, index) => {
    const definition = definitions.get(item.definition_id);
    if (!definition) errors.push(`组件 ${item.id} 已不可用`);
    if (ids.has(item.id)) errors.push(`组件编号 ${item.id} 重复`);
    ids.add(item.id);
    if (item.order !== index) errors.push('组件顺序无效');
    if (item.enabled && definition?.integration_type && sourceTypes.get(item.integration_id) !== definition.integration_type) {
      errors.push(`${definition.label} 没有可用的数据来源`);
    }
  });
  return [...new Set(errors)];
}

export function isLayoutDirty(saved, current) {
  return JSON.stringify(saved) !== JSON.stringify(current);
}

export function layoutPresentation(saved, current, catalog, sources) {
  const definitions = new Map(catalog.map((item) => [item.id, item]));
  const instances = new Map(current.widgets.map((item) => [item.definition_id, item]));
  const visible = current.widgets.filter((item) => item.enabled).map((item) => {
    const definition = definitions.get(item.definition_id);
    const matchingSources = sources.filter((source) => source.type === definition?.integration_type);
    const controls = [];
    if (definition?.integration_type && matchingSources.length > 1) controls.push({key: 'integration_id', kind: 'source', label: '数据来源'});
    for (const field of definition?.fields ?? []) controls.push({key: field.key, kind: field.kind, label: field.label});
    const controlLayout = controls.length === 0 ? 'none' : controls.length === 1 ? 'single' : 'grid';
    return {id: item.id, definition, item, controls, controlLayout};
  });
  const grouped = new Map();
  for (const definition of catalog) {
    if (instances.get(definition.id)?.enabled) continue;
    const source = definition.integration_type || 'general';
    if (!grouped.has(source)) grouped.set(source, []);
    grouped.get(source).push(definition);
  }
  return {
    visible,
    availableGroups: [...grouped].map(([source, items]) => ({source, items})),
    actionBarHidden: !isLayoutDirty(saved, current),
  };
}

function browserPreviewEnvironment() {
  return {
    isVisible: () => document.visibilityState !== 'hidden',
    onVisibilityChange(callback) {
      const listener = () => callback(document.visibilityState !== 'hidden');
      document.addEventListener('visibilitychange', listener);
      return () => document.removeEventListener('visibilitychange', listener);
    },
    onPanelSize(iframe, callback) {
      let observer;
      const measure = () => {
        const panel = iframe.contentDocument?.querySelector('.glass-panel');
        if (!panel) return;
        const report = () => {
          const bounds = panel.getBoundingClientRect();
          callback({width: bounds.width, height: bounds.height});
        };
        observer?.disconnect();
        observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(report);
        observer?.observe(panel);
        report();
      };
      iframe.addEventListener('load', measure);
      return () => { iframe.removeEventListener('load', measure); observer?.disconnect(); };
    },
  };
}

export function createPreviewController(iframe, environment = browserPreviewEnvironment()) {
  let revision = 0;
  let stopped = true;
  let unsubscribe = () => {};
  let unsubscribeSize = () => {};
  let viewportWidth = 0;
  let panelSize = null;
  const fit = () => {
    if (!panelSize) return;
    const stage = iframe.parentElement;
    const width = viewportWidth || panelSize.width;
    const scale = Math.min(1, Math.max(0.45, ((stage.clientWidth || width) - 28) / width));
    iframe.style.width = `${width}px`;
    iframe.style.height = `${Math.ceil(panelSize.height)}px`;
    iframe.style.transform = `translateX(-50%) scale(${scale})`;
    stage.style.height = `${Math.ceil(panelSize.height * scale + 36)}px`;
  };
  const show = () => { revision += 1; iframe.src = `/?desktop=1&preview=${revision}`; };
  const visibility = (visible) => {
    if (stopped) return;
    if (visible) show();
    else iframe.src = 'about:blank';
  };
  return {
    start() {
      if (!stopped) return;
      stopped = false;
      unsubscribe = environment.onVisibilityChange(visibility);
      unsubscribeSize = environment.onPanelSize?.(iframe, (size) => { panelSize = size; fit(); }) ?? (() => {});
      visibility(environment.isVisible());
    },
    refresh() { if (!stopped && environment.isVisible()) show(); },
    setViewportWidth(value) { viewportWidth = Number(value) || 0; fit(); },
    stop() { stopped = true; unsubscribe(); unsubscribeSize(); iframe.src = 'about:blank'; },
  };
}

function fieldInput(documentRef, field, value, onChange) {
  const label = documentRef.createElement('label');
  label.className = 'widget-field';
  const title = documentRef.createElement('span');
  title.textContent = field.label;
  const input = field.kind === 'select' ? documentRef.createElement('select') : documentRef.createElement('input');
  input.name = field.key;
  if (field.kind === 'select') {
    for (const option of field.options ?? []) {
      const element = documentRef.createElement('option');
      element.value = option.value; element.textContent = option.label;
      input.append(element);
    }
  } else if (field.kind === 'integer') {
    input.type = 'number';
    if (field.minimum !== undefined) input.min = field.minimum;
    if (field.maximum !== undefined) input.max = field.maximum;
  } else {
    input.type = 'text';
  }
  input.value = value ?? field.default ?? '';
  input.addEventListener('input', () => onChange(field.kind === 'integer' ? Number(input.value) : input.value));
  label.append(title, input);
  return label;
}

export function createLayoutEditor(root, api, options = {}) {
  const documentRef = options.document ?? document;
  const width = root.querySelector('#width');
  const widthValue = root.querySelector('#width-value');
  const list = root.querySelector('#layout-items');
  const library = root.querySelector('#widget-library');
  const librarySection = root.querySelector('#available-content-section');
  const status = root.querySelector('#status');
  const save = root.querySelector('#save');
  const reset = root.querySelector('#reset-layout');
  const actions = root.querySelector('#layout-actions');
  const preview = createPreviewController(root.querySelector('#layout-preview'), options.previewEnvironment);
  let catalog = [];
  let sources = [];
  let saved = {width: 360, widgets: []};
  let current = copy(saved);
  let dragged = '';

  const setStatus = (message, tone = '') => { status.textContent = message; status.dataset.tone = tone; };
  const changed = () => {
    const dirty = isLayoutDirty(saved, current);
    actions.hidden = !dirty;
    setStatus(dirty ? '有未保存的更改' : '', 'pending');
  };

  const render = () => {
    width.min = 300; width.max = 720; width.value = current.width; widthValue.value = `${current.width}px`;
    preview.setViewportWidth(current.width);
    const presentation = layoutPresentation(saved, current, catalog, sources);
    actions.hidden = presentation.actionBarHidden;
    list.replaceChildren();
    for (const rowData of presentation.visible) {
      const {item, definition} = rowData;
      const row = documentRef.createElement('article');
      row.className = 'layout-item'; row.dataset.id = item.id; row.dataset.controls = rowData.controlLayout; row.draggable = true; row.tabIndex = 0;
      const main = documentRef.createElement('div'); main.className = 'layout-item-main';
      const handle = documentRef.createElement('span'); handle.className = 'drag-handle'; handle.textContent = '⋮⋮'; handle.title = '拖动排序';
      const checkbox = documentRef.createElement('input'); checkbox.type = 'checkbox'; checkbox.checked = item.enabled; checkbox.setAttribute('aria-label', `显示${definition.label}`);
      checkbox.addEventListener('change', () => { current = updateWidget(current, item.id, {enabled: checkbox.checked}); changed(); render(); });
      const copyBlock = documentRef.createElement('div'); copyBlock.className = 'layout-item-copy';
      const heading = documentRef.createElement('strong'); heading.textContent = definition.label;
      copyBlock.append(heading);
      const moves = documentRef.createElement('div'); moves.className = 'moves';
      for (const [symbol, delta, title] of [['↑', -1, '上移'], ['↓', 1, '下移']]) {
        const button = documentRef.createElement('button'); button.type = 'button'; button.textContent = symbol; button.title = title;
        button.addEventListener('click', () => { current = reorderWidgets(current, item.id, item.order + delta); changed(); render(); });
        moves.append(button);
      }
      main.append(handle, checkbox, copyBlock, moves); row.append(main);
      const controls = documentRef.createElement('div'); controls.className = 'widget-controls';
      const availableSources = sources.filter((source) => source.type === definition.integration_type);
      if (definition.integration_type && availableSources.length > 1) {
        const sourceLabel = documentRef.createElement('label'); sourceLabel.className = 'widget-field';
        const sourceTitle = documentRef.createElement('span'); sourceTitle.textContent = '数据来源';
        const select = documentRef.createElement('select'); select.setAttribute('aria-label', `${definition.label}数据来源`);
        for (const source of availableSources) { const option = documentRef.createElement('option'); option.value = source.id; option.textContent = source.id; select.append(option); }
        select.value = item.integration_id;
        select.addEventListener('change', () => { current = updateWidget(current, item.id, {integration_id: select.value}); changed(); });
        sourceLabel.append(sourceTitle, select); controls.append(sourceLabel);
      }
      for (const field of definition.fields ?? []) {
        controls.append(fieldInput(documentRef, field, item.config?.[field.key], (value) => { current = updateWidget(current, item.id, {config: {[field.key]: value}}); changed(); }));
      }
      if (controls.childElementCount) row.append(controls);
      row.addEventListener('dragstart', () => { dragged = item.id; row.classList.add('dragging'); });
      row.addEventListener('dragend', () => { dragged = ''; row.classList.remove('dragging'); });
      row.addEventListener('dragover', (event) => event.preventDefault());
      row.addEventListener('drop', (event) => { event.preventDefault(); if (dragged) { current = reorderWidgets(current, dragged, item.order); changed(); render(); } });
      row.addEventListener('keydown', (event) => {
        if (!event.altKey || !['ArrowUp', 'ArrowDown'].includes(event.key)) return;
        event.preventDefault(); current = reorderWidgets(current, item.id, item.order + (event.key === 'ArrowUp' ? -1 : 1)); changed(); render();
        list.querySelector(`[data-id="${item.id}"]`)?.focus();
      });
      list.append(row);
    }
    library.replaceChildren();
    for (const group of presentation.availableGroups) {
      const section = documentRef.createElement('section'); section.className = 'widget-library-group'; section.dataset.source = group.source;
      const heading = documentRef.createElement('h3'); heading.textContent = group.source === 'truenas' ? 'TrueNAS' : group.source === 'general' ? '通用' : group.source;
      const items = documentRef.createElement('div'); items.className = 'widget-library-items';
      for (const definition of group.items) {
        const button = documentRef.createElement('button'); button.type = 'button'; button.className = 'widget-library-item';
        button.textContent = `＋ ${definition.label}`;
        button.addEventListener('click', () => { current = toggleDefinition(current, definition.id, true, catalog, sources); changed(); render(); });
        items.append(button);
      }
      section.append(heading, items); library.append(section);
    }
    librarySection.hidden = !library.childElementCount;
  };

  width.addEventListener('input', () => { current.width = Number(width.value); widthValue.value = `${current.width}px`; preview.setViewportWidth(current.width); changed(); });
  reset.addEventListener('click', () => { current.widgets.forEach((item) => { current = resetWidget(current, item.id, catalog); }); changed(); render(); });
  save.addEventListener('click', async () => {
    const errors = validateLayout(current, catalog, sources);
    if (errors.length) { setStatus(errors[0], 'error'); return; }
    save.disabled = true; setStatus('正在保存…');
    try {
      const data = await api.saveLayout(current);
      catalog = data.catalog; sources = data.sources; saved = copy(data.layout); current = copy(saved);
      setStatus('已保存', 'success'); render(); preview.refresh();
    } catch (error) { setStatus(error.message === 'invalid_layout' ? '布局设置无效，请检查数据来源' : '保存失败，请稍后重试', 'error'); }
    finally { save.disabled = false; }
  });

  return {
    async load() {
      const data = await api.layout();
      catalog = data.catalog; sources = data.sources; saved = copy(data.layout); current = copy(saved);
      render();
      if (!root.hidden) preview.start();
    },
    setActive(active) { if (active) preview.start(); else preview.stop(); },
    stop() { preview.stop(); },
    value() { return copy(current); },
  };
}
