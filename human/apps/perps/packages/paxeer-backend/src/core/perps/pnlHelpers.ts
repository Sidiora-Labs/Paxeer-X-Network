/**
 * Collateral, PnL & Liquidation helpers for Paxeer Perps positions.
 *
 * All calculations derive from first principles:
 *
 *   initialCollateral = sizeUsd / leverage
 *
 *   LONG  unrealizedPnl = sizeUsd × (markPrice / entryPrice − 1)
 *   SHORT unrealizedPnl = sizeUsd × (1 − markPrice / entryPrice)
 *
 *   remainingCollateral = initialCollateral + unrealizedPnl   (erodes as price moves against you)
 *
 *   Liquidation occurs when remainingCollateral ≤ maintenanceMargin (≈ 10% of initial).
 *     LONG  liqPrice = entryPrice × (1 − (1 − mm) / leverage)
 *     SHORT liqPrice = entryPrice × (1 + (1 − mm) / leverage)
 *
 *   where mm = maintenance margin ratio (0.1 = 10%)
 */

// ── Types ───────────────────────────────────────────────────────────

export interface ParsedPosition {
  positionId: bigint;
  marketId: number;
  symbol: string;
  isLong: boolean;
  size: number;       // sizeUsd (notional)
  collateral: number; // collateralUsd from subgraph (may be 0 / stale)
  leverage: number;
  entryPrice: number;
  collateralToken: string;
  collateralAmount: number;
  openedAt: string | undefined;
  openTxHash: string | undefined;
  closedAt?: string;
  closePrice?: number;
  realizedPnl?: number;
  closeTxHash?: string;
}

export interface PositionPnl {
  markPrice: number;
  initialCollateral: number;    // sizeUsd / leverage (what the user actually put up)
  remainingCollateral: number;  // initialCollateral + unrealizedPnl (live)
  collateralHealthPct: number;  // remaining / initial × 100
  unrealizedPnl: number;       // $ value
  unrealizedPnlPct: number;    // % return on collateral
  netValue: number;             // remaining collateral (same as remainingCollateral)
  liqPrice: number;             // estimated price where position is wiped out
  roe: number;                  // alias for unrealizedPnlPct
}

// ── Constants ───────────────────────────────────────────────────────

// Maintenance margin ratio — positions are liquidated when remaining
// collateral drops to this fraction of the initial collateral.
const MAINTENANCE_MARGIN = 0.10;

// ── Core computation ────────────────────────────────────────────────

export function computePositionPnl(pos: ParsedPosition, markPrice: number): PositionPnl {
  // Derive initial collateral from size & leverage (ground truth)
  const initialCollateral = pos.leverage > 0 ? pos.size / pos.leverage : pos.size;

  const empty: PositionPnl = {
    markPrice: 0,
    initialCollateral,
    remainingCollateral: initialCollateral,
    collateralHealthPct: 100,
    unrealizedPnl: 0,
    unrealizedPnlPct: 0,
    netValue: initialCollateral,
    liqPrice: 0,
    roe: 0,
  };

  if (!markPrice || markPrice <= 0 || !pos.entryPrice || pos.entryPrice <= 0) {
    return empty;
  }

  // ── Unrealized PnL ──
  const priceRatio = markPrice / pos.entryPrice;
  const unrealizedPnl = pos.isLong
    ? pos.size * (priceRatio - 1)
    : pos.size * (1 - priceRatio);

  // ── Remaining collateral (erodes as loss grows) ──
  const remainingCollateral = initialCollateral + unrealizedPnl;

  // ── Health: 100% = full, 0% = liquidated ──
  const collateralHealthPct = initialCollateral > 0
    ? (remainingCollateral / initialCollateral) * 100
    : 0;

  // ── ROE / PnL % relative to what the user put up ──
  const unrealizedPnlPct = initialCollateral > 0
    ? (unrealizedPnl / initialCollateral) * 100
    : 0;

  // ── Liquidation price ──
  // Liq when: remainingCollateral = initialCollateral × MAINTENANCE_MARGIN
  //   i.e. loss = initialCollateral × (1 − MAINTENANCE_MARGIN)
  //   loss = sizeUsd × |1 − liqPrice/entryPrice|
  //   sizeUsd × |Δ| = (sizeUsd / leverage) × (1 − mm)
  //   |Δ| = (1 − mm) / leverage
  let liqPrice = 0;
  if (pos.leverage > 0 && pos.entryPrice > 0) {
    const delta = (1 - MAINTENANCE_MARGIN) / pos.leverage;
    if (pos.isLong) {
      liqPrice = pos.entryPrice * (1 - delta);
      if (liqPrice < 0) liqPrice = 0;
    } else {
      liqPrice = pos.entryPrice * (1 + delta);
    }
  }

  return {
    markPrice,
    initialCollateral,
    remainingCollateral,
    collateralHealthPct,
    unrealizedPnl,
    unrealizedPnlPct,
    netValue: remainingCollateral,
    liqPrice,
    roe: unrealizedPnlPct,
  };
}

// ── Formatting helpers ──────────────────────────────────────────────

export function formatPnlValue(value: number): string {
  if (isNaN(value)) return "—";
  const prefix = value >= 0 ? "+" : "";
  return `${prefix}$${Math.abs(value).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function formatPnlPercent(value: number): string {
  if (isNaN(value)) return "—";
  const prefix = value >= 0 ? "+" : "";
  return `${prefix}${value.toFixed(2)}%`;
}

export function pnlColorClass(value: number): string {
  if (isNaN(value) || value === 0) return "text-[#666]";
  return value > 0 ? "text-[#2b6d16]" : "text-red-500";
}

/**
 * Returns a tailwind color class for collateral health:
 *  > 50% green, 20-50% amber, < 20% red
 */
export function collateralHealthColor(healthPct: number): string {
  if (healthPct > 50) return "text-[#2b6d16]";
  if (healthPct > 20) return "text-amber-500";
  return "text-red-500";
}
