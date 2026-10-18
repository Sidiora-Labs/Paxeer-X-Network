import { availableBalanceForIsolatedMargin } from "./availableBalance";

/**
 * @formulaId maxAdd
 * @name Maximum Margin Addition for Isolated Position
 * @formula max_add = max(0, min(USDC_balance, free_collateral - max(total_cross_unsettled_pnl, 0)))
 * @description
 *
 * ## Definition
 *
 * **max_add**: Maximum amount of margin that can be added to an isolated margin position
 *
 * **USDC_balance**: User's USDC balance
 *
 * **free_collateral**: Available collateral in the user's account (for cross margin trading)
 *
 * **total_cross_unsettled_pnl**: Total unsettled PNL from cross margin positions only
 *
 * ## Business Rules
 *
 * - Maximum add amount cannot exceed available USDC balance
 * - Maximum add amount cannot exceed free collateral minus cross margin unrealized profit
 * - Cross margin unrealized profit reduces available funds for adding isolated margin
 *
 * ## Example
 *
 * ```
 * USDC_balance = 500
 * free_collateral = 300
 * total_cross_unsettled_pnl = 100 (profit)
 * max_add = max(0, min(500, 300 - max(100, 0))) = max(0, min(500, 200)) = 200
 * ```
 *
 * @param inputs Input parameters for calculating maximum margin addition
 * @returns Maximum margin that can be added (in USDC)
 */
export function maxAdd(inputs: {
  /**
   * @description USDC balance
   */
  USDCHolding: number;
  /**
   * @description Free collateral (available for cross margin trading)
   */
  freeCollateral: number;
  /**
   * @description Total cross margin unsettled PNL
   */
  totalCrossUnsettledPnL: number;
}): number {
  return availableBalanceForIsolatedMargin(inputs);
}

/**
 * @formulaId maxReduce
 * @name Maximum Margin Reduction for Isolated Position
 * @formula max_reduce = max(0, isolated_position_margin - position_notional * imr + min(0, position_unsettled_pnl))
 * @description
 *
 * ## Definition
 *
 * **max_reduce**: Maximum amount of margin that can be reduced from an isolated margin position
 *
 * **isolated_position_margin**: Current margin allocated to the isolated position
 *
 * **position_notional**: Notional value of the isolated position
 *
 * **imr**: Initial margin rate for the isolated position
 *
 * **position_unsettled_pnl**: Unrealized PNL of the isolated position
 *
 * ## Business Rules
 *
 * - Maximum reduce amount cannot exceed current isolated position margin
 * - Unrealized losses increase the maximum reducible amount
 * - Position notional and IMR determine the minimum required margin
 *
 * ## Example
 *
 * ```
 * isolated_position_margin = 1000
 * position_notional = 5000
 * imr = 0.02
 * position_unsettled_pnl = -100 (loss)
 * max_reduce = max(0, 1000 - 5000 * 0.02 + min(0, -100)) = max(0, 1000 - 100 - 100) = 800
 * ```
 *
 * @param inputs Input parameters for calculating maximum margin reduction
 * @returns Maximum margin that can be reduced (in USDC)
 */
export function maxReduce(inputs: {
  /**
   * @description Current margin allocated to the isolated position
   */
  isolatedPositionMargin: number;
  /**
   * @description Notional value of the isolated position
   */
  positionNotional: number;
  /**
   * @description Initial margin rate for the isolated position
   */
  imr: number;
  /**
   * @description Unrealized PNL of the isolated position
   */
  positionUnsettledPnL: number;
}): number {
  const {
    isolatedPositionMargin,
    positionNotional,
    imr,
    positionUnsettledPnL,
  } = inputs;

  const minRequiredMargin = positionNotional * imr;
  const pnlAdjustment = Math.min(0, positionUnsettledPnL);

  return Math.max(
    0,
    isolatedPositionMargin - minRequiredMargin + pnlAdjustment,
  );
}
