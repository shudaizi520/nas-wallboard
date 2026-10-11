const KNOWN_ICONS = new Set(['cpu', 'disk', 'network', 'weather-sunny', 'weather-moon', 'weather-cloudy', 'weather-rain', 'weather-storm', 'weather-snow', 'weather-fog', 'fan', 'alert', 'app', 'updates', 'download', 'play', 'uptime']);
const KNOWN_TONES = new Set(['neutral', 'active', 'warn', 'bad', 'good']);
const WARNING_COLORS = new Set(['red', 'orange', 'yellow', 'blue', 'purple', 'black']);
const detailRenderKeys = new WeakMap();
const weatherValueKeys = new WeakMap();
const REMINDER_STATE = Symbol.for('nas-wallboard.weather-reminder-state');
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

function normalizeForecastDays(value) {
  if (!Array.isArray(value)) return [];
  const dates = new Set();
  return value.filter(day => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(day?.date) || dates.has(day.date)
      || !/^(今天|周[一二三四五六日])$/.test(day?.label)
      || typeof day?.condition !== 'string' || !day.condition.trim()
      || !/^-?\d+(?:\.\d+)?°F?$/.test(day?.high) || !/^-?\d+(?:\.\d+)?°F?$/.test(day?.low)) return false;
    dates.add(day.date);
    return true;
  }).slice(0, 5).map(day => ({
    date: day.date, label: day.label, icon: icon(day.icon), condition: day.condition,
    high: day.high, low: day.low, rain: text(day.rain), detail: text(day.detail), hazard: text(day.hazard),
  }));
}

function normalizeForecastHours(value) {
  if (!Array.isArray(value)) return [];
  const seen = new Set();
  return value.filter(hour => {
    if (typeof hour?.at !== 'string' || !Number.isFinite(Date.parse(hour.at)) || seen.has(hour.at)
      || !/^(现在|(?:次日)?(?:[01]?\d|2[0-3])时)$/.test(hour?.label)
      || typeof hour?.condition !== 'string' || !hour.condition.trim()
      || !/^-?\d+(?:\.\d+)?°F?$/.test(hour?.temperature)) return false;
    seen.add(hour.at);
    return true;
  }).slice(0, 6).map(hour => ({at: hour.at, label: hour.label, icon: icon(hour.icon), condition: hour.condition, temperature: hour.temperature, rain: text(hour.rain)}));
}

function normalizeWeatherReminders(activity) {
  const reminders = activity?.weather_reminders;
  if (!Array.isArray(reminders) || !reminders.length
    || !reminders.every(m => typeof m?.text === 'string' && m.text.trim() && typeof m.pinned === 'boolean')
    || reminders.map(m => m.text).join('\n') !== text(activity.detail)) return undefined;
  return reminders.map(m => ({text: m.text, pinned: m.pinned,
    ...(Array.isArray(m.detail_parts) && m.detail_parts.length && m.detail_parts.every(p => typeof p?.text === 'string')
      && m.detail_parts.map(p => p.text).join('') === m.text
      ? {detailParts: m.detail_parts.map(p => ({text: p.text, color: WARNING_COLORS.has(p.color) ? p.color : ''}))} : {}),
  }));
}

function forecastTemperature(value) {
  const match = typeof value === 'string' && value.match(/^(-?\d+(?:\.\d+)?)°(F?)$/);
  if (!match || !Number.isFinite(Number(match[1]))) return null;
  return {value: Number(match[1]), unit: match[2]};
}

export function weatherTemperatureRanges(days, currentValue = '') {
  const ranges = days.map(day => {
    const low = forecastTemperature(day.low), high = forecastTemperature(day.high);
    return low && high && low.unit === high.unit && low.value <= high.value ? {low, high} : null;
  });
  const valid = ranges.filter(Boolean);
  if (!valid.length || valid.some(range => range.low.unit !== valid[0].low.unit)) return days.map(() => null);
  const minimum = Math.min(...valid.map(range => range.low.value));
  const maximum = Math.max(...valid.map(range => range.high.value));
  const span = maximum - minimum;
  if (!Number.isFinite(span)) return days.map(() => null);
  const currentMatch = text(currentValue).match(/^(-?\d+(?:\.\d+)?°F?)(?:\s|·|$)/);
  const current = forecastTemperature(currentMatch?.[1]);
  const percent = value => span === 0 ? 50 : (value - minimum) / span * 100;
  return ranges.map((range, index) => range ? {
    start: percent(range.low.value),
    width: span === 0 ? 0 : (range.high.value - range.low.value) / span * 100,
    current: days[index].label === '今天' && current && current.unit === range.low.unit
      && current.value >= range.low.value && current.value <= range.high.value ? percent(current.value) : null,
  } : null);
}

export function weatherForecastDetail(day) {
  const detail = day.detail || day.condition;
  const missing = text(day.hazard).split('/').filter(hazard => {
    if (!hazard) return false;
    if (hazard === '强风') return !/(强风|阵风)/.test(detail);
    if (hazard === '强紫外线') return !detail.includes('紫外线');
    return !detail.includes(hazard);
  });
  return detail + (missing.length ? ` · 预报${missing.join('/')}` : '');
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
    const forecastDays = id === 'weather' ? normalizeForecastDays(activity?.forecast_days) : [];
    const forecastHours = id === 'weather' ? normalizeForecastHours(activity?.forecast_hours) : [];
    const weatherReminders = id === 'weather' ? normalizeWeatherReminders(activity) : undefined;
    return {
      id,
      icon: icon(activity?.icon),
      title: text(activity?.title),
      value: text(activity?.value),
      detail: download ? '' : text(activity?.detail),
      ...(forecastDays.length ? {forecastDays} : {}),
      ...(forecastHours.length ? {forecastHours} : {}),
      ...(weatherReminders ? {weatherReminders} : {}),
      ...(KNOWN_TONES.has(activity?.value_tone) ? {valueTone: activity.value_tone} : {}),
      ...(id === 'weather' && Array.isArray(activity?.detail_parts) && activity.detail_parts.length > 0
        && activity.detail_parts.every((part) => typeof part?.text === 'string')
        && activity.detail_parts.map((part) => part.text).join('') === text(activity?.detail)
        ? {detailParts: activity.detail_parts.map((part) => ({text: part.text, color: WARNING_COLORS.has(part.color) ? part.color : ''}))} : {}),
      progress: download && Array.isArray(activity?.progress)
        ? activity.progress.filter(Number.isFinite).map((value) => Math.max(0, Math.min(100, Math.round(value))))
        : [],
      tone: tone(activity?.tone),
    };
  }) : [];
  return {
    width,
    connectionTone: raw.connection_tone === 'good' ? 'good' : 'bad',
    dataStatus: text(raw.data_status),
    uptime: text(raw.uptime),
    nasPower: text(raw.nas_power),
    metrics,
    activities: activities.some(activity => activity.id === 'weather' && activity.forecastDays?.length)
      ? activities.filter(activity => !activity.id.startsWith('weather:forecast:')) : activities,
  };
}

function setText(root, selector, value) {
  const node = root.querySelector(selector);
  if (node && node.textContent !== value) node.textContent = value;
}

function setTone(node, value) {
  if (node) node.dataset.tone = value;
}

function setActivityDetail(node, activity) {
  if (!node) return;
  const key = JSON.stringify([activity.detail, activity.detailParts]);
  if (detailRenderKeys.get(node) === key) return;
  if (activity.detailParts) {
    const fragment = node.ownerDocument.createDocumentFragment();
    for (const part of activity.detailParts) {
      const span = node.ownerDocument.createElement('span');
      span.textContent = part.text;
      if (part.color) span.dataset.warningColor = part.color;
      fragment.append(span);
    }
    node.replaceChildren(fragment);
  } else {
    node.textContent = activity.detail;
  }
  detailRenderKeys.set(node, key);
}

function stopWeatherReminders(node) {
  const state = node?.[REMINDER_STATE];
  if (!state) return;
  if (state.timer !== null) node.ownerDocument.defaultView.clearInterval(state.timer);
  delete node[REMINDER_STATE];
}

function reminderElement(document, reminder) {
  const span = document.createElement('span');
  span.className = 'weather-reminder';
  setActivityDetail(span, {detail: reminder.text, detailParts: reminder.detailParts});
  return span;
}

function setWeatherReminders(node, activity) {
  const reminders = activity.weatherReminders;
  if (!reminders) {
    if (node[REMINDER_STATE] || node.classList.contains('weather-reminders')) {
      stopWeatherReminders(node); detailRenderKeys.delete(node);
      node.classList.remove('weather-reminders'); node.removeAttribute('aria-live');
    }
    setActivityDetail(node, activity);
    return;
  }
  const key = JSON.stringify(reminders), previous = node[REMINDER_STATE];
  if (previous?.key === key) return;
  const previousText = previous?.ordinary[previous.index]?.text;
  stopWeatherReminders(node);
  detailRenderKeys.delete(node);
  node.classList.add('weather-reminders'); node.setAttribute('aria-live', 'off');
  const document = node.ownerDocument, fragment = document.createDocumentFragment();
  for (const reminder of reminders.filter(m => m.pinned)) {
    const fixed = reminderElement(document, reminder);
    fixed.classList.add('weather-reminder-fixed'); fixed.setAttribute('aria-live', 'polite');
    fragment.append(fixed);
  }
  const ordinary = reminders.filter(m => !m.pinned);
  const carousel = document.createElement('span'); carousel.className = 'weather-reminder-carousel';
  carousel.hidden = !ordinary.length;
  carousel.setAttribute('role', 'group'); carousel.setAttribute('aria-label', '天气提醒');
  const spans = ordinary.map(m => reminderElement(document, m)); carousel.append(...spans); fragment.append(carousel);
  node.replaceChildren(fragment);
  const state = {key, ordinary, index: Math.max(0, ordinary.findIndex(m => m.text === previousText)), timer: null};
  const display = () => spans.forEach((span, index) => {
    span.classList.toggle('is-current', index === state.index);
    span.setAttribute('aria-hidden', String(index !== state.index));
  });
  node[REMINDER_STATE] = state;
  display();
  if (ordinary.length > 1) state.timer = document.defaultView.setInterval(() => {
    if (!node.isConnected) { stopWeatherReminders(node); return; }
    if (document.visibilityState === 'hidden') return;
    state.index = (state.index + 1) % ordinary.length;
    display();
  }, 6000);
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
  row.innerHTML = '<span class="activity-icon" aria-hidden="true"><svg><use data-field="icon"></use></svg></span><div class="activity-copy"><div class="activity-line"><strong data-field="title"></strong><span data-field="value"></span></div><p data-field="detail"></p><div class="activity-progress-list" data-list="progress" hidden></div></div><i class="activity-signal" aria-hidden="true"></i><div class="weather-hourly" data-list="hourly" aria-label="未来十二小时关键时段，时间间隔不固定" hidden></div><div class="weather-strip" data-list="forecast" aria-label="每日天气预报" hidden></div><p class="weather-day-detail" aria-live="polite" hidden></p>';
  return row;
}

function setWeatherValue(node, activity) {
  const key = activity.value;
  if (weatherValueKeys.get(node) === key) return;
  const match = activity.value.match(/^(-?\d+(?:\.\d+)?°F?)\s*(?:·\s*)?(.+)$/);
  if (match) {
    const temperature = node.ownerDocument.createElement('b');
    temperature.className = 'weather-temperature'; temperature.textContent = match[1];
    const condition = node.ownerDocument.createElement('small');
    condition.className = 'weather-condition'; condition.textContent = match[2];
    node.replaceChildren(temperature, node.ownerDocument.createTextNode(' '), condition);
  } else node.textContent = activity.value.replace(' · ', ' ');
  weatherValueKeys.set(node, key);
}

function createForecastDay(document) {
  const button = document.createElement('button');
  button.type = 'button'; button.className = 'weather-day';
  button.innerHTML = '<small data-field="day"></small><span class="weather-day-icon"><svg aria-hidden="true"><use data-field="icon"></use></svg><small data-field="rain" hidden></small></span><span data-field="low"></span><span class="weather-range-track" aria-hidden="true"><i class="weather-range"></i></span><b data-field="high"></b>';
  return button;
}

function renderHourly(row, hours = []) {
  const strip = row.querySelector('[data-list="hourly"]');
  strip.hidden = !hours.length;
  strip.style.setProperty('--hour-count', String(hours.length || 6));
  strip.classList.toggle('weather-hourly--next-day', hours.some(hour => hour.label.startsWith('次日')));
  reconcile(strip, hours.map(hour => ({...hour, id: hour.at})), document => {
    const node = document.createElement('div'); node.className = 'weather-hour';
    node.innerHTML = '<small data-field="hour"></small><svg aria-hidden="true"><use data-field="icon"></use></svg><span data-field="temperature"></span><small data-field="rain" hidden></small>';
    return node;
  }, (node, hour) => {
    node.dataset.icon = hour.icon;
    node.querySelector('[data-field="icon"]').setAttribute('href', `#icon-${hour.icon}`);
    const label = node.querySelector('[data-field="hour"]');
    if (label.textContent !== hour.label) {
      if (hour.label.startsWith('次日')) {
        const day = label.ownerDocument.createElement('span'); day.className = 'weather-hour-date'; day.textContent = '次日';
        const time = label.ownerDocument.createElement('span'); time.textContent = hour.label.slice(2);
        label.replaceChildren(day, time);
      } else label.textContent = hour.label;
    }
    for (const field of ['temperature','rain']) setText(node, `[data-field="${field}"]`, hour[field]);
    node.querySelector('[data-field="rain"]').hidden = !hour.rain;
    node.setAttribute('aria-label', `${hour.label}，${hour.condition}，${hour.temperature}${hour.rain ? `，降水概率${hour.rain}` : ''}`);
  });
}

function renderForecastDetails(row, days) {
  const selected = days.find(day => day.date === row.dataset.selectedDate);
  if (!selected) delete row.dataset.selectedDate;
  for (const button of row.querySelector('[data-list="forecast"]').children) {
    button.setAttribute('aria-pressed', String(button.dataset.key === selected?.date));
    button.setAttribute('aria-expanded', String(button.dataset.key === selected?.date));
  }
  const detail = row.querySelector('.weather-day-detail');
  detail.hidden = !selected;
  const value = selected ? `${selected.label} · ${selected.low}–${selected.high} · ${weatherForecastDetail(selected)}` : '';
  if (detail.textContent !== value) detail.textContent = value;
}

function renderForecast(row, days = [], currentValue = '') {
  const strip = row.querySelector('[data-list="forecast"]');
  strip.hidden = !days.length;
  // Reserve one shared label width, including optional Fahrenheit decimals.
  const temperatureChars = Math.max(0, ...days.flatMap(day => [day.low.length, day.high.length]));
  strip.style.setProperty('--forecast-temperature-size', `${temperatureChars + 0.25}ch`);
  const ranges = weatherTemperatureRanges(days, currentValue);
  reconcile(strip, days.map((day, index) => ({...day, id: day.date, range: ranges[index]})), createForecastDay, (button, day) => {
    button.dataset.icon = day.icon;
    button.querySelector('[data-field="icon"]').setAttribute('href', `#icon-${day.icon}`);
    for (const field of ['day', 'high', 'low', 'rain']) setText(button, `[data-field="${field}"]`, field === 'day' ? day.label : day[field]);
    button.querySelector('[data-field="rain"]').hidden = !day.rain;
    const track = button.querySelector('.weather-range-track');
    track.hidden = !day.range;
    if (day.range) {
      track.style.setProperty('--range-start', `${day.range.start}%`);
      track.style.setProperty('--range-width', `${day.range.width}%`);
    }
    let marker = track.querySelector('.weather-now');
    if (day.range?.current !== null && day.range?.current !== undefined) {
      if (!marker) { marker = track.ownerDocument.createElement('i'); marker.className = 'weather-now'; track.append(marker); }
      marker.style.left = `${day.range.current}%`;
    } else marker?.remove();
    button.setAttribute('aria-label', `${day.label}，${day.condition}，${day.low}到${day.high}${day.rain ? `，降水概率${day.rain}` : ''}${day.hazard ? `，预报${day.hazard}` : ''}。点击展开详情`);
    button.onclick = () => {
      row.dataset.selectedDate = row.dataset.selectedDate === day.date ? '' : day.date;
      renderForecastDetails(row, days);
      row.closest('.glass-panel')?.dispatchEvent(new Event('wallboard:rendered'));
    };
  });
  renderForecastDetails(row, days);
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
  if (!view.activities.some(a => a.id === 'weather')) stopWeatherReminders(root.querySelector('[data-key="weather"] .activity-copy p'));
  const panel = root.querySelector('.glass-panel');
  panel?.style.setProperty('--panel-width', `${view.width}px`);
  setTone(root.querySelector('[data-bind="connection"]'), view.connectionTone);
  const freshness = root.querySelector('[data-bind="freshness"]');
  if (freshness) {
    freshness.textContent = view.dataStatus;
    freshness.hidden = view.dataStatus === '';
  }
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
    node.classList.toggle('activity-row--weather-current', variant === 'weather');
    node.classList.toggle('activity-row--weather-strip', variant === 'weather' && Boolean(activity.forecastDays?.length));
    node.classList.toggle('activity-row--forecast', variant === 'weather-forecast');
    node.classList.toggle('activity-row--fan', variant === 'fan');
    node.classList.toggle('activity-row--media', variant === 'media');
    node.classList.toggle('activity-row--download', variant === 'download');
    node.dataset.icon = activity.icon;
    node.querySelector('[data-field="icon"]')?.setAttribute('href', `#icon-${activity.icon}`);
    setText(node, '[data-field="title"]', activity.title);
    if (variant === 'weather') setWeatherValue(node.querySelector('[data-field="value"]'), activity);
    else setText(node, '[data-field="value"]', activity.value);
    renderForecast(node, activity.forecastDays, activity.value);
    renderHourly(node, activity.forecastHours);
    if (variant === 'weather') setWeatherReminders(node.querySelector('[data-field="detail"]'), activity);
    else setActivityDetail(node.querySelector('[data-field="detail"]'), activity);
    const value = node.querySelector('[data-field="value"]');
    const detail = node.querySelector('[data-field="detail"]');
    if (value) {
      if (activity.valueTone) value.dataset.tone = activity.valueTone;
      else delete value.dataset.tone;
    }
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
  if (panel) {
    panel.dataset.ready = 'true';
    panel.dispatchEvent(new Event('wallboard:rendered'));
  }
}

export function renderFetchError(root) {
  const detail = root.querySelector('[data-key="weather"] .activity-copy p');
  if (detail?.[REMINDER_STATE]) {
    stopWeatherReminders(detail);
    const carousel = detail.querySelector('.weather-reminder-carousel');
    if (carousel) carousel.hidden = true;
    const notice = detail.ownerDocument.createElement('span'); notice.className = 'weather-reminder-fixed'; notice.textContent = '提醒数据过期';
    detail.append(notice); detail.hidden = false;
  }
  const connection = root.querySelector('[data-bind="connection"]');
  if (connection) connection.dataset.tone = 'bad';
  const freshness = root.querySelector('[data-bind="freshness"]');
  if (freshness) {
    freshness.textContent = '数据过期';
    freshness.hidden = false;
  }
}
