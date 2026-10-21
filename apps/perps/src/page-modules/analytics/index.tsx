import { useState, useMemo } from "react";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { PERPS_MARKETS, TRACK_B_STRINGS } from "@/core/perps/config";
import { OiChart } from "@/widgets/analytics/OiChart";
import { MarkPriceChart } from "@/widgets/analytics/MarkPriceChart";
import { ProtocolTvlChart } from "@/widgets/analytics/ProtocolTvlChart";
import { FundingHistoryChart } from "@/widgets/analytics/FundingHistoryChart";
import { MarketSnapshotTable } from "@/widgets/analytics/MarketSnapshotTable";
import { AnalyticsLiquidStat } from "@/widgets/analytics/AnalyticsLiquidStat";
import { GasWeather } from "@/ui/tokens/GasWeather";

export default function AnalyticsPage() {
  const isMobile = useIsMobile();
  const [selectedMarketId, setSelectedMarketId] = useState<number>(PERPS_MARKETS[0]?.marketId ?? 0);

  const marketOptions = useMemo(
    () => PERPS_MARKETS.map((m) => ({ value: m.marketId, label: `${m.symbol}/USD` })),
    []
  );

  return (
    <div className="flex flex-col h-full bg-background text-foreground overflow-x-hidden overflow-y-hidden">
      <main className="flex-1 min-h-0 min-w-0 overflow-y-auto overflow-x-hidden max-w-6xl mx-auto w-full p-4 md:p-6">
        {/* Meta strip — design-port signature (mono 10px, 0.14em tracking) */}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1 border-b border-foreground/10 pb-3 mb-6 font-mono text-xs-label uppercase tracking-label text-foreground/50">
          <span className="flex items-center gap-2 text-foreground/80">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-long animate-pulse" aria-hidden />
            Analytics · Live
          </span>
          <span className="hidden md:inline text-foreground/20">/</span>
          <span>Per-Market &amp; Protocol-Wide</span>
          <span className="ml-auto hidden md:inline text-foreground/40">HyperPax · Paxeer Network</span>
        </div>

        {/* Page Header */}
        <div className="mb-6">
          <h1 className="text-display-xs font-semibold text-foreground">
            {TRACK_B_STRINGS.ANALYTICS_TITLE}
          </h1>
          <p className="mt-2 font-mono text-xs-label uppercase tracking-label text-foreground/50">
            {TRACK_B_STRINGS.ANALYTICS_SUBTITLE}
          </p>
        </div>

        {/* Market selector */}
        <div className="mb-6 flex flex-col gap-2">
          <label
            htmlFor="analytics-market"
            className="font-mono text-xs-label uppercase tracking-label text-foreground/50"
          >
            Market
          </label>
          <select
            id="analytics-market"
            className="rounded-card bg-surface-2 border border-foreground/10 px-3 py-2 text-sm font-mono tabular-nums shadow-04 focus:outline-none focus:ring-2 focus:ring-long/30 transition ease-data duration-150 w-full sm:w-auto sm:min-w-[200px]"
            value={selectedMarketId}
            onChange={(e) => setSelectedMarketId(Number(e.target.value))}
          >
            {marketOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </div>

        {/* Protocol hero row — LiquidStat (cycles TVL/Vol/Markets) + live gas weather */}
        <div className="mb-6 flex flex-wrap items-start gap-4">
          <AnalyticsLiquidStat />
          <GasWeather />
        </div>

        {/* Charts grid */}
        <div className={isMobile ? "space-y-4" : "grid grid-cols-2 gap-4"}>
          <OiChart marketId={selectedMarketId} isMobile={isMobile} />
          <MarkPriceChart marketId={selectedMarketId} isMobile={isMobile} />
          <ProtocolTvlChart isMobile={isMobile} />
          <FundingHistoryChart marketId={selectedMarketId} isMobile={isMobile} />
        </div>

        {/* Market Snapshot Table */}
        <div className="mt-6">
          <MarketSnapshotTable marketId={selectedMarketId} />
        </div>
      </main>
    </div>
  );
}
