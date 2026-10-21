"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/ui/utils/cn";
import { useSpotTrades, type SpotTrade } from "@/hooks/spot/useSpotMarketStream";
import { useSpotVenueTrades } from "@/core/net/venueStream";

export interface SpotTradesTapeProps {
  symbol: string;
  /** Compact variant for the mobile trade view. */
  compact?: boolean;
}

function relativeTime(ms: number): string {
  const diff = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function fmtSize(val: number): string {
  if (!isFinite(val) || val <= 0) return "0";
  if (val >= 1_000_000) return `${(val / 1_000_000).toFixed(2)}M`;
  if (val >= 1_000) return `${(val / 1_000).toFixed(2)}K`;
  return val < 1 ? val.toFixed(4) : val.toFixed(2);
}

function fmtPrice(val: number): string {
  if (!isFinite(val) || val <= 0) return "\u2014";
  if (val >= 10000) return val.toFixed(2);
  if (val >= 1) return val.toFixed(4);
  return val.toFixed(6);
}

function fmtNotional(val: number): string {
  if (!isFinite(val) || val <= 0) return "$0";
  if (val >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  if (val >= 1_000) return `$${(val / 1_000).toFixed(2)}K`;
  return `$${val.toFixed(2)}`;
}

/**
 * `useFlashIds` returns a Set of trade ids that should pulse for ~700ms
 * after they first appear. Identification is by `trade_id` when present,
 * falling back to `trade_time + price + side`.
 */
function tradeKey(t: SpotTrade): string {
  if (t.trade_id) return t.trade_id;
  return `${t.trade_time}|${t.price}|${t.side}`;
}

function useFlashIds(trades: SpotTrade[]): Set<string> {
  const seenRef = useRef<Set<string>>(new Set());
  const [flashSet, setFlashSet] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (trades.length === 0) return;
    const newOnes: string[] = [];
    for (const t of trades) {
      const k = tradeKey(t);
      if (!seenRef.current.has(k)) {
        seenRef.current.add(k);
        newOnes.push(k);
      }
    }
    if (newOnes.length === 0) return;
    setFlashSet((prev) => {
      const next = new Set(prev);
      newOnes.forEach((k) => next.add(k));
      return next;
    });
    const id = window.setTimeout(() => {
      setFlashSet((prev) => {
        if (prev.size === 0) return prev;
        const next = new Set(prev);
        newOnes.forEach((k) => next.delete(k));
        return next;
      });
    }, 700);
    return () => window.clearTimeout(id);
  }, [trades]);

  return flashSet;
}

export function SpotTradesTape({ symbol, compact = false }: SpotTradesTapeProps) {
  // The spot venue trades of the gateway; the Chainflow reference tape while the
  // gateway socket is not open or the spot venue does not list the symbol.
  const venueTrades = useSpotVenueTrades(symbol);
  const referenceTrades = useSpotTrades(venueTrades ? undefined : symbol);
  const trades = useMemo<SpotTrade[]>(
    () =>
      venueTrades
        ? venueTrades.map((t) => ({
            side: t.side,
            price: t.price,
            size: t.size,
            trade_time: t.trade_time,
            ...(t.id !== null ? { trade_id: t.id } : {}),
          }))
        : referenceTrades,
    [venueTrades, referenceTrades],
  );
  const flashSet = useFlashIds(trades);

  const gridCols = compact
    ? "grid-cols-[44px_44px_1fr_1fr_1fr]"
    : "grid-cols-[56px_auto_1fr_1fr_1fr]";
  const padX = compact ? "px-2" : "px-3";
  const padY = compact ? "py-[3px]" : "py-[5px]";
  const labelTxt = compact ? "text-[9px]" : "text-[10px]";
  const valTxt = compact ? "text-[10px]" : "text-[11px]";

  if (trades.length === 0) {
    return (
      <div className="trading-panel flex flex-col h-full overflow-hidden">
        <div
          className={cn(
            "grid gap-x-2 py-2 font-mono uppercase tracking-wider text-muted-foreground border-b border-border shrink-0",
            gridCols,
            padX,
            labelTxt,
          )}
        >
          <span>Time</span>
          <span>Side</span>
          <span className="text-right">Notional</span>
          <span className="text-right">Size</span>
          <span className="text-right">Price</span>
        </div>
        <div className="flex-1 flex items-center justify-center">
          <span className="text-sm text-muted-foreground animate-pulse">
            No recent trades
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="trading-panel flex flex-col h-full overflow-hidden">
      <div
        className={cn(
          "grid gap-x-2 py-2 font-mono uppercase tracking-wider text-muted-foreground border-b border-border shrink-0",
          gridCols,
          padX,
          labelTxt,
        )}
      >
        <span>Time</span>
        <span>Side</span>
        <span className="text-right">Notional</span>
        <span className="text-right">Size</span>
        <span className="text-right">Price</span>
      </div>
      <div className="flex-1 overflow-y-auto scrollbar-custom">
        {trades.map((trade, i) => {
          const isBuy = trade.side === "BUY";
          const k = tradeKey(trade);
          const isFlashing = flashSet.has(k);
          const notional = trade.size * trade.price;
          return (
            <div
              key={k || `${trade.trade_time}-${i}`}
              className={cn(
                "grid gap-x-2 items-center transition-colors",
                gridCols,
                padX,
                padY,
                isFlashing
                  ? isBuy
                    ? "bg-emerald-500/[0.12]"
                    : "bg-red-500/[0.12]"
                  : "hover:bg-white/[0.02]",
              )}
            >
              <span
                className={cn(
                  "tabular-nums text-muted-foreground font-mono",
                  labelTxt,
                )}
              >
                {relativeTime(trade.trade_time)}
              </span>
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
              </span>
              <span
                className={cn(
                  "tabular-nums text-foreground font-mono text-right",
                  valTxt,
                )}
              >
                {fmtNotional(notional)}
              </span>
              <span
                className={cn(
                  "tabular-nums text-muted-foreground font-mono text-right",
                  valTxt,
                )}
              >
                {fmtSize(trade.size)}
              </span>
              <span
                className={cn(
                  "tabular-nums font-mono text-right",
                  valTxt,
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
