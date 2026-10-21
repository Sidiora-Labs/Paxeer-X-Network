"use client";

import React, { useMemo } from "react";
import { ArrowDownToLine, ArrowUpFromLine, Eye, EyeOff, RefreshCw } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { coinLogoUrl } from "@/core/perps/config";
import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { PortfolioAssetsChart } from "./PortfolioAssetsChart";
import { PortfolioPerformanceGrid } from "./PortfolioPerformanceGrid";

interface TokenBalance {
  symbol: string;
  address: `0x${string}`;
  decimals: number;
  vaultBalance: bigint;
  lockedBalance: bigint;
  walletBalance: bigint;
  available: bigint;
}

interface PortfolioOverviewPanelProps {
  isConnected: boolean;
  hasVault: boolean;
  address: string | null;
  totalVaultUsd: number;
  unrealizedPnl: number;
  openPositions: number;
  realizedPnl: number;
  totalTrades: number;
  balances: TokenBalance[];
  dailyPnl: DailyPnl[];
  dailyPnlLoading: boolean;
  /** Set when both PerfDB daily PnL reads failed. */
  dailyPnlError: Error | null;
  /** Days PerfDB returned with incomplete values. */
  dailyPnlSkippedDays: number;
  isLoading: boolean;
  privacy: boolean;
  onTogglePrivacy: () => void;
  onRefresh: () => void;
  onConnect: () => void;
  onDeposit: () => void;
  onWithdraw: () => void;
}

/**
 * Overview tab for the pro portfolio page.
 *
 *  ┌───────────────────────────────────┬──────────────────────────────┐
 *  │ Total value · Unrealised · Stats   │ Assets balance chart (7D ▾)  │
 *  │ [Deposit] [Withdraw]               │                              │
 *  ├───────────────────────────────────┴──────────────────────────────┤
 *  │ Performance · Live              7D ▾                             │
 *  │ ┌─────┐┌─────┐┌─────┐                                            │
 *  │ │ PNL ││ VOL ││ ROI │                                            │
 *  │ └─────┘└─────┘└─────┘                                            │
 *  │ ┌─────────┐┌─────────┐                                           │
 *  │ │Daily PnL││Cum PnL  │                                           │
 *  │ └─────────┘└─────────┘                                           │
 *  │ ┌─────────┐┌─────────┐                                           │
 *  │ │Daily Vol││Cum Vol  │                                           │
 *  │ └─────────┘└─────────┘                                           │
 *  └──────────────────────────────────────────────────────────────────┘
 */
export function PortfolioOverviewPanel(props: PortfolioOverviewPanelProps) {
  const {
    isConnected, hasVault, address,
    totalVaultUsd, unrealizedPnl, openPositions, realizedPnl, totalTrades,
    balances, dailyPnl, dailyPnlLoading, dailyPnlError, dailyPnlSkippedDays, isLoading, privacy,
    onTogglePrivacy, onRefresh, onConnect, onDeposit, onWithdraw,
  } = props;

  const displayValue = isConnected && hasVault ? totalVaultUsd : 0;
  const displayPnl = isConnected && hasVault ? unrealizedPnl : 0;
  const pnlPct = displayValue > 0 ? (displayPnl / displayValue) * 100 : 0;

  return (
    <div className="space-y-4 md:space-y-6">
      {/* ── Row 1 — Total Value card + Assets chart ── */}
      <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] gap-4">
        <TotalValueCard
          isConnected={isConnected}
          hasVault={hasVault}
          address={address}
          totalValue={displayValue}
          pnl={displayPnl}
          pnlPct={pnlPct}
          openPositions={openPositions}
          realizedPnl={realizedPnl}
          totalTrades={totalTrades}
          balances={balances}
          isLoading={isLoading}
          privacy={privacy}
          onTogglePrivacy={onTogglePrivacy}
          onRefresh={onRefresh}
          onConnect={onConnect}
          onDeposit={onDeposit}
          onWithdraw={onWithdraw}
        />
        <PortfolioAssetsChart
          dailyPnl={dailyPnl}
          totalVaultUsd={displayValue}
          isLoading={dailyPnlLoading}
        />
      </div>

      {/* ── Row 2 — Performance grid ── */}
      <PortfolioPerformanceGrid
        dailyPnl={dailyPnl}
        isLoading={dailyPnlLoading}
        error={dailyPnlError}
        skippedDays={dailyPnlSkippedDays}
      />
    </div>
  );
}

// ── Total value card ─────────────────────────────────────────────────

interface TotalValueCardProps {
  isConnected: boolean;
  hasVault: boolean;
  address: string | null;
  totalValue: number;
  pnl: number;
  pnlPct: number;
  openPositions: number;
  realizedPnl: number;
  totalTrades: number;
  balances: TokenBalance[];
  isLoading: boolean;
  privacy: boolean;
  onTogglePrivacy: () => void;
  onRefresh: () => void;
  onConnect: () => void;
  onDeposit: () => void;
  onWithdraw: () => void;
}

function TotalValueCard(props: TotalValueCardProps) {
  const {
    isConnected, hasVault, totalValue, pnl, pnlPct,
    openPositions, realizedPnl, totalTrades, balances, isLoading, privacy,
    onTogglePrivacy, onRefresh, onConnect, onDeposit, onWithdraw,
  } = props;

  const mask = (s: string) => (privacy ? "••••••" : s);

  // Top 3 non-zero balances to show as a compact strip under the number.
  const topBalances = useMemo(
    () =>
      balances
        .filter((b) => b.vaultBalance > BigInt(0))
        .sort((a, b) => Number(b.vaultBalance) - Number(a.vaultBalance))
        .slice(0, 4),
    [balances],
  );

  return (
    <section className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] p-4 md:p-5 flex flex-col gap-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            Total value
            <button
              type="button"
              onClick={onTogglePrivacy}
              aria-label={privacy ? "Show balances" : "Hide balances"}
              className="p-0.5 text-muted-foreground hover:text-foreground"
            >
              {privacy ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
            </button>
          </div>
          <div className="mt-1 text-3xl md:text-[32px] font-mono font-semibold tabular-nums text-foreground leading-none">
            {isConnected && hasVault ? mask(fmtUsd(totalValue)) : "—"}
          </div>
          {isConnected && hasVault ? (
            <div className="mt-2 flex items-center gap-2 text-[11px] font-mono tabular-nums">
              <span
                className={cn(
                  "inline-flex items-center gap-1 rounded-[var(--radius-pill)] px-1.5 py-0.5",
                  pnl >= 0 ? "bg-emerald-500/10 text-emerald-400" : "bg-red-500/10 text-red-400",
                )}
              >
                {pnl >= 0 ? "+" : ""}
                {mask(fmtUsd(pnl))}
                <span className="opacity-70">({pnlPct.toFixed(2)}%)</span>
              </span>
              <span className="text-muted-foreground">unrealised</span>
            </div>
          ) : (
            <div className="mt-2 text-[11px] text-muted-foreground">
              {isConnected ? "No vault created yet" : "Not connected"}
            </div>
          )}
        </div>

        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={onRefresh}
            disabled={!hasVault || isLoading}
            aria-label="Refresh"
            className="p-1.5 rounded-md text-muted-foreground hover:bg-white/[0.04] hover:text-foreground transition-colors disabled:opacity-40"
          >
            <RefreshCw className={cn("h-3.5 w-3.5", isLoading && "animate-spin")} />
          </button>
        </div>
      </div>

      {/* Balances strip */}
      {topBalances.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          {topBalances.map((b) => (
            <div
              key={b.address}
              className="flex items-center gap-1.5 rounded-[var(--radius-pill)] bg-surface-0 border border-white/[0.04] px-2 py-1"
            >
              <img src={coinLogoUrl(b.symbol)} alt="" className="h-4 w-4 rounded-full" />
              <span className="text-[11px] font-mono text-foreground tabular-nums">
                {(Number(b.vaultBalance) / 10 ** b.decimals).toLocaleString("en-US", {
                  maximumFractionDigits: b.decimals > 6 ? 4 : 2,
                })}
              </span>
              <span className="text-[10px] text-muted-foreground">{b.symbol}</span>
            </div>
          ))}
        </div>
      ) : null}

      {/* Stats grid */}
      <div className="grid grid-cols-3 gap-2">
        <MiniStat label="Open positions" value={openPositions.toLocaleString()} />
        <MiniStat
          label="Realized PnL"
          value={isConnected && hasVault ? mask(fmtUsd(realizedPnl)) : "—"}
          tone={realizedPnl >= 0 ? "positive" : "negative"}
          active={isConnected && hasVault && realizedPnl !== 0}
        />
        <MiniStat
          label="Trades"
          value={totalTrades.toLocaleString()}
        />
      </div>

      {/* CTA */}
      {isConnected && hasVault ? (
        <div className="grid grid-cols-2 gap-2">
          <Button
            onClick={onDeposit}
            className="w-full h-10 rounded-[var(--radius-md)]"
          >
            <ArrowDownToLine className="h-4 w-4 mr-1.5" /> Deposit
          </Button>
          <Button
            onClick={onWithdraw}
            variant="outline"
            className="w-full h-10 rounded-[var(--radius-md)]"
          >
            <ArrowUpFromLine className="h-4 w-4 mr-1.5" /> Withdraw
          </Button>
        </div>
      ) : (
        <Button
          onClick={onConnect}
          className="w-full h-10 rounded-[var(--radius-md)]"
        >
          Connect wallet
        </Button>
      )}
    </section>
  );
}

function MiniStat({
  label,
  value,
  tone,
  active,
}: {
  label: string;
  value: string;
  tone?: "positive" | "negative";
  active?: boolean;
}) {
  const colorClass = !active
    ? "text-foreground"
    : tone === "positive"
      ? "text-emerald-400"
      : tone === "negative"
        ? "text-red-400"
        : "text-foreground";
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] px-3 py-2">
      <div className="text-[9px] font-medium uppercase tracking-[0.14em] text-muted-foreground truncate">
        {label}
      </div>
      <div className={cn("mt-1 text-base font-mono font-semibold tabular-nums", colorClass)}>
        {value}
      </div>
    </div>
  );
}
