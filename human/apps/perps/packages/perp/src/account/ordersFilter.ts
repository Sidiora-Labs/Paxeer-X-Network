import { API, OrderSide } from "@paxeer-network/types";

export function buyOrdersFilter_by_symbol(
  orders: API.Order[],
  symbol: string,
): API.Order[] {
  return orders.filter(
    (item) => item.symbol === symbol && item.side === OrderSide.BUY,
  );
}

export function sellOrdersFilter_by_symbol(
  orders: API.Order[],
  symbol: string,
): API.Order[] {
  return orders.filter(
    (item) => item.symbol === symbol && item.side === OrderSide.SELL,
  );
}
