const MIN_WIDTH = 200;
const MAX_WIDTH = 800;
const MIN_HEIGHT = 80;
const MAX_HEIGHT = 1000;

function clampRounded(value, minimum, maximum) {
  const rounded = Math.round(Number(value) || minimum);
  return Math.max(minimum, Math.min(maximum, rounded));
}

export function startDesktopMode({window, document, ResizeObserver}) {
  const params = new URLSearchParams(window.location.search);
  if (params.get('desktop') !== '1') return () => {};

  document.documentElement.classList.add('desktop-mode');
  const panel = document.querySelector('.glass-panel');
  if (!panel) return () => {};

  const post = (message) => window.chrome?.webview?.postMessage(message);
  const reportSize = () => {
    const bounds = panel.getBoundingClientRect();
    post({
      type: 'resize',
      width: clampRounded(bounds.width, MIN_WIDTH, MAX_WIDTH),
      height: clampRounded(bounds.height, MIN_HEIGHT, MAX_HEIGHT),
      ready: panel.dataset?.ready === 'true',
    });
  };
  const hostMessage = (event) => {
    if (event.data?.type !== 'movable') return;
    document.documentElement.classList.toggle('desktop-movable', event.data.enabled === true);
  };

  const observer = new ResizeObserver(reportSize);
  observer.observe(panel);
  panel.addEventListener('wallboard:rendered', reportSize);
  window.chrome?.webview?.addEventListener?.('message', hostMessage);
  reportSize();

  return () => {
    observer.disconnect();
    panel.removeEventListener('wallboard:rendered', reportSize);
    window.chrome?.webview?.removeEventListener?.('message', hostMessage);
  };
}

if (typeof window !== 'undefined' && typeof document !== 'undefined' && typeof ResizeObserver !== 'undefined') {
  startDesktopMode({window, document, ResizeObserver});
}
