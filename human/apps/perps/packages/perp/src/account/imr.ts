import { Decimal } from "@paxeer-network/utils";
import { IMRFactorPower } from "../constants";

/**
 * @formulaId imr
 * @description
 * Initial margin rate for a symbol.
 * Max(1 / Symbol Leverage i, Base IMR i, IMR Factor i * Abs(Position Notional i + Order Notional i)^(4/5))
 */
export function IMR(inputs: {
  /**
   * effective symbol leverage (resolved by symbol + margin mode)
   */
  maxLeverage: number;
  baseIMR: number;
  IMR_Factor: number;
  positionNotional: number;
  ordersNotional: number;
  IMR_factor_power?: number;
}): number {
  const {
    maxLeverage,
    baseIMR,
    IMR_Factor,
    positionNotional,
    ordersNotional: orderNotional,
    IMR_factor_power = IMRFactorPower,
  } = inputs;
  return Math.max(
    1 / maxLeverage,
    baseIMR,
    new Decimal(IMR_Factor)
      .mul(
        new Decimal(positionNotional)
          .add(orderNotional)
          .abs()
          .toPower(IMR_factor_power),
      )
      .toNumber(),
  );
}
