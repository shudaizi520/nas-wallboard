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
  const activities = Array.isArray(raw.activities) ? raw.activities.map((activity, index) => ({
    id: text(activity?.id) || `activity:${index}`,
    icon: icon(activity?.icon),
    title: text(activity?.title),
    value: text(activity?.value),
    detail: text(activity?.detail),
    tone: tone(activity?.tone),
  })) : [];
  return {
    width,
    connectionTone: raw.connection_tone === 'good' ? 'good' : 'bad',
    uptime: text(raw.uptime),
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

function reconcile(container, items, create, update) {
  if (!container) return;
  const existing = new Map([...container.children].map((node) => [node.dataset.key, node]));
  for (const item of items) {
    let node = existing.get(item.id);
    if (!node) {
      node = create(container.ownerDocument);
      node.dataset.key = item.id;
      container.append(node);
    }
    update(node, item);
    existing.delete(item.id);
  }
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
  row.innerHTML = '<span class="activity-icon" aria-hidden="true"><svg><use data-field="icon"></use></svg></span><div class="activity-copy"><div class="activity-line"><strong data-field="title"></strong><span data-field="value"></span></div><p data-field="detail"></p></div><i class="activity-signal" aria-hidden="true"></i>';
  return row;
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
    node.classList.toggle('activity-row--weather', activity.id === 'weather');
    node.dataset.icon = activity.icon;
    node.querySelector('[data-field="icon"]')?.setAttribute('href', `#icon-${activity.icon}`);
    setText(node, '[data-field="title"]', activity.title);
    setText(node, '[data-field="value"]', activity.id === 'weather' ? activity.value.replace(' · ', ' ') : activity.value);
    setText(node, '[data-field="detail"]', activity.detail);
    const value = node.querySelector('[data-field="value"]');
    const detail = node.querySelector('[data-field="detail"]');
    if (value) value.hidden = activity.value === '';
    if (detail) detail.hidden = activity.detail === '';
  });
}

export function renderFetchError(root) {
  const connection = root.querySelector('[data-bind="connection"]');
  if (connection) connection.dataset.tone = 'bad';
}
