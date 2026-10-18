import { SnapshotStreamV2, type StreamStateV2 } from './stream.js';
import { HttpError, StreamTransportError } from './transport.js';
export type ReconnectStatus = { state: StreamStateV2; circuitOpen: boolean; authenticationRequired: boolean; reason?: string };
export function reconnectDelay(attempt: number, random = Math.random): number {
  if (!Number.isSafeInteger(attempt) || attempt < 0) throw new RangeError('Invalid reconnect attempt');
  const sample = random();
  if (!Number.isFinite(sample) || sample < 0 || sample >= 1) throw new RangeError('Invalid jitter sample');
  return sample * Math.min(30000, 500 * 2 ** Math.min(attempt, 16));
}
export class ReconnectingStream {
  private controller: AbortController | undefined;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stable: ReturnType<typeof setTimeout> | undefined;
  private attempt = 0;
  private topics: string[] = [];
  private readonly unobserve: () => void;
  private readonly observers = new Set<(status: ReconnectStatus) => void>();
  status: ReconnectStatus = { state: 'idle', circuitOpen: false, authenticationRequired: false };
  constructor(readonly stream: SnapshotStreamV2, private readonly authenticated: boolean, private readonly maxAttempts = 15) {
    if (!Number.isInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 100) throw new RangeError('Invalid retry budget');
    this.unobserve = stream.observeState((state, reason) => this.changed(state, reason));
  }
  observe(observer: (status: ReconnectStatus) => void): () => void { this.observers.add(observer); try { observer(this.status); } catch {} return () => { this.observers.delete(observer); }; }
  private emit(status: ReconnectStatus) { this.status = Object.freeze(status); for (const observer of [...this.observers]) { if (this.status !== status) return; if (!this.observers.has(observer)) continue; try { observer(status); } catch {} } }
  private changed(state: StreamStateV2, reason?: string) {
    if ((state === 'loading' || state === 'replaying' || state === 'closed') && this.timer !== undefined) { clearTimeout(this.timer); this.timer = undefined; }
    if (state !== 'live' && this.stable !== undefined) { clearTimeout(this.stable); this.stable = undefined; }
    const error = this.stream.lastError;
    const authenticationRequired = this.authenticated && ((error instanceof HttpError && (error.status === 401 || error.status === 403)) || (error instanceof StreamTransportError && ['AUTH_EXPIRED','AUTH_REQUIRED','SCOPE_DENIED','FORBIDDEN'].includes(error.code)) || /AUTH_EXPIRED|AUTH_REQUIRED|SCOPE_DENIED|SESSION_REVOKED|FORBIDDEN/.test(reason ?? ''));
    const controller = this.controller;
    this.emit({ state, circuitOpen: this.status.circuitOpen, authenticationRequired, ...(reason === undefined ? {} : { reason }) });
    if (controller !== this.controller || controller?.signal.aborted) return;
    if (authenticationRequired) {
      if (this.timer !== undefined) clearTimeout(this.timer); this.timer = undefined;
      return;
    }
    if (state === 'live' && this.stable === undefined) this.stable = setTimeout(() => { this.attempt = 0; this.stable = undefined; }, 30000);
    if ((state === 'stale' || state === 'resync-required') && reason !== 'visibility restored' && reason !== 'resume cursor expired' && !authenticationRequired && this.controller && !this.controller.signal.aborted) this.schedule();
  }
  private schedule() {
    if (this.timer !== undefined || this.status.circuitOpen) return;
    if (this.attempt >= this.maxAttempts) { this.emit({ ...this.status, circuitOpen: true, reason: 'reconnect_budget_exhausted' }); return; }
    this.timer = setTimeout(() => { this.timer = undefined; if (this.controller && !this.controller.signal.aborted) void this.stream.reconnect(this.controller.signal); }, reconnectDelay(this.attempt++));
  }
  start(topics: string[], signal?: AbortSignal): Promise<void> {
    this.stop(); this.topics = [...topics]; this.attempt = 0;
    const controller = new AbortController(); this.controller = controller;
    const abort = () => { if (this.controller === controller) this.stop(); };
    signal?.addEventListener('abort', abort, { once: true });
    controller.signal.addEventListener('abort', () => signal?.removeEventListener('abort', abort), { once: true });
    if (signal?.aborted) { this.stop(); return Promise.resolve(); }
    this.emit({ state: 'loading', circuitOpen: false, authenticationRequired: false });
    if (this.controller !== controller || controller.signal.aborted) return Promise.resolve();
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', this.visibility);
    return this.stream.start(this.topics, controller.signal);
  }
  restoreVisibility(): Promise<void> { if (!this.controller || this.status.authenticationRequired || this.status.circuitOpen) return Promise.resolve(); if (this.timer !== undefined) clearTimeout(this.timer); this.timer = undefined; return this.stream.resumeVisible(); }
  private visibility = () => { if (document.visibilityState === 'visible') void this.restoreVisibility(); };
  retry(): Promise<void> { if (!this.controller || this.status.authenticationRequired) return Promise.resolve(); const controller = this.controller; if (this.timer !== undefined) clearTimeout(this.timer); this.timer = undefined; this.emit({ ...this.status, circuitOpen: false }); if (controller !== this.controller || controller.signal.aborted) return Promise.resolve(); return this.stream.reconnect(controller.signal); }
  stop(): void {
    const controller = this.controller; this.controller = undefined; controller?.abort();
    if (this.timer !== undefined) clearTimeout(this.timer); if (this.stable !== undefined) clearTimeout(this.stable);
    this.timer = undefined; this.stable = undefined;
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', this.visibility);
    this.stream.close();
  }
  logout(): void { this.stop(); this.stream.logout(); this.emit({ state: 'closed', circuitOpen: false, authenticationRequired: true }); }
  dispose(): void { this.stop(); this.unobserve(); this.observers.clear(); }
}
