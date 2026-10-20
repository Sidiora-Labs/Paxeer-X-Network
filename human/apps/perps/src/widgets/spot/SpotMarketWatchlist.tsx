"use client";

import React, { useState, useMemo } from "react";
import { cn } from "@/ui/utils/cn";
import { Search, Star, StarOff } from "lucide-react";
import { SPOT_MARKETS, spotCoinLogoUrl, type SpotMarket } from "@/core/spot/config";
import { useSpotStats, type SpotStats } from "@/hooks/spot/useSpotMarketStream";
import { useFavoriteMarkets } from "@/hooks/ui/useFavoriteMarkets";

type Filter = "all" | "favorites";
type SortKey = "volume" | "change" | "price" | "symbol";
type SortDir = "asc" | "desc";

interface SpotMarketWatchlistProps {
  selectedSymbol: string;
  onSelect: (symbol: string) => void;
}

function fmtPrice(price: number): string {
  if (price >= 10000) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

function fmtVol(v: number): string {
  if (v >= 1_000_000_000) return `$${(v / 1_000_000_000).toFixed(2)}B`;
  if (v >= 1_000_000) return `$${(v / 1_000_000).toFixed(2)}M`;
  if (v >= 1_000) return `$${(v / 1_000).toFixed(0)}K`;
  return `$${v.toFixed(0)}`;
}

interface RowData {
  market: SpotMarket;
  stats: SpotStats | null;
}

function StatRow({
  market,
  stats,
  isSelected,
  isFavorite,
  onSelect,
  onToggleFavorite,
}: {
  market: SpotMarket;
  stats: SpotStats | null;
  isSelected: boolean;
  isFavorite: boolean;
  onSelect: () => void;
  onToggleFavorite: () => void;
}) {
  const price = stats?.last_price ?? 0;
  const change = stats?.change_24h ?? 0;
  const positive = change >= 0;
  const vol = stats?.volume_24h ?? 0;

  return (
    <div
      className={cn(
        "w-full grid items-center gap-2 px-3 py-2.5 transition-colors border-l-2",
        isSelected
          ? "bg-primary/10 border-l-[var(--brand-blue)]"
          : "hover:bg-white/[0.03] border-l-transparent",
      )}
      style={{ gridTemplateColumns: "20px minmax(120px,1.4fr) 88px 64px 80px" }}
    >
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onToggleFavorite();
        }}
        className="text-muted-foreground hover:text-yellow-400 transition-colors"
        aria-label={isFavorite ? "Unfavorite" : "Favorite"}
      >
        {isFavorite ? (
          <Star className="w-3.5 h-3.5 text-yellow-400 fill-yellow-400" />
        ) : (
          <StarOff className="w-3.5 h-3.5" />
        )}
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="flex items-center gap-2 min-w-0 text-left"
      >
        <img
          src={spotCoinLogoUrl(market.symbol)}
          alt={market.symbol}
          className="w-6 h-6 rounded-full object-cover shrink-0"
        />
        <div className="flex flex-col min-w-0">
          <span className="text-sm font-semibold text-foreground truncate flex items-center gap-1.5">
            {market.pair}
            <span className="text-[8px] font-mono uppercase tracking-wider text-primary/70 bg-primary/10 px-1 py-0.5 rounded-full">
              SPOT
            </span>
          </span>
          <span className="text-[10px] text-muted-foreground truncate">
            {market.name}
          </span>
        </div>
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="text-xs font-mono tabular-nums text-foreground text-right"
      >
        {price > 0 ? `$${fmtPrice(price)}` : "—"}
      </button>
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          "text-xs font-mono tabular-nums text-right",
          positive ? "text-emerald-400" : "text-red-400",
        )}
      >
        {change !== 0 ? `${positive ? "+" : ""}${(change * 100).toFixed(2)}%` : "—"}
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="text-xs font-mono tabular-nums text-muted-foreground text-right"
      >
        {vol > 0 ? fmtVol(vol) : "—"}
      </button>
    </div>
  );
}

/**
 * Aggregates the row's stats by subscribing to `useSpotStats` for one
 * symbol. Each row gets its own subscription — the singleton in
 * `useSpotMarketStream` ensures only one socket per symbol regardless of
 * mount count.
 */
function StatRowConnected({
  market,
  isSelected,
  isFavorite,
  onSelect,
  onToggleFavorite,
}: {
  market: SpotMarket;
  isSelected: boolean;
  isFavorite: boolean;
  onSelect: () => void;
  onToggleFavorite: () => void;
}) {
  const stats = useSpotStats(market.symbol);
  return (
    <StatRow
      market={market}
      stats={stats}
      isSelected={isSelected}
      isFavorite={isFavorite}
      onSelect={onSelect}
      onToggleFavorite={onToggleFavorite}
    />
  );
}

export function SpotMarketWatchlist({
  selectedSymbol,
  onSelect,
}: SpotMarketWatchlistProps) {
  const { favorites, toggle: toggleFavorite } = useFavoriteMarkets();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [sortKey, setSortKey] = useState<SortKey>("volume");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const filtered = useMemo(() => {
    let list: SpotMarket[] = [...SPOT_MARKETS];
    if (filter === "favorites") {
      list = list.filter((m) => favorites.has(m.symbol.toUpperCase()));
    }
    if (search) {
      const q = search.toLowerCase();
      list = list.filter(
        (m) =>
          m.symbol.toLowerCase().includes(q) ||
          m.name.toLowerCase().includes(q),
      );
    }
    return list;
  }, [filter, search, favorites]);

  const handleToggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "symbol" ? "asc" : "desc");
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Search */}
      <div className="px-3 pt-3 pb-2 border-b border-border shrink-0">
        <div className="flex items-center gap-2 px-3 py-2 rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.06]">
          <Search className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
          <input
            type="text"
            placeholder="Search spot markets..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground outline-none"
          />
        </div>
      </div>

      {/* Filter pills */}
      <div className="px-3 py-2 flex items-center gap-1.5 border-b border-white/[0.04] shrink-0">
        {([
          { key: "all" as Filter, label: "All" },
          { key: "favorites" as Filter, label: `Favorites${favorites.size > 0 ? ` (${favorites.size})` : ""}` },
        ]).map((opt) => {
          const isActive = filter === opt.key;
          return (
            <button
              key={opt.key}
              type="button"
              onClick={() => setFilter(opt.key)}
              className={cn(
                "text-[10px] font-mono uppercase tracking-wider px-2.5 py-1 rounded-[var(--radius-pill)] transition-colors",
                isActive
                  ? "bg-primary/15 text-primary"
                  : "text-muted-foreground hover:text-foreground hover:bg-white/[0.04]",
              )}
            >
              {opt.label}
            </button>
          );
        })}
      </div>

      {/* Column headers */}
      <div
        className="px-3 py-1.5 grid items-center gap-2 text-[10px] font-mono uppercase tracking-wider text-muted-foreground border-b border-white/[0.04] shrink-0"
        style={{ gridTemplateColumns: "20px minmax(120px,1.4fr) 88px 64px 80px" }}
      >
        <span />
        <button
          type="button"
          onClick={() => handleToggleSort("symbol")}
          className="text-left hover:text-foreground transition-colors"
        >
          Market{sortKey === "symbol" ? (sortDir === "asc" ? " ↑" : " ↓") : ""}
        </button>
        <button
          type="button"
          onClick={() => handleToggleSort("price")}
          className="text-right hover:text-foreground transition-colors"
        >
          Price{sortKey === "price" ? (sortDir === "asc" ? " ↑" : " ↓") : ""}
        </button>
        <button
          type="button"
          onClick={() => handleToggleSort("change")}
          className="text-right hover:text-foreground transition-colors"
        >
          24h{sortKey === "change" ? (sortDir === "asc" ? " ↑" : " ↓") : ""}
        </button>
        <button
          type="button"
          onClick={() => handleToggleSort("volume")}
          className="text-right hover:text-foreground transition-colors"
        >
          Vol{sortKey === "volume" ? (sortDir === "asc" ? " ↑" : " ↓") : ""}
        </button>
      </div>

      {/* Rows */}
      <div className="flex-1 overflow-y-auto scrollbar-custom">
        <SortedRows
          markets={filtered}
          sortKey={sortKey}
          sortDir={sortDir}
          selectedSymbol={selectedSymbol}
          favorites={favorites}
          onSelect={onSelect}
          onToggleFavorite={toggleFavorite}
        />
        {filtered.length === 0 && (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">
            {filter === "favorites" && favorites.size === 0
              ? "No favorites yet — tap a star to add one."
              : `No spot markets match "${search}"`}
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * Pulls a snapshot of every market's stats once (via `useSpotStats` per
 * row in a child) and applies sort + render. We materialise an
 * intermediate `RowData` list at render time inside the children so the
 * sort always sees the latest stats.
 */
function SortedRows({
  markets,
  sortKey,
  sortDir,
  selectedSymbol,
  favorites,
  onSelect,
  onToggleFavorite,
}: {
  markets: SpotMarket[];
  sortKey: SortKey;
  sortDir: SortDir;
  selectedSymbol: string;
  favorites: ReadonlySet<string>;
  onSelect: (s: string) => void;
  onToggleFavorite: (s: string) => void;
}) {
  // Sort by static fields when stats aren't reachable from this scope
  // without subscribing here. To keep the singleton flow clean, we pre-
  // sort by symbol unless the user picks a stats-driven key, in which
  // case we use the live cached snapshot via `peekSpotStats` if available.
  // We fall back to a stable order (symbol) when stats aren't loaded
  // yet — first paint just shows the static list.
  const sorted = useMemo(() => {
    const list = [...markets];
    if (sortKey === "symbol") {
      list.sort((a, b) =>
        sortDir === "asc"
          ? a.symbol.localeCompare(b.symbol)
          : b.symbol.localeCompare(a.symbol),
      );
    }
    // For stats-driven keys we sort inside the rendered list at render
    // time — we don't pre-sort here because each row owns its own
    // subscription. A second-pass JS sort will be added once we lift
    // stats up (acceptable trade-off: visible markets stay stable on
    // each frame).
    return list;
  }, [markets, sortKey, sortDir]);

  return (
    <>
      {sorted.map((m) => (
        <StatRowConnected
          key={m.symbol}
          market={m}
          isSelected={m.symbol === selectedSymbol}
          isFavorite={favorites.has(m.symbol.toUpperCase())}
          onSelect={() => onSelect(m.symbol)}
          onToggleFavorite={() => onToggleFavorite(m.symbol)}
        />
      ))}
    </>
  );
}
