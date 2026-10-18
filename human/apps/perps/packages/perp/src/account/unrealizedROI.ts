import { Decimal } from "@paxeer-network/utils";

/**
 * @formulaId totalUnrealizedROI
 * @name Total Unrealized ROI
 * @formula Total Unrealized ROI = Total Unrealized PNL / ( Total Value - Total Unrealized PNL ) * 100%
 * @description
 *
 * ## Definition
 *
 * **Total Unrealized PNL** = Sum of unrealized profit and loss for all current positions of the user
 *
 * **Total Value** = User's total asset value (denominated in USDC), including assets that cannot be used as collateral
 *
 * ## Example
 *
 * ```
 * Total Unrealized ROI = 200.53 / ( 2982.66 - 200.53 ) * 100% = 7.21%
 * Total Unrealized PNL = 200.53
 * Total Value = 2982.66
 * ```
 */
export function totalUnrealizedROI(inputs: {
  totalUnrealizedPnL: number;
  totalValue: number;
}) {
  const { totalUnrealizedPnL, totalValue } = inputs;

  return new Decimal(totalUnrealizedPnL)
    .div(totalValue - totalUnrealizedPnL)
    .toNumber();
}
