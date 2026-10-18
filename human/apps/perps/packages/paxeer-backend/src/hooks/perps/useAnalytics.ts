"use client";

// ─────────────────────────────────────────────────────────────────────
//  Analytics / history hooks — all go through the shared-poller
//  singleton via `useSharedPolledQuery`. Every hook exposed below
//  used to run its own `setInterval(refetch, 30_000)`; they now share
//  a single timer per (query, variables) tuple across the whole app,
//  and pause entirely when the tab is hidden.
// ─────────────────────────────────────────────────────────────────────

import { useSharedPolledQuery } from "./useGraphQLQuery";
import { INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";
import {
  OI_SNAPSHOTS_QUERY,
  MARK_PRICE_HISTORY_QUERY,
  VAULT_BALANCE_HISTORY_QUERY,
  MARKET_SNAPSHOTS_QUERY,
  PROTOCOL_SNAPSHOTS_QUERY,
  TRADE_SETTLEMENTS_QUERY,
} from "./useSubgraph";
import type {
  OiSnapshot,
  MarkPriceRecord,
  VaultBalanceRecord,
  MarketSnapshotRecord,
  ProtocolSnapshotRecord,
  TradeSettlement,
} from "@paxeer-network/paxeer-backend/types/perps";

const POLL_INTERVAL = 30_000;

// ── Open-interest snapshots ──────────────────────────────────────────
export function useOiSnapshots(marketId: number | undefined, limit = 100) {
  return useSharedPolledQuery<{ oiSnapshots: OiSnapshot[] }, OiSnapshot[]>({
    endpoint: INDEXER_GRAPHQL,
    query: OI_SNAPSHOTS_QUERY,
    variables: marketId !== undefined ? { marketId, limit } : undefined,
    intervalMs: POLL_INTERVAL,
    enabled: marketId !== undefined,
    pick: (res) => res.oiSnapshots,
  });
}

// ── Mark-price history ───────────────────────────────────────────────
export function useMarkPriceHistory(marketId: number | undefined, limit = 100) {
  return useSharedPolledQuery<{ markPriceHistory: MarkPriceRecord[] }, MarkPriceRecord[]>({
    endpoint: INDEXER_GRAPHQL,
    query: MARK_PRICE_HISTORY_QUERY,
    variables: marketId !== undefined ? { marketId, limit } : undefined,
    intervalMs: POLL_INTERVAL,
    enabled: marketId !== undefined,
    pick: (res) => res.markPriceHistory,
  });
}

// ── Vault balance history ────────────────────────────────────────────
export function useVaultBalanceHistory(
  tokenAddress?: string,
  vaultType?: number,
  limit = 100,
) {
  return useSharedPolledQuery<{ vaultBalanceHistory: VaultBalanceRecord[] }, VaultBalanceRecord[]>({
    endpoint: INDEXER_GRAPHQL,
    query: VAULT_BALANCE_HISTORY_QUERY,
    variables: { tokenAddress, vaultType, limit },
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.vaultBalanceHistory,
  });
}

// ── Market snapshots ─────────────────────────────────────────────────
export function useMarketSnapshots(marketId: number | undefined, limit = 100) {
  return useSharedPolledQuery<{ marketSnapshots: MarketSnapshotRecord[] }, MarketSnapshotRecord[]>({
    endpoint: INDEXER_GRAPHQL,
    query: MARKET_SNAPSHOTS_QUERY,
    variables: marketId !== undefined ? { marketId, limit } : undefined,
    intervalMs: POLL_INTERVAL,
    enabled: marketId !== undefined,
    pick: (res) => res.marketSnapshots,
  });
}

// ── Protocol snapshots ───────────────────────────────────────────────
export function useProtocolSnapshots(limit = 100) {
  return useSharedPolledQuery<{ protocolSnapshots: ProtocolSnapshotRecord[] }, ProtocolSnapshotRecord[]>({
    endpoint: INDEXER_GRAPHQL,
    query: PROTOCOL_SNAPSHOTS_QUERY,
    variables: { limit },
    intervalMs: POLL_INTERVAL,
    pick: (res) => res.protocolSnapshots,
  });
}

// ── Trade settlements (one-shot on mount; no interval in the old code) ─
//
// The legacy hook called `refetch()` on mount and never polled. We
// match that by passing `intervalMs: 0` which still goes through the
// singleton's in-flight dedup but never schedules a tick.
export function useTradeSettlements(params?: {
  userAddress?: string;
  marketId?: number;
  positionId?: string;
  tradeType?: number;
  limit?: number;
  offset?: number;
}) {
  return useSharedPolledQuery<{ tradeSettlements: TradeSettlement[] }, TradeSettlement[]>({
    endpoint: INDEXER_GRAPHQL,
    query: TRADE_SETTLEMENTS_QUERY,
    variables: {
      userAddress: params?.userAddress,
      marketId: params?.marketId,
      positionId: params?.positionId,
      tradeType: params?.tradeType,
      limit: params?.limit ?? 50,
      offset: params?.offset,
    },
    // Mirror the legacy behavior: fetch on mount but never auto-poll.
    intervalMs: 0,
    pick: (res) => res.tradeSettlements,
  });
}
