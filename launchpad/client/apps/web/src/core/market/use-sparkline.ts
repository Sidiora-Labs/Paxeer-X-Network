'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { Operations } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { priceNumber } from '@/page-modules/token/market-presentation';
import { useOperations } from './operations';
import { validateCandles, type Candle } from './use-candles';

export const sparklineHours = 24;

/** Small concurrency gate so a table of rows does not fan out unbounded candle requests. */
class Gate {
  private active = 0;
  private readonly queue: Array<() => void> = [];
  constructor(private readonly limit: number) {}
  async run<T>(task: () => Promise<T>): Promise<T> {
    if (this.active >= this.limit) await new Promise<void>(resolve => this.queue.push(resolve));
    this.active++;
    try { return await task(); }
    finally { this.active--; this.queue.shift()?.(); }
  }
}
const gate = new Gate(4);

/** Half-open window of the last 24 whole hours, aligned to the hour so every row shares one cache key per hour. */
export function sparklineWindow(now: number = Date.now()): { from: string; to: string } {
  const hour = 3_600_000;
  const to = Math.ceil(now / hour) * hour;
  return { from: new Date(to - sparklineHours * hour).toISOString(), to: new Date(to).toISOString() };
}

export interface Sparkline { closes: number[]; starts: string[]; generatedAt: string; coverage: 'complete' | 'partial' | 'empty' }

/** Closes ordered by bucket start; buckets with an unrepresentable price are dropped. */
export function sparklineFromCandles(items: readonly Candle[], generatedAt: string): Sparkline {
  const ordered = [...items].sort((a, b) => Date.parse(a.start) - Date.parse(b.start));
  const closes: number[] = [], starts: string[] = [];
  for (const candle of ordered) {
    const close = priceNumber(candle.close);
    if (close === null) continue;
    closes.push(close); starts.push(candle.start);
  }
  return { closes, starts, generatedAt, coverage: closes.length === 0 ? 'empty' : closes.length >= sparklineHours && ordered.every(c => c.coverage === 'complete') ? 'complete' : 'partial' };
}

export async function loadSparkline(operations: Operations, pool: string, window: { from: string; to: string }, signal?: AbortSignal): Promise<Sparkline> {
  const response = await gate.run(() => operations.call('IF_CANDLES', { path: { poolAddress: pool }, query: { interval: '1h', from: window.from, to: window.to, limit: sparklineHours } }, { signal }));
  validateCandles(response, pool, '1h');
  return sparklineFromCandles(response.data.items, response.generatedAt);
}

/** 24 x 1h closes for one pool via IF_CANDLES, batched through a small concurrency gate and cached per hour. */
export function useSparkline(poolAddress: string | null | undefined): UseQueryResult<Sparkline> {
  const operations = useOperations();
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const window = sparklineWindow();
  return useQuery({
    queryKey: queryKeys.sparkline(pool ?? '', window.from),
    queryFn: ({ signal }) => loadSparkline(operations, pool as string, window, signal),
    enabled: pool !== null,
    ...marketCachePolicy.sparkline,
  });
}
