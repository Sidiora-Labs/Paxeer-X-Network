"use client";

// Multiplexed graphql-transport-ws subscriptions for the Sidiora indexer.
//
// The indexer exposes subscriptions at /graphql-ws (graphql-transport-ws
// subprotocol). All hooks here share ONE socket per endpoint via
// `subscribeStream` from `graphqlClient.ts`.
//
// Migration guide:
//   indexerStatusUpdated  → useIndexerStatus()
//   latestPricesUpdated   → useLatestPricesSubscription()
//   priceUpdated          → useSingleMarketPriceSubscription(marketId)
//   positionChanged       → usePositionChangedSubscription(userAddress)
//   orderChanged          → useOrderChangedSubscription(userAddress)
//   tradeCreated          → useTradeCreatedSubscription(marketId)
//   liquidationCreated    → useLiquidationSubscription(marketId)

import { useState, useEffect } from "react";
import { subscribeStream } from "@paxeer-network/paxeer-backend/core/graphql/client";
import { INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";

// ── Shared types ────────────────────────────────────────────────────────

export interface IndexerStatus {
  lastIndexedBlock: number;
  chainHead: number;
  isSynced: boolean;
  blocksScanned: number;
  eventsProcessed: number;
}

export interface IndexerPriceUpdate {
  marketId: number;
  price: string;
  updatedAt: string;
  blockNumber: number;
}

export interface IndexerPosition {
  positionId: string;
  marketId: number;
  isLong: boolean;
  sizeUsd: string;
  leverage: string;
  entryPrice: string;
  status: string;
  realizedPnl: string;
}

export interface IndexerOrder {
  orderId: string;
  status: string;
  marketId: number;
  orderType: number;
  remainingSize: string;
  blockTimestamp: number;
}

export interface IndexerTrade {
  positionId: string;
  userAddress: string;
  sizeUsd: string;
  executionPrice: string;
  tradeType: string;
  blockTimestamp: number;
}

export interface IndexerLiquidation {
  positionId: string;
  userAddress: string;
  sizeUsd: string;
  executionPrice: string;
  blockTimestamp: number;
}

// ── indexerStatusUpdated ───────────────────────────────────────────────

const INDEXER_STATUS_SUBSCRIPTION = /* GraphQL */ `
  subscription {
    indexerStatusUpdated {
      lastIndexedBlock
      chainHead
      isSynced
      blocksScanned
      eventsProcessed
    }
  }
`;

export function useIndexerStatus() {
  const [status, setStatus] = useState<IndexerStatus | null>(null);

  useEffect(() => {
    return subscribeStream<{ indexerStatusUpdated: IndexerStatus }>({
      endpoint: INDEXER_GRAPHQL,
      query: INDEXER_STATUS_SUBSCRIPTION,
      onData: (data) => setStatus(data.indexerStatusUpdated),
    });
  }, []);

  return status;
}

// ── latestPricesUpdated ────────────────────────────────────────────────

const LATEST_PRICES_SUBSCRIPTION = /* GraphQL */ `
  subscription {
    latestPricesUpdated {
      marketId
      price
      updatedAt
      blockNumber
    }
  }
`;

export function useLatestPricesSubscription() {
  const [prices, setPrices] = useState<IndexerPriceUpdate[]>([]);

  useEffect(() => {
    return subscribeStream<{ latestPricesUpdated: IndexerPriceUpdate[] }>({
      endpoint: INDEXER_GRAPHQL,
      query: LATEST_PRICES_SUBSCRIPTION,
      onData: (data) => setPrices(data.latestPricesUpdated),
    });
  }, []);

  return prices;
}

// ── priceUpdated(marketId) ─────────────────────────────────────────────

const PRICE_UPDATED_SUBSCRIPTION = /* GraphQL */ `
  subscription PriceUpdated($marketId: Int!) {
    priceUpdated(marketId: $marketId) {
      marketId
      price
      updatedAt
      blockNumber
    }
  }
`;

export function useSingleMarketPriceSubscription(marketId: number | undefined) {
  const [price, setPrice] = useState<IndexerPriceUpdate | null>(null);

  useEffect(() => {
    if (marketId === undefined) return;
    return subscribeStream<{ priceUpdated: IndexerPriceUpdate }>({
      endpoint: INDEXER_GRAPHQL,
      query: PRICE_UPDATED_SUBSCRIPTION,
      variables: { marketId },
      onData: (data) => setPrice(data.priceUpdated),
    });
  }, [marketId]);

  return price;
}

// ── positionChanged(userAddress) ───────────────────────────────────────

const POSITION_CHANGED_SUBSCRIPTION = /* GraphQL */ `
  subscription PositionChanged($userAddress: String!) {
    positionChanged(userAddress: $userAddress) {
      positionId
      marketId
      isLong
      sizeUsd
      leverage
      entryPrice
      status
      realizedPnl
    }
  }
`;

export function usePositionChangedSubscription(userAddress: string | undefined) {
  const [positions, setPositions] = useState<IndexerPosition[] | null>(null);

  useEffect(() => {
    if (!userAddress) return;
    return subscribeStream<{ positionChanged: IndexerPosition[] }>({
      endpoint: INDEXER_GRAPHQL,
      query: POSITION_CHANGED_SUBSCRIPTION,
      variables: { userAddress },
      onData: (data) => setPositions(data.positionChanged),
    });
  }, [userAddress]);

  return positions;
}

// ── orderChanged(userAddress) ──────────────────────────────────────────

const ORDER_CHANGED_SUBSCRIPTION = /* GraphQL */ `
  subscription OrderChanged($userAddress: String!) {
    orderChanged(userAddress: $userAddress) {
      orderId
      status
      marketId
      orderType
      remainingSize
      blockTimestamp
    }
  }
`;

export function useOrderChangedSubscription(userAddress: string | undefined) {
  const [latestOrder, setLatestOrder] = useState<IndexerOrder | null>(null);

  useEffect(() => {
    if (!userAddress) return;
    return subscribeStream<{ orderChanged: IndexerOrder }>({
      endpoint: INDEXER_GRAPHQL,
      query: ORDER_CHANGED_SUBSCRIPTION,
      variables: { userAddress },
      onData: (data) => setLatestOrder(data.orderChanged),
    });
  }, [userAddress]);

  return latestOrder;
}

// ── tradeCreated(marketId) ─────────────────────────────────────────────

const TRADE_CREATED_SUBSCRIPTION = /* GraphQL */ `
  subscription TradeCreated($marketId: Int!) {
    tradeCreated(marketId: $marketId) {
      positionId
      userAddress
      sizeUsd
      executionPrice
      tradeType
      blockTimestamp
    }
  }
`;

export function useTradeCreatedSubscription(marketId: number | undefined) {
  const [latestTrade, setLatestTrade] = useState<IndexerTrade | null>(null);

  useEffect(() => {
    if (marketId === undefined) return;
    return subscribeStream<{ tradeCreated: IndexerTrade }>({
      endpoint: INDEXER_GRAPHQL,
      query: TRADE_CREATED_SUBSCRIPTION,
      variables: { marketId },
      onData: (data) => setLatestTrade(data.tradeCreated),
    });
  }, [marketId]);

  return latestTrade;
}

// ── liquidationCreated(marketId) ───────────────────────────────────────

const LIQUIDATION_SUBSCRIPTION = /* GraphQL */ `
  subscription Liquidations($marketId: Int!) {
    liquidationCreated(marketId: $marketId) {
      positionId
      userAddress
      sizeUsd
      executionPrice
      blockTimestamp
    }
  }
`;

export function useLiquidationSubscription(marketId: number | undefined) {
  const [latestLiquidation, setLatestLiquidation] = useState<IndexerLiquidation | null>(null);

  useEffect(() => {
    if (marketId === undefined) return;
    return subscribeStream<{ liquidationCreated: IndexerLiquidation }>({
      endpoint: INDEXER_GRAPHQL,
      query: LIQUIDATION_SUBSCRIPTION,
      variables: { marketId },
      onData: (data) => setLatestLiquidation(data.liquidationCreated),
    });
  }, [marketId]);

  return latestLiquidation;
}
