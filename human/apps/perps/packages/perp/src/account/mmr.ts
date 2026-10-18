import { Decimal } from "@paxeer-network/utils";

/**
 * @formulaId mmr
 * @name Total Maintenance Margin Ratio
 * @formula Total Maintenance Margin Ratio = sum(Position maintenance margin) / total_position_notional * 100%, total_position_notional = sum(abs(position_qty_i * mark_price_i))
 * @description
 *
 * ## Definition
 *
 * **Total Maintenance Margin Ratio** = User's account maintenance margin ratio
 *
 * **sum(Position maintenance margin)** = Total maintenance margin of all user positions (denominated in USDC)
 *
 * **total_position_notional** = Sum of notional value of current positions
 *
 * **position_qty_i** = Position quantity for a single symbol
 *
 * **mark_price_i** = Mark price for a single symbol
 *
 * ## Example
 *
 * ```
 * Total Margin Ratio = 505.61 / 10112.43 * 100% = 4.99988628%
 * total_position_notional = 10112.43
 * abs(BTC position notional) = 5197.2
 * abs(ETH position notional) = 4915.23
 * sum(Position maintenance margin) = 505.61
 * BTC position MM = 259.86
 * ETH position MM = 245.75
 * ```
 *
 * @param inputs AccountMMRInputs
 * @returns number|null
 */
export function MMR(inputs: {
  // Total Maintenance Margin of all positions of the user (USDC)
  positionsMMR: number;
  /**
   * Notional sum of all positions,
   * positions.totalNotional()
   */
  positionsNotional: number;
}): number | null {
  // If the user does not have any positions, return null
  if (inputs.positionsNotional === 0) {
    return null;
  }
  if (inputs.positionsMMR === 0) {
    return null;
  }
  return new Decimal(inputs.positionsMMR)
    .div(inputs.positionsNotional)
    .toNumber();
}
