"use client";

import { SPOT_MARKETS, spotCoinLogoUrl } from "@/core/spot/config";
import { useSpotStats } from "@/hooks/spot/useSpotMarketStream";
import { SectionCard } from "./PortfolioPrimitives";

interface PortfolioSpotSectionProps {
  title: string;
  emptyMessage: string;
}

export function PortfolioSpotSection({ title, emptyMessage }: PortfolioSpotSectionProps) {
  return (
    <SectionCard title={title}>
      <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
        <div
          className="hidden md:grid items-center px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground border-b border-white/[0.04]"
          style={{ gridTemplateColumns: "minmax(140px,2fr) 1fr 1fr 1fr" }}
        >
          <span>Market</span>
          <span className="text-right">Last Price</span>
          <span className="text-right">24h Change</span>
          <span className="text-right">24h Volume</span>
        </div>
        <div className="divide-y divide-white/[0.04]">
          {SPOT_MARKETS.map((market) => (
            <SpotRow key={market.symbol} symbol={market.symbol} pair={market.pair} name={market.name} />
          ))}
        </div>
        <div className="px-4 py-3 text-xs text-muted-foreground/60 text-center">
          {emptyMessage}
        </div>
      </div>
    </SectionCard>
  );
}

function SpotRow({ symbol, pair, name }: { symbol: string; pair: string; name: string }) {
  const stats = useSpotStats(symbol);
  const change = stats?.change_24h ?? 0;
  const positive = change >= 0;

  return (
    <div
      className="grid items-center gap-3 px-4 py-3 hover:bg-white/[0.03] transition-colors"
      style={{ gridTemplateColumns: "minmax(140px,2fr) 1fr 1fr 1fr" }}
    >
      <div className="flex items-center gap-2 min-w-0">
        <img
          src={spotCoinLogoUrl(symbol)}
          alt={symbol}
          className="w-5 h-5 rounded-full"
          onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }}
        />
        <span className="text-sm font-semibold text-foreground truncate">{pair}</span>
        <span className="text-[10px] text-muted-foreground truncate">{name}</span>
      </div>
      <span className="text-xs font-mono tabular-nums text-foreground text-right">
        {stats?.last_price
          ? `$${stats.last_price.toLocaleString("en-US", { maximumFractionDigits: 4 })}`
          : "—"}
      </span>
      <span className={`text-xs font-mono tabular-nums text-right ${positive ? "text-emerald-400" : "text-red-400"}`}>
        {change !== 0 ? `${positive ? "+" : ""}${(change * 100).toFixed(2)}%` : "—"}
      </span>
      <span className="text-xs font-mono tabular-nums text-muted-foreground text-right">
        {stats?.volume_24h && stats.volume_24h > 0
          ? `$${(stats.volume_24h / 1e6).toFixed(2)}M`
          : "—"}
      </span>
    </div>
  );
}
