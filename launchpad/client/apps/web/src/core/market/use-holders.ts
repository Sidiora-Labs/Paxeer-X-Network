'use client';
import { useQuery } from '@tanstack/react-query';
import { HttpError, type SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { statsApiUrl } from '@/core/sdk-config';
import { marketAddress, marketLimits } from '@/page-modules/token/market-session';
import { browserStatsClient } from './client';
import { loadable, toUnsupported, unsupported, type Loadable, type Unsupported } from './unsupported';

export type HoldersResponse = V2ContractTypes['HoldersResponse'];
export type HolderEntry = V2ContractTypes['HolderEntry'];

export interface LegacyHolderShare { holderAddress: string; balance: string; pctOfSupply: string; rank: number | null }

export interface HoldersPage {
  /** Transfer-coverage page from the stats host `/v1/pools/{p}/holders`; null with `canonical` set when that host is not addressable. */
  response: HoldersResponse | null;
  rows: HolderEntry[];
  canonical: Unsupported | null;
  /** Share rows from the stats host `/stats/{p}/holders`, keyed by lowercased wallet; null when that read has none. */
  legacyShare: Record<string, LegacyHolderShare> | null;
  legacySource: 'stats-host' | null;
  nextCursor: string | null;
}

/** IF_HOLDERS shape served only by core/stats-workers `GET /v1/pools/{p}/holders` (pool_holders.go:239, TransferHolders). */
export async function fetchCanonicalHolders(client: SdkClient, poolAddress: string, cursor: string | null, signal?: AbortSignal): Promise<HoldersResponse> {
  const pool = marketAddress(poolAddress);
  const response = await client.request(`/v1/pools/${pool}/holders?limit=${marketLimits.holderPage}${cursor ? '&cursor=' + encodeURIComponent(cursor) : ''}`, 'HoldersResponse', { signal, idempotentRead: true });
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Holder integrity quarantined');
  if (response.data.items.length > marketLimits.holderPage || new Set(response.data.items.map(row => row.wallet.toLowerCase())).size !== response.data.items.length) throw new Error('Holder page bound or duplicate identity disagreement');
  return response;
}

function parseLegacyHolderList(value: unknown): LegacyHolderShare[] {
  if (typeof value !== 'object' || value === null) throw new TypeError('Legacy holder list shape disagreement');
  const holders = (value as { holders?: unknown }).holders;
  if (holders === null || holders === undefined) return [];
  if (!Array.isArray(holders)) throw new TypeError('Legacy holder list shape disagreement');
  return holders.map(item => {
    const row = item as Record<string, unknown>;
    if (typeof row.holderAddress !== 'string' || typeof row.balance !== 'string' || typeof row.pctOfSupply !== 'string') throw new TypeError('Legacy holder row shape disagreement');
    return { holderAddress: marketAddress(row.holderAddress), balance: row.balance, pctOfSupply: row.pctOfSupply, rank: typeof row.rank === 'number' ? row.rank : null };
  });
}

/** Share rows from the stats host `GET /stats/{p}/holders` (core/stats-workers pool_holders.go:52, holderList); null when the stats host is not configured or has no row. */
export async function fetchLegacyShare(poolAddress: string, signal?: AbortSignal, fetcher: typeof fetch = fetch): Promise<{ rows: LegacyHolderShare[]; source: 'stats-host' } | null> {
  const pool = marketAddress(poolAddress);
  const statsUrl = statsApiUrl(`/stats/${pool}/holders?limit=100`);
  if (!statsUrl) return null;
  const response = await fetcher(statsUrl, { signal, cache: 'no-store', credentials: 'omit', redirect: 'error', headers: { Accept: 'application/json' } });
  if (response.status === 404) { await response.body?.cancel(); return null; }
  if (!response.ok) { await response.body?.cancel(); throw new HttpError(response.status); }
  return { rows: parseLegacyHolderList(await response.json()), source: 'stats-host' };
}

/** The marker useHolders reports when no stats host is configured: holders are read from the stats host only. */
export const holdersHostUnset: Unsupported = unsupported('IF_HOLDERS', null, 'Stats host not configured (NEXT_PUBLIC_STATS_API_URL)');

export async function loadHoldersPage(poolAddress: string, cursor: string | null, signal?: AbortSignal, statsClient: SdkClient | null = browserStatsClient()): Promise<HoldersPage> {
  const pool = marketAddress(poolAddress);
  const [canonical, legacy] = await Promise.all([
    statsClient ? fetchCanonicalHolders(statsClient, pool, cursor, signal).then(response => ({ response, canonical: null as Unsupported | null }), (error: unknown) => { const marker = toUnsupported('IF_HOLDERS', error); if (!marker) throw error; return { response: null, canonical: marker }; }) : Promise.resolve({ response: null, canonical: holdersHostUnset }),
    fetchLegacyShare(pool, signal).catch((error: unknown) => { if (toUnsupported('LEGACY_HOLDERS', error)) return null; throw error; }),
  ]);
  if (!canonical.response && !legacy) throw new HttpError(canonical.canonical?.status ?? 404, { code: canonical.canonical?.message ?? 'NOT_FOUND', message: 'Holders unavailable', requestId: '', retryable: false, retryAfterMs: null, fieldErrors: [], details: null });
  const legacyShare = legacy ? Object.fromEntries(legacy.rows.map(row => [row.holderAddress, row])) : null;
  return { response: canonical.response, rows: canonical.response?.data.items ?? [], canonical: canonical.canonical, legacyShare, legacySource: legacy?.source ?? null, nextCursor: canonical.response?.nextCursor ?? null };
}

/**
 * Holders for a pool from the stats host only: transfer-coverage page plus
 * share rows. Unsupported (`holdersHostUnset`) when no stats host is
 * configured, and when the stats host answers neither read.
 */
export function useHolders(poolAddress: string | null | undefined, cursor: string | null = null): Loadable<HoldersPage> {
  const statsClient = browserStatsClient();
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const query = useQuery({
    queryKey: queryKeys.holders(statsClient?.config.apiOrigin ?? '', pool ?? '', cursor),
    queryFn: ({ signal }) => loadHoldersPage(pool as string, cursor, signal, statsClient),
    enabled: pool !== null && statsClient !== null,
    ...marketCachePolicy.holders,
  });
  if (statsClient === null) return holdersHostUnset;
  return loadable(query, 'IF_HOLDERS');
}
