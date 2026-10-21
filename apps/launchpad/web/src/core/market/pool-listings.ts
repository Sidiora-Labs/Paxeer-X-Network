'use client';
import { useQuery, useQueryClient, type QueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { Operations, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { marketAddress, marketLimits } from '@/page-modules/token/market-session';
import { scaleRawInteger } from './market-units';
import { useMarketClient } from './client';
import { useOperations } from './operations';

/** v2 Pool; core/api also emits the optional registry `createdAt` (catalog Pool fields `createdAt:UTC?`). */
export type Pool = V2ContractTypes['Pool'] & { createdAt?: string };
export type PoolListSort = 'new' | 'volume' | 'trending';

/**
 * The immutable registry facts of a pool as IF_POOL_LIST / IF_POOL_DETAIL
 * serve them (core/api/internal/rest/pool_reads.go): its token, its creator
 * and its creation time. They never change, so they are cached per pool for
 * the session.
 */
export interface PoolListing {
  poolAddress: string;
  tokenAddress: string;
  creator: string;
  /** Block time of the creation event in ms; null when the registry row carries none. */
  createdAtMs: number | null;
}

/** Fresh IF_POOL_DETAIL read: the listing plus the current spot price derived from the served reserves. */
export interface PoolDetailListing extends PoolListing {
  /** Decimal USDL per whole token (effectivePricingUsdlReserveRaw / tokenReserveRaw at their decimals); null without verified token decimals or reserve. */
  spotPrice: string | null;
}

const isPool = (value: string) => /^0x[0-9a-fA-F]{40}$/.test(value);
const PRICE_DECIMALS = 18;

export function poolListing(pool: Pool): PoolListing {
  const created = pool.createdAt ? Date.parse(pool.createdAt) : NaN;
  return {
    poolAddress: marketAddress(pool.poolAddress),
    tokenAddress: marketAddress(pool.tokenAddress),
    creator: marketAddress(pool.creator),
    createdAtMs: Number.isFinite(created) && created > 0 ? created : null,
  };
}

/**
 * Spot price of a bonding-curve pool in decimal USDL per whole token, exact to
 * 18 places: effective USDL reserve (quoteAssetDecimals) over token reserve
 * (token decimals). Null when the token decimals are not verified or the
 * token reserve is zero.
 */
export function poolSpotPrice(pool: Pool, tokenDecimals: number | null): string | null {
  if (tokenDecimals === null || !Number.isSafeInteger(tokenDecimals) || tokenDecimals < 0 || tokenDecimals > 77) return null;
  const reserve = BigInt(pool.tokenReserveRaw);
  if (reserve === 0n) return null;
  const numerator = BigInt(pool.effectivePricingUsdlReserveRaw) * 10n ** BigInt(tokenDecimals + PRICE_DECIMALS);
  const denominator = reserve * 10n ** BigInt(pool.quoteAssetDecimals);
  return scaleRawInteger((numerator / denominator).toString(), PRICE_DECIMALS);
}

/** One IF_POOL_DETAIL `GET /v2/pools/{p}` with the identity and integrity guards. */
export async function fetchPoolDetailListing(operations: Operations, poolAddress: string, signal?: AbortSignal): Promise<PoolDetailListing> {
  const pool = marketAddress(poolAddress);
  const response = await operations.call('IF_POOL_DETAIL', { path: { poolAddress: pool } }, { signal });
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Pool integrity quarantined');
  const detail = response.data;
  if (marketAddress(detail.pool.poolAddress) !== pool || marketAddress(detail.token.address) !== marketAddress(detail.pool.tokenAddress)) throw new Error('Pool identity disagreement');
  const decimals = detail.token.decimalsQuality === 'verified' ? detail.token.decimals : null;
  return { ...poolListing(detail.pool), spotPrice: poolSpotPrice(detail.pool, decimals) };
}

/** Session-cached listing of one pool (IF_POOL_DETAIL on a miss). */
export async function fetchPoolListing(qc: QueryClient, operations: Operations, origin: string, pool: string, signal?: AbortSignal): Promise<PoolListing> {
  return qc.fetchQuery({
    queryKey: queryKeys.poolListing(origin, pool),
    queryFn: async () => {
      const { spotPrice: _price, ...listing } = await fetchPoolDetailListing(operations, pool, signal);
      return listing;
    },
    staleTime: Infinity,
    gcTime: Infinity,
  });
}

function seed(qc: QueryClient, origin: string, listing: PoolListing): void {
  qc.setQueryData(queryKeys.poolListing(origin, listing.poolAddress), listing);
}

/**
 * Listings for a set of pools keyed by lowercased pool address. When `sort`
 * names an IF_POOL_LIST order whose ranking holds these pools, one
 * `GET /v2/pools?sort=…&limit=100` seeds the session cache first; every pool
 * still missing is read with IF_POOL_DETAIL. A pool whose detail is not served
 * (404 / 503: no registry row or no stats projection yet) is left out; when
 * no pool resolves at all the first real failure is thrown.
 */
export async function fetchPoolListings(qc: QueryClient, operations: Operations, origin: string, pools: readonly string[], signal?: AbortSignal, sort?: PoolListSort): Promise<Record<string, PoolListing>> {
  const wanted = [...new Set(pools.map(marketAddress))].sort();
  if (wanted.length === 0) return {};
  const missing = () => wanted.filter(pool => qc.getQueryData(queryKeys.poolListing(origin, pool)) === undefined);
  let listError: unknown = null;
  if (sort && missing().length > 1) {
    try {
      const response = await operations.call('IF_POOL_LIST', { query: { sort, limit: 2 * marketLimits.rankingPage } }, { signal });
      if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Pool list integrity quarantined');
      for (const pool of response.data.items) seed(qc, origin, poolListing(pool));
    } catch (error) {
      if (signal?.aborted) throw error;
      listError = error;
    }
  }
  const settled = await Promise.allSettled(wanted.map(pool => fetchPoolListing(qc, operations, origin, pool, signal)));
  const out: Record<string, PoolListing> = {};
  let firstError: unknown = listError;
  settled.forEach((result, index) => {
    if (result.status === 'fulfilled') out[wanted[index]] = result.value;
    else firstError ??= result.reason;
  });
  if (Object.keys(out).length === 0 && firstError !== null) throw firstError;
  return out;
}

/** React binding of fetchPoolListings (session-cached per pool; the set query follows the stats batch tolerance). */
export function usePoolListings(pools: readonly string[], sort?: PoolListSort): UseQueryResult<Record<string, PoolListing>> {
  const client = useMarketClient();
  const operations = useOperations();
  const qc = useQueryClient();
  const wanted = [...new Set(pools.filter(isPool).map(value => value.toLowerCase()))].sort();
  return useQuery({
    queryKey: queryKeys.poolListings(client.config.apiOrigin, wanted),
    queryFn: ({ signal }) => fetchPoolListings(qc, operations, client.config.apiOrigin, wanted, signal, sort),
    enabled: wanted.length > 0,
    ...marketCachePolicy.statsBatch,
  });
}

/**
 * Fresh IF_POOL_DETAIL reads (listing + spot price) for a short pool list,
 * such as the recently viewed rail. Listings learned here seed the session
 * cache; an unserved pool is left out, and only a set where nothing resolves fails.
 */
export function usePoolDetailListings(pools: readonly string[]): UseQueryResult<Record<string, PoolDetailListing>> {
  const client = useMarketClient();
  const operations = useOperations();
  const qc = useQueryClient();
  const origin = client.config.apiOrigin;
  const wanted = [...new Set(pools.filter(isPool).map(value => value.toLowerCase()))].sort();
  return useQuery({
    queryKey: queryKeys.poolDetailListings(origin, wanted),
    queryFn: async ({ signal }) => {
      const settled = await Promise.allSettled(wanted.map(pool => fetchPoolDetailListing(operations, pool, signal)));
      const out: Record<string, PoolDetailListing> = {};
      let firstError: unknown = null;
      settled.forEach((result, index) => {
        if (result.status === 'fulfilled') {
          const { spotPrice: _price, ...listing } = result.value;
          seed(qc, origin, listing);
          out[wanted[index]] = result.value;
        } else firstError ??= result.reason;
      });
      if (Object.keys(out).length === 0 && firstError !== null) throw firstError;
      return out;
    },
    enabled: wanted.length > 0,
    ...marketCachePolicy.poolDetail,
  });
}
