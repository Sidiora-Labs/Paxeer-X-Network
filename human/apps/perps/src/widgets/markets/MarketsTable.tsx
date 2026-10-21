import React, { useCallback, useMemo } from "react";
import { useRouter } from "next/router";
import { ArrowUp, ArrowDown, Star } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import {
  fmtPrice,
  fmtChangePct,
  fmtUsdCompact as fmtUsd,
} from "@/ui/utils/format";
import { formatFundingRatePct } from "@/core/perps/fundingRate";
import { coinLogoUrl } from "@/core/perps/config";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import { MarketsTableSkeleton } from "./MarketsSkeletons";
import type { MarketRow, SortDir, SortKey } from "./types";

interface MarketsTableProps {
  rows: readonly MarketRow[];
  /** Full unfiltered dataset — used to decide whether to show the "waiting for live data" skeleton vs an empty state. */
  loading: boolean;
  /** Optional human-readable "empty" message override when no rows match filters. */
  emptyMessage?: string;
  favorites: ReadonlySet<string>;
  onToggleFavorite: (symbol: string) => void;
  sortKey: SortKey;
  sortDir: SortDir;
  onToggleSort: (key: SortKey) => void;
}

const COLUMN_TEMPLATE = "minmax(180px,1.5fr) minmax(90px,1fr) minmax(80px,1fr) minmax(100px,1fr) minmax(100px,1fr) minmax(100px,1fr) 96px";

export function MarketsTable({
  rows,
  loading,
  emptyMessage,
  favorites,
  onToggleFavorite,
  sortKey,
  sortDir,
  onToggleSort,
}: MarketsTableProps) {
  if (loading && rows.length === 0) {
    return <MarketsTableSkeleton rows={8} />;
  }

  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
      <MarketsTableHeader
        sortKey={sortKey}
        sortDir={sortDir}
        onToggleSort={onToggleSort}
      />
      <div className="divide-y divide-white/[0.04]">
        {rows.length === 0 ? (
          <MarketsTableEmpty message={emptyMessage} />
        ) : (
          rows.map((row) => (
            <MarketRowItem
              key={row.marketId}
              row={row}
              isFavorite={favorites.has(row.symbol.toUpperCase())}
              onToggleFavorite={onToggleFavorite}
            />
          ))
        )}
      </div>
    </div>
  );
}

// ── Header ────────────────────────────────────────────────────────────

function MarketsTableHeader({
  sortKey,
  sortDir,
  onToggleSort,
}: {
  sortKey: SortKey;
  sortDir: SortDir;
  onToggleSort: (k: SortKey) => void;
}) {
  return (
    <div
      role="row"
      className={cn(
        "hidden md:grid items-center px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground",
        "border-b border-white/[0.04]",
      )}
      style={{ gridTemplateColumns: COLUMN_TEMPLATE }}
    >
      <HeaderSort label="Market" align="left" sortKey="symbol" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <HeaderSort label="Last" align="right" sortKey="price" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <HeaderSort label="24h" align="right" sortKey="change24h" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <HeaderSort label="24h volume" align="right" sortKey="volume24h" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <HeaderSort label="Open interest" align="right" sortKey="openInterest" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <HeaderSort label="8h funding" align="right" sortKey="funding8h" active={sortKey} dir={sortDir} onToggle={onToggleSort} />
      <span aria-hidden className="text-right" />
    </div>
  );
}

function HeaderSort({
  label,
  align,
  sortKey,
  active,
  dir,
  onToggle,
}: {
  label: string;
  align: "left" | "right";
  sortKey: SortKey;
  active: SortKey;
  dir: SortDir;
  onToggle: (k: SortKey) => void;
}) {
  const isActive = active === sortKey;
  return (
    <button
      type="button"
      onClick={() => onToggle(sortKey)}
      className={cn(
        "inline-flex items-center gap-1 select-none h-full cursor-pointer",
        "hover:text-foreground transition-colors focus:outline-none",
        align === "right" ? "justify-end text-right" : "justify-start text-left",
        isActive && "text-foreground",
      )}
    >
      <span>{label}</span>
      <span
        aria-hidden
        className={cn(
          "inline-flex flex-col h-3 justify-between",
          isActive ? "text-foreground" : "text-muted-foreground/50",
        )}
      >
        {isActive ? (
          dir === "asc" ? <ArrowUp className="h-2.5 w-2.5" /> : <ArrowDown className="h-2.5 w-2.5" />
        ) : (
          <>
            <ArrowUp className="h-2 w-2 -mb-[3px]" />
            <ArrowDown className="h-2 w-2" />
          </>
        )}
      </span>
    </button>
  );
}

// ── Row ───────────────────────────────────────────────────────────────

const MarketRowItem = React.memo(function MarketRowItem({
  row,
  isFavorite,
  onToggleFavorite,
}: {
  row: MarketRow;
  isFavorite: boolean;
  onToggleFavorite: (symbol: string) => void;
}) {
  const router = useRouter();
  const positive = row.change24h >= 0;
  const fundingPositive = row.funding8h !== null && row.funding8h >= 0n;

  const goTrade = useCallback(
    (e?: React.MouseEvent | React.KeyboardEvent) => {
      e?.stopPropagation();
      router.push(`/perps?market=${row.symbol}`);
    },
    [router, row.symbol],
  );

  const toggleFav = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onToggleFavorite(row.symbol);
    },
    [onToggleFavorite, row.symbol],
  );

  return (
    <div
      role="row"
      tabIndex={0}
      onClick={() => goTrade()}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          goTrade(e);
        }
      }}
      className={cn(
        "group flex md:grid items-center gap-3 px-4 py-3 cursor-pointer transition-colors",
        "hover:bg-white/[0.03] focus:outline-none focus-visible:bg-white/[0.04]",
      )}
      style={{ gridTemplateColumns: COLUMN_TEMPLATE }}
    >
      {/* ── Symbol + meta ── */}
      <div className="flex items-center gap-3 min-w-0 flex-1 md:flex-none">
        <button
          type="button"
          onClick={toggleFav}
          aria-label={isFavorite ? `Remove ${row.symbol} from favorites` : `Add ${row.symbol} to favorites`}
          className={cn(
            "shrink-0 p-1 rounded-md transition-colors",
            "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
            isFavorite
              ? "text-[var(--brand-orange)]"
              : "text-muted-foreground/40 hover:text-[var(--brand-orange)] opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
          )}
        >
          <Star className={cn("h-3.5 w-3.5", isFavorite && "fill-[var(--brand-orange)]")} />
        </button>
        <img
          src={coinLogoUrl(row.symbol)}
          alt=""
          className="h-8 w-8 rounded-full object-cover shrink-0 bg-surface-2"
          loading="lazy"
          onError={(e) => { (e.target as HTMLImageElement).style.visibility = "hidden"; }}
        />
        <div className="flex flex-col min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold text-foreground truncate">{row.symbol}</span>
            <span className="text-[9px] font-mono font-medium text-muted-foreground bg-surface-2 rounded px-1.5 py-[1px] tabular-nums">
              {row.maxLeverage}x
            </span>
            {row.isNew ? (
              <span className="text-[9px] font-mono font-medium px-1.5 py-[1px] rounded bg-[var(--brand-orange)]/10 text-[var(--brand-orange)]">
                NEW
              </span>
            ) : null}
          </div>
          <span className="text-[10px] text-muted-foreground truncate">{row.name}</span>
        </div>
      </div>

      {/* ── Mobile: compact price/change block ── */}
      <div className="flex flex-col items-end shrink-0 md:hidden">
        <PriceFlash
          value={row.price}
          className="text-sm font-mono font-semibold tabular-nums text-foreground"
        >
          {row.price > 0 ? `$${fmtPrice(row.price)}` : "—"}
        </PriceFlash>
        <span
          className={cn(
            "text-[11px] font-mono tabular-nums",
            row.hasLiveData ? (positive ? "text-emerald-400" : "text-red-400") : "text-muted-foreground",
          )}
        >
          {row.hasLiveData ? fmtChangePct(row.change24h) : "—"}
        </span>
        <span className="text-[10px] font-mono tabular-nums text-muted-foreground mt-0.5">
          {row.volume24h > 0 ? `${fmtUsd(row.volume24h)} vol` : ""}
        </span>
      </div>

      {/* ── Desktop grid cells ── */}
      <div className="hidden md:flex justify-end">
        <PriceFlash
          value={row.price}
          className="text-sm font-mono font-semibold tabular-nums text-foreground"
        >
          {row.price > 0 ? `$${fmtPrice(row.price)}` : "—"}
        </PriceFlash>
      </div>
      <div className="hidden md:flex justify-end">
        <span
          className={cn(
            "text-xs font-mono tabular-nums font-medium",
            row.hasLiveData ? (positive ? "text-emerald-400" : "text-red-400") : "text-muted-foreground",
          )}
        >
          {row.hasLiveData ? fmtChangePct(row.change24h) : "—"}
        </span>
      </div>
      <div className="hidden md:flex justify-end">
        <PriceFlash
          value={row.volume24h}
          className="text-xs font-mono tabular-nums text-muted-foreground"
        >
          {row.volume24h > 0 ? fmtUsd(row.volume24h) : "—"}
        </PriceFlash>
      </div>
      <div className="hidden md:flex justify-end">
        <PriceFlash
          value={row.openInterest}
          className="text-xs font-mono tabular-nums text-muted-foreground"
        >
          {row.openInterest > 0 ? fmtUsd(row.openInterest) : "—"}
        </PriceFlash>
      </div>
      <div className="hidden md:flex justify-end">
        <span
          className={cn(
            "text-xs font-mono tabular-nums",
            row.funding8h === null || row.funding8h === 0n
              ? "text-muted-foreground"
              : fundingPositive
                ? "text-emerald-400"
                : "text-red-400",
          )}
        >
          {row.funding8h === null ? "—" : `${row.funding8h >= 0n ? "+" : ""}${formatFundingRatePct(row.funding8h)}`}
        </span>
      </div>

      <div className="hidden md:flex justify-end">
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-[11px] px-3"
          onClick={(e) => goTrade(e)}
        >
          Trade
        </Button>
      </div>
    </div>
  );
});

// ── Empty state ───────────────────────────────────────────────────────

function MarketsTableEmpty({ message }: { message?: string }) {
  return (
    <div className="py-16 flex flex-col items-center justify-center gap-1.5 text-center">
      <span className="text-sm font-semibold text-foreground">
        {message ?? "No markets match"}
      </span>
      <span className="text-xs text-muted-foreground max-w-xs">
        Try adjusting your filters or search. Favorites show only markets you've starred.
      </span>
    </div>
  );
}

// ── Helpers exposed for the page ──────────────────────────────────────

export function useSortedPageRows(
  rows: readonly MarketRow[],
  pageSize: number,
  page: number,
): { pageRows: MarketRow[]; totalPages: number } {
  return useMemo(() => {
    const total = rows.length;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    const start = page * pageSize;
    const pageRows = rows.slice(start, start + pageSize);
    return { pageRows, totalPages };
  }, [rows, pageSize, page]);
}
