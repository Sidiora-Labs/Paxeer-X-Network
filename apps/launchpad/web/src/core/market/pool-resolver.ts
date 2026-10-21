import { HttpError, type Operations, type V2ContractTypes } from '@kindle/sdk';
import { marketAddress } from '@/page-modules/token/market-session';
import { toUnsupported, type Unsupported } from './unsupported';
import type { PoolStats } from './use-stats-batch';

export type { PoolStats };
export type Token = V2ContractTypes['Token'];
export type MetadataGetData = V2ContractTypes['MetadataGetData'];
export type SearchResult = V2ContractTypes['SearchResult'];

export interface PoolIdentity { chainId: '125'; poolAddress: string; tokenAddress: string }

export interface PoolSnapshot {
  pool: PoolIdentity;
  token: Token | null;
  metadata: MetadataGetData | null;
  stats: PoolStats | null;
  /** Pool creator and creation time from the IF_POOL_DETAIL registry row; null when the detail is not served. */
  creator: string | null;
  createdAt: string | null;
  generatedAt: string;
  resolvedBy: 'detail' | 'search';
  /** `registry` is the IF_POOL_DETAIL marker when the detail (creator, creation time) is not served; null when it is. */
  unavailable: { stats: Unsupported | null; metadata: Unsupported | null; registry: Unsupported | null };
}

/** The resolver reads v2 routes only, through `operations`. */
export interface ResolverDeps { operations: Operations }

export class PoolNotFoundError extends Error {
  readonly code = 'POOL_NOT_FOUND';
  readonly status = 404;
  constructor(readonly address: string) { super(`No pool or token on chain 125 at ${address}`); }
}

const address = (value: unknown): string | null => typeof value === 'string' && /^0x[0-9a-fA-F]{40}$/.test(value) ? value.toLowerCase() : null;

type PoolDetailResponse = V2ContractTypes['PoolDetailResponse'];

/**
 * IF_POOL_DETAIL `GET /v2/pools/{p}` (core/api/internal/rest/pool_reads.go
 * poolDetail) as the pool-input probe, with the guards of useMarketDetail.
 * `value` is null with `unavailable` set when the address is not a served
 * pool: 404 NOT_FOUND (no registry row, or a token address), or a not-served
 * status (503 DEPENDENCY_UNAVAILABLE before the stats projection exists, 501
 * / bare 404 where the route is not deployed). Any other failure surfaces.
 */
export async function fetchPoolDetail(poolAddress: string, deps: Pick<ResolverDeps, 'operations'>, signal?: AbortSignal): Promise<{ value: PoolDetailResponse | null; unavailable: Unsupported | null }> {
  const pool = marketAddress(poolAddress);
  let response: PoolDetailResponse;
  try {
    response = await deps.operations.call('IF_POOL_DETAIL', { path: { poolAddress: pool } }, { signal });
  } catch (error) {
    const marker = toUnsupported('IF_POOL_DETAIL', error);
    if (marker) return { value: null, unavailable: error instanceof HttpError ? { ...marker, message: error.code } : marker };
    throw error;
  }
  const { pool: row, token, stats } = response.data;
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined' || stats.watermark.integrity === 'quarantined') throw new Error('Pool integrity quarantined');
  if (row.chainId !== '125' || marketAddress(row.poolAddress) !== pool || marketAddress(stats.poolAddress) !== pool || marketAddress(row.tokenAddress) !== marketAddress(token.address)) throw new Error('Pool identity disagreement');
  return { value: response, unavailable: null };
}

async function searchHit(input: string, type: 'pool' | 'token', deps: ResolverDeps, signal?: AbortSignal): Promise<SearchResult | null> {
  const response = await deps.operations.call('IF_SEARCH', { query: { q: input, type, limit: 10 } }, { signal });
  if (response.quality === 'quarantined') throw new Error('Search integrity quarantined');
  for (const item of response.data.items) {
    if (item.kind !== type) continue;
    if (type === 'token' && address(item.tokenAddress) === input && address(item.poolAddress)) return item;
    if (type === 'pool' && address(item.poolAddress) === input && address(item.tokenAddress)) return item;
  }
  return null;
}

export interface ResolvedIdentity { identity: PoolIdentity; detail: PoolDetailResponse | null; resolvedBy: 'detail' | 'search'; detailUnavailable: Unsupported | null }

/**
 * Resolve a `/coin/[address]` input, which may be a pool or a token address,
 * to its pool identity using served v2 routes only: IF_POOL_DETAIL first
 * (pool input), then IF_SEARCH by token and by pool. A pool whose detail is
 * not served yet (503 before its stats projection exists) still resolves
 * through search, with the detail marked unavailable.
 */
export async function resolvePoolIdentity(input: string, deps: ResolverDeps, signal?: AbortSignal): Promise<ResolvedIdentity> {
  const candidate = marketAddress(input);
  const direct = await fetchPoolDetail(candidate, deps, signal);
  const detail = direct.value;
  if (detail) return { identity: { chainId: '125', poolAddress: candidate, tokenAddress: marketAddress(detail.data.pool.tokenAddress) }, detail, resolvedBy: 'detail', detailUnavailable: null };
  const byToken = await searchHit(candidate, 'token', deps, signal);
  if (byToken) {
    const pool = address(byToken.poolAddress) as string;
    const resolved = await fetchPoolDetail(pool, deps, signal);
    if (resolved.value && marketAddress(resolved.value.data.pool.tokenAddress) !== candidate) throw new Error('Pool identity disagreement');
    return { identity: { chainId: '125', poolAddress: pool, tokenAddress: candidate }, detail: resolved.value, resolvedBy: 'search', detailUnavailable: resolved.unavailable };
  }
  const byPool = await searchHit(candidate, 'pool', deps, signal);
  if (byPool) return { identity: { chainId: '125', poolAddress: candidate, tokenAddress: address(byPool.tokenAddress) as string }, detail: null, resolvedBy: 'search', detailUnavailable: direct.unavailable };
  throw new PoolNotFoundError(candidate);
}

async function optional<T>(operation: string, load: () => Promise<T>): Promise<{ value: T | null; unavailable: Unsupported | null }> {
  try { return { value: await load(), unavailable: null }; }
  catch (error) {
    const marker = toUnsupported(operation, error);
    if (marker) return { value: null, unavailable: marker };
    throw error;
  }
}

/**
 * Full snapshot for the coin page from served v2 routes: identity and registry
 * facts (IF_POOL_DETAIL), stats (from the detail, else IF_STATS), metadata
 * (IF_METADATA_GET, else the detail token).
 */
export async function resolvePoolSnapshot(input: string, deps: ResolverDeps, signal?: AbortSignal): Promise<PoolSnapshot> {
  const resolved = await resolvePoolIdentity(input, deps, signal);
  const { poolAddress: pool, tokenAddress: token } = resolved.identity;
  const detail = resolved.detail;
  const [stats, metadata] = await Promise.all([
    detail
      ? Promise.resolve({ value: { data: { stats: detail.data.stats }, generatedAt: detail.generatedAt }, unavailable: null })
      : optional('IF_STATS', async () => {
        const response = await deps.operations.call('IF_STATS', { path: { poolAddress: pool } }, { signal });
        if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined' || response.data.stats.watermark.integrity === 'quarantined') throw new Error('Statistics integrity quarantined');
        if (marketAddress(response.data.stats.poolAddress) !== pool) throw new Error('Statistics identity disagreement');
        return response;
      }),
    optional('IF_METADATA_GET', async () => {
      const response = await deps.operations.call('IF_METADATA_GET', { path: { tokenAddress: token } }, { signal });
      if (response.quality === 'quarantined' || marketAddress(response.data.token.address) !== token) throw new Error('Metadata identity disagreement or integrity quarantine');
      return response;
    }),
  ]);
  const registry = detail?.data.pool as (V2ContractTypes['Pool'] & { createdAt?: string }) | undefined;
  return {
    pool: resolved.identity,
    token: metadata.value?.data.token ?? detail?.data.token ?? null,
    metadata: metadata.value?.data ?? null,
    stats: (stats.value?.data.stats as PoolStats | undefined) ?? null,
    creator: registry ? marketAddress(registry.creator) : null,
    createdAt: registry?.createdAt ?? null,
    generatedAt: stats.value?.generatedAt ?? metadata.value?.generatedAt ?? new Date().toISOString(),
    resolvedBy: resolved.resolvedBy,
    unavailable: { stats: stats.unavailable, metadata: metadata.unavailable, registry: resolved.detailUnavailable },
  };
}
