/**
 * Paxeer Portfolio API — TypeScript Types (v2-compatible snake_case)
 *
 * Preserved from v2 SDK to maintain backward compatibility with all UI code.
 * These interfaces match the API server's JSON response format exactly.
 */

// ─── Health ──────────────────────────────────────────────────────────────────

/** GET /health */
export interface HealthResponse {
  status: string;
  version: string;
}

// ─── Tokens ──────────────────────────────────────────────────────────────────

/** GET /api/v1/tokens */
export interface TokenSummary {
  total: number;
  complete_basic: number;
  with_icon: number;
  with_price: number;
  erc20_count: number;
  erc721_count: number;
}

/** GET /api/v1/tokens/audit */
export interface TokenAuditReport {
  total: number;
  complete_basic: number;
  with_icon: number;
  with_price: number;
  verified: number;
  by_type: {
    erc20: number;
    erc721: number;
    erc1155: number;
  };
  needing_enrichment_count: number;
  needing_enrichment_sample: TokenEnrichmentItem[];
}

export interface TokenEnrichmentItem {
  address: string;
  symbol: string;
  missing_icon: boolean;
  missing_price: boolean;
  missing_decimals: boolean;
}

/** POST /api/v1/tokens/sync */
export interface TokenSyncReport {
  success: boolean;
  total: number;
  complete: number;
  partial: number;
  missing: number;
  with_icon: number;
}

/** GET /api/v1/tokens/:address */
export interface TokenMetadata {
  address: string;
  name?: string | null;
  symbol?: string | null;
  decimals?: number | null;
  token_type?: string;
  total_supply?: string | null;
  holder_count?: number | null;
  icon_url?: string | null;
  description?: string | null;
  website?: string | null;
  twitter?: string | null;
  telegram?: string | null;
  discord?: string | null;
  is_verified?: boolean;
  pool_address?: string | null;
  message?: string;
}

// ─── Prices ──────────────────────────────────────────────────────────────────

/** POST /api/v1/prices/sync */
export interface PriceSyncReport {
  success: boolean;
  total_tokens: number;
  updated: number;
  by_source: {
    network: number;
    stablecoin: number;
  };
  native_pax_price: string;
}

// ─── Portfolio ───────────────────────────────────────────────────────────────

/** GET /api/v1/portfolio/:address */
export interface Portfolio {
  address: string;
  native_balance: NativeBalance;
  token_holdings: TokenHolding[];
  total_value_usd: string | null;
  token_count: number;
  transaction_count: number;
  transfer_count: number;
  computed_at: string;
}

export interface NativeBalance {
  symbol: string;
  balance_raw: string;
  balance: string;
  price_usd: string | null;
  value_usd: string | null;
}

/** GET /api/v1/portfolio/:address/holdings */
export interface TokenHolding {
  contract_address: string;
  symbol: string | null;
  name: string | null;
  decimals: number;
  balance_raw: string;
  balance: string;
  price_usd: string | null;
  value_usd: string | null;
  icon_url: string | null;
}

// ─── Transactions ────────────────────────────────────────────────────────────

export type TransactionType =
  | "native_transfer"
  | "transfer"
  | "token_transfer"
  | "nft_transfer"
  | "swap"
  | "add_liquidity"
  | "remove_liquidity"
  | "stake"
  | "unstake"
  | "claim_rewards"
  | "contract_deploy"
  | "contract_call"
  | "approval"
  | "bridge"
  | "unknown";

/** GET /api/v1/portfolio/:address/transactions — response wrapper */
export interface TransactionResponse {
  address: string;
  limit: number;
  offset: number;
  transactions: EnrichedTransaction[];
  token_transfers: TokenTransferItem[];
}

/** Native transaction item within TransactionResponse.transactions */
export interface EnrichedTransaction {
  tx_hash: string;
  block_number: number;
  timestamp: string;
  from_address: string;
  to_address: string | null;
  value_raw: string;
  value: string;
  direction: "in" | "out";
  gas_used: string;
  gas_price: string;
  gas_fee: string;
  status: boolean;
  tx_type: TransactionType;
  token_transfers: TokenTransferItem[];
}

/** Token transfer item within TransactionResponse.token_transfers */
export interface TokenTransferItem {
  tx_hash: string;
  token_address: string;
  token_symbol: string | null;
  token_name: string | null;
  token_decimals: number | null;
  from_address: string;
  to_address: string;
  amount_raw: string;
  amount: string;
  direction: "in" | "out";
  token_type: "ERC-20" | "ERC-721" | "ERC-1155";
  block_number: number;
  timestamp: string;
  log_index: number;
}

// ─── Balance / PnL ──────────────────────────────────────────────────────────

/** GET /api/v1/portfolio/:address/balance */
export interface BalanceResponse {
  address: string;
  native_balance_usd: string;
  token_balance_usd: string;
  total_balance_usd: string;
  native_balance: string;
  token_count: number;
  daily_pnl_usd: string | null;
  daily_pnl_percent: string | null;
  computed_at: string;
}

/** GET /api/v1/portfolio/:address/pnl */
export interface PnlHistoryResponse {
  address: string;
  days_requested: number;
  history: PnlHistoryItem[];
}

export interface PnlHistoryItem {
  date: string;
  total_value_usd: string;
  native_value_usd: string;
  token_value_usd: string;
  pnl_usd: string;
  pnl_percent: string;
}

// ─── Charts ──────────────────────────────────────────────────────────────────

export type ChartType = "portfolio_value" | "pnl" | "holdings_count" | "tx_volume";
export type ChartPeriod = "1d" | "7d" | "30d" | "90d" | "1y";

/** GET /api/v1/portfolio/:address/charts/* */
export interface ChartResponse {
  address: string;
  chart_type: ChartType;
  period: string;
  data: ChartDataPoint[];
}

export interface ChartDataPoint {
  date: string;
  value: string;
}

// ─── Phase 2: Enrichment ────────────────────────────────────────────────────

export type RankTier = "PLEBEIAN" | "LEGIONARY" | "CENTURION" | "PRAETORIAN" | "IMPERATOR";

/** GET /api/v1/:address/rank */
export interface UserRank {
  address: string;
  total_score: number;
  performance_score: number;
  risk_score: number;
  behavioral_score: number;
  rank: RankTier;
  percentile: number;
  leaderboard_position: number | null;
  season_id: string;
  calculated_at: string;
  is_qualified: boolean;
}

/** GET /api/v1/:address/performance */
export interface UserPerformance {
  address: string;
  season_id: string;
  roi: number;
  roi_percent: number;
  sharpe_ratio: number;
  win_rate: number;
  total_trades: number;
  winning_trades: number;
  losing_trades: number;
  avg_win_size: number;
  avg_loss_size: number;
  profit_factor: number;
  total_pnl: string;
  calculated_at: string;
  behavioral: BehavioralAnalysis | null;
}

export interface BehavioralAnalysis {
  position_sizing_consistency: number;
  diversification_score: number;
  gas_efficiency: number;
  trading_frequency: number;
  hold_duration_avg: number;
  protocol_usage_count: number;
  unique_tokens_traded: number;
  avg_time_between_trades: number;
}

export type FundStatus = "pending" | "funded" | "unknown";

/** GET /api/v1/:address/profile */
export interface UserProfile {
  address: string;
  first_name: string;
  last_name: string;
  email: string;
  funded: boolean;
  fund_status: FundStatus;
  fund_pax_tx: string | null;
  fund_usdl_tx: string | null;
  created_at: string | null;
}

// ─── Phase 3: Asset Charts ─────────────────────────────────────────────────

export type CandleTimeframe = "1m" | "2m" | "3m" | "5m" | "15m" | "30m" | "1h" | "4h" | "8h" | "1D" | "1W";

/** GET /api/v1/charts/:symbol */
export interface AssetChartResponse {
  symbol: string;
  timeframe: string;
  count: number;
  candles: Candle[];
}

export interface Candle {
  symbol: string;
  timeframe: string;
  timestamp: string;
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string | null;
}

// ─── Phase 3: DEX ──────────────────────────────────────────────────────────

export type TrendingCategory = "hot" | "warm" | "cold";

/** GET /api/v1/trending */
export interface TrendingResponse {
  count: number;
  tokens: TrendingToken[];
}

export interface TrendingToken {
  market_id: string;
  name: string;
  symbol: string;
  token_address: string;
  spot_price: string;
  market_cap: string;
  volume_24h: string;
  price_change_1h: string;
  price_change_24h: string;
  price_change_7d: string;
  holder_count: number;
  swap_count: number;
  trending_score: number;
  trending_rank: number;
  trending_category: TrendingCategory;
}

/** GET /api/v1/:address/dex-history */
export interface DexHistoryResponse {
  address: string;
  count: number;
  swaps: DexSwap[];
}

export interface DexSwap {
  id: string;
  market_name: string;
  market_symbol: string;
  tx_hash: string;
  timestamp: number;
  token_in: string;
  token_out: string;
  amount_in: string;
  amount_out: string;
  amount_in_usid: string;
  spot_price: string;
  fee_usid: string;
  price_impact: string;
  side: "BUY" | "SELL";
}

// ─── Query Params ────────────────────────────────────────────────────────────

export interface PaginationParams {
  limit?: number;
  offset?: number;
}

export interface TransactionQueryParams extends PaginationParams {}

export interface PnlQueryParams {
  days?: number;
}

export interface ChartQueryParams {
  period?: ChartPeriod;
}

export interface AssetChartQueryParams {
  timeframe?: CandleTimeframe;
  limit?: number;
}

export interface TrendingQueryParams {
  limit?: number;
}

export interface DexHistoryQueryParams extends PaginationParams {}

// ─── Phase 7: Rewards ───────────────────────────────────────────────────────

/** GET /api/v1/:address/rewards */
export interface RewardsSummary {
  address: string;
  points_balance: number;
  tier: string;
  leaderboard_rank: number | null;
  season: RewardsSeasonInfo | null;
  quests_completed: number;
  quests_total: number;
  airdrop_count: number;
  referral_count: number;
}

export interface RewardsSeasonInfo {
  id: string;
  name: string;
  start_date: string;
  end_date: string;
  is_active: boolean;
}

/** GET /api/v1/:address/rewards/quests */
export interface RewardsQuestsResponse {
  address: string;
  quests: RewardsQuest[];
}

export interface RewardsQuest {
  id: string;
  title: string;
  description: string;
  points: number;
  category: string;
  is_completed: boolean;
  progress: number;
  target: number;
  completed_at: string | null;
}

/** GET /api/v1/:address/rewards/airdrops */
export interface RewardsAirdropsResponse {
  address: string;
  active: RewardsAirdrop[];
  historical: RewardsAirdropClaim[];
}

export interface RewardsAirdrop {
  id: string;
  name: string;
  description: string;
  token_symbol: string;
  amount: string;
  eligible: boolean;
  claimed: boolean;
  claim_deadline: string | null;
}

export interface RewardsAirdropClaim {
  id: string;
  airdrop_name: string;
  token_symbol: string;
  amount: string;
  claimed_at: string;
  tx_hash: string | null;
}

/** GET /api/v1/:address/rewards/referrals */
export interface RewardsReferralsResponse {
  address: string;
  referral_code: string;
  referral_link: string;
  total_referrals: number;
  points_earned: number;
  referred_users: ReferredUser[];
}

export interface ReferredUser {
  address: string;
  joined_at: string;
  points_earned: number;
}

/** GET /api/v1/:address/rewards/history */
export interface RewardsHistoryResponse {
  address: string;
  count: number;
  entries: RewardsHistoryEntry[];
}

export interface RewardsHistoryEntry {
  id: string;
  type: "earn" | "spend" | "burn";
  points: number;
  description: string;
  source: string;
  created_at: string;
}

/** GET /api/v1/rewards/leaderboard */
export interface RewardsLeaderboardResponse {
  count: number;
  entries: RewardsLeaderboardEntry[];
}

export interface RewardsLeaderboardEntry {
  rank: number;
  address: string;
  points: number;
  tier: string;
}

export interface RewardsQueryParams extends PaginationParams {}
