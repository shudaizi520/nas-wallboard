import {managementAddress} from './versions.js';

const LAST_STEP = 5;

export function initialSetupState(status = {}) {
  return {
    step: 0,
    compatibility: false,
    migration: Boolean(status.migration),
    candidate: {url: '', username: '', apiKey: '', insecureSkipVerify: false},
    administratorUsername: 'admin',
    password: '',
    passwordConfirmation: '',
    probe: null,
    errors: {},
    pending: false,
  };
}

export function reduceSetup(state, action) {
  switch (action.type) {
    case 'acknowledge':
      return {...state, compatibility: Boolean(action.value), errors: {}};
    case 'field':
      return {...state, candidate: {...state.candidate, [action.name]: action.value}, errors: {...state.errors, [action.name]: ''}};
    case 'password':
      return {...state, [action.name]: action.value, errors: {...state.errors, [action.name]: ''}};
    case 'administratorUsername':
      return {...state, administratorUsername: action.value, errors: {...state.errors, administratorUsername: ''}};
    case 'next':
      return {...state, step: Math.min(LAST_STEP, state.step + 1), errors: {}};
    case 'back':
      return {...state, step: Math.max(0, state.step - 1), errors: {}};
    case 'probeStarted':
      return {...state, pending: true, errors: {}};
    case 'probeSucceeded':
      return {...state, pending: false, probe: action.result, errors: {}};
    case 'probeFailed':
      return {...state, pending: false, probe: null, errors: {general: action.message || '连接测试失败'}};
    case 'serverErrors':
      return {...state, pending: false, errors: {...action.errors}};
    default:
      return state;
  }
}

export function normalizeTrueNASURL(input) {
  let value = String(input ?? '').trim();
  if (!value.includes('://')) value = `https://${value}`;
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error('unsafe URL');
  if (parsed.protocol === 'https:') parsed.protocol = 'wss:';
  else if (parsed.protocol === 'http:') parsed.protocol = 'ws:';
  else if (parsed.protocol !== 'ws:' && parsed.protocol !== 'wss:') throw new Error('unsupported protocol');
  if (!parsed.hostname) throw new Error('missing host');
  parsed.pathname = parsed.pathname === '/' || parsed.pathname === '' ? '/api/current' : parsed.pathname.replace(/\/$/, '');
  if (parsed.pathname !== '/api/current') throw new Error('unsupported path');
  return parsed.toString().replace(/\/$/, '');
}

export function validateCurrentStep(state) {
  const errors = {};
  if (state.step === 0 && !state.compatibility) errors.compatibility = '请先确认 TrueNAS SCALE 版本';
  if (state.step === 1 && !state.migration) {
    try {
      normalizeTrueNASURL(state.candidate.url);
    } catch {
      errors.url = '请输入有效的 TrueNAS 地址';
    }
    if (!state.candidate.username.trim()) errors.username = '请输入专用账户名';
    if (!state.candidate.apiKey) errors.apiKey = '请输入 API Key';
  }
  if (state.step === 2 && !state.probe?.ok) errors.general = '请先完成连接测试';
  if (state.step === 3) {
    if (!/^[\p{L}\p{N}._-]{3,32}$/u.test(state.administratorUsername.trim())) errors.administratorUsername = '用户名只能包含中英文字母、数字、点、下划线和连字符';
    if ([...state.password].length < 12) errors.password = '密码至少需要 12 个字符';
    if (state.password !== state.passwordConfirmation) errors.passwordConfirmation = '两次输入的密码不一致';
  }
  return errors;
}

export function buildProbePayload(state) {
  if (state.migration) return {use_imported: true};
  return {
    url: normalizeTrueNASURL(state.candidate.url),
    username: state.candidate.username.trim(),
    api_key: state.candidate.apiKey,
    insecure_skip_verify: Boolean(state.candidate.insecureSkipVerify),
  };
}

export function buildCompletionPayload(state) {
  const candidate = state.migration ? {} : buildProbePayload(state);
  return {
    ...candidate,
    administrator_username: state.administratorUsername.trim(),
    password: state.password,
    password_confirmation: state.passwordConfirmation,
    use_imported: state.migration,
  };
}

export function redactSecretsAfterSubmit(state) {
  return {
    ...state,
    candidate: {...state.candidate, apiKey: ''},
    password: '',
    passwordConfirmation: '',
    pending: true,
  };
}

function messageForServerError(code) {
  const messages = {
    invalid_candidate: 'TrueNAS 连接信息不完整',
    invalid_username: '管理员用户名格式不正确',
    password_confirmation: '两次输入的密码不一致',
    password_policy: '管理员密码至少需要 12 个字符',
    probe_failed: '无法连接 TrueNAS，请检查地址、证书和 API Key',
    rate_limited: '操作太频繁，请稍后再试',
    save_failed: '设置没有保存，请重试',
  };
  return messages[code] || '操作失败，请检查后重试';
}

function setupApplication(root, browser) {
  let state = initialSetupState();
  const panels = [...root.querySelectorAll('[data-step]')];
  const progress = [...root.querySelectorAll('[data-progress-step]')];
  const back = root.querySelector('#back');
  const next = root.querySelector('#next');
  const probeButton = root.querySelector('#probe');
  const form = root.querySelector('#setup-form');
  const status = root.querySelector('#form-status');
  const secretInputs = [root.querySelector('#api-key'), root.querySelector('#password'), root.querySelector('#password-confirmation')];

  const dispatch = (action) => {
    state = reduceSetup(state, action);
    render();
  };

  function showErrors(errors) {
    root.querySelectorAll('[data-error]').forEach((node) => { node.textContent = errors[node.dataset.error] || ''; });
    status.textContent = errors.general || '';
  }

  function renderDiscovery() {
    const discovery = root.querySelector('#discovery');
    if (!state.probe) {
      discovery.replaceChildren();
      return;
    }
    const groups = [['存储池', state.probe.pools], ['硬盘', state.probe.disks], ['网络接口', state.probe.interfaces], ['应用', state.probe.apps]];
    discovery.replaceChildren(...groups.map(([label, items]) => {
      const card = document.createElement('article');
      const title = document.createElement('h3');
      const count = document.createElement('strong');
      title.textContent = label;
      count.textContent = String(items?.length ?? 0);
      card.append(title, count);
      return card;
    }));
  }

  function renderPermissions() {
    const list = root.querySelector('#permission-list');
    const permissions = state.probe?.permissions ?? [];
    list.replaceChildren(...permissions.map((permission) => {
      const item = document.createElement('span');
      item.dataset.ok = String(Boolean(permission.ok));
      item.textContent = `${permission.ok ? '✓' : '×'} ${permission.feature}`;
      return item;
    }));
  }

  function renderReview() {
    root.querySelector('#review-address').textContent = state.migration ? '使用已导入的 TrueNAS 连接' : normalizeTrueNASURL(state.candidate.url);
    root.querySelector('#review-version').textContent = state.probe?.version || '—';
    root.querySelector('#review-resources').textContent = `${state.probe?.pools?.length ?? 0} 个存储池 · ${state.probe?.disks?.length ?? 0} 块硬盘`;
    root.querySelector('#review-administrator').textContent = state.administratorUsername.trim();
  }

  function render() {
    panels.forEach((panel) => { panel.hidden = Number(panel.dataset.step) !== state.step; });
    progress.forEach((item) => {
      const index = Number(item.dataset.progressStep);
      item.dataset.state = index < state.step ? 'done' : index === state.step ? 'current' : 'upcoming';
      item.setAttribute('aria-current', index === state.step ? 'step' : 'false');
    });
    back.hidden = state.step === 0;
    next.hidden = state.step === 2 && !state.probe?.ok;
    next.textContent = state.step === LAST_STEP ? '完成设置' : '继续';
    next.disabled = state.pending;
    probeButton.disabled = state.pending;
    root.querySelector('#http-warning').hidden = browser.location.protocol === 'https:' || state.migration;
    root.querySelector('#fresh-connection').hidden = state.migration;
    root.querySelector('#imported-connection').hidden = !state.migration;
    root.querySelector('#detected-version').textContent = state.probe?.version ? `已连接 TrueNAS ${state.probe.version}` : '';
    if (state.step === 2) renderPermissions();
    showErrors(state.errors);
    if (state.step === 4) renderDiscovery();
    if (state.step === 5) renderReview();
  }

  root.querySelector('#compatibility').addEventListener('change', (event) => dispatch({type: 'acknowledge', value: event.target.checked}));
  for (const field of ['url', 'username', 'apiKey']) {
    root.querySelector(`#${field.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`).addEventListener('input', (event) => dispatch({type: 'field', name: field, value: event.target.value}));
  }
  root.querySelector('#insecure-skip-verify').addEventListener('change', (event) => dispatch({type: 'field', name: 'insecureSkipVerify', value: event.target.checked}));
  root.querySelector('#administrator-username').addEventListener('input', (event) => dispatch({type: 'administratorUsername', value: event.target.value}));
  root.querySelector('#password').addEventListener('input', (event) => dispatch({type: 'password', name: 'password', value: event.target.value}));
  root.querySelector('#password-confirmation').addEventListener('input', (event) => dispatch({type: 'password', name: 'passwordConfirmation', value: event.target.value}));

  back.addEventListener('click', () => dispatch({type: 'back'}));
  next.addEventListener('click', async () => {
    const errors = validateCurrentStep(state);
    if (Object.keys(errors).length) {
      dispatch({type: 'serverErrors', errors});
      return;
    }
    if (state.step === 1 && !state.migration) {
      dispatch({type: 'field', name: 'url', value: normalizeTrueNASURL(state.candidate.url)});
      root.querySelector('#url').value = normalizeTrueNASURL(state.candidate.url);
    }
    if (state.step < LAST_STEP) {
      dispatch({type: 'next'});
      return;
    }
    const payload = buildCompletionPayload(state);
    state = redactSecretsAfterSubmit(state);
    secretInputs.forEach((input) => { input.value = ''; });
    render();
    try {
      const response = await fetch('/api/setup/complete', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(result.error || 'failed');
      browser.location.assign(managementAddress('/login', browser.location.search));
    } catch (error) {
      state = {...state, pending: false, errors: {general: messageForServerError(error.message)}};
      render();
    }
  });

  probeButton.addEventListener('click', async () => {
    dispatch({type: 'probeStarted'});
    try {
      const response = await fetch('/api/setup/probe', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(buildProbePayload(state)),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(result.error || 'probe_failed');
      dispatch({type: 'probeSucceeded', result});
    } catch (error) {
      dispatch({type: 'probeFailed', message: messageForServerError(error.message)});
    }
  });

  form.addEventListener('submit', (event) => event.preventDefault());
  fetch('/api/setup/status', {cache: 'no-store', headers: {Accept: 'application/json'}})
    .then((response) => response.ok ? response.json() : Promise.reject(new Error('status')))
    .then((setupStatus) => {
      if (!setupStatus.setup_required) {
        browser.location.replace(managementAddress('/manage', browser.location.search));
        return;
      }
      state = initialSetupState(setupStatus);
      render();
    })
    .catch(() => {
      state = {...state, errors: {general: '无法读取设置状态，请刷新页面'}};
      render();
    });
  render();
}

if (typeof document !== 'undefined' && typeof window !== 'undefined') setupApplication(document, window);
