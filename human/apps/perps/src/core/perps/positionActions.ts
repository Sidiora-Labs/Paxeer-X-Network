import { parseUnits } from "viem";

import { BPS_DENOMINATOR } from "@/core/spot/slippage";
import { buildPartialClose, buildSetPositionTpSl } from "./calls";

export type PositionActionKind = "close" | "partialClose" | "adjustCollateral" | "topUp" | "setTpSl" | "cancel";

export interface PositionAction {
  kind: PositionActionKind;
  label: string;
  /** The 0x1015 writes the action sends. */
  calls: readonly string[];
  /** The 0x1015 quote whose price feeds the bound of the write, if any. */
  quote?: string;
}

/** The actions of a position row. Margin is cross, so no transfer-margin or margin-mode action exists. */
export const positionActions: readonly PositionAction[] = [
  { kind: "close", label: "Close", calls: ["closePositionWithParams"], quote: "quoteClosePosition" },
  { kind: "partialClose", label: "Partial Close", calls: ["closePositionWithParams"], quote: "quotePartialClose" },
  {
    kind: "adjustCollateral",
    label: "Adjust Collateral",
    calls: ["addCollateral", "removeCollateral", "placeOrder"],
    quote: "quoteOpenPosition",
  },
  { kind: "topUp", label: "Top Up Position", calls: ["topUpPosition"] },
  { kind: "setTpSl", label: "TP/SL", calls: ["setPositionTpSl"] },
  { kind: "cancel", label: "Cancel", calls: ["cancelOrder"] },
];

/** The partial-close fractions the panel offers, in basis points of the position size. */
export const PARTIAL_CLOSE_BPS = [2_500, 5_000, 7_500] as const;

const INDEXED_UNITS = /^(\d+)(?:\.(\d+))?(?:[eE]\+?(\d+))?$/;

/**
 * An 18-decimal integer amount as the indexer serialises it, plain ("2000000000000000000000")
 * or in NUMERIC scientific notation ("2000e+18"), parsed exactly; a fractional result is refused.
 */
export function unitsFromIndexed(value: string): bigint {
  const match = INDEXED_UNITS.exec(value.trim());
  if (!match) throw new RangeError(`not a non-negative integer amount: ${value}`);
  const [, intPart, fracPart = "", exp = "0"] = match;
  const shift = Number(exp) - fracPart.length;
  if (shift < 0) {
    const kept = fracPart.length + shift;
    if (/[^0]/.test(fracPart.slice(kept))) throw new RangeError(`not an integer amount: ${value}`);
    return BigInt(intPart + fracPart.slice(0, kept));
  }
  return BigInt(intPart + fracPart) * 10n ** BigInt(shift);
}

/** closeSizeUsd = sizeUsd * bps / 10000, refused unless strictly between 0 and the position size. */
export function partialCloseSize(sizeUsd: bigint, bps: number): bigint {
  if (!Number.isInteger(bps) || bps <= 0 || bps >= 10_000) {
    throw new RangeError(`bps must be an integer in (0, 10000), got ${bps}`);
  }
  const closeSizeUsd = (sizeUsd * BigInt(bps)) / BPS_DENOMINATOR;
  if (closeSizeUsd <= 0n || closeSizeUsd >= sizeUsd) {
    throw new RangeError(`partial close size ${closeSizeUsd} is outside (0, ${sizeUsd})`);
  }
  return closeSizeUsd;
}

export interface PartialCloseAction {
  positionId: bigint;
  isLong: boolean;
  /** The position size, 18 decimals. */
  sizeUsd: bigint;
  bps: number;
}

/** The closeSizeUsd to quote with quotePartialClose, and the write built on that quote's exitPrice. */
export function partialCloseAction(input: PartialCloseAction) {
  const closeSizeUsd = partialCloseSize(input.sizeUsd, input.bps);
  return {
    closeSizeUsd,
    call: (exitPrice: bigint) =>
      buildPartialClose({ positionId: input.positionId, isLong: input.isLong, closeSizeUsd, exitPrice }),
  };
}

/** A price field at 18 decimals; an empty field is 0, which clears that side. */
function price18(input: string): bigint {
  const trimmed = input.trim();
  return trimmed === "" ? 0n : parseUnits(trimmed, 18);
}

/** One setPositionTpSl(positionId, takeProfitPrice, stopLossPrice) at 18 decimals. */
export function setTpSlAction(positionId: bigint, isLong: boolean, takeProfit: string, stopLoss: string) {
  return buildSetPositionTpSl({
    positionId,
    isLong,
    takeProfitPrice: price18(takeProfit),
    stopLossPrice: price18(stopLoss),
  });
}
