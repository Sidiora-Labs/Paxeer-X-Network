import { Decimal } from "@paxeer-network/utils";

/**
 * @formulaId maxPositionNotional
 * @description calculate the max position notional
 * @formula max_notional = ( (1/ (leverage * imr_factor) ) ^ (1/0.8)
 */
export function maxPositionNotional(inputs: {
  /** symbol leverage */
  leverage: number;
  IMRFactor: number;
}) {
  const { leverage, IMRFactor } = inputs;
  return new Decimal(1)
    .div(new Decimal(leverage).mul(IMRFactor))
    .pow(1 / 0.8)
    .toNumber();
}

/**
 * symbol_leverage_max = 1 / ( imr_factor * notional ^ 0.8 )
 */
export function maxPositionLeverage(inputs: {
  IMRFactor: number;
  notional: number;
}) {
  const { IMRFactor, notional } = inputs;
  return new Decimal(1)
    .div(new Decimal(IMRFactor).mul(new Decimal(notional).pow(0.8)))
    .toNumber();
}
