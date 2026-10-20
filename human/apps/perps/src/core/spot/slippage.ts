/* ══════════════════════════════════════════════════════════════════════
   Pure math helpers for PECOR order construction.

   Everything in this module is side-effect-free and unit-testable. The
   pipeline is:

     1.  UI gathers a human-typed number string ("123.45").
     2.  `parseTokenAmount` → bigint at the token's decimals.
     3.  Quoter returns `expectedOut: bigint`.
     4.  `applySlippage` → `minOut: bigint`.
     5.  `buildDeadline()` → bigint UNIX seconds.
     6.  Hook fires the contract write.

   All bigint math is performed at the token's native precision; the
   only place we cross over to JS numbers is when displaying values
   back to the user.
   ══════════════════════════════════════════════════════════════════════ */

/** PECOR price feed scaling — prices are 18-decimal fixed point. */
export const PRICE_DECIMALS = 18;
export const PRICE_SCALE = 10n ** BigInt(PRICE_DECIMALS);

/** BPS denominator the contracts use for fee / slippage math. */
export const BPS_DENOMINATOR = 10_000n;

/** Default time-in-force window for market orders (seconds). */
export const DEFAULT_MARKET_DEADLINE_SECONDS = 60;

/** Default expiry for GTC limit orders (seconds). */
export const DEFAULT_LIMIT_EXPIRY_SECONDS = 24 * 60 * 60; // 24 h

/* ── slippage ───────────────────────────────────────────────────── */

/**
 * Apply a downside slippage floor to a quoted output amount.
 *
 *   minOut = expectedOut · (10_000 − slippageBps) / 10_000
 *
 * Used for `marketBuy.minTokenAmount` and `marketSell.minStablecoinAmount`.
 * Slippage of 50 bps (0.5%) means the trade will revert if the engine
 * cannot deliver at least 99.5% of the quoted amount.
 */
export function applySlippageMinOut(
  expectedOut: bigint,
  slippageBps: number,
): bigint {
  if (slippageBps < 0) throw new Error("slippageBps cannot be negative");
  if (slippageBps >= 10_000) return 0n; // 100% slippage → zero floor
  const slip = BigInt(Math.floor(slippageBps));
  return (expectedOut * (BPS_DENOMINATOR - slip)) / BPS_DENOMINATOR;
}

/**
 * Apply an upside slippage ceiling to a quoted input amount. Used for
 * `quoteExactOut` flows where we know the desired output and want to
 * cap the input the user is willing to spend.
 *
 *   maxIn = expectedIn · (10_000 + slippageBps) / 10_000
 */
export function applySlippageMaxIn(
  expectedIn: bigint,
  slippageBps: number,
): bigint {
  if (slippageBps < 0) throw new Error("slippageBps cannot be negative");
  const slip = BigInt(Math.floor(slippageBps));
  return (expectedIn * (BPS_DENOMINATOR + slip)) / BPS_DENOMINATOR;
}

/* ── deadlines ──────────────────────────────────────────────────── */

/**
 * Construct an absolute deadline `(now + seconds)` as a `uint256`
 * UNIX timestamp. Pass the result directly to `marketBuy / marketSell`.
 */
export function buildDeadline(secondsFromNow: number = DEFAULT_MARKET_DEADLINE_SECONDS): bigint {
  if (!Number.isFinite(secondsFromNow) || secondsFromNow <= 0) {
    throw new Error("secondsFromNow must be a positive finite number");
  }
  return BigInt(Math.floor(Date.now() / 1000) + Math.floor(secondsFromNow));
}

/**
 * Construct an expiry for a limit order. Same shape as `buildDeadline`
 * but with a longer default window matching GTC semantics.
 */
export function buildLimitExpiry(
  secondsFromNow: number = DEFAULT_LIMIT_EXPIRY_SECONDS,
): bigint {
  return buildDeadline(secondsFromNow);
}

/* ── decimals ───────────────────────────────────────────────────── */

/**
 * Parse a human number string ("12.345") into a bigint at the given
 * decimal precision. Throws on non-finite or negative input. Silently
 * truncates excess precision past `decimals`.
 */
export function parseTokenAmount(value: string | number, decimals: number): bigint {
  if (decimals < 0 || decimals > 38) throw new Error("decimals out of range");
  const str = typeof value === "number" ? value.toString() : value.trim();
  if (!str || str === "." || str === "-") return 0n;
  if (!/^-?\d*\.?\d*$/.test(str)) {
    throw new Error(`Invalid numeric input: ${value}`);
  }
  const negative = str.startsWith("-");
  const abs = negative ? str.slice(1) : str;
  const [intPart, fracPart = ""] = abs.split(".");
  const fracTrimmed = fracPart.slice(0, decimals);
  const fracPadded = fracTrimmed.padEnd(decimals, "0");
  const combined = `${intPart || "0"}${fracPadded}`.replace(/^0+(?=\d)/, "") || "0";
  const result = BigInt(combined);
  return negative ? -result : result;
}

/**
 * Format a bigint at the given decimal precision back into a human
 * number string. Trims trailing zeros in the fractional part.
 */
export function formatTokenAmount(value: bigint, decimals: number): string {
  if (decimals < 0 || decimals > 38) throw new Error("decimals out of range");
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const scale = 10n ** BigInt(decimals);
  const intPart = abs / scale;
  const fracPart = abs % scale;
  let fracStr = fracPart.toString().padStart(decimals, "0");
  fracStr = fracStr.replace(/0+$/, "");
  const body = fracStr ? `${intPart}.${fracStr}` : intPart.toString();
  return negative ? `-${body}` : body;
}

/* ── price conversion ───────────────────────────────────────────── */

/**
 * Convert a human price string ("3842.50") into the 18-decimal scaled
 * bigint the PECOR engine accepts for `targetPrice` / `stopPrice` /
 * `limitPrice` fields.
 */
export function parsePrice(value: string | number): bigint {
  return parseTokenAmount(value, PRICE_DECIMALS);
}

/** Format an 18-decimal scaled price back to a human number string. */
export function formatPrice(value: bigint): string {
  return formatTokenAmount(value, PRICE_DECIMALS);
}

/**
 * Given a quoted execution price (18-decimal scaled), derive the
 * minimum expected output for a known input amount. Useful when
 * synthesizing IOC/FOK limit semantics from a market call.
 *
 *   expectedOut = amountIn · price / 1e18    (when in→out via price)
 *
 * Caller must adjust for decimal mismatch between input and output
 * tokens — this helper assumes the price's quote side matches the
 * output token. For PECOR's stablecoin↔base flows that's always true.
 */
export function expectedOutFromPrice(params: {
  amountIn: bigint;
  price: bigint;
  /** Decimals of `amountIn`. */
  amountInDecimals: number;
  /** Decimals of the output token. */
  amountOutDecimals: number;
}): bigint {
  const { amountIn, price, amountInDecimals, amountOutDecimals } = params;
  // Normalize: (amountIn / 10^inDec) · (price / 10^18) · 10^outDec
  // Reorder to keep everything in bigint:
  //   amountIn · price · 10^outDec / (10^inDec · 10^18)
  const numerator = amountIn * price * 10n ** BigInt(amountOutDecimals);
  const denominator = 10n ** BigInt(amountInDecimals) * PRICE_SCALE;
  return numerator / denominator;
}

/**
 * Minimum output of an IOC / FOK limit order sent to the market rail: the
 * output the limit price guarantees for `amountIn`. A sell receives the
 * stable at `limitPrice`; a buy receives the base at its inverse. Rounded
 * up, since the rail compares whole units, so a buy never pays above its
 * limit and a sell never receives below it.
 */
export function limitMinOut(
  amountIn: bigint,
  limitPrice: bigint,
  isBuy: boolean,
  decimalsIn: number,
  decimalsOut: number,
): bigint {
  if (amountIn <= 0n) throw new Error("amountIn must be positive");
  if (limitPrice <= 0n) throw new Error("limitPrice must be positive");
  for (const d of [decimalsIn, decimalsOut]) {
    if (!Number.isInteger(d) || d < 0 || d > 38) throw new Error("decimals out of range");
  }
  if (!isBuy) {
    const out = expectedOutFromPrice({
      amountIn,
      price: limitPrice,
      amountInDecimals: decimalsIn,
      amountOutDecimals: decimalsOut,
    });
    const covered = out * PRICE_SCALE * 10n ** BigInt(decimalsIn);
    const owed = amountIn * limitPrice * 10n ** BigInt(decimalsOut);
    return covered === owed ? out : out + 1n;
  }
  const numerator = amountIn * PRICE_SCALE * 10n ** BigInt(decimalsOut);
  const denominator = limitPrice * 10n ** BigInt(decimalsIn);
  return (numerator + denominator - 1n) / denominator;
}

/* ── order routing ──────────────────────────────────────────────── */

export type SpotOrderType = "market" | "limit" | "stop-limit";
export type SpotTimeInForce = "GTC" | "IOC" | "FOK";
export type SpotRail = "market" | "limit" | "stop";

/**
 * The rail a spot order is sent to. Post-only rests on the limit rail as
 * GTC; an IOC or FOK limit is taken on the market rail at `limitMinOut`.
 */
export function spotOrderRail(
  orderType: SpotOrderType,
  tif: SpotTimeInForce,
  postOnly: boolean,
): SpotRail {
  if (orderType === "market") return "market";
  if (orderType === "stop-limit") return "stop";
  return postOnly || tif === "GTC" ? "limit" : "market";
}

/**
 * Decimals of the market quote token, read from the stablecoin metadata.
 * Every quote stable must share them, since stable amounts are compared
 * across the list at one scale.
 */
export function quoteDecimals(
  stables: readonly { readonly decimals: number }[],
): number {
  const [first, ...rest] = stables;
  if (!first) throw new Error("no quote stablecoin");
  if (rest.some((s) => s.decimals !== first.decimals)) {
    throw new Error("quote stablecoins disagree on decimals");
  }
  return first.decimals;
}
