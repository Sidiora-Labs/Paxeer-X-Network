"use client";

// Multiplexed graphql-transport-ws subscriptions for PerfDB.
//
// Previously each hook instance opened its own WebSocket, so 10 components
// subscribing to priceUpdated meant 10 simultaneous connections to the same
// endpoint. Now all subscribers share ONE socket via `subscribeStream` from
// `graphqlClient.ts` — subscribe/next/complete frames are multiplexed over
// that shared connection per the graphql-transport-ws protocol.
//
// Reconnection and backoff are handled centrally inside graphqlClient.ts.

import { useState, useEffect } from "react";
import { subscribeStream } from "@paxeer-network/paxeer-backend/core/graphql/client";
import { PERFDB_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Interfaces ─────────────────────────────────────────────────────────
export interface PriceUpdate {
  marketId: number;
  price: string;
  timestamp: number;
}

export interface TradeUpdate {
  tradeId: string;
  marketId: number;
  isLong: boolean;
  sizeUsd: string;
  price: string;
  blockTimestamp: number;
}

// ── Price Subscription ─────────────────────────────────────────────────
const PRICE_SUBSCRIPTION = /* GraphQL */ `
  subscription PriceUpdated($marketId: Int) {
    priceUpdated(marketId: $marketId) {
      marketId
      price
      timestamp
    }
  }
`;

export function usePriceSubscription(marketId?: number) {
  const [latestUpdate, setLatestUpdate] = useState<PriceUpdate | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const variables = marketId !== undefined ? { marketId } : undefined;
    const unsub = subscribeStream<{ priceUpdated: PriceUpdate }>({
      endpoint: PERFDB_GRAPHQL,
      query: PRICE_SUBSCRIPTION,
      variables,
      onData: (data) => {
        setConnected(true);
        setLatestUpdate(data.priceUpdated);
      },
      onError: () => setConnected(false),
    });
    return unsub;
  }, [marketId]);

  return { latestUpdate, connected };
}

// ── Trade Subscription ─────────────────────────────────────────────────
const TRADE_SUBSCRIPTION = /* GraphQL */ `
  subscription TradeExecuted($marketId: Int) {
    tradeExecuted(marketId: $marketId) {
      tradeId
      marketId
      isLong
      sizeUsd
      price
      blockTimestamp
    }
  }
`;

export function useTradeSubscription(marketId?: number) {
  const [latestTrade, setLatestTrade] = useState<TradeUpdate | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const variables = marketId !== undefined ? { marketId } : undefined;
    const unsub = subscribeStream<{ tradeExecuted: TradeUpdate }>({
      endpoint: PERFDB_GRAPHQL,
      query: TRADE_SUBSCRIPTION,
      variables,
      onData: (data) => {
        setConnected(true);
        setLatestTrade(data.tradeExecuted);
      },
      onError: () => setConnected(false),
    });
    return unsub;
  }, [marketId]);

  return { latestTrade, connected };
}
