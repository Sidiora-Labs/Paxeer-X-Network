'use client';
import { useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { SdkClient } from '@kindle/sdk';
import type { RankingsRequest, PoolDetailResponse, HoldersResponse } from '../../../../../packages/production-contracts/src/types.gen';
import { MarketClientContext, browserMarketClient, browserStatsClient, useMarketClient } from '@/core/market/client';
import { useOperations } from '@/core/market/operations';
import { useRankings } from '@/core/market/use-rankings';
import { useStatsBatch } from '@/core/market/use-stats-batch';
import { resolvePoolIdentity } from '@/core/market/pool-resolver';
import { fetchCanonicalHolders } from '@/core/market/use-holders';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { MarketSession, marketAddress, marketLimits } from './market-session';

export { browserMarketClient, useMarketClient };
export function MarketClientProvider({ client, children }: { client: SdkClient; children: ReactNode }) {
  return <MarketClientContext.Provider value={client}>{children}</MarketClientContext.Provider>;
}
export function useMarketInactivePoolCount() {
  const cache = useQueryClient().getQueryCache();
  const subscribe = useMemo(() => (listener: () => void) => cache.subscribe(listener), [cache]);
  const count = useMemo(() => () => {
    const pools = new Set<string>();
    for (const query of cache.getAll()) if (query.queryKey[0] === 'production-market' && !query.isActive()) for (const part of query.queryKey) if (typeof part === 'string' && /^0x[0-9a-fA-F]{40}$/.test(part)) pools.add(part.toLowerCase());
    return pools.size;
  }, [cache]);
  return useSyncExternalStore(subscribe, count, () => 0);
}
export function useMarketClock() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => { if (document.visibilityState !== 'hidden') setNow(Date.now()); }, marketLimits.freshnessIntervalMs);
    return () => clearInterval(timer);
  }, []);
  return now;
}
/** IF_RANKINGS `GET /v2/rankings/{category}` (served; core/api/internal/rest/compatibility.go:92). */
export function useMarketRanking(category: RankingsRequest['category'], cursor: string | null = null, limit: number = marketLimits.rankingPage) {
  return useRankings(category, cursor, limit);
}
/** IF_STATS_BATCH `POST /v2/pools/stats:batch` (served; compatibility.go:91). */
export function useMarketStatsBatch(addresses: readonly string[]) {
  return useStatsBatch(addresses);
}
/**
 * Canonical pool detail. The pool or token input is resolved through served
 * v2 routes (IF_POOL_DETAIL, then `/v2/search`), then IF_POOL_DETAIL
 * `GET /v2/pools/{poolAddress}` supplies the full Pool/Token/PoolStats
 * contract (registered by BE-1 at core/api/internal/rest/compatibility.go:88).
 * Where that handler is not deployed the SDK raises CapabilityUnavailableError
 * (real 404) and this query errors; the served-today subset is available
 * through `usePoolSnapshot` (core/market/use-pool-snapshot).
 */
export function useMarketDetail(address: string) {
  const client = useMarketClient(); const operations = useOperations(); const pool = marketAddress(address);
  const cache = useQueryClient();
  useEffect(() => {
    const prune = () => {
      const inactive = cache.getQueryCache().getAll().filter(query => query.queryKey[0] === 'production-market' && !query.isActive());
      const pools = new Map<string, number>();
      for (const query of inactive) for (const part of query.queryKey) if (typeof part === 'string' && /^0x[0-9a-fA-F]{40}$/.test(part)) pools.set(part, Math.max(pools.get(part) ?? 0, query.state.dataUpdatedAt));
      const evicted = new Set([...pools].sort((a, b) => b[1] - a[1]).slice(marketLimits.inactivePools).map(([identity]) => identity));
      if (evicted.size) cache.removeQueries({ predicate: query => !query.isActive() && query.queryKey[0] === 'production-market' && query.queryKey.some(part => typeof part === 'string' && evicted.has(part)) });
    };
    prune();
    return cache.getQueryCache().subscribe(prune);
  }, [cache]);
  return useQuery<PoolDetailResponse>({ queryKey: queryKeys.poolDetail(client.config.apiOrigin, pool),
    queryFn: async ({ signal }) => {
      const resolved = await resolvePoolIdentity(pool, { operations }, signal);
      const canonical = resolved.identity.poolAddress;
      const response = resolved.detail ?? await operations.call('IF_POOL_DETAIL', { path: { poolAddress: canonical } }, { signal });
      if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined' || response.data.stats.watermark.integrity === 'quarantined' || response.data.pool.chainId !== '125' || response.data.pool.quoteAssetDecimals !== 6 || marketAddress(response.data.pool.poolAddress) !== canonical || marketAddress(response.data.stats.poolAddress) !== canonical || marketAddress(response.data.pool.tokenAddress) !== marketAddress(response.data.token.address) || marketAddress(response.data.pool.tokenAddress) !== resolved.identity.tokenAddress) throw new Error('Canonical pool identity disagreement or integrity quarantine');
      return response;
    },
    ...marketCachePolicy.poolDetail,
  });
}
/**
 * Transfer-coverage holders. Origin handler: core/stats-workers
 * `GET /v1/pools/{p}/holders` (pool_holders.go:239), used directly when a
 * stats host is configured; otherwise IF_HOLDERS `GET /v2/pools/{p}/holders`
 * on the api origin (BE-1 proxy, compatibility.go:89) is requested and any
 * real 404 surfaces as the query error.
 */
export function useMarketHolders(address: string, cursor: string | null = null) {
  const client = useMarketClient(); const operations = useOperations(); const pool = marketAddress(address);
  const statsClient = browserStatsClient();
  return useQuery<HoldersResponse>({ queryKey: queryKeys.holders(statsClient?.config.apiOrigin ?? client.config.apiOrigin, pool, cursor),
    queryFn: async ({ signal }) => {
      if (statsClient) return fetchCanonicalHolders(statsClient, pool, cursor, signal);
      const response = await operations.call('IF_HOLDERS', { path: { poolAddress: pool }, query: { limit: marketLimits.holderPage, ...(cursor ? { cursor } : {}) } }, { signal });
      if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Holder integrity quarantined');
      if (response.data.items.length > marketLimits.holderPage || new Set(response.data.items.map(row => row.wallet.toLowerCase())).size !== response.data.items.length) throw new Error('Holder page bound or duplicate identity disagreement');
      return response;
    },
    ...marketCachePolicy.holders,
  });
}
/** IF_METADATA_GET `GET /v2/tokens/{t}/metadata` on media/metadata (media/metadata/internal/metadata/connected.go:138). */
export function useMarketMetadata(tokenAddress: string | null) {
  const client = useMarketClient();
  return useQuery({ queryKey: queryKeys.metadataV2(client.config.mediaOrigin, tokenAddress),
    queryFn: async ({ signal }) => {
      const token = marketAddress(tokenAddress ?? '');
      const response = await client.requestV2(`/v2/tokens/${token}/metadata`, 'MetadataGetResponse', { signal, idempotentRead: true, service: 'media' });
      if (response.quality === 'quarantined' || response.data.token.address.toLowerCase() !== token) throw new Error('Metadata identity disagreement or integrity quarantine');
      return response;
    },
    enabled: tokenAddress !== null, ...marketCachePolicy.metadata,
  });
}
/**
 * Live market session for one pool. `enabled: false` keeps the session idle: no snapshot poll,
 * no stream, no retries. The coin page passes it while the pool is unresolved and permanently
 * once the coin is known not to exist, so an unknown address issues no market traffic at all.
 */
export function useMarketLive(address: string, options: { enabled?: boolean } = {}) {
  const enabled = options.enabled ?? true;
  const client = useMarketClient(); const pool = marketAddress(address);
  const session = useMemo(() => new MarketSession(client, pool), [client, pool]);
  const state = useSyncExternalStore(session.subscribe, session.getSnapshot, session.getSnapshot);
  useEffect(() => {
    if (!enabled) { session.close(); return; }
    const controller = new AbortController();
    if (document.visibilityState === 'hidden') session.visibility(true);
    else void session.start(controller.signal);
    const visibility = () => { if (document.visibilityState === 'hidden') session.visibility(true); else void session.start(controller.signal); };
    document.addEventListener('visibilitychange', visibility);
    return () => { document.removeEventListener('visibilitychange', visibility); controller.abort(); session.close(); };
  }, [session, enabled]);
  const now = useMarketClock();
  return { session, state, freshness: session.status(now), now };
}
/** IF_TRADES `GET /v2/pools/{p}/trades` (served; core/api/internal/rest/accounting_v2.go:32,46). */
export function useMarketTrades(address: string, cursor: string | null = null) {
  const client = useMarketClient(); const pool = marketAddress(address);
  return useQuery({ queryKey: queryKeys.trades(client.config.apiOrigin, pool, cursor),
    queryFn: async ({ signal }) => {
      const response = await client.requestV2(`/v2/pools/${pool}/trades?limit=100${cursor ? '&cursor=' + encodeURIComponent(cursor) : ''}`, 'TradesResponse', { signal, idempotentRead: true });
      if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined' || response.data.items.some(row => marketAddress(row.poolAddress) !== pool)) throw new Error('Trade history identity disagreement or integrity quarantine');
      return response;
    },
    ...marketCachePolicy.statsBatch,
  });
}
