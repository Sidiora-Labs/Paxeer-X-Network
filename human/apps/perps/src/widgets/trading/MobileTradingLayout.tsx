import { useMemo, useState, useEffect } from "react";
import { cn } from "@/ui/utils/cn";
import { PERPS_MARKETS, coinLogoUrl, type PerpsMarket } from "@/core/perps/config";
import { Chart } from "./Chart";
import { Orderbook } from "./Orderbook";
import { PerpsOrderForm } from "./PerpsOrderForm";
import { PerpsPositionsPanel } from "./PerpsPositionsPanel";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import {
  hasDedicatedMarketStream,
  useSidPaxOrderbook,
  useSidPaxTrades,
  useSidPaxStats,
} from "@/hooks/perps/useSidPaxMarketStream";
import { fundingRowFigure } from "@/core/perps/fundingRate";

type MobileMarketView = "chart" | "trades" | "data";

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

/* ── Funding rate countdown ────────────────────────────────────────── */
function FundingRateRow({ symbol }: { symbol: string }) {
  const { tickers } = useRealtimePrices();
  const wsStats = useSidPaxStats(symbol);
  const ticker = tickers[symbol];
  // The symbol socket's predicted rate when its stats carry one, else the venue's last settled hourly rate, each under its own label.
  const funding = fundingRowFigure(wsStats?.funding_rate, ticker?.last_funding_rate_hourly ?? null);
  // Use server-provided next_funding_ms when available; otherwise count to next 8h mark.
  const serverNextMs = wsStats?.next_funding_ms ?? 0;

  const [countdown, setCountdown] = useState("");
  useEffect(() => {
    let id: ReturnType<typeof setInterval> | null = null;
    const tick = () => {
      let diff: number;
      if (serverNextMs > 0) {
        diff = Math.max(0, serverNextMs - (Date.now() % serverNextMs));
      } else {
        const now = Date.now();
        const next8h = Math.ceil(now / (8 * 3600_000)) * (8 * 3600_000);
        diff = Math.max(0, next8h - now);
      }
      const totalSec = Math.floor(diff / 1000);
      const h = Math.floor(totalSec / 3600);
      const m = Math.floor((totalSec % 3600) / 60);
      const s = totalSec % 60;
      if (h > 0) setCountdown(`in ${h}h ${String(m).padStart(2, "0")}m`);
      else setCountdown(`in ${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`);
    };
    const start = () => {
      if (id !== null) return;
      tick();
      id = setInterval(tick, 1000);
    };
    const stop = () => {
      if (id !== null) { clearInterval(id); id = null; }
    };
    const handleVisibility = () => {
      if (document.visibilityState === "hidden") stop();
      else start();
    };
    handleVisibility();
    document.addEventListener("visibilitychange", handleVisibility);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibility);
      stop();
    };
  }, [serverNextMs]);

  return (
    <div className="flex items-center justify-between px-3 py-1.5 bg-background">
      <span className="text-[10px] text-muted-foreground">
        {funding.label}{" "}
        <span className={cn("font-mono font-semibold tabular-nums", funding.sign > 0 ? "text-emerald-500" : funding.sign < 0 ? "text-red-500" : "")}>
          {funding.text}
        </span>{" "}
        <span className="text-muted-foreground">{countdown}</span>
      </span>
    </div>
  );
}

/* ── Data tab panel ────────────────────────────────────────────────── */
function MobileDataPanel({ market }: { market: PerpsMarket }) {
  const { prices, tickers } = useRealtimePrices();
  const orderbook = useSidPaxOrderbook(market.symbol);
  const trades = useSidPaxTrades(market.symbol);
  const wsStats = useSidPaxStats(market.symbol);

  const ticker = tickers[market.symbol];
  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastTradePrice = trades[0]?.price ?? 0;
  const livePrice = midPrice || lastTradePrice;

  const markPrice = livePrice || ticker?.mark_price || prices[market.symbol] || 0;
  const indexPrice = livePrice || ticker?.index_price || 0;
  const volume24h    = wsStats !== null ? wsStats.volume_24h_usd : (ticker?.["24h_volume"] ?? 0);
  const openInterest = wsStats !== null ? wsStats.oi_usd          : (ticker?.open_interest  ?? 0);
  const high24h = ticker?.["24h_high"] ?? 0;
  const low24h  = ticker?.["24h_low"]  ?? 0;

  const rows: { label: string; value: string; valueClass?: string }[] = [
    { label: "Mark price",   value: markPrice    > 0 ? formatPrice(markPrice) : "—" },
    { label: "Index price",  value: indexPrice   > 0 ? formatPrice(indexPrice) : "—" },
    ...(wsStats?.basis_bps != null ? [{
      label: "Basis",
      value: `${wsStats.basis_bps >= 0 ? "+" : ""}${wsStats.basis_bps.toFixed(2)} bps`,
      valueClass: wsStats.basis_bps > 0 ? "text-emerald-400" : wsStats.basis_bps < 0 ? "text-red-400" : "",
    }] : []),
    { label: "24h volume",   value: volume24h    > 0 ? `${fmtCompact(volume24h)} USD`  : "—" },
    { label: "24h high",     value: high24h      > 0 ? formatPrice(high24h) : "—" },
    { label: "24h low",      value: low24h       > 0 ? formatPrice(low24h)  : "—" },
    { label: "Open interest",value: openInterest > 0 ? `${fmtCompact(openInterest)} USD` : "—" },
    ...(wsStats?.oi_contracts != null && wsStats.oi_contracts > 0 ? [{
      label: "OI (contracts)",
      value: wsStats.oi_contracts >= 1_000
        ? `${(wsStats.oi_contracts / 1_000).toFixed(2)}K`
        : wsStats.oi_contracts.toFixed(2),
    }] : []),
    ...(wsStats?.long_short_ratio != null ? [{
      label: "L/S ratio",
      value: wsStats.long_short_ratio.toFixed(2),
      valueClass: wsStats.long_short_ratio > 1 ? "text-emerald-400" : wsStats.long_short_ratio < 1 ? "text-red-400" : "",
    }] : []),
    ...(wsStats?.liq_volume_24h != null && wsStats.liq_volume_24h > 0 ? [{
      label: "Liq volume",
      value: fmtCompact(wsStats.liq_volume_24h),
      valueClass: "text-orange-400",
    }] : []),
  ];

  return (
    <div className="divide-y divide-border">
      {rows.map((row) => (
        <div key={row.label} className="flex items-center justify-between px-4 py-2.5">
          <span className="text-xs text-muted-foreground">{row.label}</span>
          <span className={cn("text-xs font-mono font-semibold tabular-nums", row.valueClass ?? "text-foreground")}>{row.value}</span>
        </div>
      ))}
    </div>
  );
}

/* ── Trades tab (recent executions) ────────────────────────────────── */
function MobileTradesPanel({ symbol }: { symbol: string }) {
  const { tickers } = useRealtimePrices();
  const isDedicated = hasDedicatedMarketStream(symbol);
  // Hook order must stay stable — always call, but pass `undefined` when
  // the symbol doesn't have a dedicated stream so no WS is opened.
  const dedicatedTrades = useSidPaxTrades(isDedicated ? symbol : undefined);
  const ticker = tickers[symbol];
  const tickerTrades = (ticker as any)?.recent_trades as
    | { time: string; price: number; qty: number; side: string }[]
    | undefined;

  const trades: { time: string; price: number; qty: number; side: string }[] | undefined =
    isDedicated
      ? dedicatedTrades.map((t) => ({
          time: new Date(t.trade_time).toISOString().slice(11, 19),
          price: t.price,
          qty: t.size,
          side: t.side === "BUY" ? "buy" : "sell",
        }))
      : tickerTrades;

  return (
    <div className="flex flex-col">
      <div className="flex justify-between px-3 py-2 text-xs text-muted-foreground shrink-0">
        <span>Time</span>
        <span>Price</span>
        <span>Size</span>
      </div>
      <div className="overflow-y-auto" style={{ maxHeight: 260 }}>
        {trades && trades.length > 0 ? (
          trades.map((t, i) => (
            <div key={i} className="order-book-row flex justify-between px-3">
              <span className="tabular-nums text-muted-foreground">{t.time}</span>
              <span className={cn("tabular-nums", t.side === "buy" ? "text-emerald-400" : "text-red-400")}>
                {formatPrice(t.price)}
              </span>
              <span className="tabular-nums text-foreground">{t.qty.toFixed(4)}</span>
            </div>
          ))
        ) : (
          <div className="flex flex-col items-center justify-center min-h-[120px] text-muted-foreground">
            <span className="text-sm">Loading trades...</span>
          </div>
        )}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════════
   Main Mobile Trading Layout
   ═══════════════════════════════════════════════════════════════════════ */
export function MobileTradingLayout({ market, onSelectMarket }: { market: PerpsMarket; onSelectMarket: (symbol: string) => void }) {
  const [view, setView] = useState<MobileMarketView>("chart");

  return (
    <div className="flex-1 min-h-0 overflow-y-auto bg-background">

      {/* ── 1. Chart / Trades / Data tab strip ── */}
      <div className="flex border-b border-border">
        {([
          { key: "chart", label: "Chart" },
          { key: "trades", label: "Trades" },
          { key: "data", label: "Data" },
        ] as { key: MobileMarketView; label: string }[]).map((item) => (
          <button
            key={item.key}
            className="trading-tab"
            data-active={view === item.key ? "true" : "false"}
            onClick={() => setView(item.key)}
          >
            {item.label}
          </button>
        ))}
      </div>

      {/* ── 2. Visualization area (chart / trades list / data) ── */}
      <div className="bg-card">
        {view === "chart" && (
          <div style={{ height: 386 }}>
            <Chart selectedPair={`${market.symbol}/USD`} height={386} onHeightChange={() => {}} positions={[]} />
          </div>
        )}
        {view === "trades" && (
          <div style={{ height: 280 }}>
            <MobileTradesPanel symbol={market.symbol} />
          </div>
        )}
        {view === "data" && (
          <MobileDataPanel market={market} />
        )}
      </div>

      {/* ── 3. Funding rate row ── */}
      <FundingRateRow symbol={market.symbol} />

      {/* ── 4. Orderbook (left ~45%) + Order Form (right ~55%) — SIDE BY SIDE ── */}
      {/* items-stretch so the orderbook column always visually matches the form's
          natural height. Orderbook height bumped to 640 (from 480) so it shows
          enough depth to not look truncated next to the now-taller paxeer order
          form (long/short toggle + pills + leverage + toggles + preview card). */}
      <div className="flex items-stretch">
        {/* Orderbook column */}
        <div className="w-[38%] shrink-0 border-r border-border">
          <Orderbook symbol={market.symbol} height={540} compact />
        </div>
        {/* Order form column */}
        <div className="flex-1 min-w-0">
          <PerpsOrderForm market={market} compact />
        </div>
      </div>

      {/* ── 5. Positions / Activity panel ── */}
      <div className="flex flex-col">
        <PerpsPositionsPanel />
      </div>

    </div>
  );
}
