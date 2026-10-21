"use client";
import { SPOT_USER_WS_URL } from "./useSpotUserStream";
import { activePoll, onStreamResync } from "@/core/net/realtime";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  spotApi,
  type SpotHoldingsResponse,
  type SpotOrder,
  type SpotOrdersResponse,
  type SpotPosition,
  type SpotPositionsResponse,
  type SpotPnlSummary,
  type SpotTrade,
  type SpotTradesResponse,
} from "@/core/spot/api";
import { subscribeSpotUserStream } from "./useSpotUserStream";

/* ══════════════════════════════════════════════════════════════════════
   `useSpotPortfolio(address)`

   One composite hook for every panel that needs the user's full SPOT
   trading context. Layers REST snapshots on top of the user-scoped
   WebSocket push channel so the UI is fresh without aggressive polling:

     · Initial load (parallel) — holdings + positions + open-orders +
       recent trades + closed-legs (first page) + pnl summary.
     · WS push (`order` stream)      → upsert into open-orders cache.
     · WS push (`trade` stream)      → prepend to trades cache + flag
                                       holdings + positions for refetch.
     · WS push (`position` stream)   → upsert into positions cache.
     · WS push (`pnl` stream)        → replace pnl summary cache.
     · WS push (`closed-leg` stream) → prepend to closed-legs cache.

   Holdings is the only field with **no** push channel exposed by the API,
   so we still poll holdings every 8 s while the hook is mounted.

   Cancellation: every fetch uses an AbortController scoped to the
   current address. Address change cancels in-flight requests.
   ══════════════════════════════════════════════════════════════════════ */

const HOLDINGS_POLL_MS = 8_000;
const TRADES_INITIAL_LIMIT = 50;
const CLOSED_LEGS_INITIAL_LIMIT = 50;
const ORDERS_INITIAL_LIMIT = 100;

export interface SpotPortfolioState {
  isConnected: boolean;
  isLoading: boolean;
  /** True once the initial REST snapshot has resolved (success or fail). */
  hasLoadedOnce: boolean;
  /** First load error, if the snapshot fetch failed entirely. */
  error: Error | null;
  // Snapshots
  holdings: SpotHoldingsResponse | null;
  positions: SpotPosition[];
  openOrders: SpotOrder[];
  recentTrades: SpotTrade[];
  pnlSummary: SpotPnlSummary | null;
  /** Last frame received per stream, for components that want raw events. */
  lastFrames: {
    trade: unknown;
    order: unknown;
    position: unknown;
    pnl: unknown;
    closedLeg: unknown;
  };
  /** Manual refresh (re-pulls everything except cursor-paged tables). */
  refresh: () => void;
}

const EMPTY_STATE: SpotPortfolioState = {
  isConnected: false,
  isLoading: false,
  hasLoadedOnce: false,
  error: null,
  holdings: null,
  positions: [],
  openOrders: [],
  recentTrades: [],
  pnlSummary: null,
  lastFrames: { trade: null, order: null, position: null, pnl: null, closedLeg: null },
  refresh: () => {},
};

/**
 * Extract the data payload from a user-scoped WS frame. The server wraps
 * most events under a `data` key but some legacy frames put the payload
 * at top level. We strip the routing fields and return whatever's left.
 */
function extractPayload(frame: Record<string, unknown>): unknown {
  if (frame == null) return null;
  if ("data" in frame && frame.data && typeof frame.data === "object") {
    return frame.data;
  }
  // Strip routing keys.
  const { stream: _s, address: _a, op: _op, type: _t, ...rest } = frame as {
    stream?: unknown;
    address?: unknown;
    op?: unknown;
    type?: unknown;
  } & Record<string, unknown>;
  return rest;
}

/**
 * Try to extract an order id from an `order` push payload. The schema
 * field name is `orderId` per swagger; we also accept `id` as a fallback
 * because the WS push protocol isn't formally documented.
 */
function orderIdOf(o: Partial<SpotOrder>): string | null {
  if (typeof o?.orderId === "string" && o.orderId.length > 0) return o.orderId;
  if (typeof o?.id === "string" || typeof o?.id === "number") return String(o.id);
  return null;
}

function tokenIdOf(p: Partial<SpotPosition>): string | null {
  if (typeof p?.token === "string" && p.token.length > 0) return p.token.toLowerCase();
  return null;
}

export function useSpotPortfolio(address: string | null | undefined): SpotPortfolioState {
  const isConnected = !!address;

  const [holdings, setHoldings] = useState<SpotHoldingsResponse | null>(null);
  const [positions, setPositions] = useState<SpotPosition[]>([]);
  const [openOrders, setOpenOrders] = useState<SpotOrder[]>([]);
  const [recentTrades, setRecentTrades] = useState<SpotTrade[]>([]);
  const [pnlSummary, setPnlSummary] = useState<SpotPnlSummary | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoadedOnce, setHasLoadedOnce] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);
  useEffect(() => onStreamResync(url => { if (address && url === SPOT_USER_WS_URL) setRefreshTick(n => n + 1); }), [address]);

  const lastTradeFrameRef = useRef<unknown>(null);
  const lastOrderFrameRef = useRef<unknown>(null);
  const lastPositionFrameRef = useRef<unknown>(null);
  const lastPnlFrameRef = useRef<unknown>(null);
  const lastClosedLegFrameRef = useRef<unknown>(null);

  // ── Initial REST snapshot (and on refreshTick) ───────────────────────
  useEffect(() => {
    if (!address) {
      setHoldings(null);
      setPositions([]);
      setOpenOrders([]);
      setRecentTrades([]);
      setPnlSummary(null);
      setIsLoading(false);
      setHasLoadedOnce(false);
      setError(null);
      return;
    }

    const ctrl = new AbortController();
    setIsLoading(true);
    setError(null);

    Promise.allSettled([
      spotApi.getHoldings(address, ctrl.signal),
      spotApi.getPositions(address, ctrl.signal),
      spotApi.getOrders(address, {
        status: "open",
        limit: ORDERS_INITIAL_LIMIT,
        signal: ctrl.signal,
      }),
      spotApi.getTrades(address, { limit: TRADES_INITIAL_LIMIT, signal: ctrl.signal }),
      spotApi.getPnlSummary(address, ctrl.signal),
    ]).then((results) => {
      if (ctrl.signal.aborted) return;
      const [hRes, posRes, ordRes, trRes, pnlRes] = results;
      if (hRes.status === "fulfilled") setHoldings(hRes.value);
      if (posRes.status === "fulfilled") setPositions((posRes.value as SpotPositionsResponse).positions);
      if (ordRes.status === "fulfilled") setOpenOrders((ordRes.value as SpotOrdersResponse).items);
      if (trRes.status === "fulfilled") setRecentTrades((trRes.value as SpotTradesResponse).items);
      if (pnlRes.status === "fulfilled") setPnlSummary(pnlRes.value);

      // If every request failed, surface the first error.
      const firstFail = results.find((r) => r.status === "rejected") as
        | PromiseRejectedResult
        | undefined;
      const allFailed = results.every((r) => r.status === "rejected");
      if (allFailed && firstFail) {
        setError(firstFail.reason instanceof Error ? firstFail.reason : new Error(String(firstFail.reason)));
      }
      setIsLoading(false);
      setHasLoadedOnce(true);
            // Closed legs are pulled on-demand by the panels that show them
      // — keeping the snapshot minimal here.
    });

    return () => ctrl.abort();
  }, [address, refreshTick]);

  // ── Holdings polling (no WS channel for holdings) ───────────────────
  useEffect(() => {
    if (!address) return;
    const ctrl = new AbortController();
    let cancelled = false;
    const poll = async () => {
      try {
        const next = await spotApi.getHoldings(address, ctrl.signal);
        if (!cancelled) setHoldings(next);
      } catch {
        /* swallow — keep last known holdings */
      }
    };
    const stopPoll = activePoll(poll, HOLDINGS_POLL_MS, false);
    return () => {
      cancelled = true;
      ctrl.abort();
      stopPoll();
    };
  }, [address]);

  // ── WebSocket push channels ─────────────────────────────────────────

  // `trade` — prepend to recent trades + invalidate holdings + refetch
  // positions/pnl summary because spot fills move every cached number.
  useEffect(() => {
    if (!address) return;
    const off = subscribeSpotUserStream("trade", address, (frame) => {
      lastTradeFrameRef.current = frame;
      const payload = extractPayload(frame);
      if (payload && typeof payload === "object") {
        const t = payload as SpotTrade;
        if (t && (typeof t.id === "string" || typeof t.id === "number")) {
          setRecentTrades((prev) => {
            // De-dupe by id.
            const id = String(t.id);
            const filtered = prev.filter((x) => String(x.id) !== id);
            return [t, ...filtered].slice(0, 200);
          });
        }
      }
      // Lazy refetch of derived data.
      const ctrl = new AbortController();
      void spotApi
        .getHoldings(address, ctrl.signal)
        .then((h) => setHoldings(h))
        .catch(() => {});
      void spotApi
        .getPositions(address, ctrl.signal)
        .then((p) => setPositions(p.positions))
        .catch(() => {});
      void spotApi
        .getPnlSummary(address, ctrl.signal)
        .then((p) => setPnlSummary(p))
        .catch(() => {});
    });
    return off;
  }, [address]);

  // `order` — upsert / remove from open orders. When status leaves
  // "open"/"activated", drop it from the open list.
  useEffect(() => {
    if (!address) return;
    const off = subscribeSpotUserStream("order", address, (frame) => {
      lastOrderFrameRef.current = frame;
      const payload = extractPayload(frame);
      if (!payload || typeof payload !== "object") return;
      const o = payload as SpotOrder;
      const id = orderIdOf(o);
      if (!id) return;
      setOpenOrders((prev) => {
        const filtered = prev.filter((x) => orderIdOf(x) !== id);
        const stillOpen = o.status === "open" || o.status === "activated";
        return stillOpen ? [o, ...filtered] : filtered;
      });
    });
    return off;
  }, [address]);

  // `position` — upsert in positions list.
  useEffect(() => {
    if (!address) return;
    const off = subscribeSpotUserStream("position", address, (frame) => {
      lastPositionFrameRef.current = frame;
      const payload = extractPayload(frame);
      if (!payload || typeof payload !== "object") return;
      const p = payload as SpotPosition;
      const id = tokenIdOf(p);
      if (!id) return;
      setPositions((prev) => {
        const filtered = prev.filter((x) => tokenIdOf(x) !== id);
        return [p, ...filtered];
      });
    });
    return off;
  }, [address]);

  // `pnl` — replace pnl summary if shape matches; else trigger a REST
  // refetch so we never drift.
  useEffect(() => {
    if (!address) return;
    const off = subscribeSpotUserStream("pnl", address, (frame) => {
      lastPnlFrameRef.current = frame;
      const payload = extractPayload(frame);
      if (payload && typeof payload === "object") {
        setPnlSummary(payload as SpotPnlSummary);
        return;
      }
      const ctrl = new AbortController();
      void spotApi
        .getPnlSummary(address, ctrl.signal)
        .then((p) => setPnlSummary(p))
        .catch(() => {});
    });
    return off;
  }, [address]);

  // `closed-leg` — store latest frame; the closed-legs panel reads this
  // ref and decides whether to refetch its first page.
  useEffect(() => {
    if (!address) return;
    const off = subscribeSpotUserStream("closed-leg", address, (frame) => {
      lastClosedLegFrameRef.current = frame;
    });
    return off;
  }, [address]);

  const refresh = useCallback(() => setRefreshTick((t) => t + 1), []);

  const lastFrames = useMemo(
    () => ({
      trade: lastTradeFrameRef.current,
      order: lastOrderFrameRef.current,
      position: lastPositionFrameRef.current,
      pnl: lastPnlFrameRef.current,
      closedLeg: lastClosedLegFrameRef.current,
    }),
    // refreshTick used as a re-derive trigger so components can pick
    // up the latest refs on a forced refresh. Frames mutate refs and
    // intentionally bypass React updates; consumers that need live
    // frames should subscribe directly via `subscribeSpotUserStream`.
    [refreshTick],
  );

  if (!isConnected) {
    return { ...EMPTY_STATE, refresh };
  }

  return {
    isConnected,
    isLoading,
    hasLoadedOnce,
    error,
    holdings,
    positions,
    openOrders,
    recentTrades,
    pnlSummary,
    lastFrames,
    refresh,
  };
}
