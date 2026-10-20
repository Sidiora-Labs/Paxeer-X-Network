"use client";

import React, { useMemo, useState } from "react";
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from "recharts";
import { ChevronDown } from "lucide-react";
import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { parse18 } from "@/ui/utils/format";
import { cn } from "@/ui/utils/cn";

export type AssetsTimeRange = "7D" | "30D" | "90D" | "ALL";

interface PortfolioAssetsChartProps {
  dailyPnl: DailyPnl[];
  /** Current on-chain vault USD — used to anchor the rightmost data point. */
  totalVaultUsd: number;
  isLoading?: boolean;
  /** Optional fixed range; when omitted the component owns its own dropdown. */
  range?: AssetsTimeRange;
  onChangeRange?: (r: AssetsTimeRange) => void;
  className?: string;
}

/**
 * Assets balance over time.
 *
 * Reconstructs an approximate historical series by walking backwards from
 * the current vault value and subtracting per-day realised PnL. It's not a
 * perfect replay (we don't have historical deposits/withdrawals here) but
 * it gives the user a meaningful curve instead of an empty state.
 */
export function PortfolioAssetsChart({
  dailyPnl,
  totalVaultUsd,
  isLoading,
  range: externalRange,
  onChangeRange,
  className,
}: PortfolioAssetsChartProps) {
  const [internalRange, setInternalRange] = useState<AssetsTimeRange>("7D");
  const range = externalRange ?? internalRange;
  const setRange = onChangeRange ?? setInternalRange;

  const data = useMemo(() => buildAssetSeries(dailyPnl, totalVaultUsd, range), [
    dailyPnl,
    totalVaultUsd,
    range,
  ]);

  const empty = data.length === 0;
  const color = "var(--brand-blue)";

  return (
    <section
      className={cn(
        "rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] p-4 md:p-5 flex flex-col",
        className,
      )}
    >
      <header className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-foreground">Assets</h3>
        <RangePicker value={range} onChange={setRange} />
      </header>

      {isLoading && empty ? (
        <div className="flex-1 min-h-[200px] rounded-[var(--radius-md)] bg-white/[0.02] animate-pulse" />
      ) : empty ? (
        <div className="flex-1 min-h-[200px] flex items-center justify-center text-xs text-muted-foreground">
          No historical data yet
        </div>
      ) : (
        <div className="h-56 md:h-64">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="assetsFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.32} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid
                stroke="rgba(255,255,255,0.04)"
                strokeDasharray="3 3"
                vertical={false}
              />
              <XAxis
                dataKey="date"
                tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                minTickGap={32}
              />
              <YAxis
                tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                width={52}
                tickFormatter={(v: number) => {
                  const abs = Math.abs(v);
                  if (abs >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
                  if (abs >= 1_000) return `${(v / 1_000).toFixed(1)}K`;
                  return v.toFixed(0);
                }}
              />
              <Tooltip
                contentStyle={{
                  background: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: 8,
                  fontSize: 12,
                }}
                labelStyle={{ color: "hsl(var(--muted-foreground))" }}
                formatter={(value: number) => [
                  `$${value.toLocaleString("en-US", { maximumFractionDigits: 2 })}`,
                  "Assets",
                ]}
              />
              <Area
                type="monotone"
                dataKey="value"
                stroke={color}
                strokeWidth={2}
                fill="url(#assetsFill)"
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </section>
  );
}

// ── Range picker ─────────────────────────────────────────────────────

const RANGE_OPTIONS: Array<{ key: AssetsTimeRange; label: string }> = [
  { key: "7D", label: "7D" },
  { key: "30D", label: "30D" },
  { key: "90D", label: "90D" },
  { key: "ALL", label: "ALL" },
];

function RangePicker({
  value,
  onChange,
}: {
  value: AssetsTimeRange;
  onChange: (v: AssetsTimeRange) => void;
}) {
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as AssetsTimeRange)}
        className="appearance-none bg-surface-2 border border-white/[0.06] rounded-[var(--radius-pill)] pl-3 pr-7 py-1 text-[11px] font-mono font-medium text-muted-foreground hover:text-foreground transition-colors focus:outline-none focus:ring-2 focus:ring-ring/30"
      >
        {RANGE_OPTIONS.map((o) => (
          <option key={o.key} value={o.key}>
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
    </div>
  );
}

// ── Series builder ───────────────────────────────────────────────────

interface ChartPoint {
  date: string;
  value: number;
}

function rangeToDays(range: AssetsTimeRange): number {
  if (range === "7D") return 7;
  if (range === "30D") return 30;
  if (range === "90D") return 90;
  return Number.POSITIVE_INFINITY;
}

function buildAssetSeries(
  dailyPnl: DailyPnl[],
  anchorValue: number,
  range: AssetsTimeRange,
): ChartPoint[] {
  if (!dailyPnl || dailyPnl.length === 0) return [];

  const maxDays = rangeToDays(range);
  // Sort ascending (oldest → newest). The hook already sorts, but defend.
  const sorted = [...dailyPnl].sort((a, b) => a.date.localeCompare(b.date));
  const sliced = Number.isFinite(maxDays) ? sorted.slice(-maxDays) : sorted;

  if (sliced.length === 0) return [];

  // Walk back from anchor: subtract each day's pnl going left.
  // The result is a monotonic-ish series where the last point matches
  // `anchorValue` and each earlier day reflects "value before that day's pnl".
  const fromEnd: ChartPoint[] = [];
  let running = anchorValue;
  for (let i = sliced.length - 1; i >= 0; i--) {
    const day = sliced[i]!;
    fromEnd.push({
      date: formatShortDate(day.date),
      value: Math.max(0, running),
    });
    running -= parse18(day.pnl);
  }
  return fromEnd.reverse();
}

function formatShortDate(iso: string): string {
  // iso is YYYY-MM-DD — keep MM-DD for compact axis.
  return iso.length >= 10 ? iso.slice(5, 10) : iso;
}
