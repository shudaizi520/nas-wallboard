const KNOWN_ICONS = new Set(['cpu', 'disk', 'network', 'weather-sunny', 'weather-cloudy', 'weather-rain', 'weather-storm', 'weather-snow', 'weather-fog', 'fan', 'alert', 'app', 'updates', 'download', 'play', 'uptime']);
const KNOWN_TONES = new Set(['neutral', 'active', 'warn', 'bad', 'good']);
const METRIC_LABELS = new Map([
  ['cpu', '处理器'],
  ['cpu_temperature', '处理器温度'],
  ['disk_temperature', '机械硬盘'],
  ['network', '实时网速'],
  ['pool_capacity', '存储池'],
]);

function text(value) {
  return value === null || value === undefined ? '' : String(value);
}

function icon(value) {
  const candidate = text(value);
  return KNOWN_ICONS.has(candidate) ? candidate : 'app';
}

function tone(value, fallback = 'neutral') {
  const candidate = text(value);
  return KNOWN_TONES.has(candidate) ? candidate : fallback;
}

function compactMetrics(metrics) {
  const cpu = metrics.find((metric) => metric.id === 'cpu');
  const temperature = metrics.find((metric) => metric.id === 'cpu_temperature');
  if (!cpu || !temperature) return metrics;

  const rank = {neutral: 0, active: 1, good: 1, warn: 2, bad: 3};
  const combined = {
    ...cpu,
    value: `${cpu.value} · ${temperature.value}`,
    tone: rank[temperature.tone] > rank[cpu.tone] ? temperature.tone : cpu.tone,
  };
  return metrics.flatMap((metric) => {
    if (metric.id === 'cpu') return [combined];
    if (metric.id === 'cpu_temperature') return [];
    return [metric];
  });
}

export function normalizeDashboard(raw = {}) {
  const width = Number.isInteger(raw.width) && raw.width >= 300 && raw.width <= 720 ? raw.width : 360;
  const metrics = compactMetrics(Array.isArray(raw.metrics) ? raw.metrics.map((metric, index) => ({
    id: text(metric?.id) || `metric:${index}`,
    icon: icon(metric?.icon),
    value: text(metric?.value),
    tone: tone(metric?.tone),
  })) : []);
  const activities = Array.isArray(raw.activities) ? raw.activities.map((activity, index) => {
    const id = text(activity?.id) || `activity:${index}`;
    const download = id === 'downloads';
    return {
      id,
      icon: icon(activity?.icon),
      title: text(activity?.title),
      value: text(activity?.value),
      detail: download ? '' : text(activity?.detail),
      progress: download && Array.isArray(activity?.progress)
        ? activity.progress.filter(Number.isFinite).map((value) => Math.max(0, Math.min(100, Math.round(value))))
        : [],
      tone: tone(activity?.tone),
    };
  }) : [];
  return {
    width,
    connectionTone: raw.connection_tone === 'good' ? 'good' : 'bad',
    uptime: text(raw.uptime),
    nasPower: text(raw.nas_power),
    metrics,
    activities,
  };
}

function setText(root, selector, value) {
  const node = root.querySelector(selector);
  if (node && node.textContent !== value) node.textContent = value;
}

function setTone(node, value) {
  if (node) node.dataset.tone = value;
}

export function reconcile(container, items, create, update) {
  if (!container) return;
  const existing = new Map([...container.children].map((node) => [node.dataset.key, node]));
  items.forEach((item, index) => {
    let node = existing.get(item.id);
    if (!node) {
      node = create(container.ownerDocument);
      node.dataset.key = item.id;
      container.append(node);
    }
    update(node, item);
    const current = container.children[index];
    if (current !== node) container.insertBefore(node, current ?? null);
    existing.delete(item.id);
  });
  for (const node of existing.values()) node.remove();
}

function createMetric(document) {
  const metric = document.createElement('span');
  metric.className = 'health-value';
  metric.innerHTML = '<small data-field="label"></small><b data-field="value"></b><span class="network-rates" data-field="rates" hidden><i data-field="down"></i><i data-field="up"></i></span>';
  return metric;
}

function createActivityRow(document) {
  const row = document.createElement('article');
  row.className = 'activity-row';
  row.innerHTML = '<span class="activity-icon" aria-hidden="true"><svg><use data-field="icon"></use></svg></span><div class="activity-copy"><div class="activity-line"><strong data-field="title"></strong><span data-field="value"></span></div><p data-field="detail"></p><div class="activity-progress-list" data-list="progress" hidden></div></div><i class="activity-signal" aria-hidden="true"></i>';
  return row;
}

function createDownloadProgress(document) {
  const row = document.createElement('div');
  row.className = 'download-progress';
  row.innerHTML = '<span class="download-progress-label"></span><span class="download-progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100"><i></i></span><b class="download-progress-value"></b>';
  return row;
}

export function activityVariant(id) {
  if (id === 'weather') return 'weather';
  if (id.startsWith('weather:forecast:')) return 'weather-forecast';
  if (id === 'fan') return 'fan';
  if (id === 'plex' || id.startsWith('plex:') || id === 'jellyfin' || id.startsWith('jellyfin:')) return 'media';
  if (id === 'downloads') return 'download';
  return 'default';
}

export function renderDashboard(root, raw) {
  const view = normalizeDashboard(raw);
  const panel = root.querySelector('.glass-panel');
  panel?.style.setProperty('--panel-width', `${view.width}px`);
  setTone(root.querySelector('[data-bind="connection"]'), view.connectionTone);
  const uptime = root.querySelector('[data-bind="uptime"]');
  if (uptime) {
    if (uptime.textContent !== view.uptime) uptime.textContent = view.uptime;
    uptime.hidden = view.uptime === '';
  }
  const nasPower = root.querySelector('[data-bind="nas-power"]');
  if (nasPower) {
    if (nasPower.textContent !== view.nasPower) nasPower.textContent = view.nasPower;
    nasPower.hidden = view.nasPower === '';
  }
  const metricList = root.querySelector('[data-list="metrics"]');
  metricList?.style.setProperty('--metric-count', String(Math.max(view.metrics.length, 1)));
  if (metricList) metricList.dataset.count = String(view.metrics.length);
  reconcile(metricList, view.metrics, createMetric, (node, metric) => {
    setTone(node, metric.tone);
    node.dataset.variant = metric.id === 'network' ? 'network' : 'primary';
    setText(node, '[data-field="label"]', METRIC_LABELS.get(metric.id) ?? metric.id.toUpperCase());
    setText(node, '[data-field="value"]', metric.value);
    const match = metric.value.match(/^↓\s*(.*?)\s+↑\s*(.*)$/);
    const value = node.querySelector('[data-field="value"]');
    const rates = node.querySelector('[data-field="rates"]');
    const splitNetwork = metric.id === 'network' && match;
    if (value) value.hidden = Boolean(splitNetwork);
    if (rates) rates.hidden = !splitNetwork;
    if (splitNetwork) {
      setText(node, '[data-field="down"]', `↓ ${match[1]}`);
      setText(node, '[data-field="up"]', `↑ ${match[2]}`);
    }
  });
  reconcile(root.querySelector('[data-list="activities"]'), view.activities, createActivityRow, (node, activity) => {
    setTone(node, activity.tone);
    const variant = activityVariant(activity.id);
    node.classList.toggle('activity-row--weather', variant === 'weather');
    node.classList.toggle('activity-row--forecast', variant === 'weather-forecast');
    node.classList.toggle('activity-row--fan', variant === 'fan');
    node.classList.toggle('activity-row--media', variant === 'media');
    node.classList.toggle('activity-row--download', variant === 'download');
    node.dataset.icon = activity.icon;
    node.querySelector('[data-field="icon"]')?.setAttribute('href', `#icon-${activity.icon}`);
    setText(node, '[data-field="title"]', activity.title);
    setText(node, '[data-field="value"]', variant === 'weather' ? activity.value.replace(' · ', ' ') : activity.value);
    setText(node, '[data-field="detail"]', activity.detail);
    const value = node.querySelector('[data-field="value"]');
    const detail = node.querySelector('[data-field="detail"]');
    if (value) value.hidden = activity.value === '';
    if (detail) detail.hidden = activity.detail === '';
    const progressList = node.querySelector('[data-list="progress"]');
    if (progressList) progressList.hidden = variant !== 'download' || activity.progress.length === 0;
    reconcile(progressList, activity.progress.map((progress, index) => ({id: String(index), progress, label: `任务 ${index + 1}`})), createDownloadProgress, (progressNode, item) => {
      setText(progressNode, '.download-progress-label', item.label);
      setText(progressNode, '.download-progress-value', `${item.progress}%`);
      const track = progressNode.querySelector('.download-progress-track');
      track?.setAttribute('aria-label', `${item.label}下载进度`);
      track?.setAttribute('aria-valuenow', String(item.progress));
      track?.style.setProperty('--progress', `${item.progress}%`);
    });
  });
}

export function renderFetchError(root) {
  const connection = root.querySelector('[data-bind="connection"]');
  if (connection) connection.dataset.tone = 'bad';
}
