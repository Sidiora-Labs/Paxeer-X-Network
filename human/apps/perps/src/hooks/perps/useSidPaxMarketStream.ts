"use client";
import { sequenceGuard, isPageActive, scheduleActive, watchSocket, type CancelTask } from "@/core/net/realtime";

import { FEED_ORIGIN } from "@/core/feeds";
import { fetchWithPolicy } from "@/core/net/fetch";
import { isVenueSymbol, parseVenueBook, RESYNC_DEPTH } from "@/core/net/venueStream";
import { gatewayUrls } from "@/core/network/gateway";
import { normalizeLevels } from "@/hooks/spot/useSpotMarketStream";
import { useEffect, useState } from "react";

/* ══════════════════════════════════════════════════════════════════════
   Dedicated market stream for symbols whose order book + trade tape +
   candles are served by per-symbol Railway WS boxes (one host per
   symbol — same hosts that also serve UDF REST `/history` etc.).

   These servers speak a simple topic-based protocol:

     client → server :
        { event: "subscribe",   topic: "SID@orderbook" }
        { event: "subscribe",   topic: "SID@trade" }
        { event: "subscribe",   topic: "SID@kline_5m", limit: 1000 }
        { event: "unsubscribe", topic: "SID@kline_5m" }
        { event: "ping" }

     server → client :
        { topic: "SID@orderbook", data: { asks: [[p,s], ...],
                                          bids: [[p,s], ...],
                                          symbol: "SID" },
          ts?: number }
        { topic: "SID@trade",     snapshot: true,
          data:  { trades: [ { side, price, size, trade_time }, ... ] } }
        { topic: "SID@trade",
          data:  { trade: { side, price, size, trade_time } } }   // new shape
        { topic: "SID@trade",
          data:  { side: "BUY"|"SELL", price, size, trade_time } } // legacy shape
        { topic: "SID@kline_5m",  snapshot: true,
          data:  { symbol, interval, candles: [[t,o,h,l,c,v,closed], ...] } }
        { topic: "SID@kline_5m",
          data:  { symbol, interval, candle: [t,o,h,l,c,v,closed] } }
        { event: "welcome" | "subscribed" | "unsubscribed" | "pong" | "error" }

   A single WS per symbol is multiplexed across every subscriber (orderbook,
   trades, AND any number of kline intervals) via a module-level registry,
   mirroring the pattern used in `useRealtimePrices`.
   ══════════════════════════════════════════════════════════════════════ */

const MARKET_WS_ORIGIN = FEED_ORIGIN.replace(/^http/, "ws");

/**
 * Symbols that have a dedicated upstream behind the market proxy. Each
 * one carries orderbook, trade tape, AND kline streams on the same WS.
 * Adding a new symbol here must be matched with a `$upstream_host` row
 * in `proxy/nginx.conf.template`.
 */
const SUPPORTED_WS_SYMBOLS = new Set([
  "BTC", "ETH", "SOL", "AVAX", "LINK", "HYPE",
  "XRP", "ASTER", "TRUMP", "BNB", "SID", "PAX",
  "TSLA", "NVDA", "NAS100", "SPX500", "GOOGL", "XAU",
]);


// The gateway permits the Hyperpax browser origin; no client secret is shipped.

function buildSymbolWsUrl(symbol: string): string {
  return `${MARKET_WS_ORIGIN}/ws/${symbol.toLowerCase()}`;
}

/** True if the given display symbol is served by a dedicated market-proxy WS. */
export function hasDedicatedMarketStream(symbol: string | undefined | null): boolean {
  if (!symbol) return false;
  return SUPPORTED_WS_SYMBOLS.has(symbol.toUpperCase());
}

// ── Public types ──────────────────────────────────────────────────────
export interface SidPaxBookLevel {
  price: number;
  size: number;
}

export interface SidPaxOrderbook {
  bids: SidPaxBookLevel[];   // descending (highest price first)
  asks: SidPaxBookLevel[];   // ascending  (lowest  price first)
  ts: number;                // epoch ms
}

export interface SidPaxTrade {
  side: "BUY" | "SELL";
  price: number;
  /** Number of contracts traded (new perp_trade field). */
  contracts: number;
  /** Notional dollar value of the trade. */
  notional: number;
  /** True when this trade was a liquidation event. */
  liquidation: boolean;
  trade_time: number;        // epoch ms
  trade_id?: string;
  /** @deprecated Backward-compat alias — equals `notional` when present, else `contracts`. */
  size: number;
}

/**
 * Aggregated market stats pushed by the `<SYMBOL>@perp_stats` WebSocket topic.
 */
export interface SidPaxStats {
  symbol: string;
  // ── Perp-specific fields (perp_stats shape) ──
  mark_price: number;
  index_price: number;
  /** (mark − index) / index × 10 000 */
  basis_bps: number;
  /** EMA estimate of periodic funding rate. */
  funding_rate: number;
  last_funding_rate: number;
  /** Milliseconds until next 8-h settlement. */
  next_funding_ms: number;
  /** Rolling 5-min buy_notional / sell_notional (1.0 = balanced). */
  long_short_ratio: number;
  /** 24-h liquidation notional (USD). */
  liq_volume_24h: number;
  /** Open interest in contracts. */
  oi_contracts: number;
  /** Open interest in USD (oi_contracts × mark_price). */
  oi_usd: number;
  /** 24-h volume in contracts. */
  volume_24h_contracts: number;
  /** 24-h notional volume (USD). */
  volume_24h_usd: number;
  // ── Backward-compat aliases (used by PairHeader / MobileTradingLayout) ──
  /** @deprecated Use `volume_24h_usd`. */
  volume_24h: number;
  /** @deprecated Use `oi_usd`. */
  open_interest: number;
}

/**
 * Single kline candle in the compact tuple shape exposed by the
 * Railway candle servers, decoded into a structured object:
 *   `[time, open, high, low, close, volume, closed]`
 * `time` is unix seconds (bucket open time). `closed` is `1` once the
 * bucket is finalized, `0` while it is still in-progress.
 */
export interface SidPaxCandle {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  closed: 0 | 1;
}

/**
 * Callback bundle passed to `subscribeKline`. `onSnapshot` fires once
 * after the WS server delivers the historical bars for the topic, and
 * `onUpdate` fires for every subsequent live candle update.
 */
export interface SidPaxKlineSubscriber {
  onGap?: () => void;
  onSnapshot?: (candles: SidPaxCandle[]) => void;
  onUpdate?: (candle: SidPaxCandle) => void;
}

// ── Internal registry ─────────────────────────────────────────────────
interface KlineTopicState {
  topic: string;          // e.g. "BTC@kline_5m"
  interval: string;       // e.g. "5m"
  limit: number;          // requested snapshot size
  subscribers: Set<SidPaxKlineSubscriber>;
  /** Most recent snapshot delivered by the server, kept so late
   *  subscribers don't have to wait for the next live frame. */
  cachedSnapshot: SidPaxCandle[] | null;
  /** Whether a `subscribe` frame has been sent on the current socket. */
  sentOnSocket: boolean;
}

interface SymbolStream {
  hasConnected?: boolean;
  /** A sequence gap closed the socket; the book is fetched from REST once the socket is down. */
  bookGap?: boolean;
  url: string;
  orderbook: SidPaxOrderbook | null;
  trades: SidPaxTrade[];
  subsOrderbook: Set<(ob: SidPaxOrderbook | null) => void>;
  subsTrades: Set<(tr: SidPaxTrade[]) => void>;
  statsData: SidPaxStats | null;
  subsStats: Set<(s: SidPaxStats | null) => void>;
  /** Active kline topics keyed by interval string (e.g. "5m"). */
  klines: Map<string, KlineTopicState>;
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

const streams = new Map<string, SymbolStream>();

function ensureStream(symbol: string): SymbolStream | null {
  // SSR guard — WebSocket only exists in the browser.
  if (typeof window === "undefined") return null;

  const key = symbol.toUpperCase();
  if (!SUPPORTED_WS_SYMBOLS.has(key)) return null;
  const url = buildSymbolWsUrl(key);

  let stream = streams.get(key);
  if (stream) { if (stream.lingerTimer) clearTimeout(stream.lingerTimer); stream.lingerTimer = null; return stream; }

  stream = {
    url,
    orderbook: null,
    trades: [],
    subsOrderbook: new Set(),
    subsTrades: new Set(),
    statsData: null,
    subsStats: new Set(),
    klines: new Map(),
    ws: null,
    reconnectTimer: null,
    lingerTimer: null,
    disposed: false,
    reconnectDelay: INITIAL_RECONNECT_DELAY,
    pingTimer: null,
  };
  streams.set(key, stream);
  const created = stream;
  queueMicrotask(() => { if (!created.disposed) connect(key, created); });
  return stream;
}

function connect(symbol: string, s: SymbolStream) {
  if (s.disposed) return;
  if (!isPageActive()) { scheduleReconnect(symbol, s); return; }
  const orderbookTopic = `${symbol}@perp_orderbook`;
  const tradeTopic     = `${symbol}@perp_trade`;
  const statsTopic     = `${symbol}@perp_stats`;

  let ws: WebSocket;
  try {
    ws = new WebSocket(s.url);
  } catch {
    scheduleReconnect(symbol, s);
    return;
  }
  s.ws = ws;
  watchSocket(ws, () => { s.reconnectDelay = INITIAL_RECONNECT_DELAY; }, undefined, true);

  ws.addEventListener("open", () => {
    if (s.hasConnected) s.klines.forEach(k => k.subscribers.forEach(sub => {
      try { sub.onGap?.(); } catch { /* independent subscribers */ }
    }));
    s.hasConnected = true;
    try {
      ws.send(JSON.stringify({ event: "subscribe", topic: orderbookTopic }));
      ws.send(JSON.stringify({ event: "subscribe", topic: tradeTopic }));
      ws.send(JSON.stringify({ event: "subscribe", topic: statsTopic }));
      // Re-subscribe to every active kline topic on (re)connect. Each
      // topic carries its own snapshot + live stream.
      for (const k of s.klines.values()) {
        try {
          ws.send(JSON.stringify({ event: "subscribe", topic: k.topic, limit: k.limit }));
          k.sentOnSocket = true;
        } catch {
          k.sentOnSocket = false;
        }
      }
    } catch {
      /* socket may have closed between open and first send */
    }
    if (!s.pingTimer) {
      s.pingTimer = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          try { ws.send(JSON.stringify({ event: "ping" })); } catch { /* ignore */ }
        }
      }, PING_INTERVAL);
    }
  });

  const acceptSequence = sequenceGuard();
  ws.addEventListener("message", (ev) => {
    let msg: any;
    try { msg = JSON.parse(ev.data); } catch { return; }
    if (!acceptSequence(msg)) { s.bookGap = true; ws.close(4000, "sequence-gap"); return; }

    // Order-book frame
    if (msg.topic === orderbookTopic && msg.data) {
      const asks = normalizeLevels(msg.data.asks, "asks");
      const bids = normalizeLevels(msg.data.bids, "bids");
      const ts = typeof msg.ts === "number" ? msg.ts : Date.now();
      s.orderbook = { asks, bids, ts };
      s.subsOrderbook.forEach((cb) => cb(s.orderbook));
      return;
    }

    // Trade frame — snapshot or live
    if (msg.topic === tradeTopic && msg.data) {
      if (msg.snapshot && Array.isArray(msg.data.trades)) {
        s.trades = msg.data.trades.slice(0, TRADE_CAP).map(normalizeTrade);
        s.subsTrades.forEach((cb) => cb(s.trades));
      } else if (msg.data.trade) {
        // New shape: `data.trade` is the full trade object.
        const t = normalizeTrade(msg.data.trade);
        s.trades = [t, ...s.trades].slice(0, TRADE_CAP);
        s.subsTrades.forEach((cb) => cb(s.trades));
      } else if (msg.data.side && msg.data.price != null) {
        // Legacy shape: `data` itself is the trade object.
        const t = normalizeTrade(msg.data);
        s.trades = [t, ...s.trades].slice(0, TRADE_CAP);
        s.subsTrades.forEach((cb) => cb(s.trades));
      }
      return;
    }

    // Stats frame — perp_stats shape (full perp metrics snapshot).
    if (msg.topic === statsTopic && msg.data) {
      const d = msg.data;
      const fundingRate = Number(d.funding_rate) || 0;
      const volumeUsd   = Number(d.volume_24h_usd) || 0;
      const oiUsd       = Number(d.oi_usd) || 0;
      s.statsData = {
        symbol: String(d.symbol ?? symbol),
        mark_price:           Number(d.mark_price)           || 0,
        index_price:          Number(d.index_price)          || 0,
        basis_bps:            Number(d.basis_bps)            || 0,
        funding_rate:         fundingRate,
        last_funding_rate:    Number(d.last_funding_rate)    || 0,
        next_funding_ms:      Number(d.next_funding_ms)      || 0,
        long_short_ratio:     Number(d.long_short_ratio)     || 1,
        liq_volume_24h:       Number(d.liq_volume_24h)       || 0,
        oi_contracts:         Number(d.oi_contracts)         || 0,
        oi_usd:               oiUsd,
        volume_24h_contracts: Number(d.volume_24h_contracts) || 0,
        volume_24h_usd:       volumeUsd,
        // backward-compat aliases
        volume_24h:    volumeUsd,
        open_interest: oiUsd,
      };
      s.subsStats.forEach((cb) => cb(s.statsData));
      return;
    }

    // Kline frame — snapshot or live. Topic format: `<SYMBOL>@kline_<int>`.
    if (typeof msg.topic === "string" && msg.topic.startsWith(`${symbol}@kline_`) && msg.data) {
      const interval = msg.topic.slice((`${symbol}@kline_`).length);
      const k = s.klines.get(interval);
      if (!k) return;
      if (msg.snapshot && Array.isArray(msg.data.candles)) {
        const candles = msg.data.candles
          .map(parseCandleTuple)
          .filter((c: SidPaxCandle | null): c is SidPaxCandle => c !== null);
        k.cachedSnapshot = candles;
        k.subscribers.forEach((sub) => {
          try { sub.onSnapshot?.(candles); } catch { /* ignore */ }
        });
      } else if (msg.data.candle) {
        const candle = parseCandleTuple(msg.data.candle);
        if (candle) {
          k.subscribers.forEach((sub) => {
            try { sub.onUpdate?.(candle); } catch { /* ignore */ }
          });
        }
      }
      return;
    }

    // Control frames — welcome / subscribed / unsubscribed / pong / error
    // are informational only.
  });

  const onDisconnect = () => {
    if (s.disposed) return;
    s.orderbook = null; s.trades = []; s.statsData = null;
    s.subsOrderbook.forEach(cb => cb(null)); s.subsTrades.forEach(cb => cb([])); s.subsStats.forEach(cb => cb(null));
    s.klines.forEach(k => { k.sentOnSocket = false; k.cachedSnapshot = null; });
    if (s.pingTimer) {
      clearInterval(s.pingTimer);
      s.pingTimer = null;
    }
    if (s.ws === ws) s.ws = null;
    if (s.bookGap) { s.bookGap = false; void resyncBook(symbol, s); }
    scheduleReconnect(symbol, s);
  };
  ws.addEventListener("close", onDisconnect);
  ws.addEventListener("error", () => {
    // `close` will also fire; leave reconnection to it.
  });
}

/** The REST book carries no seq: the subscribe snapshot after the reconnect replaces it. */
async function resyncBook(symbol: string, s: SymbolStream) {
  if (!isVenueSymbol(symbol)) return;
  const url = `${gatewayUrls().restApiUrl}/orderbook/${symbol}?depth=${RESYNC_DEPTH}`;
  try {
    const res = await fetchWithPolicy(url);
    const json = (await res.json()) as { success?: unknown; data?: unknown };
    if (json.success !== true || s.disposed || s.orderbook !== null) return;
    const book = parseVenueBook(json.data, Date.now());
    if (!book) return;
    s.orderbook = { asks: book.asks, bids: book.bids, ts: book.ts };
    s.subsOrderbook.forEach((cb) => cb(s.orderbook));
  } catch { /* the subscribe snapshot restores the book */ }
}

function scheduleReconnect(symbol: string, s: SymbolStream) {
  if (s.reconnectTimer || s.disposed) return;
  const delay = s.reconnectDelay;
  s.reconnectTimer = scheduleActive(() => {
    s.reconnectTimer = null;
    s.reconnectDelay = Math.min(MAX_RECONNECT_DELAY, s.reconnectDelay * 2);
    connect(symbol, s);
  }, delay, s.url);
}

function releaseIfIdle(symbol: string, s: SymbolStream) {
  const listeners = () => s.subsOrderbook.size + s.subsTrades.size + s.subsStats.size + [...s.klines.values()].reduce((n, k) => n + k.subscribers.size, 0);
  if (listeners() || s.lingerTimer) return;
  s.lingerTimer = setTimeout(() => {
    s.lingerTimer = null;
    if (listeners()) return;
    s.disposed = true; s.reconnectTimer?.();
    if (s.pingTimer) clearInterval(s.pingTimer);
    s.ws?.close(1000, "no-subscribers"); s.ws = null;
    streams.delete(symbol.toUpperCase());
  }, 15000);
}

function normalizeTrade(t: any): SidPaxTrade {
  const side = String(t?.side ?? "").toUpperCase() === "BUY" ? "BUY" : "SELL";
  const tradeTimeRaw = t?.trade_time ?? t?.ts ?? Date.now();
  const tradeTime = Number(tradeTimeRaw);
  // perp_trade shape: contracts | notional | liquidation
  // Legacy shape:     size | quantity
  const contracts  = Number(t?.contracts ?? t?.size ?? t?.quantity ?? 0);
  const notional   = Number(t?.notional ?? 0) || contracts;
  return {
    side,
    price:       Number(t?.price),
    contracts,
    notional,
    liquidation: Boolean(t?.liquidation),
    size:        notional || contracts,  // backward-compat
    trade_id:    t?.trade_id ?? t?.id ?? undefined,
    trade_time:  Number.isFinite(tradeTime) ? tradeTime : Date.now(),
  };
}

function parseCandleTuple(raw: unknown): SidPaxCandle | null {
  if (!Array.isArray(raw) || raw.length < 6) return null;
  const time = Number(raw[0]);
  const open = Number(raw[1]);
  const high = Number(raw[2]);
  const low = Number(raw[3]);
  const close = Number(raw[4]);
  const volume = Number(raw[5]);
  const closed = Number(raw[6] ?? 1) === 1 ? 1 : 0;
  if (!Number.isFinite(time) || !Number.isFinite(open) || !Number.isFinite(high) ||
      !Number.isFinite(low) || !Number.isFinite(close)) {
    return null;
  }
  return {
    time,
    open,
    high,
    low,
    close,
    volume: Number.isFinite(volume) ? volume : 0,
    closed,
  };
}

/** Order-book listener for a dedicated stream: the cached book, each frame, and null while the socket is down. */
export function subscribeOrderbook(symbol: string, cb: (book: SidPaxOrderbook | null) => void): () => void {
  const s = ensureStream(symbol);
  if (!s) return () => {};
  s.subsOrderbook.add(cb);
  if (s.orderbook) cb(s.orderbook);
  return () => {
    s.subsOrderbook.delete(cb);
    releaseIfIdle(symbol, s);
  };
}

// ── React hooks ───────────────────────────────────────────────────────
/**
 * Subscribe to the dedicated order-book feed for SID or PAX.
 * Returns `null` until the first frame arrives, or if the symbol has no
 * dedicated stream configured.
 */
export function useSidPaxOrderbook(symbol: string | undefined): SidPaxOrderbook | null {
  const [book, setBook] = useState<SidPaxOrderbook | null>(null);

  useEffect(() => {
    if (!symbol) {
      setBook(null);
      return;
    }
    const s = ensureStream(symbol);
    if (!s) {
      setBook(null);
      return;
    }
    // Sync any cached value captured before the first render.
    setBook(s.orderbook);
    return subscribeOrderbook(symbol, setBook);
  }, [symbol]);

  return book;
}

/**
 * Subscribe to the dedicated trade tape for SID or PAX.
 * Always returns newest-first, capped at `TRADE_CAP` rows.
 */
export function useSidPaxTrades(symbol: string | undefined): SidPaxTrade[] {
  const [trades, setTrades] = useState<SidPaxTrade[]>([]);

  useEffect(() => {
    if (!symbol) {
      setTrades([]);
      return;
    }
    const s = ensureStream(symbol);
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

/**
 * Subscribe to the `@stats` feed — delivers 24h volume, open interest,
 * and estimated + last funding rates. Returns `null` until the first
 * frame arrives or if the symbol has no dedicated stream.
 */
export function useSidPaxStats(symbol: string | undefined): SidPaxStats | null {
  const [stats, setStats] = useState<SidPaxStats | null>(null);

  useEffect(() => {
    if (!symbol) {
      setStats(null);
      return;
    }
    const s = ensureStream(symbol);
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

// ── Kline streaming API ───────────────────────────────────────────────

const DEFAULT_KLINE_LIMIT = 1000;

/**
 * True if the given symbol has a dedicated Railway WS that carries kline
 * data (in addition to orderbook + trade tape).
 */
export function hasDedicatedKlineStream(symbol: string | undefined | null): boolean {
  return hasDedicatedMarketStream(symbol);
}

/**
 * Map a TradingView resolution string (e.g. `"1"`, `"5"`, `"60"`, `"1D"`)
 * to the corresponding Railway kline topic interval (e.g. `"1m"`, `"5m"`,
 * `"1h"`, `"1D"`). Returns `null` if the resolution is not supported by the
 * candle servers (e.g. `"1M"` monthly), in which case callers should fall
 * back to REST.
 *
 * Supported per `/config`:
 *   `1, 2, 3, 5, 15, 30, 60, 240, 480, D, W`
 */
export function tvResolutionToKlineInterval(resolution: string | undefined | null): string | null {
  if (!resolution) return null;
  const r = String(resolution).toUpperCase();
  switch (r) {
    case "1":   return "1m";
    case "2":   return "2m";
    case "3":   return "3m";
    case "5":   return "5m";
    case "15":  return "15m";
    case "30":  return "30m";
    case "60":  return "1h";
    case "240": return "4h";
    case "480": return "8h";
    case "D":
    case "1D":  return "1D";
    case "W":
    case "1W":  return "1W";
    default:    return null;
  }
}

/**
 * Subscribe to a per-symbol kline topic on the dedicated Railway WS.
 *
 * Multiplexes onto the existing `SymbolStream` socket — multiple callers
 * for the same `(symbol, interval)` share a single server-side
 * subscription. The first caller triggers a `subscribe` frame; the last
 * caller's unsubscribe sends the matching `unsubscribe` frame and frees
 * the topic state.
 *
 * Snapshot delivery: if the server has already delivered a snapshot for
 * the topic before this caller arrived, the cached snapshot is dispatched
 * synchronously on the next microtask so chart code doesn't need to wait
 * for the server to re-send it.
 *
 * Returns an unsubscribe function. Returns a no-op if the symbol has no
 * dedicated WS or if the call happens during SSR.
 */
export function subscribeKline(
  symbol: string | undefined | null,
  interval: string | undefined | null,
  subscriber: SidPaxKlineSubscriber,
  limit?: number,
): () => void {
  if (!symbol || !interval) return () => {};
  const s = ensureStream(symbol);
  if (!s) return () => {};

  const symKey = symbol.toUpperCase();
  const topic = `${symKey}@kline_${interval}`;
  const desiredLimit = Math.min(5000, Math.max(1, Math.floor(limit ?? DEFAULT_KLINE_LIMIT)));

  let k = s.klines.get(interval);
  if (!k) {
    k = {
      topic,
      interval,
      limit: desiredLimit,
      subscribers: new Set(),
      cachedSnapshot: null,
      sentOnSocket: false,
    };
    s.klines.set(interval, k);
  } else if (desiredLimit > k.limit) {
    // Bump the requested snapshot size if a later caller wants more bars.
    k.limit = desiredLimit;
  }

  k.subscribers.add(subscriber);

  // Send the subscribe frame if the socket is open and we haven't yet.
  if (!k.sentOnSocket && s.ws && s.ws.readyState === WebSocket.OPEN) {
    try {
      s.ws.send(JSON.stringify({ event: "subscribe", topic: k.topic, limit: k.limit }));
      k.sentOnSocket = true;
    } catch {
      // Will be retried by the open handler on next reconnect.
    }
  }

  // Replay cached snapshot to the new subscriber.
  if (k.cachedSnapshot && subscriber.onSnapshot) {
    const snap = k.cachedSnapshot;
    queueMicrotask(() => {
      try { subscriber.onSnapshot?.(snap); } catch { /* ignore */ }
    });
  }

  return () => {
    if (!k) return;
    k.subscribers.delete(subscriber);
    if (k.subscribers.size === 0) {
      // Last subscriber left — tell the server to stop sending this topic
      // and drop our local state so the next subscriber gets a fresh
      // snapshot.
      if (s.ws && s.ws.readyState === WebSocket.OPEN && k.sentOnSocket) {
        try {
          s.ws.send(JSON.stringify({ event: "unsubscribe", topic: k.topic }));
        } catch { /* ignore */ }
      }
      s.klines.delete(interval);
      releaseIfIdle(symKey, s);
    }
  };
}
