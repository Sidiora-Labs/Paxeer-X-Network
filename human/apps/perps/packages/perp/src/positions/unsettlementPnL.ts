import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

/** @deprecated Use inline type or the new input type instead */
export type LiqPriceInputs = {
  markPrice: number;
  totalCollateral: number;
  positionQty: number;
  positions: Pick<API.PositionExt, "position_qty" | "mark_price" | "mmr">[];
  MMR: number;
};

/**
 * @formulaId unsettlementPnl
 * @name Position Unrealized PNL
 * @formula Position Unrealized PNL = position_qty_i * (mark_price_i - entry_price_i)
 * @description
 * ## Term Definitions
 *
 * - **Position Unrealized PNL**: Single symbol unrealized profit and loss
 * - **position_qty_i**: Single symbol position quantity
 * - **mark_price_i**: Single symbol mark price
 * - **entry_price_i**: Single symbol entry price (avg. open price)
 *
 * ## Example
 *
 * **ETH Position Unrealized PNL** = position_qty_i * (mark_price_i - entry_price_i) = -3 * (1638.41 - 1710.64) = 216.69
 *
 * - ETH position_qty_i = -3
 * - ETH mark_price_i = 1638.41
 * - ETH entry_price_i = 1710.64
 *
 * @param inputs The inputs for calculating the unrealized profit or loss.
 * @returns The unrealized profit or loss of each position.
 */
export function unsettlementPnL(inputs: {
  positionQty: number;
  markPrice: number;
  costPosition: number;
  sumUnitaryFunding: number;
  lastSumUnitaryFunding: number;
}): number {
  const {
    positionQty,
    markPrice,
    costPosition,
    sumUnitaryFunding,
    lastSumUnitaryFunding,
  } = inputs;

  const qty = new Decimal(positionQty);

  return qty
    .mul(markPrice)
    .sub(costPosition)
    .sub(qty.mul(new Decimal(sumUnitaryFunding).sub(lastSumUnitaryFunding)))
    .toNumber();
}

/**
 * @formulaId totalUnsettlementPnL
 * @name Total Unsettlement PNL
 * @formula Unsettlement PNL = position_qty_i * mark_price_i - cost_position_i - position_qty_i * (sum_unitary_funding_i - last_sum_unitary_funding_i)
 * @description
 * ## Term Definitions
 *
 * - **total unsettlement PNL**: Sum of user account's unsettled PNL
 * - **mark_price_i**: Single symbol mark price
 * - **position_qty_i**: Single symbol position quantity
 * - **cost_position_i**: Single symbol notional snapshot from last settlement, `/v1/position`
 * - **sum_unitary_funding_i**: Single symbol current cumulative unit funding fee, `/v1/public/funding_rate`
 * - **last_sum_unitary_funding_i**: Single symbol cumulative unit funding fee from last settlement, `/v1/position`
 *
 * ## Example
 *
 * **BTC-PERP Unsettlement PNL** = 0.2 * 25986.2 - 5197.2 - 0.2 * (-1585.92 + 1583.92) = 0.44
 *
 * **ETH-PERP Unsettlement PNL** = -3 * 1638.41 + 4902.45 + 3 * (-52.728 + 50.728) = -18.78
 *
 * **Total Unsettlement PNL** = 0.44 - 18.78 = -18.34
 *
 * @param positions The array of positions.
 * @returns The total unrealized profit or loss of all positions.
 */
export function totalUnsettlementPnL(
  positions: (API.Position & { sum_unitary_funding: number })[],
): number {
  if (!Array.isArray(positions) || positions.length === 0) {
    return 0;
  }

  return positions.reduce((acc, cur) => {
    return (
      acc +
      unsettlementPnL({
        positionQty: cur.position_qty,
        markPrice: cur.mark_price,
        costPosition: cur.cost_position,
        sumUnitaryFunding: cur.sum_unitary_funding,
        lastSumUnitaryFunding: cur.last_sum_unitary_funding,
      })
    );
  }, 0);
}
