import { Decimal } from "@paxeer-network/utils";

/**
 * @formulaId maintenanceMargin
 * @name Position maintenance margin
 * @formula Position maintenance margin = abs (position_qty_i  * mark_price_i * MMR_i )
 * @description
 * ## Term Definitions
 *
 * - **Position maintenance margin**: Single symbol maintenance margin
 * - **MMR_i**: Single symbol maintenance margin rate
 * - **Base MMR_i**: Single symbol base maintenance margin rate
 * - **Base IMR_i**: Single symbol base initial margin rate
 * - **IMR Factor_i**: Single symbol IMR calculation factor, from v1/client/info
 * - **Position Notional_i**: Single symbol position notional sum
 * - **position_qty_i**: Single symbol position quantity
 * - **mark_price_i**: Single symbol mark price
 *
 * ## MMR Formula
 *
 * MMR_i = Max(Base MMR_i, (Base MMR_i / Base IMR_i) * IMR Factor_i * Abs(Position Notional_i)^(4/5))
 *
 * ## Example
 *
 * **BTC Position maintenance margin** = abs(position_qty_i * mark_price_i * MMR_i) = abs(0.2 * 25986.2 * 0.05) = 259.86
 *
 * **BTC MMR_i** = Max(0.05, (0.05 / 0.1) * 0.0000002512 * 5197.2^(4/5)) = Max(0.05, 0.000117924809) = 0.05
 *
 * - BTC Base MMR_i = 0.05
 * - BTC Base IMR_i = 0.1
 * - BTC IMR Factor_i = 0.0000002512
 * - Abs(BTC Position Notional_i) = 5197.2
 * - position_qty_i = 0.2
 * - mark_price_i = 25986.2
 *
 * @param inputs The inputs for calculating the maintenance margin.
 * @returns The maintenance margin of the position.
 */
export function maintenanceMargin(inputs: {
  positionQty: number;
  markPrice: number;
  MMR: number;
}) {
  const { positionQty, markPrice, MMR } = inputs;

  return new Decimal(positionQty).mul(markPrice).mul(MMR).abs().toNumber();
}
