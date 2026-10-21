"use client";

import { useMemo } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceLine,
} from "recharts";
import { Skeleton } from "@/ui/atoms/skeleton";
import { useTradeSettlements } from "@/hooks/perps/useAnalytics";
import {
  PERPS_MARKETS,
  TRADE_TYPE_LABELS,
  ANALYTICS_AXIS_FORMATTERS,
  ANALYTICS_CHART_COLORS,
  TRACK_B_STRINGS,
} from "@/core/perps/config";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { fmtUsdCompact as fmtUsd, parse18, fmtDate, weiOrZero, weiToNumber } from "@/ui/utils/format";

interface TradeSettlementHistoryProps {
  userAddress: string | undefined;
}

function getMarketSymbol(marketId: number): string {
  return PERPS_MARKETS.find((m) => m.marketId === marketId)?.symbol ?? `MKT-${marketId}`;
}

/** Magnitude of a wei string, taken exactly before the display edge. */
export function absUsd(v: string | null | undefined): number {
  const wei = weiOrZero(v);
  return weiToNumber(wei < 0n ? -wei : wei);
}

export function TradeSettlementHistory({ userAddress }: TradeSettlementHistoryProps) {
  const isMobile = useIsMobile();
  const { data, isLoading } = useTradeSettlements(
    userAddress ? { userAddress } : undefined
  );

  const chartData = useMemo(() => {
    if (!data || data.length === 0) return [];
    return data.slice(0, 20).map((s) => ({
      label: `${getMarketSymbol(s.marketId)} #${s.positionId.slice(0, 6)}`,
      grossPnl: parse18(s.grossPnl),
      fees: -absUsd(s.totalFeesUsd),
      borrowing: -absUsd(s.borrowingFeeUsd),
      funding: -absUsd(s.fundingPaidUsd),
      netPayout: parse18(s.netPayoutTokens),
    }));
  }, [data]);

  if (!userAddress) {
    return (
      <div className="py-12 text-center text-sm text-muted-foreground">
        Connect wallet to view settlements
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="w-full" style={{ height: isMobile ? 200 : 300 }} />
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (!data || data.length === 0) {
    return (
      <div className="py-12 text-center text-sm text-muted-foreground">
        {TRACK_B_STRINGS.SETTLEMENTS_EMPTY}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Waterfall chart */}
      {chartData.length > 0 && (
        <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5">
          <h3 className="text-sm font-medium text-muted-foreground mb-3">
            Settlement Breakdown
          </h3>
          <div style={{ width: "100%", height: isMobile ? 220 : 320 }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
                <XAxis
                  dataKey="label"
                  tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 9 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                  width={52}
                  tickFormatter={ANALYTICS_AXIS_FORMATTERS.compactUsd}
                />
                <ReferenceLine y={0} stroke="hsl(var(--border))" strokeDasharray="3 3" />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  labelStyle={{ color: "hsl(var(--muted-foreground))" }}
                  formatter={(value: number, name: string) => [fmtUsd(value), name]}
                />
                <Legend />
                <Bar dataKey="grossPnl" name="Gross PnL" fill={ANALYTICS_CHART_COLORS.markPrice} stackId="a" />
                <Bar dataKey="fees" name="Fees" fill={ANALYTICS_CHART_COLORS.oiShort} stackId="a" />
                <Bar dataKey="borrowing" name="Borrowing" fill={ANALYTICS_CHART_COLORS.indexPrice} stackId="a" />
                <Bar dataKey="funding" name="Funding" fill={ANALYTICS_CHART_COLORS.funding} stackId="a" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Detail table */}
      <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-muted-foreground border-b border-white/[0.06]">
                <th className="text-left px-4 py-2.5 font-medium">Market</th>
                <th className="text-left px-4 py-2.5 font-medium">Type</th>
                <th className="text-right px-4 py-2.5 font-medium">Size</th>
                <th className="text-right px-4 py-2.5 font-medium">Gross PnL</th>
                <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">Fees</th>
                <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">Borrowing</th>
                <th className="text-right px-4 py-2.5 font-medium hidden lg:table-cell">Funding</th>
                <th className="text-right px-4 py-2.5 font-medium">Net Payout</th>
                <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">Time</th>
              </tr>
            </thead>
            <tbody>
              {data.map((s) => {
                const grossPnl = parse18(s.grossPnl);
                const netPayout = parse18(s.netPayoutTokens);
                return (
                  <tr key={s.id} className="border-b border-white/[0.06]/50 hover:bg-muted/20">
                    <td className="px-4 py-2 font-medium">{getMarketSymbol(s.marketId)}/USD</td>
                    <td className="px-4 py-2 text-muted-foreground capitalize">
                      {TRADE_TYPE_LABELS[s.tradeType] ?? s.tradeTypeName ?? `Type ${s.tradeType}`}
                    </td>
                    <td className="px-4 py-2 text-right font-mono">{fmtUsd(parse18(s.sizeUsd))}</td>
                    <td className={`px-4 py-2 text-right font-mono ${grossPnl >= 0 ? "text-emerald-500" : "text-red-500"}`}>
                      {fmtUsd(grossPnl)}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-red-400 hidden md:table-cell">
                      -{fmtUsd(absUsd(s.totalFeesUsd))}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-red-400 hidden md:table-cell">
                      -{fmtUsd(absUsd(s.borrowingFeeUsd))}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-red-400 hidden lg:table-cell">
                      -{fmtUsd(absUsd(s.fundingPaidUsd))}
                    </td>
                    <td className={`px-4 py-2 text-right font-mono font-semibold ${netPayout >= 0 ? "text-emerald-500" : "text-red-500"}`}>
                      {fmtUsd(netPayout)}
                    </td>
                    <td className="px-4 py-2 text-right text-muted-foreground hidden md:table-cell">
                      {fmtDate(s.blockTimestamp)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
