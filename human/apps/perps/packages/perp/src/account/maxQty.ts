import { OrderSide } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

export type ResultOptions = {
  dp: number;
};

export type MaxQtyInputs = {
  symbol: string;

  /**
   * @description Maximum quantity limit for opening a single position, /v1/public/info.base_max
   */
  baseMaxQty: number;
  /**
   * Total collateral of the user (denominated in USDC), can be calculated from totalCollateral.
   * @see totalCollateral
   */
  totalCollateral: number;
  maxLeverage: number;
  baseIMR: number;
  /**
   * @see otherIMs
   */
  otherIMs: number;
  markPrice: number;
  // Quantity of open positions
  positionQty: number;
  // Quantity of long orders
  buyOrdersQty: number;
  // Quantity of short orders
  sellOrdersQty: number;

  IMR_Factor: number;

  takerFeeRate: number;
};

/**
 * @formulaId maxQty
 * @name Max Order QTY
 * @description
 * ## Max Long Quantity Formula
 *
 * ```
 * max long qty = MIN (
 *   base max,
 *   (((Total_collateral_value - Other_IMs) / (Max(1 / Symbol Leverage i, Base IMR i) + 2 * futures_take_fee_rate * 0.0001) / mark_price_i) * 0.995 - position_qty_this_symbol - sum_buy_order_qty_this_symbol),
 *   ((((Total_collateral_value - Other_IMs) / IMR Factor i)^(1/1.8)) / mark_price_i - position_qty_this_symbol - sum_buy_order_qty_this_symbol) / (1 + 2 * futures_take_fee_rate * 0.0001) * 0.995
 * )
 * ```
 *
 * ## Max Short Quantity Formula
 *
 * ```
 * max short qty = MIN (
 *   base max,
 *   (((Total_collateral_value - Other_IMs) / (Max(1 / Symbol Leverage i, Base IMR i) + 2 * futures_take_fee_rate * 0.0001) / mark_price_i) * 0.995 + position_qty_this_symbol - sum_sell_order_qty_this_symbol),
 *   ((((Total_collateral_value - Other_IMs) / IMR Factor i)^(1/1.8)) / mark_price_i + position_qty_this_symbol - sum_sell_order_qty_this_symbol) / (1 + 2 * futures_take_fee_rate * 0.0001) * 0.995
 * )
 * ```
 *
 * ## Reduce Only Mode
 *
 * When reduce only is enabled:
 * - If `position_qty_i > 0`: max long qty = 0, max short qty = abs(position_qty_i)
 * - If `position_qty_i < 0`: max long qty = abs(position_qty_i), max short qty = 0
 * - If `position_qty_i = 0`: max long qty = 0, max short qty = 0
 *
 * ## Variable Definitions
 *
 * | Variable | Description | API Reference |
 * |----------|-------------|---------------|
 * | `max long qty` | Maximum long quantity for current symbol | |
 * | `max short qty` | Maximum short quantity for current symbol | |
 * | `base_max` | Maximum quantity limit for opening a single position | `/v1/public/info.base_max` |
 * | `Total_collateral_value` | Total value of collateral assets in user account (USDC denominated) | |
 * | `Other_IMs` | Initial margin occupied by all other symbols excluding current symbol | |
 * | `IMR_i (with_orders)` | Initial margin rate for a single symbol (considering both position/orders notional) | |
 * | `Symbol Leverage i` | Leverage for symbol i under current margin mode | `position.leverage` or symbol+mode leverage source |
 * | `Base IMR i` | Base initial margin rate for a single symbol | `/v1/public/info` |
 * | `IMR Factor i` | IMR calculation factor for a single symbol | `v1/client/info` |
 * | `Position Notional i` | Sum of position notional for a single symbol | |
 * | `Order Notional i` | Sum of order notional for a single symbol | |
 * | `position_notional_with_orders_i` | Sum of position/orders notional for a single symbol | |
 * | `mark_price_i` | Mark price for a single symbol | |
 * | `position_qty_with_orders_i` | Sum of position/orders quantity for a single symbol | |
 * | `position_qty_i` | Position quantity for a single symbol | |
 * | `sum_position_qty_buy_orders_i` | Sum of long order quantity for a single symbol [algo orders ignored] | |
 * | `sum_position_qty_sell_orders_i` | Sum of short order quantity for a single symbol [algo orders ignored] | |
 * | `futures_take_fee_rate` | User's futures taker fee rate | `GET /v1/client/info` |
 *
 * ## Calculation Details
 *
 * ```
 * Other_IMs = sum(position_notional_with_orders_i * IMR_i (with_orders)) // excluding current symbol
 *
 * IMR_i (with_orders) = Max(1 / Symbol Leverage i, Base IMR i, IMR Factor i * Abs(Position Notional i + Order Notional i)^(4/5))
 *
 * position_notional_with_orders_i = abs(mark_price_i * position_qty_with_orders_i)
 *
 * position_qty_with_orders_i = max[abs(position_qty_i + sum_position_qty_buy_orders_i), abs(position_qty_i - sum_position_qty_sell_orders_i)]
 * ```
 *
 * ## Example Calculation
 *
 * **Given:**
 * - `futures_take_fee_rate = 8`
 * - `BTC base max = 20`
 * - `Total_collateral_value = 1981.66`
 * - `Other_IMs = ETH Initial Margin = 4915.23 * 0.1 = 491.523`
 * - `BTC mark_price_i = 25986.2`
 * - `BTC position_qty_this_symbol = 0.2`
 * - `sum_buy_order_qty_this_symbol = 0.3`
 * - `sum_sell_order_qty_this_symbol = -0.5`
 *
 * **Max Long Quantity:**
 * ```
 * max long qty = MIN(
 *   20 BTC,
 *   ((1981.66 - 491.523) / (0.1 + 2 * 8 * 0.0001) / 25986.2 * 0.995 - 0.2 - 0.3) = 0.0615815026 BTC,
 *   ((((1981.66 - 491.523) / 0.0000002512)^(1/1.8)) / 25986.2 - 0.2 - 0.3) / (1 + 2 * 8 * 0.0001) * 0.995 = 9.78216039 BTC
 * ) = 0.0615815026 BTC
 * ```
 *
 * **Max Short Quantity:**
 * ```
 * max short qty = MIN(
 *   20 BTC,
 *   ((1981.66 - 491.523) / (0.1 + 2 * 8 * 0.0001) / 25986.2 * 0.995 + 0.2 - 0.5) = 0.261581503 BTC,
 *   ((((1981.66 - 491.523) / 0.0000002512)^(1/1.8)) / 25986.2 + 0.2 - 0.5) / (1 + 2 * 8 * 0.0001) * 0.995 = 9.98084249726 BTC
 * ) = 0.261581503 BTC
 * ```
 *
 * ## Additional Examples
 *
 * **Base max qty calculation:**
 * ```
 * Base max qty = (1981.66 - 491.523) / (0.1 + 2 * 8 * 0.0001) / 25986.2 * 0.995 = 0.561581503 BTC
 * ```
 *
 * **Different position scenarios:**
 *
 * 1. **Short position -0.3 BTC:**
 *    - max long qty = 0.561581503 - (-0.3) = 0.861581503
 *    - max short qty = 0.561581503 + (-0.3) = 0.261581503
 *
 * 2. **Short position -0.3 BTC + sell orders 0.1:**
 *    - max long qty = 0.561581503 - (-0.3) = 0.861581503
 *    - max short qty = 0.561581503 + (-0.3) - 0.1 = 0.161581503
 *
 * 3. **Long position 0.3 BTC + buy orders 0.2 + sell orders 0.1:**
 *    - max long qty = 0.561581503 - 0.3 - 0.2 = 0.061581503
 *    - max short qty = 0.561581503 + 0.3 - 0.1 = 0.761581503
 *
 * ## Special Case: Insufficient Collateral
 *
 * When `totalCollatValue <= newTotalIM`:
 *
 * ```
 * newOrderSize_iter = ITERATE() return max(0, newOrderSize_iter * 99.5% + others)
 * ```
 *
 * **ITERATE() Algorithm:**
 * ```
 * ITERATE() {
 *     iteratorLeverage = min(1 / Symbol Leverage i, Base IMR i)
 *     iteratorStep = 2
 *
 *     // First iteration (30 times)
 *     for (i = 0; i < 30; i++) {
 *         iteratorLeverage = max(0, iteratorLeverage - iteratorStep)
 *         newOrderSize1 = (adjustedCollateral - othersIM) * iteratorLeverage / markPrice
 *         calculate afterTradeIM
 *         if (adjustedCollateral >= afterTradeIM) break
 *     }
 *
 *     leftLeverage = iteratorLeverage
 *     rightLeverage = min(symbolLeverage_i, leftLeverage + iteratorStep)
 *
 *     // Binary search (30 times)
 *     for (i = 0; i < 30; i++) {
 *         midLeverage = (leftLeverage + rightLeverage) / 2
 *         newOrderSize2 = (adjustedCollateral - othersIM) * midLeverage / markPrice
 *         calculate afterTradeIM
 *         precision = (adjustedCollateral - afterTradeIM) / adjustedCollateral
 *
 *         if (adjustedCollateral > afterTradeIM) {
 *             leftLeverage = midLeverage
 *             if (0 <= precision <= 0.5%) break
 *         } else {
 *             rightLeverage = midLeverage
 *         }
 *     }
 *
 *     return newOrderSize2
 * }
 * ```
 */
export function maxQty(
  side: OrderSide,
  inputs: MaxQtyInputs,
  options?: ResultOptions,
): number {
  if (side === OrderSide.BUY) {
    return maxQtyByLong(inputs);
  }
  return maxQtyByShort(inputs);
}

export function maxQtyByLong(
  inputs: Omit<MaxQtyInputs, "side">,
  options?: ResultOptions,
): number {
  try {
    const {
      baseMaxQty,
      totalCollateral,
      otherIMs,
      maxLeverage,
      baseIMR,
      markPrice,
      IMR_Factor,
      positionQty,
      buyOrdersQty,
      takerFeeRate,
    } = inputs;

    if (totalCollateral === 0) {
      return 0;
    }

    const totalCollateralDecimal = new Decimal(totalCollateral);

    const factor_1 = totalCollateralDecimal
      .sub(otherIMs)
      .div(
        new Decimal(takerFeeRate)
          .mul(2)
          .mul(0.0001)
          .add(Math.max(1 / maxLeverage, baseIMR)),
      )
      .div(markPrice)
      .mul(0.995)
      .sub(new Decimal(positionQty).add(buyOrdersQty))
      .toNumber();

    if (positionQty === 0 && buyOrdersQty === 0) {
      return Math.min(baseMaxQty, factor_1);
    }

    if (IMR_Factor === 0) {
      return Math.min(baseMaxQty, factor_1);
    }

    const factor_2 = totalCollateralDecimal
      .sub(otherIMs)
      .div(IMR_Factor)
      .toPower(1 / 1.8)
      .div(markPrice)
      .sub(
        new Decimal(positionQty).add(buyOrdersQty),
        // .abs()
        // .div(new Decimal(takerFeeRate).mul(2).mul(0.0001).add(1))
      )
      .div(new Decimal(takerFeeRate).mul(2).mul(0.0001).add(1))
      .mul(0.995)
      .toNumber();

    return Math.min(baseMaxQty, factor_1, factor_2);
  } catch (error) {
    return 0;
  }
}

export function maxQtyByShort(
  inputs: Omit<MaxQtyInputs, "side">,
  options?: ResultOptions,
): number {
  try {
    const {
      baseMaxQty,
      totalCollateral,
      otherIMs,
      maxLeverage,
      baseIMR,
      markPrice,
      IMR_Factor,
      positionQty,
      buyOrdersQty,
      sellOrdersQty,
      takerFeeRate,
    } = inputs;

    const totalCollateralDecimal = new Decimal(totalCollateral);

    const factor_1 = totalCollateralDecimal
      .sub(otherIMs)
      .div(
        new Decimal(takerFeeRate)
          .mul(2)
          .mul(0.0001)
          .add(Math.max(1 / maxLeverage, baseIMR)),
      )
      .div(markPrice)
      .mul(0.995)
      // .add(new Decimal(positionQty).add(sellOrdersQty))
      .add(positionQty)
      .sub(Math.abs(sellOrdersQty))
      .toNumber();

    if (positionQty === 0 && sellOrdersQty === 0) {
      return Math.min(baseMaxQty, factor_1);
    }

    if (IMR_Factor === 0) {
      return Math.min(baseMaxQty, factor_1);
    }

    const factor_2 = totalCollateralDecimal
      .sub(otherIMs)
      .div(IMR_Factor)
      .toPower(1 / 1.8)
      .div(markPrice)
      // .add(
      // new Decimal(positionQty)
      //   .add(sellOrdersQty)
      //   // .abs()
      //   )
      .add(positionQty)
      .sub(sellOrdersQty)
      .div(new Decimal(takerFeeRate).mul(2).mul(0.0001).add(1))
      .mul(0.995)
      .toNumber();

    return Math.min(baseMaxQty, factor_1, factor_2);
  } catch (error) {
    return 0;
  }
}
