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
  csrf = (await session.json()).csrf;
  const api = new ManageAPI(csrf);
  overview = createOverviewPage(document.querySelector('#overview-page'), api);
  overview.start().catch(() => { document.querySelector('#overview-details').textContent = '概览暂时无法读取；其他设置仍可使用。'; });
  editor = createLayoutEditor(document.querySelector('#desktop-page'), api);
  await editor.load();
  const center = createIntegrationCenter(document.querySelector('#integration-cards'), api);
  center.load().catch((error) => { document.querySelector('#integration-cards').textContent = `集成中心加载失败：${error.message}`; });
  createSettingsPage(document.querySelector('#settings-page'), api);
}

for (const tab of document.querySelectorAll('[data-page-target]')) {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.manage-page').forEach((page) => { page.hidden = page.id !== tab.dataset.pageTarget; });
    document.querySelectorAll('[data-page-target]').forEach((button) => button.setAttribute('aria-selected', String(button === tab)));
    const titles = {'overview-page': '管理中心', 'desktop-page': '桌面内容', 'integrations-page': '集成中心', 'settings-page': '设置与恢复'};
    document.querySelector('h1').textContent = titles[tab.dataset.pageTarget] ?? '管理中心';
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
