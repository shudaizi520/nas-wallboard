const RESET_PHRASE = '删除 NAS WALLBOARD';
const length = (value) => [...(value ?? '')].length;

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

export function validateReset(values) {
  const errors = {};
  if (!values.administrator) errors.administrator = '请输入管理员密码';
  if (values.confirmation !== RESET_PHRASE) errors.confirmation = '请输入完整确认文字';
  return errors;
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
  const supportButton = root.querySelector('#download-support');
  const backupForm = root.querySelector('#backup-form');
  const passwordForm = root.querySelector('#password-form');
  const resetForm = root.querySelector('#reset-form');
  const message = root.querySelector('#settings-message');
  const report = (text, error = false) => { message.textContent = text; message.dataset.tone = error ? 'error' : 'ok'; };

  supportButton.addEventListener('click', () => { window.location.assign('/api/manage/support'); });
  backupForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = {administrator: backupForm.elements.administrator.value, backup: backupForm.elements.backup.value};
    const error = firstError(validateBackup(values));
    if (error) { report(error, true); return; }
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
    if (error) { report(error, true); return; }
    try { await api.changePassword(values.current, values.replacement); window.location.replace('/login'); }
    catch { report('密码修改失败，请检查当前密码', true); passwordForm.reset(); }
  });
  resetForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = {administrator: resetForm.elements.administrator.value, confirmation: resetForm.elements.confirmation.value};
    const error = firstError(validateReset(values));
    if (error) { report(error, true); return; }
    try {
      const token = await api.reauthenticate(values.administrator);
      await api.factoryReset(values.confirmation, token.token);
      window.location.replace('/setup');
    } catch { report('恢复出厂失败；数据没有被删除', true); resetForm.reset(); }
  });
}
