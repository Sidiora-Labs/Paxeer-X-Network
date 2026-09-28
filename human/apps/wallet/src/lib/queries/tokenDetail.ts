'use client';

/**
 * Token-detail queries — metadata, pool data, OHLC candles.
 *
 * The Sidiora-token resolution chain runs in two stages:
 *   1. `useTokenMetaQuery(tokenId)`     — parallel: chain metadata + public metadata
 *   2. `useTokenPoolDataQuery(poolAddr)` — only enabled once stage 1 returns a pool address
 *
 * `useTokenCandlesQuery` picks the right OHLC source based on token type:
 *   - PAX / WPAX  → `fetchPaxHistory`
 *   - SID         → `fetchSidCandles`
 *   - Sidiora pool → `fetchSidioraCandles`
 *   - other       → no candles (returns empty array)
 */

import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import {
    fetchTokenInfo,
    fetchTokenMetadataPublic,
    fetchSidioraPoolStats,
    fetchSidioraPoolHolders,
    fetchPaxHistory,
    fetchSidCandles,
    fetchSidioraCandles,
    fetchCrossverseHistory,
    fetchCrossversePrice,
    getCrossverseSymbol,
    SID_TOKEN_ADDRESS,
    type OhlcCandle,
    type SidioraPoolStats,
    type HolderDistribution,
    type TokenMetadataPublic,
    type PaxPriceLatest,
} from '@/lib/api';
import type { WalletTokenMetadata } from '@/lib/data';

export type TimePeriod = '1H' | '1D' | '1W' | '1M' | 'ALL';

export interface TimePeriodConfig {
    id: TimePeriod;
    label: string;
    resolution: string;
    countback: number;
    paxRes: number;
    paxCount: number;
}

export const TIME_PERIODS: readonly TimePeriodConfig[] = [
    { id: '1H', label: '1H', resolution: '1', countback: 60, paxRes: 1, paxCount: 60 },
    { id: '1D', label: '1D', resolution: '15', countback: 96, paxRes: 15, paxCount: 96 },
    { id: '1W', label: '1W', resolution: '60', countback: 168, paxRes: 60, paxCount: 168 },
    { id: '1M', label: '1M', resolution: '240', countback: 180, paxRes: 240, paxCount: 180 },
    { id: 'ALL', label: 'ALL', resolution: '1D', countback: 365, paxRes: 1440, paxCount: 365 },
] as const;

export interface TokenMetaResult {
    chain: WalletTokenMetadata | null;
    public: TokenMetadataPublic | null;
}

/**
 * Combined token metadata (chain + public). Public metadata lookup may 404 for
 * non-Sidiora tokens — that's swallowed and returned as `public: null`.
 * Disabled for native PAX (no contract address).
 */
export function useTokenMetaQuery(
    tokenId: string,
    isPax: boolean,
    isWpax: boolean,
    options?: Partial<UseQueryOptions<TokenMetaResult>>,
) {
    return useQuery<TokenMetaResult>({
        queryKey: ['tokenMeta', tokenId.toLowerCase()],
        queryFn: async () => {
            const [chain, pub] = await Promise.all([
                fetchTokenInfo(tokenId).catch(() => null),
                // Skip public metadata for WPAX — never has a Sidiora pool.
                isWpax ? Promise.resolve(null) : fetchTokenMetadataPublic(tokenId).catch(() => null),
            ]);
            return { chain, public: pub };
        },
        enabled: Boolean(tokenId) && !isPax,
        staleTime: Infinity,
        gcTime: Infinity,
        refetchOnMount: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
        ...options,
    });
}

export interface PoolDataResult {
    stats: SidioraPoolStats | null;
    holders: HolderDistribution | null;
}

/**
 * Sidiora pool stats + holder distribution. Only enabled once a pool address
 * is known (typically resolved upstream by `useTokenMetaQuery`).
 */
export function useTokenPoolDataQuery(
    poolAddress: string | null | undefined,
    options?: Partial<UseQueryOptions<PoolDataResult>>,
) {
    return useQuery<PoolDataResult>({
        queryKey: ['tokenPoolData', (poolAddress ?? '').toLowerCase()],
        queryFn: async () => {
            const [stats, holders] = await Promise.all([
                fetchSidioraPoolStats(poolAddress as string).catch(() => null),
                fetchSidioraPoolHolders(poolAddress as string).catch(() => null),
            ]);
            return { stats, holders };
        },
        enabled: Boolean(poolAddress),
        staleTime: 60_000,
        ...options,
    });
}

/**
 * OHLC candles routed by token type. Returns an empty array for unknown
 * tokens with no pool — the consumer renders a "no chart data" placeholder.
 */
export function useTokenCandlesQuery(
    tokenId: string,
    period: TimePeriod,
    poolAddress: string | null | undefined,
    isPax: boolean,
    isWpax: boolean,
    options?: Partial<UseQueryOptions<OhlcCandle[]>>,
) {
    const isSid = tokenId.toLowerCase() === SID_TOKEN_ADDRESS.toLowerCase();

    return useQuery<OhlcCandle[]>({
        queryKey: ['tokenCandles', tokenId.toLowerCase(), period, poolAddress ?? null],
        queryFn: async () => {
            const cfg = TIME_PERIODS.find((t) => t.id === period)!;
            try {
                if (isPax || isWpax) {
                    const ohlc = await fetchPaxHistory(isWpax ? 'WPAX' : 'PAX', cfg.paxRes, cfg.paxCount);
                    return (ohlc as any).ohlc || [];
                }
                if (isSid) {
                    const ohlc = await fetchSidCandles(cfg.resolution, cfg.countback);
                    return (ohlc as any).ohlc || [];
                }
                const cvSymbol = getCrossverseSymbol(tokenId);
                if (cvSymbol) {
                    const ohlc = await fetchCrossverseHistory(cvSymbol, cfg.paxRes, cfg.paxCount);
                    return ohlc.ohlc;
                }
                if (poolAddress) {
                    const ohlc = await fetchSidioraCandles(poolAddress, cfg.resolution, cfg.countback);
                    return (ohlc as any).ohlc || [];
                }
            } catch {
                /* chart load failed — fall through to empty array */
            }
            return [];
        },
        // Don't fetch until we know whether this is a Sidiora token (poolAddress
        // resolution is async). For PAX/WPAX/SID we don't need a pool address.
        enabled:
            Boolean(tokenId) && (isPax || isWpax || isSid || Boolean(poolAddress) || Boolean(getCrossverseSymbol(tokenId))),
        staleTime: 30_000,
        ...options,
    });
}

/**
 * Live price for crossverse-registered tokens (SOL, ETH, BNB, PAX ERC-20).
 * Disabled when cvSymbol is null (non-crossverse tokens).
 */
export function useCrossversePriceQuery(
    cvSymbol: string | null,
    options?: Partial<UseQueryOptions<PaxPriceLatest>>,
) {
    return useQuery<PaxPriceLatest>({
        queryKey: ['crossversePrice', cvSymbol ?? ''],
        queryFn: () => fetchCrossversePrice(cvSymbol!),
        enabled: Boolean(cvSymbol),
        staleTime: 30_000,
        ...options,
    });
}
