"use client";

import React, { useEffect, useMemo, useState } from "react";
import { ExternalLink } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useSpotPortfolio } from "@/hooks/spot/useSpotPortfolio";
import {
  spotApi,
  dStr,
  dStrToToken,
  dStrToUsd,
  symbolOrShort,
  type SpotClosedLeg,
  type SpotOrder,
  type SpotPosition,
  type SpotTrade,
} from "@/core/spot/api";
import { paxeerChain } from "@/core/perps/config";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";

/**
 * Render a fetch failure as a friendly inline string. Always classifies
 * first so a raw `err.message` never leaks into the panel; also reports
 * to Sentry for triage. Returns the string to set into local state.
 */
function classifyFetchError(e: unknown, ctx: string): string {
  const ufe = classifyError(e, ctx);
  void reportError(ufe);
  return ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message;
}

/* ══════════════════════════════════════════════════════════════════════
   SpotPositionsPanel — five tabs:
     · Holdings        (live ERC-20 + native PAX, PAXSCAN-truthed)
     · Open Orders     (limit / stop-limit / stop-loss, status open)
     · Order History   (all statuses, pagination via limit param)
     · Trade History   (keyset: nextCursor.afterBlock + afterIndex)
     · Closed Legs     (keyset: nextCursor.afterId)
   ══════════════════════════════════════════════════════════════════════ */

type TabKey =
  | "holdings"
  | "openOrders"
  | "orderHistory"
  | "tradeHistory"
  | "closedLegs";

const TAB_LABELS: Record<TabKey, string> = {
  holdings: "Holdings",
  openOrders: "Open Orders",
  orderHistory: "Order History",
  tradeHistory: "Trade History",
  closedLegs: "Closed Legs",
};

const TAB_ORDER: TabKey[] = [
  "holdings",
  "openOrders",
  "orderHistory",
  "tradeHistory",
  "closedLegs",
];

const explorerBase = paxeerChain.blockExplorers?.default.url ?? "";

// ── Format helpers ────────────────────────────────────────────────────

function fmtTokenAmt(v: number, sym: string): string {
  if (!Number.isFinite(v) || v === 0) return `0 ${sym}`;
  if (Math.abs(v) >= 1_000_000) return `${(v / 1_000_000).toFixed(4)}M ${sym}`;
  return `${v.toLocaleString("en-US", {
    maximumFractionDigits: Math.abs(v) < 1 ? 6 : 4,
  })} ${sym}`;
}

function fmtUsd(v: number): string {
  if (!Number.isFinite(v)) return "—";
  return `$${v.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function fmtPct(pct: number): string {
  if (!Number.isFinite(pct)) return "—";
  return `${pct >= 0 ? "+" : ""}${pct.toFixed(2)}%`;
}

function fmtTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function shortHash(h: string | null | undefined): string {
  if (!h) return "—";
  return `${h.slice(0, 8)}…${h.slice(-6)}`;
}

// ── Top-level component ──────────────────────────────────────────────

export function SpotPositionsPanel() {
  const { isConnected, connect, address } = usePerpsWallet();
  const portfolio = useSpotPortfolio(address);
  const [activeTab, setActiveTab] = useState<TabKey>("holdings");

  if (!isConnected) {
    return (
      <div className="trading-panel flex flex-col h-full overflow-hidden">
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border shrink-0">
          <span className="text-xs font-mono uppercase tracking-wider text-muted-foreground">
            Spot Account
          </span>
        </div>
        <div className="flex-1 flex flex-col items-center justify-center gap-3">
          <span className="text-sm text-muted-foreground">
            Connect wallet to view spot positions
          </span>
          <button
            type="button"
            onClick={connect}
            className="px-4 py-2 rounded-[var(--radius-md)] text-xs font-semibold text-white transition-all hover:brightness-110"
            style={{
              background:
                "linear-gradient(135deg, var(--brand-blue), var(--brand-blue-dark, var(--brand-blue)))",
            }}
          >
            Connect Wallet
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="trading-panel flex flex-col h-full overflow-hidden">
      <div className="flex items-center gap-1 px-3 py-2 border-b border-border shrink-0 overflow-x-auto scrollbar-none">
        {TAB_ORDER.map((tab) => (
          <button
            key={tab}
            type="button"
            onClick={() => setActiveTab(tab)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium rounded-[var(--radius-md)] transition-colors whitespace-nowrap",
              activeTab === tab
                ? "bg-surface-2 text-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-surface-1",
            )}
          >
            {TAB_LABELS[tab]}
          </button>
        ))}
      </div>

      <div className="flex-1 overflow-hidden flex flex-col">
        {activeTab === "holdings" ? (
          <HoldingsTab portfolio={portfolio} />
        ) : activeTab === "openOrders" ? (
          <OpenOrdersTab orders={portfolio.openOrders} />
        ) : activeTab === "orderHistory" ? (
          <OrderHistoryTab address={address ?? null} />
        ) : activeTab === "tradeHistory" ? (
          <TradeHistoryTab
            address={address ?? null}
            initial={portfolio.recentTrades}
          />
        ) : (
          <ClosedLegsTab address={address ?? null} />
        )}
      </div>
    </div>
  );
}

// ── Holdings tab ──────────────────────────────────────────────────────

function HoldingsTab({
  portfolio,
}: {
  portfolio: ReturnType<typeof useSpotPortfolio>;
}) {
  const positionsByToken = useMemo(() => {
    const map = new Map<string, SpotPosition>();
    if (!Array.isArray(portfolio.positions)) return map;
    for (const p of portfolio.positions) {
      if (!p || typeof p.token !== "string") continue;
      map.set(p.token.toLowerCase(), p);
    }
    return map;
  }, [portfolio.positions]);

  if (portfolio.isLoading && !portfolio.holdings) {
    return <LoadingRows />;
  }
  if (!portfolio.holdings) {
    return <Empty title="No holdings to display" />;
  }

  const rows: Array<{
    token: string;
    symbol: string;
    name: string | null;
    decimals: number;
    qty: number;
    isStablecoin: boolean;
    avgEntry: number | null;
    realizedUsd: number | null;
  }> = [];

  // Native PAX
  const nativeQty = dStrToToken(portfolio.holdings.native.balance, 18);
  if (nativeQty > 0) {
    rows.push({
      token: "native:pax",
      symbol: "PAX",
      name: "Paxeer Network",
      decimals: 18,
      qty: nativeQty,
      isStablecoin: false,
      avgEntry: null,
      realizedUsd: null,
    });
  }

  const erc20List = Array.isArray(portfolio.holdings.erc20) ? portfolio.holdings.erc20 : [];
  for (const e of erc20List) {
    if (!e || typeof e.token !== "string") continue;
    const dec =
      typeof e.decimals === "number"
        ? e.decimals
        : typeof e.decimals === "string"
          ? parseInt(e.decimals, 10) || 0
          : 0;
    const qty = dStrToToken(e.balance, dec);
    if (qty <= 0) continue;
    const pos = positionsByToken.get(e.token.toLowerCase());
    rows.push({
      token: e.token,
      symbol: symbolOrShort(e.token, e.symbol),
      name: e.name,
      decimals: dec,
      qty,
      isStablecoin: !!pos?.isStablecoin,
      avgEntry: pos
        ? dStr(pos.derived.avgEntryPrice, 0)
        : null,
      realizedUsd: pos ? dStrToUsd(pos.derived.realizedPnlUsd, 0) : null,
    });
  }

  if (rows.length === 0) {
    return <Empty title="No non-zero balances yet" />;
  }

  return (
    <div className="flex-1 overflow-auto scrollbar-custom">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-card z-10">
          <tr className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground border-b border-border">
            <th className="text-left px-4 py-2 font-medium">Asset</th>
            <th className="text-right px-4 py-2 font-medium">Qty</th>
            <th className="text-right px-4 py-2 font-medium">Avg Entry</th>
            <th className="text-right px-4 py-2 font-medium">Realized PnL</th>
            <th className="text-right px-4 py-2 font-medium">Token</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr
              key={r.token}
              className="border-b border-white/[0.04] hover:bg-white/[0.02]"
            >
              <td className="px-4 py-2.5">
                <div className="flex flex-col">
                  <span className="font-semibold text-foreground flex items-center gap-1.5">
                    {r.symbol}
                    {r.isStablecoin ? (
                      <span className="text-[8px] font-mono uppercase tracking-wider text-emerald-400 bg-emerald-500/10 px-1 py-0.5 rounded-full">
                        STABLE
                      </span>
                    ) : null}
                  </span>
                  {r.name ? (
                    <span className="text-[10px] text-muted-foreground truncate">
                      {r.name}
                    </span>
                  ) : null}
                </div>
              </td>
              <td className="px-4 py-2.5 text-right tabular-nums font-mono text-foreground">
                {fmtTokenAmt(r.qty, r.symbol)}
              </td>
              <td className="px-4 py-2.5 text-right tabular-nums font-mono text-muted-foreground">
                {r.avgEntry && r.avgEntry > 0 ? fmtUsd(r.avgEntry) : "—"}
              </td>
              <td
                className={cn(
                  "px-4 py-2.5 text-right tabular-nums font-mono",
                  r.realizedUsd == null
                    ? "text-muted-foreground"
                    : r.realizedUsd >= 0
                      ? "text-emerald-400"
                      : "text-red-400",
                )}
              >
                {r.realizedUsd == null
                  ? "—"
                  : `${r.realizedUsd >= 0 ? "+" : ""}${fmtUsd(r.realizedUsd)}`}
              </td>
              <td className="px-4 py-2.5 text-right">
                {r.token.startsWith("native") ? (
                  <span className="text-[10px] text-muted-foreground/60 font-mono">
                    native
                  </span>
                ) : (
                  <a
                    href={`${explorerBase}/token/${r.token}`}
                    target="_blank"
                    rel="noreferrer noopener"
                    className="inline-flex items-center gap-1 text-[10px] font-mono text-muted-foreground hover:text-foreground"
                  >
                    {`${r.token.slice(0, 6)}…${r.token.slice(-4)}`}
                    <ExternalLink className="w-2.5 h-2.5" />
                  </a>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── Open Orders tab ──────────────────────────────────────────────────

function OpenOrdersTab({ orders }: { orders: SpotOrder[] }) {
  if (orders.length === 0) {
    return <Empty title="No open spot orders" />;
  }
  return (
    <div className="flex-1 overflow-auto scrollbar-custom">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-card z-10">
          <tr className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground border-b border-border">
            <th className="text-left px-4 py-2 font-medium">Type</th>
            <th className="text-left px-4 py-2 font-medium">Side</th>
            <th className="text-left px-4 py-2 font-medium">Asset</th>
            <th className="text-right px-4 py-2 font-medium">Amount</th>
            <th className="text-right px-4 py-2 font-medium">Price</th>
            <th className="text-right px-4 py-2 font-medium">Placed</th>
            <th className="text-right px-4 py-2 font-medium">Tx</th>
          </tr>
        </thead>
        <tbody>
          {orders.map((o) => {
            const dec = o.decimals ?? 18;
            const amount = dStrToToken(o.amount, dec);
            const price = dStr(o.limitPrice ?? o.targetPrice ?? o.triggerPrice, 0);
            const isBuy = o.isBuy === true;
            return (
              <tr key={String(o.id)} className="border-b border-white/[0.04] hover:bg-white/[0.02]">
                <td className="px-4 py-2.5 font-mono text-[11px] text-muted-foreground uppercase">
                  {o.type.replace("_", " ")}
                </td>
                <td className="px-4 py-2.5">
                  <span
                    className={cn(
                      "text-[10px] font-mono font-semibold px-2 py-0.5 rounded-full",
                      isBuy
                        ? "bg-emerald-500/10 text-emerald-400"
                        : "bg-red-500/10 text-red-400",
                    )}
                  >
                    {isBuy ? "Buy" : "Sell"}
                  </span>
                </td>
                <td className="px-4 py-2.5 font-semibold text-foreground">
                  {o.symbol ?? (o.token ? `${o.token.slice(0, 6)}…` : "—")}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                  {amount > 0 ? fmtTokenAmt(amount, o.symbol ?? "") : "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                  {price > 0 ? fmtUsd(price) : "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono text-muted-foreground">
                  {fmtTime(o.placedAt)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <TxLink hash={o.placedTxHash} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ── Order History tab ─────────────────────────────────────────────────

function OrderHistoryTab({ address }: { address: string | null }) {
  const [orders, setOrders] = useState<SpotOrder[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!address) {
      setOrders([]);
      return;
    }
    const ctrl = new AbortController();
    setLoading(true);
    setError(null);
    spotApi
      .getOrders(address, { status: "all", limit: 200, signal: ctrl.signal })
      .then((res) => {
        if (!ctrl.signal.aborted) setOrders(res.items);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(classifyFetchError(e, "spot/orders"));
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
  }, [address]);

  if (loading && !orders) return <LoadingRows />;
  if (error) return <Empty title="Failed to load order history" subtitle={error} />;
  if (!orders || orders.length === 0) return <Empty title="No order history" />;

  return (
    <div className="flex-1 overflow-auto scrollbar-custom">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-card z-10">
          <tr className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground border-b border-border">
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Type</th>
            <th className="text-left px-4 py-2 font-medium">Side</th>
            <th className="text-left px-4 py-2 font-medium">Asset</th>
            <th className="text-right px-4 py-2 font-medium">Amount</th>
            <th className="text-right px-4 py-2 font-medium">Limit / Trigger</th>
            <th className="text-right px-4 py-2 font-medium">Exec Px</th>
            <th className="text-right px-4 py-2 font-medium">When</th>
            <th className="text-right px-4 py-2 font-medium">Tx</th>
          </tr>
        </thead>
        <tbody>
          {orders.map((o) => {
            const dec = o.decimals ?? 18;
            const amount = dStrToToken(o.amount, dec);
            const isBuy = o.isBuy === true;
            const trigger = dStr(o.limitPrice ?? o.targetPrice ?? o.triggerPrice, 0);
            const exec = dStr(o.executionPrice, 0);
            const ts = o.executedAt ?? o.cancelledAt ?? o.placedAt;
            const tx = o.executedTxHash ?? o.placedTxHash;
            const statusColor =
              o.status === "executed"
                ? "text-emerald-400 bg-emerald-500/10"
                : o.status === "cancelled"
                  ? "text-muted-foreground bg-white/[0.04]"
                  : "text-amber-400 bg-amber-500/10";
            return (
              <tr key={String(o.id)} className="border-b border-white/[0.04] hover:bg-white/[0.02]">
                <td className="px-4 py-2.5">
                  <span className={cn("text-[10px] font-mono font-semibold px-2 py-0.5 rounded-full uppercase", statusColor)}>
                    {o.status}
                  </span>
                </td>
                <td className="px-4 py-2.5 font-mono text-[11px] text-muted-foreground uppercase">
                  {o.type.replace("_", " ")}
                </td>
                <td className="px-4 py-2.5">
                  <span
                    className={cn(
                      "text-[10px] font-mono font-semibold px-2 py-0.5 rounded-full",
                      isBuy ? "bg-emerald-500/10 text-emerald-400" : "bg-red-500/10 text-red-400",
                    )}
                  >
                    {isBuy ? "Buy" : "Sell"}
                  </span>
                </td>
                <td className="px-4 py-2.5 font-semibold text-foreground">
                  {o.symbol ?? (o.token ? `${o.token.slice(0, 6)}…` : "—")}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                  {amount > 0 ? fmtTokenAmt(amount, o.symbol ?? "") : "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                  {trigger > 0 ? fmtUsd(trigger) : "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                  {exec > 0 ? fmtUsd(exec) : "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums font-mono text-muted-foreground">
                  {fmtTime(ts)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <TxLink hash={tx} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ── Trade History tab ────────────────────────────────────────────────

function TradeHistoryTab({
  address,
  initial,
}: {
  address: string | null;
  initial: SpotTrade[];
}) {
  const [trades, setTrades] = useState<SpotTrade[]>(initial);
  const [cursor, setCursor] = useState<{
    afterBlock: string;
    afterIndex: number;
  } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [exhausted, setExhausted] = useState(false);

  // Sync with snapshot from `useSpotPortfolio` whenever it changes (it's
  // pushed by the WS layer, so this picks up new fills automatically).
  useEffect(() => {
    setTrades(initial);
    setCursor(null);
    setExhausted(false);
  }, [initial]);

  // Initial cursor — needs to come from the trades response, which the
  // composite hook discards. Pull it once here.
  useEffect(() => {
    if (!address) return;
    const ctrl = new AbortController();
    spotApi
      .getTrades(address, { limit: 50, signal: ctrl.signal })
      .then((res) => {
        if (ctrl.signal.aborted) return;
        if (res.items.length > 0) setTrades(res.items);
        setCursor(res.nextCursor);
        if (res.nextCursor === null) setExhausted(true);
      })
      .catch(() => {});
    return () => ctrl.abort();
  }, [address]);

  const loadMore = async () => {
    if (!address || !cursor || loading || exhausted) return;
    setLoading(true);
    setError(null);
    try {
      const res = await spotApi.getTrades(address, {
        limit: 50,
        afterBlock: cursor.afterBlock,
        afterIndex: cursor.afterIndex,
      });
      setTrades((prev) => [...prev, ...res.items]);
      setCursor(res.nextCursor);
      if (res.nextCursor === null) setExhausted(true);
    } catch (e) {
      setError(classifyFetchError(e, "spot/trades"));
    } finally {
      setLoading(false);
    }
  };

  if (trades.length === 0 && !loading) return <Empty title="No trade history" />;

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div className="flex-1 overflow-auto scrollbar-custom">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card z-10">
            <tr className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground border-b border-border">
              <th className="text-left px-4 py-2 font-medium">When</th>
              <th className="text-left px-4 py-2 font-medium">Pair</th>
              <th className="text-right px-4 py-2 font-medium">In</th>
              <th className="text-right px-4 py-2 font-medium">Out</th>
              <th className="text-right px-4 py-2 font-medium">Exec Px</th>
              <th className="text-right px-4 py-2 font-medium">Volume</th>
              <th className="text-left px-4 py-2 font-medium">Source</th>
              <th className="text-right px-4 py-2 font-medium">Tx</th>
            </tr>
          </thead>
          <tbody>
            {trades.map((t) => {
              const exec = dStr(t.executionPrice, 0);
              const vol = dStrToUsd(t.volumeUsd, 0);
              const inAmt = dStr(t.amountIn, 0);
              const outAmt = dStr(t.amountOut, 0);
              return (
                <tr key={String(t.id)} className="border-b border-white/[0.04] hover:bg-white/[0.02]">
                  <td className="px-4 py-2.5 tabular-nums font-mono text-muted-foreground">
                    {fmtTime(t.blockTimestamp)}
                  </td>
                  <td className="px-4 py-2.5 font-mono text-[11px] text-muted-foreground">
                    {`${t.tokenIn.slice(0, 6)}… → ${t.tokenOut.slice(0, 6)}…`}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {inAmt > 0 ? inAmt.toLocaleString("en-US", { maximumFractionDigits: 6 }) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {outAmt > 0 ? outAmt.toLocaleString("en-US", { maximumFractionDigits: 6 }) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {exec > 0 ? fmtUsd(exec) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {vol > 0 ? fmtUsd(vol) : "—"}
                  </td>
                  <td className="px-4 py-2.5 font-mono text-[11px] text-muted-foreground uppercase">
                    {t.source}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <TxLink hash={t.txHash} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <Pager
        loading={loading}
        exhausted={exhausted}
        onMore={loadMore}
        error={error}
      />
    </div>
  );
}

// ── Closed Legs tab ──────────────────────────────────────────────────

function ClosedLegsTab({ address }: { address: string | null }) {
  const [legs, setLegs] = useState<SpotClosedLeg[]>([]);
  const [cursor, setCursor] = useState<{ afterId: number } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [exhausted, setExhausted] = useState(false);
  const [hasFetched, setHasFetched] = useState(false);

  useEffect(() => {
    if (!address) return;
    const ctrl = new AbortController();
    setLoading(true);
    spotApi
      .getClosedLegs(address, { limit: 50, signal: ctrl.signal })
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setLegs(res.items);
        setCursor(res.nextCursor);
        if (res.nextCursor === null) setExhausted(true);
        setHasFetched(true);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted)
          setError(classifyFetchError(e, "spot/closed-legs-initial"));
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
  }, [address]);

  const loadMore = async () => {
    if (!address || !cursor || loading || exhausted) return;
    setLoading(true);
    setError(null);
    try {
      const res = await spotApi.getClosedLegs(address, {
        limit: 50,
        afterId: cursor.afterId,
      });
      setLegs((prev) => [...prev, ...res.items]);
      setCursor(res.nextCursor);
      if (res.nextCursor === null) setExhausted(true);
    } catch (e) {
      setError(classifyFetchError(e, "spot/closed-legs"));
    } finally {
      setLoading(false);
    }
  };

  if (loading && !hasFetched) return <LoadingRows />;
  if (legs.length === 0) return <Empty title="No closed legs yet" />;

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div className="flex-1 overflow-auto scrollbar-custom">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card z-10">
            <tr className="text-[10px] font-mono uppercase tracking-[0.12em] text-muted-foreground border-b border-border">
              <th className="text-left px-4 py-2 font-medium">Closed</th>
              <th className="text-left px-4 py-2 font-medium">Asset</th>
              <th className="text-right px-4 py-2 font-medium">Qty</th>
              <th className="text-right px-4 py-2 font-medium">Entry</th>
              <th className="text-right px-4 py-2 font-medium">Exit</th>
              <th className="text-right px-4 py-2 font-medium">PnL</th>
              <th className="text-right px-4 py-2 font-medium">PnL %</th>
              <th className="text-right px-4 py-2 font-medium">Hold</th>
              <th className="text-right px-4 py-2 font-medium">Tx</th>
            </tr>
          </thead>
          <tbody>
            {legs.map((leg) => {
              const dec = leg.decimals ?? 18;
              const qty = dStrToToken(leg.qty, dec);
              const entry = dStr(leg.entryPrice, 0);
              const exit = dStr(leg.exitPrice, 0);
              const pnlUsd = dStrToUsd(leg.pnlUsd, 0);
              const pnlPct =
                leg.pnlPct == null ? null : dStr(leg.pnlPct, 0) * 100;
              const hold =
                typeof leg.holdingSeconds === "number"
                  ? leg.holdingSeconds
                  : typeof leg.holdingSeconds === "string"
                    ? parseInt(leg.holdingSeconds, 10) || 0
                    : 0;
              return (
                <tr key={leg.id} className="border-b border-white/[0.04] hover:bg-white/[0.02]">
                  <td className="px-4 py-2.5 tabular-nums font-mono text-muted-foreground">
                    {fmtTime(leg.closedAt)}
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex flex-col">
                      <span className="font-semibold text-foreground">
                        {leg.symbol ?? `${leg.token.slice(0, 6)}…`}
                      </span>
                      {leg.fullyClosed ? null : (
                        <span className="text-[9px] font-mono uppercase tracking-wider text-amber-400">
                          partial
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {fmtTokenAmt(qty, leg.symbol ?? "")}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {entry > 0 ? fmtUsd(entry) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono">
                    {exit > 0 ? fmtUsd(exit) : "—"}
                  </td>
                  <td
                    className={cn(
                      "px-4 py-2.5 text-right tabular-nums font-mono",
                      pnlUsd >= 0 ? "text-emerald-400" : "text-red-400",
                    )}
                  >
                    {`${pnlUsd >= 0 ? "+" : ""}${fmtUsd(pnlUsd)}`}
                  </td>
                  <td
                    className={cn(
                      "px-4 py-2.5 text-right tabular-nums font-mono",
                      pnlPct == null
                        ? "text-muted-foreground"
                        : pnlPct >= 0
                          ? "text-emerald-400"
                          : "text-red-400",
                    )}
                  >
                    {pnlPct == null ? "—" : fmtPct(pnlPct)}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums font-mono text-muted-foreground">
                    {hold > 0 ? fmtDuration(hold) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <TxLink hash={leg.closeTxHash} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <Pager
        loading={loading}
        exhausted={exhausted}
        onMore={loadMore}
        error={error}
      />
    </div>
  );
}

// ── Shared sub-components ────────────────────────────────────────────

function Empty({
  title,
  subtitle,
}: {
  title: string;
  subtitle?: string;
}) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-1 px-6 py-10 text-center">
      <span className="text-sm text-muted-foreground">{title}</span>
      {subtitle ? (
        <span className="text-[11px] text-muted-foreground/60">{subtitle}</span>
      ) : null}
    </div>
  );
}

function LoadingRows() {
  return (
    <div className="flex-1 flex flex-col px-4 py-3 gap-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div
          key={i}
          className="h-9 rounded bg-white/[0.04] animate-pulse"
        />
      ))}
    </div>
  );
}

function TxLink({ hash }: { hash: string | null | undefined }) {
  if (!hash) {
    return <span className="text-muted-foreground/60 text-[10px] font-mono">—</span>;
  }
  return (
    <a
      href={`${explorerBase}/tx/${hash}`}
      target="_blank"
      rel="noreferrer noopener"
      className="inline-flex items-center gap-1 text-[10px] font-mono text-muted-foreground hover:text-foreground"
    >
      {shortHash(hash)}
      <ExternalLink className="w-2.5 h-2.5" />
    </a>
  );
}

function Pager({
  loading,
  exhausted,
  onMore,
  error,
}: {
  loading: boolean;
  exhausted: boolean;
  onMore: () => void;
  error: string | null;
}) {
  return (
    <div className="border-t border-white/[0.04] px-4 py-2 flex items-center justify-between bg-card shrink-0">
      <span className="text-[10px] font-mono text-muted-foreground/70 truncate">
        {error ? error : exhausted ? "End of history" : ""}
      </span>
      <button
        type="button"
        onClick={onMore}
        disabled={loading || exhausted}
        className={cn(
          "text-[11px] font-mono px-3 py-1 rounded-[var(--radius-md)] transition-colors",
          "bg-surface-1 text-foreground hover:bg-surface-2",
          "disabled:opacity-40 disabled:cursor-not-allowed",
        )}
      >
        {loading ? "Loading…" : exhausted ? "Done" : "Load more"}
      </button>
    </div>
  );
}

function fmtDuration(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}
