import { fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";
import { SUBGRAPH_URL } from "@paxeer-network/paxeer-backend/core/perps/config";

export async function subgraphQuery<T = unknown>(
  document: string,
  variables?: Record<string, unknown>
): Promise<T> {
  const res = await fetch(SUBGRAPH_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query: document, variables }),
  });
  const json = await res.json();
  if (json.errors?.length) {
    throw new Error(json.errors[0]?.message || "Subgraph query failed");
  }
  return json.data as T;
}

// ── Common GraphQL Queries ──────────────────────────────────────────

export const POSITIONS_QUERY = /* GraphQL */ `
  query GetPositions($userAddress: String!) {
    positions(userAddress: $userAddress, status: "open") {
      positionId userAddress marketId isLong sizeUsd leverage entryPrice
      collateralToken collateralAmount collateralUsd status openedAt openTxHash
      market { symbol }
    }
  }
`;

export const CLOSED_POSITIONS_QUERY = /* GraphQL */ `
  query GetClosedPositions($userAddress: String!, $limit: Int, $offset: Int) {
    positions(userAddress: $userAddress, status: "closed", limit: $limit, offset: $offset) {
      positionId userAddress marketId isLong sizeUsd leverage entryPrice
      collateralToken collateralAmount collateralUsd status realizedPnl exitPrice
      openedAt closedAt closeTxHash market { symbol }
    }
  }
`;

export const TRADES_QUERY = /* GraphQL */ `
  query GetTrades($userAddress: String!, $limit: Int, $offset: Int) {
    trades(userAddress: $userAddress, limit: $limit, offset: $offset) {
      id positionId userAddress marketId tradeType isLong sizeUsd price
      realizedPnl feeUsd blockNumber txHash blockTimestamp
    }
  }
`;

export const ORDERS_QUERY = /* GraphQL */ `
  query GetOrders($userAddress: String!, $status: String) {
    orders(userAddress: $userAddress, status: $status) {
      orderId userAddress marketId orderType orderTypeName isLong triggerPrice sizeUsd status
      positionId executionPrice failureReason placedAt resolvedAt placedTxHash resolvedTxHash
    }
  }
`;

export const MARKET_INFO_QUERY = /* GraphQL */ `
  query GetMarketInfo($marketId: Int!) {
    market(marketId: $marketId) {
      marketId symbol maxLeverage
      fundingRate { ratePerSecond rate24h }
      poolState { baseReserve quoteReserve oraclePrice }
    }
    marketStats(marketId: $marketId) {
      openPositions totalPositions totalTrades totalLiquidations totalVolume latestFundingRate
    }
  }
`;

export const MARKETS_QUERY = /* GraphQL */ `
  query GetMarkets {
    markets {
      marketId symbol name maxLeverage enabled createdAt
      latestPrice { price onchainTimestamp }
      fundingRate { ratePerSecond rate24h }
    }
  }
`;

export const GLOBAL_STATS_QUERY = /* GraphQL */ `
  query GetGlobalStats {
    globalStats {
      totalMarkets totalPositions openPositions totalTrades totalLiquidations totalVolume totalUsers
    }
  }
`;

export const USER_STATS_QUERY = /* GraphQL */ `
  query GetUserStats($userAddress: String!) {
    userStats(userAddress: $userAddress) {
      totalPositions openPositions closedPositions liquidatedPositions
      totalTrades totalRealizedPnl totalOrders activeOrders
    }
  }
`;

export const USER_VAULT_QUERY = /* GraphQL */ `
  query GetUserVault($userAddress: String!) {
    userVault(userAddress: $userAddress) {
      userAddress vaultAddress createdAt blockNumber txHash
    }
  }
`;

export const FILLED_ORDERS_QUERY = /* GraphQL */ `
  query GetFilledOrders($userAddress: String!, $limit: Int, $offset: Int) {
    orders(userAddress: $userAddress, status: "filled", limit: $limit, offset: $offset) {
      orderId userAddress marketId orderType isLong triggerPrice sizeUsd status
      positionId executionPrice placedAt resolvedAt placedTxHash resolvedTxHash
    }
  }
`;

export const ALL_ORDERS_QUERY = /* GraphQL */ `
  query GetAllOrders($userAddress: String!, $limit: Int, $offset: Int) {
    orders(userAddress: $userAddress, limit: $limit, offset: $offset) {
      orderId userAddress marketId orderType isLong triggerPrice sizeUsd status
      positionId executionPrice placedAt resolvedAt placedTxHash resolvedTxHash
    }
  }
`;

export const LIQUIDATIONS_QUERY = /* GraphQL */ `
  query GetLiquidations($userAddress: String!, $limit: Int, $offset: Int) {
    liquidations(userAddress: $userAddress, limit: $limit, offset: $offset) {
      id positionId marketId price penalty keeper txHash blockTimestamp
    }
  }
`;

export const LATEST_PRICES_QUERY = /* GraphQL */ `
  query GetLatestPrices {
    latestPrices {
      marketId price onchainTimestamp blockNumber updatedAt
    }
  }
`;

export const VAULT_EVENTS_QUERY = /* GraphQL */ `
  query GetVaultEvents($userAddress: String!, $limit: Int, $offset: Int) {
    vaultEvents(userAddress: $userAddress, limit: $limit, offset: $offset) {
      id eventType tokenAddress amount blockNumber txHash blockTimestamp
    }
  }
`;

// ── V2 Queries ────────────────────────────────────────────────────────

export const ACCOUNT_LEDGER_QUERY = /* GraphQL */ `
  query GetAccountLedger($userAddress: String!, $limit: Int, $offset: Int) {
    accountLedger(userAddress: $userAddress, limit: $limit, offset: $offset) {
      entryId entryType tokenAddress amount positionId isDebit blockTimestamp txHash
    }
  }
`;

export const POSITION_LEDGER_QUERY = /* GraphQL */ `
  query GetPositionLedger($positionId: String!, $limit: Int) {
    accountLedger(positionId: $positionId, limit: $limit) {
      entryId entryType tokenAddress amount isDebit blockTimestamp
    }
  }
`;

export const DELEGATES_QUERY = /* GraphQL */ `
  query GetDelegates($userAddress: String!) {
    delegates(userAddress: $userAddress) {
      delegateAddress canTrade canWithdraw canModifyMargin expiry isActive blockTimestamp
    }
  }
`;

export const TRADING_ACCOUNT_EVENTS_QUERY = /* GraphQL */ `
  query GetTradingAccountEvents($userAddress: String!, $limit: Int, $offset: Int) {
    tradingAccountEvents(userAddress: $userAddress, limit: $limit, offset: $offset) {
      eventType positionId tokenAddress amount blockTimestamp txHash
    }
  }
`;

export const KEEPER_CYCLES_QUERY = /* GraphQL */ `
  query GetKeeperCycles($limit: Int) {
    keeperCycles(limit: $limit) {
      onchainTimestamp marketsUpdated ordersExecuted liquidationsExecuted
      ordersFailed liquidationsFailed blockTimestamp txHash
    }
  }
`;

export const TPSL_ORDERS_QUERY = /* GraphQL */ `
  query GetTPSLOrders($userAddress: String!) {
    takeProfits: orders(userAddress: $userAddress, status: "active", orderType: 2) {
      orderId positionId triggerPrice marketId
    }
    stopLosses: orders(userAddress: $userAddress, status: "active", orderType: 3) {
      orderId positionId triggerPrice marketId
    }
  }
`;

export const DASHBOARD_V2_QUERY = /* GraphQL */ `
  query DashboardV2($userAddress: String!) {
    userVault(userAddress: $userAddress) {
      vaultAddress marginMode
    }
    positions(userAddress: $userAddress, status: "open") {
      positionId marketId isLong sizeUsd entryPrice collateralUsd leverage
      market { symbol latestPrice { price } }
    }
    orders(userAddress: $userAddress, status: "active") {
      orderId marketId orderType orderTypeName isLong triggerPrice sizeUsd
      positionId failureReason status
    }
    delegates(userAddress: $userAddress) {
      delegateAddress canTrade canWithdraw canModifyMargin expiry
    }
    userStats(userAddress: $userAddress) {
      totalPositions openPositions totalRealizedPnl totalTrades activeOrders
    }
  }
`;

export const ALL_ORDERS_V2_QUERY = /* GraphQL */ `
  query GetAllOrdersV2($userAddress: String!, $status: String, $orderType: Int, $limit: Int, $offset: Int) {
    orders(userAddress: $userAddress, status: $status, orderType: $orderType, limit: $limit, offset: $offset) {
      orderId userAddress marketId orderType orderTypeName isLong triggerPrice sizeUsd status
      positionId executionPrice failureReason placedAt resolvedAt placedTxHash resolvedTxHash
    }
  }
`;

export const LEADERBOARD_QUERY = /* GraphQL */ `
  query GetLeaderboard($limit: Int, $offset: Int) {
    leaderboard(limit: $limit, offset: $offset) {
      rank userAddress totalRealizedPnl totalTrades winRate avgHoldTime
      openPositions totalVolume
    }
  }
`;

// ── V5 Queries ─────────────────────────────────────────────────────────

export const FEES_QUERY = /* GraphQL */ `
  query GetFees($userAddress: String, $marketId: Int, $positionId: String, $feeType: Int, $limit: Int, $offset: Int) {
    fees(userAddress: $userAddress, marketId: $marketId, positionId: $positionId, feeType: $feeType, limit: $limit, offset: $offset) {
      id positionId userAddress marketId feeType feeTypeName feeUsd feeTokens
      tokenAddress blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const TRADE_SETTLEMENTS_QUERY = /* GraphQL */ `
  query GetTradeSettlements($userAddress: String, $marketId: Int, $positionId: String, $tradeType: Int, $limit: Int, $offset: Int) {
    tradeSettlements(userAddress: $userAddress, marketId: $marketId, positionId: $positionId, tradeType: $tradeType, limit: $limit, offset: $offset) {
      id positionId userAddress marketId tradeType tradeTypeName sizeUsd executionPrice
      grossPnl totalFeesUsd borrowingFeeUsd fundingPaidUsd netPayoutTokens
      blockNumber txHash blockTimestamp
    }
  }
`;

export const OI_SNAPSHOTS_QUERY = /* GraphQL */ `
  query GetOiSnapshots($marketId: Int!, $limit: Int, $offset: Int) {
    oiSnapshots(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId longOi shortOi deltaUsd isIncrease blockNumber txHash blockTimestamp
    }
  }
`;

export const MARK_PRICE_HISTORY_QUERY = /* GraphQL */ `
  query GetMarkPriceHistory($marketId: Int!, $limit: Int, $offset: Int) {
    markPriceHistory(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId markPrice indexPrice baseReserve quoteReserve blockNumber txHash blockTimestamp
    }
  }
`;

export const VAULT_BALANCE_HISTORY_QUERY = /* GraphQL */ `
  query GetVaultBalanceHistory($tokenAddress: String, $vaultType: Int, $limit: Int, $offset: Int) {
    vaultBalanceHistory(tokenAddress: $tokenAddress, vaultType: $vaultType, limit: $limit, offset: $offset) {
      id tokenAddress vaultType vaultTypeName newBalance delta isIncrease
      blockNumber txHash blockTimestamp
    }
  }
`;

export const FUNDING_PAYMENTS_QUERY = /* GraphQL */ `
  query GetFundingPayments($userAddress: String, $marketId: Int, $positionId: String, $limit: Int, $offset: Int) {
    fundingPayments(userAddress: $userAddress, marketId: $marketId, positionId: $positionId, limit: $limit, offset: $offset) {
      id positionId userAddress marketId fundingPaymentUsd newCollateralUsd
      newCollateralAmount blockNumber txHash blockTimestamp
    }
  }
`;

export const MARKET_SNAPSHOTS_QUERY = /* GraphQL */ `
  query GetMarketSnapshots($marketId: Int!, $limit: Int, $offset: Int) {
    marketSnapshots(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId longOi shortOi markPrice indexPrice fundingRatePerSecond
      fundingRate24h volume24hUsd onchainTimestamp blockNumber blockTimestamp
    }
  }
`;

export const PROTOCOL_SNAPSHOTS_QUERY = /* GraphQL */ `
  query GetProtocolSnapshots($limit: Int, $offset: Int) {
    protocolSnapshots(limit: $limit, offset: $offset) {
      id totalPositions totalOpenPositions totalMarkets tvlUsd insuranceTotalUsd
      onchainTimestamp blockNumber blockTimestamp
    }
  }
`;

// ── Enriched Stats Queries ─────────────────────────────────────────────

export const ENRICHED_GLOBAL_STATS_QUERY = /* GraphQL */ `
  query GetEnrichedGlobalStats {
    enrichedGlobalStats {
      onchain {
        totalMarkets totalPositions openPositions totalTrades totalLiquidations totalVolume totalUsers indexerBlock
      }
      paxeer-network {
        perp_volume_ytd perp_volume_ltd perp_volume_today
        perp_volume_last_1_day perp_volume_last_7_days perp_volume_last_30_days
      }
    }
  }
`;

export const ENRICHED_MARKET_STATS_QUERY = /* GraphQL */ `
  query GetEnrichedMarketStats($marketId: Int!) {
    enrichedMarketStats(marketId: $marketId) {
      onchain {
        marketId symbol totalPositions openPositions totalTrades totalLiquidations
        totalVolume latestPrice latestFundingRate
      }
      ticker {
        symbol paxeerSymbol mark_price index_price est_funding_rate last_funding_rate
        next_funding_time open_interest h24_open h24_close h24_high h24_low h24_amount h24_volume
      }
      fundingRate {
        symbol paxeerSymbol est_funding_rate est_funding_rate_timestamp
        last_funding_rate last_funding_rate_timestamp next_funding_time sum_unitary_funding
      }
      openInterest {
        symbol paxeerSymbol long_oi short_oi
      }
      priceChange {
        symbol paxeerSymbol last_price change_5m change_30m change_1h
        change_4h change_24h change_3d change_7d change_30d
      }
    }
  }
`;

// ── paxeer-network Market Data Queries ────────────────────────────────────────

export const PAXEER_NETWORK_TICKERS_QUERY = /* GraphQL */ `
  query GetPaxeerNetworkTickers {
    paxeerNetworkTickers {
      symbol paxeerSymbol mark_price index_price sum_unitary_funding est_funding_rate
      last_funding_rate next_funding_time open_interest
      h24_open h24_close h24_high h24_low h24_amount h24_volume
    }
  }
`;

export const PAXEER_NETWORK_FUNDING_RATES_QUERY = /* GraphQL */ `
  query GetPaxeerNetworkFundingRates {
    paxeerNetworkFundingRates {
      symbol paxeerSymbol est_funding_rate est_funding_rate_timestamp
      last_funding_rate last_funding_rate_timestamp next_funding_time sum_unitary_funding
    }
  }
`;

export const PAXEER_NETWORK_PRICE_CHANGES_QUERY = /* GraphQL */ `
  query GetPaxeerNetworkPriceChanges {
    paxeerNetworkPriceChanges {
      symbol paxeerSymbol last_price change_5m change_30m change_1h
      change_4h change_24h change_3d change_7d change_30d
    }
  }
`;

export const PAXEER_NETWORK_OPEN_INTERESTS_QUERY = /* GraphQL */ `
  query GetPaxeerNetworkOpenInterests {
    paxeerNetworkOpenInterests {
      symbol paxeerSymbol long_oi short_oi
    }
  }
`;

// ── V4 Spot Queries ────────────────────────────────────────────────────

export const SPOT_MARKETS_QUERY = /* GraphQL */ `
  query GetSpotMarkets($status: String, $limit: Int, $offset: Int) {
    spotMarkets(status: $status, limit: $limit, offset: $offset) {
      marketId baseToken quoteToken mode modeName status
      createdAt createdBlock createdTxHash
      lastModeChangedAt lastModeChangeBlock lastModeChangeTx
    }
  }
`;

export const SPOT_MARKET_QUERY = /* GraphQL */ `
  query GetSpotMarket($marketId: String!) {
    spotMarket(marketId: $marketId) {
      marketId baseToken quoteToken mode modeName status
      createdAt createdBlock createdTxHash
      lastModeChangedAt lastModeChangeBlock lastModeChangeTx
    }
  }
`;

export const SPOT_ORDERS_QUERY = /* GraphQL */ `
  query GetSpotOrders($traderAddress: String, $marketId: String, $status: String, $limit: Int, $offset: Int) {
    spotOrders(traderAddress: $traderAddress, marketId: $marketId, status: $status, limit: $limit, offset: $offset) {
      orderId traderAddress marketId side sideName orderType orderTypeName
      offsetBps size filledSize status
      placedAt placedBlock placedTxHash
      resolvedAt resolvedBlock resolvedTxHash
    }
  }
`;

export const SPOT_ORDER_QUERY = /* GraphQL */ `
  query GetSpotOrder($orderId: String!) {
    spotOrder(orderId: $orderId) {
      orderId traderAddress marketId side sideName orderType orderTypeName
      offsetBps size filledSize status
      placedAt placedBlock placedTxHash
      resolvedAt resolvedBlock resolvedTxHash
      fills {
        id orderId traderAddress marketId fillSize fillPrice fillScore
        blockNumber txHash logIndex blockTimestamp
      }
    }
  }
`;

export const SPOT_ORDER_FILLS_QUERY = /* GraphQL */ `
  query GetSpotOrderFills($orderId: String, $traderAddress: String, $marketId: String, $limit: Int, $offset: Int) {
    spotOrderFills(orderId: $orderId, traderAddress: $traderAddress, marketId: $marketId, limit: $limit, offset: $offset) {
      id orderId traderAddress marketId fillSize fillPrice fillScore
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_BATCH_CLEARINGS_QUERY = /* GraphQL */ `
  query GetSpotBatchClearings($marketId: String, $limit: Int, $offset: Int) {
    spotBatchClearings(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId clearingOffsetBps clearingPrice matchedVolume
      numBuysFilled numSellsFilled blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_EPOCH_SETTLEMENTS_QUERY = /* GraphQL */ `
  query GetSpotEpochSettlements($limit: Int, $offset: Int) {
    spotEpochSettlements(limit: $limit, offset: $offset) {
      epochNumber usersProcessed onchainBlock
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_FAST_SETTLEMENTS_QUERY = /* GraphQL */ `
  query GetSpotFastSettlements($userAddress: String, $tokenAddress: String, $limit: Int, $offset: Int) {
    spotFastSettlements(userAddress: $userAddress, tokenAddress: $tokenAddress, limit: $limit, offset: $offset) {
      id userAddress tokenAddress amount fee
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_COLLATERAL_MOVEMENTS_QUERY = /* GraphQL */ `
  query GetSpotCollateralMovements($userAddress: String, $tokenAddress: String, $eventType: String, $limit: Int, $offset: Int) {
    spotCollateralMovements(userAddress: $userAddress, tokenAddress: $tokenAddress, eventType: $eventType, limit: $limit, offset: $offset) {
      id eventType userAddress tokenAddress amount
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_MARKET_MODE_CHANGES_QUERY = /* GraphQL */ `
  query GetSpotMarketModeChanges($marketId: String, $limit: Int, $offset: Int) {
    spotMarketModeChanges(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId oldMode newMode batchModeUntilBlock
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_POFQ_UPDATES_QUERY = /* GraphQL */ `
  query GetSpotPofqUpdates($entityAddress: String, $isVault: Boolean, $limit: Int, $offset: Int) {
    spotPofqUpdates(entityAddress: $entityAddress, isVault: $isVault, limit: $limit, offset: $offset) {
      id entityAddress newScore newWeight isVault
      blockNumber txHash logIndex blockTimestamp
    }
  }
`;

export const SPOT_PLVS_QUERY = /* GraphQL */ `
  query GetSpotPLVs($isRegistered: Boolean, $limit: Int, $offset: Int) {
    spotPLVs(isRegistered: $isRegistered, limit: $limit, offset: $offset) {
      vaultAddress isRegistered
      registeredAt registeredBlock registeredTxHash
      deregisteredAt deregisteredBlock deregisteredTxHash
    }
  }
`;

export const SPOT_KEEPER_CYCLES_QUERY = /* GraphQL */ `
  query GetSpotKeeperCycles($limit: Int, $offset: Int) {
    spotKeeperCycles(limit: $limit, offset: $offset) {
      id onchainTimestamp marketsEvaluated batchesCleared epochSettled
      blockNumber txHash blockTimestamp
    }
  }
`;

export const SPOT_BATCH_CLEAR_FAILURES_QUERY = /* GraphQL */ `
  query GetSpotBatchClearFailures($marketId: String, $limit: Int, $offset: Int) {
    spotBatchClearFailures(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId reason blockNumber txHash logIndex blockTimestamp
    }
  }
`;
