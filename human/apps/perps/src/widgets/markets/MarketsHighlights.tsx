import React, { useMemo, useState } from "react";
import { useRouter } from "next/router";
import { ArrowUpRight, ArrowDownRight, Sparkles } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { coinLogoUrl } from "@/core/perps/config";
import { fmtPrice, fmtChangePct } from "@/ui/utils/format";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import type { MarketRow, MarketsHighlight } from "./types";
import { HighlightsPanelSkeleton } from "./MarketsSkeletons";

interface MarketsHighlightsProps {
  rows: readonly MarketRow[];
  hasAnyLive: boolean;
  /** Mobile/narrow layouts can use the tabbed variant. */
  variant?: "columns" | "tabs";
  className?: string;
}

const PANEL_MAX_ROWS = 5;

const PANEL_META: Record<MarketsHighlight, { label: string; icon: React.ReactNode; accent: string }> = {
  new: {
    label: "New listings",
    icon: <Sparkles className="h-3.5 w-3.5" />,
    accent: "text-[var(--brand-orange)]",
  },
  gainers: {
    label: "Top gainers",
    icon: <ArrowUpRight className="h-3.5 w-3.5" />,
    accent: "text-emerald-400",
  },
  losers: {
    label: "Top losers",
    icon: <ArrowDownRight className="h-3.5 w-3.5" />,
    accent: "text-red-400",
  },
};

/**
 * Compute top-5 gainers / losers from the given rows. Excludes markets
 * with no live snapshot yet so the lists don't fill up with zeros before
 * the WS feed arrives.
 */
function selectRows(rows: readonly MarketRow[]): Record<MarketsHighlight, MarketRow[]> {
  const live = rows.filter((r) => r.hasLiveData);
  const newRows = rows.filter((r) => r.isNew).slice(0, PANEL_MAX_ROWS);
  const gainers = [...live]
    .sort((a, b) => b.change24h - a.change24h)
    .slice(0, PANEL_MAX_ROWS);
  const losers = [...live]
    .sort((a, b) => a.change24h - b.change24h)
    .slice(0, PANEL_MAX_ROWS);
  return { new: newRows, gainers, losers };
}

export function MarketsHighlights({
  rows,
  hasAnyLive,
  variant = "columns",
  className,
}: MarketsHighlightsProps) {
  const panels = useMemo(() => selectRows(rows), [rows]);

  if (variant === "tabs") {
    return <HighlightsTabs rows={rows} hasAnyLive={hasAnyLive} panels={panels} className={className} />;
  }

  return (
    <div
      className={cn(
        "grid grid-cols-1 sm:grid-cols-3 gap-3",
        className,
      )}
    >
      <HighlightPanel kind="new" rows={panels.new} hasAnyLive={hasAnyLive} />
      <HighlightPanel kind="gainers" rows={panels.gainers} hasAnyLive={hasAnyLive} />
      <HighlightPanel kind="losers" rows={panels.losers} hasAnyLive={hasAnyLive} />
    </div>
  );
}

function HighlightsTabs({
  rows,
  hasAnyLive,
  panels,
  className,
}: {
  rows: readonly MarketRow[];
  hasAnyLive: boolean;
  panels: Record<MarketsHighlight, MarketRow[]>;
  className?: string;
}) {
  const [active, setActive] = useState<MarketsHighlight>("new");
  // If there are no new listings to show, fall back to gainers so the
  // default view isn't an empty state.
  const effectiveActive = active === "new" && panels.new.length === 0 ? "gainers" : active;
  void rows;

  return (
    <div
      className={cn(
        "rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden",
        className,
      )}
    >
      <div className="flex items-center gap-1 px-2 pt-2">
        {(Object.keys(PANEL_META) as MarketsHighlight[]).map((kind) => {
          const isActive = effectiveActive === kind;
          const meta = PANEL_META[kind];
          return (
            <button
              key={kind}
              type="button"
              onClick={() => setActive(kind)}
              aria-pressed={isActive}
              className={cn(
                "flex-1 flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-md)] text-xs font-medium transition-colors",
                isActive
                  ? "bg-surface-2 text-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-white/[0.03]",
              )}
            >
              <span className={cn(isActive ? meta.accent : "text-muted-foreground")}>{meta.icon}</span>
              {meta.label}
            </button>
          );
        })}
      </div>
      <HighlightList rows={panels[effectiveActive]} hasAnyLive={hasAnyLive} kind={effectiveActive} />
    </div>
  );
}

function HighlightPanel({
  kind,
  rows,
  hasAnyLive,
}: {
  kind: MarketsHighlight;
  rows: MarketRow[];
  hasAnyLive: boolean;
}) {
  const meta = PANEL_META[kind];
  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2.5 border-b border-white/[0.04]">
        <span className={cn("inline-flex items-center justify-center h-5 w-5 rounded-[var(--radius-sm)] bg-surface-2", meta.accent)}>
          {meta.icon}
        </span>
        <span className="text-xs font-semibold">{meta.label}</span>
      </div>
      <HighlightList rows={rows} hasAnyLive={hasAnyLive} kind={kind} />
    </div>
  );
}

function HighlightList({
  rows,
  hasAnyLive,
  kind,
}: {
  rows: MarketRow[];
  hasAnyLive: boolean;
  kind: MarketsHighlight;
}) {
  const router = useRouter();
  if (!hasAnyLive && rows.length === 0) {
    return <HighlightsPanelSkeleton rows={PANEL_MAX_ROWS} />;
  }
  if (rows.length === 0) {
    return (
      <div className="px-3 py-6 text-center text-xs text-muted-foreground">
        {kind === "new" ? "No new listings right now" : "No data yet"}
      </div>
    );
  }
  return (
    <div className="flex flex-col">
      {rows.map((m) => {
        const positive = m.change24h >= 0;
        return (
          <button
            key={m.marketId}
            type="button"
            onClick={() => router.push(`/perps?market=${m.symbol}`)}
            className="group flex items-center justify-between gap-3 px-3 py-2 hover:bg-white/[0.03] transition-colors focus:outline-none focus-visible:bg-white/[0.05]"
          >
            <div className="flex items-center gap-2 min-w-0">
              <img
                src={coinLogoUrl(m.symbol)}
                alt=""
                className="h-7 w-7 rounded-full object-cover shrink-0 bg-surface-2"
                loading="lazy"
                onError={(e) => { (e.target as HTMLImageElement).style.visibility = "hidden"; }}
              />
              <div className="flex flex-col items-start min-w-0">
                <span className="text-sm font-semibold text-foreground truncate">{m.symbol}</span>
                <span className="text-[10px] text-muted-foreground truncate max-w-[9rem]">
                  {m.name}
                </span>
              </div>
            </div>
            <div className="flex flex-col items-end shrink-0">
              <PriceFlash
                value={m.price}
                className="text-sm font-mono font-semibold tabular-nums text-foreground"
              >
                {m.price > 0 ? `$${fmtPrice(m.price)}` : "—"}
              </PriceFlash>
              <span
                className={cn(
                  "text-[10px] font-mono tabular-nums",
                  positive ? "text-emerald-400" : "text-red-400",
                )}
              >
                {m.hasLiveData ? fmtChangePct(m.change24h) : "—"}
              </span>
            </div>
          </button>
        );
      })}
    </div>
  );
}
