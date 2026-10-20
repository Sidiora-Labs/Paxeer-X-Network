package graphql

import (
	"database/sql"

	graphql "github.com/graph-gophers/graphql-go"
)

type Resolver struct {
	DB *sql.DB
}

func NewSchema(db *sql.DB) (*graphql.Schema, error) {
	return graphql.ParseSchema(SDL, &Resolver{DB: db},
		graphql.UseFieldResolvers(), graphql.MaxDepth(10), graphql.MaxParallelism(20))
}

const SDL = `scalar BigDecimal

scalar DateTime

type Market {
  marketId: Int!
  name: String!
  symbol: String!
  maxLeverage: BigDecimal!
  enabled: Boolean!
  createdAt: DateTime
  latestPrice: LatestPrice
  poolState: PoolState
  fundingRate: FundingRate
}

type Position {
  positionId: BigDecimal!
  userAddress: String!
  marketId: Int!
  isLong: Boolean!
  sizeUsd: BigDecimal!
  leverage: BigDecimal!
  entryPrice: BigDecimal!
  collateralToken: String
  collateralAmount: BigDecimal!
  collateralUsd: BigDecimal!
  status: String!
  realizedPnl: BigDecimal
  exitPrice: BigDecimal
  openedAt: DateTime
  closedAt: DateTime
  openBlock: Int
  closeBlock: Int
  openTxHash: String
  closeTxHash: String
  market: Market
}

type Trade {
  executionPrice: BigDecimal
  id: Int!
  positionId: BigDecimal!
  userAddress: String
  marketId: Int
  tradeType: String!
  isLong: Boolean
  sizeUsd: BigDecimal!
  price: BigDecimal!
  realizedPnl: BigDecimal
  feeUsd: BigDecimal
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type Order {
  remainingSize: BigDecimal
  blockTimestamp: DateTime
  orderId: BigDecimal!
  userAddress: String!
  marketId: Int!
  orderType: Int!
  orderTypeName: String
  isLong: Boolean!
  triggerPrice: BigDecimal!
  sizeUsd: BigDecimal!
  status: String!
  positionId: BigDecimal
  executionPrice: BigDecimal
  failureReason: String
  placedAt: DateTime
  resolvedAt: DateTime
  placedBlock: Int
  resolvedBlock: Int
  placedTxHash: String
  resolvedTxHash: String
}

type Liquidation {
  sizeUsd: BigDecimal
  executionPrice: BigDecimal
  id: Int!
  positionId: BigDecimal!
  userAddress: String!
  marketId: Int!
  price: BigDecimal!
  penalty: BigDecimal!
  keeper: String!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type PriceUpdate {
  updatedAt: DateTime
  timestamp: Int
  id: Int!
  marketId: Int!
  price: BigDecimal!
  onchainTimestamp: Int!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type LatestPrice {
  marketId: Int!
  price: BigDecimal!
  onchainTimestamp: Int!
  blockNumber: Int!
  updatedAt: DateTime
}

type FundingRate {
  id: Int!
  marketId: Int!
  ratePerSecond: BigDecimal!
  rate24h: BigDecimal!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type UserVault {
  marginMode: Int
  userAddress: String!
  vaultAddress: String!
  createdAt: DateTime
  blockNumber: Int
  txHash: String
}

type VaultEvent {
  id: Int!
  eventType: String!
  userAddress: String
  tokenAddress: String!
  amount: BigDecimal!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type CollateralToken {
  tokenAddress: String!
  decimals: Int!
  isActive: Boolean!
  addedAt: DateTime
}

type PoolState {
  marketId: Int!
  baseReserve: BigDecimal!
  quoteReserve: BigDecimal!
  oraclePrice: BigDecimal
  updatedAt: DateTime
  blockNumber: Int
}

type FeeConfig {
  takerFeeBps: Int!
  makerFeeBps: Int!
  liquidationFeeBps: Int!
  insuranceFeeBps: Int!
  updatedAt: DateTime
}

type ProtocolEvent {
  id: Int!
  eventName: String!
  eventData: String!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type IndexerStatus {
  lastIndexedBlock: Int!
  chainHead: Int
  blocksScanned: Int
  eventsProcessed: Int
  isSynced: Boolean
}

type KeeperCycle {
  id: Int!
  onchainTimestamp: Int!
  marketsUpdated: Int!
  ordersExecuted: Int!
  liquidationsExecuted: Int!
  ordersFailed: Int!
  liquidationsFailed: Int!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type AccountLedgerEntry {
  id: Int!
  entryId: BigDecimal!
  userAddress: String!
  entryType: Int!
  tokenAddress: String!
  amount: BigDecimal!
  positionId: BigDecimal!
  isDebit: Boolean!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type Delegate {
  id: Int!
  userAddress: String!
  delegateAddress: String!
  canTrade: Boolean!
  canWithdraw: Boolean!
  canModifyMargin: Boolean!
  expiry: BigDecimal!
  isActive: Boolean!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type TradingAccountEvent {
  id: Int!
  eventType: String!
  userAddress: String!
  positionId: BigDecimal
  tokenAddress: String
  amount: BigDecimal
  extraData: String
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type Fee {
  id: Int!
  positionId: BigDecimal!
  userAddress: String!
  marketId: Int!
  feeType: Int!
  feeTypeName: String
  feeUsd: BigDecimal!
  feeTokens: BigDecimal!
  tokenAddress: String!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type TradeSettlement {
  id: Int!
  positionId: BigDecimal!
  userAddress: String!
  marketId: Int!
  tradeType: Int!
  tradeTypeName: String
  sizeUsd: BigDecimal!
  executionPrice: BigDecimal!
  grossPnl: BigDecimal!
  totalFeesUsd: BigDecimal!
  borrowingFeeUsd: BigDecimal!
  fundingPaidUsd: BigDecimal!
  netPayoutTokens: BigDecimal!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type OiSnapshot {
  id: Int!
  marketId: Int!
  longOi: BigDecimal!
  shortOi: BigDecimal!
  deltaUsd: BigDecimal!
  isIncrease: Boolean!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type MarkPriceRecord {
  id: Int!
  marketId: Int!
  markPrice: BigDecimal!
  indexPrice: BigDecimal!
  baseReserve: BigDecimal!
  quoteReserve: BigDecimal!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type VaultBalanceRecord {
  id: Int!
  tokenAddress: String!
  vaultType: Int!
  vaultTypeName: String
  newBalance: BigDecimal!
  delta: BigDecimal!
  isIncrease: Boolean!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type FundingPayment {
  id: Int!
  positionId: BigDecimal!
  userAddress: String!
  marketId: Int!
  fundingPaymentUsd: BigDecimal!
  newCollateralUsd: BigDecimal!
  newCollateralAmount: BigDecimal!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type MarketSnapshotRecord {
  id: Int!
  marketId: Int!
  longOi: BigDecimal
  shortOi: BigDecimal
  markPrice: BigDecimal!
  indexPrice: BigDecimal
  fundingRatePerSecond: BigDecimal
  fundingRate24h: BigDecimal
  volume24hUsd: BigDecimal!
  onchainTimestamp: Int!
  blockNumber: Int!
  blockTimestamp: DateTime!
}

type ProtocolSnapshotRecord {
  id: Int!
  totalPositions: BigDecimal!
  totalOpenPositions: BigDecimal!
  totalMarkets: BigDecimal!
  tvlUsd: BigDecimal!
  insuranceTotalUsd: BigDecimal!
  onchainTimestamp: Int!
  blockNumber: Int!
  blockTimestamp: DateTime!
}

type UserStats {
  address: String
  totalPnl: BigDecimal
  totalVolume: BigDecimal
  totalFeesPaid: BigDecimal
  winCount: Int
  lossCount: Int
  liquidationCount: Int
  userAddress: String!
  totalPositions: Int!
  openPositions: Int!
  closedPositions: Int!
  liquidatedPositions: Int!
  totalTrades: Int!
  totalRealizedPnl: BigDecimal!
  totalOrders: Int!
  activeOrders: Int!
}

type MarketStats {
  marketId: Int!
  symbol: String
  totalPositions: Int!
  openPositions: Int!
  totalTrades: Int!
  totalLiquidations: Int!
  totalVolume: BigDecimal!
  latestPrice: BigDecimal
  latestFundingRate: BigDecimal
}

type GlobalStats {
  totalMarkets: Int!
  totalPositions: Int!
  openPositions: Int!
  totalTrades: Int!
  totalLiquidations: Int!
  totalVolume: BigDecimal!
  totalUsers: Int!
  indexerBlock: Int!
}

type OrderlyTicker {
  symbol: String!
  paxeerSymbol: String!
  mark_price: Float
  index_price: Float
  sum_unitary_funding: Float
  est_funding_rate: Float
  last_funding_rate: Float
  next_funding_time: Float
  open_interest: Float
  h24_open: Float
  h24_close: Float
  h24_high: Float
  h24_low: Float
  h24_amount: Float
  h24_volume: Float
}

type OrderlyVolumeStats {
  perp_volume_ytd: Float
  perp_volume_ltd: Float
  perp_volume_today: Float
  perp_volume_last_1_day: Float
  perp_volume_last_7_days: Float
  perp_volume_last_30_days: Float
}

type OrderlyPriceChange {
  symbol: String!
  paxeerSymbol: String!
  last_price: Float
  change_5m: Float
  change_30m: Float
  change_1h: Float
  change_4h: Float
  change_24h: Float
  change_3d: Float
  change_7d: Float
  change_30d: Float
}

type OrderlyOpenInterest {
  symbol: String!
  paxeerSymbol: String!
  long_oi: Float
  short_oi: Float
}

type OrderlyFundingRate {
  symbol: String!
  paxeerSymbol: String!
  est_funding_rate: Float
  est_funding_rate_timestamp: Float
  last_funding_rate: Float
  last_funding_rate_timestamp: Float
  next_funding_time: Float
  sum_unitary_funding: Float
}

type EnrichedGlobalStats {
  onchain: GlobalStats!
  orderly: OrderlyVolumeStats
}

type EnrichedMarketStats {
  onchain: MarketStats!
  ticker: OrderlyTicker
  fundingRate: OrderlyFundingRate
  openInterest: OrderlyOpenInterest
  priceChange: OrderlyPriceChange
}

type Query {
  leaderboard(limit: Int, offset: Int): [LeaderboardEntry!]!
  position(positionId: String!): Position
  positions(
    userAddress: String
    marketId: Int
    status: String
    limit: Int
    offset: Int
  ): [Position!]!

  trades(
    userAddress: String
    marketId: Int
    positionId: String
    tradeType: String
    limit: Int
    offset: Int
  ): [Trade!]!

  order(orderId: String!): Order
  orders(
    userAddress: String
    marketId: Int
    status: String
    orderType: Int
    limit: Int
    offset: Int
  ): [Order!]!

  liquidations(
    userAddress: String
    marketId: Int
    limit: Int
    offset: Int
  ): [Liquidation!]!

  market(marketId: Int!): Market
  markets: [Market!]!

  latestPrices: [LatestPrice!]!
  priceHistory(
    marketId: Int!
    limit: Int
    offset: Int
  ): [PriceUpdate!]!

  fundingRates(
    marketId: Int!
    limit: Int
    offset: Int
  ): [FundingRate!]!

  userVault(userAddress: String!): UserVault
  vaultEvents(
    userAddress: String
    eventType: String
    limit: Int
    offset: Int
  ): [VaultEvent!]!

  collateralTokens: [CollateralToken!]!

  poolStates: [PoolState!]!
  poolState(marketId: Int!): PoolState

  feeConfig: FeeConfig

  protocolEvents(
    eventName: String
    limit: Int
    offset: Int
  ): [ProtocolEvent!]!

  userStats(userAddress: String, address: String): UserStats
  marketStats(marketId: Int!): MarketStats
  globalStats: GlobalStats
  indexerStatus: IndexerStatus

  keeperCycles(limit: Int, offset: Int): [KeeperCycle!]!

  accountLedger(
    userAddress: String
    positionId: String
    limit: Int
    offset: Int
  ): [AccountLedgerEntry!]!

  delegates(userAddress: String!): [Delegate!]!

  tradingAccountEvents(
    userAddress: String
    eventType: String
    limit: Int
    offset: Int
  ): [TradingAccountEvent!]!

  fees(
    userAddress: String
    marketId: Int
    positionId: String
    feeType: Int
    limit: Int
    offset: Int
  ): [Fee!]!

  tradeSettlements(
    userAddress: String
    marketId: Int
    positionId: String
    tradeType: Int
    limit: Int
    offset: Int
  ): [TradeSettlement!]!

  oiSnapshots(
    marketId: Int!
    limit: Int
    offset: Int
  ): [OiSnapshot!]!

  markPriceHistory(
    marketId: Int!
    limit: Int
    offset: Int
  ): [MarkPriceRecord!]!

  vaultBalanceHistory(
    tokenAddress: String
    vaultType: Int
    limit: Int
    offset: Int
  ): [VaultBalanceRecord!]!

  fundingPayments(
    userAddress: String
    marketId: Int
    positionId: String
    limit: Int
    offset: Int
  ): [FundingPayment!]!

  marketSnapshots(
    marketId: Int!
    limit: Int
    offset: Int
  ): [MarketSnapshotRecord!]!

  protocolSnapshots(
    limit: Int
    offset: Int
  ): [ProtocolSnapshotRecord!]!

  enrichedGlobalStats: EnrichedGlobalStats!
  enrichedMarketStats(marketId: Int!): EnrichedMarketStats!

  orderlyTickers: [OrderlyTicker!]!
  orderlyFundingRates: [OrderlyFundingRate!]!
  orderlyPriceChanges: [OrderlyPriceChange!]!
  orderlyOpenInterests: [OrderlyOpenInterest!]!

  spotMarket(marketId: String!): SpotMarket
  spotMarkets(status: String, limit: Int, offset: Int): [SpotMarket!]!

  spotOrder(orderId: String!): SpotOrder
  spotOrders(
    traderAddress: String
    marketId: String
    status: String
    limit: Int
    offset: Int
  ): [SpotOrder!]!

  spotOrderFills(
    orderId: String
    traderAddress: String
    marketId: String
    limit: Int
    offset: Int
  ): [SpotOrderFill!]!

  spotBatchClearings(
    marketId: String
    limit: Int
    offset: Int
  ): [SpotBatchClearing!]!

  spotEpochSettlements(limit: Int, offset: Int): [SpotEpochSettlement!]!

  spotFastSettlements(
    userAddress: String
    tokenAddress: String
    limit: Int
    offset: Int
  ): [SpotFastSettlement!]!

  spotCollateralMovements(
    userAddress: String
    tokenAddress: String
    eventType: String
    limit: Int
    offset: Int
  ): [SpotCollateralMovement!]!

  spotMarketModeChanges(
    marketId: String
    limit: Int
    offset: Int
  ): [SpotMarketModeChange!]!

  spotPofqUpdates(
    entityAddress: String
    isVault: Boolean
    limit: Int
    offset: Int
  ): [SpotPofqUpdate!]!

  spotPLVs(isRegistered: Boolean, limit: Int, offset: Int): [SpotPLV!]!

  spotKeeperCycles(limit: Int, offset: Int): [SpotKeeperCycle!]!

  spotBatchClearFailures(
    marketId: String
    limit: Int
    offset: Int
  ): [SpotBatchClearFailure!]!
}

type SpotMarket {
  marketId: String!
  baseToken: String!
  quoteToken: String!
  mode: Int!
  modeName: String
  status: String!
  createdAt: DateTime
  createdBlock: Int
  createdTxHash: String
  lastModeChangedAt: DateTime
  lastModeChangeBlock: Int
  lastModeChangeTx: String
}

type SpotOrder {
  orderId: BigDecimal!
  traderAddress: String!
  marketId: String!
  side: Int!
  sideName: String
  orderType: Int!
  orderTypeName: String
  offsetBps: Int!
  size: BigDecimal!
  filledSize: BigDecimal!
  status: String!
  placedAt: DateTime!
  placedBlock: Int!
  placedTxHash: String!
  resolvedAt: DateTime
  resolvedBlock: Int
  resolvedTxHash: String
  market: SpotMarket
  fills: [SpotOrderFill!]!
}

type SpotOrderFill {
  id: Int!
  orderId: BigDecimal!
  traderAddress: String!
  marketId: String!
  fillSize: BigDecimal!
  fillPrice: BigDecimal!
  fillScore: BigDecimal!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotBatchClearing {
  id: Int!
  marketId: String!
  clearingOffsetBps: Int!
  clearingPrice: BigDecimal!
  matchedVolume: BigDecimal!
  numBuysFilled: Int!
  numSellsFilled: Int!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotEpochSettlement {
  epochNumber: BigDecimal!
  usersProcessed: Int!
  onchainBlock: Int!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotFastSettlement {
  id: Int!
  userAddress: String!
  tokenAddress: String!
  amount: BigDecimal!
  fee: BigDecimal!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotCollateralMovement {
  id: Int!
  eventType: String!
  userAddress: String!
  tokenAddress: String!
  amount: BigDecimal!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotMarketModeChange {
  id: Int!
  marketId: String!
  oldMode: Int!
  newMode: Int!
  batchModeUntilBlock: BigDecimal!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotPofqUpdate {
  id: Int!
  entityAddress: String!
  newScore: BigDecimal!
  newWeight: BigDecimal!
  isVault: Boolean!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type SpotPLV {
  vaultAddress: String!
  isRegistered: Boolean!
  registeredAt: DateTime
  registeredBlock: Int
  registeredTxHash: String
  deregisteredAt: DateTime
  deregisteredBlock: Int
  deregisteredTxHash: String
}

type SpotKeeperCycle {
  id: Int!
  onchainTimestamp: Int!
  marketsEvaluated: Int!
  batchesCleared: Int!
  epochSettled: Boolean!
  blockNumber: Int!
  txHash: String!
  blockTimestamp: DateTime!
}

type SpotBatchClearFailure {
  id: Int!
  marketId: String!
  reason: String!
  blockNumber: Int!
  txHash: String!
  logIndex: Int!
  blockTimestamp: DateTime!
}

type LeaderboardEntry {
  rank: Int
  userAddress: String
  totalRealizedPnl: BigDecimal
  totalTrades: Int
  winRate: Float
  avgHoldTime: Float
  openPositions: Int
  totalVolume: BigDecimal
}

type Subscription {
  indexerStatusUpdated: IndexerStatus
  latestPricesUpdated: [LatestPrice!]!
  priceUpdated(marketId: Int): PriceUpdate
  positionChanged(userAddress: String!): [Position!]!
  orderChanged(userAddress: String!): Order
  tradeCreated(marketId: Int!): Trade
  liquidationCreated(marketId: Int!): Liquidation
  tradeExecuted(marketId: Int): TradeExecuted
}
` + PerfdbSDL

const PerfdbSDL = `
extend type Query {
  user(address: String!): User
  userBalances(address: String!): [UserBalance!]!
  userDailyPnl(address: String!, limit: Int): [DailyPnl!]!
  userAlerts(address: String!): [UserAlert!]!
  userReferrals(address: String!): UserReferrals
  referralTiers: [ReferralTier!]!
  competitions: [Competition!]!
  competition(id: String!): Competition
}

type User {
  address: String
  vaultAddress: String
  referralCode: String
  tier: String
  feeDiscountBps: Int
}

type UserBalance {
  token: String
  balance: BigDecimal
  totalDeposited: BigDecimal
  totalWithdrawn: BigDecimal
  depositCount: Int
  withdrawCount: Int
}

type DailyPnl {
  date: String
  pnl: BigDecimal
  volume: BigDecimal
  trades: Int
}

type UserAlert {
  id: Int
  alertType: String
  marketId: Int
  threshold: String
  enabled: Boolean
  triggeredAt: String
}

type UserReferrals {
  referralCode: String
  tier: String
  referralCount: Int
  totalEarned: BigDecimal
}

type ReferralTier {
  tier: String
  minReferrals: Int
  minVolume: BigDecimal
  rewardBps: Int
  feeDiscountBps: Int
}

type Competition {
  id: String
  name: String
  description: String
  startAt: String
  endAt: String
  metric: String
  prizePool: BigDecimal
  active: Boolean
  rankings: [CompetitionEntry!]!
}

type CompetitionEntry {
  rank: Int
  address: String
  score: BigDecimal
}

type TradeExecuted {
  tradeId: String
  marketId: Int
  isLong: Boolean
  sizeUsd: BigDecimal
  price: BigDecimal
  blockTimestamp: Int
}
`
