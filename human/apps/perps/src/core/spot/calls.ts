import type { Abi, AbiStateMutability, Address, ContractFunctionArgs, ContractFunctionName } from "viem";

import { makeError } from "@/core/errors/classify";
import type { ErrorCode } from "@/core/errors/types";
import { PRECOMPILES } from "@/core/network/precompiles";
import { pecorAbi } from "@/core/network/spot_abi/PECOR";
import { pecorOrdersAbi } from "@/core/network/spot_abi/PECOROrders";
import { pecorStopOrdersAbi } from "@/core/network/spot_abi/PECORStopOrders";

/** A call at a spot precompile whose args are checked against that rail's ABI. */
export interface SpotCall<
  TAbi extends Abi,
  F extends ContractFunctionName<TAbi>,
  A extends ContractFunctionArgs<TAbi, AbiStateMutability, F> = ContractFunctionArgs<TAbi, AbiStateMutability, F>,
> {
  address: Address;
  abi: TAbi;
  functionName: F;
  args: A;
  value?: bigint;
}

/** Wei per uhpx: the EVM value of 1 uhpx (6 decimals) at 18 decimals. */
export const WEI_PER_UHPX = 10n ** 12n;

const MARKET = PRECOMPILES.spot.PECOR;
const ORDERS = PRECOMPILES.spot.PECOROrders;
const STOPS = PRECOMPILES.spot.PECORStopOrders;

function refuse(code: ErrorCode): never {
  throw makeError(code, "spot.calls");
}

function requirePositive(value: bigint): bigint {
  if (value <= 0n) refuse("VALIDATION_AMOUNT_INVALID");
  return value;
}

function requireOrderId(orderId: bigint): bigint {
  if (orderId < 0n) refuse("VALIDATION_AMOUNT_INVALID");
  return orderId;
}

/* ── market rail, 0x1021 ────────────────────────────────────────── */

export interface MarketBuyCallInput {
  stablecoin: Address;
  token: Address;
  /** Stablecoin to spend, token units. */
  stablecoinAmount: bigint;
  /** Minimum `token` out, token units. */
  minOut: bigint;
  /** Absolute UNIX-seconds deadline, non-zero. */
  deadline: bigint;
}

export function buildMarketBuy(input: MarketBuyCallInput) {
  const args = [
    input.stablecoin,
    input.token,
    requirePositive(input.stablecoinAmount),
    requirePositive(input.minOut),
    requirePositive(input.deadline),
  ] as const;
  return { address: MARKET, abi: pecorAbi, functionName: "marketBuy", args } satisfies SpotCall<
    typeof pecorAbi,
    "marketBuy"
  >;
}

export interface MarketSellCallInput {
  /** The ERC20 or native pointer sold; the uhpx pointer for PAX. */
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
  /** Minimum stablecoin out, token units. */
  minOut: bigint;
  deadline: bigint;
}

export function buildMarketSell(input: MarketSellCallInput) {
  const args = [
    input.token,
    input.stablecoin,
    requirePositive(input.tokenAmount),
    requirePositive(input.minOut),
    requirePositive(input.deadline),
  ] as const;
  return { address: MARKET, abi: pecorAbi, functionName: "marketSell", args } satisfies SpotCall<
    typeof pecorAbi,
    "marketSell"
  >;
}

export interface SwapExactInNativeCallInput {
  tokenOut: Address;
  /** PAX sold from native value, uhpx units (6 decimals). */
  amount: bigint;
  amountOutMin: bigint;
  deadline: bigint;
}

/** A sell paid from native value: the attached value is amount x 1e12 wei, credited as amount uhpx. */
export function buildSwapExactInNative(input: SwapExactInNativeCallInput) {
  const value = requirePositive(input.amount) * WEI_PER_UHPX;
  const args = [input.tokenOut, requirePositive(input.amountOutMin), requirePositive(input.deadline)] as const;
  return {
    address: MARKET,
    abi: pecorAbi,
    functionName: "swapExactInNative",
    args,
    value,
  } satisfies SpotCall<typeof pecorAbi, "swapExactInNative">;
}

/** The uhpx pointer, the PAX token of the venue, as WETH() of 0x1021 reports it. */
export function buildGetPaxPointer() {
  return { address: MARKET, abi: pecorAbi, functionName: "WETH", args: [] as const } satisfies SpotCall<
    typeof pecorAbi,
    "WETH"
  >;
}

/* ── limit rail, 0x1022 ─────────────────────────────────────────── */

export interface LimitCallInput {
  /** Spent token: the stablecoin of a buy, the base token of a sell. */
  amount: bigint;
  /** Target price, 18 decimals. */
  targetPrice: bigint;
  /** Absolute UNIX-seconds expiry. */
  expiresAt: bigint;
}

export function buildPlaceLimitBuy(input: LimitCallInput & { stablecoin: Address; token: Address }) {
  const args = [
    input.stablecoin,
    input.token,
    requirePositive(input.amount),
    requirePositive(input.targetPrice),
    requirePositive(input.expiresAt),
  ] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "placeLimitBuy", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "placeLimitBuy"
  >;
}

export function buildPlaceLimitSell(input: LimitCallInput & { token: Address; stablecoin: Address }) {
  const args = [
    input.token,
    input.stablecoin,
    requirePositive(input.amount),
    requirePositive(input.targetPrice),
    requirePositive(input.expiresAt),
  ] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "placeLimitSell", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "placeLimitSell"
  >;
}

export function buildCancelLimitOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "cancelLimitOrder", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "cancelLimitOrder"
  >;
}

export function buildModifyLimitOrder(input: { orderId: bigint; newTargetPrice: bigint; newExpiresAt: bigint }) {
  const args = [
    requireOrderId(input.orderId),
    requirePositive(input.newTargetPrice),
    requirePositive(input.newExpiresAt),
  ] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "modifyLimitOrder", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "modifyLimitOrder"
  >;
}

export function buildGetUserLimitOrders(user: Address) {
  const args = [user] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "getUserLimitOrders", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "getUserLimitOrders"
  >;
}

export function buildGetLimitOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return { address: ORDERS, abi: pecorOrdersAbi, functionName: "getLimitOrder", args } satisfies SpotCall<
    typeof pecorOrdersAbi,
    "getLimitOrder"
  >;
}

/* ── stop rail, 0x1023 ──────────────────────────────────────────── */

export interface StopLimitCallInput {
  amount: bigint;
  /** Activation price, 18 decimals. */
  stopPrice: bigint;
  /** Post-activation limit price, 18 decimals. */
  limitPrice: bigint;
  expiresAt: bigint;
}

function requireStopPrices(stopPrice: bigint, limitPrice: bigint, isBuy: boolean): void {
  requirePositive(stopPrice);
  requirePositive(limitPrice);
  if (isBuy && limitPrice < stopPrice) refuse("VALIDATION_BELOW_MIN");
  if (!isBuy && limitPrice > stopPrice) refuse("VALIDATION_ABOVE_MAX");
}

/** A buy activates at stopPrice and fills at most at limitPrice, so limitPrice >= stopPrice. */
export function buildPlaceStopLimitBuy(input: StopLimitCallInput & { stablecoin: Address; token: Address }) {
  requireStopPrices(input.stopPrice, input.limitPrice, true);
  const args = [
    input.stablecoin,
    input.token,
    requirePositive(input.amount),
    input.stopPrice,
    input.limitPrice,
    requirePositive(input.expiresAt),
  ] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "placeStopLimitBuy",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "placeStopLimitBuy">;
}

/** A sell activates at stopPrice and fills at least at limitPrice, so limitPrice <= stopPrice. */
export function buildPlaceStopLimitSell(input: StopLimitCallInput & { token: Address; stablecoin: Address }) {
  requireStopPrices(input.stopPrice, input.limitPrice, false);
  const args = [
    input.token,
    input.stablecoin,
    requirePositive(input.amount),
    input.stopPrice,
    input.limitPrice,
    requirePositive(input.expiresAt),
  ] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "placeStopLimitSell",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "placeStopLimitSell">;
}

export function buildCancelStopLimitOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "cancelStopLimitOrder",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "cancelStopLimitOrder">;
}

export function buildPlaceStopLoss(input: {
  token: Address;
  stablecoin: Address;
  tokenAmount: bigint;
  /** Trigger price, 18 decimals: the sell fires when the mark is at or below it. */
  triggerPrice: bigint;
  expiresAt: bigint;
}) {
  const args = [
    input.token,
    input.stablecoin,
    requirePositive(input.tokenAmount),
    requirePositive(input.triggerPrice),
    requirePositive(input.expiresAt),
  ] as const;
  return { address: STOPS, abi: pecorStopOrdersAbi, functionName: "placeStopLoss", args } satisfies SpotCall<
    typeof pecorStopOrdersAbi,
    "placeStopLoss"
  >;
}

export function buildCancelStopLossOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "cancelStopLossOrder",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "cancelStopLossOrder">;
}

export function buildGetUserStopLimitOrders(user: Address) {
  const args = [user] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "getUserStopLimitOrders",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "getUserStopLimitOrders">;
}

export function buildGetUserStopLossOrders(user: Address) {
  const args = [user] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "getUserStopLossOrders",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "getUserStopLossOrders">;
}

export function buildGetStopLimitOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "getStopLimitOrder",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "getStopLimitOrder">;
}

export function buildGetStopLossOrder(orderId: bigint) {
  const args = [requireOrderId(orderId)] as const;
  return {
    address: STOPS,
    abi: pecorStopOrdersAbi,
    functionName: "getStopLossOrder",
    args,
  } satisfies SpotCall<typeof pecorStopOrdersAbi, "getStopLossOrder">;
}
