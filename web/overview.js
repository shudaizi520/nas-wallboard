export function healthCards(data = {}) {
  const cards = [];
  const nas = data.nas ?? {};
  cards.push({
    title: 'TrueNAS', value: nas.version || '等待数据', tone: nas.connected ? 'good' : 'bad',
    detail: nas.connected ? '数据连接正常' : '连接中断，请检查 TrueNAS 集成',
  });
  for (const item of data.integrations ?? []) {
    const good = item.running && item.healthy;
    cards.push({
      title: item.type, value: good ? '运行中' : (item.message || '异常'), tone: good ? 'good' : 'bad',
      detail: good ? '采集器工作正常' : '请到集成中心检查连接和权限',
    });
  }
  return cards;
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

export function createOverviewPage(root, api, documentRef = document) {
  const cards = root.querySelector('#overview-cards');
  const details = root.querySelector('#overview-details');
  const updates = root.querySelector('#update-result');
  const check = root.querySelector('#check-update');
  const render = async () => {
    const data = await api.overview();
    cards.replaceChildren(...healthCards(data).map((item) => {
      const card = documentRef.createElement('article');
      card.className = `health-card ${item.tone}`;
      const title = documentRef.createElement('span'); title.textContent = item.title;
      const value = documentRef.createElement('strong'); value.textContent = item.value;
      const detail = documentRef.createElement('small'); detail.textContent = item.detail;
      card.append(title, value, detail);
      return card;
    }));
    details.textContent = `Wallboard ${data.version} · 应用已运行 ${Math.max(0, data.application_uptime_seconds ?? 0)} 秒${data.migration?.imported ? ' · 已从旧配置迁移' : ''}`;
  };
  check.addEventListener('click', async () => {
    check.disabled = true;
    try {
      const result = await api.updateStatus();
      updates.textContent = result.error || (result.available ? `发现新版本 ${result.latest}` : '当前已是最新版本');
      if (result.url) {
        const text = updates.textContent;
        const anchor = documentRef.createElement('a');
        anchor.href = result.url; anchor.target = '_blank'; anchor.rel = 'noopener'; anchor.textContent = text;
        updates.replaceChildren(anchor);
      }
    } catch { updates.textContent = '暂时无法检查更新'; }
    finally { check.disabled = false; }
  });
  return createOverviewPoller(documentRef, render);
}
