import { createPoller, visibleRefreshInterval } from './poller.js';
import { renderDashboard, renderFetchError } from './view.js?v=18';

const root = document;

async function showSetupState() {
  try {
    const response = await fetch('/api/setup/status', {cache: 'no-store', headers: {Accept: 'application/json'}});
    if (!response.ok) return;
    const status = await response.json();
    if (!status.setup_required || status.migration) return;
    const panel = root.querySelector('.glass-panel');
    panel.classList.add('setup-pending');
    root.querySelector('[data-bind="setup-required"]').hidden = false;
  } catch {
    // Older installations do not expose setup state; keep the dashboard unchanged.
  }
}

async function fetchDashboard() {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 4_000);
  try {
    const response = await fetch('/api/dashboard', {
      cache: 'no-store',
      headers: {Accept: 'application/json'},
      signal: controller.signal,
    });
    if (!response.ok) throw new Error(`dashboard ${response.status}`);
    return await response.json();
  } finally {
    window.clearTimeout(timeout);
  }
}

const poller = createPoller({
  fetchStatus: fetchDashboard,
  onData: (view) => renderDashboard(root, view),
  onError: () => renderFetchError(root),
  visibleEvery: visibleRefreshInterval(window.location.search),
  hiddenEvery: 60_000,
});

poller.start();
showSetupState();
window.addEventListener('pagehide', () => poller.stop(), {once: true});
