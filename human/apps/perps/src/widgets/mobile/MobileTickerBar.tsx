import { useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { PERPS_MARKETS, coinLogoUrl, type PerpsMarket } from "@/core/perps/config";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useEnrichedMarketStats } from "@/hooks/perps/useEnrichedStats";
import {
  useSidPaxOrderbook,
  useSidPaxTrades,
  useSidPaxStats,
} from "@/hooks/perps/useSidPaxMarketStream";

function fmtPrice(price: number): string {
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

interface MobileTickerBarProps {
  market: PerpsMarket;
  onOpenSelector: () => void;
}

export function MobileTickerBar({ market, onOpenSelector }: MobileTickerBarProps) {
  const [expanded, setExpanded] = useState(false);
  const { prices, tickers } = useRealtimePrices();
  const { data: enriched } = useEnrichedMarketStats(market.marketId);
  const orderbook = useSidPaxOrderbook(market.symbol);
  const trades = useSidPaxTrades(market.symbol);
  const wsStats = useSidPaxStats(market.symbol);

  const ticker = tickers[market.symbol];
  const et = enriched?.ticker;

  const pickNum = (...values: Array<number | null | undefined>): number => {
    for (const v of values) {
      if (typeof v === "number" && !Number.isNaN(v) && v !== 0) return v;
    }
    return 0;
  };

  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastTradePrice = trades[0]?.price ?? 0;
  const markPrice = midPrice || lastTradePrice || pickNum(ticker?.mark_price, et?.mark_price, prices[market.symbol]);

  const open24h = pickNum(ticker?.["24h_open"], et?.h24_open);
  const close24h = pickNum(ticker?.["24h_close"], et?.h24_close) || markPrice;
  const changePct = open24h > 0 ? ((close24h - open24h) / open24h) * 100 : 0;
  const changeAbs = close24h - open24h;
  const isPositive = changePct >= 0;

  const volume24h = wsStats !== null ? wsStats.volume_24h_usd : (ticker?.["24h_volume"] ?? 0);
  const high24h = ticker?.["24h_high"] ?? 0;
  const low24h = ticker?.["24h_low"] ?? 0;

  return (
    <div
      className="border-b md:hidden"
      style={{ borderColor: "var(--border-soft)", background: "var(--bg)" }}
    >
      {/* Market header strip */}
      <div className="flex items-center justify-between px-3 py-2">
        {/* Left block: asset icon + pair label + leverage badge + chevron */}
        <button
          onClick={onOpenSelector}
          className="flex items-center gap-2 min-w-0"
        >
          <img
            src={coinLogoUrl(market.symbol)}
            alt={market.symbol}
            className="w-7 h-7 rounded-full object-cover shrink-0"
          />
          <div className="flex flex-col items-start leading-tight">
            <span
              className="text-sm font-semibold"
              style={{ color: "var(--fg)" }}
            >
              {market.symbol}/USD
            </span>
            <span
              className="text-[9px] font-mono rounded px-1 py-[1px] mt-0.5"
              style={{
                color: "var(--fg-muted)",
                background: "var(--bg-subtle)",
              }}
            >
              {market.maxLeverage}x
            </span>
          </div>
          <ChevronDown className="w-3.5 h-3.5" style={{ color: "var(--fg-muted)" }} />
        </button>

        {/* Right block: mark price + change row + expand chevron */}
        <div className="flex items-center gap-3">
          <div className="text-right">
            <div
              className="tabular-nums"
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 16,
                fontWeight: 500,
                color: "var(--fg)",
                letterSpacing: "-0.01em",
              }}
            >
              ${markPrice > 0 ? fmtPrice(markPrice) : "—"}
            </div>
            <div
              className="tabular-nums"
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                color: isPositive ? "var(--success)" : "var(--danger)",
              }}
            >
              {isPositive ? "+" : ""}{fmtPrice(Math.abs(changeAbs))} / {isPositive ? "+" : ""}{changePct.toFixed(2)}%
            </div>
          </div>
          <button
            onClick={() => setExpanded((e) => !e)}
            className="p-1.5 rounded-md transition-colors"
            style={{ background: "var(--bg-subtle)" }}
            aria-label={expanded ? "Collapse metrics" : "Expand metrics"}
          >
            {expanded ? (
              <ChevronUp className="w-4 h-4" style={{ color: "var(--fg-muted)" }} />
            ) : (
              <ChevronDown className="w-4 h-4" style={{ color: "var(--fg-muted)" }} />
            )}
          </button>
        </div>
      </div>

      {/* Expanded metrics row */}
      <div
        className="overflow-hidden transition-all duration-300 ease-in-out"
        style={{ maxHeight: expanded ? 60 : 0, opacity: expanded ? 1 : 0 }}
      >
        <div
          className="flex items-center justify-between px-3 pb-2 gap-4"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.04em",
          }}
        >
          <div className="flex flex-col items-center">
            <span style={{ color: "var(--fg-subtle)", fontSize: 9, textTransform: "uppercase", letterSpacing: "0.1em" }}>24h Vol</span>
            <span className="tabular-nums" style={{ color: "var(--fg)" }}>{volume24h > 0 ? fmtCompact(volume24h) : "—"}</span>
          </div>
          <div className="flex flex-col items-center">
            <span style={{ color: "var(--fg-subtle)", fontSize: 9, textTransform: "uppercase", letterSpacing: "0.1em" }}>24h High</span>
            <span className="tabular-nums" style={{ color: "var(--success)" }}>{high24h > 0 ? fmtPrice(high24h) : "—"}</span>
          </div>
          <div className="flex flex-col items-center">
            <span style={{ color: "var(--fg-subtle)", fontSize: 9, textTransform: "uppercase", letterSpacing: "0.1em" }}>24h Low</span>
            <span className="tabular-nums" style={{ color: "var(--danger)" }}>{low24h > 0 ? fmtPrice(low24h) : "—"}</span>
          </div>
          <div className="flex flex-col items-center">
            <span style={{ color: "var(--fg-subtle)", fontSize: 9, textTransform: "uppercase", letterSpacing: "0.1em" }}>Change</span>
            <span className="tabular-nums" style={{ color: isPositive ? "var(--success)" : "var(--danger)" }}>
              {isPositive ? "+" : ""}{changePct.toFixed(2)}%
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
