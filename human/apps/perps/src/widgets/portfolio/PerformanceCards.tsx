"use client";

import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { parse18 } from "@/ui/utils/format";
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

export function PerformanceCards({
  dailyPnl,
  isLoading,
}: PerformanceCardsProps) {
  // ── Computed values — real data from PerfDB `userDailyPnl` ─────
  const last7 = dailyPnl.slice(-7);
  const prev7 = dailyPnl.slice(-14, -7);

  const sevenDayPnl = last7.reduce((sum, d) => sum + parse18(d.pnl), 0);
  const prevSevenDayPnl = prev7.reduce((sum, d) => sum + parse18(d.pnl), 0);
  const sevenDayVolume = last7.reduce((sum, d) => sum + parse18(d.volume), 0);
  const sevenDayTrades = last7.reduce((sum, d) => sum + (d.trades ?? 0), 0);

  // ROI: 7D PnL as percent of notional turned over this week (matches existing semantics).
  const sevenDayRoiPct = sevenDayVolume > 0 ? (sevenDayPnl / sevenDayVolume) * 100 : null;
  const sevenDayRoiAnnualizedPct = sevenDayRoiPct !== null ? sevenDayRoiPct * 52 : null;

  // Week-over-week delta as percent of prev week's absolute PnL.
  const pnlVsPrevPct =
    prev7.length === 7 && Math.abs(prevSevenDayPnl) > 0.0001
      ? ((sevenDayPnl - prevSevenDayPnl) / Math.abs(prevSevenDayPnl)) * 100
      : null;

  const dailyPnls = last7.map((d) => parse18(d.pnl));
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
