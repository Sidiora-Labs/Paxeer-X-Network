'use client';
import { useCandles, type Candle, type CandleInterval } from '@/core/market/use-candles';
import { loadable, type Loadable } from '@/core/market/unsupported';
import type { PoolStats } from '@/core/market/use-stats-batch';
import { priceNumber } from '@/page-modules/token/market-presentation';
import type { Trade } from '../../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';

export type StatsWindow = '5m' | '1h' | '6h' | '24h';
export const statsWindows: readonly StatsWindow[] = ['5m', '1h', '6h', '24h'];

interface WindowSpec { interval: CandleInterval; intervalMs: number; buckets: number }
const minute = 60_000;
export const windowSpecs: Record<StatsWindow, WindowSpec> = {
  '5m': { interval: '1m', intervalMs: minute, buckets: 5 },
  '1h': { interval: '5m', intervalMs: 5 * minute, buckets: 12 },
  '6h': { interval: '15m', intervalMs: 15 * minute, buckets: 24 },
  '24h': { interval: '1h', intervalMs: 60 * minute, buckets: 24 },
};

/** Bucket-aligned half-open range: the last `buckets` candle buckets including the current one. */
export function windowRange(window: StatsWindow, now: number): { from: number; to: number } {
  const spec = windowSpecs[window];
  const current = Math.floor(now / spec.intervalMs) * spec.intervalMs;
  return { from: current - (spec.buckets - 1) * spec.intervalMs, to: current + spec.intervalMs };
}

type Position = Trade['position'];

/** Chain order of two positions: negative when `a` is older. */
export function comparePositions(a: Pick<Position, 'height' | 'transactionIndex' | 'logIndex'>, b: Pick<Position, 'height' | 'transactionIndex' | 'logIndex'>): number {
  for (const field of ['height', 'transactionIndex', 'logIndex'] as const) {
    const x = BigInt(a[field]), y = BigInt(b[field]);
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

export interface SideCounts {
  buys: number;
  sells: number;
  source: 'pool-stats' | 'fills';
  /** Fills only: true when the retained history reaches back to the window's first fill. */
  complete: boolean;
  /** Fills only: the retained fills counted in this window. */
  retained: number;
  /** Fills only, partial coverage: start of the candle bucket holding the oldest retained fill, or null when no bucket holds it. */
  coveredSince: string | null;
  /** Fills only, partial coverage: the oldest retained fill's block height. */
  oldestHeight: string | null;
}

export interface WindowStats {
  window: StatsWindow;
  /** Start of the first candle bucket in the window (ISO). */
  since: string;
  volumeRaw: bigint;
  buyUsdlRaw: bigint;
  sellUsdlRaw: bigint;
  netRaw: bigint;
  trades: bigint;
  changePct: number | null;
  partialCandles: number;
  counts: SideCounts;
}

function countFills(candles: readonly Candle[], fills: readonly Trade[], trades: bigint): SideCounts {
  const base = { source: 'fills' as const, coveredSince: null, oldestHeight: null };
  if (candles.length === 0) return { ...base, buys: 0, sells: 0, complete: true, retained: 0 };
  const boundary = candles.reduce((oldest, candle) => (comparePositions(candle.firstPosition, oldest) < 0 ? candle.firstPosition : oldest), candles[0].firstPosition);
  let buys = 0, sells = 0, oldest: Position | null = null, reachesBoundary = false;
  for (const fill of fills) {
    const order = comparePositions(fill.position, boundary);
    if (order <= 0) reachesBoundary = true;
    if (order < 0) continue;
    if (fill.side === 'buy') buys++; else sells++;
    if (!oldest || comparePositions(fill.position, oldest) < 0) oldest = fill.position;
  }
  const retained = buys + sells;
  const complete = reachesBoundary || BigInt(retained) >= trades;
  if (complete) return { ...base, buys, sells, complete, retained };
  const holder = oldest ? candles.find(candle => comparePositions(candle.firstPosition, oldest) <= 0 && comparePositions(oldest, candle.lastPosition) <= 0) : undefined;
  return { ...base, buys, sells, complete, retained, coveredSince: holder?.start ?? null, oldestHeight: oldest?.height ?? null };
}

/**
 * Window figures from served data only. USDL volume, buy / sell USDL, trade
 * count and price change come from the window's IF_CANDLES buckets. Side
 * counts come from IF_POOL_STATS for 24h (its own rolling window), and for
 * shorter windows from the retained fills newer than the window's first
 * fill (Candle.firstPosition), with explicit coverage when the retained
 * history does not reach that far back. Nothing is extrapolated.
 */
export function deriveWindowStats(window: StatsWindow, candles: readonly Candle[], fills: readonly Trade[], stats: PoolStats | null, from: number): WindowStats {
  const ordered = [...candles].sort((a, b) => a.start.localeCompare(b.start));
  let volumeRaw = 0n, buyUsdlRaw = 0n, sellUsdlRaw = 0n, trades = 0n, partialCandles = 0;
  for (const candle of ordered) {
    volumeRaw += BigInt(candle.volumeUsdlRaw);
    buyUsdlRaw += BigInt(candle.buyVolumeUsdlRaw);
    sellUsdlRaw += BigInt(candle.sellVolumeUsdlRaw);
    trades += BigInt(candle.trades);
    if (candle.coverage === 'partial') partialCandles++;
  }
  const open = ordered.length ? priceNumber(ordered[0].open) : null;
  const close = ordered.length ? priceNumber(ordered[ordered.length - 1].close) : null;
  const changePct = open !== null && close !== null && open > 0 ? ((close - open) / open) * 100 : null;
  const counts: SideCounts = window === '24h' && stats
    ? { buys: Number(stats.buys24h), sells: Number(stats.sells24h), source: 'pool-stats', complete: true, retained: 0, coveredSince: null, oldestHeight: null }
    : countFills(ordered, fills, trades);
  return { window, since: new Date(from).toISOString(), volumeRaw, buyUsdlRaw, sellUsdlRaw, netRaw: buyUsdlRaw - sellUsdlRaw, trades, changePct, partialCandles, counts };
}

export interface WindowSeries { window: StatsWindow; from: number; candles: Loadable<readonly Candle[]> }

/** One IF_CANDLES read per window; bounds are bucket-aligned so the query keys only move at bucket edges. */
export function useWindowCandles(poolAddress: string, now: number): Record<StatsWindow, WindowSeries> {
  const r5 = windowRange('5m', now), r1 = windowRange('1h', now), r6 = windowRange('6h', now), r24 = windowRange('24h', now);
  const q5 = useCandles(poolAddress, windowSpecs['5m'].interval, { ...r5, limit: windowSpecs['5m'].buckets + 1 });
  const q1 = useCandles(poolAddress, windowSpecs['1h'].interval, { ...r1, limit: windowSpecs['1h'].buckets + 1 });
  const q6 = useCandles(poolAddress, windowSpecs['6h'].interval, { ...r6, limit: windowSpecs['6h'].buckets + 1 });
  const q24 = useCandles(poolAddress, windowSpecs['24h'].interval, { ...r24, limit: windowSpecs['24h'].buckets + 1 });
  const series = (window: StatsWindow, from: number, query: typeof q5): WindowSeries => {
    const state = loadable(query, 'IF_CANDLES');
    return { window, from, candles: state.state === 'ready' ? { state: 'ready', data: state.data.data.items } : state };
  };
  return { '5m': series('5m', r5.from, q5), '1h': series('1h', r1.from, q1), '6h': series('6h', r6.from, q6), '24h': series('24h', r24.from, q24) };
}
