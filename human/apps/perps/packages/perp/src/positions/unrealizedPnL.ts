import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

/** @deprecated Use inline type or the new input type instead */
export type UnrealPnLInputs = {
  markPrice: number;
  openPrice: number;
  qty: number;
};

/**
 * @formulaId unrealizedPnL
 * @description Calculates the unrealized profit or loss of a single position.
 * This formula applies to both cross margin and isolated margin positions.
 * @formula Unrealized PnL = position_qty * (mark_price - entry_price)
 * @param inputs The inputs for calculating the unrealized profit or loss.
 * @returns The unrealized profit or loss of the position (in USDC).
 */
export function unrealizedPnL(inputs: {
  /** symbol mark price */
  markPrice: number;
  /** symbol open price (entry price) */
  openPrice: number;
  /** symbol quantity (position quantity, positive for long, negative for short) */
  qty: number;
}): number {
  return new Decimal(inputs.qty)
    .mul(inputs.markPrice - inputs.openPrice)
    .toNumber();
}

/**
 * @formulaId unrealizedPnLROI
 * @name Position unrealized ROI
 * @formula Position unrealized ROI = Position unrealized PNL / ( IMR_i *  abs(position_qty_i * entry_price_i) ) * 100%, IMR_i = Max(1 / Symbol Leverage i, Base IMR i, IMR Factor i * Abs(Position Notional i)^(4/5))
 * @description
 * ## Term Definitions
 *
 * - **Position unrealized ROI**: Single symbol unrealized return on investment
 * - **Position unrealized PNL**: Single symbol unrealized profit and loss
 * - **IMR_i**: Single symbol initial margin rate
 * - **Symbol Leverage_i**: Current leverage for symbol i under current margin mode
 * - **Base IMR_i**: Single symbol base initial margin rate
 * - **IMR Factor_i**: Single symbol IMR calculation factor, from v1/client/info
 * - **Position Notional_i**: Single symbol position notional sum
 * - **position_qty_i**: Single symbol position quantity
 * - **entry_price_i**: Single symbol entry price (avg. open price)
 *
 * ## Example
 *
 * **Position unrealized ROI** = Position unrealized PNL / (IMR_i * abs(position_qty_i * entry_price_i)) * 100% = 216.69 / (0.1 * abs(-3 * 1710.64)) * 100% = 42.22%
 *
 * **ETH IMR_i** = Max(1/10, 0.1, 0.0000003754 * abs(-4915.23)^(4/5)) = Max(0.1, 0.1, 0.000337077174) = 0.1
 *
 * - Symbol Leverage_i = 10
 * - ETH Base IMR_i = 0.1
 * - ETH IMR Factor_i = 0.0000003754
 * - ETH position_qty_i = -3
 * - ETH entry_price_i = 1710.64
 * - ETH mark_price_i = 1638.41
 * - ETH Position Notional = -3 * 1638.41 = -4915.23
 * - ETH Position unrealized PNL = 216.69
 *
 * @param inputs The inputs for calculating the ROI.
 * @returns The ROI of the position's unrealized profit or loss.
 */
export function unrealizedPnLROI(inputs: {
  positionQty: number;
  openPrice: number;
  IMR: number;
  unrealizedPnL: number;
}): number {
  const { openPrice, IMR } = inputs;

  if (
    inputs.unrealizedPnL === 0 ||
    inputs.positionQty === 0 ||
    openPrice === 0 ||
    IMR === 0
  )
    return 0;

  return new Decimal(inputs.unrealizedPnL)
    .div(new Decimal(Math.abs(inputs.positionQty)).mul(openPrice).mul(IMR))
    .toNumber();
}

/**
 * @formulaId totalUnrealizedPnl
 * @name Total Unrealized PNL
 * @formula Total Unrealized PNL = sum ( unrealized_pnl_i ), unrealized_pnl_i = position_qty_i * ( mark_price_i - entry_price _i )
 * @description
 * ## Term Definitions
 *
 * - **Total Unrealized PNL**: Sum of all current unrealized profit and loss for user's positions
 * - **unrealized_pnl_i**: Current unrealized profit and loss for a single symbol
 * - **position_qty_i**: Single symbol position quantity
 * - **mark_price_i**: Single symbol mark price
 * - **entry_price_i**: Single symbol entry price (avg. open price)
 *
 * ## Example
 *
 * ```
 * BTC-PERP unrealized_pnl_i = 0.2 * (25986.2 - 26067) = -16.16
 * ETH-PERP unrealized_pnl_i = -3 * (1638.41 - 1710.64) = 216.69
 *
 * Total Unrealized PNL = -16.16 + 216.69 = 200.53
 * ```
 * @param positions The array of positions.
 * @returns The total unrealized profit or loss of all positions.
 */
export function totalUnrealizedPnL(positions: API.Position[]): number {
  return positions.reduce((acc, cur) => {
    return (
      acc +
      unrealizedPnL({
        qty: cur.position_qty,
        openPrice: cur.average_open_price,
        markPrice: cur.mark_price,
      })
    );
  }, 0);
}
