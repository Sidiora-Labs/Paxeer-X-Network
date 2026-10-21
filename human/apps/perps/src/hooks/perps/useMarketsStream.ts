"use client";
import { sequenceGuard, isPageActive, scheduleActive, watchSocket, notifyStreamResync, type CancelTask } from "@/core/net/realtime";

import { FEED_ORIGIN } from "@/core/feeds";
import { useEffect, useState } from "react";
import { parseFundingRateHourly } from "@/core/perps/fundingRate";

/* ══════════════════════════════════════════════════════════════════════
   Dedicated markets aggregate stream (`/ws/markets`).

   This feed exposes two read-only topics:

     · `markets@all`     → every market snapshot as an array, frames
                           emitted whenever any field changes.
     · `markets@totals`  → aggregate protocol stats (24h/7d/30d vol, OI,
                           total trades).

   One multiplexed WebSocket per browser tab — N React subscribers share
   the same socket. Reconnect uses exponential backoff, and subscription
   frames are replayed on every (re)open. Mirrors the pattern used by
   `useSidPaxMarketStream.ts`.
   ══════════════════════════════════════════════════════════════════════ */


// The gateway permits the Hyperpax browser origin; no client secret is shipped.

export const MARKETS_WS_URL = `${FEED_ORIGIN.replace(/^http/, "ws")}/ws/markets`;

// ── Public types ──────────────────────────────────────────────────────

/**
 * One row from the `markets@all` topic. `last_updated` is server wall-clock
 * epoch milliseconds. Numeric fields are already decoded to JS numbers.
 */
export interface MarketSnapshot {
  symbol: string;
  symbol_full: string;
  price: number;
  /** 24h % change as a percentage number (e.g. 1.23 means +1.23%). */
  change_24h: number;
  volume_24h: number;
  open_interest: number;
  /** Realised 8h funding — a ratio (e.g. 0.0001 = 0.01%). */
  funding_8h: number;
  /** Last settled hourly funding rate at 18 decimals (1e18 = 1.0); null before the first settlement. */
  last_funding_rate_hourly: bigint | null;
  volume_7d: number;
  volume_30d: number;
  trades_count: number;
  last_updated: number;
  price_updated_at: number;
  price_24h: number;
  high_24h: number;
  low_24h: number;
  amount_24h: number;
  reported_volume_24h: number;
  reported_open_interest: number;
}

/**
 * Single payload from the `markets@totals` topic. Frames emit on any
 * aggregate field change, so callers can treat this as a live snapshot.
 */
export interface MarketsTotals {
  total_trades: number;
  volume_24h: number;
  volume_7d: number;
  volume_30d: number;
  total_oi: number;
  /** Epoch ms of the most recent frame. */
  ts: number;
}

export type ConnectionState = "idle" | "connecting" | "open" | "closed";

// ── Internal singleton state ──────────────────────────────────────────

interface StreamState {
  ws: WebSocket | null;
  markets: MarketSnapshot[];
  marketsBySymbol: Map<string, MarketSnapshot>;
  totals: MarketsTotals | null;
  connection: ConnectionState;

  subsMarkets: Set<(m: MarketSnapshot[]) => void>;
  subsTotals: Set<(t: MarketsTotals | null) => void>;
  subsConnection: Set<(c: ConnectionState) => void>;

  reconnectTimer: CancelTask | null;
  reconnectDelay: number;
  pingTimer: ReturnType<typeof setInterval> | null;
  /** Monotonic counter — bumped when either topic is (re)subscribed so we
   *  can detect "we have sent the subscribe frame on this socket". */
  socketEpoch: number;
  /** Whether we've already sent the subscribe frames on the CURRENT open
   *  socket. Reset to `false` on each reconnect. */
  sentSubscribes: boolean;
}

const INITIAL_RECONNECT_DELAY = 500;
const MAX_RECONNECT_DELAY = 60_000;
const PING_INTERVAL = 25_000;

const state: StreamState = {
  ws: null,
  markets: [],
  marketsBySymbol: new Map(),
  totals: null,
  connection: "idle",
  subsMarkets: new Set(),
  subsTotals: new Set(),
  subsConnection: new Set(),
  reconnectTimer: null,
  reconnectDelay: INITIAL_RECONNECT_DELAY,
  pingTimer: null,
  socketEpoch: 0,
  sentSubscribes: false,
};

function setConnection(next: ConnectionState) {
  if (next === "closed" || next === "idle") { state.markets = []; state.marketsBySymbol.clear(); state.totals = null; state.subsTotals.forEach(cb => cb(null)); state.subsMarkets.forEach(cb => cb([])); }
  if (state.connection === next) return;
  state.connection = next;
  state.subsConnection.forEach((cb) => {
    try { cb(next); } catch { /* ignore */ }
  });
}

function ensureConnected(): void {
  if (typeof window === "undefined") return;
  if (state.reconnectTimer) return;
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) {
    // Socket is already live — nothing to do. Subscribe frames were sent
    // either on open or on the previous subscribe path.
    return;
  }
  connect();
}

function connect(): void {
  if (!isPageActive()) { scheduleReconnect(); return; }
  if (typeof window === "undefined") return;
  setConnection("connecting");
  state.socketEpoch += 1;
  state.sentSubscribes = false;

  let ws: WebSocket;
  try {
    ws = new WebSocket(MARKETS_WS_URL);
  } catch {
    scheduleReconnect();
    return;
  }
  state.ws = ws;
  watchSocket(ws, () => { state.reconnectDelay = INITIAL_RECONNECT_DELAY; }, undefined, true);

  ws.addEventListener("open", () => {
    notifyStreamResync(MARKETS_WS_URL);
    setConnection("open");
    sendSubscribes();
    if (!state.pingTimer) {
      state.pingTimer = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          try { ws.send(JSON.stringify({ event: "ping" })); } catch { /* ignore */ }
        }
      }, PING_INTERVAL);
    }
  });

  const acceptSequence = sequenceGuard();
  ws.addEventListener("message", (ev) => {
    let msg: unknown;
    try { msg = JSON.parse(ev.data); } catch { return; }
    if (!acceptSequence(msg)) { ws.close(4000, "sequence-gap"); return; }
    if (!msg || typeof msg !== "object") return;

    const topic = (msg as { topic?: string }).topic;
    const data = (msg as { data?: unknown }).data;
    const tsRaw = (msg as { ts?: number }).ts;
    const ts = typeof tsRaw === "number" ? tsRaw : Date.now();

    if (topic === "markets@all" && Array.isArray(data)) {
      const rows = data
        .map(normalizeMarket)
        .filter((m): m is MarketSnapshot => m !== null);
      state.markets = rows;
      state.marketsBySymbol = new Map(rows.map((r) => [r.symbol, r]));
      state.subsMarkets.forEach((cb) => {
        try { cb(rows); } catch { /* ignore */ }
      });
      return;
    }

    if (topic === "markets@totals" && data && typeof data === "object") {
      const t = normalizeTotals(data, ts);
      if (t) {
        state.totals = t;
        state.subsTotals.forEach((cb) => {
          try { cb(t); } catch { /* ignore */ }
        });
      }
      return;
    }

    // Control frames: welcome / subscribed / unsubscribed / pong / error
    // are informational only. Explicitly ignore.
  });

  const onClosed = () => {
    if (state.pingTimer) {
      clearInterval(state.pingTimer);
      state.pingTimer = null;
    }
    if (state.ws === ws) state.ws = null;
    state.sentSubscribes = false;
    setConnection("closed");
    scheduleReconnect();
  };
  ws.addEventListener("close", onClosed);
  ws.addEventListener("error", () => {
    // `close` will fire next — handle reconnect there so we don't
    // double-schedule.
  });
}

function sendSubscribes(): void {
  const ws = state.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  if (state.sentSubscribes) return;
  try {
    ws.send(JSON.stringify({ event: "subscribe", topic: "markets@all" }));
    ws.send(JSON.stringify({ event: "subscribe", topic: "markets@totals" }));
    state.sentSubscribes = true;
  } catch {
    // Socket closed mid-send — the reconnect handler will retry on the
    // next `open`.
  }
}

function scheduleReconnect(): void {
  if (state.reconnectTimer) return;
  // Only reconnect if at least one consumer is still listening.
  if (
    state.subsMarkets.size === 0 &&
    state.subsTotals.size === 0 &&
    state.subsConnection.size === 0
  ) {
    return;
  }
  const delay = state.reconnectDelay;
  state.reconnectTimer = scheduleActive(() => {
    state.reconnectTimer = null;
    state.reconnectDelay = Math.min(MAX_RECONNECT_DELAY, state.reconnectDelay * 2);
    connect();
  }, delay, MARKETS_WS_URL);
}

function teardownIfIdle(): void {
  if (
    state.subsMarkets.size > 0 ||
    state.subsTotals.size > 0 ||
    state.subsConnection.size > 0
  ) {
    return;
  }
  if (state.reconnectTimer) {
    state.reconnectTimer();
    state.reconnectTimer = null;
  }
  if (state.pingTimer) {
    clearInterval(state.pingTimer);
    state.pingTimer = null;
  }
  const ws = state.ws;
  state.ws = null;
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    try { ws.close(1000, "no-subscribers"); } catch { /* ignore */ }
  }
  state.reconnectDelay = INITIAL_RECONNECT_DELAY;
  setConnection("idle");
}

// ── Normalisers ───────────────────────────────────────────────────────

function num(v: unknown): number {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

/**
 * The venue's `last_funding_rate_hourly` at 18 decimals. Absent (null or
 * missing) is null, never zero; anything present that is not an exact
 * decimal string throws.
 */
export function parseVenueFundingHourly(v: unknown): bigint | null {
  if (v === null || v === undefined) return null;
  return parseFundingRateHourly(v as string);
}

export function normalizeMarket(raw: unknown): MarketSnapshot | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const symbol = typeof r.symbol === "string" ? r.symbol : null;
  if (!symbol) return null;
  return {
    symbol,
    symbol_full: typeof r.symbol_full === "string" ? r.symbol_full : symbol,
    price: num(r.price),
    change_24h: num(r.change_24h),
    volume_24h: num(r.volume_24h),
    open_interest: num(r.open_interest),
    funding_8h: num(r.funding_8h),
    last_funding_rate_hourly: parseVenueFundingHourly(r.last_funding_rate_hourly),
    volume_7d: num(r.volume_7d),
    volume_30d: num(r.volume_30d),
    trades_count: num(r.trades_count),
    last_updated: num(r.last_updated),
    price_updated_at: num(r.price_updated_at),
    price_24h: num(r.price_24h),
    high_24h: num(r.high_24h),
    low_24h: num(r.low_24h),
    amount_24h: num(r.amount_24h),
    reported_volume_24h: num(r.reported_volume_24h ?? r.volume_24h),
    reported_open_interest: num(r.reported_open_interest ?? r.open_interest),
  };
}

function normalizeTotals(raw: unknown, ts: number): MarketsTotals | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  return {
    total_trades: num(r.total_trades),
    volume_24h: num(r.volume_24h),
    volume_7d: num(r.volume_7d),
    volume_30d: num(r.volume_30d),
    total_oi: num(r.total_oi),
    ts,
  };
}

// ── React hooks ───────────────────────────────────────────────────────

/**
 * Subscribe to the live per-market snapshot array (`markets@all`).
 * Returns the most recent snapshot synchronously (may be empty on first
 * render before the first frame lands).
 */
export function useMarketsSnapshot(enabled = true): MarketSnapshot[] {
  const [rows, setRows] = useState<MarketSnapshot[]>(state.markets);

  useEffect(() => {
    if (!enabled) { setRows([]); return; }
    state.subsMarkets.add(setRows);
    ensureConnected();
    // Sync any frames that arrived between module-eval and subscribe.
    if (rows !== state.markets) setRows(state.markets);
    return () => {
      state.subsMarkets.delete(setRows);
      // Defer tear-down so a remount within the same tick doesn't flap
      // the socket.
      queueMicrotask(teardownIfIdle);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled]);

  return rows;
}

/**
 * Look up one symbol's snapshot from the shared cache. Re-renders only
 * when that symbol's frame changes. Returns `undefined` until the first
 * `markets@all` frame arrives (or if the symbol isn't in the feed).
 */
export function useMarketSnapshot(symbol: string | undefined | null): MarketSnapshot | undefined {
  const key = symbol ? symbol.toUpperCase() : "";
  const [snap, setSnap] = useState<MarketSnapshot | undefined>(() =>
    key ? state.marketsBySymbol.get(key) : undefined,
  );

  useEffect(() => {
    if (!key) {
      setSnap(undefined);
      return;
    }
    queueMicrotask(ensureConnected);
    setSnap(state.marketsBySymbol.get(key));
    const onAll = (rows: MarketSnapshot[]) => {
      // O(1) thanks to the symbol index we rebuilt in `message`.
      const next = state.marketsBySymbol.get(key);
      setSnap(next);
      // `rows` param is required by the subscriber signature but we index
      // through the shared map for speed.
      void rows;
    };
    state.subsMarkets.add(onAll);
    return () => {
      state.subsMarkets.delete(onAll);
      queueMicrotask(teardownIfIdle);
    };
  }, [key]);

  return snap;
}

/**
 * Subscribe to the aggregate totals feed (`markets@totals`). Returns
 * `null` until the first frame arrives.
 */
export function useMarketsTotals(): MarketsTotals | null {
  const [totals, setTotals] = useState<MarketsTotals | null>(state.totals);

  useEffect(() => {
    queueMicrotask(ensureConnected);
    if (totals !== state.totals) setTotals(state.totals);
    state.subsTotals.add(setTotals);
    return () => {
      state.subsTotals.delete(setTotals);
      queueMicrotask(teardownIfIdle);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return totals;
}

/**
 * Observe the connection state for UI affordances (live dot, retry prompt).
 */
export function useMarketsStreamConnection(): ConnectionState {
  const [conn, setConn] = useState<ConnectionState>(state.connection);

  useEffect(() => {
    queueMicrotask(ensureConnected);
    setConn(state.connection);
    state.subsConnection.add(setConn);
    return () => {
      state.subsConnection.delete(setConn);
      queueMicrotask(teardownIfIdle);
    };
  }, []);

  return conn;
}
