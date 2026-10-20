"use client";

import { cn } from "@/ui/utils/cn";
import { useSidPaxTrades } from "@/hooks/perps/useSidPaxMarketStream";

export interface MarketTradesTapeProps {
  symbol: string;
}

function relativeTime(ms: number): string {
  const diff = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function fmtNotional(val: number): string {
  if (!isFinite(val) || val <= 0) return "$0";
  if (val >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  if (val >= 1_000) return `$${(val / 1_000).toFixed(2)}K`;
  return `$${val.toFixed(2)}`;
}

function fmtContracts(val: number): string {
  if (!isFinite(val)) return "—";
  if (val >= 1_000) return `${(val / 1_000).toFixed(2)}K`;
  return val.toFixed(val < 1 ? 4 : 2);
}

function fmtPrice(val: number): string {
  if (!isFinite(val) || val <= 0) return "—";
  if (val >= 10000) return val.toFixed(2);
  if (val >= 100) return val.toFixed(2);
  if (val >= 1) return val.toFixed(4);
  return val.toFixed(6);
}

export function MarketTradesTape({ symbol }: MarketTradesTapeProps) {
  const trades = useSidPaxTrades(symbol);

  if (trades.length === 0) {
    return (
      <div className="trading-panel flex flex-col h-full overflow-hidden">
        <div className="grid grid-cols-[56px_auto_1fr_1fr_1fr] gap-x-2 px-3 py-2 text-[10px] font-mono uppercase tracking-wider text-muted-foreground border-b border-border shrink-0">
          <span>Time</span>
          <span>Side</span>
          <span className="text-right">Notional</span>
          <span className="text-right">Ctrs</span>
          <span className="text-right">Price</span>
        </div>
        <div className="flex-1 flex items-center justify-center">
          <span className="text-sm text-muted-foreground animate-pulse">No recent trades</span>
        </div>
      </div>
    );
  }

  return (
    <div className="trading-panel flex flex-col h-full overflow-hidden">
      <div className="grid grid-cols-[56px_auto_1fr_1fr_1fr] gap-x-2 px-3 py-2 text-[10px] font-mono uppercase tracking-wider text-muted-foreground border-b border-border shrink-0">
        <span>Time</span>
        <span>Side</span>
        <span className="text-right">Notional</span>
        <span className="text-right">Ctrs</span>
        <span className="text-right">Price</span>
      </div>
      <div className="flex-1 overflow-y-auto scrollbar-custom" style={{ maxHeight: 300 }}>
        {trades.map((trade, i) => {
          const isBuy = trade.side === "BUY";
          return (
            <div
              key={trade.trade_id ?? `${trade.trade_time}-${i}`}
              className="grid grid-cols-[56px_auto_1fr_1fr_1fr] gap-x-2 items-center px-3 py-[5px] hover:bg-white/[0.02] transition-colors"
            >
              <span className="tabular-nums text-muted-foreground text-[10px] font-mono">
                {relativeTime(trade.trade_time)}
              </span>

              {/* Side badge — includes LIQ indicator for liquidation trades */}
              <span className="flex items-center gap-1 shrink-0">
                <span
                  className={cn(
                    "text-[9px] font-mono font-semibold px-1.5 py-0.5 rounded-full",
                    isBuy
                      ? "bg-emerald-500/10 text-emerald-400"
                      : "bg-red-500/10 text-red-400",
                  )}
                >
                  {isBuy ? "Buy" : "Sell"}
                </span>
                {trade.liquidation && (
                  <span className="text-[8px] font-mono font-semibold px-1 py-0.5 rounded bg-orange-500/15 text-orange-400 uppercase tracking-wider">
                    Liq
                  </span>
                )}
              </span>

              <span className="tabular-nums text-foreground text-[11px] font-mono text-right">
                {fmtNotional(trade.notional || trade.size)}
              </span>
              <span className="tabular-nums text-muted-foreground text-[11px] font-mono text-right">
                {fmtContracts(trade.contracts)}
              </span>
              <span
                className={cn(
                  "tabular-nums text-[11px] font-mono text-right",
                  isBuy ? "text-emerald-400" : "text-red-400",
                )}
              >
                {fmtPrice(trade.price)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
