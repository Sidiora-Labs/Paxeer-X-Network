import { PAXEER_CONFIG } from './constants';
import { PWA_NETWORK_ENV, configuredBase } from '@/pwa/config';
import { rewriteLogoUrl } from './mediaProxy';
import {
    watchAddress as portfolioWatchAddress,
    getWatchStatus as portfolioGetWatchStatus,
    unwatchAddress as portfolioUnwatchAddress,
    createWebhook as portfolioCreateWebhook,
    listWebhooks as portfolioListWebhooks,
    getWebhook as portfolioGetWebhook,
    updateWebhook as portfolioUpdateWebhook,
    deleteWebhook as portfolioDeleteWebhook,
    getWebhookDeliveries as portfolioGetWebhookDeliveries,
} from '@/lib/portfolio-api';
import type {
    ApiV2WatchRequest,
    CreateWebhookRequest,
    UpdateWebhookRequest,
} from '@/lib/portfolio-api';
import {
    initBlockscout,
    getWalletPortfolio,
    getWalletHoldings,
    getWalletBalance,
    getWalletTransactions,
    getWalletTokenMetadata,
    getWalletTopTokens,
    searchWalletTokens,
    getWalletTxDetail,
    getWalletTxTokenTransfers,
    getWalletTxLogs,
    getWalletAddressTokenBalances,
    getWalletTokenHolders,
    getWalletAddressCounters,
    getWalletCoinBalanceHistory,
} from '@/lib/data';
import type {
    WalletPortfolio,
    WalletTokenHolding,
    WalletBalanceSummary,
    WalletTransactionsResponse,
    WalletTokenMetadata,
    WalletToken,
} from '@/lib/data';
import type {
    PnlHistoryResponse,
    ChartResponse,
    ChartPeriod,
    UserRank,
    UserPerformance,
    UserProfile,
    AssetChartResponse,
    CandleTimeframe,
    TrendingResponse,
    TrendingToken,
    DexHistoryResponse,
    RewardsSummary,
    RewardsQuestsResponse,
    RewardsAirdropsResponse,
    RewardsReferralsResponse,
    RewardsHistoryResponse,
    RewardsLeaderboardResponse,
    HealthResponse,
} from './portfolio-types';

// Re-export wallet-data types as the canonical portfolio/balance types
export type {
    WalletPortfolio as Portfolio,
    WalletTokenHolding as TokenHolding,
    WalletTokenMetadata as TokenMetadata,
    WalletTransactionsResponse as TransactionResponse,
    WalletBalanceSummary as BalanceResponse,
    WalletToken as PaxscanToken,
};

export type {
    PnlHistoryResponse,
    ChartResponse,
    ChartPeriod,
    UserRank,
    UserPerformance,
    UserProfile,
    AssetChartResponse,
    CandleTimeframe,
    TrendingResponse,
    TrendingToken,
    DexHistoryResponse,
    RewardsSummary,
    RewardsQuestsResponse,
    RewardsAirdropsResponse,
    RewardsReferralsResponse,
    RewardsHistoryResponse,
    RewardsLeaderboardResponse,
};

export type { ApiV2WatchRequest, CreateWebhookRequest, UpdateWebhookRequest };

// ── Initialise Blockscout client (sole data source for on-chain data) ─────
initBlockscout({ baseUrl: PAXEER_CONFIG.blockscoutApiBase });

// ── Portfolio API — Watch / Webhooks (native fetch, see @/lib/portfolio-api) ─

// ── v1 API Transport (raw fetch — preserves snake_case types exactly) ────
const API_BASE = PAXEER_CONFIG.portfolioApiBase;

class PortfolioApiError extends Error {
    constructor(
        public readonly status: number,
        public readonly statusText: string,
        public readonly body: string,
        public readonly url: string,
    ) {
        super(`Portfolio API ${status} ${statusText}: ${body.slice(0, 200)}`);
        this.name = 'PortfolioApiError';
    }
}

async function apiGet<T>(
    path: string,
    params?: Record<string, string | number | boolean | undefined>,
): Promise<T> {
    let url = `${API_BASE}${path}`;
    if (params) {
        const qs = Object.entries(params)
            .filter(([, v]) => v !== undefined)
            .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
            .join('&');
        if (qs) url += `?${qs}`;
    }
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 30_000);
    try {
        const res = await fetch(url, {
            method: 'GET',
            headers: { Accept: 'application/json' },
            signal: controller.signal,
        });
        if (!res.ok) {
            const body = await res.text();
            throw new PortfolioApiError(res.status, res.statusText, body, url);
        }
        return (await res.json()) as T;
    } finally {
        clearTimeout(timer);
    }
}

async function apiPost<T>(path: string): Promise<T> {
    const url = `${API_BASE}${path}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 30_000);
    try {
        const res = await fetch(url, {
            method: 'POST',
            headers: { Accept: 'application/json' },
            signal: controller.signal,
        });
        if (!res.ok) {
            const body = await res.text();
            throw new PortfolioApiError(res.status, res.statusText, body, url);
        }
        return (await res.json()) as T;
    } finally {
        clearTimeout(timer);
    }
}

// ── Health ──────────────────────────────────────────────────────────────────

export const fetchHealth = (): Promise<HealthResponse> =>
    apiGet('/health');

// ── Tokens (Blockscout via @paxeer/wallet-data) ────────────────────────────

export const fetchTokenMetadata = (tokenAddress: string) =>
    getWalletTokenMetadata(tokenAddress);

// ── Portfolio (Blockscout via @paxeer/wallet-data) ─────────────────────────

export const fetchPortfolio = (address: string, _opts?: { fresh?: boolean }) =>
    getWalletPortfolio(address);

export const fetchPortfolioFresh = (address: string) =>
    getWalletPortfolio(address);

export const fetchHoldings = (address: string, _opts?: { fresh?: boolean }) =>
    getWalletHoldings(address);

export const fetchHoldingsFresh = (address: string) =>
    getWalletHoldings(address);

export const fetchPortfolioTransactions = (
    address: string,
    _limit = 50,
    _offset = 0,
) => getWalletTransactions(address);

// ── Balance & PnL ──────────────────────────────────────────────────────────

export const fetchBalance = (address: string, _opts?: { fresh?: boolean }) =>
    getWalletBalance(address);

export const fetchBalanceFresh = (address: string) =>
    getWalletBalance(address);

export const fetchPnlHistory = (address: string, days = 30): Promise<PnlHistoryResponse> =>
    apiGet(`/api/v1/portfolio/${address}/pnl`, { days });

// ── Portfolio Charts ────────────────────────────────────────────────────────

export const fetchPortfolioValueChart = (
    address: string,
    period: ChartPeriod = '30d',
): Promise<ChartResponse> =>
    apiGet(`/api/v1/portfolio/${address}/charts/value`, { period });

export const fetchPnlChart = (
    address: string,
    period: ChartPeriod = '30d',
): Promise<ChartResponse> =>
    apiGet(`/api/v1/portfolio/${address}/charts/pnl`, { period });

export const fetchHoldingsChart = (
    address: string,
    period: ChartPeriod = '30d',
): Promise<ChartResponse> =>
    apiGet(`/api/v1/portfolio/${address}/charts/holdings`, { period });

export const fetchTxVolumeChart = (
    address: string,
    period: ChartPeriod = '30d',
): Promise<ChartResponse> =>
    apiGet(`/api/v1/portfolio/${address}/charts/tx-volume`, { period });

// ── Phase 2: Enrichment (Argus + Auth) ──────────────────────────────────────

export const fetchUserRank = (address: string): Promise<UserRank> =>
    apiGet(`/api/v1/${address}/rank`);

export const fetchUserPerformance = (address: string): Promise<UserPerformance> =>
    apiGet(`/api/v1/${address}/performance`);

export const fetchUserProfile = (address: string): Promise<UserProfile> =>
    apiGet(`/api/v1/${address}/profile`);

// ── Phase 3: Asset Charts & DEX ─────────────────────────────────────────────

export const fetchAssetChart = (
    symbol: string,
    opts?: { timeframe?: CandleTimeframe; limit?: number },
): Promise<AssetChartResponse> =>
    apiGet(`/api/v1/charts/${symbol}`, opts);

export const fetchTrending = (limit = 20): Promise<TrendingResponse> =>
    apiGet('/api/v1/trending', { limit });

// ── Ranking Algorithm API (discovery rankings service) ──────────────────────
// All four bases route through /api/sdk/[...path] — upstream URL never ships
// in the client bundle (matches SIDIORA_SDK_UPSTREAM env var server-side).
const RANKING_API_BASE = '/api/sdk/ranking';
const STATS_API_BASE = '/api/sdk/stats';
const METADATA_API_BASE = '/api/sdk/metadata';

export type RankingCategory = 'trending' | 'breakout' | 'new' | 'top_volume' | 'unusual' | 'movers';

export interface RankedPoolStats {
    price?: string;
    priceChange1h?: string;
    priceChange24h?: string;
    volume24h?: string;
    volume1h?: string;
    marketCap?: string;
    holderCount?: number;
}

export interface RankedPool {
    poolAddress: string;
    score: number;
    rank: number;
    stats?: RankedPoolStats | null;
}

export interface RankingsResponse {
    category: RankingCategory;
    items: RankedPool[];
    total: number;
    limit: number;
    offset: number;
}

export interface PoolStats {
    poolAddress: string;
    tokenAddress: string;
    price: string;
    priceChange24h?: string;
    volume24h: string;
    marketCap?: string;
    holderCount?: number;
    riskRating?: number;
}

export interface TokenMetadataPublic {
    token_address: string;
    pool_address?: string | null;
    name?: string | null;
    symbol?: string | null;
    decimals?: number;
    total_supply?: string | null;
    creator?: string | null;
    description?: string | null;
    socials?: {
        website?: string | null;
        twitter?: string | null;
        telegram?: string | null;
        discord?: string | null;
    } | null;
    tags?: string[] | null;
    images?: { logo?: string | null; banner?: string | null };
    created_at?: number | null;
    updated_at?: number | null;
}

async function externalGet<T>(baseUrl: string, path: string): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15_000);
    try {
        const res = await fetch(`${baseUrl}${path}`, {
            headers: { Accept: 'application/json' },
            signal: controller.signal,
        });
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        return (await res.json()) as T;
    } finally {
        clearTimeout(timer);
    }
}

export async function fetchRankings(
    category: RankingCategory,
    limit = 20,
    offset = 0,
): Promise<RankingsResponse> {
    return externalGet(RANKING_API_BASE, `/rankings/${category}?limit=${limit}&offset=${offset}`);
}

export async function fetchBatchStats(poolAddresses: string[]): Promise<Record<string, PoolStats>> {
    if (poolAddresses.length === 0) return {};
    return externalGet(STATS_API_BASE, `/stats/batch?pools=${poolAddresses.join(',')}`);
}

export async function fetchTokenMetadataPublic(tokenAddress: string): Promise<TokenMetadataPublic> {
    return externalGet(METADATA_API_BASE, `/metadata/${tokenAddress}.json`);
}

export interface EnrichedRankedToken {
    poolAddress: string;
    tokenAddress: string;
    name: string;
    symbol: string;
    logoUrl: string | null;
    price: number;
    priceChange24h: number;
    volume24h: number;
    marketCap: number;
    holderCount: number;
    rank: number;
    score: number;
    category: RankingCategory;
}

export async function fetchEnrichedRankings(
    category: RankingCategory = 'trending',
    limit = 20,
): Promise<EnrichedRankedToken[]> {
    const rankings = await fetchRankings(category, limit);
    if (!rankings.items?.length) return [];

    const poolAddrs = rankings.items.map((r) => r.poolAddress);
    const batchStats = await fetchBatchStats(poolAddrs).catch(() => ({} as Record<string, PoolStats>));

    const tokenAddrs = new Map<string, string>();
    for (const pool of rankings.items) {
        const stats = batchStats[pool.poolAddress];
        if (stats?.tokenAddress) {
            tokenAddrs.set(pool.poolAddress, stats.tokenAddress);
        }
    }

    const metaResults = await Promise.allSettled(
        [...tokenAddrs.values()].map((addr) => fetchTokenMetadataPublic(addr)),
    );
    const metaMap = new Map<string, TokenMetadataPublic>();
    const addrs = [...tokenAddrs.values()];
    metaResults.forEach((r, i) => {
        if (r.status === 'fulfilled') metaMap.set(addrs[i].toLowerCase(), r.value);
    });

    return rankings.items.map((pool) => {
        const tokenAddr = tokenAddrs.get(pool.poolAddress) || '';
        const meta = metaMap.get(tokenAddr.toLowerCase());
        const stats = batchStats[pool.poolAddress] || pool.stats;
        const priceWad = parseFloat(stats?.price || '0');
        const price = priceWad > 0 ? priceWad / 1e18 : 0;
        const priceChange24hBps = parseFloat(stats?.priceChange24h || pool.stats?.priceChange24h || '0');
        const vol24hRaw = parseFloat(stats?.volume24h || pool.stats?.volume24h || '0');

        return {
            poolAddress: pool.poolAddress,
            tokenAddress: tokenAddr,
            name: meta?.name || tokenAddr.slice(0, 8) + '…',
            symbol: meta?.symbol || '???',
            logoUrl: rewriteLogoUrl(meta?.images?.logo) ?? null,
            price,
            priceChange24h: priceChange24hBps / 100,
            volume24h: vol24hRaw / 1e6,
            marketCap: parseFloat(stats?.marketCap || pool.stats?.marketCap || '0') / 1e6,
            holderCount: stats?.holderCount || pool.stats?.holderCount || 0,
            rank: pool.rank,
            score: pool.score,
            category,
        };
    });
}

export const fetchDexHistory = (
    address: string,
    opts?: { limit?: number; offset?: number },
): Promise<DexHistoryResponse> =>
    apiGet(`/api/v1/${address}/dex-history`, opts);

// ── v2 Real-Time Balance Indexer (Blockscout via @paxeer/wallet-data) ─────

export const fetchIndexedBalances = async (address: string, _fresh?: boolean) =>
    getWalletAddressTokenBalances(address);

export const fetchBalanceHistory = async (
    address: string,
    _opts?: { token?: string; limit?: number; offset?: number },
) =>
    getWalletCoinBalanceHistory(address);

export const fetchBalanceChanges = async (address: string, _limit?: number) =>
    getWalletCoinBalanceHistory(address);

// ── v2 Watch (register addresses for real-time tracking) ─────────────────

export const watchAddress = (body: ApiV2WatchRequest) =>
    portfolioWatchAddress(body);

export const getWatchStatus = (address: string) =>
    portfolioGetWatchStatus(address);

export const unwatchAddress = (address: string) =>
    portfolioUnwatchAddress(address);

// ── v2 Webhooks (balance change notifications) ───────────────────────────

export const createWebhook = (body: CreateWebhookRequest) =>
    portfolioCreateWebhook(body);

export const listWebhooks = (address: string) =>
    portfolioListWebhooks(address);

export const getWebhook = (id: string) =>
    portfolioGetWebhook(id);

export const updateWebhook = (id: string, body: UpdateWebhookRequest) =>
    portfolioUpdateWebhook(id, body);

export const deleteWebhook = (id: string) =>
    portfolioDeleteWebhook(id);

export const getWebhookDeliveries = (id: string, limit?: number) =>
    portfolioGetWebhookDeliveries(id, limit);

// ── v2 WebSocket URL (NEW — for real-time balance subscriptions) ─────────

export const getWebSocketUrl = (): string => {
    const base = PAXEER_CONFIG.portfolioApiBase.replace(/^http/, 'ws');
    return `${base}/api/v2/ws`;
};

// ── Points / Rewards (sidiora-points-indexer) ───────────────────────────────

const POINTS_API_BASE = (process.env.NEXT_PUBLIC_POINTS_API_BASE ?? '').replace(/\/+$/, '');

export interface PointsBalance {
    available: number;
    lifetimeEarned: number;
    lifetimeSpent: number;
    lifetimeBurned: number;
    pending: number;
    seasonName: string;
    seasonSlug: string;
    tierName: string;
    tierSlug: string;
    pointsMultiplier: number;
}

export interface PointsBalanceResponse {
    found: boolean;
    balance: PointsBalance | null;
}

export async function fetchPointsBalance(
    address: string,
): Promise<PointsBalanceResponse | null> {
    if (!POINTS_API_BASE) return null;
    try {
        const res = await fetch(
            `${POINTS_API_BASE}/points/balance/${address.toLowerCase()}`,
            { headers: { 'Accept': 'application/json', 'Content-Type': 'application/json' } },
        );
        if (!res.ok) return null;
        return res.json();
    } catch {
        return null;
    }
}

// ── Network Indexer API (Blockscout via @paxeer/wallet-data) ────────────────

export const fetchTxDetail = (hash: string) =>
    getWalletTxDetail(hash);

export const fetchTxTokenTransfers = (hash: string) =>
    getWalletTxTokenTransfers(hash);

export const fetchTxLogs = (hash: string) =>
    getWalletTxLogs(hash);

export const fetchAddressTxs = (address: string, filter?: string) =>
    getWalletTransactions(address, filter ? { filter: filter as 'to' | 'from' } : undefined);

export const fetchAddressTokenBalances = (address: string) =>
    getWalletAddressTokenBalances(address);

export const fetchTokenInfo = (tokenAddress: string) =>
    getWalletTokenMetadata(tokenAddress);

export const fetchTokenHolders = (tokenAddress: string) =>
    getWalletTokenHolders(tokenAddress);

export const fetchAddressCounters = (address: string) =>
    getWalletAddressCounters(address);

// ── Token Discovery (Blockscout via @paxeer/wallet-data) ────────────────────

export const fetchTopTokens = () => getWalletTopTokens();

export const searchTokens = (query: string) => searchWalletTokens(query);

// ── PAX/WPAX Price API (wallet.api.balance.paxportwallet.com) ─────────────
function marketDataBase(): string {
    const base = configuredBase(PWA_NETWORK_ENV.marketData);
    if (base === null) throw new Error(`${PWA_NETWORK_ENV.marketData} is not set`);
    return base;
}

export interface PaxPriceLatest {
    latest: number;
    lastUpdated?: number;
}

export interface PaxPriceResponse {
    symbol: string;
    price: number;
    timestamp: string;
    open: number;
    high: number;
    low: number;
}

export interface PaxHistoryCandle {
    t: number;  // timestamp
    o: number;  // open
    h: number;  // high
    l: number;  // low
    c: number;  // close
    v: number;  // volume
}

export interface PaxHistoryResponse {
    s: string;  // status "ok" or "error"
    t: number[];  // timestamps
    o: number[];  // opens
    h: number[];  // highs
    l: number[];  // lows
    c: number[];  // closes
    v: number[];  // volumes
}

export interface OhlcCandle {
    timestamp: number;
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
}

export interface OhlcResponse {
    interval: string;
    count: number;
    ohlc: OhlcCandle[];
    lastUpdated: number;
}

export async function fetchPaxPriceLatest(symbol: string = 'PAX'): Promise<PaxPriceLatest> {
    const res = await fetch(`${marketDataBase()}/pax/price/?symbol=${symbol}`);
    if (!res.ok) throw new Error(`Price API ${res.status}`);
    const data: PaxPriceResponse = await res.json();
    return {
        latest: data.price,
        lastUpdated: new Date(data.timestamp).getTime(),
    };
}

export async function fetchPaxHistory(
    symbol: string = 'PAX',
    resolution: number = 15,
    countback: number = 100,
): Promise<OhlcResponse> {
    const to = Math.floor(Date.now() / 1000);
    const from = to - (countback * resolution * 60);
    const res = await fetch(
        `/api/candle/pax/history?symbol=${symbol}&resolution=${resolution}&from=${from}&to=${to}&countback=${countback}`
    );
    if (!res.ok) throw new Error(`History API ${res.status}`);
    const data: PaxHistoryResponse = await res.json();

    if (data.s !== 'ok' || !data.t || data.t.length === 0) {
        return { interval: `${resolution}m`, count: 0, ohlc: [], lastUpdated: Date.now() };
    }

    const ohlc: OhlcCandle[] = data.t.map((timestamp, i) => ({
        timestamp: timestamp * 1000,
        open: data.o[i],
        high: data.h[i],
        low: data.l[i],
        close: data.c[i],
        volume: data.v[i],
    }));

    return {
        interval: `${resolution}m`,
        count: ohlc.length,
        ohlc,
        lastUpdated: Date.now(),
    };
}

// Legacy alias for backward compatibility
export async function fetchPaxOhlc(interval = '5m'): Promise<OhlcResponse> {
    const resolutionMap: Record<string, number> = {
        '1m': 1,
        '5m': 5,
        '15m': 15,
        '1h': 60,
        '4h': 240,
        '1d': 1440,
    };
    const resolution = resolutionMap[interval] || 5;
    return fetchPaxHistory('PAX', resolution, 100);
}

// ── Sidiora Pool APIs (Candles + Stats) ─────────────────────────────────────
const CANDLES_API_BASE = '/api/sdk/candles';

export interface SidioraPoolStats {
    poolAddress: string;
    tokenAddress: string;
    price: string;
    priceChange1m?: string;
    priceChange5m?: string;
    priceChange15m?: string;
    priceChange1h?: string;
    priceChange24h?: string;
    priceChangeDollar1h?: string;
    priceChangeDollar24h?: string;
    high24h?: string;
    low24h?: string;
    volume24h: string;
    volume1h?: string;
    volume5m?: string;
    marketCap?: string;
    buyCount24h?: number;
    sellCount24h?: number;
    uniqueTraders24h?: number;
    holderCount?: number;
    top10Concentration?: string;
    creatorHoldingsPct?: string;
    riskRating?: number;
    riskFactors?: string;
    createdAt?: number;
    updatedAt?: number;
}

export interface HolderDistribution {
    totalHolders: number;
    brackets: { label: string; count: number; totalBalancePctBps: number }[];
    top10: { address: string; balance: string; pctBps: number; rank: number }[];
    top10Pct: string;
    top20Pct: string;
    top50Pct: string;
}

export async function fetchSidioraPoolStats(poolAddress: string): Promise<SidioraPoolStats> {
    return externalGet(STATS_API_BASE, `/stats/${poolAddress}`);
}

export async function fetchSidioraPoolHolders(
    poolAddress: string,
): Promise<HolderDistribution> {
    return externalGet(STATS_API_BASE, `/stats/${poolAddress}/holders/distribution`);
}

export async function fetchSidioraCandles(
    poolAddress: string,
    resolution: string = '60',
    countback: number = 200,
): Promise<OhlcResponse> {
    const to = Math.floor(Date.now() / 1000);
    const from = to - countback * (resolutionToSeconds(resolution));
    const res = await fetch(
        `${CANDLES_API_BASE}/history?symbol=${poolAddress}&from=${from}&to=${to}&resolution=${resolution}&countback=${countback}`,
        { headers: { Accept: 'application/json' } },
    );
    if (!res.ok) throw new Error(`Candles API ${res.status}`);
    const data: PaxHistoryResponse = await res.json();
    if (data.s !== 'ok' || !data.t || data.t.length === 0) {
        return { interval: resolution, count: 0, ohlc: [], lastUpdated: Date.now() };
    }
    const ohlc: OhlcCandle[] = data.t.map((timestamp, i) => ({
        timestamp: timestamp * 1000,
        open: data.o[i],
        high: data.h[i],
        low: data.l[i],
        close: data.c[i],
        volume: data.v[i],
    }));
    return { interval: resolution, count: ohlc.length, ohlc, lastUpdated: Date.now() };
}

function resolutionToSeconds(res: string): number {
    const map: Record<string, number> = {
        '1': 60, '5': 300, '15': 900, '60': 3600, '240': 14400,
        '1D': 86400, 'D': 86400, '1W': 604800, 'W': 604800,
    };
    return map[res] || 3600;
}

// ── Crossverse token registry ────────────────────────────────────────────────
// Maps on-chain token address (lowercase) → crossverse symbol used in the
// data-api.crossverse.app/api/{symbol}/* routes.
// These tokens get their price + OHLC from the generic /api/candle/cv proxy.
export const CROSSVERSE_TOKEN_MAP: Record<string, string> = {
    '0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de': 'pax',  // PAX ERC-20
    '0x38416f047c53c6d295aff15e2fd296b6c896e2d8': 'sol',  // Bridged SOL
    '0x5ba2f89f60f5805512a265bdfbb8984c85b4c9b7': 'eth',  // Bridged ETH
    '0x2ce6495af2f6cf20ea1b4d637dc2e882a0276f36': 'bnb',  // Bridged BNB
};

export function getCrossverseSymbol(tokenId: string): string | null {
    return CROSSVERSE_TOKEN_MAP[tokenId.toLowerCase()] ?? null;
}

export async function fetchCrossverseHistory(
    cvSymbol: string,
    resolution: number = 15,
    countback: number = 100,
): Promise<OhlcResponse> {
    const to = Math.floor(Date.now() / 1000);
    const from = to - countback * resolution * 60;
    const symbol = cvSymbol.toUpperCase();
    const res = await fetch(
        `/api/candle/cv/${cvSymbol}/history?symbol=${symbol}&resolution=${resolution}&from=${from}&to=${to}&countback=${countback}`,
        { headers: { Accept: 'application/json' } },
    );
    if (!res.ok) throw new Error(`Crossverse history API ${res.status}`);
    const data: PaxHistoryResponse = await res.json();
    if (data.s !== 'ok' || !data.t || data.t.length === 0) {
        return { interval: `${resolution}m`, count: 0, ohlc: [], lastUpdated: Date.now() };
    }
    const ohlc: OhlcCandle[] = data.t.map((timestamp, i) => ({
        timestamp: timestamp * 1000,
        open: data.o[i],
        high: data.h[i],
        low: data.l[i],
        close: data.c[i],
        volume: data.v[i],
    }));
    return { interval: `${resolution}m`, count: ohlc.length, ohlc, lastUpdated: Date.now() };
}

export async function fetchCrossversePrice(cvSymbol: string): Promise<PaxPriceLatest> {
    const symbol = cvSymbol.toUpperCase();
    const res = await fetch(
        `${marketDataBase()}/${cvSymbol}/price/?symbol=${symbol}`,
    );
    if (!res.ok) throw new Error(`Crossverse price API ${res.status}`);
    const data: PaxPriceResponse = await res.json();
    return { latest: data.price, lastUpdated: new Date(data.timestamp).getTime() };
}

// ── SID Token Candle API ────────────────────────────────────────────────────
// Routed through /api/candle/sid proxy → data-api.crossverse.app/api/sid
export const SID_TOKEN_ADDRESS = '0x86949e4CdB89496490890B67C9cfF63eD8efB4b1';

export async function fetchSidCandles(
    resolution: string = '60',
    countback: number = 200,
): Promise<OhlcResponse> {
    const to = Math.floor(Date.now() / 1000);
    const from = to - countback * resolutionToSeconds(resolution);
    const res = await fetch(
        `/api/candle/sid/history?symbol=SID&from=${from}&to=${to}&resolution=${resolution}&countback=${countback}`,
        { headers: { Accept: 'application/json' } },
    );
    if (!res.ok) throw new Error(`SID Candles API ${res.status}`);
    const data: PaxHistoryResponse = await res.json();
    if (data.s !== 'ok' || !data.t || data.t.length === 0) {
        return { interval: resolution, count: 0, ohlc: [], lastUpdated: Date.now() };
    }
    const ohlc: OhlcCandle[] = data.t.map((timestamp, i) => ({
        timestamp: timestamp * 1000,
        open: data.o[i],
        high: data.h[i],
        low: data.l[i],
        close: data.c[i],
        volume: data.v[i],
    }));
    return { interval: resolution, count: ohlc.length, ohlc, lastUpdated: Date.now() };
}
