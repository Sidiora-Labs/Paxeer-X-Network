import { createContext, useContext } from "react";
import { API, OrderEntity } from "@paxeer-network/types";

export interface OrderListContextState {
  onCancelOrder: (order: API.Order | API.AlgoOrder) => Promise<any>;
  editOrder: (id: string, order: OrderEntity) => Promise<any>;
  editAlgoOrder: (id: string, order: OrderEntity) => Promise<any>;
  checkMinNotional: (
    symbol: string,
    price?: string | number,
    qty?: string | number,
  ) => string | undefined;
}

export const OrderListContext = createContext<OrderListContextState>(
  {} as OrderListContextState,
);

export const useOrderListContext = () => {
  return useContext(OrderListContext);
};

export interface OrderListProviderProps {
  cancelOrder: (orderId: number, symbol: string) => Promise<any>;
  editOrder: (orderId: string, order: OrderEntity) => Promise<any>;
  cancelAlgoOrder: (orderId: number, symbol: string) => Promise<any>;
  editAlgoOrder: (orderId: string, order: OrderEntity) => Promise<any>;
}
