/* ══════════════════════════════════════════════════════════════════════
   PECORQuoter read helpers.

   The quoter exposes a 23-field `QuoteResult` per request — far richer
   than a typical AMM quote. We surface every field as a typed object
   the order form can render directly (effective fee bps, tier, price
   impact, TWAP deviation, utilization cap, staleness flags, etc.).

   The hook is stateless and async — no React state, no caching. Higher
   layers (`useSpotOrders`, the order form's preview pane) call these
   functions and decide their own debounce / SWR / re-fetch cadence.

   Source-of-truth read patterns the order pipeline uses:
     · `quoteMarketBuy / Sell`  — preflight a market order
     · `batchQuote`             — multi-pair price ladder for the form
     · `getAllLiquidityInfo`    — fan-out depth for the stable selector
     · `getTokenPrice`          — live oracle price + staleness flag
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";
import { pecorQuoterAbi } from "@paxeer-network/paxeer-backend/core/network/spot_abi/PECORQuoter";
import { PECOR_CONTRACTS } from "@paxeer-network/paxeer-backend/core/spot/addresses";
import { readContract } from "@paxeer-network/paxeer-backend/hooks/perps/useContract";

/* ── typed quote result ────────────────────────────────────────── */

/**
 * Full-fidelity representation of `PECORQuoter.QuoteResult`. Field
 * names mirror the Solidity struct exactly to make audit-trail review
 * trivial.
 */
export interface SpotQuoteResult {
  amountIn: bigint;
  amountOut: bigint;
  grossAmountOut: bigint;
  executionPrice: bigint;
  spotPriceIn: bigint;
  spotPriceOut: bigint;
  feeAmount: bigint;
  feeBps: bigint;
  sufficientLiquidity: boolean;
  availableLiquidity: bigint;
  priceTimestampIn: bigint;
  priceTimestampOut: bigint;
  priceStaleIn: boolean;
  priceStaleOut: boolean;
  utilizationCapBps: bigint;
  tradeUtilizationBps: bigint;
  cappedByUtilization: boolean;
  twapOut: bigint;
  twapDeviationBps: bigint;
  effectiveFeeBps: bigint;
  tier: bigint;
  priceImpactBps: bigint;
  priceImpactDeduction: bigint;
}

/** Per-token oracle snapshot returned by `getTokenPrice`. */
export interface SpotTokenPrice {
  /** Oracle-quoted price, 18-decimal fixed point. */
  price: bigint;
  /** Last update timestamp (UNIX seconds). */
  timestamp: bigint;
  /** True when the oracle hasn't updated within its freshness window. */
  isStale: boolean;
}

/** Per-token liquidity snapshot returned by `getLiquidityInfo`. */
export interface SpotLiquidityInfo {
  /** Pool reserves available to swap into. */
  available: bigint;
  /** Current token price, 18-decimal fixed point. */
  tokenPrice: bigint;
  /** Whether the underlying oracle price is stale. */
  isStale: boolean;
}

/** Compact summary returned by `getAllLiquidityInfo` (per token). */
export interface SpotAllLiquidityRow {
  token: Address;
  reserve: bigint;
  price: bigint;
  stale: boolean;
}

/* ── parsing helpers ───────────────────────────────────────────── */

/**
 * viem returns the QuoteResult tuple as an object keyed by the field
 * names from the ABI. We re-construct it explicitly to (a) get an
 * exact type, (b) guard against any future ABI re-ordering, and
 * (c) coerce numeric edge cases.
 */
function parseQuoteResult(raw: unknown): SpotQuoteResult {
  const o = (raw ?? {}) as Record<string, unknown>;
  const big = (v: unknown): bigint =>
    typeof v === "bigint" ? v : v == null ? 0n : BigInt(v as string | number);
  const bool = (v: unknown): boolean => v === true;
  return {
    amountIn: big(o.amountIn),
    amountOut: big(o.amountOut),
    grossAmountOut: big(o.grossAmountOut),
    executionPrice: big(o.executionPrice),
    spotPriceIn: big(o.spotPriceIn),
    spotPriceOut: big(o.spotPriceOut),
    feeAmount: big(o.feeAmount),
    feeBps: big(o.feeBps),
    sufficientLiquidity: bool(o.sufficientLiquidity),
    availableLiquidity: big(o.availableLiquidity),
    priceTimestampIn: big(o.priceTimestampIn),
    priceTimestampOut: big(o.priceTimestampOut),
    priceStaleIn: bool(o.priceStaleIn),
    priceStaleOut: bool(o.priceStaleOut),
    utilizationCapBps: big(o.utilizationCapBps),
    tradeUtilizationBps: big(o.tradeUtilizationBps),
    cappedByUtilization: bool(o.cappedByUtilization),
    twapOut: big(o.twapOut),
    twapDeviationBps: big(o.twapDeviationBps),
    effectiveFeeBps: big(o.effectiveFeeBps),
    tier: big(o.tier),
    priceImpactBps: big(o.priceImpactBps),
    priceImpactDeduction: big(o.priceImpactDeduction),
  };
}

/* ── market quotes ─────────────────────────────────────────────── */

/**
 * Preflight a market BUY: how much `token` will `stablecoinAmount` of
 * `stablecoin` buy, with full fee / price-impact / staleness breakdown?
 */
export async function quoteMarketBuy(params: {
  stablecoin: Address;
  token: Address;
  stablecoinAmount: bigint;
}): Promise<SpotQuoteResult> {
  const { stablecoin, token, stablecoinAmount } = params;
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "quoteMarketBuy",
    args: [stablecoin, token, stablecoinAmount],
  });
  return parseQuoteResult(raw);
}

/**
 * Preflight a market SELL: how much `stablecoin` will `tokenAmount` of
 * `token` sell for, with the same rich breakdown.
 */
export async function quoteMarketSell(params: {
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
}): Promise<SpotQuoteResult> {
  const { token, stablecoin, tokenAmount } = params;
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "quoteMarketSell",
    args: [token, stablecoin, tokenAmount],
  });
  return parseQuoteResult(raw);
}

/* ── generic exact-in / exact-out quotes ───────────────────────── */

/** Generic exact-in quote — any token pair, not restricted to stable↔token. */
export async function quoteExactIn(params: {
  tokenIn: Address;
  tokenOut: Address;
  amountIn: bigint;
}): Promise<SpotQuoteResult> {
  const { tokenIn, tokenOut, amountIn } = params;
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "quoteExactIn",
    args: [tokenIn, tokenOut, amountIn],
  });
  return parseQuoteResult(raw);
}

/** Generic exact-out quote — caller specifies desired output amount. */
export async function quoteExactOut(params: {
  tokenIn: Address;
  tokenOut: Address;
  amountOut: bigint;
}): Promise<SpotQuoteResult> {
  const { tokenIn, tokenOut, amountOut } = params;
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "quoteExactOut",
    args: [tokenIn, tokenOut, amountOut],
  });
  return parseQuoteResult(raw);
}

/* ── batch quotes ──────────────────────────────────────────────── */

export interface BatchQuoteRequest {
  tokenIn: Address;
  tokenOut: Address;
  amount: bigint;
  isExactIn: boolean;
}

/** Fan-out multiple quotes in a single RPC round-trip. */
export async function batchQuote(
  requests: ReadonlyArray<BatchQuoteRequest>,
): Promise<SpotQuoteResult[]> {
  if (requests.length === 0) return [];
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "batchQuote",
    args: [requests],
  });
  const list = Array.isArray(raw) ? raw : [];
  return list.map(parseQuoteResult);
}

/* ── price + liquidity reads ───────────────────────────────────── */

/** Read the oracle price + staleness for a single token. */
export async function getTokenPrice(token: Address): Promise<SpotTokenPrice> {
  const raw = (await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "getTokenPrice",
    args: [token],
  })) as readonly [bigint, bigint, boolean];
  return {
    price: raw?.[0] ?? 0n,
    timestamp: raw?.[1] ?? 0n,
    isStale: raw?.[2] ?? false,
  };
}

/** Read pool liquidity + price for a single token. */
export async function getLiquidityInfo(token: Address): Promise<SpotLiquidityInfo> {
  const raw = (await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "getLiquidityInfo",
    args: [token],
  })) as readonly [bigint, bigint, boolean];
  return {
    available: raw?.[0] ?? 0n,
    tokenPrice: raw?.[1] ?? 0n,
    isStale: raw?.[2] ?? false,
  };
}

/**
 * Read every supported token's liquidity in one RPC call. Used by the
 * stable selector to rank vault depth on the SELL side.
 */
export async function getAllLiquidityInfo(): Promise<SpotAllLiquidityRow[]> {
  const raw = (await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "getAllLiquidityInfo",
    args: [],
  })) as readonly [readonly Address[], readonly bigint[], readonly bigint[], readonly boolean[]];

  const tokens = raw?.[0] ?? [];
  const reserves = raw?.[1] ?? [];
  const prices = raw?.[2] ?? [];
  const stale = raw?.[3] ?? [];

  return tokens.map((token, i) => ({
    token,
    reserve: reserves[i] ?? 0n,
    price: prices[i] ?? 0n,
    stale: stale[i] ?? false,
  }));
}

/** Read TWAP price for a token over `period` seconds. */
export async function getTWAP(token: Address, period: bigint): Promise<bigint> {
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORQuoter,
    abi: pecorQuoterAbi,
    functionName: "getTWAP",
    args: [token, period],
  });
  return typeof raw === "bigint" ? raw : raw == null ? 0n : BigInt(raw as string | number);
}

/* ── hook surface ──────────────────────────────────────────────── */

/**
 * Pure-function-style hook. Returns every quoter read bound for
 * ergonomic destructuring in the order pipeline.
 */
export function useSpotQuoter() {
  return {
    quoteMarketBuy,
    quoteMarketSell,
    quoteExactIn,
    quoteExactOut,
    batchQuote,
    getTokenPrice,
    getLiquidityInfo,
    getAllLiquidityInfo,
    getTWAP,
  };
}
