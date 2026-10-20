"use client";

import React, { useMemo, useState } from "react";
import {
  Bar,
  BarChart,
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  Cell,
} from "recharts";
import { ChevronDown } from "lucide-react";
import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { parse18, fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { cn } from "@/ui/utils/cn";

export type PerformanceRange = "7D" | "30D" | "90D" | "ALL";

interface Props {
  dailyPnl: DailyPnl[];
  isLoading?: boolean;
  className?: string;
}

interface SeriesPoint {
  date: string;
  pnl: number;
  cumPnl: number;
  volume: number;
  cumVolume: number;
}

/**
 * The reference design's 4-chart performance grid.
 *
 * Renders 3 headline KPIs (PnL, Volume, ROI) above a 2x2 grid of:
 *  - Daily PnL           (bar, coloured by sign)
 *  - Cumulative PnL      (area, tinted green/red by endpoint)
 *  - Daily Volume        (bar)
 *  - Cumulative Volume   (area)
 *
 * All four charts share a single time-range dropdown.
 */
export function PortfolioPerformanceGrid({ dailyPnl, isLoading, className }: Props) {
  const [range, setRange] = useState<PerformanceRange>("7D");
  const series = useMemo(() => buildSeries(dailyPnl, range), [dailyPnl, range]);

  const sums = useMemo(() => {
    const pnl = series.reduce((s, p) => s + p.pnl, 0);
    const volume = series.reduce((s, p) => s + p.volume, 0);
    const trades = sliceByRange(dailyPnl, range).reduce(
      (s, d) => s + (d.trades ?? 0),
      0,
    );
    const roi = volume > 0 ? (pnl / volume) * 100 : null;
    return { pnl, volume, trades, roi };
  }, [series, dailyPnl, range]);

  return (
    <section
      className={cn(
        "rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] p-4 md:p-5 space-y-4",
        className,
      )}
    >
      <header className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold text-foreground">Performance</h3>
          <span className="hidden sm:inline-flex items-center gap-1.5 px-2 py-[3px] rounded-[var(--radius-pill)] text-[10px] font-mono uppercase tracking-widest bg-emerald-500/10 text-emerald-400">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
            Live
          </span>
        </div>
        <RangePicker value={range} onChange={setRange} />
      </header>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <KpiCard
          label={`${range === "ALL" ? "All-time" : range} PNL`}
          value={sums.pnl}
          colorBySign
        />
        <KpiCard
          label={`${range === "ALL" ? "All-time" : range} Volume`}
          value={sums.volume}
          subtitle={`${sums.trades.toLocaleString()} trades`}
        />
        <KpiCard
          label={`${range === "ALL" ? "All-time" : range} ROI`}
          value={sums.roi !== null ? `${sums.roi >= 0 ? "+" : ""}${sums.roi.toFixed(2)}%` : "—"}
          colorBySign={sums.roi !== null && sums.roi !== 0}
          rawForColor={sums.roi ?? 0}
          subtitle="As % of notional"
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <ChartPanel title="Daily PnL" isLoading={isLoading} empty={series.length === 0}>
          <DailyPnlBars series={series} />
        </ChartPanel>
        <ChartPanel title="Cumulative PnL" isLoading={isLoading} empty={series.length === 0}>
          <CumulativeArea
            series={series}
            valueKey="cumPnl"
            positive={(series.at(-1)?.cumPnl ?? 0) >= 0}
          />
        </ChartPanel>
        <ChartPanel title="Daily Volume" isLoading={isLoading} empty={series.length === 0}>
          <DailyVolumeBars series={series} />
        </ChartPanel>
        <ChartPanel title="Cumulative Volume" isLoading={isLoading} empty={series.length === 0}>
          <CumulativeArea series={series} valueKey="cumVolume" positive />
        </ChartPanel>
      </div>
    </section>
  );
}

// ── Range picker (matches the Assets chart styling) ──────────────────

const RANGE_OPTIONS: Array<{ key: PerformanceRange; label: string }> = [
  { key: "7D", label: "7D" },
  { key: "30D", label: "30D" },
  { key: "90D", label: "90D" },
  { key: "ALL", label: "ALL" },
];

function RangePicker({
  value,
  onChange,
}: {
  value: PerformanceRange;
  onChange: (v: PerformanceRange) => void;
}) {
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as PerformanceRange)}
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

// ── KPI card ─────────────────────────────────────────────────────────

function KpiCard({
  label,
  value,
  subtitle,
  colorBySign,
  rawForColor,
}: {
  label: string;
  value: number | string;
  subtitle?: string;
  colorBySign?: boolean;
  rawForColor?: number;
}) {
  const displayValue =
    typeof value === "number" ? `${value >= 0 ? "+" : "-"}${fmtUsd(Math.abs(value))}` : value;
  const raw = typeof value === "number" ? value : rawForColor ?? 0;
  const positive = raw >= 0;
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] px-4 py-3">
      <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-2xl font-mono font-semibold tabular-nums",
          colorBySign
            ? positive
              ? "text-emerald-400"
              : "text-red-400"
            : "text-foreground",
        )}
      >
        {displayValue}
      </div>
      {subtitle ? (
        <div className="mt-0.5 text-[10px] font-mono text-muted-foreground/80 tabular-nums">
          {subtitle}
        </div>
      ) : null}
    </div>
  );
}

// ── Chart panel (shared frame) ───────────────────────────────────────

function ChartPanel({
  title,
  children,
  isLoading,
  empty,
}: {
  title: string;
  children: React.ReactNode;
  isLoading?: boolean;
  empty?: boolean;
}) {
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] p-3 md:p-4">
      <div className="text-xs font-semibold text-foreground mb-2">{title}</div>
      <div className="h-44 md:h-48">
        {isLoading && empty ? (
          <div className="h-full w-full rounded bg-white/[0.02] animate-pulse" />
        ) : empty ? (
          <div className="h-full flex items-center justify-center text-[11px] text-muted-foreground">
            No data yet
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            {children as React.ReactElement}
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}

// ── Chart implementations ────────────────────────────────────────────

function DailyPnlBars({ series }: { series: SeriesPoint[] }) {
  return (
    <BarChart data={series} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
      <CartesianGrid stroke="rgba(255,255,255,0.04)" strokeDasharray="3 3" vertical={false} />
      <XAxis dataKey="date" tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }} axisLine={false} tickLine={false} minTickGap={20} />
      <YAxis
        tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
        axisLine={false}
        tickLine={false}
        width={48}
        tickFormatter={compactTick}
      />
      <Tooltip content={<PnlTooltip />} />
      <Bar dataKey="pnl" radius={[3, 3, 0, 0]}>
        {series.map((p, idx) => (
          <Cell key={idx} fill={p.pnl >= 0 ? "var(--brand-green)" : "var(--brand-red)"} />
        ))}
      </Bar>
    </BarChart>
  );
}

function DailyVolumeBars({ series }: { series: SeriesPoint[] }) {
  return (
    <BarChart data={series} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
      <CartesianGrid stroke="rgba(255,255,255,0.04)" strokeDasharray="3 3" vertical={false} />
      <XAxis dataKey="date" tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }} axisLine={false} tickLine={false} minTickGap={20} />
      <YAxis
        tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
        axisLine={false}
        tickLine={false}
        width={48}
        tickFormatter={compactTick}
      />
      <Tooltip content={<VolumeTooltip />} />
      <Bar dataKey="volume" fill="var(--brand-blue)" radius={[3, 3, 0, 0]} />
    </BarChart>
  );
}

function CumulativeArea({
  series,
  valueKey,
  positive,
}: {
  series: SeriesPoint[];
  valueKey: "cumPnl" | "cumVolume";
  positive: boolean;
}) {
  const color = positive ? "var(--brand-green)" : "var(--brand-red)";
  const gradId = `cum-${valueKey}-${positive ? "pos" : "neg"}`;
  return (
    <AreaChart data={series} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity={0.32} />
          <stop offset="100%" stopColor={color} stopOpacity={0} />
        </linearGradient>
      </defs>
      <CartesianGrid stroke="rgba(255,255,255,0.04)" strokeDasharray="3 3" vertical={false} />
      <XAxis
        dataKey="date"
        tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
        axisLine={false}
        tickLine={false}
        minTickGap={20}
      />
      <YAxis
        tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
        axisLine={false}
        tickLine={false}
        width={48}
        tickFormatter={compactTick}
      />
      <Tooltip
        contentStyle={{
          background: "hsl(var(--popover))",
          border: "1px solid hsl(var(--border))",
          borderRadius: 8,
          fontSize: 12,
        }}
        labelStyle={{ color: "hsl(var(--muted-foreground))" }}
        formatter={(value: number) => [fmtUsd(value), valueKey === "cumPnl" ? "Cumulative PnL" : "Cumulative Volume"]}
      />
      <Area
        type="monotone"
        dataKey={valueKey}
        stroke={color}
        strokeWidth={2}
        fill={`url(#${gradId})`}
        isAnimationActive={false}
      />
    </AreaChart>
  );
}

// ── Tooltips ─────────────────────────────────────────────────────────

function PnlTooltip({ active, payload, label }: any) {
  if (!active || !payload || payload.length === 0) return null;
  const v: number = payload[0].value;
  return (
    <div className="rounded-md bg-popover border border-border px-2 py-1.5 text-xs shadow-lg">
      <div className="text-muted-foreground">{label}</div>
      <div className={cn("font-mono tabular-nums", v >= 0 ? "text-emerald-400" : "text-red-400")}>
        {v >= 0 ? "+" : ""}
        {fmtUsd(v)}
      </div>
    </div>
  );
}

function VolumeTooltip({ active, payload, label }: any) {
  if (!active || !payload || payload.length === 0) return null;
  const v: number = payload[0].value;
  return (
    <div className="rounded-md bg-popover border border-border px-2 py-1.5 text-xs shadow-lg">
      <div className="text-muted-foreground">{label}</div>
      <div className="font-mono tabular-nums text-foreground">{fmtUsd(v)}</div>
    </div>
  );
}

function compactTick(v: number): string {
  const abs = Math.abs(v);
  if (abs >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (abs >= 1_000) return `${(v / 1_000).toFixed(1)}K`;
  return v.toFixed(0);
}

// ── Series prep helpers ──────────────────────────────────────────────

export function sliceByRange(dailyPnl: DailyPnl[], range: PerformanceRange): DailyPnl[] {
  if (!dailyPnl || dailyPnl.length === 0) return [];
  const sorted = [...dailyPnl].sort((a, b) => a.date.localeCompare(b.date));
  if (range === "ALL") return sorted;
  const n = range === "7D" ? 7 : range === "30D" ? 30 : 90;
  return sorted.slice(-n);
}

export function buildSeries(dailyPnl: DailyPnl[], range: PerformanceRange): SeriesPoint[] {
  const rows = sliceByRange(dailyPnl, range);
  if (rows.length === 0) return [];

  let cumPnl = 0;
  let cumVol = 0;
  return rows.map((d) => {
    const pnl = parse18(d.pnl);
    const volume = parse18(d.volume);
    cumPnl += pnl;
    cumVol += volume;
    return {
      date: d.date.length >= 10 ? d.date.slice(5, 10) : d.date,
      pnl,
      cumPnl,
      volume,
      cumVolume: cumVol,
    };
  });
}
