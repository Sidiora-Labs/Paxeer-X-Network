import {
  OrderSide,
  OrderType,
  API as orderUtils,
} from "@paxeer-network/types";
import { Decimal, getTPSLDirection, zero } from "@paxeer-network/utils";
import { IMRFactorPower, IsoTakerFeeBuffer } from "./constants";

// ============ Backward Compatibility Types ============
/** @deprecated Use inline type or the new input type instead */
export type EstimatedLiquidationPriceInputs = {
  totalCollateral: number;
  markPrice: number;
  baseMMR: number;
  baseIMR: number;
  IMR_Factor: number;
  orderFee: number;
  positions: Pick<
    orderUtils.PositionExt,
    "position_qty" | "mark_price" | "symbol" | "mmr"
  >[];
  newOrder: {
    symbol: string;
    qty: number;
    price: number;
  };
};

/** @deprecated Use inline type or the new input type instead */
export type EstimatedLeverageInputs = {
  totalCollateral: number;
  positions: Pick<
    orderUtils.PositionExt,
    "position_qty" | "mark_price" | "symbol" | "mmr"
  >[];
  newOrder: {
    symbol: string;
    qty: number;
    price: number;
  };
};
// ====================================================

/**
 * Maximum price when placing an order
 */
export function maxPrice(markprice: number, range: number) {
  return markprice * (1 + range);
}

/**
 * Minimum price when placing an order
 */
export function minPrice(markprice: number, range: number) {
  return markprice * (1 - range);
}

/**
 * Scope price when placing an order
 * @returns number
 */
export function scopePrice(
  price: number,
  scope: number,
  side: "BUY" | "SELL",
): number {
  if (side === "BUY") {
    return price * (1 - scope);
  }
  return price * (1 + scope);
}

/**
 * Calculate the order fee
 */
export function orderFee(inputs: {
  /**
   * Order quantity
   */
  qty: number;
  price: number;
  futuresTakeFeeRate: number;
}): number {
  return new Decimal(inputs.qty)
    .mul(inputs.price)
    .mul(inputs.futuresTakeFeeRate)
    .toNumber();
}

/**
 * Calculate reference price for a **new order** based on business rules.
 *
 * The reference price is used by risk / margin formulas as the effective
 * execution price when the user is preparing a new order.
 *
 * Business rules (simplified):
 *
 * - LIMIT
 *   - BUY:   `reference = limit_price`
 *   - SELL:  `reference = max(limit_price, Bid1)`
 *   - If `limit_price` is not provided: same as MARKET
 *
 * - MARKET
 *   - With `markPrice` + `priceRange`: backend-aligned freezing price, i.e.
 *     `BUY = floor(markPrice * (1 + priceRange))`,
 *     `SELL = ceil(markPrice * (1 - priceRange))`
 *   - Otherwise (legacy fallback): `BUY = Ask1`, `SELL = Bid1`
 *
 * - STOP MARKET
 *   - If `stop_price` provided: `reference = stop_price`
 *   - Else: same as MARKET
 *
 * - STOP LIMIT
 *   - If `limit_price` provided: `reference = limit_price`
 *   - Else: same as MARKET
 *
 * - TRAILING STOP
 *   - If `trigger_price` provided: `reference = trigger_price`
 *   - Else: same as MARKET
 *
 * - BBO (LIMIT with ASK / BID as `orderTypeExt`)
 *   - BUY + ASK  => `reference = Ask1`
 *   - SELL + BID => `reference = Bid1`
 *   - BUY + BID  => `reference = Bid1`
 *   - SELL + ASK => `reference = Ask1`
 *
 * @param order     Lightweight description of the new order
 * @param askPrice  Ask1 price from orderbook
 * @param bidPrice  Bid1 price from orderbook
 * @returns Reference price or null if it cannot be determined
 */
export function getOrderReferencePrice(
  order: {
    /**
     * @description Order type (e.g. LIMIT, MARKET, STOP_LIMIT, STOP_MARKET, TRAILING_STOP, ASK, BID)
     */
    orderType: OrderType;
    /**
     * @description Extended order type for BBO orders (ASK / BID as LIMIT extensions)
     */
    orderTypeExt?: OrderType;
    /**
     * @description Order side (BUY or SELL)
     */
    side: OrderSide;
    /**
     * @description User input LIMIT price (for LIMIT / STOP_LIMIT orders)
     */
    limitPrice?: number;
    /**
     * Trigger price (for STOP_MARKET / STOP_LIMIT / TRAILING_STOP orders)
     */
    triggerPrice?: number;
    /**
     * Mark price of the symbol. When provided together with `priceRange`,
     * MARKET orders are priced from the mark price instead of Ask1/Bid1,
     * matching the backend risk-engine freezing reference price.
     */
    markPrice?: number;
    /**
     * Symbol `price_range` config (e.g. 0.1 = 10%) applied to `markPrice`
     * for MARKET reference pricing.
     */
    priceRange?: number;
    /**
     * Decimal places used to round MARKET reference prices (usually quote_dp).
     */
    pricePrecision?: number;
  },
  askPrice: number,
  bidPrice: number,
): number | null {
  /**
   * Helper: get MARKET-style reference price.
   *
   * The backend risk engine freezes MARKET orders at the mark price adjusted
   * by the symbol price range, not at the touch: BUY rounds
   * `markPrice * (1 + priceRange)` down, SELL rounds
   * `markPrice * (1 - priceRange)` up. Ask1/Bid1 remain as a fallback when
   * mark/config data is unavailable.
   */
  const getMarketRefPrice = (): number | null => {
    const mark = isValidPrice(order.markPrice) ? order.markPrice : undefined;
    const range =
      typeof order.priceRange === "number" && order.priceRange > 0
        ? order.priceRange
        : undefined;

    if (mark !== undefined && range !== undefined) {
      const raw =
        order.side === OrderSide.BUY
          ? new Decimal(mark).mul(new Decimal(1).add(range))
          : new Decimal(mark).mul(new Decimal(1).sub(range));

      const rounded =
        typeof order.pricePrecision === "number" && order.pricePrecision >= 0
          ? raw.toDecimalPlaces(
              order.pricePrecision,
              order.side === OrderSide.BUY
                ? Decimal.ROUND_FLOOR
                : Decimal.ROUND_CEIL,
            )
          : raw;

      return rounded.gt(0) ? rounded.toNumber() : null;
    }

    if (order.side === OrderSide.BUY) {
      return askPrice > 0 ? askPrice : null;
    }
    return bidPrice > 0 ? bidPrice : null;
  };

  const isValidPrice = (price?: number): price is number =>
    typeof price === "number" && Number.isFinite(price) && price > 0;

  const { orderType, orderTypeExt, side } = order;
  const limitPrice = isValidPrice(order.limitPrice)
    ? order.limitPrice
    : undefined;
  const triggerPrice = isValidPrice(order.triggerPrice)
    ? order.triggerPrice
    : undefined;

  // ---- BBO orders (LIMIT + ASK/BID extension) ----
  if (
    orderType === OrderType.LIMIT &&
    (orderTypeExt === OrderType.ASK || orderTypeExt === OrderType.BID)
  ) {
    // BID / ASK reference rules
    if (side === OrderSide.BUY) {
      // BUY ASK / BUY BID
      return orderTypeExt === OrderType.ASK
        ? isValidPrice(askPrice)
          ? askPrice
          : null
        : isValidPrice(bidPrice)
          ? bidPrice
          : null;
    }

    // SELL ASK / SELL BID
    return orderTypeExt === OrderType.ASK
      ? isValidPrice(askPrice)
        ? askPrice
        : null
      : isValidPrice(bidPrice)
        ? bidPrice
        : null;
  }

  switch (orderType) {
    case OrderType.LIMIT:
    case OrderType.IOC:
    case OrderType.FOK:
    case OrderType.POST_ONLY: {
      // LIMIT-family orders follow the same reference rules
      if (!limitPrice) {
        // No limit price yet -> behave as MARKET order
        return getMarketRefPrice();
      }

      if (side === OrderSide.BUY) {
        // LIMIT BUY: reference = limit price
        return limitPrice;
      }

      // LIMIT SELL: reference = max(limit_price, Bid1)
      const effectiveBid = isValidPrice(bidPrice) ? bidPrice : 0;
      return Math.max(limitPrice, effectiveBid);
    }

    case OrderType.MARKET: {
      return getMarketRefPrice();
    }

    case OrderType.STOP_MARKET: {
      if (triggerPrice) {
        // STOP price explicitly provided
        return triggerPrice;
      }
      // Same as MARKET when stop price not provided
      return getMarketRefPrice();
    }

    case OrderType.STOP_LIMIT: {
      if (limitPrice) {
        // Use LIMIT price for both BUY / SELL
        return limitPrice;
      }
      // Same as MARKET when limit price not provided
      return getMarketRefPrice();
    }

    case OrderType.TRAILING_STOP: {
      if (triggerPrice) {
        return triggerPrice;
      }
      // Fallback: behave as MARKET when trigger not defined yet
      return getMarketRefPrice();
    }

    default:
      // For unsupported order types we do not guess a reference price
      return null;
  }
}

/**
 * @formulaId estLiqPriceIsolated
 * @name Est. Position liq. Price (Isolated Margin)
 * @description
 * Estimate the liquidation price for an isolated-margin position after placing a new order.
 *
 * The underlying formula is:
 *
 * \[
 * liquidation\_price =
 * \frac{
 *   isolated\_position\_margin' - cost\_position' - funding\_adjustment
 * }{
 *   |position\_qty'| \cdot MMR' - position\_qty'
 * }
 * \]
 *
 * Where:
 * - `position_qty' = positionQty + orderSide * orderQty`
 * - `funding_adjustment = position_qty' * (sumUnitaryFunding - lastSumUnitaryFunding)`
 * - `MMR' = max(baseMMR, (baseMMR / baseIMR) * IMR_Factor * abs(position_qty' * reference_price)^(4/5))`
 *
 * Notes:
 * - This function only considers a **single isolated position** (no cross-margin collateral or other symbols).
 * - `newOrder.qty > 0` is treated as a BUY, `newOrder.qty < 0` as a SELL.
 *
 * @param inputs Estimation inputs for isolated-margin liquidation price
 * @returns Estimated liquidation price (in quote currency), or 0 when invalid / no position
 */
export function estLiqPriceIsolated(inputs: {
  /**
   * @description Current isolated margin of the position
   */
  isolatedPositionMargin: number;
  /**
   * @description Current position cost (qty * average entry price)
   */
  costPosition: number;
  /**
   * @description Current position quantity (positive for long, negative for short)
   */
  positionQty: number;
  /**
   * @description Current cumulative unitary funding
   */
  sumUnitaryFunding: number;
  /**
   * @description Last cumulative unitary funding at the last settlement
   */
  lastSumUnitaryFunding: number;
  /**
   * @description Current mark price of the symbol
   */
  markPrice: number;
  /**
   * @description Base maintenance margin rate
   */
  baseMMR: number;
  /**
   * @description Base initial margin rate
   */
  baseIMR: number;
  /**
   * @description IMR calculation factor
   */
  IMR_Factor: number;
  /**
   * @description Leverage for this isolated position
   */
  leverage: number;
  /**
   * @description Fee buffer reserved in isolated margin estimates (default: 0.0006)
   */
  isoTakerFeeBuffer?: number;
  /**
   * @description New order information used for estimation
   */
  newOrder: {
    /**
     * @description Symbol of the order (kept for interface consistency)
     */
    symbol: string;
    /**
     * @description Order quantity (positive for BUY, negative for SELL)
     */
    qty: number;
    /**
     * @description Order price (reference price when opening / adding / flipping)
     */
    price: number;
  };
}): number {
  const {
    isolatedPositionMargin,
    costPosition,
    positionQty,
    sumUnitaryFunding,
    lastSumUnitaryFunding,
    markPrice,
    baseMMR,
    baseIMR,
    IMR_Factor: IMRFactor,
    leverage,
    isoTakerFeeBuffer = IsoTakerFeeBuffer,
    newOrder,
  } = inputs;

  // newOrder.qty is signed: positive for BUY, negative for SELL
  const signedOrderQty = newOrder?.qty ?? 0;

  // Calculate new position quantity after order execution
  const newPositionQty = positionQty + signedOrderQty;

  // No position after order execution - cannot compute liquidation price
  if (newPositionQty === 0) {
    return 0;
  }

  // Reference price for margin/cost calculations
  const orderRefPrice = newOrder?.price ?? markPrice;

  // Validate reference prices
  if (markPrice <= 0 || (signedOrderQty !== 0 && orderRefPrice <= 0)) {
    return 0;
  }

  // Helper: check if two values have the same sign (both positive or both negative)
  const isSameSign = (a: number, b: number) =>
    (a > 0 && b > 0) || (a < 0 && b < 0);

  // Determine order scenario based on position and order relationship
  type OrderScenario = "NO_ORDER" | "OPEN_ADD" | "REDUCE" | "FLIP";
  const getScenario = (): OrderScenario => {
    if (signedOrderQty === 0) return "NO_ORDER";
    if (positionQty === 0 || isSameSign(signedOrderQty, positionQty))
      return "OPEN_ADD";
    if (isSameSign(positionQty, newPositionQty)) return "REDUCE";
    return "FLIP";
  };

  // Pre-compute Decimal instances to avoid redundant allocations
  const decNewPositionQty = new Decimal(newPositionQty);
  const decAbsNewPositionQty = decNewPositionQty.abs();
  const decCostPosition = new Decimal(costPosition);
  const decIsolatedMargin = new Decimal(isolatedPositionMargin);
  const decOrderCost = new Decimal(signedOrderQty).mul(orderRefPrice);
  const getMarginRate = () =>
    new Decimal(1).div(leverage).add(isoTakerFeeBuffer);

  // Calculate isolated_position_margin' and cost_position' based on scenario
  let newIsolatedPositionMargin: Decimal;
  let newCostPosition: Decimal;

  switch (getScenario()) {
    case "NO_ORDER":
      // Use current values unchanged
      newIsolatedPositionMargin = decIsolatedMargin;
      newCostPosition = decCostPosition;
      break;

    case "OPEN_ADD":
      // Add margin based on order notional with isolated fee buffer.
      newIsolatedPositionMargin = decIsolatedMargin.add(
        new Decimal(Math.abs(signedOrderQty))
          .mul(orderRefPrice)
          .mul(getMarginRate()),
      );
      newCostPosition = decCostPosition.add(decOrderCost);
      break;

    case "REDUCE":
      // Margin proportionally reduced based on remaining position ratio
      newIsolatedPositionMargin = decIsolatedMargin
        .mul(newPositionQty)
        .div(positionQty);
      newCostPosition = decCostPosition.add(decOrderCost);
      break;

    case "FLIP":
      // Completely new position in opposite direction
      newIsolatedPositionMargin = decAbsNewPositionQty
        .mul(orderRefPrice)
        .mul(getMarginRate());
      newCostPosition = decNewPositionQty.mul(orderRefPrice);
      break;
  }

  // Calculate funding adjustment: position_qty' * (sumUnitaryFunding - lastSumUnitaryFunding)
  const fundingAdjustment = decNewPositionQty.mul(
    new Decimal(sumUnitaryFunding).sub(lastSumUnitaryFunding),
  );

  // Calculate MMR' based on new position notional (using mark price for MMR calculation)
  const newPositionNotional = decAbsNewPositionQty.mul(markPrice);
  const dynamicMMR = new Decimal(baseMMR)
    .div(baseIMR)
    .mul(IMRFactor)
    .mul(newPositionNotional.toPower(IMRFactorPower))
    .toNumber();
  const newMMR = Math.max(baseMMR, dynamicMMR);

  // Calculate denominator: abs(position_qty') * MMR' - position_qty'
  const denominator = decAbsNewPositionQty.mul(newMMR).sub(decNewPositionQty);

  if (denominator.isZero()) {
    return 0;
  }

  // Calculate liquidation price: (margin' - cost' - funding) / denominator
  const liquidationPrice = newIsolatedPositionMargin
    .sub(newCostPosition)
    .sub(fundingAdjustment)
    .div(denominator)
    .toNumber();

  return Math.max(0, liquidationPrice);
}

/**
 * @formulaId estLiqPrice
 * @name Est. Position liq. Price
 * @description
 *
 * ## When user has positions:
 *
 * ```
 * Est. liq. Position Price = max(mark_price_i + (total_collateral_value - new_total_MM - order_fee) / (abs(position_qty_i + new_order_qty_i) * new_MMRi - (position_qty_i + new_order_qty_i)), 0)
 * ```
 *
 * ## When user has no positions:
 *
 * ```
 * Est. liq. Position Price = max(order_price_i + (total_collateral_value - new_total_MM - order_fee) / (abs(position_qty_i + new_order_qty_i) * new_MMRi - (position_qty_i + new_order_qty_i)), 0)
 * ```
 *
 * ## Formula Components:
 *
 * - `order_fee = new_order_qty_i * order_price_i * futures_take_fee_rate`
 * - `new_total_MM = sum(abs(position_qty_i * mark_price_i + new_order_qty_i * order_price_i)) * MMRi)`
 * - `new_MMRi = Max(Base_MMR_i, (Base_MMR_i / Base_IMR_i) * IMR_Factor_i * Abs(position_qty_i * mark_price_i + new_order_qty_i * limit_price_i)^(4/5))`
 *
 * ## Order Price Determination:
 *
 * ### Market Order:
 * - **Long order**: `order_price_i = ask0`
 * - **Short order**: `order_price_i = bid0`
 *
 * ### Limit Order:
 *
 * #### Long order:
 * - If `limit_price >= ask0`: `order_price_i = ask0`
 * - If `limit_price < ask0`: `order_price_i = limit_price`
 *
 * #### Short order:
 * - If `limit_price <= bid0`: `order_price_i = bid0`
 * - If `limit_price > bid0`: `order_price_i = limit_price`
 *
 * ## Parameter Definitions:
 *
 * | Parameter | Description |
 * |-----------|-------------|
 * | `Est. Position liq. Price` | Estimated liquidation price for the position |
 * | `position_qty_i` | Position quantity for a single symbol |
 * | `mark_price_i` | Mark price for a single symbol |
 * | `total_collateral_value` | Total asset value of user's account margin (USDC denominated) |
 * | `new_order_qty_i` | Symbol quantity when user prepares to open position (positive for long, negative for short) |
 * | `new_total_MM` | Sum of current position maintenance margin (including prepared order maintenance margin) |
 * | `new_MMR_i` | Maintenance margin rate for a single symbol (including prepared order notional consideration) |
 * | `Base_MMR_i` | Base maintenance margin rate for a single symbol |
 * | `Base_IMR_i` | Base initial margin rate for a single symbol |
 * | `IMR_Factor_i` | IMR calculation factor for a single symbol, from v1/client/info |
 * | `Position_Notional_i` | Sum of position notional for a single symbol |
 * | `order_fee` | Estimated order fee when user prepares to open position |
 * | `futures_take_fee_rate` | User's futures take fee rate, from GET /v1/client/info |
 * | `order_price_i` | Estimated execution price when user prepares to open position |
 * | `limit_price` | Price entered by user when preparing to open position |
 * | `ask0` | Minimum ask price from orderbook |
 * | `bid0` | Maximum bid price from orderbook |
 *
 * ## Examples:
 *
 * ### Market Order Example:
 * Long BTC qty = 0.1, mark price = 25986.2, ask0 = 26000, BTC position_qty_i = 0.2, ETH position_qty_i = -3
 * futures_take_fee_rate = 0.06%
 *
 * **Result**: BTC Est. Position liq. Price = 21268.7316
 *
 * ### Limit Order Example 1:
 * Long BTC qty = 0.1, mark price = 25986.2, ask0 = 26000, limit price = 25000, BTC position_qty_i = 0.2, ETH position_qty_i = -3
 *
 * **Result**: BTC Est. Position liq. Price = 21250.9772
 *
 * ### Limit Order Example 2:
 * Short BTC qty = -0.1, mark price = 25986.2, bid0 = 25900, limit price = 25000, BTC position_qty_i = 0.2, ETH position_qty_i = -3
 *
 * **Result**: BTC Est. Position liq. Price = 9102.17368
 *
 * ### No Position Example:
 * Long BTC qty = 0.1, mark price = 25986.2, ask0 = 26000, limit price = 25000
 *
 * **Result**: BTC Est. Position liq. Price = 5472
 *
 * @param inputs
 * @returns
 */
export function estLiqPrice(inputs: {
  totalCollateral: number;
  markPrice: number;
  baseMMR: number;
  baseIMR: number;
  IMR_Factor: number;
  orderFee: number;
  positions: {
    position_qty: number;
    mark_price: number;
    symbol: string;
    mmr: number;
  }[];
  newOrder: {
    symbol: string;
    qty: number;
    price: number;
  };
}): number {
  const {
    positions,
    newOrder,
    totalCollateral,
    markPrice,
    baseIMR,
    baseMMR,
    orderFee,
    IMR_Factor,
  } = inputs;
  // opened positions for the symbol
  let currentPosition:
    | Pick<
        orderUtils.PositionExt,
        "position_qty" | "mark_price" | "symbol" | "mmr"
      >
    | undefined = undefined;

  let newTotalMM = zero;

  const hasPosition =
    positions.filter((item) => item.position_qty > 0).length > 0;

  const basePrice = hasPosition ? markPrice : newOrder.price;

  const newOrderNotional = new Decimal(newOrder.qty).mul(newOrder.price);

  for (let index = 0; index < positions.length; index++) {
    const position = positions[index];
    let notional = new Decimal(position.position_qty).mul(position.mark_price);
    if (newOrder.symbol === position.symbol) {
      currentPosition = position;
      notional = notional.add(newOrderNotional);
    }

    newTotalMM = newTotalMM.add(notional.abs().mul(position.mmr));
  }

  // if no position
  if (!currentPosition) {
    newTotalMM = newTotalMM.add(newOrderNotional.mul(baseMMR));
  }

  const newMMR = Math.max(
    baseMMR,
    new Decimal(baseMMR)
      .div(baseIMR)
      .mul(IMR_Factor)
      .mul(
        newOrderNotional
          .add(
            !!currentPosition
              ? new Decimal(currentPosition.position_qty).mul(
                  currentPosition.mark_price,
                )
              : zero,
          )
          .abs(),
      )
      .toPower(4 / 5)
      .toNumber(),
  );

  // console.log("new MMR", newMMR, newTotalMM.toNumber());

  const newQty = new Decimal(newOrder.qty).add(
    currentPosition?.position_qty ?? 0,
  );

  if (newQty.eq(0)) {
    return 0;
  }

  const denominator = newQty.abs().mul(newMMR).sub(newQty);

  if (denominator.eq(zero)) {
    return 0;
  }

  const price = new Decimal(basePrice)
    .add(
      new Decimal(totalCollateral)
        .sub(newTotalMM)
        .sub(orderFee)
        .div(denominator),
    )
    .toNumber();

  return Math.max(0, price);
}

/**
 * Estimated leverage
 * @param inputs EstimtedLeverageInputs
 * @returns number
 */
export function estLeverage(inputs: {
  totalCollateral: number;
  positions: Pick<
    orderUtils.PositionExt,
    "position_qty" | "mark_price" | "symbol"
  >[];
  newOrder: {
    symbol: string;
    qty: number;
    price: number;
  };
}): number | null {
  const { totalCollateral, positions, newOrder } = inputs;
  if (totalCollateral <= 0) {
    return null;
  }
  let hasPosition = false;
  let sumPositionNotional = positions.reduce((acc, cur) => {
    let count = new Decimal(cur.position_qty).mul(cur.mark_price);
    // acc = acc.add(
    //   new Decimal(cur.position_qty).mul(cur.mark_price)
    //   // .abs()
    // );

    if (cur.symbol === newOrder.symbol) {
      hasPosition = true;
      // acc = acc.add(new Decimal(newOrder.qty).mul(newOrder.price));
      count = count.add(new Decimal(newOrder.qty).mul(newOrder.price));
    }

    return acc.add(count.abs());
  }, zero);

  if (!hasPosition) {
    sumPositionNotional = sumPositionNotional.add(
      new Decimal(newOrder.qty).mul(newOrder.price).abs(),
    );
  }

  if (sumPositionNotional.eq(zero)) {
    return null;
  }

  const totalMarginRatio = new Decimal(totalCollateral).div(
    sumPositionNotional,
  );

  return new Decimal(1)
    .div(totalMarginRatio)
    .toDecimalPlaces(2, Decimal.ROUND_HALF_EVEN)
    .toNumber();
}

// ROI = (close price - order_price) / order_price × leverage × direction
// leverage = MIN( current_account_leverage, symbol_leverage)
export function tpslROI(inputs: {
  side: OrderSide;
  type: "tp" | "sl";
  closePrice: number;
  orderPrice: number;
  leverage: number;
}) {
  const direction = getTPSLDirection({
    side: inputs.side,
    type: inputs.type,
    closePrice: inputs.closePrice,
    orderPrice: inputs.orderPrice,
  });

  const { closePrice, orderPrice, leverage } = inputs;
  return new Decimal(closePrice)
    .minus(orderPrice)
    .div(orderPrice)
    .mul(leverage)
    .abs()
    .mul(direction)
    .toNumber();
}
