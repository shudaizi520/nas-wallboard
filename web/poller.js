function browserEnvironment() {
  return {
    setTimeout: (callback, delay) => window.setTimeout(callback, delay),
    clearTimeout: (id) => window.clearTimeout(id),
    isVisible: () => document.visibilityState !== 'hidden',
    onVisibilityChange(handler) {
      const listener = () => handler(document.visibilityState !== 'hidden');
      document.addEventListener('visibilitychange', listener);
      return () => document.removeEventListener('visibilitychange', listener);
    },
  };
}

export function visibleRefreshInterval(search = '') {
  return new URLSearchParams(search).get('desktop') === '1' ? 15_000 : 5_000;
}

export function createPoller({
  fetchStatus,
  onData,
  onError,
  visibleEvery = 5_000,
  hiddenEvery = 60_000,
  environment = browserEnvironment(),
}) {
  let started = false;
  let stopped = false;
  let inFlight = false;
  let refreshAfterFlight = false;
  let timer = null;
  let unsubscribe = () => {};
  let wasVisible = environment.isVisible();

  const clearTimer = () => {
    if (timer !== null) {
      environment.clearTimeout(timer);
      timer = null;
    }
  };

  const schedule = () => {
    if (stopped) return;
    clearTimer();
    const delay = environment.isVisible() ? visibleEvery : hiddenEvery;
    timer = environment.setTimeout(run, delay);
  };

  const run = async () => {
    if (stopped || inFlight) return;
    clearTimer();
    inFlight = true;
    try {
      const data = await fetchStatus();
      if (!stopped) onData(data);
    } catch (error) {
      if (!stopped) onError(error);
    } finally {
      inFlight = false;
      if (stopped) return;
      if (refreshAfterFlight) {
        refreshAfterFlight = false;
        void run();
      } else {
        schedule();
      }
    }
  };

  const visibilityChanged = (visible) => {
    const restored = visible && !wasVisible;
    wasVisible = visible;
    if (!restored || stopped) return;
    clearTimer();
    if (inFlight) {
      refreshAfterFlight = true;
    } else {
      void run();
    }
  };

  return {
    start() {
      if (started) return;
      started = true;
      stopped = false;
      wasVisible = environment.isVisible();
      unsubscribe = environment.onVisibilityChange(visibilityChanged);
      void run();
    },
    stop() {
      if (!started || stopped) return;
      stopped = true;
      clearTimer();
      unsubscribe();
    },
    refresh() {
      if (stopped || inFlight) return;
      void run();
    },
  };
}
