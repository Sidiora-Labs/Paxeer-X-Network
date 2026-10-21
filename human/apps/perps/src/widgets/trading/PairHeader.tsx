import React, { useState } from "react";
import * as Popover from "@radix-ui/react-popover";
import { cn } from "@/ui/utils/cn";
import { PERPS_MARKETS, coinLogoUrl } from "@/core/perps/config";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useEnrichedMarketStats } from "@/hooks/perps/useEnrichedStats";
import { useSidPaxOrderbook, useSidPaxTrades, useSidPaxStats } from "@/hooks/perps/useSidPaxMarketStream";
import { marketFundingHourly } from "@/hooks/perps/useMarketsStream";
import { FUNDING_LABEL_1H, FUNDING_LABEL_24H, fundingFigures } from "@/core/perps/fundingRate";
import { ChevronDown } from "lucide-react";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import { MarketWatchlist } from "./MarketSelectorPanel";

interface PairHeaderProps {
  selectedPair: string;
  onPairChange: (value: string) => void;
  onOpenMarketPanel?: () => void;
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

function fmtPercent(val: number): string {
  const pct = val * 100;
  return `${pct >= 0 ? "+" : ""}${pct.toFixed(4)}%`;
}

function fmtBps(bps: number): string {
  return `${bps >= 0 ? "+" : ""}${bps.toFixed(2)} bps`;
}

function fmtRatio(val: number): string {
  return val.toFixed(2);
}

function fmtCountdown(ms: number): string {
  if (!ms || ms <= 0) return "—";
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

function fmtChangePct(open: number, close: number): { text: string; positive: boolean } {
  if (open <= 0) return { text: "\u2014", positive: true };
  const pct = ((close - open) / open) * 100;
  return { text: `${pct >= 0 ? "+" : ""}${pct.toFixed(2)}%`, positive: pct >= 0 };
}

function StatChip({ label, value, valueClass }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="flex flex-col shrink-0">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className={cn("text-sm font-medium tabular-nums", valueClass)}>{value}</span>
    </div>
  );
}

export function PairHeader({ selectedPair, onPairChange, onOpenMarketPanel }: PairHeaderProps) {
  const { prices, tickers } = useRealtimePrices();
  const [marketSelectorOpen, setMarketSelectorOpen] = useState(false);

  const symbol = selectedPair.split("/")[0];
  const market = PERPS_MARKETS.find((m) => m.symbol === symbol) ?? PERPS_MARKETS[0]!;
  const { data: enriched } = useEnrichedMarketStats(market.marketId);

  // WS feeds — orderbook mid + last trade for live price; stats for volume / OI / funding.
  const orderbook = useSidPaxOrderbook(market.symbol);
  const trades = useSidPaxTrades(market.symbol);
  const wsStats = useSidPaxStats(market.symbol);

  // Mid-price from top-of-book; fall back to last trade price from the tape.
  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastTradePrice = trades[0]?.price ?? 0;
  const livePrice = midPrice || lastTradePrice;

  // Small helper — pick first non-null/>0 value. The realtime `/tickers` REST
  // feed is the primary source because it polls every 3s, but for markets it
  // doesn't track (PAX, SID, and any other Paxeer-native listing) we fall
  // back to the subgraph `enrichedMarketStats` result, which carries the same
  // 24h / OI / funding fields under the `h24_*` / `open_interest` names.
  const pick = (...values: Array<number | null | undefined>): number => {
    for (const v of values) {
      if (typeof v === "number" && !Number.isNaN(v) && v !== 0) return v;
    }
    return 0;
  };

  const ticker = tickers[market.symbol];
  const et = enriched?.ticker;
  const markPrice = livePrice || pick(ticker?.mark_price, prices[market.symbol]);
  const indexPrice = livePrice || pick(ticker?.index_price);
  const high24h = pick(ticker?.["24h_high"], et?.h24_high);
  const low24h = pick(ticker?.["24h_low"], et?.h24_low);
  const open24h = pick(ticker?.["24h_open"], et?.h24_open);
  const close24h = pick(ticker?.["24h_close"], et?.h24_close) || markPrice;
  const volume24h    = wsStats !== null ? wsStats.volume_24h_usd   : pick(ticker?.["24h_volume"], et?.h24_volume);
  // The symbol socket's EMA estimate is shown only as an estimate; the venue's
  // last settled hourly rate and its ×24 projection are labelled as such.
  const estFunding   = wsStats?.funding_rate ?? null;
  const venueHourly  = ticker?.last_funding_rate_hourly
    ?? marketFundingHourly(market.symbol, "last_funding_rate_hourly", et?.last_funding_rate_hourly ?? enriched?.fundingRate?.last_funding_rate_hourly);
  const venueFunding = venueHourly === null ? null : fundingFigures(venueHourly);
  const openInterest = wsStats !== null ? wsStats.oi_usd             : pick(ticker?.open_interest, et?.open_interest);
  // Perp-specific stats — only available when wsStats is populated
  const basisBps         = wsStats?.basis_bps          ?? null;
  const nextFundingMs    = wsStats?.next_funding_ms     ?? null;
  const longShortRatio   = wsStats?.long_short_ratio    ?? null;
  const liqVolume        = wsStats?.liq_volume_24h      ?? null;
  const oiContracts      = wsStats?.oi_contracts        ?? null;
  const vol24hContracts  = wsStats?.volume_24h_contracts ?? null;

  const change = fmtChangePct(open24h, close24h);

  return (
    <div className="flex items-center gap-6 px-4 py-2 bg-card border-b border-border overflow-x-auto scrollbar-hide">
      {/* Market selector trigger — opens inline popover with full watchlist. */}
      <Popover.Root open={marketSelectorOpen} onOpenChange={setMarketSelectorOpen}>
        <Popover.Trigger asChild>
          <button
            className="flex items-center gap-2 hover:opacity-80 transition-opacity shrink-0"
            onClick={onOpenMarketPanel}
            aria-label="Open market selector"
          >
            <img
              src={coinLogoUrl(market.symbol)}
              alt={market.symbol}
              className="w-7 h-7 rounded-full object-cover"
            />
            <div className="flex flex-col items-start">
              <span className="text-sm font-semibold whitespace-nowrap leading-tight">{market.symbol}/USD</span>
              <span className="text-[9px] text-muted-foreground leading-none">{market.name}</span>
            </div>
            <ChevronDown className="w-3 h-3 text-muted-foreground" />
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={6}
            className="z-50 w-[640px] max-w-[calc(100vw-32px)] h-[70vh] max-h-[640px] overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface-1 shadow-2xl outline-none"
            onOpenAutoFocus={(e) => e.preventDefault()}
          >
            <MarketWatchlist
              chrome="popover"
              selectedSymbol={market.symbol}
              onSelect={(sym) => {
                onPairChange(`${sym}/USD`);
                setMarketSelectorOpen(false);
              }}
            />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>

      {/* Divider */}
      <div className="w-px h-8 bg-border shrink-0" />

      {/* Mark price (large) + 24h change */}
      <div className="shrink-0 flex flex-col items-start">
        {markPrice > 0 ? (
          <PriceFlash
            value={markPrice}
            className={cn("text-xl font-bold tabular-nums text-foreground")}
          >
            {`$${formatPrice(markPrice)}`}
          </PriceFlash>
        ) : (
          <span className="text-xl font-bold tabular-nums text-muted-foreground">Reconnecting…</span>
        )}
        <span className={cn("text-sm font-medium tabular-nums", change.positive ? "text-emerald-500" : "text-red-500")}>
          {change.text}
        </span>
      </div>

      {/* Divider */}
      <div className="w-px h-8 bg-border shrink-0" />

      {/* Stats chips — all API data */}
      <div className="flex items-center gap-6 overflow-x-auto scrollbar-hide">
        <StatChip
          label="Index"
          value={indexPrice > 0 ? `$${formatPrice(indexPrice)}` : "\u2014"}
        />

        <StatChip
          label="24h High"
          value={high24h > 0 ? `$${formatPrice(high24h)}` : "\u2014"}
          valueClass="text-emerald-500"
        />

        <StatChip
          label="24h Low"
          value={low24h > 0 ? `$${formatPrice(low24h)}` : "\u2014"}
          valueClass="text-red-500"
        />

        <StatChip
          label="24h Volume"
          value={volume24h > 0 ? fmtCompact(volume24h) : "\u2014"}
        />

        {estFunding !== null && (
          <StatChip
            label="Est. Funding"
            value={fmtPercent(estFunding)}
            valueClass={estFunding > 0 ? "text-emerald-500" : estFunding < 0 ? "text-red-500" : ""}
          />
        )}

        {(venueFunding ?? [FUNDING_LABEL_1H, FUNDING_LABEL_24H].map((label) => ({ label, rate: 0n, text: "\u2014" }))).map((f) => (
          <StatChip
            key={f.label}
            label={f.label}
            value={f.text}
            valueClass={f.rate > 0n ? "text-emerald-500" : f.rate < 0n ? "text-red-500" : ""}
          />
        ))}

        <StatChip
          label="Open Interest"
          value={openInterest > 0 ? fmtCompact(openInterest) : "\u2014"}
        />

        {oiContracts !== null && oiContracts > 0 && (
          <StatChip
            label="OI (Contracts)"
            value={oiContracts >= 1_000 ? `${(oiContracts / 1_000).toFixed(2)}K` : String(oiContracts.toFixed(2))}
          />
        )}

        {vol24hContracts !== null && vol24hContracts > 0 && (
          <StatChip
            label="Vol (Contracts)"
            value={vol24hContracts >= 1_000 ? `${(vol24hContracts / 1_000).toFixed(2)}K` : String(vol24hContracts.toFixed(2))}
          />
        )}

        {basisBps !== null && (
          <StatChip
            label="Basis"
            value={fmtBps(basisBps)}
            valueClass={basisBps > 0 ? "text-emerald-500" : basisBps < 0 ? "text-red-500" : ""}
          />
        )}

        {nextFundingMs !== null && nextFundingMs > 0 && (
          <StatChip
            label="Next Funding"
            value={fmtCountdown(nextFundingMs)}
          />
        )}

        {longShortRatio !== null && (
          <StatChip
            label="L/S Ratio"
            value={fmtRatio(longShortRatio)}
            valueClass={longShortRatio > 1 ? "text-emerald-500" : longShortRatio < 1 ? "text-red-500" : ""}
          />
        )}

        {liqVolume !== null && liqVolume > 0 && (
          <StatChip
            label="Liq Volume"
            value={fmtCompact(liqVolume)}
            valueClass="text-orange-400"
          />
        )}

        <StatChip
          label="Max Leverage"
          value={`${market.maxLeverage}x`}
        />
      </div>
    </div>
  );
}
