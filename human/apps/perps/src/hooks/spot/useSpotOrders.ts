/* ══════════════════════════════════════════════════════════════════════
   Spot order pipeline on the spot precompiles.

   Each public function is a single high-level action the order form
   can invoke. They handle (in this order):

     1.  Preflight quote (where applicable) — confirm liquidity + freshness.
     2.  ERC-20 approve of exactly the call amount to the rail that pulls
         it (`SPOT_SPENDERS`); the PAX and SID pointers and native value
         move by bank transfer and need none.
     3.  Contract write built by `core/spot/calls`:
           · 0x1021 `marketBuy / marketSell / swapExactInNative` — market rail
           · 0x1022 `placeLimitBuy / Sell`                      — limit rail
           · 0x1023 `placeStopLimit* / placeStopLoss`           — stop rails
     4.  Wait for receipt (3 s finality + 7 s RPC lag tolerance, see
         `useContract.waitForTx`). Throws on revert or timeout.

   Every action returns a typed result containing the receipt and any
   contract-derived metadata (e.g. orderId, executed amounts). The
   order form layers optimistic UI on top of these returns.

   IOC / FOK semantics: market orders are atomic in PECOR, so IOC ≡ FOK
   ≡ marketBuy/Sell with `minOut` derived from the limit price. The form
   maps `tif === "IOC" | "FOK"` to this action — the limit rail itself
   is GTC-only and always escrows funds until execution.
   ══════════════════════════════════════════════════════════════════════ */

import { isAddress, zeroAddress, type Address } from "viem";

import { SPOT_SPENDERS } from "@/core/network/approvals";
import { NATIVE_PAX_ADDRESS } from "@/core/spot/addresses";
import {
  WEI_PER_UHPX,
  buildCancelLimitOrder,
  buildCancelStopLimitOrder,
  buildCancelStopLossOrder,
  buildGetLimitOrder,
  buildGetPaxPointer,
  buildGetStopLimitOrder,
  buildGetStopLossOrder,
  buildGetUserLimitOrders,
  buildGetUserStopLimitOrders,
  buildGetUserStopLossOrders,
  buildMarketBuy,
  buildMarketSell,
  buildModifyLimitOrder,
  buildPlaceLimitBuy,
  buildPlaceLimitSell,
  buildPlaceStopLimitBuy,
  buildPlaceStopLimitSell,
  buildPlaceStopLoss,
  buildSwapExactInNative,
} from "@/core/spot/calls";
import {
  applySlippageMinOut,
  buildDeadline,
  DEFAULT_LIMIT_EXPIRY_SECONDS,
} from "@/core/spot/slippage";

import {
  readContract,
  writeContract,
  waitForTx,
  type WaitForTxResult,
} from "@/hooks/perps/useContract";
import {
  ensureAllowance,
  type EnsureAllowanceResult,
} from "@/hooks/spot/useSpotApprovals";
import {
  quoteMarketBuy,
  quoteMarketSell,
  type SpotQuoteResult,
} from "@/hooks/spot/useSpotQuoter";

/* ══════════════════════════════════════════════════════════════════════
   Shared types
   ══════════════════════════════════════════════════════════════════════ */

export interface SpotOrderResult {
  /** Tx receipt. */
  receipt: WaitForTxResult;
  /** Approval receipt, if a fresh approval was issued in this pipeline. */
  approval?: EnsureAllowanceResult;
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

/* ── PAX token ─────────────────────────────────────────────────── */

/** The uhpx pointer, the PAX token of the venue, as the market rail reports it. */
export async function getPaxPointer(): Promise<Address> {
  const pointer = await readContract(buildGetPaxPointer());
  if (typeof pointer !== "string" || !isAddress(pointer) || pointer.toLowerCase() === zeroAddress) {
    throw new Error("getPaxPointer: market rail returned no uhpx pointer");
  }
  return pointer;
}

/* ══════════════════════════════════════════════════════════════════════
   Market rail — 0x1021 marketBuy / marketSell / swapExactInNative
   ══════════════════════════════════════════════════════════════════════ */

export interface MarketBuyParams {
  stablecoin: Address;
  /** Token the user wants to receive. */
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

  // 2. Approve exactly the stablecoin amount to the market rail.
  const approval = await ensureAllowance({
    token: stablecoin,
    owner: user,
    spender: SPOT_SPENDERS.market,
    amount: stablecoinAmount,
  });

  // 3. Build slippage floor + deadline, then execute marketBuy.
  const minOut = applySlippageMinOut(quote.amountOut, slippageBps);
  const hash = await writeContract(
    buildMarketBuy({
      stablecoin,
      token,
      stablecoinAmount,
      minOut,
      deadline: buildDeadline(deadlineSeconds),
    }),
  );
  const receipt = await waitForTx(hash);

  return { receipt, approval, quote, minOut };
}

export interface MarketSellParams {
  /**
   * Token the user is selling. `NATIVE_PAX_ADDRESS` sells PAX from native
   * value through swapExactInNative.
   */
  token: Address;
  stablecoin: Address;
  /** Amount of `token` to sell (token-native decimals; uhpx units for PAX). */
  tokenAmount: bigint;
  slippageBps: number;
  user: Address;
  precomputedQuote?: SpotQuoteResult;
  deadlineSeconds?: number;
  /**
   * Present on the PAX market: the sell moves bank uhpx through the uhpx
   * pointer, so `token` is replaced by that pointer and no allowance is
   * read or requested. `nativeBalance` is the user's PAX balance in wei.
   */
  native?: {
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

  // 0. A PAX sell trades the uhpx pointer, from the bank balance or from native value.
  const fromValue = token.toLowerCase() === NATIVE_PAX_ADDRESS;
  const sellsPax = fromValue || native !== undefined;
  if (native && native.nativeBalance < tokenAmount * WEI_PER_UHPX) {
    throw new Error(
      `marketSell: insufficient PAX (need ${tokenAmount * WEI_PER_UHPX} wei, have ${native.nativeBalance})`,
    );
  }
  const sellToken = sellsPax ? await getPaxPointer() : token;

  // 1. Preflight quote.
  const quote =
    precomputedQuote ??
    (await quoteMarketSell({ token: sellToken, stablecoin, tokenAmount }));
  assertTradableQuote(quote, "marketSell");

  // 2. Approve exactly the token amount to the market rail; PAX moves by bank transfer.
  const approval = sellsPax
    ? undefined
    : await ensureAllowance({
        token: sellToken,
        owner: user,
        spender: SPOT_SPENDERS.market,
        amount: tokenAmount,
      });

  // 3. Build slippage floor + deadline, then execute.
  const minOut = applySlippageMinOut(quote.amountOut, slippageBps);
  const deadline = buildDeadline(deadlineSeconds);
  const hash = await writeContract(
    fromValue
      ? buildSwapExactInNative({ tokenOut: stablecoin, amount: tokenAmount, amountOutMin: minOut, deadline })
      : buildMarketSell({ token: sellToken, stablecoin, tokenAmount, minOut, deadline }),
  );
  const receipt = await waitForTx(hash);

  return { receipt, approval, quote, minOut };
}

/* ══════════════════════════════════════════════════════════════════════
   Limit rail — 0x1022 placeLimitBuy / placeLimitSell
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
    spender: SPOT_SPENDERS.limit,
    amount: stablecoinAmount,
  });

  const hash = await writeContract(
    buildPlaceLimitBuy({
      stablecoin,
      token,
      amount: stablecoinAmount,
      targetPrice,
      expiresAt: expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS),
    }),
  );
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
    spender: SPOT_SPENDERS.limit,
    amount: tokenAmount,
  });

  const hash = await writeContract(
    buildPlaceLimitSell({
      token,
      stablecoin,
      amount: tokenAmount,
      targetPrice,
      expiresAt: expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS),
    }),
  );
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelLimitOrder(orderId: bigint): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildCancelLimitOrder(orderId)));
}

export async function modifyLimitOrder(params: {
  orderId: bigint;
  newTargetPrice: bigint;
  newExpiresAt: bigint;
}): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildModifyLimitOrder(params)));
}

/* ── limit reads ───────────────────────────────────────────────── */

export async function getUserLimitOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract(buildGetUserLimitOrders(user));
  return (raw as readonly bigint[]) ?? [];
}

export async function getLimitOrder(orderId: bigint) {
  return readContract(buildGetLimitOrder(orderId));
}

/* ══════════════════════════════════════════════════════════════════════
   Stop-limit rail — 0x1023 placeStopLimitBuy / Sell
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
    spender: SPOT_SPENDERS.stop,
    amount: stablecoinAmount,
  });

  const hash = await writeContract(
    buildPlaceStopLimitBuy({
      stablecoin,
      token,
      amount: stablecoinAmount,
      stopPrice,
      limitPrice,
      expiresAt: expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS),
    }),
  );
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
    spender: SPOT_SPENDERS.stop,
    amount: tokenAmount,
  });

  const hash = await writeContract(
    buildPlaceStopLimitSell({
      token,
      stablecoin,
      amount: tokenAmount,
      stopPrice,
      limitPrice,
      expiresAt: expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS),
    }),
  );
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelStopLimitOrder(orderId: bigint): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildCancelStopLimitOrder(orderId)));
}

/* ══════════════════════════════════════════════════════════════════════
   Stop-loss rail — 0x1023 placeStopLoss
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
    spender: SPOT_SPENDERS.stop,
    amount: tokenAmount,
  });

  const hash = await writeContract(
    buildPlaceStopLoss({
      token,
      stablecoin,
      tokenAmount,
      triggerPrice,
      expiresAt: expiresAt ?? buildDeadline(DEFAULT_LIMIT_EXPIRY_SECONDS),
    }),
  );
  const receipt = await waitForTx(hash);
  return { receipt, approval };
}

export async function cancelStopLossOrder(orderId: bigint): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildCancelStopLossOrder(orderId)));
}

/* ── stop reads ────────────────────────────────────────────────── */

export async function getUserStopLimitOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract(buildGetUserStopLimitOrders(user));
  return (raw as readonly bigint[]) ?? [];
}

export async function getUserStopLossOrders(user: Address): Promise<readonly bigint[]> {
  const raw = await readContract(buildGetUserStopLossOrders(user));
  return (raw as readonly bigint[]) ?? [];
}

export async function getStopLimitOrder(orderId: bigint) {
  return readContract(buildGetStopLimitOrder(orderId));
}

export async function getStopLossOrder(orderId: bigint) {
  return readContract(buildGetStopLossOrder(orderId));
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
