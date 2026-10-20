"use client";
import { isPageActive, scheduleActive, sequenceGuard, watchSocket, type CancelTask } from "@/core/net/realtime";
import { FEED_ORIGIN } from "@/core/feeds";

import { useEffect, useState } from "react";

/* ══════════════════════════════════════════════════════════════════════
   Dedicated SPOT market stream — mirrors the perps
   `useSidPaxMarketStream` singleton pattern but subscribes to the
   non-prefixed SPOT topics:

     `<SYMBOL>@orderbook`   (not `@perp_orderbook`)
     `<SYMBOL>@trade`       (not `@perp_trade`)
     `<SYMBOL>@stats`       (not `@perp_stats`)

   Klines (`@kline_*`) are shared and don't carry a perp_ prefix on
   either side, so the perps kline infra can be reused as-is.

   The WS proxy is the same `data.feed.chainflowtrading.com` host — each
   symbol gets its own `/ws/<sym>` path. The proxy routes to the
   correct upstream internally.
   ══════════════════════════════════════════════════════════════════════ */

const MARKET_WS_ORIGIN = FEED_ORIGIN.replace(/^http/, "ws");

/** Symbols that have a SPOT stream available. */
const SUPPORTED_SPOT_SYMBOLS = new Set(["SID", "BNB", "ETH", "PAX"]);


// The gateway permits the Hyperpax browser origin; no client secret is shipped.

export function buildSymbolWsUrl(symbol: string): string {
  return `${MARKET_WS_ORIGIN}/ws/${symbol.toLowerCase()}`;
}

/** True if the symbol has a dedicated SPOT market stream. */
export function hasSpotMarketStream(symbol: string | undefined | null): boolean {
  if (!symbol) return false;
  return SUPPORTED_SPOT_SYMBOLS.has(symbol.toUpperCase());
}

// ── Public types ──────────────────────────────────────────────────────
export interface SpotBookLevel {
  price: number;
  size: number;
}

export interface SpotOrderbook {
  bids: SpotBookLevel[];
  asks: SpotBookLevel[];
  ts: number;
}

export interface SpotTrade {
  side: "BUY" | "SELL";
  price: number;
  /** Size in base-unit floats (not integer contracts like PERPS). */
  size: number;
  trade_time: number;
  trade_id?: string;
}

/**
 * Aggregated market stats from the `<SYMBOL>@stats` topic.
 * SPOT stats are simpler than perps — no mark/index/funding fields.
 */
export interface SpotStats {
  symbol: string;
  last_price: number;
  high_24h: number;
  low_24h: number;
  volume_24h: number;
  change_24h: number;
}

// ── Internal registry ─────────────────────────────────────────────────
interface SpotSymbolStream {
  url: string;
  orderbook: SpotOrderbook | null;
  trades: SpotTrade[];
  subsOrderbook: Set<(ob: SpotOrderbook | null) => void>;
  subsTrades: Set<(tr: SpotTrade[]) => void>;
  statsData: SpotStats | null;
  subsStats: Set<(s: SpotStats | null) => void>;
  ws: WebSocket | null;
  reconnectTimer: CancelTask | null;
  lingerTimer: ReturnType<typeof setTimeout> | null;
  disposed: boolean;
  reconnectDelay: number;
  pingTimer: ReturnType<typeof setInterval> | null;
}

const TRADE_CAP = 100;
const MAX_RECONNECT_DELAY = 60_000;
const INITIAL_RECONNECT_DELAY = 500;
const PING_INTERVAL = 25_000;

const spotStreams = new Map<string, SpotSymbolStream>();

function ensureSpotStream(symbol: string): SpotSymbolStream | null {
  if (typeof window === "undefined") return null;

  const key = symbol.toUpperCase();
  if (!SUPPORTED_SPOT_SYMBOLS.has(key)) return null;
  const url = buildSymbolWsUrl(key);

  let stream = spotStreams.get(key);
  if (stream) { if (stream.lingerTimer) clearTimeout(stream.lingerTimer); stream.lingerTimer = null; return stream; }

  stream = {
    url,
    orderbook: null,
    trades: [],
    subsOrderbook: new Set(),
    subsTrades: new Set(),
    statsData: null,
    subsStats: new Set(),
    ws: null,
    reconnectTimer: null,
    lingerTimer: null,
    disposed: false,
    reconnectDelay: INITIAL_RECONNECT_DELAY,
    pingTimer: null,
  };
  spotStreams.set(key, stream);
  queueMicrotask(() => connectSpot(key, stream!));
  return stream;
}

function connectSpot(symbol: string, s: SpotSymbolStream) {
  if (s.disposed) return;
  if (!isPageActive()) { scheduleSpotReconnect(symbol, s); return; }
  // SPOT topics — NO `perp_` prefix
  const orderbookTopic = `${symbol}@orderbook`;
  const tradeTopic = `${symbol}@trade`;
  const statsTopic = `${symbol}@stats`;

  let ws: WebSocket;
  try {
    ws = new WebSocket(s.url);
  } catch {
    scheduleSpotReconnect(symbol, s);
    return;
  }
  s.ws = ws;
  watchSocket(ws, () => { s.reconnectDelay = INITIAL_RECONNECT_DELAY; }, undefined, true);

  ws.addEventListener("open", () => {
    try {
      ws.send(JSON.stringify({ event: "subscribe", topic: orderbookTopic }));
      ws.send(JSON.stringify({ event: "subscribe", topic: tradeTopic }));
      ws.send(JSON.stringify({ event: "subscribe", topic: statsTopic }));
    } catch {
      /* socket may have closed between open and first send */
    }
    if (!s.pingTimer) {
      s.pingTimer = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          try {
            ws.send(JSON.stringify({ event: "ping" }));
          } catch {
            /* ignore */
          }
        }
      }, PING_INTERVAL);
    }
  });

  const acceptSequence = sequenceGuard();
  ws.addEventListener("message", (ev) => {
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(ev.data as string) as Record<string, unknown>;
    } catch {
      return;
    }
    if (!acceptSequence(msg)) { ws.close(4000, "sequence-gap"); return; }

    // Order-book frame
    if (msg.topic === orderbookTopic && msg.data) {
      const data = msg.data as Record<string, unknown>;
      const asks = normalizeLevels(data.asks).sort((a, b) => a.price - b.price);
      const bids = normalizeLevels(data.bids).sort((a, b) => b.price - a.price);
      const ts = typeof msg.ts === "number" ? msg.ts : Date.now();
      s.orderbook = { asks, bids, ts };
      s.subsOrderbook.forEach((cb) => cb(s.orderbook));
      return;
    }

    // Trade frame — snapshot or live
    if (msg.topic === tradeTopic && msg.data) {
      const data = msg.data as Record<string, unknown>;
      if (msg.snapshot && Array.isArray(data.trades)) {
        s.trades = (data.trades as unknown[]).slice(0, TRADE_CAP).map(normalizeSpotTrade);
        s.subsTrades.forEach((cb) => cb(s.trades));
      } else if (data.trade) {
        const t = normalizeSpotTrade(data.trade);
        s.trades = [t, ...s.trades].slice(0, TRADE_CAP);
        s.subsTrades.forEach((cb) => cb(s.trades));
      } else if (data.side && data.price != null) {
        const t = normalizeSpotTrade(data);
        s.trades = [t, ...s.trades].slice(0, TRADE_CAP);
        s.subsTrades.forEach((cb) => cb(s.trades));
      }
      return;
    }

    // Stats frame — SPOT stats shape (simpler than perps)
    if (msg.topic === statsTopic && msg.data) {
      const d = msg.data as Record<string, unknown>;
      s.statsData = {
        symbol: String(d.symbol ?? symbol),
        last_price: Number(d.last_price ?? d.price ?? 0),
        high_24h: Number(d.high_24h ?? d["24h_high"] ?? 0),
        low_24h: Number(d.low_24h ?? d["24h_low"] ?? 0),
        volume_24h: Number(d.volume_24h ?? d["24h_volume"] ?? 0),
        change_24h: Number(d.change_24h ?? d["24h_change"] ?? 0),
      };
      s.subsStats.forEach((cb) => cb(s.statsData));
      return;
    }
  });

  const onDisconnect = () => {
    if (s.disposed) return;
    s.orderbook = null; s.trades = []; s.statsData = null;
    s.subsOrderbook.forEach(cb => cb(null)); s.subsTrades.forEach(cb => cb([])); s.subsStats.forEach(cb => cb(null));
    if (s.pingTimer) {
      clearInterval(s.pingTimer);
      s.pingTimer = null;
    }
    if (s.ws === ws) s.ws = null;
    scheduleSpotReconnect(symbol, s);
  };
  ws.addEventListener("close", onDisconnect);
  ws.addEventListener("error", () => {
    // `close` will also fire; leave reconnection to it.
  });
}

function scheduleSpotReconnect(symbol: string, s: SpotSymbolStream) {
  if (s.reconnectTimer || s.disposed) return;
  const delay = s.reconnectDelay;
  s.reconnectTimer = scheduleActive(() => {
    s.reconnectTimer = null;
    s.reconnectDelay = Math.min(MAX_RECONNECT_DELAY, s.reconnectDelay * 2);
    connectSpot(symbol, s);
  }, delay, s.url);
}

function releaseIfIdle(symbol: string, s: SpotSymbolStream) {
  const listeners = () => s.subsOrderbook.size + s.subsTrades.size + s.subsStats.size;
  if (listeners() || s.lingerTimer) return;
  s.lingerTimer = setTimeout(() => {
    s.lingerTimer = null;
    if (listeners()) return;
    s.disposed = true; s.reconnectTimer?.();
    if (s.pingTimer) clearInterval(s.pingTimer);
    s.ws?.close(1000, "no-subscribers"); s.ws = null;
    spotStreams.delete(symbol.toUpperCase());
  }, 15000);
}

function normalizeLevels(raw: unknown): SpotBookLevel[] {
  if (!Array.isArray(raw)) return [];
  const out: SpotBookLevel[] = [];
  for (const r of raw) {
    const price = Array.isArray(r)
      ? Number(r[0])
      : Number((r as Record<string, unknown>)?.price);
    const size = Array.isArray(r)
      ? Number(r[1])
      : Number(
          (r as Record<string, unknown>)?.size ??
            (r as Record<string, unknown>)?.quantity,
        );
    if (Number.isFinite(price) && Number.isFinite(size)) {
      out.push({ price, size });
    }
  }
  return out;
}

function normalizeSpotTrade(t: unknown): SpotTrade {
  const obj = t as Record<string, unknown>;
  const side = String(obj?.side ?? "").toUpperCase() === "BUY" ? "BUY" : "SELL";
  const tradeTimeRaw = obj?.trade_time ?? obj?.ts ?? Date.now();
  const tradeTime = Number(tradeTimeRaw);
  const size = Number(obj?.size ?? obj?.quantity ?? 0);
  return {
    side,
    price: Number(obj?.price),
    size,
    trade_id: (obj?.trade_id ?? obj?.id ?? undefined) as string | undefined,
    trade_time: Number.isFinite(tradeTime) ? tradeTime : Date.now(),
  };
}

/** SPOT order-book listener: the cached book, each frame, and null while the socket is down. */
export function subscribeSpotOrderbook(symbol: string, cb: (book: SpotOrderbook | null) => void): () => void {
  const s = ensureSpotStream(symbol);
  if (!s) return () => {};
  s.subsOrderbook.add(cb);
  if (s.orderbook) cb(s.orderbook);
  return () => {
    s.subsOrderbook.delete(cb);
    releaseIfIdle(symbol, s);
  };
}

// ── React hooks ───────────────────────────────────────────────────────

/** Subscribe to the SPOT order-book feed. */
export function useSpotOrderbook(symbol: string | undefined): SpotOrderbook | null {
  const [book, setBook] = useState<SpotOrderbook | null>(null);

  useEffect(() => {
    if (!symbol) {
      setBook(null);
      return;
    }
    const s = ensureSpotStream(symbol);
    if (!s) {
      setBook(null);
      return;
    }
    setBook(s.orderbook);
    return subscribeSpotOrderbook(symbol, setBook);
  }, [symbol]);

  return book;
}

/** Subscribe to the SPOT trade tape. Newest-first, capped at TRADE_CAP rows. */
export function useSpotTrades(symbol: string | undefined): SpotTrade[] {
  const [trades, setTrades] = useState<SpotTrade[]>([]);

  useEffect(() => {
    if (!symbol) {
      setTrades([]);
      return;
    }
    const s = ensureSpotStream(symbol);
    if (!s) {
      setTrades([]);
      return;
    }
    setTrades(s.trades);
    s.subsTrades.add(setTrades);
    return () => {
      s.subsTrades.delete(setTrades);
      releaseIfIdle(symbol, s);
    };
  }, [symbol]);

  return trades;
}

/** Subscribe to the SPOT `@stats` feed. */
export function useSpotStats(symbol: string | undefined): SpotStats | null {
  const [stats, setStats] = useState<SpotStats | null>(null);

  useEffect(() => {
    if (!symbol) {
      setStats(null);
      return;
    }
    const s = ensureSpotStream(symbol);
    if (!s) {
      setStats(null);
      return;
    }
    setStats(s.statsData);
    s.subsStats.add(setStats);
    return () => {
      s.subsStats.delete(setStats);
      releaseIfIdle(symbol, s);
    };
  }, [symbol]);

  return stats;
}
