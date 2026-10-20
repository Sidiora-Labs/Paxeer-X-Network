// Layer 2 — money math helpers.
//
// All on-chain inputs MUST flow through `safeParseUnits`. Using
// `parseFloat()` + `Math.floor(num * 10**decimals)` silently loses
// precision once the user types ≥ ~15 significant digits and rounds
// down in the wrong direction near JS float boundaries. viem's
// `parseUnits` does the conversion as a string → big-integer walk and
// never goes through a JS `number`, so the produced bigint matches
// the EVM call exactly.
//
// `safeParseUnits` adds the input hardening that the form needs:
//   - empty / whitespace / "." / "-" → 0n (don't throw, don't poison)
//   - trims excess fractional digits (avoids parseUnits throwing on
//     paste of "1.123456789" into a 6-decimal field)
//   - negative numbers → 0n (we never short-sell with negative size)
//
// Use 18 decimals for everything quoted in USD on-chain (sizeUsd,
// triggerPrice, limitPrice, leverage). Use the token's own decimals
// (from `COLLATERAL_TOKENS`) for collateral transfers.

import { parseUnits } from "viem";
import { COLLATERAL_TOKENS } from "./config";

const SAFE_DECIMAL = /^\d*\.?\d*$/;

export function safeParseUnits(value: string | undefined | null, decimals: number): bigint {
  if (value === undefined || value === null) return 0n;
  const trimmed = String(value).trim();
  if (!trimmed || trimmed === "." || !SAFE_DECIMAL.test(trimmed)) return 0n;
  const [intPart, fracPart = ""] = trimmed.split(".");
  const truncated = fracPart.slice(0, decimals);
  const composed = `${intPart || "0"}${truncated ? `.${truncated}` : ""}`;
  try {
    return parseUnits(composed, decimals);
  } catch {
    return 0n;
  }
}

export function getCollateralDecimals(tokenAddress: string): number {
  const lower = tokenAddress.toLowerCase();
  for (const meta of Object.values(COLLATERAL_TOKENS)) {
    if (meta.address.toLowerCase() === lower) return meta.decimals;
  }
  throw new Error(`unknown collateral token ${tokenAddress}`);
}

/**
 * Derive the implied collateral amount in native token units when only
 * the notional size + leverage are known (used for LIMIT / STOP-LIMIT
 * orders). All inputs are USD-scaled 1e18 bigints; output is scaled
 * to the collateral token's native decimals.
 *
 *   collateral_native = floor( sizeUsd_1e18 * 10^D / leverage_1e18 )
 *
 * Integer-only math — no float intermediate, no precision drift.
 */
export function deriveImpliedCollateral(
  sizeUsd1e18: bigint,
  leverage1e18: bigint,
  collateralDecimals: number,
): bigint {
  if (sizeUsd1e18 <= 0n || leverage1e18 <= 0n) return 0n;
  const scale = 10n ** BigInt(collateralDecimals);
  return (sizeUsd1e18 * scale) / leverage1e18;
}
