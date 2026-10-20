"use client";

// ─────────────────────────────────────────────────────────────────────
//  Enriched stats + Orderly-mirror hooks.
//
//  Previously each of the six hooks in this file owned its own
//  `setInterval(refetch, 15_000)`. With 30+ subscribers open on the
//  portfolio / market pages that meant ~120 requests/min against the
//  indexer just from this file. Now all subscribers share one timer
//  and one in-flight fetch per (query, variables) via the
//  `useSharedPolledQuery` singleton. Polling pauses when the tab is
//  hidden.
// ─────────────────────────────────────────────────────────────────────

import { useSharedPolledQuery } from "./useGraphQLQuery";
import { INDEXER_GRAPHQL } from "@/core/perps/config";
import {
  ENRICHED_GLOBAL_STATS_QUERY,
  ENRICHED_MARKET_STATS_QUERY,
  ORDERLY_TICKERS_QUERY,
  ORDERLY_FUNDING_RATES_QUERY,
  ORDERLY_PRICE_CHANGES_QUERY,
  ORDERLY_OPEN_INTERESTS_QUERY,
} from "./useSubgraph";
import type {
  EnrichedGlobalStats,
  EnrichedMarketStats,
  OrderlyTicker,
  OrderlyFundingRate,
  OrderlyPriceChange,
  OrderlyOpenInterest,
} from "@/types/perps";

const POLL_INTERVAL = 15_000;

// ── Global enriched stats (chain + Orderly aggregates) ───────────────
export function useEnrichedGlobalStats() {
  return useSharedPolledQuery<
    { enrichedGlobalStats: EnrichedGlobalStats },
    EnrichedGlobalStats
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ENRICHED_GLOBAL_STATS_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.enrichedGlobalStats,
  });
}

// ── Per-market enriched stats ────────────────────────────────────────
export function useEnrichedMarketStats(marketId: number | undefined) {
  return useSharedPolledQuery<
    { enrichedMarketStats: EnrichedMarketStats },
    EnrichedMarketStats
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ENRICHED_MARKET_STATS_QUERY,
    variables: marketId !== undefined ? { marketId } : undefined,
    intervalMs: POLL_INTERVAL,
    enabled: marketId !== undefined,
    pick: (res) => res.enrichedMarketStats,
  });
}

// ── Orderly ticker table ─────────────────────────────────────────────
export function useOrderlyTickers() {
  return useSharedPolledQuery<
    { orderlyTickers: OrderlyTicker[] },
    OrderlyTicker[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ORDERLY_TICKERS_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.orderlyTickers,
  });
}

// ── Orderly funding rates ────────────────────────────────────────────
export function useOrderlyFundingRates() {
  return useSharedPolledQuery<
    { orderlyFundingRates: OrderlyFundingRate[] },
    OrderlyFundingRate[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ORDERLY_FUNDING_RATES_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.orderlyFundingRates,
  });
}

// ── Orderly price changes ────────────────────────────────────────────
export function useOrderlyPriceChanges() {
  return useSharedPolledQuery<
    { orderlyPriceChanges: OrderlyPriceChange[] },
    OrderlyPriceChange[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ORDERLY_PRICE_CHANGES_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.orderlyPriceChanges,
  });
}

// ── Orderly open interests ───────────────────────────────────────────
export function useOrderlyOpenInterests() {
  return useSharedPolledQuery<
    { orderlyOpenInterests: OrderlyOpenInterest[] },
    OrderlyOpenInterest[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: ORDERLY_OPEN_INTERESTS_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.orderlyOpenInterests,
  });
}
