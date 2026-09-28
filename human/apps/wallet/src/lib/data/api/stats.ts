/** Blockscout API v2 — Stats & main-page endpoints */

import { getBlockscoutClient } from '../client';
import type { Transaction } from '../types/transaction';
import type { Block } from '../types/block';

// ── GET /v2/stats ───────────────────────────────────────────────────────────

export interface ChainStats {
  total_blocks: string;
  total_addresses: string;
  total_transactions: string;
  average_block_time: number;
  coin_price: string | null;
  coin_price_change_percentage: number | null;
  total_gas_used: string;
  static_gas_price: string | null;
  market_cap: string | null;
  tvl: string | null;
  network_utilization_percentage: number;
}

export async function getStats(): Promise<ChainStats> {
  return getBlockscoutClient().get<ChainStats>('/v2/stats');
}

// ── GET /v2/main-page/transactions ──────────────────────────────────────────

export async function getMainPageTransactions(): Promise<Transaction[]> {
  return getBlockscoutClient().get<Transaction[]>('/v2/main-page/transactions');
}

// ── GET /v2/main-page/blocks ────────────────────────────────────────────────

export async function getMainPageBlocks(): Promise<Block[]> {
  return getBlockscoutClient().get<Block[]>('/v2/main-page/blocks');
}
