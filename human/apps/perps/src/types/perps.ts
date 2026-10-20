// ═══════════════════════════════════════════════════════════════════════
//  Shared TypeScript types for the Sidiora Perpetual Protocol data layer
//  Derived from: @paxeer/api/perpetual.graphql + @paxeer/abi/*.ts
// ═══════════════════════════════════════════════════════════════════════

// ── V5 — Fee (FeeCollected) ────────────────────────────────────────────
export interface Fee {
  id: number;
  positionId: string;
  userAddress: string;
  marketId: number;
  feeType: number;
  feeTypeName: string | null;
  feeUsd: string;
  feeTokens: string;
  tokenAddress: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

// ── V5 — Trade Settlement (TradeSettled) ───────────────────────────────
export interface TradeSettlement {
  id: number;
  positionId: string;
  userAddress: string;
  marketId: number;
  tradeType: number;
  tradeTypeName: string | null;
  sizeUsd: string;
  executionPrice: string;
  grossPnl: string;
  totalFeesUsd: string;
  borrowingFeeUsd: string;
  fundingPaidUsd: string;
  netPayoutTokens: string;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

// ── V5 — OI Snapshot (OpenInterestChanged) ─────────────────────────────
export interface OiSnapshot {
  id: number;
  marketId: number;
  longOi: string;
  shortOi: string;
  deltaUsd: string;
  isIncrease: boolean;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

// ── V5 — Mark Price Record (MarkPriceChanged) ─────────────────────────
export interface MarkPriceRecord {
  id: number;
  marketId: number;
  markPrice: string;
  indexPrice: string;
  baseReserve: string;
  quoteReserve: string;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

// ── V5 — Vault Balance Record (VaultBalanceChanged) ────────────────────
export interface VaultBalanceRecord {
  id: number;
  tokenAddress: string;
  vaultType: number;
  vaultTypeName: string | null;
  newBalance: string;
  delta: string;
  isIncrease: boolean;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

// ── V5 — Funding Payment (PositionFundingApplied) ──────────────────────
export interface FundingPayment {
  id: number;
  positionId: string;
  userAddress: string;
  marketId: number;
  fundingPaymentUsd: string;
  newCollateralUsd: string;
  newCollateralAmount: string;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

// ── V5 — Market Snapshot ───────────────────────────────────────────────
export interface MarketSnapshotRecord {
  id: number;
  marketId: number;
  longOi: string;
  shortOi: string;
  markPrice: string;
  indexPrice: string;
  fundingRatePerSecond: string;
  fundingRate24h: string;
  volume24hUsd: string;
  onchainTimestamp: number;
  blockNumber: number;
  blockTimestamp: string;
}

// ── V5 — Protocol Snapshot ─────────────────────────────────────────────
export interface ProtocolSnapshotRecord {
  id: number;
  totalPositions: string;
  totalOpenPositions: string;
  totalMarkets: string;
  tvlUsd: string;
  insuranceTotalUsd: string;
  onchainTimestamp: number;
  blockNumber: number;
  blockTimestamp: string;
}

// ── Aggregates (existing, typed for re-export) ─────────────────────────
export interface GlobalStats {
  totalMarkets: number;
  totalPositions: number;
  openPositions: number;
  totalTrades: number;
  totalLiquidations: number;
  totalVolume: string;
  totalUsers: number;
  indexerBlock: number;
}

export interface MarketStats {
  marketId: number;
  symbol: string | null;
  totalPositions: number;
  openPositions: number;
  totalTrades: number;
  totalLiquidations: number;
  totalVolume: string;
  latestPrice: string | null;
  latestFundingRate: string | null;
}

// ── Orderly / Enriched types ───────────────────────────────────────────
export interface OrderlyTicker {
  symbol: string;
  paxeerSymbol: string;
  mark_price: number | null;
  index_price: number | null;
  sum_unitary_funding: number | null;
  est_funding_rate: number | null;
  last_funding_rate: number | null;
  next_funding_time: number | null;
  open_interest: number | null;
  h24_open: number | null;
  h24_close: number | null;
  h24_high: number | null;
  h24_low: number | null;
  h24_amount: number | null;
  h24_volume: number | null;
}

export interface OrderlyVolumeStats {
  perp_volume_ytd: number | null;
  perp_volume_ltd: number | null;
  perp_volume_today: number | null;
  perp_volume_last_1_day: number | null;
  perp_volume_last_7_days: number | null;
  perp_volume_last_30_days: number | null;
}

export interface OrderlyPriceChange {
  symbol: string;
  paxeerSymbol: string;
  last_price: number | null;
  change_5m: number | null;
  change_30m: number | null;
  change_1h: number | null;
  change_4h: number | null;
  change_24h: number | null;
  change_3d: number | null;
  change_7d: number | null;
  change_30d: number | null;
}

export interface OrderlyOpenInterest {
  symbol: string;
  paxeerSymbol: string;
  long_oi: number | null;
  short_oi: number | null;
}

export interface OrderlyFundingRate {
  symbol: string;
  paxeerSymbol: string;
  est_funding_rate: number | null;
  est_funding_rate_timestamp: number | null;
  last_funding_rate: number | null;
  last_funding_rate_timestamp: number | null;
  next_funding_time: number | null;
  sum_unitary_funding: number | null;
}

export interface EnrichedGlobalStats {
  onchain: GlobalStats;
  orderly: OrderlyVolumeStats | null;
}

export interface EnrichedMarketStats {
  onchain: MarketStats;
  ticker: OrderlyTicker | null;
  fundingRate: OrderlyFundingRate | null;
  openInterest: OrderlyOpenInterest | null;
  priceChange: OrderlyPriceChange | null;
}

// ── Spot Market (from SpotMarketRegistryFacet ABI) ─────────────────────
export interface SpotMarket {
  marketId: `0x${string}`;
  baseToken: `0x${string}`;
  quoteToken: `0x${string}`;
  mode: number;
  active: boolean;
  minOrderSize: bigint;
  maxOffsetBps: number;
  takerFeeBps: number;
  makerRebateBps: number;
  batchModeUntilBlock: bigint;
}

// ── Spot Order (from SpotOrderBookFacet ABI) ───────────────────────────
export interface SpotOrder {
  id: bigint;
  trader: `0x${string}`;
  marketId: `0x${string}`;
  side: number;
  orderType: number;
  offsetBps: number;
  size: bigint;
  filledSize: bigint;
  blockSubmitted: bigint;
  active: boolean;
}

// ── Spot Book Level (from getSpotBookDepth) ────────────────────────────
export interface SpotBookLevel {
  offset: number;
  size: bigint;
}

// ── Spot Epoch Info (from SpotSettlementFacet ABI) ─────────────────────
export interface SpotEpochInfo {
  epochCounter: bigint;
  epochStart: bigint;
  epochLength: bigint;
  dirtyUserCount: bigint;
}

// ── Spot Order Placement Args ──────────────────────────────────────────
export interface PlaceSpotOrderArgs {
  marketId: `0x${string}`;
  side: number;
  orderType: number;
  offsetBps: number;
  size: bigint;
}

// ── Spot Collateral Args ───────────────────────────────────────────────
export interface SpotCollateralArgs {
  token: `0x${string}`;
  amount: bigint;
}

// ── PLV Score (from PLVRegistryFacet ABI) ──────────────────────────────
export interface PLVScore {
  vault: `0x${string}`;
  score: bigint;
  weight: bigint;
}

// ── PLV Quote (from PLVRegistryFacet ABI) ──────────────────────────────
export interface PLVQuote {
  price: bigint;
  maxFillSize: bigint;
}

export interface PLVQuoteArgs {
  vault: `0x${string}`;
  side: number;
  size: bigint;
  oraclePrice: bigint;
  volatility: bigint;
}

// ── PoFQ Types (from PoFQFacet ABI) ───────────────────────────────────
export interface PoFQScore {
  score: bigint;
  weight: bigint;
}

export interface TraderFeeTier {
  tier: number;
  volume30d: bigint;
  rebateBps: bigint;
}

export interface FeeTierConfig {
  thresholds: readonly [bigint, bigint, bigint, bigint];
  rebates: readonly [bigint, bigint, bigint, bigint];
}

// ── Margin Mode (from TradingAccount ABI) ──────────────────────────────
export enum MarginMode {
  Cross = 0,
  Isolated = 1,
}

export interface PositionMargin {
  token: `0x${string}`;
  lockedAmount: bigint;
  marketId: bigint;
  isLong: boolean;
}

export interface AccountSummary {
  idle: bigint;
  totalLocked: bigint;
  positionCount: bigint;
}

export interface TopUpPositionArgs {
  positionId: bigint;
  amount: bigint;
}

export interface TransferMarginArgs {
  fromPositionId: bigint;
  toPositionId: bigint;
  amount: bigint;
}

// ── PoFQ Tier Labels (used by config) ──────────────────────────────────
export type PoFQTierIndex = 0 | 1 | 2 | 3;

// ── Spot Order Side / Type Enums ───────────────────────────────────────
export enum SpotSide {
  Buy = 0,
  Sell = 1,
}

export enum SpotOrderType {
  Market = 0,
  Limit = 1,
}

// ═══════════════════════════════════════════════════════════════════════
//  V4 SPOT — Subgraph types (indexed historical events)
//  Prefix: Sg = SubGraph. Disambiguates from RPC-derived types above
//  whose shapes come from the Diamond facet structs.
// ═══════════════════════════════════════════════════════════════════════

export interface SgSpotMarket {
  marketId: string;
  baseToken: string;
  quoteToken: string;
  mode: number;
  modeName: string | null;
  status: string;
  createdAt: string | null;
  createdBlock: number | null;
  createdTxHash: string | null;
  lastModeChangedAt: string | null;
  lastModeChangeBlock: number | null;
  lastModeChangeTx: string | null;
}

export interface SgSpotOrder {
  id: any;
  orderId: string;
  traderAddress: string;
  marketId: string;
  side: number;
  sideName: string | null;
  orderType: number;
  orderTypeName: string | null;
  offsetBps: number;
  size: string;
  filledSize: string;
  status: string;
  placedAt: string;
  placedBlock: number;
  placedTxHash: string;
  resolvedAt: string | null;
  resolvedBlock: number | null;
  resolvedTxHash: string | null;
  market?: SgSpotMarket | null;
  fills?: SgSpotOrderFill[];
}

export interface SgSpotOrderFill {
  id: number;
  orderId: string;
  traderAddress: string;
  marketId: string;
  fillSize: string;
  fillPrice: string;
  fillScore: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotBatchClearing {
  id: number;
  marketId: string;
  clearingOffsetBps: number;
  clearingPrice: string;
  matchedVolume: string;
  numBuysFilled: number;
  numSellsFilled: number;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotEpochSettlement {
  epochNumber: string;
  usersProcessed: number;
  onchainBlock: number;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotFastSettlement {
  id: number;
  userAddress: string;
  tokenAddress: string;
  amount: string;
  fee: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotCollateralMovement {
  id: number;
  eventType: string;
  userAddress: string;
  tokenAddress: string;
  amount: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotMarketModeChange {
  id: number;
  marketId: string;
  oldMode: number;
  newMode: number;
  batchModeUntilBlock: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotPofqUpdate {
  id: number;
  entityAddress: string;
  newScore: string;
  newWeight: string;
  isVault: boolean;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}

export interface SgSpotPLV {
  vaultAddress: string;
  isRegistered: boolean;
  registeredAt: string | null;
  registeredBlock: number | null;
  registeredTxHash: string | null;
  deregisteredAt: string | null;
  deregisteredBlock: number | null;
  deregisteredTxHash: string | null;
}

export interface SgSpotKeeperCycle {
  id: number;
  onchainTimestamp: number;
  marketsEvaluated: number;
  batchesCleared: number;
  epochSettled: boolean;
  blockNumber: number;
  txHash: string;
  blockTimestamp: string;
}

export interface SgSpotBatchClearFailure {
  id: number;
  marketId: string;
  reason: string;
  blockNumber: number;
  txHash: string;
  logIndex: number;
  blockTimestamp: string;
}
