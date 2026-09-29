const RESET_PHRASE = '删除 NAS WALLBOARD';
const length = (value) => [...(value ?? '')].length;

export function settingsSectionDefinitions() {
  return [
    {id: 'administrator', primary: 'username', secondary: 'password', danger: false},
    {id: 'diagnostics', primary: 'description', secondary: 'download', danger: false},
    {id: 'encrypted-backup', primary: 'credentials', secondary: 'download', danger: false},
    {id: 'factory-reset', primary: 'confirmation', secondary: 'delete', danger: true},
  ];
}

export function validateBackup(values) {
  const errors = {};
  if (!values.administrator) errors.administrator = '请输入管理员密码';
  if (length(values.backup) < 12) errors.backup = '备份密码至少 12 个字符';
  return errors;
}

export function validatePasswordChange(values) {
  const errors = {};
  if (!values.current) errors.current = '请输入当前密码';
  if (length(values.replacement) < 12) errors.replacement = '新密码至少 12 个字符';
  else if (values.replacement !== values.confirmation) errors.confirmation = '两次输入的新密码不一致';
  return errors;
}

export function validateUsernameChange(values) {
  const errors = {};
  if (!/^[\p{L}\p{N}._-]{3,32}$/u.test((values.username ?? '').trim())) errors.username = '用户名只能包含中英文字母、数字、点、下划线和连字符';
  if (!values.currentPassword) errors.currentPassword = '请输入当前密码';
  return errors;
}

export function redactUsernameChange(values) {
  return {username: values.username, currentPassword: ''};
}

export function validateReset(values) {
  const errors = {};
  if (!values.administrator) errors.administrator = '请输入管理员密码';
  if (values.confirmation !== RESET_PHRASE) errors.confirmation = '请输入完整确认文字';
  return errors;
}

export function bindExclusiveDisclosures(root) {
  const disclosures = [...root.querySelectorAll('[data-operation-disclosure]')];
  const clear = (disclosure) => {
    for (const form of disclosure.querySelectorAll('form')) form.reset();
    const message = root.querySelector?.('#settings-message');
    if (message) { message.textContent = ''; delete message.dataset.tone; }
  };
  const bindings = disclosures.map((disclosure) => {
    const listener = () => {
      if (!disclosure.open) { clear(disclosure); return; }
      for (const sibling of disclosures) {
        if (sibling === disclosure || !sibling.open) continue;
        sibling.open = false;
        clear(sibling);
      }
    };
    disclosure.addEventListener('toggle', listener);
    return [disclosure, listener];
  });
  return () => { for (const [disclosure, listener] of bindings) disclosure.removeEventListener('toggle', listener); };
}

function firstError(errors) { return Object.values(errors)[0] ?? ''; }

function saveDownload(download, documentRef = document, urlAPI = URL) {
  const url = urlAPI.createObjectURL(download.blob);
  const anchor = documentRef.createElement('a');
  anchor.href = url; anchor.download = download.filename; anchor.hidden = true;
  documentRef.body.append(anchor); anchor.click(); anchor.remove();
  urlAPI.revokeObjectURL(url);
}

export function createSettingsPage(root, api, documentRef = document) {
  for (const definition of settingsSectionDefinitions()) {
    root.querySelector(`[data-settings-section="${definition.id}"]`)?.classList.toggle('danger-section', definition.danger);
  }
  const supportButton = root.querySelector('#download-support');
  const backupForm = root.querySelector('#backup-form');
  const usernameForm = root.querySelector('#username-form');
  const passwordForm = root.querySelector('#password-form');
  const resetForm = root.querySelector('#reset-form');
  const message = root.querySelector('#settings-message');
  bindExclusiveDisclosures(root);
  const report = (text, error = false) => { message.textContent = text; message.dataset.tone = error ? 'error' : 'ok'; };

  supportButton.addEventListener('click', () => { window.location.assign('/api/manage/support'); });
  usernameForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    let values = {username: usernameForm.elements.username.value, currentPassword: usernameForm.elements.currentPassword.value};
    const error = firstError(validateUsernameChange(values));
    if (error) { report(error, true); usernameForm.elements.currentPassword.value = ''; return; }
    const request = {username: values.username.trim(), currentPassword: values.currentPassword};
    values = redactUsernameChange(values);
    usernameForm.elements.username.value = values.username;
    usernameForm.elements.currentPassword.value = values.currentPassword;
    try { await api.changeUsername(request.currentPassword, request.username); window.location.replace('/login'); }
    catch { report('用户名修改失败，请检查当前密码', true); }
  });
  backupForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = {administrator: backupForm.elements.administrator.value, backup: backupForm.elements.backup.value};
    const error = firstError(validateBackup(values));
    if (error) { report(error, true); backupForm.reset(); return; }
    try {
      const token = await api.reauthenticate(values.administrator);
      const download = await api.backup(values.backup, token.token);
      saveDownload(download, documentRef);
      report('加密备份已下载');
    } catch { report('备份失败，请检查管理员密码', true); }
    finally { backupForm.reset(); }
  });
  passwordForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = {current: passwordForm.elements.current.value, replacement: passwordForm.elements.replacement.value, confirmation: passwordForm.elements.confirmation.value};
    const error = firstError(validatePasswordChange(values));
    if (error) { report(error, true); passwordForm.reset(); return; }
    passwordForm.reset();
    try { await api.changePassword(values.current, values.replacement); window.location.replace('/login'); }
    catch { report('密码修改失败，请检查当前密码', true); }
  });
  resetForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = {administrator: resetForm.elements.administrator.value, confirmation: resetForm.elements.confirmation.value};
    const error = firstError(validateReset(values));
    if (error) { report(error, true); resetForm.reset(); return; }
    resetForm.reset();
    try {
      const token = await api.reauthenticate(values.administrator);
      await api.factoryReset(values.confirmation, token.token);
      window.location.replace('/setup');
    } catch { report('恢复出厂失败；数据没有被删除', true); }
  });
}
