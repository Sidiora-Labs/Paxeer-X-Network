import { Decimal } from "@paxeer-network/utils";

export function availableBalance(inputs: {
  USDCHolding: number;
  unsettlementPnL: number;
}) {
  const { USDCHolding, unsettlementPnL } = inputs;

  return new Decimal(USDCHolding).add(unsettlementPnL).toNumber();
}

/**
 * @formulaId availableBalanceForIsolatedMargin
 * @name Available Balance for Isolated Margin
 * @formula availableBalanceForIsolatedMargin = max(0, min(USDC_balance, free_collateral - max(total_cross_unsettled_pnl, 0)))
 * @description
 *
 * ## Definition
 *
 * max(0, min(USDC_balance, free_collateral - max(total_cross_unsettled_pnl, 0))), where
 *
 * **USDC_balance** = User's USDC balance
 *
 * **free_collateral** = Available collateral in the user's account (for cross margin trading)
 *
 * **total_cross_unsettled_pnl**  = sum( unsettled_PNL_i ) across all cross margin positions
 */
export function availableBalanceForIsolatedMargin(inputs: {
  USDCHolding: number;
  totalCrossUnsettledPnL: number;
  freeCollateral: number;
}): number {
  return Math.max(
    0,
    Math.min(
      inputs.USDCHolding,
      new Decimal(inputs.freeCollateral)
        .sub(Math.max(inputs.totalCrossUnsettledPnL, 0))
        .toNumber(),
    ),
  );
}
