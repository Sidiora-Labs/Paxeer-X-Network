import { useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import { spotCoinLogoUrl, type SpotMarket } from "@/core/spot/config";
import {
  useSpotOrderbook,
  useSpotStats,
  useSpotTrades,
} from "@/hooks/spot/useSpotMarketStream";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotTickerBar — SPOT mirror of MobileTickerBar.

   Same topology as the perps version:
     · Left   — coin icon, pair label, "SPOT" tag, chevron → opens selector
     · Right  — last price, 24h change row, expand chevron
     · Below  — collapsible 24h Vol / High / Low / Change strip
   ══════════════════════════════════════════════════════════════════════ */

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

interface MobileSpotTickerBarProps {
  market: SpotMarket;
  onOpenSelector: () => void;
}

export function MobileSpotTickerBar({ market, onOpenSelector }: MobileSpotTickerBarProps) {
  const [expanded, setExpanded] = useState(false);
  const orderbook = useSpotOrderbook(market.symbol);
  const trades = useSpotTrades(market.symbol);
  const wsStats = useSpotStats(market.symbol);

  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastTradePrice = trades[0]?.price ?? 0;
  const livePrice = lastTradePrice || midPrice || (wsStats?.last_price ?? 0);

  // SpotStats.change_24h is a fraction (e.g. 0.0235 for +2.35%).
  const changeFrac = wsStats?.change_24h ?? 0;
  const changePct = changeFrac * 100;
  const isPositive = changePct >= 0;
  const open24 = livePrice > 0 && changeFrac !== 0 ? livePrice / (1 + changeFrac) : 0;
  const changeAbs = open24 > 0 ? livePrice - open24 : 0;

  const high24h = wsStats?.high_24h ?? 0;
  const low24h = wsStats?.low_24h ?? 0;
  const volume24h = wsStats?.volume_24h ?? 0;

  return (
    <div
      className="border-b md:hidden"
      style={{ borderColor: "var(--border-soft)", background: "var(--bg)" }}
    >
      {/* Market header strip */}
      <div className="flex items-center justify-between px-3 py-2">
        {/* Left block: asset icon + pair label + SPOT badge + chevron */}
        <button
          onClick={onOpenSelector}
          className="flex items-center gap-2 min-w-0"
        >
          <img
            src={spotCoinLogoUrl(market.symbol)}
            alt={market.symbol}
            className="w-7 h-7 rounded-full object-cover shrink-0"
          />
          <div className="flex flex-col items-start leading-tight">
            <span
              className="text-sm font-semibold"
              style={{ color: "var(--fg)" }}
            >
              {market.pair}
            </span>
            <span
              className="text-[9px] font-mono rounded px-1 py-[1px] mt-0.5"
              style={{
                color: "var(--accent-solid)",
                background: "rgba(0, 76, 237, 0.10)",
                letterSpacing: "0.08em",
                textTransform: "uppercase",
              }}
            >
              SPOT
            </span>
          </div>
          <ChevronDown className="w-3.5 h-3.5" style={{ color: "var(--fg-muted)" }} />
        </button>

        {/* Right block: last price + change row + expand chevron */}
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
              ${livePrice > 0 ? fmtPrice(livePrice) : "—"}
            </div>
            <div
              className="tabular-nums"
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                color: isPositive ? "var(--success)" : "var(--danger)",
              }}
            >
              {open24 > 0
                ? `${isPositive ? "+" : ""}${fmtPrice(Math.abs(changeAbs))} / ${isPositive ? "+" : ""}${changePct.toFixed(2)}%`
                : "—"}
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
