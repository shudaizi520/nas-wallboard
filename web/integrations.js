export const MASKED_SECRET = '********';

export function orderedCatalog(catalog = []) {
  return [...catalog].sort((a, b) => a.metadata.category.localeCompare(b.metadata.category, 'zh-CN') || a.metadata.name.localeCompare(b.metadata.name, 'zh-CN') || a.id.localeCompare(b.id));
}

export function validateField(field, value) {
  if (field.kind === 'secret' && value === MASKED_SECRET) return '';
  if (field.required && (value === undefined || value === null || String(value).trim() === '')) return `${field.label}不能为空`;
  if (value === undefined || value === null || String(value).trim() === '') return '';
  if (field.kind === 'url') {
    try { const url = new URL(String(value)); if (!['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) || url.username || url.password) throw new Error(); } catch { return `${field.label}格式不正确`; }
  }
  if (field.kind === 'integer') {
    const number = Number(value); if (!Number.isInteger(number) || (field.minimum != null && number < field.minimum) || (field.maximum != null && number > field.maximum)) return `${field.label}必须是有效整数`;
  }
  if (field.kind === 'duration' && !/^\d+(?:\.\d+)?(?:ms|s|m|h)$/.test(String(value))) return `${field.label}格式应类似 15s 或 2h`;
  if (field.kind === 'boolean' && typeof value !== 'boolean') return `${field.label}必须选择开或关`;
  if (field.kind === 'select' && !field.options?.some((option) => option.value === value)) return `${field.label}选项无效`;
  if (field.kind === 'entity_id' && !/^[a-z0-9_]+\.[A-Za-z0-9_./-]+$/.test(String(value))) return `${field.label}格式不正确`;
  return '';
}

export function candidateFromForm(definition, form, instance) {
  const config = {};
  const secrets = {};
  const errors = {};
  for (const field of definition.fields) {
    const control = form.elements.namedItem(field.key);
    let value = field.kind === 'boolean' ? control.checked : control.value;
    if (field.kind === 'integer' && value !== '') value = Number(value);
    const error = validateField(field, value);
    if (error) errors[field.key] = error;
    if (field.kind === 'secret') secrets[field.key] = value || (instance?.secrets?.[field.key] ? MASKED_SECRET : '');
    else if (value !== '') config[field.key] = value;
  }
  return {candidate: {type: definition.id, config, secrets}, errors};
}

function controlFor(field, instance) {
  const wrapper = document.createElement('label');
  wrapper.className = 'integration-field';
  const title = document.createElement('span'); title.textContent = field.label;
  let control;
  if (field.kind === 'select') {
    control = document.createElement('select');
    for (const option of field.options ?? []) { const node = document.createElement('option'); node.value = option.value; node.textContent = option.label; control.append(node); }
  } else {
    control = document.createElement('input');
    control.type = field.kind === 'secret' ? 'password' : field.kind === 'integer' ? 'number' : field.kind === 'boolean' ? 'checkbox' : 'text';
  }
  control.name = field.key;
  if (field.minimum != null) control.min = field.minimum;
  if (field.maximum != null) control.max = field.maximum;
  if (field.placeholder) control.placeholder = field.placeholder;
  const stored = instance?.config?.[field.key] ?? field.default;
  if (field.kind === 'boolean') control.checked = Boolean(stored);
  else if (field.kind === 'secret' && instance?.secrets?.[field.key]) control.value = MASKED_SECRET;
  else if (stored != null) control.value = stored;
  const help = document.createElement('small'); help.textContent = field.help;
  const error = document.createElement('small'); error.className = 'field-error'; error.dataset.error = field.key;
  wrapper.append(title, control, help, error);
  return wrapper;
}

export function createIntegrationCenter(root, api) {
  let state = {catalog: [], instances: [], health: []};
  let controller = null;
  const dialog = document.querySelector('#integration-dialog');
  const form = dialog.querySelector('form');
  const render = () => {
    const instances = new Map(state.instances.map((item) => [item.type, item]));
    root.replaceChildren(...orderedCatalog(state.catalog).map((definition) => {
      const instance = instances.get(definition.id);
      const health = state.health.find((item) => item.instance_id === instance?.id);
      const card = document.createElement('article'); card.className = 'integration-card'; card.dataset.integration = definition.id;
      const badge = instance ? (!instance.enabled ? '已停用' : health?.healthy === false ? '异常' : '已配置') : definition.detected ? '已发现' : '可添加';
      card.innerHTML = `<div class="integration-icon">${definition.metadata.icon.slice(0, 1).toUpperCase()}</div><div class="integration-copy"><div><h3></h3><span class="integration-badge"></span></div><p></p></div>`;
      card.querySelector('h3').textContent = definition.metadata.name;
      card.querySelector('p').textContent = definition.metadata.description;
      card.querySelector('.integration-badge').textContent = badge;
      const actions = document.createElement('div'); actions.className = 'integration-actions';
      const edit = document.createElement('button'); edit.type = 'button'; edit.textContent = instance ? '设置' : '添加'; edit.addEventListener('click', () => open(definition, instance)); actions.append(edit);
      if (instance && !definition.metadata.required) {
        const toggle = document.createElement('button'); toggle.type = 'button'; toggle.textContent = instance.enabled ? '停用' : '启用'; toggle.addEventListener('click', async () => { await api.action(instance.id, instance.enabled ? 'disable' : 'enable'); await load(); }); actions.append(toggle);
        const remove = document.createElement('button'); remove.type = 'button'; remove.textContent = '删除'; remove.addEventListener('click', async () => { if (!window.confirm(`确定删除“${definition.metadata.name}”吗？`)) return; await api.remove(instance.id); await load(); }); actions.append(remove);
      }
      card.append(actions); return card;
    }));
  };
  const load = async () => { state = await api.integrations(); render(); };
  const close = () => { controller?.abort(); controller = null; dialog.close(); form.replaceChildren(); };
  const open = (definition, instance) => {
    controller?.abort(); controller = new AbortController();
    form.replaceChildren();
    const heading = document.createElement('h2'); heading.textContent = definition.metadata.name;
    const intro = document.createElement('p'); intro.className = 'dialog-intro'; intro.textContent = definition.metadata.description;
    form.append(heading, intro, ...definition.fields.map((field) => controlFor(field, instance)));
    const result = document.createElement('p'); result.className = 'probe-result'; result.setAttribute('role', 'status');
    const buttons = document.createElement('div'); buttons.className = 'dialog-actions';
    const cancel = document.createElement('button'); cancel.type = 'button'; cancel.textContent = '取消'; cancel.addEventListener('click', close);
    const probe = document.createElement('button'); probe.type = 'button'; probe.textContent = '测试连接';
    const save = document.createElement('button'); save.type = 'submit'; save.className = 'primary'; save.textContent = '保存';
    buttons.append(cancel, probe, save); form.append(result, buttons);
    const read = () => { const value = candidateFromForm(definition, form, instance); form.querySelectorAll('[data-error]').forEach((node) => { node.textContent = value.errors[node.dataset.error] ?? ''; }); return value; };
    probe.addEventListener('click', async () => { const {candidate, errors} = read(); if (Object.keys(errors).length) return; probe.disabled = true; result.textContent = '正在测试连接…'; try { const response = await api.probe(candidate, controller.signal); result.textContent = response.message || '连接成功'; } catch (error) { if (error.name !== 'AbortError') result.textContent = error.data?.probe?.message || (error.code === 'probe_failed' ? '连接测试失败' : '无法完成测试'); } finally { probe.disabled = false; } });
    form.addEventListener('submit', async (event) => { event.preventDefault(); const {candidate, errors} = read(); if (Object.keys(errors).length) return; save.disabled = true; try { if (instance) await api.update(instance.id, candidate); else await api.create(candidate); close(); await load(); } catch (error) { result.textContent = error.data?.probe?.message || (error.code === 'probe_failed' ? '连接测试失败，未保存' : '保存失败，请检查字段'); save.disabled = false; } });
    dialog.showModal();
  };
  dialog.addEventListener('cancel', (event) => { event.preventDefault(); close(); });
  return {load, close};
}
