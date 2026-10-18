import { Decimal } from "@paxeer-network/utils";
import { IMRFactorPower } from "../constants";

/**
 * @formulaId MMR
 * @name Position Maintenance Margin Rate
 * @formula MMR_i = Max(Base MMR_i, (Base MMR_i / Base IMR_i) * IMR Factor_i * Abs(Position Notional_i)^(4/5))
 * @description
 * ## Term Definitions
 *
 * - **MMR_i**: Single symbol maintenance margin rate
 * - **Base MMR_i**: Single symbol base maintenance margin rate
 * - **Base IMR_i**: Single symbol base initial margin rate
 * - **IMR Factor_i**: Single symbol IMR calculation factor, from v1/client/info
 * - **Position Notional_i**: Single symbol position notional sum
 * - **position_qty_i**: Single symbol position quantity
 * - **mark_price_i**: Single symbol mark price
 *
 * ## Example
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
 * @param inputs The inputs for calculating the MMR.
 * @returns The MMR of the position.
 */
export function MMR(inputs: {
  baseMMR: number;
  baseIMR: number;
  IMRFactor: number;
  positionNotional: number;
  IMR_factor_power: number;
}): number {
  const {
    baseMMR,
    baseIMR,
    IMRFactor,
    positionNotional,
    IMR_factor_power = IMRFactorPower,
  } = inputs;
  return Math.max(
    baseMMR,
    new Decimal(baseMMR)
      .div(baseIMR)
      .mul(IMRFactor)
      .mul(Math.pow(Math.abs(positionNotional), IMR_factor_power))
      // .toPower(IMR_factor_power)
      .toNumber(),
  );
}
