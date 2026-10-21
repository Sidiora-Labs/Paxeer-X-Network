/**
 * Collateral, PnL & Liquidation helpers for Paxeer Perps positions.
 *
 * Every amount is a bigint at the 18-decimal scale the perps gateway and the
 * perps precompile use for sizeUsd, collateralUsd, leverage, entryPrice,
 * fundingPaidUsd and totalFeesUsd (1e18 = 1 USD, 1e18 = 1x). Percentages are
 * 18-decimal too (1e18 = 1%). Conversion to text happens only in the format
 * helpers at the bottom.
 *
 *   initialCollateral = sizeUsd / leverage
 *
 *   LONG  unrealizedPnl = sizeUsd × (markPrice − entryPrice) / entryPrice
 *   SHORT unrealizedPnl = sizeUsd × (entryPrice − markPrice) / entryPrice
 *
 *   remainingCollateral = initialCollateral + unrealizedPnl − fundingPaid − fees
 *
 *   Liquidation occurs when remainingCollateral ≤ maintenanceMargin (10% of initial).
 *     cushion  = (1 − mm) / leverage − (fundingPaid + fees) / sizeUsd
 *     LONG  liqPrice = entryPrice × (1 − cushion)
 *     SHORT liqPrice = entryPrice × (1 + cushion)
 *
 * Rounding: every division truncates toward zero (sub-wei digits are
 * dropped), so a long and a short of the same size have PnL and ROE that are
 * exact negatives for the same move. Liquidation prices round toward the
 * entry price (long up, short down), the side that warns earlier.
 */

import { parseWei, toPlainDecimal } from "@/ui/utils/format";

export const WAD = 10n ** 18n;
const HUNDRED_PCT = 100n * WAD;
const BPS = 10_000n;

// ── Types ───────────────────────────────────────────────────────────

export interface ParsedPosition {
  positionId: bigint;
  marketId: number;
  symbol: string;
  isLong: boolean;
  sizeUsd: bigint;          // notional, 18-dec USD
  collateralUsd: bigint;    // collateralUsd from the indexer (may be 0 / stale), 18-dec USD
  leverage: bigint;         // 18-dec (1e18 = 1x)
  entryPrice: bigint;       // 18-dec USD
  collateralToken: string;
  collateralAmount: bigint; // collateral token wei
  fundingPaidUsd: bigint;   // accrued funding paid by the position, 18-dec USD (negative = received)
  feesUsd: bigint;          // fees charged to the position, 18-dec USD
  openedAt: string | undefined;
  openTxHash: string | undefined;
  closedAt?: string;
  closePrice?: bigint;
  realizedPnl?: bigint;
  closeTxHash?: string;
}

export interface PositionPnl {
  markPrice: bigint;
  initialCollateral: bigint;    // sizeUsd / leverage (what the user actually put up)
  remainingCollateral: bigint;  // initialCollateral + unrealizedPnl − funding − fees (live)
  collateralHealthPct: bigint;  // remaining / initial × 100, 18-dec percent
  unrealizedPnl: bigint;        // 18-dec USD
  unrealizedPnlPct: bigint;     // return on initial collateral, 18-dec percent
  netValue: bigint;             // remaining collateral (same as remainingCollateral)
  marginPct: bigint;            // remaining / sizeUsd × 100, 18-dec percent
  liqPrice: bigint;             // estimated price where position is wiped out, 18-dec USD
  liqSafetyBps: bigint;         // 0 = at liquidation, 10000 = at or beyond entry
  roe: bigint;                  // alias for unrealizedPnlPct
}

/** A position row as the gateway/indexer returns it: 18-decimal integer strings. */
export interface PositionRowInput {
  positionId: string | number | bigint;
  marketId: string | number;
  isLong: boolean;
  sizeUsd?: string | null;
  collateralUsd?: string | null;
  leverage?: string | null;
  entryPrice?: string | null;
  collateralToken?: string | null;
  collateralAmount?: string | null;
  fundingPaidUsd?: string | null;
  feesUsd?: string | null;
  openedAt?: string;
  openTxHash?: string;
}

// ── Constants ───────────────────────────────────────────────────────

// Maintenance margin — positions are liquidated when remaining collateral
// drops to this fraction of the initial collateral (1000 bps = 10%).
const MAINTENANCE_MARGIN_BPS = 1_000n;

// ── Parsing ─────────────────────────────────────────────────────────

function wei(v: string | null | undefined): bigint {
  return v == null || v === "" ? 0n : parseWei(v);
}

export function parsePositionRow(row: PositionRowInput, symbol: string): ParsedPosition {
  return {
    positionId: BigInt(row.positionId),
    marketId: Number(row.marketId),
    symbol,
    isLong: row.isLong,
    sizeUsd: wei(row.sizeUsd),
    collateralUsd: wei(row.collateralUsd),
    leverage: wei(row.leverage),
    entryPrice: wei(row.entryPrice),
    collateralToken: row.collateralToken ?? "",
    collateralAmount: wei(row.collateralAmount),
    fundingPaidUsd: wei(row.fundingPaidUsd),
    feesUsd: wei(row.feesUsd),
    openedAt: row.openedAt,
    openTxHash: row.openTxHash,
  };
}

/** A decimal string ("65000.5", "1e-3") as 18-dec wad; sub-wei digits truncate toward zero. */
export function decimalToWad(v: string): bigint {
  const plain = toPlainDecimal(v);
  const neg = plain.startsWith("-");
  const [int, frac = ""] = (neg ? plain.slice(1) : plain).split(".");
  const abs = BigInt(int) * WAD + BigInt(frac.padEnd(18, "0").slice(0, 18));
  return neg ? -abs : abs;
}

/**
 * Ticker mark price (the gateway sends a JSON number) as 18-dec wad, taken
 * from its shortest decimal text. Missing, non-finite or non-positive → 0n.
 */
export function markPriceWad(v: number | string | null | undefined): bigint {
  if (v == null || v === "") return 0n;
  if (typeof v === "number" && !Number.isFinite(v)) return 0n;
  const wad = decimalToWad(String(v));
  return wad > 0n ? wad : 0n;
}

/** num / den × 100 as an 18-dec percent, truncated toward zero; 0n when den ≤ 0. */
export function pctWad(num: bigint, den: bigint): bigint {
  return den > 0n ? (num * HUNDRED_PCT) / den : 0n;
}

const ceilDiv = (a: bigint, b: bigint) => (a + b - 1n) / b; // a ≥ 0, b > 0

// ── Core computation ────────────────────────────────────────────────

export function computePositionPnl(pos: ParsedPosition, markPrice: bigint): PositionPnl {
  const { sizeUsd, entryPrice, leverage } = pos;
  const initialCollateral = leverage > 0n ? (sizeUsd * WAD) / leverage : sizeUsd;
  const charges = pos.fundingPaidUsd + pos.feesUsd;
  const idle = initialCollateral - charges;

  if (markPrice <= 0n || entryPrice <= 0n) {
    return {
      markPrice: 0n,
      initialCollateral,
      remainingCollateral: idle,
      collateralHealthPct: pctWad(idle, initialCollateral),
      unrealizedPnl: 0n,
      unrealizedPnlPct: 0n,
      netValue: idle,
      marginPct: pctWad(idle, sizeUsd),
      liqPrice: 0n,
      liqSafetyBps: BPS,
      roe: 0n,
    };
  }

  const move = pos.isLong ? markPrice - entryPrice : entryPrice - markPrice;
  const unrealizedPnl = (sizeUsd * move) / entryPrice;
  const remainingCollateral = idle + unrealizedPnl;

  // ROE = unrealizedPnl / initialCollateral = move × leverage / entry: size
  // cancels, so the percent is one exact truncation of the price move.
  const roe = initialCollateral > 0n
    ? (move * (leverage > 0n ? leverage : WAD) * 100n) / entryPrice
    : 0n;

  // Liq when remaining collateral = mm × initial, i.e. the loss equals
  // initial × (1 − mm) − funding − fees. Over size, with initial = size / lev:
  //   |Δ| / entry = (1 − mm) / lev − charges / size
  // kept over the one denominator BPS × lev × size so nothing truncates early.
  let liqPrice = 0n;
  if (leverage > 0n && sizeUsd > 0n) {
    const den = BPS * leverage * sizeUsd;
    const cushion = (BPS - MAINTENANCE_MARGIN_BPS) * WAD * sizeUsd - charges * BPS * leverage;
    liqPrice = pos.isLong
      ? den > cushion ? ceilDiv(entryPrice * (den - cushion), den) : 0n
      : den + cushion > 0n ? (entryPrice * (den + cushion)) / den : 0n;
  }

  // Safety: distance from mark to liq over the distance from entry to liq.
  // Charges beyond the cushion put liq on the far side of entry: no room left.
  let liqSafetyBps = BPS;
  if (liqPrice > 0n) {
    const range = pos.isLong ? entryPrice - liqPrice : liqPrice - entryPrice;
    const room = pos.isLong ? markPrice - liqPrice : liqPrice - markPrice;
    const raw = room <= 0n ? 0n : range > 0n ? (room * BPS) / range : BPS;
    liqSafetyBps = raw > BPS ? BPS : raw;
  }

  return {
    markPrice,
    initialCollateral,
    remainingCollateral,
    collateralHealthPct: pctWad(remainingCollateral, initialCollateral),
    unrealizedPnl,
    unrealizedPnlPct: roe,
    netValue: remainingCollateral,
    marginPct: pctWad(remainingCollateral, sizeUsd),
    liqPrice,
    liqSafetyBps,
    roe,
  };
}

// ── Formatting helpers (display edge) ───────────────────────────────

/** Exact decimal text of an 18-dec wad, trailing zeros trimmed ("-1.5", "0"). */
export function wadToDecimal(v: bigint): string {
  const abs = v < 0n ? -v : v;
  const frac = (abs % WAD).toString().padStart(18, "0").replace(/0+$/, "");
  const body = frac ? `${abs / WAD}.${frac}` : `${abs / WAD}`;
  return v < 0n ? `-${body}` : body;
}

/** An 18-dec wad rounded half away from zero to `dp` decimals ("-0.01", "12.30"). */
export function formatWadFixed(v: bigint, dp: number, group = false): string {
  const abs = v < 0n ? -v : v;
  const unit = 10n ** BigInt(18 - dp);
  const scaled = (abs + unit / 2n) / unit;
  const step = 10n ** BigInt(dp);
  const int = (scaled / step).toString();
  const intText = group ? int.replace(/\B(?=(\d{3})+(?!\d))/g, ",") : int;
  const body = dp > 0 ? `${intText}.${(scaled % step).toString().padStart(dp, "0")}` : intText;
  return v < 0n && scaled > 0n ? `-${body}` : body;
}

export function formatPnlValue(value: bigint): string {
  const text = formatWadFixed(value < 0n ? -value : value, 2, true);
  return `${value < 0n && text !== "0.00" ? "-" : "+"}$${text}`;
}

export function formatPnlPercent(value: bigint): string {
  const text = formatWadFixed(value, 2);
  return `${text.startsWith("-") ? "" : "+"}${text}%`;
}

export function pnlColorClass(value: bigint): string {
  if (value === 0n) return "text-[#666]";
  return value > 0n ? "text-[#2b6d16]" : "text-red-500";
}

/**
 * Returns a tailwind color class for collateral health (18-dec percent):
 *  > 50% green, 20-50% amber, < 20% red
 */
export function collateralHealthColor(healthPct: bigint): string {
  if (healthPct > 50n * WAD) return "text-[#2b6d16]";
  if (healthPct > 20n * WAD) return "text-amber-500";
  return "text-red-500";
}
