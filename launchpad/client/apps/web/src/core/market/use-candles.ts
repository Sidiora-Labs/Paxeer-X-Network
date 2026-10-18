'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { OperationInput, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { marketAddress } from '@/page-modules/token/market-session';
import { useOperations } from './operations';

export type CandlesResponse = V2ContractTypes['CandlesResponse'];
export type Candle = V2ContractTypes['Candle'];
export type CandleInterval = OperationInput<'IF_CANDLES'>['query']['interval'];
export interface CandleRange { from: Date | string | number; to: Date | string | number; limit?: number }

export const maxCanonicalCandles = 2000;

/** Canonical UTC millisecond bound the handler requires (accounting_v2.go:210-215). */
export function canonicalBound(value: Date | string | number): string {
  const date = value instanceof Date ? value : new Date(value);
  if (!Number.isFinite(date.getTime()) || date.getTime() < 0) throw new RangeError('Invalid candle bound');
  return date.toISOString();
}

export function validateCandles(response: CandlesResponse, pool: string, interval: CandleInterval): CandlesResponse {
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Candle integrity quarantined');
  if (response.data.items.length > maxCanonicalCandles) throw new RangeError('Candle page exceeds contract bound');
  const starts = new Set<string>();
  for (const candle of response.data.items) {
    if (marketAddress(candle.poolAddress) !== pool || candle.interval !== interval || candle.seriesModelVersion !== response.data.seriesModelVersion) throw new Error('Candle identity disagreement');
    if (starts.has(candle.start)) throw new Error('Duplicate candle bucket');
    starts.add(candle.start);
  }
  return response;
}

/** IF_CANDLES `GET /v2/pools/{p}/candles` (core/api/internal/rest/accounting_v2.go:33,47). */
export function useCandles(poolAddress: string | null | undefined, interval: CandleInterval, range: CandleRange): UseQueryResult<CandlesResponse> {
  const operations = useOperations();
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const from = canonicalBound(range.from), to = canonicalBound(range.to);
  const limit = range.limit ?? 500;
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > maxCanonicalCandles) throw new RangeError('Invalid candle limit');
  if (Date.parse(to) <= Date.parse(from)) throw new RangeError('Candle range must be half-open and non-empty');
  return useQuery({
    queryKey: queryKeys.candles(pool ?? '', interval, from, to, limit),
    queryFn: async ({ signal }) => validateCandles(await operations.call('IF_CANDLES', { path: { poolAddress: pool as string }, query: { interval, from, to, limit } }, { signal }), pool as string, interval),
    enabled: pool !== null,
    ...marketCachePolicy.candles,
  });
}
