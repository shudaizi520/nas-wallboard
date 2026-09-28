export class ManageAPI {
  constructor(csrf, fetcher = fetch) { this.csrf = csrf; this.fetcher = (...args) => fetcher(...args); }
  async request(path, options = {}) {
    const headers = {Accept: 'application/json', ...(options.headers ?? {})};
    if (options.method && options.method !== 'GET') headers['X-CSRF-Token'] = this.csrf;
    const response = await this.fetcher(path, {...options, headers});
    const data = response.status === 204 ? null : await response.json().catch(() => ({}));
    if (!response.ok) { const error = new Error(data.error ?? 'request_failed'); error.code = data.error; error.status = response.status; error.data = data; throw error; }
    return data;
  }
  integrations() { return this.request('/api/manage/integrations'); }
  layout() { return this.request('/api/manage/layout'); }
  saveLayout(layout) { return this.request('/api/manage/layout', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(layout)}); }
  probe(candidate, signal) { return this.request('/api/manage/integrations/probe', {method: 'POST', signal, headers: {'Content-Type': 'application/json'}, body: JSON.stringify(candidate)}); }
  create(candidate) { return this.request('/api/manage/integrations', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(candidate)}); }
  update(id, candidate) { return this.request(`/api/manage/integrations/${encodeURIComponent(id)}`, {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(candidate)}); }
  action(id, action) { return this.request(`/api/manage/integrations/${encodeURIComponent(id)}/${action}`, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}'}); }
  remove(id) { return this.request(`/api/manage/integrations/${encodeURIComponent(id)}`, {method: 'DELETE', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({confirm: id})}); }
  overview() { return this.request('/api/manage/overview'); }
  updateStatus() { return this.request('/api/manage/update'); }
  reauthenticate(password) { return this.request('/api/manage/reauth', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({password})}); }
  changePassword(current, replacement) { return this.request('/api/manage/password', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({current, replacement})}); }
  factoryReset(confirmation, token) { return this.request('/api/manage/reset', {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Reauth-Token': token}, body: JSON.stringify({confirmation})}); }
  async backup(password, token) {
    const response = await this.fetcher('/api/manage/backup', {method: 'POST', headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrf, 'X-Reauth-Token': token}, body: JSON.stringify({password})});
    if (!response.ok) { const data = await response.json().catch(() => ({})); const error = new Error(data.error ?? 'backup_failed'); error.code = data.error; throw error; }
    return {blob: await response.blob(), filename: 'nas-wallboard-backup.age'};
  }
}
