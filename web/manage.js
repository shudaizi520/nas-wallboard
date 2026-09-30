import {ManageAPI} from './manage-api.js';
import {createIntegrationCenter} from './integrations.js';
import {createLayoutEditor} from './layout.js';
import {createOverviewPage} from './overview.js';
import {createSettingsPage} from './settings.js';

const status = document.querySelector('#status');
const logout = document.querySelector('#logout');
let csrf = '';
let overview;
let editor;

async function load() {
  const session = await fetch('/api/auth/session', {cache: 'no-store'});
  if (!session.ok) {
    window.location.replace('/login');
    throw new Error('unauthorized');
  }
  const sessionData = await session.json();
  csrf = sessionData.csrf;
  document.querySelector('#current-username').textContent = sessionData.username;
  const accountUsername = document.querySelector('#account-username');
  if (accountUsername) accountUsername.textContent = sessionData.username;
  const usernameInput = document.querySelector('#username-form [name="username"]');
  if (usernameInput) usernameInput.value = sessionData.username;
  const api = new ManageAPI(csrf);
  overview = createOverviewPage(document.querySelector('#overview-page'), api);
  overview.start().catch(() => {
    const error = document.createElement('p');
    error.className = 'exception-message';
    error.textContent = '概览加载失败';
    document.querySelector('#overview-sections').replaceChildren(error);
  });
  editor = createLayoutEditor(document.querySelector('#desktop-page'), api);
  editor.load().catch(() => {});
  const center = createIntegrationCenter(document.querySelector('#integration-cards'), api, {onChange: () => editor.synchronize().catch(() => {})});
  center.load().catch(() => { document.querySelector('#integration-cards').textContent = '集成加载失败'; });
  createSettingsPage(document.querySelector('#settings-page'), api);
}

for (const tab of document.querySelectorAll('[data-page-target]')) {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.manage-page').forEach((page) => { page.hidden = page.id !== tab.dataset.pageTarget; });
    document.querySelectorAll('[data-page-target]').forEach((button) => button.setAttribute('aria-selected', String(button === tab)));
    const titles = {'overview-page': '管理中心', 'desktop-page': '桌面内容', 'integrations-page': '集成', 'settings-page': '设置与恢复'};
    document.querySelector('#page-title').textContent = titles[tab.dataset.pageTarget] ?? '管理中心';
    if (tab.dataset.pageTarget === 'overview-page') overview?.start(); else overview?.stop();
    editor?.setActive(tab.dataset.pageTarget === 'desktop-page');
  });
}

logout.addEventListener('click', async () => {
  logout.disabled = true;
  const response = await fetch('/api/auth/logout', {method: 'POST', headers: {'X-CSRF-Token': csrf}});
  if (response.ok) {
    window.location.replace('/login');
    return;
  }
  status.textContent = '退出失败';
  logout.disabled = false;
});

load().catch(() => { status.textContent = '加载失败'; status.dataset.tone = 'error'; });
