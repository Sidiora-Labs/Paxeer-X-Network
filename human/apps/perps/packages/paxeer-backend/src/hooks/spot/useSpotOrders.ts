/* ══════════════════════════════════════════════════════════════════════
   Spot order pipeline for the PECOR engine.

   Each public function is a single high-level action the order form
   can invoke. They handle (in this order):

     1.  Preflight quote (where applicable) — confirm liquidity + freshness.
     2.  Auto-wrap native PAX → WPAX9 when the base token is native and
         the user lacks enough wrapped balance for the trade.
     3.  ERC-20 approve (MaxUint256) against the rail-specific spender.
     4.  Contract write to the appropriate PECOR contract:
           · `PECOR.marketBuy / marketSell`         — market rail
           · `PECOROrders.placeLimitBuy / Sell`     — limit rail
           · `PECORStopOrders.placeStopLimit*`      — stop-limit rail
           · `PECORStopOrders.placeStopLoss`        — stop-loss rail
     5.  Wait for receipt (3 s finality + 7 s RPC lag tolerance, see
         `useContract.waitForTx`). Throws on revert or timeout.

   Every action returns a typed result containing the receipt and any
   contract-derived metadata (e.g. orderId, executed amounts). The
   order form layers optimistic UI on top of these returns.

   IOC / FOK semantics: market orders are atomic in PECOR, so IOC ≡ FOK
   ≡ marketBuy/Sell with `minOut` derived from the limit price. The form
   maps `tif === "IOC" | "FOK"` to this action — the limit rail itself
   is GTC-only and always escrows funds until execution.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";

import { pecorAbi } from "@paxeer-network/paxeer-backend/core/network/spot_abi/PECOR";
import { pecorOrdersAbi } from "@paxeer-network/paxeer-backend/core/network/spot_abi/PECOROrders";
import { pecorStopOrdersAbi } from "@paxeer-network/paxeer-backend/core/network/spot_abi/PECORStopOrders";

import { PECOR_CONTRACTS, NATIVE_PAX_ADDRESS } from "@paxeer-network/paxeer-backend/core/spot/addresses";
import {
  WPAX9_ADDRESS,
  wpax9MinimalAbi,
  computeWrapShortfall,
  shouldWrapBeforeSell,
} from "@paxeer-network/paxeer-backend/core/spot/native";
import {
  applySlippageMinOut,
  buildDeadline,
  DEFAULT_LIMIT_EXPIRY_SECONDS,
} from "@paxeer-network/paxeer-backend/core/spot/slippage";

import {
  readContract,
  writeContract,
  waitForTx,
  type WaitForTxResult,
} from "@paxeer-network/paxeer-backend/hooks/perps/useContract";
import {
  ensureAllowance,
  type EnsureAllowanceResult,
} from "@paxeer-network/paxeer-backend/hooks/spot/useSpotApprovals";
import {
  quoteMarketBuy,
  quoteMarketSell,
  type SpotQuoteResult,
} from "@paxeer-network/paxeer-backend/hooks/spot/useSpotQuoter";

/* ══════════════════════════════════════════════════════════════════════
   Shared types
   ══════════════════════════════════════════════════════════════════════ */

export interface SpotOrderResult {
  /** Tx receipt. */
  receipt: WaitForTxResult;
  /** Approval receipt, if a fresh approval was issued in this pipeline. */
  approval?: EnsureAllowanceResult;
  /** Wrap receipt, if native PAX was wrapped in this pipeline. */
  wrap?: WaitForTxResult;
}

export interface MarketOrderResult extends SpotOrderResult {
  /** The preflight quote used to compute slippage protection. */
  quote: SpotQuoteResult;
  /** The minimum-output floor passed to the contract. */
  minOut: bigint;
}

export interface LimitOrderResult extends SpotOrderResult {
  /** Order id returned by the contract (best-effort decoded from receipt logs). */
  orderId?: bigint;
}

export type StopLimitOrderResult = LimitOrderResult;
export type StopLossOrderResult = LimitOrderResult;

/* ── quote-side guards ─────────────────────────────────────────── */

function assertTradableQuote(quote: SpotQuoteResult, label: string): void {
  if (!quote.sufficientLiquidity) {
    throw new Error(`${label}: insufficient liquidity in vault`);
  }
  if (quote.cappedByUtilization) {
    throw new Error(`${label}: trade exceeds vault utilization cap`);
  }
  if (quote.priceStaleIn || quote.priceStaleOut) {
    throw new Error(`${label}: oracle price feed is stale`);
  }
  if (quote.amountOut <= 0n) {
    throw new Error(`${label}: quote returned zero output`);
  }
}

/* ══════════════════════════════════════════════════════════════════════
   Market rail — PECOR.marketBuy / marketSell
   ══════════════════════════════════════════════════════════════════════ */

export interface MarketBuyParams {
  stablecoin: Address;
  /** ERC-20 the user wants to receive (resolve PAX → WPAX9 upstream). */
  token: Address;
  /** Amount of `stablecoin` to spend (token-native decimals). */
  stablecoinAmount: bigint;
  /** Slippage tolerance in basis points (50 = 0.5 %). */
  slippageBps: number;
  /** User EOA placing the trade (needed for allowance lookup). */
  user: Address;
  /** Optional pre-fetched quote; skips the read round-trip when supplied. */
  precomputedQuote?: SpotQuoteResult;
  /** Custom deadline window in seconds (defaults to 60 s). */
  deadlineSeconds?: number;
}

export async function marketBuy(params: MarketBuyParams): Promise<MarketOrderResult> {
  const {
    stablecoin,
    token,
    stablecoinAmount,
    slippageBps,
    user,
    precomputedQuote,
    deadlineSeconds,
  } = params;

  if (stablecoinAmount <= 0n) {
    throw new Error("marketBuy: stablecoinAmount must be positive");
  }

  // 1. Preflight quote.
  const quote =
    precomputedQuote ??
    (await quoteMarketBuy({ stablecoin, token, stablecoinAmount }));
  assertTradableQuote(quote, "marketBuy");

  // 2. Approve stablecoin against the PECOR exchange.
  const approval = await ensureAllowance({
    token: stablecoin,
    owner: user,
    spender: PECOR_CONTRACTS.PECOR,
    amount: stablecoinAmount,
  });

  // 3. Build slippage floor + deadline.
  const minOut = applySlippageMinOut(quote.amountOut, slippageBps);
  const deadline = buildDeadline(deadlineSeconds);

  // 4. Execute marketBuy.
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOR,
    abi: pecorAbi,
    functionName: "marketBuy",
    args: [stablecoin, token, stablecoinAmount, minOut, deadline],
  });
  const receipt = await waitForTx(hash);

  return { receipt, approval, quote, minOut };
}

export interface MarketSellParams {
  /** ERC-20 the user is selling (resolved to WPAX9 for the PAX market). */
  token: Address;
  stablecoin: Address;
  /** Amount of `token` to sell (token-native decimals). */
  tokenAmount: bigint;
  slippageBps: number;
  user: Address;
  precomputedQuote?: SpotQuoteResult;
  deadlineSeconds?: number;
  /**
   * Native-PAX auto-wrap inputs. Provide when selling on the PAX market
   * so the pipeline can wrap shortfall native balance silently before
   * the marketSell call. Omit on all other markets.
   */
  native?: {
    /** User's current WPAX9 ERC-20 balance. */
    wpaxBalance: bigint;
    /** User's current native PAX balance (wei). */
    nativeBalance: bigint;
  };
}

export async function marketSell(params: MarketSellParams): Promise<MarketOrderResult> {
  const {
    token,
    stablecoin,
    tokenAmount,
    slippageBps,
    user,
    precomputedQuote,
    deadlineSeconds,
    native,
  } = params;

  if (tokenAmount <= 0n) {
    throw new Error("marketSell: tokenAmount must be positive");
  }

  // 0. PAX auto-wrap: top up WPAX9 from native PAX if shortfall.
  let wrapReceipt: WaitForTxResult | undefined;
  if (native && token.toLowerCase() === WPAX9_ADDRESS.toLowerCase()) {
    const plan = computeWrapShortfall(tokenAmount, native.wpaxBalance, native.nativeBalance);
    if (!plan.feasible) {
      throw new Error(
        `marketSell: insufficient PAX (need ${tokenAmount}, have ${native.wpaxBalance + native.nativeBalance} combined)`,
      );
    }
    if (shouldWrapBeforeSell(plan)) {
      const wrapHash = await writeContract({
        address: WPAX9_ADDRESS,
        abi: wpax9MinimalAbi,
        functionName: "deposit",
        args: [],
        value: plan.wrapAmount,
      });
      wrapReceipt = await waitForTx(wrapHash);
    }
  }

  // 1. Preflight quote.
  const quote =
    precomputedQuote ??
    (await quoteMarketSell({ token, stablecoin, tokenAmount }));
  assertTradableQuote(quote, "marketSell");

  // 2. Approve token against the PECOR exchange.
  const approval = await ensureAllowance({
    token,
    owner: user,
    spender: PECOR_CONTRACTS.PECOR,
    amount: tokenAmount,
  });

  // 3. Build slippage floor + deadline.
  const minOut = applySlippageMinOut(quote.amountOut, slippageBps);
  const deadline = buildDeadline(deadlineSeconds);

  // 4. Execute marketSell.
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOR,
    abi: pecorAbi,
    functionName: "marketSell",
    args: [token, stablecoin, tokenAmount, minOut, deadline],
  });
  const receipt = await waitForTx(hash);

  return { receipt, approval, quote, minOut, wrap: wrapReceipt };
}

/* ══════════════════════════════════════════════════════════════════════
   Limit rail — PECOROrders.placeLimitBuy / placeLimitSell
   ══════════════════════════════════════════════════════════════════════ */

export interface PlaceLimitBuyParams {
  stablecoin: Address;
  token: Address;
  /** Stable amount the user commits to spend if the limit fills. */
  stablecoinAmount: bigint;
  /** Target price (18-decimal scaled). */
  targetPrice: bigint;
  /** Absolute UNIX-seconds expiry (defaults to 24 h from now). */
  expiresAt?: bigint;
  user: Address;
}

export async function placeLimitBuy(
  params: PlaceLimitBuyParams,
): Promise<LimitOrderResult> {
  const { stablecoin, token, stablecoinAmount, targetPrice, expiresAt, user } = params;

  if (stablecoinAmount <= 0n) throw new Error("placeLimitBuy: amount must be positive");
  if (targetPrice <= 0n) throw new Error("placeLimitBuy: targetPrice must be positive");

  const approval = await ensureAllowance({
    token: stablecoin,
    owner: user,
    spender: PECOR_CONTRACTS.PECOROrders,
    amount: stablecoinAmount,
  });

  const expiry = expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS);
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "placeLimitBuy",
    args: [stablecoin, token, stablecoinAmount, targetPrice, expiry],
  });
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export interface PlaceLimitSellParams {
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
  targetPrice: bigint;
  expiresAt?: bigint;
  user: Address;
}

export async function placeLimitSell(
  params: PlaceLimitSellParams,
): Promise<LimitOrderResult> {
  const { token, stablecoin, tokenAmount, targetPrice, expiresAt, user } = params;

  if (tokenAmount <= 0n) throw new Error("placeLimitSell: amount must be positive");
  if (targetPrice <= 0n) throw new Error("placeLimitSell: targetPrice must be positive");

  const approval = await ensureAllowance({
    token,
    owner: user,
    spender: PECOR_CONTRACTS.PECOROrders,
    amount: tokenAmount,
  });

  const expiry = expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS);
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "placeLimitSell",
    args: [token, stablecoin, tokenAmount, targetPrice, expiry],
  });
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelLimitOrder(orderId: bigint): Promise<WaitForTxResult> {
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "cancelLimitOrder",
    args: [orderId],
  });
  return waitForTx(hash);
}

export async function modifyLimitOrder(params: {
  orderId: bigint;
  newTargetPrice: bigint;
  newExpiresAt: bigint;
}): Promise<WaitForTxResult> {
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "modifyLimitOrder",
    args: [params.orderId, params.newTargetPrice, params.newExpiresAt],
  });
  return waitForTx(hash);
}

/* ── limit reads ───────────────────────────────────────────────── */

export async function getUserLimitOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "getUserLimitOrders",
    args: [user],
  });
  return (raw as readonly bigint[]) ?? [];
}

export async function getLimitOrder(orderId: bigint) {
  return readContract({
    address: PECOR_CONTRACTS.PECOROrders,
    abi: pecorOrdersAbi,
    functionName: "getLimitOrder",
    args: [orderId],
  });
}

/* ══════════════════════════════════════════════════════════════════════
   Stop-limit rail — PECORStopOrders.placeStopLimitBuy / Sell
   ══════════════════════════════════════════════════════════════════════ */

export interface PlaceStopLimitBuyParams {
  stablecoin: Address;
  token: Address;
  stablecoinAmount: bigint;
  /** Activation price (18-decimal scaled). */
  stopPrice: bigint;
  /** Post-activation limit price (must be ≥ stopPrice for a buy). */
  limitPrice: bigint;
  expiresAt?: bigint;
  user: Address;
}

export async function placeStopLimitBuy(
  params: PlaceStopLimitBuyParams,
): Promise<StopLimitOrderResult> {
  const { stablecoin, token, stablecoinAmount, stopPrice, limitPrice, expiresAt, user } =
    params;

  if (stablecoinAmount <= 0n) throw new Error("placeStopLimitBuy: amount must be positive");
  if (stopPrice <= 0n || limitPrice <= 0n) {
    throw new Error("placeStopLimitBuy: prices must be positive");
  }
  if (limitPrice < stopPrice) {
    throw new Error("placeStopLimitBuy: limit must be ≥ stop for a buy");
  }

  const approval = await ensureAllowance({
    token: stablecoin,
    owner: user,
    spender: PECOR_CONTRACTS.PECORStopOrders,
    amount: stablecoinAmount,
  });

  const expiry = expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS);
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "placeStopLimitBuy",
    args: [stablecoin, token, stablecoinAmount, stopPrice, limitPrice, expiry],
  });
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export interface PlaceStopLimitSellParams {
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
  stopPrice: bigint;
  /** Post-activation limit price (must be ≤ stopPrice for a sell). */
  limitPrice: bigint;
  expiresAt?: bigint;
  user: Address;
}

export async function placeStopLimitSell(
  params: PlaceStopLimitSellParams,
): Promise<StopLimitOrderResult> {
  const { token, stablecoin, tokenAmount, stopPrice, limitPrice, expiresAt, user } = params;

  if (tokenAmount <= 0n) throw new Error("placeStopLimitSell: amount must be positive");
  if (stopPrice <= 0n || limitPrice <= 0n) {
    throw new Error("placeStopLimitSell: prices must be positive");
  }
  if (limitPrice > stopPrice) {
    throw new Error("placeStopLimitSell: limit must be ≤ stop for a sell");
  }

  const approval = await ensureAllowance({
    token,
    owner: user,
    spender: PECOR_CONTRACTS.PECORStopOrders,
    amount: tokenAmount,
  });

  const expiry = expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS);
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "placeStopLimitSell",
    args: [token, stablecoin, tokenAmount, stopPrice, limitPrice, expiry],
  });
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelStopLimitOrder(orderId: bigint): Promise<WaitForTxResult> {
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "cancelStopLimitOrder",
    args: [orderId],
  });
  return waitForTx(hash);
}

/* ══════════════════════════════════════════════════════════════════════
   Stop-loss rail — PECORStopOrders.placeStopLoss
   ══════════════════════════════════════════════════════════════════════ */

export interface PlaceStopLossParams {
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
  /** Trigger price (18-decimal scaled). When mark price ≤ trigger, sell. */
  triggerPrice: bigint;
  expiresAt?: bigint;
  user: Address;
}

export async function placeStopLoss(
  params: PlaceStopLossParams,
): Promise<StopLossOrderResult> {
  const { token, stablecoin, tokenAmount, triggerPrice, expiresAt, user } = params;

  if (tokenAmount <= 0n) throw new Error("placeStopLoss: amount must be positive");
  if (triggerPrice <= 0n) throw new Error("placeStopLoss: triggerPrice must be positive");

  const approval = await ensureAllowance({
    token,
    owner: user,
    spender: PECOR_CONTRACTS.PECORStopOrders,
    amount: tokenAmount,
  });

  const expiry = expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS);
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "placeStopLoss",
    args: [token, stablecoin, tokenAmount, triggerPrice, expiry],
  });
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelStopLossOrder(orderId: bigint): Promise<WaitForTxResult> {
  const hash = await writeContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "cancelStopLossOrder",
    args: [orderId],
  });
  return waitForTx(hash);
}

/* ── stop reads ────────────────────────────────────────────────── */

export async function getUserStopLimitOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "getUserStopLimitOrders",
    args: [user],
  });
  return (raw as readonly bigint[]) ?? [];
}

export async function getUserStopLossOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "getUserStopLossOrders",
    args: [user],
  });
  return (raw as readonly bigint[]) ?? [];
}

export async function getStopLimitOrder(orderId: bigint) {
  return readContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "getStopLimitOrder",
    args: [orderId],
  });
}

export async function getStopLossOrder(orderId: bigint) {
  return readContract({
    address: PECOR_CONTRACTS.PECORStopOrders,
    abi: pecorStopOrdersAbi,
    functionName: "getStopLossOrder",
    args: [orderId],
  });
}

/* ══════════════════════════════════════════════════════════════════════
   Hook surface
   ══════════════════════════════════════════════════════════════════════ */

/**
 * Pure-function-style hook exposing every order action and read for
 * the PECOR rails. Stateless — callers compose their own React state
 * around these functions.
 */
export function useSpotOrders() {
  return {
    // market rail
    marketBuy,
    marketSell,
    // limit rail
    placeLimitBuy,
    placeLimitSell,
    cancelLimitOrder,
    modifyLimitOrder,
    getUserLimitOrders,
    getLimitOrder,
    // stop-limit rail
    placeStopLimitBuy,
    placeStopLimitSell,
    cancelStopLimitOrder,
    getUserStopLimitOrders,
    getStopLimitOrder,
    // stop-loss rail
    placeStopLoss,
    cancelStopLossOrder,
    getUserStopLossOrders,
    getStopLossOrder,
    // sentinel re-export so the form can detect native-PAX flows
    NATIVE_PAX_ADDRESS,
  };
}
