import React, { useMemo } from "react";
import { cn } from "@/ui/utils/cn";
import { fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import { useMarketsTotals, type MarketsTotals } from "@/hooks/perps/useMarketsStream";
import type { MarketRow } from "./types";
import { OverviewStatsSkeleton } from "./MarketsSkeletons";

interface StatItem {
  label: string;
  value: number;
  /** Optional secondary line under the main value. */
  hint?: string | null;
}

/**
 * Build the canonical overview stats rows out of the live totals feed,
 * with a row-level fallback for TVL that sums per-market OI in case the
 * aggregate `total_oi` hasn't been delivered yet.
 */
function buildStats(
  totals: MarketsTotals | null,
  rows: readonly MarketRow[],
): { items: StatItem[]; ready: boolean } {
  const sumOi = rows.reduce((acc, r) => acc + r.openInterest, 0);
  const sumVol = rows.reduce((acc, r) => acc + r.volume24h, 0);

  const volume24h = totals?.volume_24h ?? sumVol;
  const openInterest = totals?.total_oi ?? sumOi;
  const volume7d = totals?.volume_7d ?? 0;
  const volume30d = totals?.volume_30d ?? 0;

  const items: StatItem[] = [
    {
      label: "24h volume",
      value: volume24h,
      hint: totals && totals.total_trades > 0
        ? `${totals.total_trades.toLocaleString()} trades`
        : null,
    },
    {
      label: "Open interest",
      value: openInterest,
      hint: rows.length > 0 ? `Across ${rows.length} markets` : null,
    },
    {
      label: "7d volume",
      value: volume7d,
      hint: volume30d > 0 ? `${fmtUsd(volume30d)} / 30d` : null,
    },
  ];

  // Ready as soon as totals has arrived OR we have at least one live row —
  // either source gives the user a real number, not a zero skeleton.
  const ready = Boolean(totals) || rows.some((r) => r.hasLiveData);

  return { items, ready };
}

interface MarketsOverviewStatsProps {
  rows: readonly MarketRow[];
  /** Rendered in a row on mobile, a column on desktop. */
  layout?: "column" | "row";
  className?: string;
}

export function MarketsOverviewStats({
  rows,
  layout = "column",
  className,
}: MarketsOverviewStatsProps) {
  const totals = useMarketsTotals();

  const { items, ready } = useMemo(
    () => buildStats(totals, rows),
    [totals, rows],
  );

  if (!ready) {
    return (
      <div className={cn(layout === "row" && "grid grid-cols-3 gap-2", className)}>
        <OverviewStatsSkeleton />
      </div>
    );
  }

  return (
    <div
      className={cn(
        layout === "row"
          ? "grid grid-cols-3 gap-2"
          : "flex flex-col gap-2",
        className,
      )}
    >
      {items.map((s) => (
        <StatCard key={s.label} item={s} layout={layout} />
      ))}
    </div>
  );
}

function StatCard({ item, layout }: { item: StatItem; layout: "column" | "row" }) {
  const compact = layout === "row";
  return (
    <div
      className={cn(
        "rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04]",
        compact ? "px-3 py-2.5" : "px-4 py-3",
        "transition-colors",
      )}
    >
      <div
        className={cn(
          "text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground",
          compact && "text-[9px]",
        )}
      >
        {item.label}
      </div>
      <PriceFlash
        value={item.value}
        className={cn(
          "mt-1 font-mono font-semibold tabular-nums text-foreground",
          compact ? "text-base" : "text-xl",
        )}
      >
        {fmtUsd(item.value)}
      </PriceFlash>
      {item.hint ? (
        <div
          className={cn(
            "mt-0.5 text-[10px] font-mono text-muted-foreground/80 tabular-nums truncate",
            compact && "text-[9px]",
          )}
        >
          {item.hint}
        </div>
      ) : null}
    </div>
  );
}
