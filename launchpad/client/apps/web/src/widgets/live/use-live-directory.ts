'use client';
import { useCallback, useMemo } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { Operations, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { getLivePage, type StreamDirectoryEntry, type StreamViewerQuality } from '@/core/clients/livestream-api';
import { getTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { fetchPoolDetailListing } from '@/core/market/pool-listings';
import { useOperations } from '@/core/market/operations';
import { loadable, toUnsupported, type Loadable, type Unsupported } from '@/core/market/unsupported';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { retryTransientOnly } from '@/core/transient-retry';
import type { TokenMetadata } from '@/widgets/home/types';
import { compareDescending, rawToBigInt, shortId } from './format';
import { marketCapRaw } from '@/core/market/pool-stats-display';

type PoolStats = V2ContractTypes['PoolStats'];
type StatsBatchResponse = V2ContractTypes['StatsBatchResponse'];

/** Outcome of one enrichment source for a refresh; `skipped` when there was nothing to request. */
export type SourceState = { state: 'ready' } | { state: 'skipped' } | Unsupported | { state: 'error'; message: string };

export interface LiveDirectoryEntry {
  stream: StreamDirectoryEntry;
  poolAddress: string;
  tokenAddress: string | null;
  creatorWallet: string;
  viewerCount: number | null;
  viewerQuality: StreamViewerQuality;
  /** Circulating market cap in raw USDL (6 decimals); null when the stats projection has none. */
  mcapRaw: string | null;
  mcapQuality: PoolStats['supplyValuationQuality'];
  metadata: TokenMetadata | null;
  name: string | null;
  symbol: string | null;
  imageUrl: string | null;
  thumbnailUrl: string | null;
}

export interface LiveDirectoryData {
  entries: LiveDirectoryEntry[];
  fetchedAt: string;
  pages: number;
  /** True when more live pages existed beyond the page cap. */
  truncated: boolean;
  /** Server cursor for the first page past the cap; null when the walk reached the end. */
  nextCursor: string | null;
  sources: { stats: SourceState; identity: SourceState; metadata: SourceState };
}

export interface LiveDirectory {
  state: Loadable<LiveDirectoryData>;
  entries: LiveDirectoryEntry[];
  byViewers: LiveDirectoryEntry[];
  byMcap: LiveDirectoryEntry[];
  featured: LiveDirectoryEntry[];
  /** Profile display name when known, otherwise the 8-character short id of the wallet. */
  creatorLabel(wallet: string): string;
  isRefreshing: boolean;
  updatedAt: number | null;
  refresh(): void;
}

export const liveDirectoryMaxPages = 5;
/** Rows per `/v2/streams/live` page (media/livestream accepts 1 to 100). */
export const liveDirectoryPageSize = 100;
const liveDirectoryRefreshMs = 10_000;
const featuredCount = 6;
const statsBatchLimit = 50;
const empty: LiveDirectoryEntry[] = [];

/** Pool to token identity, learned once per pool from IF_POOL_DETAIL and kept for the session. */
const tokenByPool = new Map<string, string>();

function chunk<T>(items: readonly T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size));
  return out;
}

function failure(operation: string, error: unknown): SourceState {
  return toUnsupported(operation, error) ?? { state: 'error', message: error instanceof Error ? error.message : String(error) };
}

function throwIfAborted(signal?: AbortSignal): void {
  if (!signal?.aborted) return;
  throw signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError');
}

/**
 * Walk `/v2/streams/live` by cursor from `start` (at most `maxPages` pages),
 * keeping only live rows once each. `nextCursor` is the server cursor for the
 * first page past the cap, so a caller can continue the walk for real.
 */
export async function collectLivePages(signal?: AbortSignal, start: string | null = null, maxPages: number = liveDirectoryMaxPages): Promise<{ streams: StreamDirectoryEntry[]; pages: number; truncated: boolean; nextCursor: string | null }> {
  const streams: StreamDirectoryEntry[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined = start ?? undefined;
  let pages = 0;
  for (;;) {
    throwIfAborted(signal);
    const page = await getLivePage(cursor, { limit: liveDirectoryPageSize });
    pages++;
    for (const row of page.streams) {
      if (row.status !== 'live' || seen.has(row.id)) continue;
      seen.add(row.id);
      streams.push(row);
    }
    if (!page.nextCursor) return { streams, pages, truncated: false, nextCursor: null };
    if (pages >= maxPages) return { streams, pages, truncated: true, nextCursor: page.nextCursor };
    cursor = page.nextCursor;
  }
}

async function resolveTokens(operations: Operations, pools: readonly string[], signal?: AbortSignal): Promise<{ tokens: Map<string, string>; state: SourceState }> {
  const missing = pools.filter(pool => !tokenByPool.has(pool));
  let state: SourceState = missing.length === 0 && pools.length === 0 ? { state: 'skipped' } : { state: 'ready' };
  if (missing.length > 0) {
    // One IF_POOL_DETAIL per unseen pool; a pool the registry does not serve yet stays without a token.
    const settled = await Promise.allSettled(missing.map(pool => fetchPoolDetailListing(operations, pool, signal)));
    throwIfAborted(signal);
    let firstError: unknown = null;
    settled.forEach(result => {
      if (result.status === 'fulfilled') tokenByPool.set(result.value.poolAddress, result.value.tokenAddress);
      else firstError ??= result.reason;
    });
    if (firstError !== null && settled.every(result => result.status === 'rejected')) state = failure('IF_POOL_DETAIL', firstError);
  }
  const tokens = new Map<string, string>();
  for (const pool of pools) {
    const token = tokenByPool.get(pool);
    if (token) tokens.set(pool, token);
  }
  return { tokens, state };
}

async function loadStats(operations: Operations, pools: readonly string[], signal?: AbortSignal): Promise<{ stats: Map<string, PoolStats>; state: SourceState }> {
  const stats = new Map<string, PoolStats>();
  if (pools.length === 0) return { stats, state: { state: 'skipped' } };
  try {
    for (const group of chunk(pools, statsBatchLimit)) {
      const response: StatsBatchResponse = await operations.call('IF_STATS_BATCH', { body: { poolAddresses: [...group] } }, { signal });
      if (response.quality === 'quarantined') throw new Error('Statistics integrity quarantined');
      for (const item of response.data.items) {
        if (item.watermark.integrity === 'quarantined') continue;
        stats.set(item.poolAddress.toLowerCase(), item);
      }
    }
    return { stats, state: { state: 'ready' } };
  } catch (error) {
    return { stats, state: failure('IF_STATS_BATCH', error) };
  }
}

async function loadMetadata(tokens: readonly string[], signal?: AbortSignal): Promise<{ metadata: Record<string, TokenMetadata | null>; state: SourceState }> {
  if (tokens.length === 0) return { metadata: {}, state: { state: 'skipped' } };
  try {
    return { metadata: await getTokenMetadataBatch([...tokens], signal), state: { state: 'ready' } };
  } catch (error) {
    return { metadata: {}, state: failure('/metadata/batch', error) };
  }
}

/** Join live rows with their stats, token identity and metadata; every field degrades to null, never to a made-up value. */
export function assembleDirectory(
  streams: readonly StreamDirectoryEntry[],
  stats: ReadonlyMap<string, PoolStats>,
  tokens: ReadonlyMap<string, string>,
  metadata: Readonly<Record<string, TokenMetadata | null>>,
): LiveDirectoryEntry[] {
  return streams.map(stream => {
    const poolAddress = stream.poolAddress.toLowerCase();
    const tokenAddress = tokens.get(poolAddress) ?? null;
    const row = stats.get(poolAddress);
    const meta = tokenAddress ? metadata[tokenAddress] ?? null : null;
    return {
      stream,
      poolAddress,
      tokenAddress,
      creatorWallet: stream.creatorWallet.toLowerCase(),
      viewerCount: stream.viewerCount,
      viewerQuality: stream.viewerQuality,
      mcapRaw: marketCapRaw(row),
      mcapQuality: row?.supplyValuationQuality ?? 'unavailable',
      metadata: meta,
      name: meta?.name ?? null,
      symbol: meta?.symbol ?? null,
      imageUrl: meta?.images?.logo ?? null,
      thumbnailUrl: stream.thumbnailUrl,
    };
  });
}

const byId = (a: LiveDirectoryEntry, b: LiveDirectoryEntry) => a.stream.id.localeCompare(b.stream.id);

/** Most viewers first; streams without an estimate last; stable by stream id. */
export function rankByViewers(entries: readonly LiveDirectoryEntry[]): LiveDirectoryEntry[] {
  return [...entries].sort((a, b) => compareDescending(a.viewerCount, b.viewerCount) || byId(a, b));
}

/** Highest market cap first; streams whose pool has no market cap last; ties fall back to viewers. */
export function rankByMarketCap(entries: readonly LiveDirectoryEntry[]): LiveDirectoryEntry[] {
  return [...entries].sort((a, b) => compareDescending(rawToBigInt(a.mcapRaw), rawToBigInt(b.mcapRaw)) || compareDescending(a.viewerCount, b.viewerCount) || byId(a, b));
}

/**
 * One refresh of the live directory: one `/v2/streams/live` page set, one
 * IF_STATS_BATCH (per 50 pools) and one metadata batch. Pool to token identity
 * comes from IF_POOL_DETAIL the first time a pool is seen and is cached for
 * the session, so steady-state refreshes issue no extra request.
 */
async function loadLiveDirectory(operations: Operations, signal?: AbortSignal, start: string | null = null, maxPages: number = liveDirectoryMaxPages): Promise<LiveDirectoryData> {
  const { streams, pages, truncated, nextCursor } = await collectLivePages(signal, start, maxPages);
  const pools = [...new Set(streams.map(stream => stream.poolAddress.toLowerCase()))].sort();
  const [{ stats, state: statsState }, { tokens, state: identityState }] = await Promise.all([loadStats(operations, pools, signal), resolveTokens(operations, pools, signal)]);
  throwIfAborted(signal);
  const { metadata, state: metadataState } = await loadMetadata([...new Set(tokens.values())].sort(), signal);
  return {
    entries: assembleDirectory(streams, stats, tokens, metadata),
    fetchedAt: new Date().toISOString(),
    pages,
    truncated,
    nextCursor,
    sources: { stats: statsState, identity: identityState, metadata: metadataState },
  };
}

export interface UseLiveDirectoryOptions {
  enabled?: boolean;
  refetchInterval?: number | false;
}

/** Shared live directory: hero, ranked rows and show-all pages read one object so counts never disagree. */
export function useLiveDirectory(options: UseLiveDirectoryOptions = {}): LiveDirectory {
  const operations = useOperations();
  const query = useQuery({
    queryKey: queryKeys.liveDirectory(),
    queryFn: ({ signal }) => loadLiveDirectory(operations, signal),
    enabled: options.enabled ?? true,
    staleTime: 5_000,
    gcTime: 60_000,
    ...retryTransientOnly,
    refetchInterval: options.refetchInterval ?? liveDirectoryRefreshMs,
    refetchOnWindowFocus: true,
  });
  const entries = query.data?.entries ?? empty;
  const wallets = useMemo(() => entries.map(entry => entry.creatorWallet), [entries]);
  const { identities } = useWalletIdentities(wallets);
  const byViewers = useMemo(() => rankByViewers(entries), [entries]);
  const byMcap = useMemo(() => rankByMarketCap(entries), [entries]);
  const featured = useMemo(() => byViewers.slice(0, featuredCount), [byViewers]);
  return {
    state: loadable(query, 'IF_STREAM_LIST'),
    entries,
    byViewers,
    byMcap,
    featured,
    creatorLabel: wallet => shortId(wallet, identities[wallet.toLowerCase()]?.displayName),
    isRefreshing: query.isFetching && query.status === 'success',
    updatedAt: query.dataUpdatedAt || null,
    refresh: () => { void query.refetch(); },
  };
}

export interface LiveDirectoryContinuation {
  entries: LiveDirectoryEntry[];
  hasMore: boolean;
  loading: boolean;
  error: string | null;
  loadMore(): void;
}

/**
 * Continues the `/v2/streams/live` walk past the directory's page cap from the
 * server cursor `start` (LiveDirectoryData.nextCursor), one enriched page per
 * `loadMore`. Nothing is requested while `start` is null.
 */
export function useLiveDirectoryContinuation(start: string | null): LiveDirectoryContinuation {
  const operations = useOperations();
  const query = useInfiniteQuery({
    queryKey: queryKeys.liveDirectoryContinuation(start ?? ''),
    initialPageParam: start,
    queryFn: ({ pageParam, signal }) => loadLiveDirectory(operations, signal, pageParam, 1),
    getNextPageParam: last => last.nextCursor,
    enabled: start !== null,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: 60_000,
    ...retryTransientOnly,
  });
  const pages = query.data?.pages;
  const entries = useMemo(() => pages?.flatMap(page => page.entries) ?? empty, [pages]);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;
  const loadMore = useCallback(() => { if (hasNextPage && !isFetchingNextPage) void fetchNextPage(); }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  return {
    entries,
    hasMore: start !== null && (query.status !== 'success' || hasNextPage),
    loading: query.isFetching,
    error: query.error ? (query.error instanceof Error ? query.error.message : String(query.error)) : null,
    loadMore,
  };
}
