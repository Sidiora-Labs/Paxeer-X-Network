"use client";
import { isPageActive, scheduleActive, sequenceGuard, watchSocket, notifyStreamResync, type CancelTask } from "@/core/net/realtime";
import { gatewayUrls } from "@/core/network/gateway";

import { useEffect, useState } from "react";

/* ══════════════════════════════════════════════════════════════════════
   User-scoped SPOT WebSocket — the gateway `/v1/ws` socket.

   Wire protocol (verified live):
     hello → { type: "hello", service: "websocket",
               streams: ["trade","position","pnl","order","closed-leg"],
               protocolVersion: 1 }

     client → server :
        { op: "subscribe",   stream: "<stream>", address: "0x..." }
        { op: "unsubscribe", stream: "<stream>", address: "0x..." }
        { op: "ping" }

   Frame routing rule of thumb: any frame that includes `stream` and
   `address` (lower-cased to match the subscribe form) is treated as a
   data frame for that subscription. Frame payloads are delivered under
   the `data` key when present, falling back to the whole frame minus
   the routing fields.

   Singleton: ONE WebSocket per browser tab. Multiplexed across N React
   subscribers and across the 5 streams. Subscribe frames are sent on
   first subscription per `(stream, address)` and the matching
   unsubscribe is sent when the last subscriber leaves. On reconnect we
   replay every active subscription.
   ══════════════════════════════════════════════════════════════════════ */

export const SPOT_USER_WS_URL = gatewayUrls().spotUserWsUrl;

const PING_INTERVAL = 25_000;
const INITIAL_RECONNECT_DELAY = 500;
const MAX_RECONNECT_DELAY = 60_000;

export type SpotUserStreamName =
  | "trade"
  | "position"
  | "pnl"
  | "order"
  | "closed-leg";

export type SpotUserConnectionState = "idle" | "connecting" | "open" | "closed";

/** Generic frame body. The server wraps payloads in `data` for most frames. */
export type SpotUserFrame = Record<string, unknown>;

type FrameHandler = (frame: SpotUserFrame) => void;

interface SubscriptionKey {
  stream: SpotUserStreamName;
  address: string; // lower-cased
}

interface Subscription {
  stream: SpotUserStreamName;
  address: string;
  subscribers: Set<FrameHandler>;
  /** True once a `subscribe` op has been sent on the current socket. */
  sentOnSocket: boolean;
}

interface State {
  ws: WebSocket | null;
  connection: SpotUserConnectionState;
  subscriptions: Map<string, Subscription>;
  subsConnection: Set<(c: SpotUserConnectionState) => void>;
  reconnectTimer: CancelTask | null;
  reconnectDelay: number;
  pingTimer: ReturnType<typeof setInterval> | null;
}

const state: State = {
  ws: null,
  connection: "idle",
  subscriptions: new Map(),
  subsConnection: new Set(),
  reconnectTimer: null,
  reconnectDelay: INITIAL_RECONNECT_DELAY,
  pingTimer: null,
};

function subKey(stream: SpotUserStreamName, address: string): string {
  return `${stream}|${address.toLowerCase()}`;
}

function setConnection(next: SpotUserConnectionState): void {
  if (state.connection === next) return;
  state.connection = next;
  state.subsConnection.forEach((cb) => {
    try {
      cb(next);
    } catch {
      /* ignore */
    }
  });
}

function ensureSocket(): void {
  if (state.reconnectTimer) return;
  if (typeof window === "undefined") return;
  if (
    state.ws &&
    (state.ws.readyState === WebSocket.OPEN ||
      state.ws.readyState === WebSocket.CONNECTING)
  ) {
    return;
  }
  connect();
}

function connect(): void {
  if (!isPageActive()) { scheduleReconnect(); return; }
  if (typeof window === "undefined") return;
  setConnection("connecting");

  let ws: WebSocket;
  try {
    ws = new WebSocket(SPOT_USER_WS_URL);
  } catch {
    scheduleReconnect();
    return;
  }
  state.ws = ws;
  watchSocket(ws, () => { state.reconnectDelay = INITIAL_RECONNECT_DELAY; });
  // Reset all sentOnSocket flags on a brand-new socket.
  for (const sub of state.subscriptions.values()) {
    sub.sentOnSocket = false;
  }

  ws.addEventListener("open", () => {
    notifyStreamResync(SPOT_USER_WS_URL);
    setConnection("open");
    flushSubscribes();
    if (!state.pingTimer) {
      state.pingTimer = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          try {
            ws.send(JSON.stringify({ op: "ping" }));
          } catch {
            /* ignore */
          }
        }
      }, PING_INTERVAL);
    }
  });

  const acceptSequence = sequenceGuard();
  ws.addEventListener("message", (ev) => {
    let frame: SpotUserFrame;
    try {
      frame = JSON.parse(ev.data as string) as SpotUserFrame;
    } catch {
      return;
    }
    if (!acceptSequence(frame)) { ws.close(4000, "sequence-gap"); return; }

    // Server sometimes echoes hello / pong / subscribed control frames.
    // Only data frames carry both `stream` and `address`.
    const stream = typeof frame.stream === "string" ? (frame.stream as SpotUserStreamName) : null;
    const addrRaw = typeof frame.address === "string" ? frame.address : null;
    if (!stream || !addrRaw) return;

    const key = subKey(stream, addrRaw);
    const sub = state.subscriptions.get(key);
    if (!sub || sub.subscribers.size === 0) return;

    sub.subscribers.forEach((cb) => {
      try {
        cb(frame);
      } catch {
        /* ignore */
      }
    });
  });

  const onClosed = () => {
    if (state.pingTimer) {
      clearInterval(state.pingTimer);
      state.pingTimer = null;
    }
    if (state.ws === ws) state.ws = null;
    for (const sub of state.subscriptions.values()) {
      sub.sentOnSocket = false;
    }
    setConnection("closed");
    scheduleReconnect();
  };

  ws.addEventListener("close", onClosed);
  ws.addEventListener("error", () => {
    /* `close` will fire next */
  });
}

function flushSubscribes(): void {
  const ws = state.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  for (const sub of state.subscriptions.values()) {
    if (sub.sentOnSocket) continue;
    try {
      ws.send(
        JSON.stringify({ op: "subscribe", stream: sub.stream, address: sub.address }),
      );
      sub.sentOnSocket = true;
    } catch {
      /* will retry on next open */
    }
  }
}

function scheduleReconnect(): void {
  if (state.reconnectTimer) return;
  // Only reconnect if there's still demand.
  if (state.subscriptions.size === 0 && state.subsConnection.size === 0) return;
  const delay = state.reconnectDelay;
  state.reconnectTimer = scheduleActive(() => {
    state.reconnectTimer = null;
    state.reconnectDelay = Math.min(MAX_RECONNECT_DELAY, state.reconnectDelay * 2);
    connect();
  }, delay, SPOT_USER_WS_URL);
}

function teardownIfIdle(): void {
  if (state.subscriptions.size > 0 || state.subsConnection.size > 0) return;
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
    try {
      ws.close(1000, "no-subscribers");
    } catch {
      /* ignore */
    }
  }
  state.reconnectDelay = INITIAL_RECONNECT_DELAY;
  setConnection("idle");
}

// ── Subscription primitive ────────────────────────────────────────────

/**
 * Subscribe to one user-scoped stream for one address. Returns an
 * unsubscribe function. Handler receives every frame that lands on the
 * stream for the address (the routing fields are still on the frame).
 */
export function subscribeSpotUserStream(
  stream: SpotUserStreamName,
  address: string,
  handler: FrameHandler,
): () => void {
  if (typeof window === "undefined") return () => {};
  const lcAddr = address.toLowerCase();
  const key = subKey(stream, lcAddr);

  let sub = state.subscriptions.get(key);
  if (!sub) {
    sub = {
      stream,
      address: lcAddr,
      subscribers: new Set(),
      sentOnSocket: false,
    };
    state.subscriptions.set(key, sub);
  }
  sub.subscribers.add(handler);

  ensureSocket();
  if (
    !sub.sentOnSocket &&
    state.ws &&
    state.ws.readyState === WebSocket.OPEN
  ) {
    try {
      state.ws.send(
        JSON.stringify({ op: "subscribe", stream, address: lcAddr }),
      );
      sub.sentOnSocket = true;
    } catch {
      /* will retry on next open */
    }
  }

  return () => {
    if (!sub) return;
    sub.subscribers.delete(handler);
    if (sub.subscribers.size > 0) return;
    // Last subscriber for this (stream,address) — tell the server and
    // free the local slot so a future subscribe gets a fresh start.
    if (state.ws && state.ws.readyState === WebSocket.OPEN && sub.sentOnSocket) {
      try {
        state.ws.send(
          JSON.stringify({ op: "unsubscribe", stream, address: lcAddr }),
        );
      } catch {
        /* ignore */
      }
    }
    state.subscriptions.delete(key);
    queueMicrotask(teardownIfIdle);
  };
}

// ── React hooks ───────────────────────────────────────────────────────

/**
 * Connection state for the user-scoped SPOT WebSocket. Useful for an
 * ambient "Live / Connecting / Offline" indicator.
 */
export function useSpotUserConnection(): SpotUserConnectionState {
  const [conn, setConn] = useState<SpotUserConnectionState>(state.connection);

  useEffect(() => {
    state.subsConnection.add(setConn);
    setConn(state.connection);
    return () => {
      state.subsConnection.delete(setConn);
      queueMicrotask(teardownIfIdle);
    };
  }, []);

  return conn;
}

/**
 * Subscribe to a single stream for an address. Returns the latest frame
 * (or `null` until one arrives). Re-renders only on new frames for this
 * `(stream, address)` pair.
 *
 * For higher-level hooks that build typed lists, use
 * `subscribeSpotUserStream` directly.
 */
export function useSpotUserStream(
  stream: SpotUserStreamName,
  address: string | null | undefined,
): SpotUserFrame | null {
  const [frame, setFrame] = useState<SpotUserFrame | null>(null);

  useEffect(() => {
    if (!address) {
      setFrame(null);
      return;
    }
    const off = subscribeSpotUserStream(stream, address, (f) => setFrame(f));
    return off;
  }, [stream, address]);

  return frame;
}

/**
 * Subscribe to multiple streams at once for an address. Returns a map of
 * latest frame keyed by stream. Internally one subscription per stream.
 */
export function useSpotUserStreams(
  streams: SpotUserStreamName[],
  address: string | null | undefined,
): Partial<Record<SpotUserStreamName, SpotUserFrame>> {
  const [frames, setFrames] = useState<
    Partial<Record<SpotUserStreamName, SpotUserFrame>>
  >({});

  useEffect(() => {
    if (!address || streams.length === 0) {
      setFrames({});
      return;
    }
    const offs = streams.map((s) =>
      subscribeSpotUserStream(s, address, (f) =>
        setFrames((prev) => ({ ...prev, [s]: f })),
      ),
    );
    return () => offs.forEach((off) => off());
    // streams is a dynamic array — depend on the joined key so React
    // doesn't churn on stable arrays passed inline.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [address, streams.join("|")]);

  return frames;
}
