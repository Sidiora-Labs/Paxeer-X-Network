import {
  numberToHex,
  zeroAddress,
  type AbiStateMutability,
  type Address,
  type ContractFunctionArgs,
  type ContractFunctionName,
  type ContractFunctionReturnType,
  type Hex,
} from "viem";

import { makeError } from "@/core/errors/classify";
import type { ErrorCode } from "@/core/errors/types";
import { PerpsPrecompileABI, OrderType, TimeInForce } from "@/core/network/abi/PerpsPrecompile";
import {
  PERPS_SPENDER,
  planApproval,
  type ApproveCall,
  type NativeTokens,
} from "@/core/network/approvals";
import { PRECOMPILES } from "@/core/network/precompiles";
import { BPS_DENOMINATOR, DEFAULT_MARKET_DEADLINE_SECONDS, buildDeadline } from "@/core/spot/slippage";
import { COLLATERAL_TOKENS, DEFAULT_SLIPPAGE_BPS } from "./config";

type PerpsAbi = typeof PerpsPrecompileABI;
type PerpsFunction = ContractFunctionName<PerpsAbi>;

/** A call at the perpetuals precompile whose args are checked against PerpsPrecompileABI. */
export interface PerpsCall<
  F extends PerpsFunction,
  A extends ContractFunctionArgs<PerpsAbi, AbiStateMutability, F> = ContractFunctionArgs<
    PerpsAbi,
    AbiStateMutability,
    F
  >,
> {
  address: Address;
  abi: PerpsAbi;
  functionName: F;
  args: A;
}

export type OrderParams = ContractFunctionArgs<PerpsAbi, "nonpayable", "placeOrder">[0];
export type AccountState = ContractFunctionReturnType<PerpsAbi, "view", "getAccount">;

const USDL: Address = COLLATERAL_TOKENS.usdl.address;
const ONE_X = 10n ** 18n;
const NO_CLOID: Hex = "0x00000000000000000000000000000000";
const CLOID_RE = /^0x[0-9a-fA-F]{32}$/;
const MAX_LADDER_ID = 1n << 96n;
const MAX_RUNG = 2 ** 32;

function call<F extends PerpsFunction, A extends ContractFunctionArgs<PerpsAbi, AbiStateMutability, F>>(
  functionName: F,
  args: A,
): PerpsCall<F, A> {
  return { address: PRECOMPILES.perps, abi: PerpsPrecompileABI, functionName, args };
}

function refuse(code: ErrorCode): never {
  throw makeError(code, "perps.calls");
}

function requirePositive(value: bigint): bigint {
  if (value <= 0n) refuse("VALIDATION_AMOUNT_INVALID");
  return value;
}

function requireLeverage(leverage: bigint): bigint {
  if (leverage < ONE_X) refuse("VALIDATION_BELOW_MIN");
  return leverage;
}

function requireAccount(account: Address): Address {
  if (account.toLowerCase() === zeroAddress) refuse("WALLET_NOT_CONNECTED");
  return account;
}

/**
 * Refuse a take-profit / stop-loss pair that does not straddle the reference price:
 * a long needs stopLoss < reference < takeProfit and a short the mirror. 0 leaves a side unset.
 */
function requireTpSl(isLong: boolean, reference: bigint, takeProfitPrice: bigint, stopLossPrice: bigint): void {
  if (takeProfitPrice < 0n || stopLossPrice < 0n) refuse("TRADE_TP_SL_INVALID");
  const tpOk = takeProfitPrice === 0n || (isLong ? takeProfitPrice > reference : takeProfitPrice < reference);
  const slOk = stopLossPrice === 0n || (isLong ? stopLossPrice < reference : stopLossPrice > reference);
  if (!tpOk || !slOk) refuse("TRADE_TP_SL_INVALID");
}

/**
 * The price bound of a market-priced write, a bigint at 18 decimals:
 * a long open or a short close pays at most price * (10000 + bps) / 10000,
 * a short open or a long close receives at least price * (10000 - bps) / 10000.
 */
export function acceptablePrice(
  price: bigint,
  isLong: boolean,
  action: "open" | "close",
  bps: number = DEFAULT_SLIPPAGE_BPS,
): bigint {
  requirePositive(price);
  if (!Number.isInteger(bps) || bps < 0 || bps >= 10_000) {
    throw new RangeError(`bps must be an integer in [0, 10000), got ${bps}`);
  }
  const buys = isLong === (action === "open");
  const slip = BigInt(bps);
  return (price * (buys ? BPS_DENOMINATOR + slip : BPS_DENOMINATOR - slip)) / BPS_DENOMINATOR;
}

/** The bytes16 client order id of rung `rung` of ladder `ladderId`: ladderId in the high 12 bytes, rung in the low 4. */
export function ladderCloid(ladderId: bigint, rung: number): Hex {
  if (ladderId <= 0n || ladderId >= MAX_LADDER_ID) {
    throw new RangeError(`ladderId must be in (0, 2^96), got ${ladderId}`);
  }
  if (!Number.isInteger(rung) || rung < 0 || rung >= MAX_RUNG) {
    throw new RangeError(`rung must be an integer in [0, 2^32), got ${rung}`);
  }
  return numberToHex((ladderId << 32n) | BigInt(rung), { size: 16 });
}

function requireCloid(cloid: Hex): Hex {
  if (!CLOID_RE.test(cloid) || BigInt(cloid) === 0n) {
    throw new RangeError(`cloid must be a non-zero bytes16, got ${cloid}`);
  }
  return cloid;
}

function marketDeadline(): bigint {
  return buildDeadline(DEFAULT_MARKET_DEADLINE_SECONDS);
}

function placeOrder(p: OrderParams): PerpsCall<"placeOrder", readonly [OrderParams]> {
  return call("placeOrder", [p] as const);
}

/* ── position writes ────────────────────────────────────────────── */

export interface OpenQuote {
  /** quoteOpenPosition entryPrice, 18 decimals. */
  entryPrice: bigint;
  /** quoteOpenPosition sizeUsd, 18 decimals. */
  sizeUsd: bigint;
}

export interface OpenPositionInput {
  marketId: bigint;
  isLong: boolean;
  /** USDL base units (6 decimals). */
  collateralAmount: bigint;
  /** 18-decimal multiple, at least 1e18. */
  leverage: bigint;
  quote: OpenQuote;
  timeInForce?: typeof TimeInForce.IOC | typeof TimeInForce.FOK;
  takeProfitPrice?: bigint;
  stopLossPrice?: bigint;
}

export function buildOpenPosition(input: OpenPositionInput) {
  const takeProfitPrice = input.takeProfitPrice ?? 0n;
  const stopLossPrice = input.stopLossPrice ?? 0n;
  requirePositive(input.collateralAmount);
  requireLeverage(input.leverage);
  requirePositive(input.quote.sizeUsd);
  requireTpSl(input.isLong, requirePositive(input.quote.entryPrice), takeProfitPrice, stopLossPrice);
  return placeOrder({
    marketId: input.marketId,
    isLong: input.isLong,
    orderType: OrderType.MARKET,
    timeInForce: input.timeInForce ?? TimeInForce.IOC,
    sizeUsd: input.quote.sizeUsd,
    leverage: input.leverage,
    limitPrice: 0n,
    triggerPrice: 0n,
    acceptablePrice: acceptablePrice(input.quote.entryPrice, input.isLong, "open"),
    collateralAmount: input.collateralAmount,
    reduceOnly: false,
    takeProfitPrice,
    stopLossPrice,
    deadline: marketDeadline(),
    cloid: NO_CLOID,
  });
}

export interface AddSizeInput {
  position: { marketId: bigint; isLong: boolean };
  /** USDL base units (6 decimals). */
  additionalCollateral: bigint;
  /** 18-decimal multiple, at least 1e18. */
  leverage: bigint;
  quote: OpenQuote;
}

export function buildAddSize(input: AddSizeInput) {
  requirePositive(input.additionalCollateral);
  requireLeverage(input.leverage);
  requirePositive(input.quote.sizeUsd);
  requirePositive(input.quote.entryPrice);
  return placeOrder({
    marketId: input.position.marketId,
    isLong: input.position.isLong,
    orderType: OrderType.MARKET,
    timeInForce: TimeInForce.IOC,
    sizeUsd: input.quote.sizeUsd,
    leverage: input.leverage,
    limitPrice: 0n,
    triggerPrice: 0n,
    acceptablePrice: acceptablePrice(input.quote.entryPrice, input.position.isLong, "open"),
    collateralAmount: input.additionalCollateral,
    reduceOnly: false,
    takeProfitPrice: 0n,
    stopLossPrice: 0n,
    deadline: marketDeadline(),
    cloid: NO_CLOID,
  });
}

export interface ClosePositionInput {
  positionId: bigint;
  isLong: boolean;
  /** The position size, 18 decimals. */
  sizeUsd: bigint;
  /** quoteClosePosition exitPrice, 18 decimals. */
  exitPrice: bigint;
}

export function buildClosePosition(input: ClosePositionInput) {
  return call("closePositionWithParams", [
    input.positionId,
    requirePositive(input.sizeUsd),
    acceptablePrice(input.exitPrice, input.isLong, "close"),
    marketDeadline(),
  ] as const);
}

export interface PartialCloseInput {
  positionId: bigint;
  isLong: boolean;
  /** 18 decimals; must be positive, since 0 closes the whole position. */
  closeSizeUsd: bigint;
  /** quotePartialClose exitPrice, 18 decimals. */
  exitPrice: bigint;
}

export function buildPartialClose(input: PartialCloseInput) {
  return call("closePositionWithParams", [
    input.positionId,
    requirePositive(input.closeSizeUsd),
    acceptablePrice(input.exitPrice, input.isLong, "close"),
    marketDeadline(),
  ] as const);
}

export interface SetPositionTpSlInput {
  positionId: bigint;
  isLong: boolean;
  /** 18 decimals; 0 clears the side. */
  takeProfitPrice: bigint;
  /** 18 decimals; 0 clears the side. */
  stopLossPrice: bigint;
}

export function buildSetPositionTpSl(input: SetPositionTpSlInput) {
  const { isLong, takeProfitPrice: tp, stopLossPrice: sl } = input;
  if (tp < 0n || sl < 0n) refuse("TRADE_TP_SL_INVALID");
  if (tp !== 0n && sl !== 0n && (isLong ? sl >= tp : sl <= tp)) refuse("TRADE_TP_SL_INVALID");
  return call("setPositionTpSl", [input.positionId, tp, sl] as const);
}

/** USDL base units (6 decimals). */
export function buildAddCollateral(positionId: bigint, amount: bigint) {
  return call("addCollateral", [positionId, requirePositive(amount)] as const);
}

/** USDL base units (6 decimals). */
export function buildRemoveCollateral(positionId: bigint, amount: bigint) {
  return call("removeCollateral", [positionId, requirePositive(amount)] as const);
}

/** USDL base units (6 decimals). */
export function buildTopUpPosition(positionId: bigint, amount: bigint) {
  return call("topUpPosition", [positionId, requirePositive(amount)] as const);
}

/* ── orders ─────────────────────────────────────────────────────── */

export interface LimitOrderInput {
  marketId: bigint;
  isLong: boolean;
  /** 18 decimals. */
  limitPrice: bigint;
  /** 18 decimals. */
  sizeUsd: bigint;
  /** 18-decimal multiple, at least 1e18. */
  leverage: bigint;
  /** USDL base units (6 decimals). */
  collateralAmount: bigint;
  timeInForce: TimeInForce;
  reduceOnly: boolean;
  takeProfitPrice?: bigint;
  stopLossPrice?: bigint;
  cloid: Hex;
}

export interface StopLimitOrderInput extends LimitOrderInput {
  /** 18 decimals. */
  triggerPrice: bigint;
}

function restingOrder(input: LimitOrderInput, orderType: OrderType, triggerPrice: bigint, reference: bigint) {
  const takeProfitPrice = input.takeProfitPrice ?? 0n;
  const stopLossPrice = input.stopLossPrice ?? 0n;
  requirePositive(input.sizeUsd);
  requirePositive(input.collateralAmount);
  requireLeverage(input.leverage);
  requirePositive(input.limitPrice);
  requireTpSl(input.isLong, requirePositive(reference), takeProfitPrice, stopLossPrice);
  const immediate = input.timeInForce === TimeInForce.IOC || input.timeInForce === TimeInForce.FOK;
  return placeOrder({
    marketId: input.marketId,
    isLong: input.isLong,
    orderType,
    timeInForce: input.timeInForce,
    sizeUsd: input.sizeUsd,
    leverage: input.leverage,
    limitPrice: input.limitPrice,
    triggerPrice,
    acceptablePrice: 0n,
    collateralAmount: input.collateralAmount,
    reduceOnly: input.reduceOnly,
    takeProfitPrice,
    stopLossPrice,
    deadline: immediate ? marketDeadline() : 0n,
    cloid: requireCloid(input.cloid),
  });
}

/** A LIMIT placeOrder; an IOC or FOK order carries the market deadline, a resting one none. */
export function buildPlaceLimitOrder(input: LimitOrderInput) {
  return restingOrder(input, OrderType.LIMIT, 0n, input.limitPrice);
}

export function buildPlaceStopLimitOrder(input: StopLimitOrderInput) {
  return restingOrder(input, OrderType.STOP_LIMIT, requirePositive(input.triggerPrice), input.triggerPrice);
}

export function buildCancelOrder(orderId: bigint) {
  return call("cancelOrder", [orderId] as const);
}

export function buildCancelByCloid(marketId: bigint, cloid: Hex) {
  return call("cancelByCloid", [marketId, requireCloid(cloid)] as const);
}

/* ── account ────────────────────────────────────────────────────── */

/** deposit(USDL, amount) with amount in USDL base units; the first deposit opens the account. */
export function buildDeposit(amount: bigint) {
  return call("deposit", [USDL, requirePositive(amount)] as const);
}

/** withdraw(USDL, amount) with amount in USDL base units. */
export function buildWithdraw(amount: bigint) {
  return call("withdraw", [USDL, requirePositive(amount)] as const);
}

export type DepositPlan = readonly (ApproveCall | ReturnType<typeof buildDeposit>)[];

/** The exact USDL approval to 0x1015 when the allowance is short, followed by the deposit. */
export async function buildDepositPlan(
  amount: bigint,
  allowance: () => Promise<bigint>,
  native: NativeTokens,
): Promise<DepositPlan> {
  const deposit = buildDeposit(amount);
  const approvals = await planApproval(USDL, PERPS_SPENDER, allowance, amount, native);
  return [...approvals, deposit];
}

/** Withdraw everything getAccount(account) reports withdrawable, never the idle balance. */
export function buildWithdrawAll(accountState: Pick<AccountState, "withdrawable">) {
  return buildWithdraw(accountState.withdrawable);
}

/* ── reads ──────────────────────────────────────────────────────── */

export function buildGetAccount(account: Address) {
  return call("getAccount", [requireAccount(account)] as const);
}

export function buildGetAccountSummary(account: Address, token: Address) {
  return call("getAccountSummary", [requireAccount(account), token] as const);
}

export function buildGetBalance(account: Address, token: Address) {
  return call("getBalance", [requireAccount(account), token] as const);
}

export function buildGetLockedBalance(account: Address, token: Address) {
  return call("getLockedBalance", [requireAccount(account), token] as const);
}

export function buildGetPositionMargin(positionId: bigint) {
  return call("getPositionMargin", [positionId] as const);
}

export function buildGetPosition(positionId: bigint) {
  return call("getPosition", [positionId] as const);
}

export function buildGetPositionDetails(positionId: bigint) {
  return call("getPositionDetails", [positionId] as const);
}

export function buildGetUserPositionIds(account: Address) {
  return call("getUserPositionIds", [requireAccount(account)] as const);
}

export function buildGetOrder(orderId: bigint) {
  return call("getOrder", [orderId] as const);
}

export function buildGetUserOrderIds(account: Address) {
  return call("getUserOrderIds", [requireAccount(account)] as const);
}
