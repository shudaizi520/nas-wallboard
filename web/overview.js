import {createSettingsSection, setInlineStatus} from './manage-ui.js';

const collectionNames = {truenas:'TrueNAS',plex:'Plex',jellyfin:'Jellyfin',qbittorrent:'qBittorrent',home_assistant:'Home Assistant',qweather:'和风天气',scrutiny:'Scrutiny',uptime_kuma:'Uptime Kuma'};
export function collectionRows(collectors = []) {
  return collectors.map((item) => {
    const date = new Date(item.last_success ?? '');
    return {name:collectionNames[item.type] || item.type, status:item.message || (item.healthy ? '采集正常' : '等待采集'), lastSuccess:date.getUTCFullYear() > 1970 ? date.toLocaleString('zh-CN', {timeZone:'Asia/Shanghai',hour12:false}) : '尚无成功采集'};
  });
}

function formatDuration(totalSeconds) {
  const seconds = Math.max(0, Number(totalSeconds) || 0);
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days) return `${days}天 ${hours}小时`;
  if (hours) return `${hours}小时 ${minutes}分`;
  return `${minutes}分`;
}

export function overviewSummary(data = {}) {
  const collectors = data.integrations ?? [];
  const healthy = collectors.filter((item) => item.running && item.healthy).length;
  const failed = collectors.length - healthy;
  const allHealthy = Boolean(data.nas?.connected) && failed === 0;
  return [
    {label: '运行状态', value: allHealthy ? '运行正常' : '需要关注', tone: allHealthy ? 'good' : 'bad'},
    {label: '正常采集', value: String(healthy), tone: 'good'},
    {label: '异常采集', value: String(failed), tone: failed ? 'bad' : 'neutral'},
    {label: '运行时间', value: formatDuration(data.application_uptime_seconds), tone: 'neutral'},
  ];
}

export function overviewRows(data = {}) {
  const nas = data.nas ?? {};
  const collectors = data.integrations ?? [];
  const healthy = collectors.filter((item) => item.running && item.healthy).length;
  const failed = collectors.length - healthy;
  const firstFailure = collectors.find((item) => !(item.running && item.healthy));
  return [
    {
      id: 'application', title: '应用',
      value: `${data.version || '未知版本'} · ${formatDuration(data.application_uptime_seconds)}`,
      tone: 'neutral', update: true,
    },
    {
      id: 'truenas', title: 'TrueNAS',
      value: nas.version || '等待数据', detail: nas.connected ? '' : '连接中断',
      tone: nas.connected ? 'neutral' : 'bad',
    },
    {
      id: 'collectors', title: '采集器',
      value: `${healthy} 正常 · ${failed} 异常`, detail: firstFailure?.message || '',
      tone: failed ? 'bad' : 'neutral', collections:collectionRows(collectors),
    },
    {
      id: 'desktop-client', title: '桌面客户端',
      value: 'Windows 小组件', tone: 'neutral',
      actions: [
        {label: '安装桌面小组件', href: '/download/nas-wallboard-desktop.zip'},
        {label: '网页预览', href: '/', external: true},
      ],
    },
  ];
}

export function createOverviewPoller(documentRef, load, setTimer = setTimeout, clearTimer = clearTimeout, interval = 60_000) {
  let timer = 0;
  let stopped = true;
  let loading = false;
  const schedule = () => {
    if (stopped) return;
    timer = setTimer(async () => {
      if (!documentRef.hidden) await run();
      else schedule();
    }, interval);
  };
  const run = async () => {
    if (stopped || loading || documentRef.hidden) return;
    loading = true;
    try { await load(); } finally { loading = false; schedule(); }
  };
  const visibility = async () => {
    clearTimer(timer);
    if (!documentRef.hidden) await run();
  };
  return {
    start() {
      if (!stopped) return Promise.resolve();
      stopped = false;
      documentRef.addEventListener('visibilitychange', visibility);
      return run();
    },
    stop() { stopped = true; clearTimer(timer); documentRef.removeEventListener?.('visibilitychange', visibility); },
    refresh: run,
  };
}

function statusBlock(documentRef, row) {
  const block = documentRef.createElement('div');
  block.className = 'overview-value';
  const value = documentRef.createElement('strong');
  value.textContent = row.value;
  value.dataset.tone = row.tone;
  block.append(value);
  if (row.tone === 'bad' && row.detail) {
    const detail = documentRef.createElement('p');
    detail.className = 'exception-message';
    detail.textContent = row.detail;
    block.append(detail);
  }
  if (row.collections?.length) {
    const details = documentRef.createElement('details');
    const summary = documentRef.createElement('summary'); summary.textContent = '查看采集状态与最后成功时间'; details.append(summary);
    for (const item of row.collections) { const line = documentRef.createElement('p'); line.textContent = `${item.name} · ${item.status} · ${item.lastSuccess}`; details.append(line); }
    block.append(details);
  }
  return block;
}

function actionLinks(documentRef, actions) {
  const group = documentRef.createElement('div');
  group.className = 'section-actions';
  for (const action of actions) {
    const anchor = documentRef.createElement('a');
    anchor.className = action === actions[0] ? 'primary-button' : 'secondary-button';
    anchor.href = action.href;
    anchor.textContent = action.label;
    if (action.external) { anchor.target = '_blank'; anchor.rel = 'noopener'; }
    group.append(anchor);
  }
  return group;
}

function summaryBlock(documentRef, values) {
  const section = documentRef.createElement('section');
  section.className = 'overview-summary';
  section.setAttribute('aria-label', '运行摘要');
  for (const item of values) {
    const metric = documentRef.createElement('div');
    const label = documentRef.createElement('span');
    label.textContent = item.label;
    const value = documentRef.createElement('strong');
    value.textContent = item.value;
    value.dataset.tone = item.tone;
    metric.append(label, value);
    section.append(metric);
  }
  return section;
}

export function createOverviewPage(root, api, documentRef = document) {
  const list = root.querySelector('#overview-sections');
  const updateControls = () => {
    const group = documentRef.createElement('div');
    group.className = 'overview-update';
    const result = documentRef.createElement('span');
    const button = documentRef.createElement('button');
    button.type = 'button';
    button.className = 'secondary-button';
    button.textContent = '检查更新';
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        const update = await api.updateStatus();
        setInlineStatus(result, update.error || (update.available ? `发现新版本 ${update.latest}` : '当前已是最新版本'), update.error ? 'bad' : 'good');
        if (update.url) {
          const anchor = documentRef.createElement('a');
          anchor.href = update.url; anchor.target = '_blank'; anchor.rel = 'noopener'; anchor.textContent = result.textContent;
          result.replaceChildren(anchor);
        }
      } catch { setInlineStatus(result, '暂时无法检查更新', 'bad'); }
      finally { button.disabled = false; }
    });
    group.append(result, button);
    return group;
  };

  const render = async () => {
    const data = await api.overview();
    const sections = overviewRows(data).map((row) => {
      let aside;
      if (row.update) aside = updateControls();
      else if (row.actions) aside = actionLinks(documentRef, row.actions);
      const section = createSettingsSection(documentRef, {
        className: 'overview-row', title: row.title, description: row.description, main: statusBlock(documentRef, row), aside,
      });
      section.dataset.section = row.id;
      return section;
    });
    list.replaceChildren(summaryBlock(documentRef, overviewSummary(data)), ...sections);
  };
  return createOverviewPoller(documentRef, render);
}
