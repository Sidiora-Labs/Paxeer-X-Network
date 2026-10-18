export interface RankingItem {
  poolAddress: string;
  score: number;
  rank: number;
  stats?: {
    price?: string;
    priceChange1m?: string;
    priceChange5m?: string;
    priceChange15m?: string;
    priceChange1h?: string;
    priceChange24h?: string;
    priceChangeDollar1m?: string;
    priceChangeDollar5m?: string;
    priceChangeDollar15m?: string;
    priceChangeDollar1h?: string;
    priceChangeDollar24h?: string;
    volume24h?: string;
    volume1h?: string;
    volume5m?: string;
    marketCap?: string;
    holderCount?: number;
  } | null;
}

export interface RankingsResponse {
  category: string;
  items: RankingItem[];
  total: number;
  limit: number;
  offset: number;
}

/**
 * GET /metadata/{tokenAddress}.json — PublicMetadataResponse (new primary endpoint)
 * GET /metadata/{tokenAddress} — LegacyMetadataResponse (fallback)
 * Union type covers both shapes.
 */
export interface TokenMetadata {
  token_address?: string;
  pool_address?: string | null;
  name?: string | null;
  symbol?: string | null;
  decimals?: number;
  total_supply?: string;
  creator?: string | null;
  description?: string | null;
  socials?: {
    website?: string | null;
    twitter?: string | null;
    telegram?: string | null;
    discord?: string | null;
  };
  tags?: string[];
  images?: { logo?: string | null; banner?: string | null };
  created_at?: number | null;
  updated_at?: number | null;
}

/** The v2 PoolStats row (interface 2.2.0); display reads live in core/market/pool-stats-display.ts. */
import type { PoolStats } from '@/core/market/use-stats-batch';
export type { PoolStats };

/** GET /stats/{poolAddress}/transactions — actual API returns camelCase */
export interface PoolTransaction {
  id: string;
  poolAddress: string;
  sender: string;
  isBuy: boolean;
  amountIn: string;
  amountOut: string;
  price: string;
  fee: string;
  blockTimestamp: number;
  txHash: string;
}

/** GET /stats/{poolAddress}/holders — actual API returns camelCase */
export interface PoolHolder {
  poolAddress?: string;
  holderAddress: string;
  balance: string;
  pctOfSupply: string;
  lastUpdated?: number;
  rank?: number;
}

export interface EnrichedToken {
  poolAddress: string;
  rank?: number;
  meta: TokenMetadata | null;
  stats: PoolStats | null;
  isWatchlisted: boolean;
}
