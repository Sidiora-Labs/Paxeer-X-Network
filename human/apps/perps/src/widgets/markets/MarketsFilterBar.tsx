import React from "react";
import { Star, Search } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import type { MarketsCategoryFilter } from "./types";

interface MarketsFilterBarProps {
  category: MarketsCategoryFilter;
  onChangeCategory: (c: MarketsCategoryFilter) => void;
  search: string;
  onChangeSearch: (v: string) => void;
  favoriteCount: number;
  className?: string;
}

const CATEGORIES: Array<{ key: MarketsCategoryFilter; label: string }> = [
  { key: "all", label: "All markets" },
  { key: "crypto", label: "Crypto" },
  { key: "rwa", label: "RWA" },
  { key: "new", label: "New listings" },
];

/**
 * Full filter bar: ⭐ favorites pill on the far left, category pills
 * in the middle, and a search field on the right. Wraps gracefully on
 * mobile — pills scroll horizontally if they overflow.
 */
export function MarketsFilterBar({
  category,
  onChangeCategory,
  search,
  onChangeSearch,
  favoriteCount,
  className,
}: MarketsFilterBarProps) {
  return (
    <div
      className={cn(
        "flex flex-col sm:flex-row sm:items-center gap-2 md:gap-3",
        className,
      )}
    >
      <div className="flex items-center gap-1.5 overflow-x-auto scrollbar-hide">
        <FavoritesPill
          active={category === "favorites"}
          onClick={() => onChangeCategory("favorites")}
          count={favoriteCount}
        />
        {CATEGORIES.map((c) => {
          const active = category === c.key;
          return (
            <button
              key={c.key}
              type="button"
              aria-pressed={active}
              onClick={() => onChangeCategory(c.key)}
              className={cn(
                "shrink-0 px-3 py-1.5 rounded-[var(--radius-pill)] text-xs font-medium transition-colors whitespace-nowrap",
                "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
                active
                  ? "bg-surface-2 text-foreground border border-white/[0.08]"
                  : "bg-surface-1/80 text-muted-foreground hover:text-foreground hover:bg-surface-2 border border-transparent",
              )}
            >
              {c.label}
            </button>
          );
        })}
      </div>

      <div className="relative ml-auto w-full sm:w-64">
        <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
        <input
          type="search"
          inputMode="search"
          placeholder="Search market"
          value={search}
          onChange={(e) => onChangeSearch(e.target.value)}
          className={cn(
            "w-full h-9 rounded-[var(--radius-pill)] bg-surface-1 border border-white/[0.06] pl-9 pr-3",
            "text-sm text-foreground placeholder:text-muted-foreground",
            "focus:outline-none focus:ring-2 focus:ring-ring/30 focus:border-white/[0.12]",
            "transition-colors",
          )}
        />
      </div>
    </div>
  );
}

function FavoritesPill({
  active,
  onClick,
  count,
}: {
  active: boolean;
  onClick: () => void;
  count: number;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      aria-label="Favorites"
      onClick={onClick}
      className={cn(
        "shrink-0 inline-flex items-center gap-1 h-[30px] px-2.5 rounded-[var(--radius-pill)] transition-colors",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
        active
          ? "bg-[var(--brand-orange)]/15 text-[var(--brand-orange)] border border-[var(--brand-orange)]/30"
          : "bg-surface-1/80 text-muted-foreground hover:text-[var(--brand-orange)] hover:bg-surface-2 border border-transparent",
      )}
    >
      <Star
        className={cn("h-3.5 w-3.5", active && "fill-[var(--brand-orange)]")}
        strokeWidth={active ? 2 : 1.75}
      />
      {count > 0 ? (
        <span className="text-[11px] font-mono font-medium tabular-nums">{count}</span>
      ) : null}
    </button>
  );
}
