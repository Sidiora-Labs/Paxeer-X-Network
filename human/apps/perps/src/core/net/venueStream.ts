"use client";
import { useEffect, useState } from "react";
import { fetchWithPolicy } from "@/core/net/fetch";
import { scheduleActive, sequenceGuard, watchSocket, type CancelTask } from "@/core/net/realtime";
import { gatewayUrls } from "@/core/network/gateway";
import { PERPS_MARKETS } from "@/core/perps/config";

/** Gateway topic protocol of the shared `/ws` socket: one socket per origin, topics multiplexed. */

export interface VenueLevel {
  price: number;
  size: number;
}

export interface VenueBook {
  asks: VenueLevel[];
  bids: VenueLevel[];
  markPrice: number | null;
  indexPrice: number | null;
  basisBps: number | null;
  ts: number;
}

export interface VenueFill {
  id: string | null;
  side: "BUY" | "SELL";
  price: number;
  contracts: number;
  notional: number;
  liquidation: boolean;
  trade_time: number;
}

export interface SpotVenueTrade {
  id: string | null;
  side: "BUY" | "SELL";
  price: number;
  size: number;
  trade_time: number;
}

export type VenueFrame = { event: "subscribe" | "unsubscribe"; topic: string };

export const MAX_SUBSCRIPTIONS = 200;
export const FILL_CAP = 100;
export const RESYNC_DEPTH = 100;
export const GAP_CLOSE_CODE = 4000;
const INITIAL_RECONNECT_DELAY = 500;
const MAX_RECONNECT_DELAY = 60_000;

type TopicValue = VenueBook | VenueFill[] | SpotVenueTrade[];
type Listener = (value: TopicValue | null) => void;

interface Topic {
  name: string;
  symbol: string;
  kind: "book" | "fills" | "spotBook" | "spotTrades";
  listeners: Set<Listener>;
  value: TopicValue | null;
  seq: number | null;
  subscribed: boolean;
  unavailable: boolean;
}

interface Connection {
  url: string;
  ws: WebSocket | null;
  open: boolean;
  topics: Map<string, Topic>;
  accept: (frame: unknown) => boolean;
  snapshotId: string | null;
  reconnect: CancelTask | null;
  delay: number;
  disposed: boolean;
}

const VENUE_SYMBOLS = new Set(PERPS_MARKETS.map(m => m.symbol.toUpperCase()));
/** The spot venue serves @orderbook and @trade for PAX and SID only; other symbols answer unknown_topic (contracts/data-services.md section 10). */
const SPOT_VENUE_SYMBOLS = new Set(["PAX", "SID"]);
const connections = new Map<string, Connection>();

export function isVenueSymbol(symbol: string | null | undefined): boolean {
  return !!symbol && VENUE_SYMBOLS.has(symbol.toUpperCase());
}

export function isSpotVenueSymbol(symbol: string | null | undefined): boolean {
  return !!symbol && SPOT_VENUE_SYMBOLS.has(symbol.toUpperCase());
}

export function perpBookTopic(symbol: string): string {
  return `${symbol.toUpperCase()}@perp_orderbook`;
}

export function perpTradeTopic(symbol: string): string {
  return `${symbol.toUpperCase()}@perp_trade`;
}

export function spotBookTopic(symbol: string): string {
  return `${symbol.toUpperCase()}@orderbook`;
}

export function spotTradeTopic(symbol: string): string {
  return `${symbol.toUpperCase()}@trade`;
}

const TOPIC_NAMES: Record<Topic["kind"], (symbol: string) => string> = {
  book: perpBookTopic,
  fills: perpTradeTopic,
  spotBook: spotBookTopic,
  spotTrades: spotTradeTopic,
};

function finite(value: unknown): number | null {
  const n = typeof value === "number" ? value : typeof value === "string" && value.trim() !== "" ? Number(value) : NaN;
  return Number.isFinite(n) ? n : null;
}

function parseLevels(raw: unknown, descending: boolean): VenueLevel[] {
  if (!Array.isArray(raw)) return [];
  const levels: VenueLevel[] = [];
  for (const entry of raw) {
    const price = finite(Array.isArray(entry) ? entry[0] : (entry as { price?: unknown } | null)?.price);
    const size = finite(Array.isArray(entry) ? entry[1] : (entry as { quantity?: unknown; size?: unknown } | null)?.quantity ?? (entry as { size?: unknown } | null)?.size);
    if (price !== null && size !== null && price > 0 && size >= 0) levels.push({ price, size });
  }
  return levels.sort((a, b) => (descending ? b.price - a.price : a.price - b.price));
}

export function parseVenueBook(data: unknown, ts: number): VenueBook | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  if (!Array.isArray(d.asks) && !Array.isArray(d.bids)) return null;
  return {
    asks: parseLevels(d.asks, false),
    bids: parseLevels(d.bids, true),
    markPrice: finite(d.mark_price),
    indexPrice: finite(d.index_price),
    basisBps: finite(d.basis_bps),
    ts,
  };
}

export function parseVenueFill(raw: unknown): VenueFill | null {
  if (!raw || typeof raw !== "object") return null;
  const t = raw as Record<string, unknown>;
  const side = typeof t.side === "string" ? t.side.toUpperCase() : "";
  const price = finite(t.price);
  const contracts = finite(t.contracts);
  const notional = finite(t.notional);
  const tradeTime = finite(t.trade_time);
  if ((side !== "BUY" && side !== "SELL") || price === null || contracts === null || notional === null || tradeTime === null) return null;
  return {
    id: typeof t.id === "string" || typeof t.id === "number" ? String(t.id) : null,
    side,
    price,
    contracts,
    notional,
    liquidation: t.liquidation === true,
    trade_time: tradeTime,
  };
}

export function parseSpotVenueTrade(raw: unknown): SpotVenueTrade | null {
  if (!raw || typeof raw !== "object") return null;
  const t = raw as Record<string, unknown>;
  const side = typeof t.side === "string" ? t.side.toUpperCase() : "";
  const price = finite(t.price);
  const size = finite(t.size);
  const tradeTime = finite(t.trade_time);
  if ((side !== "BUY" && side !== "SELL") || price === null || size === null || tradeTime === null) return null;
  return {
    id: typeof t.id === "string" || typeof t.id === "number" ? String(t.id) : null,
    side,
    price,
    size,
    trade_time: tradeTime,
  };
}

function parseTradeList<T>(raw: unknown, parse: (entry: unknown) => T | null): T[] {
  if (!Array.isArray(raw)) return [];
  const trades: T[] = [];
  for (const entry of raw) {
    const trade = parse(entry);
    if (trade) trades.push(trade);
    if (trades.length === FILL_CAP) break;
  }
  return trades;
}

/** `null` when a live frame carries no valid trade. */
function nextTape<T>(prev: T[] | null, data: Record<string, unknown> | undefined, snapshot: boolean, parse: (entry: unknown) => T | null): T[] | null {
  if (snapshot) return parseTradeList(data?.trades, parse);
  const trade = parse(data);
  return trade ? [trade, ...(prev ?? [])].slice(0, FILL_CAP) : null;
}

function emit(conn: Connection, topic: Topic) {
  const value = conn.open && !topic.unavailable ? topic.value : null;
  topic.listeners.forEach(fn => {
    try { fn(value); } catch { /* independent consumers */ }
  });
}

function send(conn: Connection, frame: VenueFrame | { event: "ping" }): boolean {
  if (!conn.ws || !conn.open) return false;
  try { conn.ws.send(JSON.stringify(frame)); return true; } catch { return false; }
}

function subscribedCount(conn: Connection): number {
  let n = 0;
  conn.topics.forEach(t => { if (t.subscribed) n++; });
  return n;
}

function subscribe(conn: Connection, topic: Topic) {
  if (topic.subscribed || !conn.open) return;
  if (subscribedCount(conn) >= MAX_SUBSCRIPTIONS) { topic.unavailable = true; emit(conn, topic); return; }
  topic.subscribed = send(conn, { event: "subscribe", topic: topic.name });
}

function dropState(conn: Connection) {
  conn.accept = sequenceGuard();
  conn.topics.forEach(topic => { topic.value = null; topic.seq = null; emit(conn, topic); });
}

/** The REST book carries no seq: the subscribe snapshot that follows sets the baseline. */
async function resyncFromRest(conn: Connection, topic: Topic) {
  const url = `${gatewayUrls().restApiUrl}/orderbook/${topic.symbol}?depth=${RESYNC_DEPTH}`;
  try {
    const res = await fetchWithPolicy(url);
    const json = (await res.json()) as { success?: unknown; data?: unknown };
    if (json.success !== true || conn.topics.get(topic.name) !== topic || topic.value !== null) return;
    const book = parseVenueBook(json.data, Date.now());
    if (!book) return;
    topic.value = book;
    emit(conn, topic);
  } catch { /* the subscribe snapshot restores the book */ }
}

function handleFrame(conn: Connection, ws: WebSocket, msg: Record<string, unknown>) {
  const snapshotId = typeof msg.snapshotId === "string" ? msg.snapshotId : null;
  if (msg.event === "welcome") {
    if (snapshotId !== null && conn.snapshotId !== null && snapshotId !== conn.snapshotId) dropState(conn);
    if (snapshotId !== null) conn.snapshotId = snapshotId;
    return;
  }
  if (msg.event === "error") {
    const topic = typeof msg.topic === "string" ? conn.topics.get(msg.topic) : undefined;
    if (topic) { topic.subscribed = false; topic.unavailable = true; topic.value = null; emit(conn, topic); }
    return;
  }
  if (typeof msg.event === "string" || typeof msg.topic !== "string") return;
  const topic = conn.topics.get(msg.topic);
  const seq = msg.seq;
  if (!topic || typeof seq !== "number") return;

  if (snapshotId !== null && conn.snapshotId !== null && snapshotId !== conn.snapshotId) {
    conn.snapshotId = snapshotId;
    dropState(conn);
    conn.topics.forEach(t => {
      if (t.subscribed) send(conn, { event: "unsubscribe", topic: t.name });
      t.subscribed = false;
      subscribe(conn, t);
    });
    return;
  }
  if (snapshotId !== null) conn.snapshotId = snapshotId;

  if (!conn.accept(msg)) {
    if (topic.kind === "book") { topic.value = null; topic.seq = null; void resyncFromRest(conn, topic); }
    ws.close(GAP_CLOSE_CODE, "sequence-gap");
    return;
  }
  const snapshot = msg.snapshot === true;
  if (topic.seq !== null && (snapshot ? seq < topic.seq : seq <= topic.seq)) return;
  const ts = typeof msg.ts === "number" ? msg.ts : Date.now();

  if (topic.kind === "book" || topic.kind === "spotBook") {
    const book = parseVenueBook(msg.data, ts);
    if (!book) return;
    topic.value = book;
  } else {
    const data = msg.data as Record<string, unknown> | undefined;
    const tape = topic.kind === "fills"
      ? nextTape(topic.value as VenueFill[] | null, data, snapshot, parseVenueFill)
      : nextTape(topic.value as SpotVenueTrade[] | null, data, snapshot, parseSpotVenueTrade);
    if (!tape) return;
    topic.value = tape;
  }
  topic.seq = seq;
  emit(conn, topic);
}

function connect(conn: Connection) {
  conn.reconnect = null;
  if (conn.disposed) return;
  let ws: WebSocket;
  try { ws = new WebSocket(conn.url); } catch { scheduleReconnect(conn); return; }
  conn.ws = ws;
  watchSocket(ws, () => { conn.delay = INITIAL_RECONNECT_DELAY; }, { event: "ping" });
  ws.addEventListener("open", () => {
    if (conn.ws !== ws) return;
    conn.open = true;
    conn.topics.forEach(topic => { subscribe(conn, topic); emit(conn, topic); });
  });
  ws.addEventListener("message", event => {
    if (conn.ws !== ws) return;
    let msg: unknown;
    try { msg = JSON.parse(typeof event.data === "string" ? event.data : ""); } catch { return; }
    if (msg && typeof msg === "object") handleFrame(conn, ws, msg as Record<string, unknown>);
  });
  ws.addEventListener("close", () => {
    if (conn.ws !== ws) return;
    conn.ws = null;
    conn.open = false;
    conn.topics.forEach(topic => { topic.subscribed = false; topic.unavailable = false; });
    conn.accept = sequenceGuard();
    conn.topics.forEach(topic => {
      // A gap keeps the REST resync book of its topic; every other topic waits for a fresh snapshot.
      if (topic.seq !== null || topic.kind === "fills") topic.value = null;
      topic.seq = null;
      emit(conn, topic);
    });
    scheduleReconnect(conn);
  });
}

function scheduleReconnect(conn: Connection) {
  if (conn.disposed || conn.reconnect) return;
  const delay = conn.delay;
  conn.delay = Math.min(MAX_RECONNECT_DELAY, conn.delay * 2);
  conn.reconnect = scheduleActive(() => connect(conn), delay, conn.url);
}

function acquire(kind: Topic["kind"], symbol: string, listener: Listener): CancelTask {
  const key = symbol.toUpperCase();
  const listed = kind === "spotBook" || kind === "spotTrades" ? isSpotVenueSymbol(key) : isVenueSymbol(key);
  if (typeof window === "undefined" || !listed) { listener(null); return () => {}; }
  const url = gatewayUrls().venueWsUrl;
  let conn = connections.get(url);
  if (!conn) {
    conn = { url, ws: null, open: false, topics: new Map(), accept: sequenceGuard(), snapshotId: null, reconnect: null, delay: INITIAL_RECONNECT_DELAY, disposed: false };
    connections.set(url, conn);
  }
  const owner = conn;
  const name = TOPIC_NAMES[kind](key);
  let topic = owner.topics.get(name);
  if (!topic) {
    topic = { name, symbol: key, kind, listeners: new Set(), value: null, seq: null, subscribed: false, unavailable: false };
    owner.topics.set(name, topic);
    subscribe(owner, topic);
  }
  const held = topic;
  held.listeners.add(listener);
  listener(owner.open && !held.unavailable ? held.value : null);
  if (!owner.ws && !owner.reconnect) connect(owner);

  let released = false;
  return () => {
    if (released) return;
    released = true;
    held.listeners.delete(listener);
    if (held.listeners.size > 0) return;
    if (held.subscribed) send(owner, { event: "unsubscribe", topic: held.name });
    owner.topics.delete(held.name);
    if (owner.topics.size > 0) return;
    owner.disposed = true;
    owner.reconnect?.();
    owner.reconnect = null;
    const ws = owner.ws;
    owner.ws = null;
    owner.open = false;
    ws?.close(1000, "no-subscribers");
    connections.delete(owner.url);
  };
}

/** `null` while the gateway socket is not open, before the first snapshot, or when the venue does not list the symbol. */
export function subscribeVenueBook(symbol: string, listener: (book: VenueBook | null) => void): CancelTask {
  return acquire("book", symbol, listener as Listener);
}

/** Newest first, at most FILL_CAP; `null` under the same conditions as subscribeVenueBook. */
export function subscribeVenueFills(symbol: string, listener: (fills: VenueFill[] | null) => void): CancelTask {
  return acquire("fills", symbol, listener as Listener);
}

export function useVenueBook(symbol: string | undefined): VenueBook | null {
  const [book, setBook] = useState<VenueBook | null>(null);
  useEffect(() => {
    if (!symbol) { setBook(null); return; }
    return subscribeVenueBook(symbol, setBook);
  }, [symbol]);
  return book;
}

export function useVenueFills(symbol: string | undefined): VenueFill[] | null {
  const [fills, setFills] = useState<VenueFill[] | null>(null);
  useEffect(() => {
    if (!symbol) { setFills(null); return; }
    return subscribeVenueFills(symbol, setFills);
  }, [symbol]);
  return fills;
}

/** The spot venue book of <SYMBOL>@orderbook; `null` while the gateway socket is not open, before the first snapshot, or for a symbol the spot venue does not list. */
export function subscribeSpotVenueBook(symbol: string, listener: (book: VenueBook | null) => void): CancelTask {
  return acquire("spotBook", symbol, listener as Listener);
}

/** The spot venue trades of <SYMBOL>@trade, newest first, at most FILL_CAP; `null` under the same conditions as subscribeSpotVenueBook. */
export function subscribeSpotVenueTrades(symbol: string, listener: (trades: SpotVenueTrade[] | null) => void): CancelTask {
  return acquire("spotTrades", symbol, listener as Listener);
}

export function useSpotVenueBook(symbol: string | undefined): VenueBook | null {
  const [book, setBook] = useState<VenueBook | null>(null);
  useEffect(() => {
    if (!symbol) { setBook(null); return; }
    return subscribeSpotVenueBook(symbol, setBook);
  }, [symbol]);
  return book;
}

export function useSpotVenueTrades(symbol: string | undefined): SpotVenueTrade[] | null {
  const [trades, setTrades] = useState<SpotVenueTrade[] | null>(null);
  useEffect(() => {
    if (!symbol) { setTrades(null); return; }
    return subscribeSpotVenueTrades(symbol, setTrades);
  }, [symbol]);
  return trades;
}
