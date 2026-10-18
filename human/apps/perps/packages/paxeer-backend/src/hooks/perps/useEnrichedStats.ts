"use client";

// ─────────────────────────────────────────────────────────────────────
//  Enriched stats + paxeer-network-mirror hooks.
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
import { INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";
import {
  ENRICHED_GLOBAL_STATS_QUERY,
  ENRICHED_MARKET_STATS_QUERY,
  PAXEER_NETWORK_TICKERS_QUERY,
  PAXEER_NETWORK_FUNDING_RATES_QUERY,
  PAXEER_NETWORK_PRICE_CHANGES_QUERY,
  PAXEER_NETWORK_OPEN_INTERESTS_QUERY,
} from "./useSubgraph";
import type {
  EnrichedGlobalStats,
  EnrichedMarketStats,
  PaxeerNetworkTicker,
  PaxeerNetworkFundingRate,
  PaxeerNetworkPriceChange,
  PaxeerNetworkOpenInterest,
} from "@paxeer-network/paxeer-backend/types/perps";

const POLL_INTERVAL = 15_000;

// ── Global enriched stats (chain + paxeer-network aggregates) ───────────────
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

// ── paxeer-network ticker table ─────────────────────────────────────────────
export function usePaxeerNetworkTickers() {
  return useSharedPolledQuery<
    { paxeerNetworkTickers: PaxeerNetworkTicker[] },
    PaxeerNetworkTicker[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: PAXEER_NETWORK_TICKERS_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.paxeerNetworkTickers,
  });
}

// ── paxeer-network funding rates ────────────────────────────────────────────
export function usePaxeerNetworkFundingRates() {
  return useSharedPolledQuery<
    { paxeerNetworkFundingRates: PaxeerNetworkFundingRate[] },
    PaxeerNetworkFundingRate[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: PAXEER_NETWORK_FUNDING_RATES_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.paxeerNetworkFundingRates,
  });
}

// ── paxeer-network price changes ────────────────────────────────────────────
export function usePaxeerNetworkPriceChanges() {
  return useSharedPolledQuery<
    { paxeerNetworkPriceChanges: PaxeerNetworkPriceChange[] },
    PaxeerNetworkPriceChange[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: PAXEER_NETWORK_PRICE_CHANGES_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.paxeerNetworkPriceChanges,
  });
}

// ── paxeer-network open interests ───────────────────────────────────────────
export function usePaxeerNetworkOpenInterests() {
  return useSharedPolledQuery<
    { paxeerNetworkOpenInterests: PaxeerNetworkOpenInterest[] },
    PaxeerNetworkOpenInterest[]
  >({
    endpoint: INDEXER_GRAPHQL,
    query: PAXEER_NETWORK_OPEN_INTERESTS_QUERY,
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.paxeerNetworkOpenInterests,
  });
}
