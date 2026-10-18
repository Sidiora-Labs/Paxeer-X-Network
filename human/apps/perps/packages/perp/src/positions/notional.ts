import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

/**
 * Calculates the notional value of a single position.
 * @param qty The quantity of the position.
 * @param mark_price The price of the position.
 * @returns The notional value of the position.
 */
export function notional(qty: number, mark_price: number): number {
  return new Decimal(qty).mul(mark_price).abs().toNumber();
}

/**
 * @formulaId totalNotional
 * @name Total Notional
 * @formula Total Notional = sum ( abs(position_qty_i * mark_price_i) )
 * @description
 * ## Term Definitions
 *
 * - **Total Notional**: Sum of current position notional values
 * - **position_qty_i**: Single symbol position quantity
 * - **mark_price_i**: Single symbol mark price
 *
 * ## Example
 *
 * **Total Notional** = 10112.43
 *
 * **abs(BTC position notional)** = 5197.2
 *
 * **abs(ETH position notional)** = 4915.23
 *
 * @param positions The array of positions.
 * @returns The total notional value of all positions.
 */
export function totalNotional(positions: API.Position[]): number {
  return positions.reduce((acc, cur) => {
    return acc + notional(cur.position_qty, cur.mark_price);
  }, 0);
}
