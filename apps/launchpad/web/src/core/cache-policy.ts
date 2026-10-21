import { retryTransientOnly } from './transient-retry';

/**
 * Explicit stale-data tolerance policy (T03.2).
 *
 * On a trading UI, rendering stale data as if it were fresh is a correctness
 * bug — a price that is 60s old can mislead a trade decision. Previously each
 * `useQuery` picked its own `staleTime` as an undocumented magic number. This
 * module makes the tolerance an EXPLICIT, named decision so every query states
 * how stale its data may be, and the rationale lives in one place.
 *
 * Field meanings (TanStack Query semantics):
 *   - staleTime       how long fetched data is considered FRESH (no refetch on
 *                     mount / window-focus / reconnect while fresh).
 *   - gcTime          how long an UNUSED cache entry is kept before garbage
 *                     collection (formerly `cacheTime`).
 *   - refetchInterval background poll cadence; omitted = no polling.
 *
 * Tiers are ordered by how financially sensitive the data is:
 *
 *   REALTIME  Prices / pool stats that drive trade decisions. The live
 *             WebSocket stream is the primary source; this REST poll is the
 *             backstop, so the stale window is short and it polls.
 *   FAST      Frequently-changing aggregates (holders, recent transactions).
 *   STANDARD  User-action-driven data (profiles, balances) — refetch on focus.
 *   SLOW      Rarely-changing data (rankings, leaderboards).
 *   STATIC    Effectively immutable for a session (token metadata, config).
 *
 * Note: `refetchIntervalInBackground` is intentionally left `false` at the
 * QueryClient default so hidden tabs never poll; the WsManager already handles
 * resync-on-visible for live streams.
 */
export const cachePolicy = {
  REALTIME: { staleTime: 5_000, gcTime: 60_000, refetchInterval: 10_000 },
  FAST: { staleTime: 10_000, gcTime: 120_000, refetchInterval: 30_000 },
  STANDARD: { staleTime: 30_000, gcTime: 5 * 60_000 },
  SLOW: { staleTime: 5 * 60_000, gcTime: 30 * 60_000 },
  STATIC: { staleTime: Number.POSITIVE_INFINITY, gcTime: Number.POSITIVE_INFINITY },
} as const;

export type CacheTier = keyof typeof cachePolicy;

/**
 * Per-family tolerance for the typed market data layer (`core/market/*`).
 * Every family retries only failures the server marks transient (retryable
 * 503 / 429 with retryAfterMs, a dropped connection) on the server's delay,
 * bounded by core/transient-retry.ts. Any other failed contract request is
 * surfaced at once as the real error (or an Unsupported marker).
 * Live deltas, not polling, refresh pool-scoped families; the stale windows
 * are therefore short and no family polls.
 */
export const marketCachePolicy = {
  poolSnapshot: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  poolDetail: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  statsBatch: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  rankings: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  candles: { staleTime: 15_000, gcTime: 60_000, ...retryTransientOnly },
  sparkline: { staleTime: 60_000, gcTime: 5 * 60_000, ...retryTransientOnly },
  holders: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  profile: { staleTime: 60_000, gcTime: 10 * 60_000, ...retryTransientOnly },
  walletIdentity: { staleTime: 5 * 60_000, gcTime: 30 * 60_000, ...retryTransientOnly },
  leaderboard: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  feed: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  callouts: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  watchlistRows: { staleTime: 10_000, gcTime: 30_000, ...retryTransientOnly },
  metadata: { staleTime: 30_000, gcTime: 30_000, ...retryTransientOnly },
  bounties: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  holderRewards: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  cups: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  profileReplies: { staleTime: 30_000, gcTime: 60_000, ...retryTransientOnly },
  calloutHistory: { staleTime: 60_000, gcTime: 5 * 60_000, ...retryTransientOnly },
} as const;

export type MarketCacheFamily = keyof typeof marketCachePolicy;
