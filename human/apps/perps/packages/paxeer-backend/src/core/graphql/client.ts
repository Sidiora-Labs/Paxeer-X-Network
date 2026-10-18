import { activePoll, onStreamResync } from "@paxeer-network/paxeer-backend/core/net/realtime";
import { isPageActive, scheduleActive, watchSocket, notifyStreamResync, type CancelTask } from "@paxeer-network/paxeer-backend/core/net/realtime";
import { fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";
/* ══════════════════════════════════════════════════════════════════════
   GraphQL singleton client — ECC Phase 1 polling consolidation.

   Replaces the ~30 independent setIntervals scattered across the perps
   hooks with three shared mechanisms:

     1. **Shared in-flight fetch dedup** — N concurrent callers asking for
        the same `(endpoint, query, variables)` reuse a single Promise
        so the upstream sees one round-trip instead of N.

     2. **Shared interval poller** — one `setInterval` per
        `(endpoint, query, variables)` tuple, regardless of how many
        components subscribe. The fastest requested cadence wins; new
        subscribers join the existing schedule + immediately receive the
        cached snapshot.

     3. **Visibility-aware pause** — a single `visibilitychange`
        listener freezes every poller when the tab is hidden and fires
        them all on focus. Background tabs stop generating traffic.

   For graphql-transport-ws subscriptions we additionally maintain ONE
   WebSocket per endpoint and multiplex `subscribe`/`next`/`complete`
   frames over it (similar to `useSidPaxMarketStream.ts`). This replaces
   the per-hook WebSocket pattern in `usePerfdbSubscriptions.ts`.

   Public surface:
     • `gqlEndpoint(url, opts?)` — declare an endpoint once.
     • `gqlQuery(endpoint, query, vars)` — one-shot deduped fetch.
     • `subscribeQuery({ endpoint, query, variables, intervalMs, onData,
                          onError, immediate })` — shared polled
        subscription. Returns an unsubscribe fn.
     • `subscribeStream({ endpoint, query, variables, onData, onError })`
        — multiplexed graphql-transport-ws subscription. Returns an
        unsubscribe fn.

   Hooks should never call `setInterval` against `/graphql` directly —
   always go through `subscribeQuery` / `subscribeStream`.
   ══════════════════════════════════════════════════════════════════════ */

// ── Endpoint config ───────────────────────────────────────────────────

export interface GraphQLEndpoint {
  /** HTTPS endpoint for queries (POST + JSON body). */
  url: string;
  /** Optional graphql-transport-ws endpoint for subscriptions. */
  wsUrl?: string;
  /**
   * Function returning auth headers at fetch time. Called for every
   * request so token refresh is picked up automatically. Must be cheap.
   */
  authHeaders?: () => Record<string, string>;
  /** Human label for logging / debugging. */
  name?: string;
}

export function gqlEndpoint(url: string, opts: Partial<GraphQLEndpoint> = {}): GraphQLEndpoint {
  return { url, ...opts };
}

// ── Stable cache key derivation ───────────────────────────────────────

/**
 * Stable JSON.stringify with sorted object keys so `{a:1,b:2}` and
 * `{b:2,a:1}` map to the same cache key. Cheap; acceptable for the
 * shapes (small variable bags) we deal with here.
 */
function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return "[" + value.map(stableStringify).join(",") + "]";
  const obj = value as Record<string, unknown>;
  const keys = Object.keys(obj).sort();
  return "{" + keys.map((k) => JSON.stringify(k) + ":" + stableStringify(obj[k])).join(",") + "}";
}

function makeKey(endpoint: GraphQLEndpoint, query: string, variables: unknown): string {
  return endpoint.url + "|" + query + "|" + stableStringify(variables ?? null);
}

// ── One-shot deduped fetch ────────────────────────────────────────────

const inFlight = new Map<string, Promise<any>>();

async function rawFetch<T>(
  endpoint: GraphQLEndpoint,
  query: string,
  variables?: Record<string, unknown>,
): Promise<T> {
  const auth = endpoint.authHeaders?.() ?? {};
  const res = await fetch(endpoint.url, {
    method: "POST",
    idempotent: /^\s*(query\b|\{)/.test(query),
    headers: { "Content-Type": "application/json", ...auth },
    body: JSON.stringify({ query, variables }),
  });
  if (!res.ok) {
    throw new Error(`GraphQL ${endpoint.name ?? endpoint.url} ${res.status}`);
  }
  const json = await res.json();
  if (json.errors?.length) {
    throw new Error(json.errors[0]?.message ?? "GraphQL error");
  }
  return json.data as T;
}

/**
 * One-shot GraphQL query with concurrent-call dedup. Two callers asking
 * for the same `(endpoint, query, variables)` while a fetch is in
 * flight reuse the same Promise.
 */
export function gqlQuery<T>(
  endpoint: GraphQLEndpoint,
  query: string,
  variables?: Record<string, unknown>,
): Promise<T> {
  const key = makeKey(endpoint, query, variables);
  const existing = inFlight.get(key);
  if (existing) return existing as Promise<T>;
  const p = rawFetch<T>(endpoint, query, variables).finally(() => {
    // Clear once settled so the next call starts fresh; we cache
    // *successful* results separately inside the polled-query layer.
    if (inFlight.get(key) === p) inFlight.delete(key);
  });
  inFlight.set(key, p);
  return p;
}

// ── Shared polled query ───────────────────────────────────────────────

export interface PolledSubscriptionOptions<T> {
  endpoint: GraphQLEndpoint;
  query: string;
  variables?: Record<string, unknown>;
  /**
   * Polling cadence in ms. The fastest requested cadence wins across
   * subscribers — joining a slower subscriber to an already-fast poller
   * does not slow things down.
   */
  intervalMs: number;
  /** Fired with the latest data on every successful poll + on subscribe (cached snapshot). */
  onData: (data: T) => void;
  /** Fired with the error on every failing poll. Optional. */
  onError?: (err: Error) => void;
  /**
   * If true (default) the new subscriber gets the cached snapshot
   * synchronously on subscribe and triggers an immediate fetch when the
   * cache is empty. Set false to wait for the next scheduled tick.
   */
  immediate?: boolean;
}

interface PollerState {
  endpoint: GraphQLEndpoint;
  query: string;
  variables?: Record<string, unknown>;
  intervalMs: number;
  timer: CancelTask | null;
  fetching?: boolean;
  /** Last successful payload, replayed to new subscribers on join. */
  cached: any;
  hasCached: boolean;
  lastFetchAt: number;
  subscribers: Set<{
    onData: (data: any) => void;
    onError?: (err: Error) => void;
    intervalMs: number;
  }>;
}

const pollers = new Map<string, PollerState>();

function broadcast(p: PollerState, data: any) {
  p.cached = data;
  p.hasCached = true;
  p.lastFetchAt = Date.now();
  p.subscribers.forEach((s) => {
    try { s.onData(data); } catch { /* ignore subscriber errors */ }
  });
}

function broadcastError(p: PollerState, err: Error) {
  p.subscribers.forEach((s) => {
    try { s.onError?.(err); } catch { /* ignore */ }
  });
}

function fastestInterval(p: PollerState): number {
  let min = Infinity;
  p.subscribers.forEach((s) => { if (s.intervalMs < min) min = s.intervalMs; });
  return Number.isFinite(min) ? min : p.intervalMs;
}

function runFetch(p: PollerState, propagateFailure = false) {
  // Skip if no subscribers (race with unsubscribe) or tab is hidden.
  if (p.subscribers.size === 0) return;
  if (!isPageActive() || p.fetching) return;
  p.fetching = true;
  return gqlQuery(p.endpoint, p.query, p.variables)
    .then((data) => broadcast(p, data))
    .catch((err) => { broadcastError(p, err instanceof Error ? err : new Error(String(err))); if (propagateFailure) throw err; })
    .finally(() => { p.fetching = false; });
}

function startTimer(p: PollerState) {
  if (p.timer) {
    p.timer();
    p.timer = null;
  }
  p.intervalMs = fastestInterval(p);
  // intervalMs ≤ 0 is the documented "one-shot, no polling" sentinel —
  // fetch on subscribe + visibility-change but skip the timer entirely.
  p.timer = activePoll(() => runFetch(p, true), p.intervalMs, false);
}

function stopTimer(p: PollerState) {
  if (p.timer) {
    p.timer();
    p.timer = null;
  }
}

/**
 * Subscribe to a shared polled GraphQL query. N components asking for
 * the same `(endpoint, query, variables)` share a single timer +
 * fetch cycle. Returns an unsubscribe function — when the last
 * subscriber leaves the timer is torn down.
 */
export function subscribeQuery<T>(opts: PolledSubscriptionOptions<T>): () => void {
  const { endpoint, query, variables, intervalMs, onData, onError, immediate = true } = opts;
  const key = makeKey(endpoint, query, variables);
  let p = pollers.get(key);
  if (!p) {
    p = {
      endpoint,
      query,
      variables,
      intervalMs,
      timer: null,
      cached: undefined,
      hasCached: false,
      lastFetchAt: 0,
      subscribers: new Set(),
    };
    pollers.set(key, p);
  }

  const sub = { onData: onData as (d: any) => void, onError, intervalMs };
  p.subscribers.add(sub);

  // Replay cached snapshot to the new subscriber on the next microtask
  // so the caller's React `setState` runs after the current render.
  if (immediate && p.hasCached) {
    const cached = p.cached;
    queueMicrotask(() => {
      try { sub.onData(cached); } catch { /* ignore */ }
    });
  }

  // First subscriber kicks off the timer + an immediate fetch.
  if (p.subscribers.size === 1) {
    startTimer(p);
    if (immediate) runFetch(p);
  } else if (intervalMs < p.intervalMs) {
    // A faster subscriber joined — speed up the timer.
    startTimer(p);
  }

  return () => {
    p!.subscribers.delete(sub);
    if (p!.subscribers.size === 0) {
      stopTimer(p!);
      pollers.delete(key);
    } else if (sub.intervalMs === p!.intervalMs) {
      // The slowest-required-fastest subscriber left; we may be able
      // to relax the cadence.
      const next = fastestInterval(p!);
      if (next > p!.intervalMs) startTimer(p!);
    }
  };
}

// Reconcile query snapshots after the subscription channel reconnects.
onStreamResync(url => {
  pollers.forEach(p => { if (p.endpoint.wsUrl === url) void runFetch(p); });
});

// ── Multiplexed graphql-transport-ws subscriptions ────────────────────

interface WsConnection {
  endpoint: GraphQLEndpoint;
  ws: WebSocket | null;
  ready: boolean;
  /** subscription id → metadata. ID is allocated on subscribe. */
  subs: Map<string, {
    query: string;
    variables?: Record<string, unknown>;
    onData: (data: any) => void;
    onError?: (err: Error) => void;
  }>;
  reconnectTimer: CancelTask | null;
  hasConnected: boolean;
  backoffMs: number;
  nextId: number;
}

const connections = new Map<string, WsConnection>();

function getOrCreateConnection(endpoint: GraphQLEndpoint): WsConnection | null {
  if (!endpoint.wsUrl) return null;
  if (typeof window === "undefined") return null;
  let conn = connections.get(endpoint.wsUrl);
  if (conn) return conn;
  conn = {
    endpoint,
    ws: null,
    ready: false,
    subs: new Map(),
    reconnectTimer: null,
    hasConnected: false,
    backoffMs: 1000,
    nextId: 1,
  };
  connections.set(endpoint.wsUrl, conn);
  const created = conn;
  queueMicrotask(() => { if (created.subs.size) openSocket(created); });
  return conn;
}

function openSocket(conn: WsConnection) {
  if (!conn.endpoint.wsUrl || !conn.subs.size) return;
  if (!isPageActive()) { scheduleReconnect(conn); return; }
  let ws: WebSocket;
  try {
    ws = new WebSocket(conn.endpoint.wsUrl, "graphql-transport-ws");
  } catch {
    scheduleReconnect(conn);
    return;
  }
  conn.ws = ws;
  watchSocket(ws, () => { conn.backoffMs = 1000; }, { type: "ping" });
  conn.ready = false;

  ws.onopen = () => {
    try {
      ws.send(JSON.stringify({ type: "connection_init" }));
    } catch {
      // closed between open + send; onclose will reconnect.
    }
  };

  ws.onmessage = (ev) => {
    let msg: any;
    try { msg = JSON.parse(ev.data as string); } catch { return; }
    switch (msg.type) {
      case "connection_ack": {
        conn.ready = true;
        if (conn.hasConnected) notifyStreamResync(conn.endpoint.wsUrl!);
        conn.hasConnected = true;
        // Re-send all subscriptions on (re)connect.
        conn.subs.forEach((sub, id) => {
          try {
            ws.send(JSON.stringify({
              id,
              type: "subscribe",
              payload: { query: sub.query, variables: sub.variables },
            }));
          } catch { /* ignore */ }
        });
        break;
      }
      case "next": {
        const sub = conn.subs.get(msg.id);
        if (sub && msg.payload?.data) {
          try { sub.onData(msg.payload.data); } catch { /* ignore */ }
        }
        break;
      }
      case "error": {
        const sub = conn.subs.get(msg.id);
        const err = new Error(
          Array.isArray(msg.payload)
            ? (msg.payload[0]?.message ?? "subscription error")
            : "subscription error",
        );
        if (sub) try { sub.onError?.(err); } catch { /* ignore */ }
        break;
      }
      case "ping": {
        try { ws.send(JSON.stringify({ type: "pong" })); } catch { /* ignore */ }
        break;
      }
      // complete / pong — no-op
    }
  };

  ws.onclose = () => {
    conn.ready = false;
    conn.ws = null;
    if (conn.subs.size > 0) scheduleReconnect(conn);
  };

  ws.onerror = () => {
    // onclose will fire next; nothing to do here.
  };
}

function scheduleReconnect(conn: WsConnection) {
  if (conn.reconnectTimer) return;
  const delay = Math.min(conn.backoffMs, 30_000);
  conn.backoffMs = Math.min(conn.backoffMs * 2, 30_000);
  conn.reconnectTimer = scheduleActive(() => {
    conn.reconnectTimer = null;
    openSocket(conn);
  }, delay, conn.endpoint.wsUrl);
}

export interface StreamSubscriptionOptions<T> {
  endpoint: GraphQLEndpoint;
  query: string;
  variables?: Record<string, unknown>;
  onData: (data: T) => void;
  onError?: (err: Error) => void;
}

/**
 * Subscribe to a graphql-transport-ws subscription on the endpoint's
 * `wsUrl`. All subscribers across the app share a single WebSocket per
 * endpoint; subscribe/unsubscribe frames are multiplexed.
 *
 * Returns a cleanup function that sends `complete` upstream and tears
 * down the socket once the last subscriber leaves.
 */
export function subscribeStream<T>(opts: StreamSubscriptionOptions<T>): () => void {
  const conn = getOrCreateConnection(opts.endpoint);
  if (!conn) return () => {};

  const id = String(conn.nextId++);
  conn.subs.set(id, {
    query: opts.query,
    variables: opts.variables,
    onData: opts.onData as (d: any) => void,
    onError: opts.onError,
  });

  if (conn.ready && conn.ws?.readyState === WebSocket.OPEN) {
    try {
      conn.ws.send(JSON.stringify({
        id,
        type: "subscribe",
        payload: { query: opts.query, variables: opts.variables },
      }));
    } catch { /* re-sent on reconnect */ }
  }

  return () => {
    conn.subs.delete(id);
    if (conn.ready && conn.ws?.readyState === WebSocket.OPEN) {
      try { conn.ws.send(JSON.stringify({ id, type: "complete" })); } catch { /* ignore */ }
    }
    // Last subscriber for this endpoint? Close the socket so we don't
    // hold an idle connection forever.
    if (conn.subs.size === 0 && conn.endpoint.wsUrl) {
      try { conn.ws?.close(); } catch { /* ignore */ }
      conn.ws = null;
      if (conn.reconnectTimer) {
        conn.reconnectTimer();
        conn.reconnectTimer = null;
      }
      connections.delete(conn.endpoint.wsUrl);
    }
  };
}
