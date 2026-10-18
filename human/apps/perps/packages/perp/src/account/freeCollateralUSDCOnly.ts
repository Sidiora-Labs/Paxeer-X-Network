import { Decimal, zero } from "@paxeer-network/utils";
import { positiveCollateralContribution } from "./collateral";
import type { NonUSDCHolding } from "./totalCollateral";

export type FreeCollateralUSDCOnlyInputs = {
  /**
   * Free collateral (total_collateral_value - total_initial_margin_with_orders).
   * @see freeCollateral
   */
  freeCollateral: Decimal;
  /**
   * Non-USDC token holdings; same structure as in totalCollateral.
   * Each positive eligible item contributes (capped effective holding × index price × collateral ratio) to the sum to subtract.
   */
  nonUSDCHolding: NonUSDCHolding[];
};

/**
 * @formulaId freeCollateralUSDCOnly
 * @name Free Collateral (USDC Only)
 * @formula Free Collateral USDC Only = max(0, free_collateral - SUM(non_usdc_token.holding × mark_price × discount))
 * @description
 *
 * ## Definition
 *
 * **Free Collateral (USDC Only)**: Legacy metric representing the part of free collateral backed only by USDC (and unsettled PNL). Isolated-margin limits use total free collateral instead.
 *
 * **free_collateral**: From freeCollateral (total_collateral_value - total_initial_margin_with_orders).
 *
 * **non_usdc_token.holding × mark_price × discount**: Same as in totalCollateral — value of each non-USDC asset (capped by collateralCap), using indexPrice as mark price and collateralRatio as discount.
 *
 * ## Example
 *
 * ```
 * free_collateral = 550
 * SUM(non_usdc.holding × mark_price × discount) = 1 * 2000 * 0.9 = 1800
 * Free Collateral USDC Only = max(0, 550 - 1800) = 0
 * ```
 */
export function freeCollateralUSDCOnly(
  inputs: FreeCollateralUSDCOnlyInputs,
): Decimal {
  const { freeCollateral, nonUSDCHolding } = inputs;

  const nonUSDCHoldingValue = nonUSDCHolding.reduce<Decimal>((acc, cur) => {
    const effectiveHolding = new Decimal(cur.holding)
      .add(cur.pendingShort ?? 0)
      .toNumber();

    if (cur.isCollateral === false) {
      return acc;
    }

    return acc.add(
      positiveCollateralContribution({
        collateralQty: effectiveHolding,
        collateralCap: cur.collateralCap,
        collateralRatio: cur.collateralRatio,
        indexPrice: cur.indexPrice,
      }),
    );
  }, zero);

  const value = freeCollateral.sub(nonUSDCHoldingValue);
  return value.isNegative() ? zero : value;
}
