"use client";

import { useMemo } from "react";
import {
  MARKET_CATEGORIES,
  PERPS_MARKETS,
  getMarketCategory,
} from "@/core/perps/config";
import {
  useMarketsSnapshot,
  type MarketSnapshot,
} from "@/hooks/perps/useMarketsStream";
import type { MarketRow } from "./types";

/**
 * Canonical "new listing" market IDs. Matches the existing convention
 * from `page-modules/markets`. When the backend grows a dedicated "new"
 * flag this list can move to config.
 */
export const NEW_MARKET_IDS: ReadonlySet<number> = new Set([5, 6, 7, 8, 9, 10]);

/**
 * True if the category filter should include this market.
 *
 * - `rwa` bundles stocks + indices + commodities (as labelled in the
 *   reference design's filter chip).
 * - `crypto` maps 1:1 to the canonical crypto market list.
 * - `new` uses `NEW_MARKET_IDS`.
 * - `all` (and anything unrecognised) includes every market.
 * - `favorites` is resolved upstream in the page — it's out of scope here.
 */
function matchesCategory(category: string, row: MarketRow): boolean {
  if (category === "all" || category === "favorites") return true;
  if (category === "new") return row.isNew;
  if (category === "rwa") {
    return (
      MARKET_CATEGORIES.stocks.includes(row.marketId) ||
      MARKET_CATEGORIES.indices.includes(row.marketId) ||
      MARKET_CATEGORIES.commodities.includes(row.marketId)
    );
  }
  if (category === "crypto") {
    return MARKET_CATEGORIES.crypto.includes(row.marketId);
  }
  return true;
}

/**
 * Build the canonical `MarketRow[]` for the page. The order returned
 * follows `PERPS_MARKETS` (i.e. stable, deterministic); callers apply
 * their own sort/filter on top.
 */
export function useMarketsRows(): { rows: MarketRow[]; bySymbol: Map<string, MarketRow>; hasAnyLive: boolean } {
  const snapshots = useMarketsSnapshot();

  return useMemo(() => {
    const snapBySymbol = new Map<string, MarketSnapshot>();
    for (const s of snapshots) {
      snapBySymbol.set(s.symbol.toUpperCase(), s);
    }

    const rows: MarketRow[] = PERPS_MARKETS.map((m) => {
      const snap = snapBySymbol.get(m.symbol.toUpperCase());
      return {
        ...m,
        price: snap?.price ?? 0,
        change24h: snap?.change_24h ?? 0,
        volume24h: snap?.volume_24h ?? 0,
        openInterest: snap?.open_interest ?? 0,
        funding8h: snap?.funding_8h ?? 0,
        fundingHourly: snap?.last_funding_rate_hourly ?? null,
        category: getMarketCategory(m.marketId),
        isNew: NEW_MARKET_IDS.has(m.marketId),
        hasLiveData: Boolean(snap),
        snapshot: snap,
      };
    });

    const bySymbol = new Map(rows.map((r) => [r.symbol, r]));
    return { rows, bySymbol, hasAnyLive: snapshots.length > 0 };
  }, [snapshots]);
}

/**
 * Pure filter + sort utility, extracted so tests and view-level memos
 * can share the same logic without creating a hook dependency cycle.
 */
export function filterAndSort(
  rows: readonly MarketRow[],
  opts: {
    search?: string;
    category?: string;
    favorites?: ReadonlySet<string>;
    sortKey?: import("./types").SortKey;
    sortDir?: import("./types").SortDir;
  },
): MarketRow[] {
  const { search, category = "all", favorites, sortKey = "volume24h", sortDir = "desc" } = opts;

  let list = rows.filter((r) => {
    if (category === "favorites") {
      if (!favorites || !favorites.has(r.symbol.toUpperCase())) return false;
    } else if (!matchesCategory(category, r)) {
      return false;
    }
    if (search) {
      const q = search.toLowerCase();
      if (
        !r.symbol.toLowerCase().includes(q) &&
        !r.name.toLowerCase().includes(q)
      ) {
        return false;
      }
    }
    return true;
  });

  list = [...list].sort((a, b) => {
    let cmp = 0;
    switch (sortKey) {
      case "symbol":
        cmp = a.symbol.localeCompare(b.symbol);
        break;
      case "price":
        cmp = a.price - b.price;
        break;
      case "change24h":
        cmp = a.change24h - b.change24h;
        break;
      case "volume24h":
        cmp = a.volume24h - b.volume24h;
        break;
      case "openInterest":
        cmp = a.openInterest - b.openInterest;
        break;
      case "funding8h":
        cmp = a.funding8h - b.funding8h;
        break;
    }
    return sortDir === "asc" ? cmp : -cmp;
  });

  return list;
}
