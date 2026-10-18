import { Decimal, zero } from "@paxeer-network/utils";

export type TotalValueInputs = {
  totalUnsettlementPnL: number;
  USDCHolding: number;
  nonUSDCHolding: {
    holding: number;
    indexPrice: number;
  }[];
};

/**
 * @formulaId totalValue
 * @name Total Value
 * @formula Total Value = total_holding + total_isolated_position_margin + total_unsettled_PNL, total_holding = usdc balance.holding + SUM(non-usdc balance.holding * mark price)
 * @description
 *
 * ## Definition
 *
 * **Total Value** = User's total asset value (denominated in USDC), including assets that cannot be used as collateral
 *
 * **Total holding** = Sum of all holding quantities in the user's account
 *
 * **usdc balance.holding** = USDC holding quantity
 *
 * **non-usdc balance.holding * mark price** = Value of non-USDC asset holdings (denominated in USDC)
 *
 * **total_isolated_position_margin** = Sum of all isolated margin position margins
 *
 * **holding**: Asset quantity held by the user, from `/v1/client/holding` or v2 Websocket API | Balance
 *
 * **mark price**: Current price of the asset, from v2 Websocket API | Balance
 *
 * **total unsettlement PNL** = Sum of user's account unsettled PNL (including both cross and isolated margin positions)
 *
 * ## Example
 *
 * ```
 * total_holding = 2000 + 1000 * 1.001 = 3001
 * total_isolated_position_margin = 500
 * total_unsettled_PNL = -18.34
 * Total Value = 3001 + 500 - 18.34 = 3482.66
 * ```
 */
export function totalValue(inputs: {
  /**
   * @description Total unsettled PNL of user account (including both cross and isolated margin positions)
   */
  totalUnsettlementPnL: number;
  /**
   * @description USDC holding quantity
   */
  USDCHolding: number;
  /**
   * @description Non-USDC holdings with their index prices
   */
  nonUSDCHolding: {
    holding: number;
    indexPrice: number;
  }[];
  /**
   * @description Total isolated position margin (sum of all isolated margin positions). Pass 0 if no isolated margin positions exist.
   * @default 0
   */
  totalIsolatedPositionMargin?: number;
}): Decimal {
  const {
    totalUnsettlementPnL,
    USDCHolding,
    nonUSDCHolding,
    totalIsolatedPositionMargin = 0,
  } = inputs;
  const nonUSDCHoldingValue = nonUSDCHolding.reduce((acc, cur) => {
    return new Decimal(cur.holding).mul(cur.indexPrice).add(acc);
  }, zero);
  return nonUSDCHoldingValue
    .add(USDCHolding)
    .add(totalIsolatedPositionMargin)
    .add(totalUnsettlementPnL);
}
