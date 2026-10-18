import { Decimal } from "@paxeer-network/utils";

/**
 * Sum of notional value for a symbol's position and orders.
 */
export function positionNotionalWithOrder_by_symbol(inputs: {
  markPrice: number;
  positionQtyWithOrders: number;
}): Decimal {
  return new Decimal(inputs.markPrice).mul(inputs.positionQtyWithOrders);
}

/**
 *  Sum of position quantity and orders quantity for a symbol.
 */
export function positionQtyWithOrders_by_symbol(inputs: {
  positionQty: number;
  // Total quantity of buy orders for a symbol
  buyOrdersQty: number;
  // Total quantity of sell orders for a symbol
  sellOrdersQty: number;
}): number {
  const { positionQty, buyOrdersQty, sellOrdersQty } = inputs;
  const positionQtyDecimal = new Decimal(positionQty);
  const qty = Math.max(
    positionQtyDecimal.add(buyOrdersQty).abs().toNumber(),
    positionQtyDecimal.sub(sellOrdersQty).abs().toNumber(),
  );

  return qty;
}
