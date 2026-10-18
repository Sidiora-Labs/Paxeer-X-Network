'use client';

/**
 * Bridge the public v2 market stream (core/api `/v2/snapshots` + WS
 * `/v2/stream`) to TanStack Query cache invalidation — the push replacement for
 * `refetchInterval` polling. Invalidation is THROTTLED (leading + trailing, at
 * most once per `throttleMs`) so a burst of frames cannot melt a snapshot
 * endpoint, and `invalidateQueries` only refetches mounted queries.
 *
 * The first topic snapshot installed after subscribing matches what the REST
 * query just loaded, so it is not an invalidation; every later snapshot (a
 * reconnect resync) and every live event is.
 *
 *   - `useRefetchOnPoolEvent` / `useReloadOnPoolEvent` — one pool's
 *     market.<family>.<pool> topics (default trades + stats).
 *   - `useRefetchOnAnyEvent` / `useReloadOnAnyEvent` — platform-wide changes,
 *     carried by the ranking projections (default market.rankings.trending,
 *     recomputed as trades land). There is no all-pools trade topic in v2.
 */

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { QueryKey } from '@tanstack/react-query';
import { useMarketTopics } from '@/core/realtime/use-data-stream';
import { marketTopics, type MarketPoolFamily } from '@/core/realtime/data-stream';

/**
 * Returns a throttled function: invokes `fn` immediately on the leading edge,
 * then at most once per `intervalMs`, with a trailing call if events arrived
 * during the cooldown.
 */
function useThrottledCallback(fn: () => void, intervalMs: number): () => void {
  const fnRef = useRef(fn);
  useEffect(() => {
    fnRef.current = fn;
  });

  const lastRun = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  return useCallback(() => {
    const now = Date.now();
    const elapsed = now - lastRun.current;
    if (elapsed >= intervalMs) {
      lastRun.current = now;
      fnRef.current();
      return;
    }
    if (timer.current) return;
    timer.current = setTimeout(() => {
      timer.current = null;
      lastRun.current = Date.now();
      fnRef.current();
    }, intervalMs - elapsed);
  }, [intervalMs]);
}

/** Default pool topic families whose frames re-validate a pool snapshot. */
export const STATS_FAMILIES: readonly MarketPoolFamily[] = ['trades', 'stats'];

/** Default platform-wide topic: the trending ranking projection. */
export const PLATFORM_TOPICS: readonly string[] = [marketTopics.rankings('trending')];

function poolTopics(poolAddress: string | null | undefined, families: readonly MarketPoolFamily[]): string[] {
  if (!poolAddress || !/^0x[0-9a-fA-F]{40}$/.test(poolAddress)) return [];
  return families.map(family => marketTopics.pool(family, poolAddress));
}

/** Calls `trigger` on every live event and on every snapshot after the first. */
function useTopicTrigger(topics: readonly string[], trigger: () => void, enabled: boolean): void {
  const installed = useRef(false);
  const identity = topics.join(',');
  useEffect(() => { installed.current = false; }, [identity, enabled]);
  useMarketTopics(topics, {
    enabled,
    onSnapshot: () => {
      if (installed.current) trigger();
      installed.current = true;
    },
    onEvent: () => trigger(),
  });
}

export interface RefetchOnPoolEventOptions {
  poolAddress: string | undefined | null;
  /** Query key(s) to invalidate (prefix-matched). */
  queryKeys: QueryKey[];
  /** Topic families to listen on. Defaults to trades + stats. */
  families?: readonly MarketPoolFamily[];
  /** Minimum ms between refetches (throttle). Default 4s for a single pool. */
  throttleMs?: number;
  enabled?: boolean;
}

/** Re-validate REST snapshot queries for one pool when its v2 topics change. */
export function useRefetchOnPoolEvent(opts: RefetchOnPoolEventOptions): void {
  const { poolAddress, queryKeys: keys, families = STATS_FAMILIES, throttleMs = 4_000, enabled = true } = opts;
  const qc = useQueryClient();
  const keysRef = useRef(keys);
  useEffect(() => {
    keysRef.current = keys;
  });
  const invalidate = useThrottledCallback(() => {
    for (const key of keysRef.current) void qc.invalidateQueries({ queryKey: key });
  }, throttleMs);
  const topics = useMemo(() => poolTopics(poolAddress, families), [poolAddress, families]);
  useTopicTrigger(topics, invalidate, enabled && topics.length > 0);
}

export interface ReloadOnPoolEventOptions {
  families?: readonly MarketPoolFamily[];
  throttleMs?: number;
  enabled?: boolean;
}

/** Imperative pool variant for non-React-Query consumers: calls `reload` (throttled). */
export function useReloadOnPoolEvent(
  poolAddress: string | undefined | null,
  reload: () => void,
  opts?: ReloadOnPoolEventOptions,
): void {
  const { families = STATS_FAMILIES, throttleMs = 5_000, enabled = true } = opts ?? {};
  const throttled = useThrottledCallback(reload, throttleMs);
  const topics = useMemo(() => poolTopics(poolAddress, families), [poolAddress, families]);
  useTopicTrigger(topics, throttled, enabled && topics.length > 0);
}

export interface RefetchOnAnyEventOptions {
  /** Query key(s) to invalidate (prefix-matched). */
  queryKeys: QueryKey[];
  /** v2 topics to listen on. Defaults to market.rankings.trending. */
  topics?: readonly string[];
  /** Minimum ms between refetches (throttle). Default 15s. */
  throttleMs?: number;
  enabled?: boolean;
}

/** Re-validate aggregate snapshot queries (rankings, trending, batch stats, platform) on platform-wide changes. */
export function useRefetchOnAnyEvent(opts: RefetchOnAnyEventOptions): void {
  const { queryKeys: keys, topics = PLATFORM_TOPICS, throttleMs = 15_000, enabled = true } = opts;
  const qc = useQueryClient();
  const keysRef = useRef(keys);
  useEffect(() => {
    keysRef.current = keys;
  });
  const invalidate = useThrottledCallback(() => {
    for (const key of keysRef.current) void qc.invalidateQueries({ queryKey: key });
  }, throttleMs);
  useTopicTrigger(topics, invalidate, enabled);
}

export interface ReloadOnAnyEventOptions {
  topics?: readonly string[];
  throttleMs?: number;
  enabled?: boolean;
}

/** Imperative platform-wide variant for non-React-Query consumers. */
export function useReloadOnAnyEvent(reload: () => void, opts?: ReloadOnAnyEventOptions): void {
  const { topics = PLATFORM_TOPICS, throttleMs = 15_000, enabled = true } = opts ?? {};
  const throttled = useThrottledCallback(reload, throttleMs);
  useTopicTrigger(topics, throttled, enabled);
}
