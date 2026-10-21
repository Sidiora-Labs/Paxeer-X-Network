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
import { parseWei, weiToNumber, fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { cn } from "@/ui/utils/cn";

export type PerformanceRange = "7D" | "30D" | "90D" | "ALL";

interface Props {
  dailyPnl: DailyPnl[];
  isLoading?: boolean;
  /** Set when both PerfDB reads failed; the grid shows an error, never an empty chart. */
  error?: Error | null;
  /** Days PerfDB returned without a usable pnl, volume or trades value. */
  skippedDays?: number;
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
export function PortfolioPerformanceGrid({ dailyPnl, isLoading, error, skippedDays = 0, className }: Props) {
  const [range, setRange] = useState<PerformanceRange>("7D");
  const series = useMemo(() => buildSeries(dailyPnl, range), [dailyPnl, range]);
  const totals = useMemo(() => windowTotals(sliceByRange(dailyPnl, range)), [dailyPnl, range]);
  const failed = Boolean(error);
  const roi = totals?.roiHundredthsPct ?? null;
  const rangeLabel = range === "ALL" ? "All-time" : range;

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

      {failed ? (
        <div role="alert" className="text-[11px] text-red-400">
          Performance history is unavailable right now.
        </div>
      ) : skippedDays > 0 ? (
        <div role="status" className="text-[11px] text-amber-400">
          {skippedDays} {skippedDays === 1 ? "day" : "days"} skipped: PerfDB returned incomplete values.
        </div>
      ) : null}

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <KpiCard
          label={`${rangeLabel} PNL`}
          value={totals && !failed ? weiToNumber(totals.pnlWei) : "—"}
          colorBySign={Boolean(totals) && !failed}
        />
        <KpiCard
          label={`${rangeLabel} Volume`}
          value={totals && !failed ? weiToNumber(totals.volumeWei) : "—"}
          subtitle={totals && !failed ? `${totals.trades.toLocaleString()} trades` : undefined}
        />
        <KpiCard
          label={`${rangeLabel} ROI`}
          value={roi !== null && !failed ? `${roi >= 0n ? "+" : "-"}${formatHundredths(roi < 0n ? -roi : roi)}%` : "—"}
          colorBySign={roi !== null && roi !== 0n && !failed}
          rawForColor={roi !== null && roi < 0n ? -1 : 1}
          subtitle="As % of notional"
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <ChartPanel title="Daily PnL" isLoading={isLoading} failed={failed} empty={series.length === 0}>
          <DailyPnlBars series={series} />
        </ChartPanel>
        <ChartPanel title="Cumulative PnL" isLoading={isLoading} failed={failed} empty={series.length === 0}>
          <CumulativeArea
            series={series}
            valueKey="cumPnl"
            positive={(series.at(-1)?.cumPnl ?? 0) >= 0}
          />
        </ChartPanel>
        <ChartPanel title="Daily Volume" isLoading={isLoading} failed={failed} empty={series.length === 0}>
          <DailyVolumeBars series={series} />
        </ChartPanel>
        <ChartPanel title="Cumulative Volume" isLoading={isLoading} failed={failed} empty={series.length === 0}>
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
  failed,
  empty,
}: {
  title: string;
  children: React.ReactNode;
  isLoading?: boolean;
  failed?: boolean;
  empty?: boolean;
}) {
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] p-3 md:p-4">
      <div className="text-xs font-semibold text-foreground mb-2">{title}</div>
      <div className="h-44 md:h-48">
        {isLoading && empty ? (
          <div className="h-full w-full rounded bg-white/[0.02] animate-pulse" />
        ) : failed ? (
          <div className="h-full flex items-center justify-center text-[11px] text-red-400">
            Could not load data
          </div>
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

const RANGE_DAYS: Record<Exclude<PerformanceRange, "ALL">, number> = { "7D": 7, "30D": 30, "90D": 90 };
const DAY_MS = 86_400_000;

/** First UTC day key (YYYY-MM-DD) of the last N calendar UTC days including today, the gateway's day boundary. */
export function windowStartKey(range: Exclude<PerformanceRange, "ALL">, now: Date = new Date()): string {
  const todayUtc = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  return new Date(todayUtc - (RANGE_DAYS[range] - 1) * DAY_MS).toISOString().slice(0, 10);
}

/** Rows on the last N calendar UTC days including today; ALL keeps every row. */
export function sliceByRange(dailyPnl: DailyPnl[], range: PerformanceRange, now: Date = new Date()): DailyPnl[] {
  if (!dailyPnl || dailyPnl.length === 0) return [];
  const sorted = [...dailyPnl].sort((a, b) => a.date.localeCompare(b.date));
  if (range === "ALL") return sorted;
  const start = windowStartKey(range, now);
  return sorted.filter((d) => d.date.slice(0, 10) >= start);
}

export interface WindowTotals {
  pnlWei: bigint;
  volumeWei: bigint;
  trades: number;
  /** PnL as a percentage of volume in hundredths of a percent, truncated toward zero; null without volume. */
  roiHundredthsPct: bigint | null;
}

/** Exact sums over the 18-decimal strings; null for an empty window. */
export function windowTotals(rows: DailyPnl[]): WindowTotals | null {
  if (rows.length === 0) return null;
  let pnlWei = 0n;
  let volumeWei = 0n;
  let trades = 0;
  for (const d of rows) {
    pnlWei += parseWei(d.pnl);
    volumeWei += parseWei(d.volume);
    trades += d.trades;
  }
  return { pnlWei, volumeWei, trades, roiHundredthsPct: volumeWei > 0n ? (pnlWei * 10_000n) / volumeWei : null };
}

function formatHundredths(v: bigint): string {
  return `${v / 100n}.${(v % 100n).toString().padStart(2, "0")}`;
}

export function buildSeries(dailyPnl: DailyPnl[], range: PerformanceRange, now: Date = new Date()): SeriesPoint[] {
  const rows = sliceByRange(dailyPnl, range, now);
  if (rows.length === 0) return [];

  let cumPnl = 0n;
  let cumVol = 0n;
  return rows.map((d) => {
    const pnl = parseWei(d.pnl);
    const volume = parseWei(d.volume);
    cumPnl += pnl;
    cumVol += volume;
    return {
      date: d.date.length >= 10 ? d.date.slice(5, 10) : d.date,
      pnl: weiToNumber(pnl),
      cumPnl: weiToNumber(cumPnl),
      volume: weiToNumber(volume),
      cumVolume: weiToNumber(cumVol),
    };
  });
}
