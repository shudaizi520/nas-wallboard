export function initialLoginState() {
  return {password: '', pending: false, error: ''};
}

export function reduceLogin(state, action) {
  switch (action.type) {
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
  return state.password ? {} : {password: '请输入管理员密码'};
}

export function buildLoginPayload(state) {
  return {password: state.password};
}

function loginApplication(root, browser) {
  let state = initialLoginState();
  const form = root.querySelector('#login-form');
  const password = root.querySelector('#login-password');
  const submit = root.querySelector('#login-submit');
  const status = root.querySelector('#login-status');

  password.addEventListener('input', (event) => {
    state = reduceLogin(state, {type: 'password', value: event.target.value});
    status.textContent = '';
  });
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const errors = validateLogin(state);
    if (errors.password) {
      status.textContent = errors.password;
      password.focus();
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
      if (!response.ok) throw new Error(response.status === 429 ? '尝试次数过多，请稍后再试' : '密码不正确');
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
