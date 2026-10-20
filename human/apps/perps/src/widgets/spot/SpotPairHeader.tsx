import React, { useState } from "react";
import * as Popover from "@radix-ui/react-popover";
import { cn } from "@/ui/utils/cn";
import { SPOT_MARKETS, spotCoinLogoUrl } from "@/core/spot/config";
import { useSpotOrderbook, useSpotTrades, useSpotStats } from "@/hooks/spot/useSpotMarketStream";
import { ChevronDown } from "lucide-react";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import { SpotMarketWatchlist } from "./SpotMarketWatchlist";

interface SpotPairHeaderProps {
  selectedPair: string;
  onPairChange: (value: string) => void;
}

function formatPrice(price: number): string {
  if (price >= 10000) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

function fmtCompact(val: number): string {
  if (Math.abs(val) >= 1_000_000_000) return `$${(val / 1_000_000_000).toFixed(2)}B`;
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  if (Math.abs(val) >= 1_000) return `$${(val / 1_000).toFixed(2)}K`;
  return `$${val.toFixed(2)}`;
}

function fmtBaseVol(val: number, symbol: string): string {
  if (!Number.isFinite(val) || val <= 0) return "—";
  const abs = Math.abs(val);
  if (abs >= 1_000_000) return `${(val / 1_000_000).toFixed(2)}M ${symbol}`;
  if (abs >= 1_000) return `${(val / 1_000).toFixed(2)}K ${symbol}`;
  return `${val.toFixed(abs >= 1 ? 2 : 4)} ${symbol}`;
}

function fmtChangePct(pct: number): { text: string; positive: boolean } {
  if (!Number.isFinite(pct)) return { text: "—", positive: true };
  return { text: `${pct >= 0 ? "+" : ""}${pct.toFixed(2)}%`, positive: pct >= 0 };
}

function StatChip({
  label,
  value,
  valueClass,
}: {
  label: string;
  value: string;
  valueClass?: string;
}) {
  return (
    <div className="flex flex-col shrink-0">
      <span
        className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground/70"
      >
        {label}
      </span>
      <span className={cn("text-sm font-medium tabular-nums leading-tight", valueClass)}>
        {value}
      </span>
    </div>
  );
}

export function SpotPairHeader({ selectedPair, onPairChange }: SpotPairHeaderProps) {
  const [marketSelectorOpen, setMarketSelectorOpen] = useState(false);

  const symbol = selectedPair.split("/")[0]!;
  const market = SPOT_MARKETS.find((m) => m.symbol === symbol) ?? SPOT_MARKETS[0]!;

  const orderbook = useSpotOrderbook(market.symbol);
  const trades = useSpotTrades(market.symbol);
  const wsStats = useSpotStats(market.symbol);

  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const spread = bestAsk > 0 && bestBid > 0 ? bestAsk - bestBid : 0;
  const spreadBps = midPrice > 0 ? (spread / midPrice) * 10_000 : 0;

  const lastTradePrice = trades[0]?.price ?? 0;
  const livePrice = lastTradePrice || midPrice || (wsStats?.last_price ?? 0);

  const high24h = wsStats?.high_24h ?? 0;
  const low24h = wsStats?.low_24h ?? 0;
  const volume24h = wsStats?.volume_24h ?? 0; // quote (USD)
  // Spot stats does not split base/quote — derive a base-volume estimate
  // from the last trade price when the WS only carries quote volume.
  const baseVolEst = livePrice > 0 ? volume24h / livePrice : 0;
  const change24h = wsStats?.change_24h ?? 0;

  const change = fmtChangePct(change24h);

  return (
    <div className="flex items-center gap-6 px-4 py-2 bg-card border-b border-border overflow-x-auto scrollbar-hide">
      <Popover.Root open={marketSelectorOpen} onOpenChange={setMarketSelectorOpen}>
        <Popover.Trigger asChild>
          <button
            className="flex items-center gap-2 hover:opacity-80 transition-opacity shrink-0"
            aria-label="Open market selector"
          >
            <img
              src={spotCoinLogoUrl(market.symbol)}
              alt={market.symbol}
              className="w-7 h-7 rounded-full object-cover"
            />
            <div className="flex flex-col items-start">
              <span className="text-sm font-semibold whitespace-nowrap leading-tight flex items-center">
                {market.pair}
                <span className="ml-1.5 text-[9px] font-mono uppercase tracking-wider text-primary/70 bg-primary/10 px-1.5 py-0.5 rounded-full">
                  SPOT
                </span>
              </span>
              <span className="text-[9px] text-muted-foreground leading-none">
                {market.name}
              </span>
            </div>
            <ChevronDown className="w-3 h-3 text-muted-foreground" />
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={6}
            className="z-50 w-[640px] max-w-[calc(100vw-32px)] h-[60vh] max-h-[560px] overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface-1 shadow-2xl outline-none"
            onOpenAutoFocus={(e) => e.preventDefault()}
          >
            <SpotMarketWatchlist
              selectedSymbol={market.symbol}
              onSelect={(sym) => {
                onPairChange(`${sym}/USD`);
                setMarketSelectorOpen(false);
              }}
            />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>

      <div className="w-px h-8 bg-border shrink-0" />

      {/* Live last price + change */}
      <div className="shrink-0 flex flex-col items-start">
        {livePrice > 0 ? (
          <PriceFlash
            value={livePrice}
            className="text-xl font-bold tabular-nums text-foreground"
          >
            {`$${formatPrice(livePrice)}`}
          </PriceFlash>
        ) : (
          <span className="text-xl font-bold tabular-nums text-muted-foreground">
            ...
          </span>
        )}
        <span
          className={cn(
            "text-sm font-medium tabular-nums",
            change.positive ? "text-emerald-500" : "text-red-500",
          )}
        >
          {change.text}
        </span>
      </div>

      <div className="w-px h-8 bg-border shrink-0" />

      {/* Bid / Ask / Spread */}
      <div className="flex items-center gap-6 overflow-x-auto scrollbar-hide">
        <StatChip
          label="Best Bid"
          value={bestBid > 0 ? `$${formatPrice(bestBid)}` : "—"}
          valueClass="text-emerald-500"
        />
        <StatChip
          label="Best Ask"
          value={bestAsk > 0 ? `$${formatPrice(bestAsk)}` : "—"}
          valueClass="text-red-500"
        />
        <StatChip
          label="Spread"
          value={
            spread > 0
              ? `${formatPrice(spread)} (${spreadBps.toFixed(2)} bps)`
              : "—"
          }
          valueClass="text-muted-foreground"
        />

        <div className="w-px h-8 bg-border shrink-0" />

        <StatChip
          label="24h High"
          value={high24h > 0 ? `$${formatPrice(high24h)}` : "—"}
          valueClass="text-emerald-500"
        />
        <StatChip
          label="24h Low"
          value={low24h > 0 ? `$${formatPrice(low24h)}` : "—"}
          valueClass="text-red-500"
        />
        <StatChip
          label="24h Volume"
          value={volume24h > 0 ? fmtCompact(volume24h) : "—"}
        />
        <StatChip
          label={`24h Vol (${market.symbol})`}
          value={baseVolEst > 0 ? fmtBaseVol(baseVolEst, market.symbol) : "—"}
          valueClass="text-muted-foreground"
        />
      </div>
    </div>
  );
}
