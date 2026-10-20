"use client";

import { useMemo } from "react";
import { cn } from "@/ui/utils/cn";
import { fmtUsdCompact as fmtUsd, parse18 } from "@/ui/utils/format";
import { coinLogoUrl, PERPS_MARKETS } from "@/core/perps/config";
import { computePositionPnl, type ParsedPosition } from "@/core/perps/pnlHelpers";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { SectionCard, EmptyState, ConnectGate } from "./PortfolioPrimitives";

/**
 * Positions tab.
 *
 * Takes the raw positions array from `usePortfolioStats` and enriches each
 * row with realtime mark prices to compute uPnL / uPnL%. Rendering is a
 * responsive table: full grid on md+, condensed flex layout on mobile.
 *
 * Self-contained: page module just passes `positions` and connection state.
 */

export interface PortfolioPositionsPanelProps {
  /** Raw positions snapshot from `usePortfolioStats().positions`. */
  positions: any[];
  isConnected: boolean;
  hasVault: boolean;
  onConnect: () => void;
}

interface PositionRow {
  id: string;
  symbol: string;
  side: "long" | "short";
  size: number;
  leverage: number;
  entryPrice: number;
  markPrice: number;
  unrealizedPnl: number;
  pnlPct: number;
}

export function PortfolioPositionsPanel({
  positions,
  isConnected,
  hasVault,
  onConnect,
}: PortfolioPositionsPanelProps) {
  const { tickers } = useRealtimePrices();

  const rows = useMemo<PositionRow[]>(() => {
    return positions.map((pos) => {
      const sym =
        PERPS_MARKETS.find((m) => m.marketId === Number(pos.marketId))?.symbol ??
        `MKT-${pos.marketId}`;
      const parsed: ParsedPosition = {
        positionId: BigInt(pos.positionId),
        marketId: Number(pos.marketId),
        symbol: sym,
        isLong: pos.isLong,
        size: parse18(pos.sizeUsd),
        collateral: parse18(pos.collateralUsd),
        leverage: parse18(pos.leverage),
        entryPrice: parse18(pos.entryPrice),
        collateralToken: pos.collateralToken ?? "",
        collateralAmount: parse18(pos.collateralAmount),
        openedAt: pos.openedAt,
        openTxHash: pos.openTxHash,
      };
      const markPrice = tickers[sym]?.mark_price ?? 0;
      const pnlData = markPrice > 0 ? computePositionPnl(parsed, markPrice) : null;
      return {
        id: String(pos.positionId),
        symbol: sym,
        side: pos.isLong ? "long" : "short",
        size: parsed.size,
        leverage: parsed.leverage,
        entryPrice: parsed.entryPrice,
        markPrice,
        unrealizedPnl: pnlData?.unrealizedPnl ?? 0,
        pnlPct:
          pnlData && parsed.collateral > 0
            ? (pnlData.unrealizedPnl / parsed.collateral) * 100
            : 0,
      };
    });
  }, [positions, tickers]);

  if (!isConnected) {
    return (
      <SectionCard title="Open positions">
        <ConnectGate isConnected={isConnected} onConnect={onConnect} />
      </SectionCard>
    );
  }
  if (!hasVault) {
    return (
      <SectionCard title="Open positions">
        <EmptyState
          title="No vault yet"
          description="Create a vault to start trading."
        />
      </SectionCard>
    );
  }
  if (rows.length === 0) {
    return (
      <SectionCard title="Open positions">
        <EmptyState
          title="No open positions"
          description="Your open positions will appear here once you place a trade."
        />
      </SectionCard>
    );
  }

  return (
    <SectionCard title="Open positions" count={rows.length}>
      <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] overflow-hidden">
        {/* ── Header (desktop only) ── */}
        <div
          className="hidden md:grid items-center px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground border-b border-white/[0.04]"
          style={{
            gridTemplateColumns:
              "minmax(140px,1.2fr) 90px 1fr 1fr 1fr 1.2fr",
          }}
        >
          <span>Market</span>
          <span>Side</span>
          <span className="text-right">Size</span>
          <span className="text-right">Entry</span>
          <span className="text-right">Mark</span>
          <span className="text-right">uPNL</span>
        </div>

        {/* ── Rows ── */}
        <div className="divide-y divide-white/[0.04]">
          {rows.map((r) => (
            <PositionRowItem key={r.id} row={r} />
          ))}
        </div>
      </div>
    </SectionCard>
  );
}

function PositionRowItem({ row: r }: { row: PositionRow }) {
  return (
    <div
      className="flex md:grid items-center gap-3 px-4 py-3 justify-between"
      style={{
        gridTemplateColumns: "minmax(140px,1.2fr) 90px 1fr 1fr 1fr 1.2fr",
      }}
    >
      <div className="flex items-center gap-2 min-w-0">
        <img
          src={coinLogoUrl(r.symbol)}
          alt=""
          className="h-6 w-6 rounded-full shrink-0"
        />
        <div className="min-w-0">
          <div className="text-sm font-semibold text-foreground truncate">
            {r.symbol}
          </div>
          <div className="text-[10px] font-mono text-muted-foreground md:hidden">
            {r.side === "long" ? "Long" : "Short"} · {r.leverage.toFixed(1)}x
          </div>
        </div>
      </div>

      <span
        className={cn(
          "hidden md:inline-flex items-center justify-center w-[72px] h-6 rounded-[var(--radius-pill)] text-[11px] font-semibold font-mono uppercase",
          r.side === "long"
            ? "bg-emerald-500/10 text-emerald-400"
            : "bg-red-500/10 text-red-400",
        )}
      >
        {r.side} {r.leverage.toFixed(0)}x
      </span>

      <span className="hidden md:block text-right font-mono tabular-nums text-xs">
        {fmtUsd(r.size)}
      </span>
      <span className="hidden md:block text-right font-mono tabular-nums text-xs">
        ${r.entryPrice.toLocaleString("en-US", { maximumFractionDigits: 4 })}
      </span>
      <span className="hidden md:block text-right font-mono tabular-nums text-xs text-muted-foreground">
        {r.markPrice > 0
          ? `$${r.markPrice.toLocaleString("en-US", { maximumFractionDigits: 4 })}`
          : "—"}
      </span>

      <div className="flex flex-col items-end">
        <span
          className={cn(
            "font-mono tabular-nums text-xs font-semibold",
            r.unrealizedPnl >= 0 ? "text-emerald-400" : "text-red-400",
          )}
        >
          {r.unrealizedPnl >= 0 ? "+" : ""}
          {fmtUsd(r.unrealizedPnl)}
        </span>
        <span
          className={cn(
            "text-[10px] font-mono tabular-nums",
            r.unrealizedPnl >= 0 ? "text-emerald-400/70" : "text-red-400/70",
          )}
        >
          {r.pnlPct >= 0 ? "+" : ""}
          {r.pnlPct.toFixed(2)}%
        </span>
      </div>
    </div>
  );
}
