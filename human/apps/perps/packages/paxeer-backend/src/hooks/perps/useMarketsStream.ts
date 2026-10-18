"use client";
import { useEffect, useState } from "react";
import { activePoll, type CancelTask } from "../../core/net/realtime";
import { fetchJsonWithPolicy } from "../../core/net/fetch";
import { feedFundingDeadline } from "../../core/feedFunding";
import { MARKET_DATA_URL } from "../../core/feeds";

// The deployed mint allowlist currently covers per-symbol sockets. Aggregate reads
// use the SDK's authenticated REST contract at the documented five-second cadence.
export interface MarketSnapshot {
  symbol: string;
  symbol_full: string;
  price: number;
  index_price?: number;
  next_funding_time?: number;
  /** 24h % change as a percentage number (e.g. 1.23 means +1.23%). */
  change_24h: number;
  volume_24h: number;
  open_interest: number;
  /** Realised 8h funding — a ratio (e.g. 0.0001 = 0.01%). */
  funding_8h: number;
  /** Estimated next funding rate — ratio. */
  est_funding_rate: number;
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

const state = {
  markets: [] as MarketSnapshot[],
  marketsBySymbol: new Map<string, MarketSnapshot>(),
  totals: null as MarketsTotals | null,
  connection: "idle" as ConnectionState,
  subsMarkets: new Set<(rows: MarketSnapshot[]) => void>(),
  subsTotals: new Set<(totals: MarketsTotals | null) => void>(),
  subsConnection: new Set<(connection: ConnectionState) => void>(),
};
let stopPoll: CancelTask | null = null;
let generation = 0;
function setConnection(value: ConnectionState) {
  if (state.connection === value) return;
  state.connection = value;
  state.subsConnection.forEach((cb) => cb(value));
}
function publishMarkets(rows: MarketSnapshot[]) {
  state.markets = rows;
  state.marketsBySymbol = new Map(rows.map((row) => [row.symbol, row]));
  state.subsMarkets.forEach((cb) => cb(rows));
}
function ensureConnected(): void {
  if (typeof window === "undefined" || stopPoll) return;
  const current = ++generation;
  setConnection("connecting");
  stopPoll = activePoll(async () => {
    try {
      const body = await fetchJsonWithPolicy<{
        markets: unknown[];
        ts: number;
      }>(`${MARKET_DATA_URL}/markets`);
      if (current !== generation) return;
      const rows = body.markets
        .map(normalizeMarket)
        .filter((row): row is MarketSnapshot => row !== null);
      publishMarkets(rows);
      setConnection("open");
      if (state.subsTotals.size) {
        const aggregate = await fetchJsonWithPolicy<{
          totals: unknown;
          ts: number;
        }>(`${MARKET_DATA_URL}/totals`);
        if (current !== generation) return;
        state.totals = normalizeTotals(aggregate.totals, aggregate.ts);
        state.subsTotals.forEach((cb) => cb(state.totals));
      }
    } catch (error) {
      if (current === generation) {
        publishMarkets([]);
        state.totals = null;
        state.subsTotals.forEach((cb) => cb(null));
        setConnection("closed");
      }
      throw error;
    }
  }, 5000);
}
function teardownIfIdle(): void {
  if (
    state.subsMarkets.size ||
    state.subsTotals.size ||
    state.subsConnection.size
  )
    return;
  generation++;
  stopPoll?.();
  stopPoll = null;
  state.markets = [];
  state.marketsBySymbol.clear();
  state.totals = null;
  setConnection("idle");
}

function num(v: unknown): number {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

function normalizeMarket(raw: unknown): MarketSnapshot | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const symbol = typeof r.symbol === "string" ? r.symbol : null;
  if (
    !symbol ||
    num(r.price) <= 0 ||
    num(r.price_updated_at) <= 0 ||
    Date.now() - num(r.price_updated_at) > 30_000
  )
    return null;
  return {
    symbol,
    symbol_full: typeof r.symbol_full === "string" ? r.symbol_full : symbol,
    price: num(r.price),
    index_price: num(r.perp_index_price ?? r.index_price) || undefined,
    next_funding_time:
      num(r.next_funding_time) ||
      feedFundingDeadline(num(r.last_updated), num(r.perp_next_funding_ms)),
    change_24h: num(r.change_24h),
    volume_24h: num(r.volume_24h),
    open_interest: num(r.open_interest),
    funding_8h: num(r.funding_8h),
    est_funding_rate: num(r.est_funding_rate),
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
    if (!enabled) {
      setRows([]);
      return;
    }
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
export function useMarketSnapshot(
  symbol: string | undefined | null,
): MarketSnapshot | undefined {
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
