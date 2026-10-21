/**
 * Centralized TanStack Query key factory.
 * All query keys in the app should be defined here for deduplication and
 * cache invalidation consistency.
 *
 * Keys under the `production-market` root carry lowercased 0x addresses as
 * discrete parts so the market cache pruner (market-session-context) can
 * evict inactive pool-scoped entries by address.
 */

const lower = (value: string) => value.toLowerCase();
const sorted = (values: readonly string[]) => [...new Set(values.map(lower))].sort();
const MARKET = 'production-market';
const ACCOUNT = 'private-account';

export const queryKeys = {
  // ── Token stats (per pool) ───────────────────────────────────
  tokenStats: (poolAddress: string) =>
    ['token-stats', poolAddress.toLowerCase()] as const,

  tokenStatsBatch: (pools: string[]) =>
    ['token-stats-batch', pools.map((p) => p.toLowerCase()).sort().join(',')] as const,

  // ── Token metadata (per token address) ───────────────────────
  tokenMetadata: (tokenAddress: string) =>
    ['token-metadata', tokenAddress.toLowerCase()] as const,

  tokenMetadataBatch: (tokens: string[]) =>
    ['token-metadata-batch', tokens.map((t) => t.toLowerCase()).sort().join(',')] as const,

  // ── Rankings ─────────────────────────────────────────────────
  ranking: (category: string, limit: number, offset: number) =>
    ['ranking', category, limit, offset] as const,

  // ── Platform stats ───────────────────────────────────────────
  platformStats: () => ['platform-stats'] as const,

  // ── Token risk ───────────────────────────────────────────────
  tokenRisk: (poolAddress: string) =>
    ['token-risk', poolAddress.toLowerCase()] as const,

  // ── Token transactions ───────────────────────────────────────
  tokenTransactions: (poolAddress: string) =>
    ['token-transactions', poolAddress.toLowerCase()] as const,

  // ── Pool transactions with filter ──────────────────────────
  poolTransactions: (poolAddress: string, filter: string = 'all') =>
    ['pool-transactions', poolAddress.toLowerCase(), filter] as const,

  // ── Token holders ────────────────────────────────────────────
  tokenHolders: (poolAddress: string) =>
    ['token-holders', poolAddress.toLowerCase()] as const,

  // ── Candle stats (ATH/ATL) ───────────────────────────────────
  candleStats: (poolAddress: string) =>
    ['candle-stats', poolAddress.toLowerCase()] as const,

  // ── User profile ─────────────────────────────────────────────
  userProfile: (wallet: string) =>
    ['user-profile', wallet.toLowerCase()] as const,

  // ── Typed market data layer (core/market/*) ──────────────────
  poolSnapshot: (address: string) => [MARKET, 'pool-snapshot', lower(address)] as const,
  poolDetail: (origin: string, pool: string) => [MARKET, origin, 'pool', lower(pool)] as const,
  statsBatch: (origin: string, pools: readonly string[]) => [MARKET, origin, 'stats-batch', ...sorted(pools)] as const,
  /** Session cache of one pool's immutable registry facts (outside the pruned `production-market` root). */
  poolListing: (origin: string, pool: string) => ['market', origin, 'pool-listing', lower(pool)] as const,
  poolListings: (origin: string, pools: readonly string[]) => [MARKET, origin, 'pool-listings', ...sorted(pools)] as const,
  poolDetailListings: (origin: string, pools: readonly string[]) => [MARKET, origin, 'pool-detail-listings', ...sorted(pools)] as const,
  poolStats: (pool: string) => [MARKET, 'pool-stats', lower(pool)] as const,
  rankings: (origin: string, category: string, cursor: string | null, limit: number) => [MARKET, origin, 'rankings', category, cursor, limit] as const,
  candles: (pool: string, interval: string, from: string, to: string, limit: number) => [MARKET, 'candles', lower(pool), interval, from, to, limit] as const,
  sparkline: (pool: string, from: string) => [MARKET, 'sparkline', lower(pool), from] as const,
  profile: (wallet: string) => [MARKET, 'profile', lower(wallet)] as const,
  walletIdentity: (wallet: string) => [MARKET, 'wallet-identity', lower(wallet)] as const,
  holders: (origin: string, pool: string, cursor: string | null) => [MARKET, origin, 'holders', lower(pool), cursor] as const,
  leaderboard: (category: string, window: string, limit: number) => [MARKET, 'leaderboard', category, window, limit] as const,
  feed: (order: string, cursor: string | null, limit: number) => [MARKET, 'feed', order, cursor, limit] as const,
  poolCallouts: (pool: string, cursor: string | null) => [MARKET, 'callouts', lower(pool), cursor] as const,
  watchlistRows: (pools: readonly string[]) => [MARKET, 'watchlist-rows', ...sorted(pools)] as const,
  search: (query: string, type: string, limit: number) => [MARKET, 'search', query, type, limit] as const,
  metadataV2: (origin: string, token: string | null) => [MARKET, origin, 'metadata-v2', token === null ? null : lower(token)] as const,
  trades: (origin: string, pool: string, cursor: string | null) => [MARKET, origin, 'trades-v2', lower(pool), cursor] as const,
  liveDirectory: () => [MARKET, 'live-directory'] as const,
  liveDirectoryContinuation: (start: string) => [MARKET, 'live-directory', 'continuation', start] as const,
  streamCandles: (pool: string, interval: string) => [MARKET, 'stream-candles', lower(pool), interval] as const,
  portfolioHistory: (wallet: string, interval: string, from: string, to: string) => [ACCOUNT, 'portfolio-history', lower(wallet), interval, from, to] as const,
  publicPortfolio: (wallet: string, cursor: string | null) => [MARKET, 'public-portfolio', lower(wallet), cursor] as const,
  legacyPositions: (wallet: string) => [MARKET, 'legacy-positions', lower(wallet)] as const,
  legacyTrades: (wallet: string) => [MARKET, 'legacy-trades', lower(wallet)] as const,
  bounties: (filter: string, cursor: string | null) => [MARKET, 'bounties', filter, cursor] as const,
  bountiesProbe: () => [MARKET, 'bounties', 'probe'] as const,
  bountiesStats: () => [MARKET, 'bounties', 'stats'] as const,
  bounty: (bountyId: string) => [MARKET, 'bounty', bountyId] as const,
  bountySubmissions: (bountyId: string) => [MARKET, 'bounty', bountyId, 'submissions'] as const,
  holderRewardsStats: () => [MARKET, 'holder-rewards-stats'] as const,
  holderRewardsPools: (sort: string, limit: number) => [MARKET, 'holder-rewards-pools', sort, limit] as const,
  holderRewardsAirdropBalance: (pool: string) => [MARKET, 'holder-rewards-airdrop-balance', lower(pool)] as const,
  cups: () => [MARKET, 'cups'] as const,
  cupCurrent: () => [MARKET, 'cups', 'current'] as const,
  cup: (cupId: string) => [MARKET, 'cup', cupId] as const,
  cupResults: (cupId: string) => [MARKET, 'cup', cupId, 'results'] as const,
  cupStandings: (cupId: string, cursor: string | null, limit: number) => [MARKET, 'cup', cupId, 'standings', cursor, limit] as const,
  followStats: (wallet: string) => [MARKET, 'follow-stats', lower(wallet)] as const,
  walletLinks: (wallet: string) => [ACCOUNT, 'wallet-links', lower(wallet)] as const,
  activity: (wallet: string, cursor: string | null) => [MARKET, 'activity', lower(wallet), cursor] as const,
  profileReplies: (wallet: string) => [MARKET, 'profile-replies', lower(wallet)] as const,
  calloutHistory: (wallet: string) => [MARKET, 'callout-history', lower(wallet)] as const,
  similarPools: (origin: string, q: string) => [MARKET, origin, 'similar-pools', lower(q)] as const,
  terminalFollowing: (wallet: string) => ['terminal', 'following', lower(wallet)] as const,
  terminalOrders: (sessionId: string, wallet: string, epoch: number | string) => [ACCOUNT, sessionId, lower(wallet), '125', String(epoch), 'terminal-orders', 'active'] as const,
} as const;
