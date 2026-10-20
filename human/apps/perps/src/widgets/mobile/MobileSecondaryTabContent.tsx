"use client";

import { useState, useEffect, useCallback } from "react";
import { ExternalLink, Clock, TrendingUp, TrendingDown } from "lucide-react";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";
import { usePerfdbBalances, type PerfdbBalance } from "@/hooks/perps/usePerfdbBalances";
import { useFundingPayments } from "@/hooks/perps/useFundingPayments";
import { useTradeSettlements } from "@/hooks/perps/useAnalytics";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { computePositionPnl, type ParsedPosition } from "@/core/perps/pnlHelpers";
import {
  subgraphQuery,
  ORDERS_QUERY,
  TRADES_QUERY,
} from "@/hooks/perps/useSubgraph";
import {
  PERPS_MARKETS,
  coinLogoUrl,
  EXPLORER_URL,
  COLLATERAL_TOKENS,
} from "@/core/perps/config";
import {
  fmtUsd,
  fmtUsdCompact,
  fmtPrice,
  parse18,
  shortAddr,
} from "@/ui/utils/format";
import type { TradeSettlement, FundingPayment } from "@/types/perps";

/* ═══════════════════════════════════════════════════════════════
   Style tokens
   ═══════════════════════════════════════════════════════════════ */

const LABEL: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.12em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};

const MONO: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 11,
  color: "var(--fg)",
  fontVariantNumeric: "tabular-nums",
};

const SCROLL_STYLE: React.CSSProperties = {
  WebkitOverflowScrolling: "touch" as const,
  overscrollBehavior: "contain",
};

/* ═══════════════════════════════════════════════════════════════ */

interface MobileSecondaryTabContentProps {
  activeTab: string;
}

export function MobileSecondaryTabContent({ activeTab }: MobileSecondaryTabContentProps) {
  const { address, isConnected } = usePerpsWallet();

  if (!isConnected) {
    return (
      <div
        className="flex items-center justify-center py-10 px-4"
        style={{ ...LABEL, fontSize: 10, letterSpacing: "0.1em" }}
      >
        Connect wallet to view {activeTab.toLowerCase()}
      </div>
    );
  }

  switch (activeTab) {
    case "Balances":
      return <BalancesTab address={address!} />;
    case "Positions":
      return <PositionsTab />;
    case "Outcomes":
      return <OutcomesTab address={address!} />;
    case "Open Orders":
      return <OpenOrdersTab address={address!} />;
    case "TWAP":
      return <TwapTab />;
    case "Trade History":
      return <TradeHistoryTab address={address!} />;
    case "Funding":
      return <FundingTab address={address!} />;
    default:
      return null;
  }
}

/* ─── Shared helpers ─────────────────────────────────────────── */

function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex items-center justify-center py-10" style={LABEL}>
      <span style={{ fontSize: 10, letterSpacing: "0.1em" }}>{text}</span>
    </div>
  );
}

function LoadingState() {
  return (
    <div className="flex items-center justify-center py-10">
      <div
        className="w-4 h-4 rounded-full border-2 animate-spin"
        style={{
          borderColor: "var(--border-soft)",
          borderTopColor: "var(--accent-solid)",
        }}
      />
    </div>
  );
}

function TxLink({ hash }: { hash: string }) {
  return (
    <a
      href={`${EXPLORER_URL}/tx/${hash}`}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center"
      style={{ color: "var(--fg-subtle)" }}
    >
      <ExternalLink className="w-3 h-3" />
    </a>
  );
}

function ListHeader({ cols }: { cols: string[] }) {
  return (
    <div
      className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-4 py-2"
      style={{ ...LABEL, borderBottom: "1px solid var(--border-soft)" }}
    >
      {cols.map((c, i) => (
        <span
          key={c || i}
          className={i > 0 ? "text-right" : ""}
          style={i === cols.length - 1 && c === "" ? { width: 16 } : undefined}
        >
          {c}
        </span>
      ))}
    </div>
  );
}

function fmtTs(ts: string | null | undefined): string {
  if (!ts) return "\u2014";
  const n = Number(ts);
  const d = isNaN(n) ? new Date(ts) : new Date(n < 1e12 ? n * 1000 : n);
  if (isNaN(d.getTime())) return "\u2014";
  return d.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function getMarketSymbol(marketId: number | string): string {
  const id = typeof marketId === "string" ? parseInt(marketId, 10) : marketId;
  return PERPS_MARKETS.find((m) => m.marketId === id)?.symbol ?? `MKT-${id}`;
}

function getTokenSymbol(addr: string): string {
  const t = Object.values(COLLATERAL_TOKENS).find(
    (c) => c.address.toLowerCase() === addr?.toLowerCase()
  );
  return t?.symbol ?? shortAddr(addr);
}

function MetricCell({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div>
      <div style={LABEL}>{label}</div>
      <div style={{ ...MONO, marginTop: 1, color: color ?? "var(--fg)" }}>{value}</div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   1. BALANCES
   ═══════════════════════════════════════════════════════════════ */

function BalancesTab({ address }: { address: string }) {
  const { balances, isLoading, error } = usePerfdbBalances(address);
  const { totalVaultUsd, freeCollateral, unrealizedPnl } = usePortfolioStats();

  if (isLoading) return <LoadingState />;
  if (error) return <EmptyState text="Failed to load balances" />;

  return (
    <div className="flex flex-col overflow-y-auto" style={SCROLL_STYLE}>
      {/* Summary row */}
      <div
        className="grid grid-cols-3 gap-2 px-4 py-3"
        style={{
          background: "var(--bg-subtle)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <div>
          <div style={LABEL}>Total</div>
          <div style={{ ...MONO, fontSize: 13, fontWeight: 500, marginTop: 2 }}>
            {fmtUsd(totalVaultUsd)}
          </div>
        </div>
        <div>
          <div style={LABEL}>Available</div>
          <div style={{ ...MONO, fontSize: 13, fontWeight: 500, marginTop: 2 }}>
            {fmtUsd(freeCollateral)}
          </div>
        </div>
        <div>
          <div style={LABEL}>uPnL</div>
          <div
            style={{
              ...MONO,
              fontSize: 13,
              fontWeight: 500,
              marginTop: 2,
              color: unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)",
            }}
          >
            {unrealizedPnl >= 0 ? "+" : ""}{fmtUsd(unrealizedPnl)}
          </div>
        </div>
      </div>

      {balances.length === 0 ? (
        <EmptyState text="No token balances" />
      ) : (
        <div className="divide-y" style={{ borderColor: "var(--border-soft)" }}>
          {balances.map((b) => (
            <BalanceRow key={b.token} balance={b} />
          ))}
        </div>
      )}
    </div>
  );
}

function BalanceRow({ balance }: { balance: PerfdbBalance }) {
  const tokenName = getTokenSymbol(balance.token);
  const bal = parseFloat(balance.balance || "0");
  const deposited = parseFloat(balance.totalDeposited || "0");
  const withdrawn = parseFloat(balance.totalWithdrawn || "0");

  return (
    <div className="flex items-center justify-between px-4 py-3">
      <div className="flex items-center gap-2.5">
        <div
          className="w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0"
          style={{
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
            color: "var(--fg-muted)",
          }}
        >
          {tokenName.slice(0, 2)}
        </div>
        <div>
          <div style={{ ...MONO, fontSize: 12, fontWeight: 500 }}>{tokenName}</div>
          <div style={{ ...LABEL, marginTop: 1 }}>
            {balance.depositCount} dep / {balance.withdrawCount} wd
          </div>
        </div>
      </div>
      <div className="text-right">
        <div style={{ ...MONO, fontSize: 12, fontWeight: 500 }}>{fmtUsd(bal)}</div>
        <div style={{ ...LABEL, marginTop: 1 }}>
          +{fmtUsdCompact(deposited)} / -{fmtUsdCompact(withdrawn)}
        </div>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   2. POSITIONS
   ═══════════════════════════════════════════════════════════════ */

function PositionsTab() {
  const { positions, isLoading } = usePortfolioStats();
  const { tickers } = useRealtimePrices();

  if (isLoading) return <LoadingState />;
  if (positions.length === 0) return <EmptyState text="No open positions" />;

  return (
    <div className="flex flex-col gap-2 p-3 overflow-y-auto" style={SCROLL_STYLE}>
      {positions.map((pos: any) => {
        const symbol = getMarketSymbol(pos.marketId);
        const markPrice = tickers[symbol]?.mark_price ?? 0;
        return (
          <PositionCard key={pos.positionId} pos={pos} symbol={symbol} markPrice={markPrice} />
        );
      })}
    </div>
  );
}

function PositionCard({ pos, symbol, markPrice }: { pos: any; symbol: string; markPrice: number }) {
  const entryPrice = parse18(pos.entryPrice);
  const sizeUsd = parse18(pos.sizeUsd);
  const leverage = parse18(pos.leverage);
  const collateralUsd = parse18(pos.collateralUsd);

  const parsed: ParsedPosition = {
    positionId: BigInt(pos.positionId),
    marketId: Number(pos.marketId),
    symbol,
    isLong: pos.isLong,
    size: sizeUsd,
    collateral: collateralUsd,
    leverage,
    entryPrice,
    collateralToken: pos.collateralToken ?? "",
    collateralAmount: parse18(pos.collateralAmount),
    openedAt: pos.openedAt,
    openTxHash: pos.openTxHash,
  };

  const pnlData = markPrice > 0 ? computePositionPnl(parsed, markPrice) : null;
  const pnlColor = (pnlData?.unrealizedPnl ?? 0) >= 0 ? "var(--success)" : "var(--danger)";
  const sideColor = pos.isLong ? "var(--success)" : "var(--danger)";

  return (
    <div
      className="rounded-xl p-3"
      style={{
        background: "var(--bg-subtle)",
        border: "1px solid var(--border-soft)",
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <img src={coinLogoUrl(symbol)} alt={symbol} className="w-6 h-6 rounded-full shrink-0" />
          <span style={{ ...MONO, fontSize: 12, fontWeight: 600 }}>{symbol}/USD</span>
          <span
            className="px-1.5 py-0.5 rounded text-[9px]"
            style={{
              fontFamily: "var(--font-mono)",
              letterSpacing: "0.08em",
              textTransform: "uppercase",
              fontWeight: 600,
              color: sideColor,
              background: pos.isLong ? "rgba(5,193,104,0.12)" : "rgba(255,90,101,0.12)",
            }}
          >
            {pos.isLong ? "Long" : "Short"} {leverage.toFixed(0)}x
          </span>
        </div>
        {pnlData && (
          <div className="text-right">
            <div style={{ ...MONO, fontSize: 13, fontWeight: 600, color: pnlColor }}>
              {pnlData.unrealizedPnl >= 0 ? "+" : ""}{fmtUsd(pnlData.unrealizedPnl)}
            </div>
            <div style={{ ...MONO, fontSize: 9, color: pnlColor }}>
              {pnlData.roe >= 0 ? "+" : ""}{pnlData.roe.toFixed(2)}%
            </div>
          </div>
        )}
      </div>

      {/* Metrics */}
      <div className="grid grid-cols-3 gap-2 mt-1">
        <MetricCell label="Size" value={fmtUsdCompact(sizeUsd)} />
        <MetricCell label="Entry" value={`$${fmtPrice(entryPrice)}`} />
        <MetricCell label="Mark" value={markPrice > 0 ? `$${fmtPrice(markPrice)}` : "\u2014"} />
      </div>

      {pnlData && (
        <div className="grid grid-cols-2 gap-2 mt-2 pt-2" style={{ borderTop: "1px solid var(--border-soft)" }}>
          <MetricCell
            label="Liq. Price"
            value={pnlData.liqPrice > 0 ? `$${fmtPrice(pnlData.liqPrice)}` : "\u2014"}
            color="var(--danger)"
          />
          <MetricCell label="Collateral" value={fmtUsd(pnlData.initialCollateral)} />
        </div>
      )}
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   3. OUTCOMES (Trade Settlements)
   ═══════════════════════════════════════════════════════════════ */

function OutcomesTab({ address }: { address: string }) {
  const { data: settlements, isLoading } = useTradeSettlements({
    userAddress: address,
    limit: 30,
  });

  if (isLoading) return <LoadingState />;
  if (!settlements || settlements.length === 0) return <EmptyState text="No outcomes yet" />;

  return (
    <div className="flex flex-col overflow-y-auto" style={SCROLL_STYLE}>
      <ListHeader cols={["Market", "Size", "PnL", ""]} />
      <div className="divide-y" style={{ borderColor: "var(--border-soft)" }}>
        {settlements.map((s: TradeSettlement) => (
          <OutcomeRow key={s.id} s={s} />
        ))}
      </div>
    </div>
  );
}

function OutcomeRow({ s }: { s: TradeSettlement }) {
  const symbol = getMarketSymbol(s.marketId);
  const grossPnl = parse18(s.grossPnl);
  const pnlColor = grossPnl >= 0 ? "var(--success)" : "var(--danger)";
  const size = parse18(s.sizeUsd);
  const execPrice = parse18(s.executionPrice);

  return (
    <div className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-4 py-2.5 items-center">
      <div className="flex items-center gap-2 min-w-0">
        <img src={coinLogoUrl(symbol)} alt="" className="w-5 h-5 rounded-full shrink-0" />
        <div className="min-w-0">
          <div style={{ ...MONO, fontSize: 11, fontWeight: 500 }}>{symbol}/USD</div>
          <div style={{ ...LABEL, fontSize: 8 }}>
            @${fmtPrice(execPrice)} · {fmtTs(s.blockTimestamp)}
          </div>
        </div>
      </div>
      <div className="text-right w-16" style={MONO}>{fmtUsdCompact(size)}</div>
      <div className="text-right w-16" style={{ ...MONO, color: pnlColor, fontWeight: 500 }}>
        {grossPnl >= 0 ? "+" : ""}{fmtUsdCompact(grossPnl)}
      </div>
      <div className="w-4">
        {s.txHash && <TxLink hash={s.txHash} />}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   4. OPEN ORDERS
   ═══════════════════════════════════════════════════════════════ */

function OpenOrdersTab({ address }: { address: string }) {
  const [orders, setOrders] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchOrders = useCallback(async () => {
    try {
      setLoading(true);
      const res = await subgraphQuery<{ orders: any[] }>(ORDERS_QUERY, {
        userAddress: address,
        status: "active",
      });
      setOrders(res.orders ?? []);
    } catch {
      setOrders([]);
    } finally {
      setLoading(false);
    }
  }, [address]);

  useEffect(() => { fetchOrders(); }, [fetchOrders]);

  if (loading) return <LoadingState />;
  if (orders.length === 0) return <EmptyState text="No open orders" />;

  return (
    <div className="flex flex-col overflow-y-auto" style={SCROLL_STYLE}>
      <ListHeader cols={["Market / Type", "Side", "Price", "Size"]} />
      <div className="divide-y" style={{ borderColor: "var(--border-soft)" }}>
        {orders.map((o: any) => (
          <OrderRow key={o.orderId} o={o} />
        ))}
      </div>
    </div>
  );
}

function OrderRow({ o }: { o: any }) {
  const symbol = getMarketSymbol(o.marketId);
  const trigPrice = parse18(o.triggerPrice);
  const size = parse18(o.sizeUsd);
  const sideColor = o.isLong ? "var(--success)" : "var(--danger)";
  const typeLabel = o.orderTypeName ?? (
    o.orderType === 0 ? "Limit"
    : o.orderType === 1 ? "Stop"
    : o.orderType === 2 ? "TP"
    : o.orderType === 3 ? "SL"
    : "Order"
  );

  return (
    <div className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-4 py-2.5 items-center">
      <div className="min-w-0">
        <div className="flex items-center gap-1.5">
          <img src={coinLogoUrl(symbol)} alt="" className="w-5 h-5 rounded-full shrink-0" />
          <span style={{ ...MONO, fontSize: 11, fontWeight: 500 }}>{symbol}/USD</span>
        </div>
        <div style={{ ...LABEL, fontSize: 8, marginTop: 1 }}>
          {typeLabel} · {fmtTs(o.placedAt)}
        </div>
      </div>
      <div
        className="text-right w-14"
        style={{
          ...MONO,
          fontSize: 10,
          fontWeight: 600,
          color: sideColor,
          textTransform: "uppercase",
          letterSpacing: "0.08em",
        }}
      >
        {o.isLong ? "Long" : "Short"}
      </div>
      <div className="text-right w-16" style={MONO}>${fmtPrice(trigPrice)}</div>
      <div className="text-right w-14" style={MONO}>{fmtUsdCompact(size)}</div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   5. TWAP
   ═══════════════════════════════════════════════════════════════ */

function TwapTab() {
  return (
    <div className="flex flex-col items-center justify-center py-12 gap-3 px-6">
      <Clock className="w-8 h-8" style={{ color: "var(--fg-subtle)", opacity: 0.5 }} />
      <div
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 14,
          fontWeight: 500,
          color: "var(--fg-muted)",
        }}
      >
        TWAP Orders
      </div>
      <div
        className="text-center"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          color: "var(--fg-subtle)",
          letterSpacing: "0.04em",
          lineHeight: 1.5,
        }}
      >
        Time-Weighted Average Price orders execute large trades
        incrementally over a set duration. Coming soon.
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   6. TRADE HISTORY
   ═══════════════════════════════════════════════════════════════ */

function TradeHistoryTab({ address }: { address: string }) {
  const [trades, setTrades] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchTrades = useCallback(async () => {
    try {
      setLoading(true);
      const res = await subgraphQuery<{ trades: any[] }>(TRADES_QUERY, {
        userAddress: address,
        limit: 50,
      });
      setTrades(res.trades ?? []);
    } catch {
      setTrades([]);
    } finally {
      setLoading(false);
    }
  }, [address]);

  useEffect(() => { fetchTrades(); }, [fetchTrades]);

  if (loading) return <LoadingState />;
  if (trades.length === 0) return <EmptyState text="No trade history" />;

  return (
    <div className="flex flex-col overflow-y-auto" style={SCROLL_STYLE}>
      <ListHeader cols={["Market", "Price", "PnL", ""]} />
      <div className="divide-y" style={{ borderColor: "var(--border-soft)" }}>
        {trades.map((t: any, i: number) => (
          <TradeRow key={t.id ?? i} t={t} />
        ))}
      </div>
    </div>
  );
}

function TradeRow({ t }: { t: any }) {
  const symbol = getMarketSymbol(t.marketId);
  const price = parse18(t.price);
  const realizedPnl = parse18(t.realizedPnl);
  const size = parse18(t.sizeUsd);
  const pnlColor = realizedPnl >= 0 ? "var(--success)" : "var(--danger)";

  return (
    <div className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-4 py-2.5 items-center">
      <div className="flex items-center gap-2 min-w-0">
        <img src={coinLogoUrl(symbol)} alt="" className="w-5 h-5 rounded-full shrink-0" />
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <span style={{ ...MONO, fontSize: 11, fontWeight: 500 }}>{symbol}/USD</span>
            {t.isLong !== undefined && (
              t.isLong
                ? <TrendingUp className="w-3 h-3" style={{ color: "var(--success)" }} />
                : <TrendingDown className="w-3 h-3" style={{ color: "var(--danger)" }} />
            )}
          </div>
          <div style={{ ...LABEL, fontSize: 8 }}>
            {fmtUsdCompact(size)} · {fmtTs(t.blockTimestamp)}
          </div>
        </div>
      </div>
      <div className="text-right w-14" style={MONO}>${fmtPrice(price)}</div>
      <div className="text-right w-14" style={{ ...MONO, color: pnlColor, fontWeight: 500 }}>
        {realizedPnl !== 0 ? `${realizedPnl >= 0 ? "+" : ""}${fmtUsdCompact(realizedPnl)}` : "\u2014"}
      </div>
      <div className="w-4">
        {t.txHash && <TxLink hash={t.txHash} />}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
   7. FUNDING
   ═══════════════════════════════════════════════════════════════ */

function FundingTab({ address }: { address: string }) {
  const { data: payments, isLoading } = useFundingPayments({
    userAddress: address,
    limit: 50,
  });

  if (isLoading) return <LoadingState />;
  if (!payments || payments.length === 0) return <EmptyState text="No funding payments" />;

  return (
    <div className="flex flex-col overflow-y-auto" style={SCROLL_STYLE}>
      <ListHeader cols={["Market", "Payment", "Collateral", ""]} />
      <div className="divide-y" style={{ borderColor: "var(--border-soft)" }}>
        {payments.map((p: FundingPayment) => (
          <FundingRow key={p.id} p={p} />
        ))}
      </div>
    </div>
  );
}

function FundingRow({ p }: { p: FundingPayment }) {
  const symbol = getMarketSymbol(p.marketId);
  const payment = parse18(p.fundingPaymentUsd);
  const newCollateral = parse18(p.newCollateralUsd);
  const paymentColor = payment >= 0 ? "var(--success)" : "var(--danger)";

  return (
    <div className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-4 py-2.5 items-center">
      <div className="flex items-center gap-2 min-w-0">
        <img src={coinLogoUrl(symbol)} alt="" className="w-5 h-5 rounded-full shrink-0" />
        <div className="min-w-0">
          <div style={{ ...MONO, fontSize: 11, fontWeight: 500 }}>{symbol}/USD</div>
          <div style={{ ...LABEL, fontSize: 8 }}>
            #{String(p.positionId).slice(0, 8)} · {fmtTs(p.blockTimestamp)}
          </div>
        </div>
      </div>
      <div className="text-right w-16" style={{ ...MONO, color: paymentColor, fontWeight: 500 }}>
        {payment >= 0 ? "+" : ""}{fmtUsd(payment)}
      </div>
      <div className="text-right w-16" style={MONO}>{fmtUsdCompact(newCollateral)}</div>
      <div className="w-4">
        {p.txHash && <TxLink hash={p.txHash} />}
      </div>
    </div>
  );
}
