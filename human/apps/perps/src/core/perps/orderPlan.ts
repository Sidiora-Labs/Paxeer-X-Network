import { formatUnits } from "viem";

import { isUserFacingError, makeError } from "@/core/errors/classify";
import type { ErrorCode, UserFacingError } from "@/core/errors/types";
import { OrderType, TimeInForce } from "@/core/network/abi/PerpsPrecompile";
import type { DraftOrderLines } from "@/hooks/perps/useOrderLineStore";
import type { OpenPositionQuote } from "@/hooks/perps/useQuoter";
import { deriveImpliedCollateral, safeParseUnits } from "./amounts";
import { buildOpenPosition, buildPlaceLimitOrder, buildPlaceStopLimitOrder, ladderCloid } from "./calls";
import { COLLATERAL_TOKENS } from "./config";

/** Maximum age of a market quote at submit before it is re-fetched. */
export const QUOTE_MAX_AGE_MS = 6_000;
export const MIN_SCALED_ORDERS = 2;
export const MAX_SCALED_ORDERS = 20;

const CONTEXT = "perps.orderPlan";
const USD_DECIMALS = 18;
const ONE_X = 10n ** 18n;
const CENT = 10n ** 16n;
const COLLATERAL_DECIMALS = COLLATERAL_TOKENS.usdl.decimals;

export type OrderKind = "market" | "limit" | "stop-limit" | "scaled";
export type ScaledDistribution = "flat" | "asc" | "desc" | "custom";

export interface OrderOptions {
  postOnly: boolean;
  ioc: boolean;
  fok: boolean;
}

/** The order form inputs as PerpsOrderForm holds them: decimal strings and toggles. */
export interface OrderFormState extends OrderOptions {
  orderType: OrderKind;
  marketId: bigint;
  isLong: boolean;
  /** USDL amount of a market order. */
  collateralAmount: string;
  leverage: string;
  /** The limit price of a limit order and the trigger price of a stop-limit order. */
  triggerPrice: string;
  /** The limit price of a stop-limit order. */
  limitPrice: string;
  /** Notional of a limit, stop-limit or scaled order. */
  sizeUsd: string;
  scaledStartPrice: string;
  scaledEndPrice: string;
  scaledTotalOrders: string;
  scaledDistribution: ScaledDistribution;
  tpslEnabled: boolean;
  tpPrice: string;
  slPrice: string;
  reduceOnly: boolean;
  /** Client order id source: a resting order carries ladderCloid(ladderId, rung). */
  ladderId: bigint;
}

export type PlaceOrderCall = ReturnType<typeof buildOpenPosition>;

export interface OrderPlan {
  calls: readonly PlaceOrderCall[];
  error: UserFacingError | null;
}

function refusal(code: ErrorCode, message?: string): OrderPlan {
  const error = makeError(code, CONTEXT);
  return { calls: [], error: message === undefined ? error : { ...error, message } };
}

/** Leverage at 18 decimals; anything below 1x is 0n, which blocks the plan. */
export function parseLeverage(value: string): bigint {
  const parsed = safeParseUnits(value, USD_DECIMALS);
  return parsed < ONE_X ? 0n : parsed;
}

/** Turning one of post-only, IOC and FOK on turns the other two off. */
export function selectOrderOption(options: OrderOptions, option: keyof OrderOptions, on: boolean): OrderOptions {
  if (!on) return { ...options, [option]: false };
  return { postOnly: option === "postOnly", ioc: option === "ioc", fok: option === "fok" };
}

/**
 * The timeInForce an order sends: post-only, then IOC, then FOK; with none set a resting order is GTC
 * and a market order IOC. A market order never takes post-only, so it is never GTC or POST_ONLY.
 */
export function timeInForceOf(kind: OrderKind, options: OrderOptions): TimeInForce {
  if (kind === "market") return options.fok && !options.ioc ? TimeInForce.FOK : TimeInForce.IOC;
  if (options.postOnly) return TimeInForce.POST_ONLY;
  if (options.ioc) return TimeInForce.IOC;
  if (options.fok) return TimeInForce.FOK;
  return TimeInForce.GTC;
}

function weight(distribution: Exclude<ScaledDistribution, "custom">, rung: number, rungs: number): bigint {
  if (distribution === "asc") return BigInt(rung + 1);
  if (distribution === "desc") return BigInt(rungs - rung);
  return 1n;
}

/** Rung sizes of a ladder at integer precision, the remainder on the last rung. */
function rungSizes(total: bigint, distribution: Exclude<ScaledDistribution, "custom">, rungs: number): bigint[] {
  const weights = Array.from({ length: rungs }, (_, i) => weight(distribution, i, rungs));
  const sum = weights.reduce((a, b) => a + b, 0n);
  const sizes = weights.slice(0, -1).map((w) => (total * w) / sum);
  return [...sizes, total - sizes.reduce((a, b) => a + b, 0n)];
}

/** The rung count of a ladder, or the refusal code of an out-of-range or non-integer count. */
function parseRungs(value: string): number | ErrorCode {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return "VALIDATION_AMOUNT_INVALID";
  const rungs = Number(trimmed);
  if (rungs < MIN_SCALED_ORDERS) return "VALIDATION_BELOW_MIN";
  if (rungs > MAX_SCALED_ORDERS) return "VALIDATION_ABOVE_MAX";
  return rungs;
}

function planCalls(state: OrderFormState, quote: OpenPositionQuote | null, leverage: bigint): OrderPlan {
  const timeInForce = timeInForceOf(state.orderType, state);
  const takeProfitPrice = state.tpslEnabled ? safeParseUnits(state.tpPrice, USD_DECIMALS) : 0n;
  const stopLossPrice = state.tpslEnabled ? safeParseUnits(state.slPrice, USD_DECIMALS) : 0n;
  const sizeUsd = safeParseUnits(state.sizeUsd, USD_DECIMALS);
  const resting = {
    marketId: state.marketId,
    isLong: state.isLong,
    leverage,
    timeInForce,
    reduceOnly: state.reduceOnly,
  };

  switch (state.orderType) {
    case "market": {
      if (quote === null) return refusal("VALIDATION_REQUIRED_FIELD", "A market order needs a current quote.");
      const call = buildOpenPosition({
        marketId: state.marketId,
        isLong: state.isLong,
        collateralAmount: safeParseUnits(state.collateralAmount, COLLATERAL_DECIMALS),
        leverage,
        quote,
        timeInForce: timeInForce === TimeInForce.FOK ? TimeInForce.FOK : TimeInForce.IOC,
        takeProfitPrice,
        stopLossPrice,
      });
      return { calls: [call], error: null };
    }
    case "limit": {
      const call = buildPlaceLimitOrder({
        ...resting,
        limitPrice: safeParseUnits(state.triggerPrice, USD_DECIMALS),
        sizeUsd,
        collateralAmount: deriveImpliedCollateral(sizeUsd, leverage, COLLATERAL_DECIMALS),
        takeProfitPrice,
        stopLossPrice,
        cloid: ladderCloid(state.ladderId, 0),
      });
      return { calls: [call], error: null };
    }
    case "stop-limit": {
      const call = buildPlaceStopLimitOrder({
        ...resting,
        triggerPrice: safeParseUnits(state.triggerPrice, USD_DECIMALS),
        limitPrice: safeParseUnits(state.limitPrice, USD_DECIMALS),
        sizeUsd,
        collateralAmount: deriveImpliedCollateral(sizeUsd, leverage, COLLATERAL_DECIMALS),
        takeProfitPrice,
        stopLossPrice,
        cloid: ladderCloid(state.ladderId, 0),
      });
      return { calls: [call], error: null };
    }
    case "scaled": {
      const distribution = state.scaledDistribution;
      if (distribution === "custom") {
        return refusal("VALIDATION_REQUIRED_FIELD", "The custom distribution has no input; choose flat, ascending or descending.");
      }
      const rungs = parseRungs(state.scaledTotalOrders);
      if (typeof rungs === "string") {
        return refusal(rungs, `A scaled order takes ${MIN_SCALED_ORDERS} to ${MAX_SCALED_ORDERS} orders.`);
      }
      const start = safeParseUnits(state.scaledStartPrice, USD_DECIMALS);
      const end = safeParseUnits(state.scaledEndPrice, USD_DECIMALS);
      const span = BigInt(rungs - 1);
      const calls = rungSizes(sizeUsd, distribution, rungs).map((rungSize, i) =>
        buildPlaceLimitOrder({
          ...resting,
          limitPrice: start + ((end - start) * BigInt(i)) / span,
          sizeUsd: rungSize,
          collateralAmount: deriveImpliedCollateral(rungSize, leverage, COLLATERAL_DECIMALS),
          cloid: ladderCloid(state.ladderId, i),
        }),
      );
      return { calls, error: null };
    }
  }
}

/**
 * The ordered placeOrder calls handleSubmit sends for the form state, or no calls and the refusal.
 * `quote` is the current quoteOpenPosition of a market order; resting orders take null.
 */
export function buildOrderPlan(state: OrderFormState, quote: OpenPositionQuote | null): OrderPlan {
  const leverage = parseLeverage(state.leverage);
  if (leverage === 0n) return refusal("VALIDATION_BELOW_MIN", "Leverage must be at least 1x.");
  try {
    return planCalls(state, quote, leverage);
  } catch (err) {
    if (isUserFacingError(err)) return { calls: [], error: err };
    throw err;
  }
}

/** A quote older than QUOTE_MAX_AGE_MS is re-fetched and the bound rebuilt before submit. */
export function needsRequote(quoteAgeMs: number): boolean {
  return !(quoteAgeMs >= 0 && quoteAgeMs <= QUOTE_MAX_AGE_MS);
}

/** With orderConfirm on, nothing is signed until the confirmation is accepted. */
export function needsConfirmation(orderConfirm: boolean): boolean {
  return orderConfirm;
}

export interface OrderConfirmation {
  side: "long" | "short";
  /** Total notional of the plan, 18 decimals. */
  sizeUsd: bigint;
  /** The market bound, or the limit price of the first resting order; 18 decimals. */
  acceptablePrice: bigint;
  /** quoteOpenPosition tradingFeeUsd, 18 decimals; null for a resting order, which has no quote. */
  tradingFeeUsd: bigint | null;
  /** quoteOpenPosition liquidationPrice, 18 decimals; null for a resting order, which has no quote. */
  liquidationPrice: bigint | null;
  display: {
    side: string;
    size: string;
    acceptablePrice: string;
    fee: string | null;
    liquidationPrice: string | null;
  };
}

function usdCents(value: bigint): string {
  const cents = (value + CENT / 2n) / CENT;
  return `${cents / 100n}.${(cents % 100n).toString().padStart(2, "0")} USD`;
}

/** The confirmation content of a non-empty plan; fee and liquidation price come from the quote. */
export function buildConfirmation(quote: OpenPositionQuote | null, plan: OrderPlan): OrderConfirmation | null {
  const first = plan.calls[0]?.args[0];
  if (plan.error !== null || first === undefined) return null;
  const isMarket = first.orderType === OrderType.MARKET;
  const fee = isMarket && quote !== null ? quote.tradingFeeUsd : null;
  const liquidation = isMarket && quote !== null ? quote.liquidationPrice : null;
  const sizeUsd = plan.calls.reduce((total, c) => total + c.args[0].sizeUsd, 0n);
  const acceptablePrice = isMarket ? first.acceptablePrice : first.limitPrice;
  return {
    side: first.isLong ? "long" : "short",
    sizeUsd,
    acceptablePrice,
    tradingFeeUsd: fee,
    liquidationPrice: liquidation,
    display: {
      side: first.isLong ? "Long" : "Short",
      size: usdCents(sizeUsd),
      acceptablePrice: formatUnits(acceptablePrice, USD_DECIMALS),
      fee: fee === null ? null : usdCents(fee),
      liquidationPrice: liquidation === null ? null : formatUnits(liquidation, USD_DECIMALS),
    },
  };
}

export type ClearedInputs = Pick<
  OrderFormState,
  | "collateralAmount"
  | "triggerPrice"
  | "limitPrice"
  | "sizeUsd"
  | "tpPrice"
  | "slPrice"
  | "scaledStartPrice"
  | "scaledEndPrice"
>;

export interface SuccessReset {
  inputs: ClearedInputs;
  lines: Pick<DraftOrderLines, "entryPrice" | "tpPrice" | "slPrice">;
}

/** After a receipt with status success, the form inputs and the chart order lines clear together. */
export function successReset(status: "success" | "reverted"): SuccessReset | null {
  if (status !== "success") return null;
  return {
    inputs: {
      collateralAmount: "",
      triggerPrice: "",
      limitPrice: "",
      sizeUsd: "",
      tpPrice: "",
      slPrice: "",
      scaledStartPrice: "",
      scaledEndPrice: "",
    },
    lines: { entryPrice: 0, tpPrice: 0, slPrice: 0 },
  };
}
