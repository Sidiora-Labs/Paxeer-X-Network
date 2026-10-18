/** Shared reconnect, visibility and liveness rules for all browser streams. */
export type CancelTask = () => void;
export const isPageActive = () => typeof document === 'undefined' || (!document.hidden && (typeof navigator === 'undefined' || navigator.onLine !== false));
export const jitter = (ceiling: number, random = Math.random) => Math.floor(random() * Math.min(60000, Math.max(0, ceiling)));
const retryUntil = new Map<string, number>();

// A cancelled task removes its browser listeners, even while offline/hidden.
export function scheduleActive(task: () => void, ceiling = 1000, url = ''): CancelTask {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let cancelled = false;
  const clear = () => { if (timer) clearTimeout(timer); timer = undefined; };
  const cancel = () => { cancelled = true; clear(); if (typeof window !== 'undefined') { window.removeEventListener('online', resume); window.removeEventListener('offline', resume); document.removeEventListener('visibilitychange', resume); } };
  const resume = () => {
    clear();
    if (cancelled || !isPageActive()) return;
    const delay = Math.max(jitter(ceiling), (retryUntil.get(url) || 0) - Date.now());
    timer = setTimeout(() => { if (!isPageActive()) { resume(); return; } cancel(); task(); }, delay);
  };
  if (typeof window !== 'undefined') { window.addEventListener('online', resume); window.addEventListener('offline', resume); document.addEventListener('visibilitychange', resume); }
  resume();
  return cancel;
}

const reconnectListeners = new Set<(url: string) => void>();
export function onStreamResync(callback: (url: string) => void): CancelTask { reconnectListeners.add(callback); return () => { reconnectListeners.delete(callback); }; }
export function notifyStreamResync(url: string) { reconnectListeners.forEach(fn => { try { fn(url); } catch { /* independent consumers */ } }); }

/** Per-topic sequence tracking; a gap requires a new authoritative snapshot. */
export function sequenceGuard() {
  const topics = new Map<string, { seq: number; epoch: unknown }>();
  return (value: unknown) => {
    if (!value || typeof value !== 'object') return true;
    const frame = value as { topic?: unknown; seq?: unknown; snapshotId?: unknown; snapshot?: unknown };
    if (typeof frame.topic !== 'string' || typeof frame.seq !== 'number') return true;
    const old = topics.get(frame.topic);
    if (frame.snapshot || !old) {
      // An asynchronous subscribe snapshot may arrive after a live frame.
      // Don't roll the sequence baseline backwards and invent a gap.
      if (!old || old.epoch !== frame.snapshotId || frame.seq >= old.seq) topics.set(frame.topic, { seq: frame.seq, epoch: frame.snapshotId });
      return true;
    }
    if (old.epoch !== frame.snapshotId || frame.seq > old.seq + 1) return false;
    if (frame.seq <= old.seq) return true;
    topics.set(frame.topic, { seq: frame.seq, epoch: frame.snapshotId });
    return true;
  };
}

/** Close half-open sockets, pause hidden tabs, reset backoff only after 30s. */
export function watchSocket(ws: WebSocket, onStable: () => void, ping?: unknown, requireData = false): void {
  let lastMessage = Date.now();
  let lastData = Date.now();
  let openedAt = 0;
  let stable = false;
  let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
  const visibility = () => {
    if (hiddenTimer) clearTimeout(hiddenTimer);
    if (!isPageActive()) hiddenTimer = setTimeout(() => ws.close(4000, 'inactive'), 30000);
  };
  const offline = () => ws.close(4000, 'offline');
  ws.addEventListener('open', () => { openedAt = lastMessage = lastData = Date.now(); visibility(); });
  ws.addEventListener('message', event => {
    lastMessage = Date.now();
    if (requireData) {
      try {
        const frame = JSON.parse(event.data);
        if (frame?.data) {
          if (typeof frame.ts === 'number' && Date.now() - frame.ts > 60000) { ws.close(4000, 'lagging-data'); return; }
          lastData = Date.now();
        }
      } catch { /* control/non-JSON frames do not refresh data freshness */ }
    }
    if (!stable && openedAt && lastMessage - openedAt >= 30000) { stable = true; onStable(); }
  });
  const watchdog = setInterval(() => {
    if (Date.now() - lastMessage > 55000 || (requireData && Date.now() - lastData > 55000)) { ws.close(4000, 'stale'); return; }
    if (ping && ws.readyState === 1) { try { ws.send(JSON.stringify(ping)); } catch { /* close will recover */ } }
  }, ping ? 20000 : 5000);
  window.addEventListener('offline', offline);
  document.addEventListener('visibilitychange', visibility);
  ws.addEventListener('close', event => {
    clearInterval(watchdog); if (hiddenTimer) clearTimeout(hiddenTimer);
    window.removeEventListener('offline', offline); document.removeEventListener('visibilitychange', visibility);
    const hint = /retry[-_ ]?after[=: ]+(\d+)/i.exec(event.reason || '');
    if (hint || event.code === 1013) retryUntil.set(ws.url, Date.now() + Math.min(120000, hint ? Number(hint[1]) * 1000 : 10000));
  }, { once: true });
}

/** Poll only while visible/online, never overlap, and jitter resumes/errors. */
export function activePoll(task: () => unknown | Promise<unknown>, intervalMs: number, immediate = true): CancelTask {
  let stopped = false, running = false, failures = 0;
  let cancelNext: CancelTask | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const run = async () => {
    if (stopped || running || !isPageActive()) return;
    running = true;
    try { await task(); failures = 0; } catch { failures++; }
    finally {
      running = false;
      if (!stopped && intervalMs > 0) timer = setTimeout(() => { cancelNext = scheduleActive(run, failures ? Math.min(60000, intervalMs * 2 ** failures) : 200); }, intervalMs);
    }
  };
  const resume = () => { if (timer) clearTimeout(timer); cancelNext?.(); if (!running) cancelNext = scheduleActive(run, 2000); };
  if (typeof window !== 'undefined') { window.addEventListener('online', resume); window.addEventListener('offline', resume); document.addEventListener('visibilitychange', resume); }
  if (immediate) cancelNext = scheduleActive(run, 250); else if (intervalMs > 0) timer = setTimeout(resume, intervalMs);
  return () => { stopped = true; cancelNext?.(); if (timer) clearTimeout(timer); if (typeof window !== 'undefined') { window.removeEventListener('online', resume); window.removeEventListener('offline', resume); document.removeEventListener('visibilitychange', resume); } };
}
