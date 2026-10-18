import { createContext, useContext } from "react";
import { API, OrderSide, OrderType } from "@paxeer-network/types";

export interface PositionsRowContextState {
  quantity: string;
  price: string;
  type: OrderType;
  side: OrderSide;
  position: API.PositionExt | API.PositionTPSLExt;
  updateQuantity: (value: string) => void;
  updatePriceChange: (value: string) => void;
  updateOrderType: (value: OrderType) => void;

  closeOrderData: any;

  onSubmit: () => Promise<any>;
  submitting: boolean;
  tpslOrder?: API.AlgoOrder;
  partialTPSLOrder?: API.AlgoOrder;
  quoteDp?: number;
  baseDp?: number;
  baseTick?: number;
  errors: any | undefined;
}

export const PositionsRowContext = createContext(
  {} as PositionsRowContextState,
);

export const usePositionsRowContext = () => {
  return useContext(PositionsRowContext);
};
