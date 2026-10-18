import { useMemo, useRef } from "react";
import { OrderSide } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { OrderValidationResult } from "../useOrderEntry";
import { ERROR_MSG_CODES } from "./errorMsgCodes";

type TpslPriceParams = {
  /// default is 0.01
  warning_threshold?: number;
  slPrice?: string;
  liqPrice: number | null;
  side?: OrderSide;
  /**
   * Mark price for the symbol. When set with `side`, liq values that `useGetEstLiqPrice` would
   * hide (invalid vs mark — e.g. long est. liq above mark) are ignored here too so SL checks do
   * not run against a number the UI does not show as “Est. liq. price”.
   */
  markPrice?: number | null;
  /** Current position qty (signed: positive=Long, negative=Short). If missing, treated as is_reducing=true, skip liq check. */
  currentPosition?: number;
  /** Order quantity (non-negative, sign derived from side). If missing or 0, treated as is_reducing=true, skip liq check. */
  orderQuantity?: number;
};

// deep compare function
const isEqual = (
  a: OrderValidationResult | null,
  b: OrderValidationResult | null,
): boolean => {
  if (a === b) return true;
  if (a === null || b === null) return false;
  const aItem = a.sl_trigger_price;
  const bItem = b.sl_trigger_price;
  if (aItem === bItem) return true;
  if (aItem === undefined || bItem === undefined) return false;
  return aItem.type === bItem.type && aItem.message === bItem.message;
};

/**
 * Calculate the distance ratio between stop loss price and liquidation price
 * @param slPriceDecimal Stop loss price (Decimal)
 * @param liqPriceDecimal Liquidation price (Decimal)
 * @param side Order side
 * @returns Distance ratio: Long is (sl - liq) / liq, Short is (liq - sl) / liq
 */
function calculateDistanceRatio(
  slPriceDecimal: Decimal,
  liqPriceDecimal: Decimal,
  side: OrderSide,
): Decimal {
  return side === OrderSide.BUY
    ? slPriceDecimal.minus(liqPriceDecimal).div(liqPriceDecimal)
    : liqPriceDecimal.minus(slPriceDecimal).div(liqPriceDecimal);
}

/**
 * Check and return the corresponding validation result based on distance ratio
 * @param distance_ratio Distance ratio
 * @param warning_threshold Warning threshold
 * @returns Validation result: null (no warning), warning, or error
 */
function checkDistanceAndReturnResult(
  distance_ratio: Decimal,
  warning_threshold: number,
): OrderValidationResult | null {
  // distance_ratio > warning_threshold → no warning
  if (distance_ratio.gt(warning_threshold)) {
    return null;
  }

  // 0 < distance_ratio <= warning_threshold → warning
  if (distance_ratio.gt(0) && distance_ratio.lte(warning_threshold)) {
    return {
      sl_trigger_price: {
        type: ERROR_MSG_CODES.SL_PRICE_WARNING,
        message:
          "Stop losses set near the liq. price may not trigger. Note: the liq. price can change with position notional.",
      },
    };
  }

  // distance_ratio <= 0 → error (invalid, blocks execution)
  return {
    sl_trigger_price: {
      type: ERROR_MSG_CODES.SL_PRICE_ERROR,
      message: "Stop loss crosses the liq. price. Please adjust your SL.",
    },
  };
}

export const useTpslPriceChecker = (
  params: TpslPriceParams,
): OrderValidationResult | null => {
  const {
    warning_threshold = 0.01,
    slPrice,
    liqPrice,
    side,
    markPrice,
    currentPosition,
    orderQuantity,
  } = params;

  // use useRef to store the previous result
  const prevResultRef = useRef<OrderValidationResult | null>(null);

  /**
   * Basic validation logic: Check the distance between stop loss price and liquidation price based only on order side
   * Applicable when position information or order quantity cannot be obtained
   * @param slPriceDecimal Stop loss price (Decimal)
   * @param liqPriceDecimal Liquidation price (Decimal)
   * @returns Validation result: null (passed), warning, or error
   */
  function validateSlPriceBasic(
    slPriceDecimal: Decimal,
    liqPriceDecimal: Decimal,
  ): OrderValidationResult | null {
    if (!side) return null;

    const distance_ratio = calculateDistanceRatio(
      slPriceDecimal,
      liqPriceDecimal,
      side,
    );

    return checkDistanceAndReturnResult(distance_ratio, warning_threshold);
  }

  /**
   * Enhanced validation logic: Consider current position and order quantity to determine if it's a reducing position operation
   * Skip liquidation price check for reducing position operations, only perform strict validation for increasing position or reverse operations
   * @param slPriceDecimal Stop loss price (Decimal)
   * @param liqPriceDecimal Liquidation price (Decimal)
   * @param currentPosition Current position quantity (signed: positive=Long, negative=Short)
   * @param orderQuantity Order quantity (non-negative, sign derived from order side)
   * @returns Validation result: null (passed or check skipped), warning, or error
   */
  function validateSlPriceWithPosition(
    slPriceDecimal: Decimal,
    liqPriceDecimal: Decimal,
    currentPosition: number,
    orderQuantity: number,
  ): OrderValidationResult | null {
    if (!side) return null;

    // Cannot compute is_reducing: treat as is_reducing=true, skip liq check and warning
    const canComputeIsReducing =
      currentPosition !== undefined &&
      orderQuantity !== undefined &&
      orderQuantity > 0;
    if (!canComputeIsReducing) {
      return null;
    }

    // orderQty: signed (Buy=positive, Sell=negative)
    const orderQty =
      side === OrderSide.SELL
        ? -((orderQuantity as number) || 0)
        : (orderQuantity as number) || 0;
    const cur = (currentPosition as number) ?? 0;
    const net = new Decimal(cur).plus(orderQty).toNumber();
    const is_reducing = new Decimal(Math.abs(net)).lt(Math.abs(cur));

    const posSide = currentPosition > 0 ? OrderSide.BUY : OrderSide.SELL;
    const isSameSide = posSide === side;

    // is_reducing: skip liq check and warning
    if (is_reducing && isSameSide) {
      return null;
    }

    // Liquidation check: only when !is_reducing
    // net > 0 (Long): sl <= liq → Error
    // net < 0 (Short): sl >= liq → Error
    if (net > 0 && slPriceDecimal.lte(liqPriceDecimal)) {
      return {
        sl_trigger_price: {
          type: ERROR_MSG_CODES.SL_PRICE_ERROR,
          message: "Stop loss crosses the liq. price. Please adjust your SL.",
        },
      };
    }
    if (net < 0 && slPriceDecimal.gte(liqPriceDecimal)) {
      return {
        sl_trigger_price: {
          type: ERROR_MSG_CODES.SL_PRICE_ERROR,
          message: "Stop loss crosses the liq. price. Please adjust your SL.",
        },
      };
    }

    // Warning: distance_ratio near liq (only when we did liq check and no error)
    const distance_ratio = calculateDistanceRatio(
      slPriceDecimal,
      liqPriceDecimal,
      side,
    );

    return checkDistanceAndReturnResult(distance_ratio, warning_threshold);
  }

  // calculate current result
  const currentResult = useMemo(() => {
    if (
      slPrice === undefined ||
      liqPrice === undefined ||
      side === undefined ||
      liqPrice === null
    ) {
      return null;
    }

    // Align with `useGetEstLiqPrice`: do not validate SL against liq the UI hides as invalid.
    if (markPrice != null && Number.isFinite(markPrice) && markPrice > 0) {
      if (side === OrderSide.BUY && liqPrice > markPrice) {
        return null;
      }
      if (side === OrderSide.SELL && liqPrice < markPrice) {
        return null;
      }
    }
    // Reject non-positive / non-finite liq so we never compare SL against a bogus anchor.
    if (
      typeof liqPrice === "number" &&
      (!Number.isFinite(liqPrice) || liqPrice <= 0)
    ) {
      return null;
    }

    // Convert prices to Decimal for precise calculation
    let slPriceDecimal: Decimal;
    let liqPriceDecimal: Decimal;

    try {
      slPriceDecimal = new Decimal(slPrice);
      liqPriceDecimal = new Decimal(liqPrice);
    } catch (e) {
      return null;
    }

    // Validate that both prices are valid and not zero
    const slPriceNum = slPriceDecimal.toNumber();
    const liqPriceNum = liqPriceDecimal.toNumber();

    if (
      Number.isNaN(slPriceNum) ||
      Number.isNaN(liqPriceNum) ||
      !Number.isFinite(slPriceNum) ||
      !Number.isFinite(liqPriceNum) ||
      liqPriceDecimal.isZero() ||
      slPriceDecimal.isZero()
    ) {
      return null;
    }

    if (
      currentPosition !== undefined &&
      orderQuantity !== undefined &&
      orderQuantity > 0
    ) {
      // Use enhanced validation when position and order quantity are available
      return validateSlPriceWithPosition(
        slPriceDecimal,
        liqPriceDecimal,
        currentPosition,
        orderQuantity,
      );
    }

    // Fallback to basic validation when position info is unavailable
    return validateSlPriceBasic(slPriceDecimal, liqPriceDecimal);
  }, [
    slPrice,
    liqPrice,
    side,
    markPrice,
    warning_threshold,
    currentPosition,
    orderQuantity,
  ]);

  // if the content is the same, return the previous reference; otherwise, update and return the new reference
  if (isEqual(prevResultRef.current, currentResult)) {
    return prevResultRef.current;
  }

  prevResultRef.current = currentResult;
  return currentResult;
};
