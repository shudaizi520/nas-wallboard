export function initialLoginState() {
  return {username: '', password: '', pending: false, error: ''};
}

export function reduceLogin(state, action) {
  switch (action.type) {
    case 'username':
      return {...state, username: action.value, error: ''};
    case 'password':
      return {...state, password: action.value, error: ''};
    case 'submit':
      return {...state, password: '', pending: true, error: ''};
    case 'failure':
      return {...state, password: '', pending: false, error: action.message || '登录失败'};
    default:
      return state;
  }
}

export function validateLogin(state) {
  const errors = {};
  if (!state.username.trim()) errors.username = '请输入管理员用户名';
  if (!state.password) errors.password = '请输入管理员密码';
  return errors;
}

export function buildLoginPayload(state) {
  return {username: state.username.trim(), password: state.password};
}

function loginApplication(root, browser) {
  let state = initialLoginState();
  const form = root.querySelector('#login-form');
  const username = root.querySelector('#login-username');
  const password = root.querySelector('#login-password');
  const submit = root.querySelector('#login-submit');
  const status = root.querySelector('#login-status');

  username.addEventListener('input', (event) => {
    state = reduceLogin(state, {type: 'username', value: event.target.value});
    status.textContent = '';
  });
  password.addEventListener('input', (event) => {
    state = reduceLogin(state, {type: 'password', value: event.target.value});
    status.textContent = '';
  });
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const errors = validateLogin(state);
    const firstError = errors.username || errors.password;
    if (firstError) {
      status.textContent = firstError;
      (errors.username ? username : password).focus();
      return;
    }
    const payload = buildLoginPayload(state);
    state = reduceLogin(state, {type: 'submit'});
    password.value = '';
    submit.disabled = true;
    try {
      const response = await fetch('/api/auth/login', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload),
      });
      if (!response.ok) throw new Error(response.status === 429 ? '尝试次数过多，请稍后再试' : '用户名或密码不正确');
      browser.location.replace('/manage');
    } catch (error) {
      state = reduceLogin(state, {type: 'failure', message: error.message});
      status.textContent = state.error;
      submit.disabled = false;
      password.focus();
    }
  });

  fetch('/api/auth/session', {cache: 'no-store'})
    .then((response) => {
      if (response.ok) browser.location.replace('/manage');
    })
    .catch(() => {});
}

if (typeof document !== 'undefined' && typeof window !== 'undefined') loginApplication(document, window);
