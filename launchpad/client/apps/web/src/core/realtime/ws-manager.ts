/**
 * Shared WebSocket connection manager (T05.1 / D6).
 *
 * Generalises the resilient connection logic that previously lived only in
 * chat-ws.ts into a reusable backbone for every real-time stream (chat,
 * candles, future orderbook/trades). It adds the financial-correctness
 * guarantees the old ad-hoc clients lacked:
 *
 *   - Auto-reconnect with exponential backoff + full jitter, capped, with a
 *     circuit breaker after `maxReconnectAttempts`.
 *   - Heartbeat ping + PONG-TIMEOUT dead-connection detection (a silently dead
 *     socket is force-closed and reconnected instead of hanging until TCP RST).
 *   - Monotonic SEQUENCE/GAP detection: a missed message triggers `onGap` and a
 *     resync instead of silently corrupting an orderbook/trade feed.
 *   - SNAPSHOT + DELTA resync: `onResync` fires on every (re)connect so the
 *     consumer can re-pull a REST snapshot before applying live diffs.
 *   - BACKPRESSURE: optional bounded, keyed coalescing of inbound ticks flushed
 *     on requestAnimationFrame — bursts never flood the main thread, stale
 *     ticks are dropped in favour of the latest per key.
 *   - PAGE VISIBILITY awareness (T05.3): pause/throttle when the tab is hidden,
 *     full resync on re-show.
 *
 * Framework-agnostic and SSR-safe (no-ops where `WebSocket` is unavailable).
 */

import { CoalescingBuffer, RafScheduler, backoffWithJitter } from './raf-batch';

export type WsStatus = 'idle' | 'connecting' | 'open' | 'closed' | 'circuit-open';

export interface WsManagerContext {
  /** Send a JSON-serialisable payload if the socket is open. */
  send: (data: unknown) => void;
  /** Current connection status. */
  status: WsStatus;
}

export interface WsManagerOptions<TMessage = unknown> {
  /** Resolved lazily so the URL reflects current location/scheme each connect. */
  url: () => string;
  protocols?: string | string[];

  /** Parse a raw socket frame into a typed message (default: JSON.parse). */
  parse?: (raw: string) => TMessage;

  /** Route a parsed message to consumers. */
  onMessage: (message: TMessage, ctx: WsManagerContext) => void;

  /**
   * Fires once per (re)connection, after the socket opens. Send auth /
   * (re)subscribe frames here and kick off a REST snapshot pull. This is the
   * snapshot half of snapshot+delta resync.
   */
  onResync?: (ctx: WsManagerContext) => void;

  onStatusChange?: (status: WsStatus) => void;
  onError?: (error: string) => void;

  /**
   * Extract a monotonically-increasing sequence number from a message, or
   * `undefined` if the message is not sequenced. When provided, a gap triggers
   * `onGap` and a resync.
   */
  getSequence?: (message: TMessage) => number | undefined;
  onGap?: (expected: number, received: number) => void;

  /** Decide whether a close event should trigger reconnect (default: true). */
  shouldReconnect?: (event: CloseEvent) => boolean;

  /** Heartbeat frame (default `{ type: 'ping' }`). */
  pingMessage?: () => unknown;
  /** Detect a heartbeat reply (default `message.type === 'pong'`). */
  isPong?: (message: TMessage) => boolean;

  pingIntervalMs?: number;
  pongTimeoutMs?: number;
  maxReconnectAttempts?: number;
  backoffBaseMs?: number;
  backoffCapMs?: number;

  /**
   * Enable inbound backpressure coalescing. `key` derives a coalescing key per
   * message (e.g. `${pool}:${timeframe}`); only the latest message per key per
   * frame is delivered. Omit for streams (like chat) that must not drop frames.
   */
  coalesce?: {
    key: (message: TMessage) => string;
    maxBuffer?: number;
    replaceable?: (message: TMessage) => boolean;
  };

  /** React to tab visibility changes (default true in the browser). */
  pauseWhenHidden?: boolean;
  signal?: AbortSignal;
}

const noopRng = Math.random;

export class WsManager<TMessage = unknown> {
  private ws: WebSocket | null = null;
  private status: WsStatus = 'idle';
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private awaitingPong = false;
  private lastSeq: number | null = null;
  private wantOpen = false;
  private stableTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly onAbort = () => this.close();
  private visibilityBound = false;

  private readonly buffer: CoalescingBuffer<TMessage> | null;
  private readonly flusher = new RafScheduler();

  private readonly opts: Required<
    Pick<
      WsManagerOptions<TMessage>,
      'pingIntervalMs' | 'pongTimeoutMs' | 'maxReconnectAttempts' | 'backoffBaseMs' | 'backoffCapMs'
    >
  > &
    WsManagerOptions<TMessage>;

  constructor(options: WsManagerOptions<TMessage>) {
    this.opts = {
      pingIntervalMs: 25_000,
      pongTimeoutMs: 10_000,
      maxReconnectAttempts: 15,
      backoffBaseMs: 500,
      backoffCapMs: 30_000,
      ...options,
    };
    this.buffer = options.coalesce
      ? new CoalescingBuffer<TMessage>({ maxSize: options.coalesce.maxBuffer })
      : null;
  }

  // ── Public API ────────────────────────────────────────────────────────────

  getStatus(): WsStatus {
    return this.status;
  }

  connect(): void {
    if (typeof WebSocket === 'undefined' || this.opts.signal?.aborted) return;
    if (this.wantOpen) return;
    this.wantOpen = true;
    this.reconnectAttempt = 0;
    this.opts.signal?.addEventListener('abort', this.onAbort, { once: true });
    this.bindVisibility();
    this.openSocket();
  }

  /** Permanently close and tear down (consumer unmount / logout). */
  close(): void {
    this.wantOpen = false;
    this.opts.signal?.removeEventListener('abort', this.onAbort);
    this.clearTimers();
    this.flusher.cancel();
    this.buffer?.clear();
    this.unbindVisibility();
    if (this.ws) {
      const socket = this.ws;
      socket.onclose = null;
      socket.onerror = null;
      socket.onmessage = null;
      if (socket.readyState === WebSocket.CONNECTING) {
        // Closing a half-open socket aborts the handshake and the browser logs
        // "closed before the connection is established"; finish it, then close.
        socket.onopen = () => { try { socket.close(); } catch { /* already closing */ } };
      } else {
        socket.onopen = null;
        try {
          socket.close();
        } catch {
          /* already closing */
        }
      }
      this.ws = null;
    }
    this.setStatus('closed');
  }

  send(data: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(typeof data === 'string' ? data : JSON.stringify(data));
    }
  }

  // ── Connection lifecycle ────────────────────────────────────────────────────

  private get ctx(): WsManagerContext {
    return { send: (d) => this.send(d), status: this.status };
  }

  private openSocket(): void {
    if (!this.wantOpen) return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    this.setStatus('connecting');
    if (!this.wantOpen) return;
    this.lastSeq = null; // sequence restarts each connection

    let socket: WebSocket;
    try {
      const url = new URL(this.opts.url(), typeof window === 'undefined' ? undefined : window.location.href);
      if (!['ws:', 'wss:'].includes(url.protocol) || url.username || url.password || url.hash || [...url.searchParams.keys()].some(key => /^(?:token|ticket|jwt|authorization|access_token|refresh_token)$/i.test(key))) throw new TypeError('Invalid credential-free stream URL');
      socket = this.opts.protocols
        ? new WebSocket(url.toString(), this.opts.protocols)
        : new WebSocket(url.toString());
    } catch {
      this.report('WEBSOCKET_CONSTRUCTION_FAILED');
      this.scheduleReconnect();
      return;
    }
    this.ws = socket;

    socket.onopen = () => {
      if (!this.wantOpen || this.ws !== socket) return;
      this.stableTimer = setTimeout(() => { this.reconnectAttempt = 0; this.stableTimer = null; }, 30_000);
      this.setStatus('open');
      if (!this.wantOpen || this.ws !== socket) return;
      this.startHeartbeat();
      // Snapshot+delta: let the consumer (re)subscribe + pull a fresh snapshot.
      this.resync();
    };

    socket.onmessage = (event: MessageEvent) => { if (this.wantOpen && this.ws === socket) this.handleRaw(event); };

    socket.onclose = (event: CloseEvent) => {
      if (this.ws !== socket) return;
      this.flusher.cancel(); this.buffer?.clear();
      this.stopHeartbeat();
      if (this.stableTimer) clearTimeout(this.stableTimer);
      this.stableTimer = null;
      this.ws = null;
      this.setStatus('closed');
      if (!this.wantOpen) return;
      let should = true; try { should = this.opts.shouldReconnect ? this.opts.shouldReconnect(event) : true; } catch { this.report('RECONNECT_POLICY_FAILED'); should = false; }
      if (should) this.scheduleReconnect();
    };

    socket.onerror = () => {
      if (this.ws !== socket) return;
      // Let onclose drive reconnect; just surface it.
      this.report('websocket error');
      try {
        socket.close();
      } catch {
        /* noop */
      }
    };
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer || !this.wantOpen) return;
    if (this.reconnectAttempt >= this.opts.maxReconnectAttempts) {
      this.setStatus('circuit-open');
      this.report('Connection lost, refresh the page');
      return;
    }
    const delay = backoffWithJitter(
      this.reconnectAttempt,
      this.opts.backoffBaseMs,
      this.opts.backoffCapMs,
      noopRng,
    );
    this.reconnectAttempt++;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.openSocket();
    }, delay);
  }

  // ── Heartbeat + dead-connection detection ───────────────────────────────────

  private startHeartbeat(): void {
    this.stopHeartbeat();
    const ping = this.opts.pingMessage ?? (() => ({ type: 'ping' }));
    // pongTimeoutMs <= 0 disables the pong watchdog (for streams whose server
    // does not reply to ping and which can be legitimately silent between
    // events — e.g. candles between bar closes). Pings are still sent as a
    // NAT/proxy keepalive; dead-conn is then handled by TCP close + reconnect.
    const pongWatchdog = this.opts.pongTimeoutMs > 0;
    this.pingTimer = setInterval(() => {
      if (this.ws?.readyState !== WebSocket.OPEN) return;
      this.send(ping());
      if (!pongWatchdog) return;
      this.awaitingPong = true;
      if (this.pongTimer) clearTimeout(this.pongTimer);
      this.pongTimer = setTimeout(() => {
        if (this.awaitingPong) {
          // Server went silent — force a reconnect rather than hang.
          this.report('heartbeat timeout — reconnecting');
          try {
            this.ws?.close();
          } catch {
            /* noop */
          }
        }
      }, this.opts.pongTimeoutMs);
    }, this.opts.pingIntervalMs);
  }

  private stopHeartbeat(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
    this.awaitingPong = false;
  }

  // ── Inbound message handling ────────────────────────────────────────────────

  private handleRaw(event: MessageEvent): void {
    // Any inbound traffic proves the connection is alive.
    this.awaitingPong = false;
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }

    let message: TMessage;
    try {
      message = this.opts.parse ? this.opts.parse(event.data) : (JSON.parse(event.data) as TMessage);
    } catch {
      this.report('INVALID_STREAM_ENVELOPE');
      return;
    }

    const isPong = this.opts.isPong
      ? this.opts.isPong(message)
      : (message as { type?: string })?.type === 'pong';
    if (isPong) return;

    // Sequence/gap detection -> resync.
    if (this.opts.getSequence) {
      const seq = this.opts.getSequence(message);
      if (typeof seq === 'number') {
        if (this.lastSeq !== null && seq > this.lastSeq + 1) {
          this.opts.onGap?.(this.lastSeq + 1, seq);
          this.resync();
          return;
        }
        if (this.lastSeq !== null && seq <= this.lastSeq) return;
        // Only advance on forward progress; ignore stale/replayed frames.
        if (this.lastSeq === null || seq > this.lastSeq) this.lastSeq = seq;
      }
    }

    if (this.buffer && this.opts.coalesce && (this.opts.coalesce.replaceable?.(message) ?? true)) {
      if (this.buffer.set(this.opts.coalesce.key(message), message) !== null) { this.buffer.clear(); this.flusher.cancel(); this.report('UI_STATE_BUFFER_OVERFLOW'); this.resync(); return; }
      this.flusher.schedule(() => this.flushBuffer());
    } else {
      try { this.opts.onMessage(message, this.ctx); }
      catch { this.report('CONSUMER_CALLBACK_FAILED'); }
    }
  }

  private flushBuffer(): void {
    if (!this.buffer) return;
    const socket = this.ws;
    const batch = this.buffer.drain();
    for (const msg of batch) {
      if (!this.wantOpen || this.ws !== socket) return;
      try { this.opts.onMessage(msg, this.ctx); }
      catch { this.report('CONSUMER_CALLBACK_FAILED'); }
    }
  }

  // ── Page visibility (T05.3) ─────────────────────────────────────────────────

  private bindVisibility(): void {
    if (this.visibilityBound) return;
    if (typeof document === 'undefined') return;
    if (this.opts.pauseWhenHidden === false) return;
    document.addEventListener('visibilitychange', this.onVisibilityChange);
    this.visibilityBound = true;
  }

  private unbindVisibility(): void {
    if (!this.visibilityBound || typeof document === 'undefined') return;
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
    this.visibilityBound = false;
  }

  private onVisibilityChange = (): void => {
    if (typeof document === 'undefined') return;
    if (document.visibilityState === 'visible' && this.wantOpen) {
      // Coalesced ticks accumulated while hidden are stale — drop and resync.
      this.buffer?.clear();
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.resync();
      } else {
        this.openSocket();
      }
    }
  };

  // ── Misc ────────────────────────────────────────────────────────────────────

  private clearTimers(): void {
    if (this.stableTimer) clearTimeout(this.stableTimer);
    this.stableTimer = null;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.stopHeartbeat();
  }

  private report(error: string): void { try { this.opts.onError?.(error); } catch {} }
  private resync(): void { try { this.opts.onResync?.(this.ctx); } catch { this.report('RESYNC_CALLBACK_FAILED'); this.ws?.close(); } }

  private setStatus(status: WsStatus): void {
    if (this.status === status) return;
    this.status = status;
    try { this.opts.onStatusChange?.(status); } catch { this.report('STATUS_CALLBACK_FAILED'); }
  }
}
