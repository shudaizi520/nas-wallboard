import {createSettingsSection} from './manage-ui.js';

export const MASKED_SECRET = '********';

export function entityOptions(entities, kind, current) {
  const options = entities.filter((item) => item.kind === kind);
  if (current && !options.some((item) => item.id === current)) options.unshift({id:current,name:'当前/手动实体',kind});
  return options;
}

export function orderedCatalog(catalog = []) {
  return [...catalog].sort((a, b) => a.metadata.category.localeCompare(b.metadata.category, 'zh-CN') || a.metadata.name.localeCompare(b.metadata.name, 'zh-CN') || a.id.localeCompare(b.id));
}

export function partitionFields(fields = []) {
  return fields.reduce((groups, field) => {
    groups[field.advanced ? 'advanced' : 'common'].push(field);
    return groups;
  }, {common: [], advanced: []});
}

function integrationEndpoint(definition, instance) {
  if (!instance) return '未配置';
  const urlField = definition.fields?.find((field) => field.kind === 'url');
  return (urlField && instance.config?.[urlField.key]) || '已配置';
}

export function integrationRows(state = {}) {
  const instances = new Map((state.instances ?? []).map((item) => [item.type, item]));
  return orderedCatalog(state.catalog).map((definition) => {
    const instance = instances.get(definition.id);
    const health = (state.health ?? []).find((item) => item.instance_id === instance?.id);
    const required = Boolean(definition.metadata.required);
    const actions = ['configure'];
    if (instance && !required) actions.push(instance.enabled ? 'disable' : 'enable', 'remove');
    const tone = !instance || !instance.enabled ? 'neutral' : health?.healthy === false ? 'bad' : 'good';
    return {
      id: definition.id,
      name: definition.metadata.name,
      configured: integrationEndpoint(definition, instance),
      error: instance?.enabled && health?.healthy === false ? (health.message || '连接异常') : '',
      enabled: !instance ? '未启用' : instance.enabled ? '已启用' : '已停用',
      tone,
      actions,
    };
  });
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
  return {candidate: {type: definition.id, config, secrets, ...(instance?.id ? {instance_id: instance.id} : {})}, errors};
}

const pendingRows = new WeakSet();

export async function runIntegrationRowAction({buttons, status, task, reload}) {
  if (pendingRows.has(status)) return false;
  pendingRows.add(status);
  for (const button of buttons) button.disabled = true;
  status.textContent = '正在处理…';
  status.dataset.tone = 'neutral';
  try {
    await task();
    await reload();
    return true;
  } catch {
    status.textContent = '操作失败，请重试';
    status.dataset.tone = 'bad';
    return false;
  } finally {
    pendingRows.delete(status);
    for (const button of buttons) button.disabled = false;
  }
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
  const help = document.createElement('small'); help.className = 'integration-help'; help.textContent = field.help || '';
  const error = document.createElement('span'); error.className = 'field-error'; error.dataset.error = field.key;
  wrapper.append(title, control, help, error);
  return wrapper;
}

export function createIntegrationCenter(root, api, options = {}) {
  let state = {catalog: [], instances: [], health: []};
  let controller = null;
  const dialog = document.querySelector('#integration-dialog');
  const form = dialog.querySelector('form');
  const render = () => {
    const instances = new Map(state.instances.map((item) => [item.type, item]));
    const definitions = new Map(state.catalog.map((item) => [item.id, item]));
    root.replaceChildren(...integrationRows(state).map((row) => {
      const definition = definitions.get(row.id);
      const instance = instances.get(definition.id);
      const main = document.createElement('div'); main.className = 'integration-status';
      const endpoint = document.createElement('strong'); endpoint.className = 'integration-endpoint'; endpoint.textContent = row.configured;
      main.append(endpoint);
      if (row.error) {
        const detail = document.createElement('p'); detail.className = 'exception-message'; detail.textContent = row.error;
        main.append(detail);
      }
      const actionStatus = document.createElement('p'); actionStatus.className = 'integration-action-status'; actionStatus.setAttribute('role', 'status'); actionStatus.setAttribute('aria-live', 'polite');
      main.append(actionStatus);
      const aside = document.createElement('div'); aside.className = 'integration-secondary';
      const stateLabel = document.createElement('strong'); stateLabel.textContent = row.enabled; stateLabel.dataset.tone = row.tone;
      const actions = document.createElement('div'); actions.className = 'section-actions';
      for (const action of row.actions) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = action === 'configure' ? 'secondary-button' : '';
        button.textContent = action === 'configure' ? (instance ? '设置' : '添加') : action === 'disable' ? '停用' : action === 'enable' ? '启用' : '删除';
        if (action === 'configure') button.addEventListener('click', () => open(definition, instance));
        if (action === 'disable' || action === 'enable') button.addEventListener('click', () => {
          void runIntegrationRowAction({buttons: [...actions.querySelectorAll('button')], status: actionStatus, task: async () => { await api.action(instance.id, action); await options.onChange?.(); }, reload: load});
        });
        if (action === 'remove') button.addEventListener('click', () => {
          if (!window.confirm(`确定删除“${definition.metadata.name}”吗？`)) return;
          void runIntegrationRowAction({buttons: [...actions.querySelectorAll('button')], status: actionStatus, task: async () => { await api.remove(instance.id); await options.onChange?.(); }, reload: load});
        });
        actions.append(button);
      }
      aside.append(stateLabel, actions);
      const section = createSettingsSection(document, {className: 'integration-row', title: row.name, description: row.description, main, aside});
      section.dataset.integration = row.id;
      return section;
    }));
  };
  const load = async () => { state = await api.integrations(); render(); };
  const close = () => { controller?.abort(); controller = null; dialog.close(); form.replaceChildren(); };
  const open = (definition, instance) => {
    controller?.abort(); controller = new AbortController();
    form.replaceChildren();
    const heading = document.createElement('h2'); heading.textContent = definition.metadata.name;
    const description = document.createElement('p'); description.className = 'integration-description'; description.textContent = definition.metadata.description;
    const fields = partitionFields(definition.fields);
    form.append(heading, description, ...fields.common.map((field) => controlFor(field, instance)));
    if (fields.advanced.length) {
      const advanced = document.createElement('details'); advanced.className = 'integration-advanced';
      const summary = document.createElement('summary'); summary.textContent = '高级设置';
      const body = document.createElement('div'); body.className = 'integration-advanced-fields';
      body.append(...fields.advanced.map((field) => controlFor(field, instance)));
      advanced.append(summary, body); form.append(advanced);
    }
    const result = document.createElement('p'); result.className = 'probe-result'; result.setAttribute('role', 'status');
    const buttons = document.createElement('div'); buttons.className = 'dialog-actions';
    const cancel = document.createElement('button'); cancel.type = 'button'; cancel.textContent = '取消'; cancel.addEventListener('click', close);
    const probe = document.createElement('button'); probe.type = 'button'; probe.textContent = '测试连接';
    const save = document.createElement('button'); save.type = 'submit'; save.className = 'primary'; save.textContent = '保存';
    buttons.append(cancel, probe, save); form.append(result, buttons);
    if (definition.id === 'home_assistant') {
      const discover = document.createElement('button'); discover.type = 'button'; discover.textContent = '读取风扇与功耗设备';
      buttons.insertBefore(discover, probe);
      const signal = controller.signal;
      discover.addEventListener('click', async () => {
        const {candidate, errors} = candidateFromForm(definition, form, instance);
        delete errors.entity_id; delete errors.power_entity_id;
        if (Object.keys(errors).length) { result.textContent = Object.values(errors)[0]; return; }
        discover.disabled = true; probe.disabled = true; save.disabled = true; result.textContent = '正在读取设备…';
        try {
          const {entities} = await api.entities(candidate, instance?.id, signal);
          if (signal.aborted) return;
          for (const [key, kind] of [['entity_id','fan'],['power_entity_id','power']]) {
            const control = form.elements.namedItem(key); const id = `ha-options-${key}`;
            form.querySelector(`#${id}`)?.remove();
            const list = document.createElement('datalist'); list.id = id;
            for (const item of entityOptions(entities, kind, control.value)) {
              const option = document.createElement('option'); option.value = item.id; option.label = item.name; list.append(option);
            }
            control.setAttribute('list', id); form.append(list);
          }
          result.textContent = `已读取 ${entities.length} 个设备，请在实体输入框的下拉列表中选择；也可手动填写。`;
        } catch (error) { if (error.name !== 'AbortError') result.textContent = '设备读取失败，请检查地址和令牌；仍可手动填写实体。'; }
        finally { discover.disabled = false; probe.disabled = false; save.disabled = false; }
      });
    }
    const read = () => { const value = candidateFromForm(definition, form, instance); form.querySelectorAll('[data-error]').forEach((node) => { node.textContent = value.errors[node.dataset.error] ?? ''; }); return value; };
    probe.addEventListener('click', async () => { const {candidate, errors} = read(); if (Object.keys(errors).length) return; probe.disabled = true; result.textContent = '正在测试连接…'; try { const response = await api.probe(candidate, controller.signal); result.textContent = response.message || '连接成功'; } catch (error) { if (error.name !== 'AbortError') result.textContent = error.data?.probe?.message || (error.code === 'probe_failed' ? '连接测试失败' : '无法完成测试'); } finally { probe.disabled = false; } });
    form.onsubmit = async (event) => { event.preventDefault(); const {candidate, errors} = read(); if (Object.keys(errors).length) return; save.disabled = true; try { if (instance) await api.update(instance.id, candidate); else await api.create(candidate); close(); await options.onChange?.(); await load(); } catch (error) { result.textContent = error.data?.probe?.message || (error.code === 'probe_failed' ? '连接测试失败，未保存' : '保存失败，请检查字段'); save.disabled = false; } };
    dialog.showModal();
  };
  dialog.addEventListener('cancel', (event) => { event.preventDefault(); close(); });
  return {load, close};
}
