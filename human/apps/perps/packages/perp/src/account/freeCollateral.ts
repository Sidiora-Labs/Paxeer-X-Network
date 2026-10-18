import { Decimal, zero } from "@paxeer-network/utils";

export type FreeCollateralInputs = {
  totalCollateral: Decimal;
  totalInitialMarginWithOrders: number;
};

/**
 * @formulaId freeCollateral
 * @name Free Collateral
 * @formula Free Collateral = Total_collateral_value - total_initial_margin_with_orders
 * Total_collateral_value = (usdc balance.holding + usdc balance.pending_short_qty - usdc balance.isolated_order_frozen) + SUM(non-usdc balance.holding * mark price * discount) + total_cross_unsettled_PNL
 * total_initial_margin_with_orders = sum ( cross_position_notional_with_orders_i * cross_IMR_i (with_orders))
 * IMR_i (with_orders) = Max(1 / Symbol Leverage i, Base IMR i, IMR Factor i * Abs(Position Notional i + Order Notional i)^(4/5))
 * position_notional_with_orders_i = abs( mark_price_i * position_qty_with_orders_i)
 * position_qty_with_orders_i = max[ abs(position_qty_i + sum_position_qty_buy_orders_i), abs(position_qty_i - sum_position_qty_sell_orders_i)]
 * @description
 *
 * ## Definition
 *
 * **Free collateral**: Total value of available margin in the user's account (denominated in USDC). For isolated margin mode, this only considers cross margin positions and orders.
 *
 * **Total_collateral_value**: Total value of collateral assets in the user's account (denominated in USDC). For isolated margin mode, includes cross margin unsettled PNL but excludes isolated order frozen amounts.
 * Use `totalCollateral` function with optional parameters (`usdcBalancePendingShortQty`, `usdcBalanceIsolatedOrderFrozen`, `totalCrossUnsettledPnL`) to calculate this value.
 *
 * **total_initial_margin_with_orders**: Total initial margin used by cross margin positions and orders (isolated margin positions are excluded).
 * Use `totalInitialMarginWithQty` function with `orders` parameter to calculate this value. The function automatically filters to cross margin only.
 *
 * **usdc balance.pending_short_qty**: USDC balance frozen for pending short orders
 *
 * **usdc balance.isolated_order_frozen**: USDC balance frozen for isolated margin orders
 *
 * **total_cross_unsettled_PNL**: Total unsettled PNL from cross margin positions only
 *
 * **initial_margin_i with order**: Initial margin for symbol i (considering both positions and orders)
 *
 * **IMR_i (with_orders)**: Initial margin rate for a single symbol (considering both position and order notional)
 *
 * **Symbol Leverage i**: Leverage for symbol i under current margin mode (cross/isolated). Use `position.leverage` when position exists; otherwise resolve by symbol + mode leverage source.
 *
 * **Base IMR i**: Base initial margin rate for a single symbol, from `/v1/public/info`
 *
 * **IMR Factor i**: IMR calculation factor for a single symbol, from `v1/client/info`
 *
 * **Position Notional i**: Sum of position notional for a single symbol
 *
 * **Order Notional i**: Sum of order notional for a single symbol
 *
 * **position_notional_with_orders_i**: Sum of position and order notional for a single symbol
 *
 * **mark_price_i**: Mark price for a single symbol
 *
 * **position_qty_with_orders_i**: Sum of position and order quantity for a single symbol
 *
 * **position_qty_i**: Position quantity for a single symbol
 *
 * **sum_position_qty_buy_orders_i**: Sum of long order quantity for a single symbol [algo orders should be ignored]
 *
 * **sum_position_qty_sell_orders_i**: Sum of short order quantity for a single symbol [algo orders should be ignored]
 *
 * ## Example
 *
 * ```
 * Total_collateral_value = (2000 + 100 - 200) + 1 * 2000 * 0.9 + 50 = 2050
 * total_initial_margin_with_orders = 1500
 * Free Collateral = 2050 - 1500 = 550
 * ```
 */
export function freeCollateral(inputs: {
  /**
   * @description Total collateral value
   */
  totalCollateral: Decimal;
  /**
   * @description Total initial margin with orders (for cross margin positions only)
   */
  totalInitialMarginWithOrders: number;
}): Decimal {
  const value = inputs.totalCollateral.sub(inputs.totalInitialMarginWithOrders);
  // free collateral cannot be less than 0
  return value.isNegative() ? zero : value;
}
