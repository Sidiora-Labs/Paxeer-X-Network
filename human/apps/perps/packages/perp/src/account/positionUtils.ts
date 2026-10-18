import { API, OrderSide } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import {
  buyOrdersFilter_by_symbol,
  sellOrdersFilter_by_symbol,
} from "./ordersFilter";

/**
 * Get the quantity of a specified symbol from the list of positions.
 */
export function getQtyFromPositions(
  positions: API.Position[],
  symbol: string,
): number {
  if (!positions) {
    return 0;
  }
  const position = positions.find((item) => item.symbol === symbol);
  return position?.position_qty || 0;
}

/**
 * Get the quantity of long and short orders for a specified symbol from the list of orders.
 */
export function getQtyFromOrdersBySide(
  orders: API.Order[],
  symbol: string,
  side: OrderSide,
): number {
  const ordersBySide =
    side === OrderSide.SELL
      ? sellOrdersFilter_by_symbol(orders, symbol)
      : buyOrdersFilter_by_symbol(orders, symbol);
  return ordersBySide.reduce((acc, cur) => {
    return acc + cur.quantity;
  }, 0);
}

export function getPositonsAndOrdersNotionalBySymbol(inputs: {
  positions: API.Position[];
  orders: API.Order[];
  symbol: string;
  markPrice: number;
}): number {
  const { positions, orders, symbol, markPrice } = inputs;
  const positionQty = getQtyFromPositions(positions, symbol);
  const buyOrdersQty = getQtyFromOrdersBySide(orders, symbol, OrderSide.BUY);
  const sellOrdersQty = getQtyFromOrdersBySide(orders, symbol, OrderSide.SELL);

  const markPriceDecimal = new Decimal(markPrice);

  return markPriceDecimal
    .mul(positionQty)
    .add(markPriceDecimal.mul(new Decimal(buyOrdersQty).add(sellOrdersQty)))
    .abs()
    .toNumber();
}
