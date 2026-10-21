import type { MarketSnapshot } from "@/hooks/perps/useMarketsStream";
import type { PerpsMarket } from "@/core/perps/config";

/**
 * A single row in the Markets page table. Combines static per-market
 * config (from `PERPS_MARKETS`) with the live `markets@all` snapshot
 * and a derived `category` string. The snapshot may be `undefined` on
 * first render before the WS feed lands.
 */
export interface MarketRow extends PerpsMarket {
  /** Oracle / mid price — `0` until the feed responds. */
  price: number;
  /** 24h change as a percentage (e.g. -1.87 for -1.87%). */
  change24h: number;
  /** Notional 24h volume, in quote USD. */
  volume24h: number;
  /** Open interest notional, in quote USD (matches the feed convention). */
  openInterest: number;
  /** Realised 8h funding ratio (0.0001 = 0.01%). */
  funding8h: number;
  /** Last settled hourly funding rate at 18 decimals; null before the first settlement or the feed. */
  fundingHourly: bigint | null;
  /** Canonical category (crypto / stocks / indices / commodities). */
  category: string;
  /** True if this market is flagged as a "new listing" on the product. */
  isNew: boolean;
  /** True if the snapshot is live; false if placeholder (pre-WS frame). */
  hasLiveData: boolean;
  /** Raw feed snapshot if available — lets consumers read uncommon fields. */
  snapshot?: MarketSnapshot;
}

/**
 * Active filter for the main table. `favorites` is synthetic and resolved
 * by the page against `useFavoriteMarkets`.
 */
export type MarketsCategoryFilter =
  | "favorites"
  | "all"
  | "crypto"
  | "rwa"
  | "new";

/** One of the highlight panels on the Markets overview. */
export type MarketsHighlight = "new" | "gainers" | "losers";

export type SortKey =
  | "symbol"
  | "price"
  | "change24h"
  | "volume24h"
  | "openInterest"
  | "funding8h";

export type SortDir = "asc" | "desc";
