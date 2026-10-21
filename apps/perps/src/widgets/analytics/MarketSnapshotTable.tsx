"use client";

import { Skeleton } from "@/ui/atoms/skeleton";
import {
  PERPS_MARKETS,
  ANALYTICS_AXIS_FORMATTERS,
  TRACK_B_STRINGS,
} from "@/core/perps/config";
import { useMarketSnapshots } from "@/hooks/perps/useAnalytics";
import { formatSignedFundingDecimalPct, fundingDecimalSign, fundingWadDecimal, parseFundingRateWad } from "@/core/perps/fundingRate";

/** The subgraph snapshot's on-chain 24h funding figure, never the venue rate. */
export const FUNDING_LABEL_ONCHAIN_24H = "funding paid (24h, on-chain)";

interface MarketSnapshotTableProps {
  marketId: number | undefined;
}

function parse18(val: string | null | undefined): number {
  if (!val) return 0;
  return parseFloat(val) / 1e18;
}

function fmtPrice(price: number): string {
  if (price >= 10000) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

function getMarketSymbol(marketId: number): string {
  return PERPS_MARKETS.find((m) => m.marketId === marketId)?.symbol ?? `MKT-${marketId}`;
}

export function MarketSnapshotTable({ marketId }: MarketSnapshotTableProps) {
  const { data, isLoading } = useMarketSnapshots(marketId, 20);

  if (isLoading || !data) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.MARKET_SNAPSHOT_TABLE_TITLE}
        </h3>
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (data.length === 0) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.MARKET_SNAPSHOT_TABLE_TITLE}
        </h3>
        <div className="py-8 text-center text-sm text-muted-foreground">
          No snapshot data available
        </div>
      </div>
    );
  }

  const symbol = marketId !== undefined ? getMarketSymbol(marketId) : "All";

  return (
    <div className="rounded-card bg-surface-1 overflow-hidden shadow-04 transition-colors ease-data duration-200">
      <div className="flex items-center justify-between gap-3 px-5 py-3 border-b border-foreground/10">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/70">
          {TRACK_B_STRINGS.MARKET_SNAPSHOT_TABLE_TITLE}
        </h3>
        <span className="font-mono text-xs-label uppercase tracking-label text-foreground/50 tabular-nums">
          {symbol}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-foreground/40 border-b border-foreground/10">
              <th className="text-left px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">Time</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">Mark Price</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">Index Price</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">Long OI</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">Short OI</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">24h Volume</th>
              <th className="text-right px-4 py-2.5 font-mono text-xs-label uppercase tracking-label">{FUNDING_LABEL_ONCHAIN_24H}</th>
            </tr>
          </thead>
          <tbody>
            {data.slice(0, 20).map((s) => {
              const fundingRate = fundingWadDecimal(parseFundingRateWad(s.fundingRate24h));
              const fundingSign = fundingDecimalSign(fundingRate);
              return (
                <tr key={s.id} className="border-b border-foreground/5 hover:bg-foreground/[0.03] transition-colors ease-data duration-150">
                  <td className="px-4 py-2 text-muted-foreground">
                    {ANALYTICS_AXIS_FORMATTERS.shortDate(s.blockTimestamp)}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">
                    ${fmtPrice(parse18(s.markPrice))}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">
                    ${fmtPrice(parse18(s.indexPrice))}
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-long">
                    {ANALYTICS_AXIS_FORMATTERS.compactUsd(parse18(s.longOi))}
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-short">
                    {ANALYTICS_AXIS_FORMATTERS.compactUsd(parse18(s.shortOi))}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">
                    {ANALYTICS_AXIS_FORMATTERS.compactUsd(parse18(s.volume24hUsd))}
                  </td>
                  <td className={`px-4 py-2 text-right font-mono ${fundingSign === 1 ? "text-long" : fundingSign === -1 ? "text-short" : "text-muted-foreground"}`}>
                    {formatSignedFundingDecimalPct(fundingRate)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
