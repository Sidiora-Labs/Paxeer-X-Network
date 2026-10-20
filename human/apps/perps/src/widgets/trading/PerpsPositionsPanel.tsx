import React, { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { cn } from "@/ui/utils/cn";
import { Tabs, TabsContent } from "@/ui/atoms/tabs";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/ui/atoms/dropdown-menu";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePosition } from "@/hooks/perps/usePosition";
import { usePerpsOrders } from "@/hooks/perps/usePerpsOrders";
import { useQuoter } from "@/hooks/perps/useQuoter";
import { writeContract, waitForTx } from "@/hooks/perps/useContract";
import { PARTIAL_CLOSE_BPS, partialCloseAction, unitsFromIndexed } from "@/core/perps/positionActions";
import { computePositionPnl, type ParsedPosition } from "@/core/perps/pnlHelpers";
import {
  subgraphQuery,
  CLOSED_POSITIONS_QUERY,
  ORDERS_QUERY,
  ALL_ORDERS_V2_QUERY,
  TRADES_QUERY,
  LIQUIDATIONS_QUERY,
  TPSL_ORDERS_QUERY,
} from "@/hooks/perps/useSubgraph";
import { PERPS_MARKETS, EXPLORER_URL, coinLogoUrl, ORDER_TYPE_LABELS, ORDER_TYPE_COLORS, COLLATERAL_TOKENS, POSITION_ACTION_COPY } from "@/core/perps/config";
import { ExternalLink, MoreHorizontal } from "lucide-react";
import { IcoExclamation, IcoShare } from "@/ui/atoms/custom-icon";
import { PnlShareCard, type PnlCardData } from "./PnlShareCard";
import { PriceFlash } from "@/ui/atoms/PriceFlash";
import { fmtUsdCompact as formatUsd, fmtDate as formatDate, fmtPrice as formatPrice, parse18 } from "@/ui/utils/format";
import { SetTpSlModal } from "./modals/SetTpSlModal";
import { AdjustCollateralModal } from "./modals/AdjustCollateralModal";
import { TopUpPositionModal } from "./modals/TopUpPositionModal";
import { toastError } from "@/widgets/errors";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import {
  usePortfolioStats,
  refreshPositions,
  markPositionLocallyClosed,
  unmarkPositionLocallyClosed,
} from "@/hooks/perps/usePortfolioStats";
import { subscribeStream } from "@/core/graphql/client";
import { onStreamResync } from "@/core/net/realtime";
import { INDEXER_GRAPHQL } from "@/core/perps/config";

type TabKey = "positions" | "pending" | "tpsl" | "filled" | "pos-history" | "order-history" | "liquidation";

// ── Paxeer style tokens (shared across rows/cards/headers) ─────────────
const PX_META_LABEL: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};
const PX_MONO: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 11,
  color: "var(--fg)",
  fontVariantNumeric: "tabular-nums",
};

function getMarketSymbol(marketId: number): string {
  return PERPS_MARKETS.find((m) => m.marketId === marketId)?.symbol ?? `MKT-${marketId}`;
}

function EmptyState({ text }: { text: string }) {
  return (
    <div
      className="text-center py-8"
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: 10,
        letterSpacing: "0.14em",
        textTransform: "uppercase",
        color: "var(--fg-subtle)",
      }}
    >
      {text}
    </div>
  );
}

function tokenSymbol(addr: string): string {
  const t = Object.values(COLLATERAL_TOKENS).find((c) => c.address.toLowerCase() === addr?.toLowerCase());
  return t?.symbol ?? "USD";
}

const MobilePositionCard = React.memo(function MobilePositionCard({ pos, markPrice, tpsl, onClose, isClosing }: {
  pos: any; markPrice: number; tpsl?: { tp?: string; sl?: string };
  onClose: (id: string) => void; isClosing: boolean;
}) {
  const symbol = getMarketSymbol(pos.marketId);
  const entryPrice = parse18(pos.entryPrice);
  const sizeUsd = parse18(pos.sizeUsd);
  const leverage = parse18(pos.leverage);
  const parsed: import("@/core/perps/pnlHelpers").ParsedPosition = {
    positionId: BigInt(pos.positionId),
    marketId: pos.marketId,
    symbol,
    isLong: pos.isLong,
    size: sizeUsd,
    collateral: parse18(pos.collateralUsd),
    leverage,
    entryPrice,
    collateralToken: pos.collateralToken ?? "",
    collateralAmount: parse18(pos.collateralAmount),
    openedAt: pos.openedAt,
    openTxHash: pos.openTxHash,
  };
  const pnlData = computePositionPnl(parsed, markPrice);
  const pnlColor = pnlData.unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)";

  // Liquidation safety: 0 = dead, 1 = safe. Distance from mark to liq
  // normalised by the distance between entry and liq on that side.
  const range = Math.abs(entryPrice - pnlData.liqPrice) || 1;
  const rawDanger = pos.isLong
    ? (markPrice - pnlData.liqPrice) / range
    : (pnlData.liqPrice - markPrice) / range;
  const safety = Math.max(0, Math.min(1, markPrice > 0 && pnlData.liqPrice > 0 ? rawDanger : 1));

  return (
    <div
      style={{
        padding: 18,
        background: "var(--bg-subtle)",
        border: "1px solid var(--border-soft)",
        borderRadius: 16,
        display: "flex",
        flexDirection: "column",
        gap: 12,
      }}
    >
      {/* Top row — symbol badge + sym/USD + side chip + notional, PnL right */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <img
          src={coinLogoUrl(symbol)}
          alt={symbol}
          style={{ width: 36, height: 36, borderRadius: 10, flexShrink: 0 }}
        />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <span style={{ fontSize: 15, fontWeight: 500, color: "var(--fg)" }}>{symbol}/USD</span>
            <span
              style={{
                padding: "2px 7px",
                borderRadius: 4,
                background: pos.isLong ? "rgba(5, 193, 104, 0.16)" : "rgba(255, 90, 101, 0.16)",
                color: pos.isLong ? "var(--success)" : "var(--danger)",
                fontFamily: "var(--font-mono)",
                fontSize: 9,
                letterSpacing: "0.1em",
                textTransform: "uppercase",
              }}
            >
              {pos.isLong ? "Long" : "Short"} · {leverage.toFixed(0)}×
            </span>
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              color: "var(--fg-subtle)",
              marginTop: 2,
              letterSpacing: "0.04em",
            }}
          >
            {formatUsd(sizeUsd)} · {formatDate(pos.openedAt)}
          </div>
        </div>
        <div style={{ textAlign: "right" }}>
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 22,
              fontWeight: 500,
              color: pnlColor,
              letterSpacing: "-0.01em",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {pnlData.unrealizedPnl >= 0 ? "+" : ""}
            {formatUsd(pnlData.unrealizedPnl)}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              color: pnlColor,
              marginTop: 1,
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {pnlData.roe >= 0 ? "▲" : "▼"} {Math.abs(pnlData.roe).toFixed(2)}%
          </div>
        </div>
      </div>

      {/* Liquidation safety bar */}
      <div>
        <div
          className="flex justify-between"
          style={{ ...PX_META_LABEL, fontSize: 9, marginBottom: 6 }}
        >
          <span>Liq {pnlData.liqPrice > 0 ? `$${formatPrice(pnlData.liqPrice)}` : "—"}</span>
          <span>safety · {(safety * 100).toFixed(0)}%</span>
          <span>Mark {markPrice > 0 ? `$${formatPrice(markPrice)}` : "—"}</span>
        </div>
        <div
          style={{
            position: "relative",
            height: 6,
            background: "var(--bg)",
            borderRadius: 999,
            overflow: "hidden",
          }}
        >
          <div
            style={{
              position: "absolute",
              left: 0,
              top: 0,
              bottom: 0,
              width: `${safety * 100}%`,
              background:
                safety > 0.6
                  ? "linear-gradient(90deg, var(--danger), var(--warning), var(--success))"
                  : safety > 0.3
                  ? "linear-gradient(90deg, var(--danger), var(--warning))"
                  : "var(--danger)",
              transition: "width 400ms",
            }}
          />
          <div
            style={{
              position: "absolute",
              left: `${safety * 100}%`,
              top: -3,
              width: 2,
              height: 12,
              background: "#fff",
              transform: "translateX(-1px)",
              borderRadius: 999,
            }}
          />
        </div>
      </div>

      {/* Metrics grid — Entry, Net value, Collateral */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 8, paddingTop: 4 }}>
        <div>
          <div style={PX_META_LABEL}>Entry</div>
          <div style={{ ...PX_MONO, fontSize: 12, marginTop: 3 }}>${formatPrice(entryPrice)}</div>
        </div>
        <div>
          <div style={PX_META_LABEL}>Net value</div>
          <div style={{ ...PX_MONO, fontSize: 12, marginTop: 3 }}>{formatUsd(pnlData.netValue)}</div>
        </div>
        <div>
          <div style={PX_META_LABEL}>Collateral</div>
          <div style={{ ...PX_MONO, fontSize: 12, marginTop: 3 }}>{formatUsd(pnlData.initialCollateral)}</div>
        </div>
      </div>

      {/* TP/SL chips (if set) */}
      {tpsl && (tpsl.tp || tpsl.sl) && (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 12,
            paddingTop: 8,
            borderTop: "1px solid var(--border-soft)",
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.04em",
          }}
        >
          {tpsl.tp && <span style={{ color: "var(--success)" }}>TP {formatUsd(parse18(tpsl.tp))}</span>}
          {tpsl.sl && <span style={{ color: "var(--danger)" }}>SL {formatUsd(parse18(tpsl.sl))}</span>}
        </div>
      )}

      {/* Actions */}
      <div style={{ display: "flex", gap: 6 }}>
        <button
          onClick={() => {}}
          style={{
            flex: 1,
            padding: 8,
            borderRadius: 8,
            background: "var(--bg)",
            color: "var(--fg-muted)",
            border: "1px solid var(--border-soft)",
            fontFamily: "var(--font-body)",
            fontSize: 12,
            cursor: "pointer",
            transition: "all 150ms",
          }}
        >
          TP / SL
        </button>
        <button
          onClick={() => onClose(pos.positionId)}
          disabled={isClosing}
          style={{
            flex: 1,
            padding: 8,
            borderRadius: 8,
            background: "var(--bg)",
            color: "var(--danger)",
            border: "1px solid rgba(255, 90, 101, 0.3)",
            fontFamily: "var(--font-body)",
            fontSize: 12,
            cursor: isClosing ? "not-allowed" : "pointer",
            opacity: isClosing ? 0.5 : 1,
            transition: "all 150ms",
          }}
        >
          {isClosing ? "Closing…" : "Close"}
        </button>
      </div>
    </div>
  );
});

/* ── Desktop position row (memoized; only re-renders on its own props) ── */
interface DesktopPositionRowProps {
  pos: any;
  markPrice: number;
  tpsl?: { tp?: string; sl?: string };
  closingId: string | null;
  onShare: (data: PnlCardData) => void;
  onSetTpsl: (t: { positionId: string; entryPrice: number; isLong: boolean; markPrice: number; size: number }) => void;
  onAdjust: (t: { positionId: string; mode: "add" | "remove" | "addSize"; collateral: number; size: number; leverage: number }) => void;
  onTopUp: (positionId: string) => void;
  onClose: (positionId: string) => void;
  onPartialClose: (positionId: string, bps: number) => void;
}
const DesktopPositionRow = React.memo(function DesktopPositionRow({
  pos, markPrice, tpsl, closingId,
  onShare, onSetTpsl, onAdjust, onTopUp, onClose, onPartialClose,
}: DesktopPositionRowProps) {
  const symbol = getMarketSymbol(pos.marketId);
  const entryPrice = parse18(pos.entryPrice);
  const sizeUsd = parse18(pos.sizeUsd);
  const leverage = parse18(pos.leverage);
  const collateralUsd = parse18(pos.collateralUsd);

  const parsed: ParsedPosition = {
    positionId: BigInt(pos.positionId),
    marketId: pos.marketId,
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
  const pnlData = computePositionPnl(parsed, markPrice);
  const marginPct = sizeUsd > 0 ? (pnlData.remainingCollateral / sizeUsd) * 100 : 0;

  const sideColor = pos.isLong ? "var(--success)" : "var(--danger)";
  const pnlColor = pnlData.unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)";
  const roeColor = pnlData.roe >= 0 ? "var(--success)" : "var(--danger)";
  const marginColor =
    marginPct > 10 ? "var(--success)" : marginPct > 5 ? "var(--warning)" : "var(--danger)";

  const cellBase: React.CSSProperties = {
    padding: "8px 12px",
    whiteSpace: "nowrap",
    fontFamily: "var(--font-mono)",
    fontSize: 11,
    color: "var(--fg)",
    fontVariantNumeric: "tabular-nums",
  };
  const cellRight: React.CSSProperties = { ...cellBase, textAlign: "right" };

  const smallBtn: React.CSSProperties = {
    padding: "3px 8px",
    borderRadius: 6,
    background: "var(--bg-subtle)",
    border: "1px solid var(--border-soft)",
    color: "var(--fg-muted)",
    fontFamily: "var(--font-mono)",
    fontSize: 10,
    letterSpacing: "0.08em",
    cursor: "pointer",
    transition: "all 150ms",
  };

  return (
    <tr
      className="hover:bg-white/[0.03]"
      style={{ borderBottom: "1px solid var(--border-soft)" }}
    >
      <td style={cellBase}>
        <div className="flex items-center gap-1.5">
          <img src={coinLogoUrl(symbol)} alt={symbol} className="w-4 h-4 rounded-full" />
          <span style={{ color: "var(--fg)", fontFamily: "var(--font-body)", fontSize: 12, fontWeight: 500 }}>
            {symbol}/USD
          </span>
          <span style={{ color: "var(--fg-subtle)", fontSize: 9, letterSpacing: "0.04em" }}>
            {leverage.toFixed(0)}×
          </span>
        </div>
      </td>
      <td style={{ ...cellBase, color: sideColor, textTransform: "uppercase", letterSpacing: "0.08em", fontWeight: 600, fontSize: 10 }}>
        {pos.isLong ? "Long" : "Short"}
      </td>
      <td style={cellRight}>{formatUsd(sizeUsd)}</td>
      <td style={cellRight}>
        <div>{formatUsd(pnlData.initialCollateral)}</div>
        <div style={{ fontSize: 9, color: "var(--fg-subtle)", letterSpacing: "0.04em" }}>
          {tokenSymbol(pos.collateralToken)}
        </div>
      </td>
      <td style={cellRight}>${formatPrice(entryPrice)}</td>
      <td style={cellRight}>
        {markPrice > 0 ? (
          <PriceFlash value={markPrice}>{`$${formatPrice(markPrice)}`}</PriceFlash>
        ) : (
          <span className="animate-pulse" style={{ color: "var(--fg-subtle)" }}>…</span>
        )}
      </td>
      <td style={{ ...cellRight, color: "var(--danger)" }}>
        {pnlData.liqPrice > 0 ? `$${formatPrice(pnlData.liqPrice)}` : "—"}
      </td>
      <td style={{ ...cellRight, color: pnlColor, fontWeight: 600 }}>
        {pnlData.unrealizedPnl >= 0 ? "+" : ""}
        {formatUsd(pnlData.unrealizedPnl)}
      </td>
      <td style={{ ...cellRight, color: roeColor }}>
        {pnlData.roe >= 0 ? "+" : ""}
        {pnlData.roe.toFixed(2)}%
      </td>
      <td style={cellRight}>{formatUsd(pnlData.netValue)}</td>
      <td style={{ ...cellRight, color: marginColor }}>{marginPct.toFixed(2)}%</td>
      <td style={{ ...cellBase, textAlign: "center" }}>
        {tpsl ? (
          <div
            className="flex flex-col items-center"
            style={{ gap: 2, fontFamily: "var(--font-mono)", fontSize: 9, letterSpacing: "0.04em" }}
          >
            {tpsl.tp && <span style={{ color: "var(--success)" }}>TP {formatUsd(parse18(tpsl.tp))}</span>}
            {tpsl.sl && <span style={{ color: "var(--danger)" }}>SL {formatUsd(parse18(tpsl.sl))}</span>}
          </div>
        ) : (
          <span style={{ color: "var(--fg-subtle)" }}>—</span>
        )}
      </td>
      <td style={{ ...cellRight, color: "var(--fg-subtle)" }}>{formatDate(pos.openedAt)}</td>
      <td style={cellRight}>
        <div className="flex items-center justify-end gap-1">
          <button
            title="Share PnL"
            style={{ ...smallBtn, padding: "3px 5px" }}
            onClick={() =>
              onShare({
                symbol,
                isLong: pos.isLong,
                leverage,
                entryPrice,
                size: sizeUsd,
                pnl: pnlData,
                openedAt: pos.openedAt,
              })
            }
          >
            <IcoShare size={11} />
          </button>
          <button
            style={smallBtn}
            onClick={() =>
              onSetTpsl({
                positionId: pos.positionId,
                entryPrice,
                isLong: pos.isLong,
                markPrice,
                size: sizeUsd,
              })
            }
          >
            TP/SL
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button style={{ ...smallBtn, padding: "3px 5px" }}>
                <MoreHorizontal className="h-3 w-3" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => onAdjust({ positionId: pos.positionId, mode: "add", collateral: collateralUsd, size: sizeUsd, leverage })}>
                {POSITION_ACTION_COPY.addCollateral}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onAdjust({ positionId: pos.positionId, mode: "remove", collateral: collateralUsd, size: sizeUsd, leverage })}>
                {POSITION_ACTION_COPY.removeCollateral}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onAdjust({ positionId: pos.positionId, mode: "addSize", collateral: collateralUsd, size: sizeUsd, leverage })}>
                {POSITION_ACTION_COPY.addSize}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onTopUp(pos.positionId)}>
                {POSITION_ACTION_COPY.topUp}
              </DropdownMenuItem>
              {PARTIAL_CLOSE_BPS.map((bps) => (
                <DropdownMenuItem
                  key={bps}
                  disabled={closingId === pos.positionId}
                  onClick={() => onPartialClose(pos.positionId, bps)}
                >
                  {`Close ${bps / 100}%`}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            disabled={closingId === pos.positionId}
            onClick={() => onClose(pos.positionId)}
            style={{
              ...smallBtn,
              color: "var(--danger)",
              borderColor: "rgba(255, 90, 101, 0.3)",
              opacity: closingId === pos.positionId ? 0.5 : 1,
              cursor: closingId === pos.positionId ? "not-allowed" : "pointer",
            }}
          >
            {closingId === pos.positionId ? "…" : "Close"}
          </button>
        </div>
      </td>
    </tr>
  );
});

/* ── Shared order table (extracted so memoization actually works) ── */
interface OrderTableProps {
  orders: any[];
  showCancel?: boolean;
  cancellingId: string | null;
  onCancelOrder: (orderId: string) => void;
}
const PX_THEAD_STYLE: React.CSSProperties = {
  ...PX_META_LABEL,
  textAlign: "left",
  padding: "10px 12px",
  fontWeight: 500,
  borderBottom: "1px solid var(--border-soft)",
};
const PX_TD_BASE: React.CSSProperties = {
  padding: "8px 12px",
  whiteSpace: "nowrap",
  fontFamily: "var(--font-mono)",
  fontSize: 11,
  color: "var(--fg)",
  fontVariantNumeric: "tabular-nums",
};
const PX_TD_RIGHT: React.CSSProperties = { ...PX_TD_BASE, textAlign: "right" };

const OrderTable = React.memo(function OrderTable({ orders, showCancel, cancellingId, onCancelOrder }: OrderTableProps) {
  const smallBtn: React.CSSProperties = {
    padding: "3px 8px",
    borderRadius: 6,
    background: "var(--bg-subtle)",
    border: "1px solid var(--border-soft)",
    color: "var(--fg-muted)",
    fontFamily: "var(--font-mono)",
    fontSize: 10,
    letterSpacing: "0.08em",
    cursor: "pointer",
    transition: "all 150ms",
  };
  return (
    <div className="overflow-x-auto">
      <table className="w-full" style={{ borderCollapse: "collapse" }}>
        <thead>
          <tr>
            <th style={PX_THEAD_STYLE}>Market</th>
            <th style={PX_THEAD_STYLE}>Type</th>
            <th style={PX_THEAD_STYLE}>Side</th>
            <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Trigger</th>
            <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Size</th>
            <th style={PX_THEAD_STYLE}>Status</th>
            <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}></th>
          </tr>
        </thead>
        <tbody>
          {orders.map((order: any) => {
            const symbol = getMarketSymbol(order.marketId);
            const typeLabel = order.orderTypeName || ORDER_TYPE_LABELS[order.orderType] || `Type ${order.orderType}`;
            const typeColor = ORDER_TYPE_COLORS[order.orderType] || "";
            const isActive = order.status === "pending" || order.status === "active";
            const sideColor = order.isLong ? "var(--success)" : "var(--danger)";
            const statusFilled = order.status === "filled" || order.status === "executed";
            const statusBg = statusFilled
              ? "rgba(5, 193, 104, 0.1)"
              : order.status === "failed"
              ? "rgba(255, 90, 101, 0.1)"
              : order.status === "cancelled"
              ? "var(--bg-subtle)"
              : "rgba(255, 158, 44, 0.1)";
            const statusFg = statusFilled
              ? "var(--success)"
              : order.status === "failed"
              ? "var(--danger)"
              : order.status === "cancelled"
              ? "var(--fg-subtle)"
              : "var(--warning)";
            return (
              <tr
                key={order.orderId}
                className="hover:bg-white/[0.03]"
                style={{ borderBottom: "1px solid var(--border-soft)" }}
              >
                <td style={PX_TD_BASE}>
                  <div className="flex items-center gap-1.5">
                    <img src={coinLogoUrl(symbol)} alt={symbol} className="w-4 h-4 rounded-full" />
                    <span style={{ fontFamily: "var(--font-body)", fontSize: 12, fontWeight: 500 }}>{symbol}/USD</span>
                  </div>
                </td>
                <td className={cn(typeColor)} style={{ ...PX_TD_BASE, fontFamily: "var(--font-body)", fontSize: 12 }}>
                  {typeLabel}
                </td>
                <td
                  style={{
                    ...PX_TD_BASE,
                    color: sideColor,
                    textTransform: "uppercase",
                    letterSpacing: "0.08em",
                    fontWeight: 600,
                    fontSize: 10,
                  }}
                >
                  {order.isLong ? "Long" : "Short"}
                </td>
                <td style={PX_TD_RIGHT}>{formatUsd(parse18(order.triggerPrice))}</td>
                <td style={PX_TD_RIGHT}>{formatUsd(parse18(order.sizeUsd))}</td>
                <td style={PX_TD_BASE}>
                  <div className="flex items-center gap-1">
                    <span
                      style={{
                        padding: "2px 7px",
                        borderRadius: 4,
                        background: statusBg,
                        color: statusFg,
                        fontFamily: "var(--font-mono)",
                        fontSize: 9,
                        letterSpacing: "0.1em",
                        textTransform: "uppercase",
                      }}
                    >
                      {order.status}
                    </span>
                    {order.failureReason && (
                      <span
                        title={order.failureReason}
                        style={{ color: "var(--danger)", cursor: "help" }}
                      >
                        <IcoExclamation size={12} />
                      </span>
                    )}
                  </div>
                </td>
                <td style={PX_TD_RIGHT}>
                  {showCancel && isActive ? (
                    <button
                      disabled={cancellingId === order.orderId}
                      onClick={() => onCancelOrder(order.orderId)}
                      style={{
                        ...smallBtn,
                        color: "var(--danger)",
                        borderColor: "rgba(255, 90, 101, 0.3)",
                        opacity: cancellingId === order.orderId ? 0.5 : 1,
                        cursor: cancellingId === order.orderId ? "not-allowed" : "pointer",
                      }}
                    >
                      {cancellingId === order.orderId ? "…" : "Cancel"}
                    </button>
                  ) : order.resolvedTxHash ? (
                    <a
                      href={`${EXPLORER_URL}/tx/${order.resolvedTxHash}`}
                      target="_blank"
                      rel="noopener"
                      style={{ color: "var(--fg-subtle)" }}
                    >
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  ) : null}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
});

export function PerpsPositionsPanel() {
  const { address, isConnected } = usePerpsWallet();
  const isMobile = useIsMobile();
  const positionHook = usePosition();
  const ordersHook = usePerpsOrders();
  const quoter = useQuoter();
  const quoterRef = useRef(quoter);
  quoterRef.current = quoter;
  const positionRef = useRef(positionHook);
  positionRef.current = positionHook;
  const ordersRef = useRef(ordersHook);
  ordersRef.current = ordersHook;

  const portfolioStats = usePortfolioStats();
  const [tab, setTab] = useState<TabKey>("positions");
  const [allOrders, setAllOrders] = useState<any[]>([]);
  const [closedPositions, setClosedPositions] = useState<any[]>([]);
  const [trades, setTrades] = useState<any[]>([]);
  const [liquidations, setLiquidations] = useState<any[]>([]);
  const [tpslMap, setTpslMap] = useState<Record<string, { tp?: string; sl?: string }>>({});
  const { tickers } = useRealtimePrices();
  const [loading, setLoading] = useState(false);
  const [closingId, setClosingId] = useState<string | null>(null);
  const [cancellingId, setCancellingId] = useState<string | null>(null);
  const [shareCard, setShareCard] = useState<PnlCardData | null>(null);
  // Optimistic UI: hide cancelled orders immediately, reconcile when subgraph catches up.
  // (Closed-position optimism lives in the usePortfolioStats singleton so it
  // persists across page navigations and hard reloads — see
  // markPositionLocallyClosed in @/hooks/perps/usePortfolioStats.)
  const [optimisticCancelled, setOptimisticCancelled] = useState<Set<string>>(() => new Set());

  // Modal state for position actions
  const [tpslTarget, setTpslTarget] = useState<{ positionId: string; entryPrice: number; isLong: boolean; markPrice: number; size: number } | null>(null);
  const [adjustTarget, setAdjustTarget] = useState<{ positionId: string; mode: "add" | "remove" | "addSize"; collateral: number; size: number; leverage: number } | null>(null);
  const [topUpTarget, setTopUpTarget] = useState<string | null>(null);

  // Fetch live subgraph data: pending orders + TP/SL only (10 s)
  // Open positions come from usePortfolioStats singleton (already polling at 5 s)
  const fetchData = useCallback(async () => {
    if (!address) return;
    setLoading(true);
    try {
      const [ordData, tpslData] = await Promise.allSettled([
        subgraphQuery<{ orders: any[] }>(ALL_ORDERS_V2_QUERY, { userAddress: address, limit: 100 }),
        subgraphQuery<{ takeProfits: any[]; stopLosses: any[] }>(TPSL_ORDERS_QUERY, { userAddress: address }),
      ]);
      if (ordData.status === "fulfilled") setAllOrders(ordData.value.orders ?? []);
      if (tpslData.status === "fulfilled") {
        const map: Record<string, { tp?: string; sl?: string }> = {};
        for (const tp of tpslData.value.takeProfits ?? []) {
          if (tp.positionId) {
            if (!map[tp.positionId]) map[tp.positionId] = {};
            map[tp.positionId].tp = tp.triggerPrice;
          }
        }
        for (const sl of tpslData.value.stopLosses ?? []) {
          if (sl.positionId) {
            if (!map[sl.positionId]) map[sl.positionId] = {};
            map[sl.positionId].sl = sl.triggerPrice;
          }
        }
        setTpslMap(map);
      }
    } catch (err) {
      console.error("Failed to fetch positions data:", err);
    } finally {
      setLoading(false);
    }
  }, [address]);

  // Fetch historical data once on mount (closed positions, trades, liquidations)
  const fetchHistoryData = useCallback(async () => {
    if (!address) return;
    try {
      const [histData, tradesData, liqData] = await Promise.allSettled([
        subgraphQuery<{ positions: any[] }>(CLOSED_POSITIONS_QUERY, { userAddress: address, limit: 50 }),
        subgraphQuery<{ trades: any[] }>(TRADES_QUERY, { userAddress: address, limit: 50 }),
        subgraphQuery<{ liquidations: any[] }>(LIQUIDATIONS_QUERY, { userAddress: address, limit: 50 }),
      ]);
      if (histData.status === "fulfilled") setClosedPositions(histData.value.positions ?? []);
      if (tradesData.status === "fulfilled") setTrades(tradesData.value.trades ?? []);
      if (liqData.status === "fulfilled") setLiquidations(liqData.value.liquidations ?? []);
    } catch {}
  }, [address]);

  // Live orders: fetch once on address change, then re-fetch on every
  // orderChanged push from the indexer subscription instead of polling.
  useEffect(() => onStreamResync(url => { if (url === INDEXER_GRAPHQL.wsUrl && address) void fetchData(); }), [address, fetchData]);
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    if (!address) return;
    return subscribeStream<{ orderChanged: unknown }>({
      endpoint: INDEXER_GRAPHQL,
      query: /* GraphQL */ `
        subscription OrderChanged($userAddress: String!) {
          orderChanged(userAddress: $userAddress) { orderId status }
        }
      `,
      variables: { userAddress: address },
      onData: () => fetchData(),
    });
  }, [address, fetchData]);

  useEffect(() => {
    fetchHistoryData();
  }, [fetchHistoryData]);

  // Open positions: the singleton already filters out locally-closed IDs,
  // so consume them directly here.
  const visiblePositions = portfolioStats.positions;

  const pendingOrders = useMemo(() => allOrders.filter((o: any) =>
    (o.status === "pending" || o.status === "active")
    && o.orderType !== 2 && o.orderType !== 3
    && !optimisticCancelled.has(o.orderId)
  ), [allOrders, optimisticCancelled]);

  const tpslOrders = useMemo(() => allOrders.filter((o: any) =>
    (o.status === "pending" || o.status === "active")
    && (o.orderType === 2 || o.orderType === 3)
    && !optimisticCancelled.has(o.orderId)
  ), [allOrders, optimisticCancelled]);

  const filledOrders = useMemo(() => allOrders.filter((o: any) =>
    o.status === "filled" || o.status === "executed"
  ), [allOrders]);

  // Reconciliation for locally-closed positions is now handled inside the
  // usePortfolioStats singleton (see _reconcileLocallyClosed). The
  // component-local cancelled-orders set still needs its own reconcile.
  useEffect(() => {
    if (optimisticCancelled.size === 0) return;
    // Treat an order as "still pending" only if its status is pending/active.
    const stillPending = new Set(
      allOrders.filter((o: any) => o.status === "pending" || o.status === "active")
        .map((o: any) => o.orderId)
    );
    let changed = false;
    const next = new Set<string>();
    optimisticCancelled.forEach((id) => {
      if (stillPending.has(id)) next.add(id); else changed = true;
    });
    if (changed) setOptimisticCancelled(next);
  }, [allOrders, optimisticCancelled]);

  // Silent refresh: re-pull orders+TPSL AND force the singleton's positions
  // list to refetch (with backoff retries) so closed/changed rows disappear
  // immediately instead of waiting for the WS positionChanged push.
  const refreshAll = useCallback(() => {
    fetchData();
    refreshPositions();
  }, [fetchData]);

  const handleClose = useCallback(async (positionId: string) => {
    setClosingId(positionId);
    // Optimistically hide the row globally (perps + portfolio panels). The
    // singleton persists this across page navigations and reloads until the
    // indexer drops the position from its result.
    markPositionLocallyClosed(positionId);
    try {
      await positionRef.current.closePosition(BigInt(positionId));
      refreshAll();
    } catch (err) {
      // Roll back: surface the row again so the user can retry.
      unmarkPositionLocallyClosed(positionId);
      toastError(err, {
        context: "position/close",
        titleOverride: "Failed to close position",
        onRetry: () => { void handleClose(positionId); },
      });
    } finally {
      setClosingId(null);
    }
  }, [refreshAll]);

  const handlePartialClose = useCallback(async (positionId: string, bps: number) => {
    const pos = portfolioStats.positions.find((p: any) => p.positionId === positionId);
    if (!pos) return;
    setClosingId(positionId);
    try {
      const action = partialCloseAction({
        positionId: BigInt(positionId),
        isLong: pos.isLong,
        sizeUsd: unitsFromIndexed(pos.sizeUsd),
        bps,
      });
      const quote = await quoterRef.current.quotePartialClose(BigInt(positionId), action.closeSizeUsd);
      await waitForTx(await writeContract(action.call(quote.exitPrice)));
      refreshAll();
    } catch (err) {
      toastError(err, {
        context: "position/partial-close",
        titleOverride: "Failed to partially close position",
        onRetry: () => { void handlePartialClose(positionId, bps); },
      });
    } finally {
      setClosingId(null);
    }
  }, [portfolioStats.positions, refreshAll]);

  const handleCancelOrder = useCallback(async (orderId: string) => {
    setCancellingId(orderId);
    setOptimisticCancelled((prev) => {
      const n = new Set(prev); n.add(orderId); return n;
    });
    try {
      await ordersRef.current.cancelOrder(BigInt(orderId));
      refreshAll();
    } catch (err) {
      setOptimisticCancelled((prev) => {
        const n = new Set(prev); n.delete(orderId); return n;
      });
      toastError(err, {
        context: "order/cancel",
        titleOverride: "Failed to cancel order",
        onRetry: () => { void handleCancelOrder(orderId); },
      });
    } finally {
      setCancellingId(null);
    }
  }, [refreshAll]);

  if (!isConnected) {
    return (
      <div
        className="flex items-center justify-center h-full min-h-[180px]"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
        }}
      >
        Connect wallet to view positions
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)}>
        {/* Paxeer tab bar — mono caps with accent underline + optional count chip */}
        <div
          className="flex items-center overflow-x-auto scrollbar-hide"
          style={{
            borderBottom: "1px solid var(--border-soft)",
            paddingLeft: 4,
          }}
        >
          {([
            { value: "positions", label: "Positions", count: visiblePositions.length },
            { value: "pending", label: "Pending", count: pendingOrders.length },
            { value: "tpsl", label: "TP/SL", count: tpslOrders.length },
            { value: "filled", label: "Filled", count: filledOrders.length },
            { value: "pos-history", label: "Position History" },
            { value: "order-history", label: "Order History" },
            { value: "liquidation", label: "Liquidation" },
          ] as { value: TabKey; label: string; count?: number }[]).map((t) => {
            const active = tab === t.value;
            return (
              <button
                key={t.value}
                onClick={() => setTab(t.value)}
                style={{
                  position: "relative",
                  padding: "10px 14px",
                  fontFamily: "var(--font-mono)",
                  fontSize: 10,
                  letterSpacing: "0.14em",
                  textTransform: "uppercase",
                  color: active ? "var(--fg)" : "var(--fg-subtle)",
                  background: "transparent",
                  border: "none",
                  cursor: "pointer",
                  whiteSpace: "nowrap",
                  transition: "color 150ms ease",
                }}
              >
                <span className="flex items-center" style={{ gap: 6 }}>
                  {t.label}
                  {t.count !== undefined && t.count > 0 && (
                    <span
                      style={{
                        minWidth: 16,
                        height: 16,
                        padding: "0 5px",
                        borderRadius: 999,
                        background: active
                          ? "rgba(var(--accent-solid-rgb), 0.18)"
                          : "var(--bg-subtle)",
                        color: active ? "var(--color-blue-200)" : "var(--fg-muted)",
                        fontFamily: "var(--font-mono)",
                        fontSize: 9,
                        fontVariantNumeric: "tabular-nums",
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        letterSpacing: 0,
                      }}
                    >
                      {t.count}
                    </span>
                  )}
                </span>
                {active && (
                  <span
                    aria-hidden
                    style={{
                      position: "absolute",
                      bottom: -1,
                      left: 12,
                      right: 12,
                      height: 2,
                      background: "var(--accent-solid)",
                      borderRadius: 999,
                    }}
                  />
                )}
              </button>
            );
          })}
        </div>

        {/* ── Positions ── */}
        <TabsContent value="positions" className="mt-0">
          {visiblePositions.length === 0 ? <EmptyState text="No open positions" /> : isMobile ? (
            <div className="p-3 space-y-3">
              {visiblePositions.map((pos: any) => (
                <MobilePositionCard
                  key={pos.positionId}
                  pos={pos}
                  markPrice={tickers[getMarketSymbol(pos.marketId)]?.mark_price ?? 0}
                  tpsl={tpslMap[pos.positionId]}
                  onClose={handleClose}
                  isClosing={closingId === pos.positionId}
                />
              ))}
            </div>
          ) : (
            <div className="overflow-x-auto scrollbar-custom">
              <table className="w-full" style={{ borderCollapse: "collapse" }}>
                <thead>
                  <tr>
                    <th style={PX_THEAD_STYLE}>Market</th>
                    <th style={PX_THEAD_STYLE}>Side</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Size</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Collateral</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Entry</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Mark</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Liq. Price</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>PnL</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>ROE</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Net Value</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Margin</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "center" }}>TP / SL</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Opened</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}></th>
                  </tr>
                </thead>
                <tbody>
                  {visiblePositions.map((pos: any) => (
                    <DesktopPositionRow
                      key={pos.positionId}
                      pos={pos}
                      markPrice={tickers[getMarketSymbol(pos.marketId)]?.mark_price ?? 0}
                      tpsl={tpslMap[pos.positionId]}
                      closingId={closingId}
                      onShare={setShareCard}
                      onSetTpsl={setTpslTarget}
                      onAdjust={setAdjustTarget}
                      onTopUp={setTopUpTarget}
                      onClose={handleClose}
                      onPartialClose={handlePartialClose}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </TabsContent>

        {/* ── Pending ── */}
        <TabsContent value="pending" className="mt-0">
          {pendingOrders.length === 0 ? <EmptyState text="No pending orders" /> : <OrderTable orders={pendingOrders} showCancel cancellingId={cancellingId} onCancelOrder={handleCancelOrder} />}
        </TabsContent>

        {/* ── TP/SL ── */}
        <TabsContent value="tpsl" className="mt-0">
          {tpslOrders.length === 0 ? <EmptyState text="No TP/SL orders" /> : <OrderTable orders={tpslOrders} showCancel cancellingId={cancellingId} onCancelOrder={handleCancelOrder} />}
        </TabsContent>

        {/* ── Filled ── */}
        <TabsContent value="filled" className="mt-0">
          {filledOrders.length === 0 ? <EmptyState text="No filled orders" /> : <OrderTable orders={filledOrders} cancellingId={cancellingId} onCancelOrder={handleCancelOrder} />}
        </TabsContent>

        {/* ── Position History ── */}
        <TabsContent value="pos-history" className="mt-0">
          {closedPositions.length === 0 ? <EmptyState text="No position history" /> : (
            <div className="overflow-x-auto">
              <table className="w-full" style={{ borderCollapse: "collapse" }}>
                <thead>
                  <tr>
                    <th style={PX_THEAD_STYLE}>Market</th>
                    <th style={PX_THEAD_STYLE}>Side</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Size</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Entry</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Exit</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>PnL</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Closed</th>
                  </tr>
                </thead>
                <tbody>
                  {closedPositions.map((pos: any) => {
                    const symbol = getMarketSymbol(pos.marketId);
                    const pnl = parse18(pos.realizedPnl);
                    const sideColor = pos.isLong ? "var(--success)" : "var(--danger)";
                    const pnlColor = pnl >= 0 ? "var(--success)" : "var(--danger)";
                    return (
                      <tr
                        key={pos.positionId}
                        className="hover:bg-white/[0.03]"
                        style={{ borderBottom: "1px solid var(--border-soft)" }}
                      >
                        <td style={{ ...PX_TD_BASE, fontFamily: "var(--font-body)", fontSize: 12, fontWeight: 500 }}>
                          {symbol}/USD
                        </td>
                        <td
                          style={{
                            ...PX_TD_BASE,
                            color: sideColor,
                            textTransform: "uppercase",
                            letterSpacing: "0.08em",
                            fontWeight: 600,
                            fontSize: 10,
                          }}
                        >
                          {pos.isLong ? "Long" : "Short"}
                        </td>
                        <td style={PX_TD_RIGHT}>{formatUsd(parse18(pos.sizeUsd))}</td>
                        <td style={PX_TD_RIGHT}>{formatUsd(parse18(pos.entryPrice))}</td>
                        <td style={PX_TD_RIGHT}>{formatUsd(parse18(pos.exitPrice))}</td>
                        <td style={{ ...PX_TD_RIGHT, color: pnlColor, fontWeight: 600 }}>
                          {pnl >= 0 ? "+" : ""}{formatUsd(pnl)}
                        </td>
                        <td style={{ ...PX_TD_RIGHT, color: "var(--fg-subtle)" }}>
                          {formatDate(pos.closedAt)}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </TabsContent>

        {/* ── Order History ── */}
        <TabsContent value="order-history" className="mt-0">
          {allOrders.length === 0 ? <EmptyState text="No order history" /> : <OrderTable orders={allOrders} cancellingId={cancellingId} onCancelOrder={handleCancelOrder} />}
        </TabsContent>

        {/* ── Liquidation ── */}
        <TabsContent value="liquidation" className="mt-0">
          {liquidations.length === 0 ? <EmptyState text="No liquidations" /> : (
            <div className="overflow-x-auto">
              <table className="w-full" style={{ borderCollapse: "collapse" }}>
                <thead>
                  <tr>
                    <th style={PX_THEAD_STYLE}>Market</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Price</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Penalty</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}>Time</th>
                    <th style={{ ...PX_THEAD_STYLE, textAlign: "right" }}></th>
                  </tr>
                </thead>
                <tbody>
                  {liquidations.map((liq: any) => (
                    <tr
                      key={liq.id}
                      className="hover:bg-white/[0.03]"
                      style={{ borderBottom: "1px solid var(--border-soft)" }}
                    >
                      <td style={{ ...PX_TD_BASE, fontFamily: "var(--font-body)", fontSize: 12, fontWeight: 500 }}>
                        {getMarketSymbol(liq.marketId)}/USD
                      </td>
                      <td style={PX_TD_RIGHT}>{formatUsd(parse18(liq.price))}</td>
                      <td style={{ ...PX_TD_RIGHT, color: "var(--danger)" }}>
                        {formatUsd(parse18(liq.penalty))}
                      </td>
                      <td style={{ ...PX_TD_RIGHT, color: "var(--fg-subtle)" }}>
                        {formatDate(liq.blockTimestamp)}
                      </td>
                      <td style={PX_TD_RIGHT}>
                        <a
                          href={`${EXPLORER_URL}/tx/${liq.txHash}`}
                          target="_blank"
                          rel="noopener"
                          style={{ color: "var(--fg-subtle)" }}
                        >
                          <ExternalLink className="h-3 w-3" />
                        </a>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </TabsContent>
      </Tabs>

      {shareCard && (
        <PnlShareCard data={shareCard} onClose={() => setShareCard(null)} />
      )}

      {/* Position action modals */}
      {tpslTarget && (
        <SetTpSlModal
          positionId={tpslTarget.positionId}
          currentSize={tpslTarget.size}
          currentEntryPrice={tpslTarget.entryPrice}
          isLong={tpslTarget.isLong}
          markPrice={tpslTarget.markPrice}
          onClose={() => setTpslTarget(null)}
          onSuccess={refreshAll}
        />
      )}
      {adjustTarget && (
        <AdjustCollateralModal
          positionId={adjustTarget.positionId}
          mode={adjustTarget.mode}
          currentCollateral={adjustTarget.collateral}
          currentSize={adjustTarget.size}
          currentLeverage={adjustTarget.leverage}
          onClose={() => setAdjustTarget(null)}
          onSuccess={refreshAll}
        />
      )}
      {topUpTarget && (
        <TopUpPositionModal
          positionId={topUpTarget}
          userAddress={address ?? undefined}
          onClose={() => setTopUpTarget(null)}
          onSuccess={refreshAll}
        />
      )}
    </div>
  );
}
