"use client";

import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { weiOrZero, weiPct, weiToNumber } from "@/ui/utils/format";
import { Performance } from "@/ui/tokens/Performance";

interface PerformanceCardsProps {
  dailyPnl: DailyPnl[];
  isLoading: boolean;
}

// Stable weekday labels so SSR and client agree.
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function dateToWeekday(iso: string): string {
  // iso is YYYY-MM-DD. Parse as UTC-noon to avoid TZ flip.
  const d = new Date(`${iso}T12:00:00Z`);
  if (Number.isNaN(d.getTime())) return "";
  return WEEKDAYS[d.getUTCDay()] ?? "";
}

/** 0.0001 USD in wei: below this the previous week is treated as flat. */
const MIN_PREV_WEI = 10n ** 14n;

/** Exact wei sums of the last seven rows and the seven before them. */
export function sevenDayTotals(dailyPnl: DailyPnl[]): { pnlWei: bigint; prevPnlWei: bigint; volumeWei: bigint } {
  const sum = (rows: DailyPnl[], key: "pnl" | "volume") => rows.reduce((acc, d) => acc + weiOrZero(d[key]), 0n);
  const last7 = dailyPnl.slice(-7);
  return { pnlWei: sum(last7, "pnl"), prevPnlWei: sum(dailyPnl.slice(-14, -7), "pnl"), volumeWei: sum(last7, "volume") };
}

export function PerformanceCards({
  dailyPnl,
  isLoading,
}: PerformanceCardsProps) {
  // ── Computed values — real data from PerfDB `userDailyPnl` ─────
  const last7 = dailyPnl.slice(-7);
  const prev7 = dailyPnl.slice(-14, -7);
  const t = sevenDayTotals(dailyPnl);

  const sevenDayPnl = weiToNumber(t.pnlWei);
  const sevenDayVolume = weiToNumber(t.volumeWei);
  const sevenDayTrades = last7.reduce((sum, d) => sum + (d.trades ?? 0), 0);

  // ROI: 7D PnL as percent of notional turned over this week (matches existing semantics).
  const sevenDayRoiPct = t.volumeWei > 0n ? weiPct(t.pnlWei, t.volumeWei) : null;
  const sevenDayRoiAnnualizedPct = sevenDayRoiPct !== null ? sevenDayRoiPct * 52 : null;

  // Week-over-week delta as percent of prev week's absolute PnL.
  const prevAbs = t.prevPnlWei < 0n ? -t.prevPnlWei : t.prevPnlWei;
  const pnlVsPrevPct =
    prev7.length === 7 && prevAbs > MIN_PREV_WEI ? weiPct(t.pnlWei - t.prevPnlWei, prevAbs) : null;

  const dailyPnls = last7.map((d) => weiToNumber(weiOrZero(d.pnl)));
  const dailyLabels = last7.map((d) => dateToWeekday(d.date));

  // ── Loading state — keep section shape so layout doesn't jump ──
  if (isLoading && dailyPnl.length === 0) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          Performance · Live
        </h3>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="rounded-[12px] border border-foreground/10 bg-surface-0 h-[74px] animate-pulse" />
          ))}
        </div>
        <div className="mt-3 h-[122px] rounded-[12px] border border-foreground/10 bg-surface-0 animate-pulse" />
      </div>
    );
  }

  // ── Rendered state — delegates to the design-port `Performance` primitive ──
  return (
    <div className="rounded-card bg-surface-1 p-5 shadow-04">
      <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
        Performance · Live
      </h3>
      <Performance
        sevenDPnl={sevenDayPnl}
        sevenDPnlVsPrevPct={pnlVsPrevPct}
        sevenDVolume={sevenDayVolume}
        sevenDTrades={sevenDayTrades}
        sevenDRoiPct={sevenDayRoiPct}
        sevenDRoiAnnualizedPct={sevenDayRoiAnnualizedPct}
        dailyPnls={dailyPnls}
        dailyLabels={dailyLabels}
      />
    </div>
  );
}
