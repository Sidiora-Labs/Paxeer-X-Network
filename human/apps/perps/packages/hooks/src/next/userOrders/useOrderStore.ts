import { OrderStatus } from "@paxeer-network/types";
import type { API } from "@paxeer-network/types";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";

type OrderStoreState = {
  // ALL: API.OrderExt[];
} & {
  [OrderStatus.NEW]: API.OrderExt[];
  // [OrderStatus.FILLED]: API.OrderExt[];
  // [OrderStatus.CANCELLED]: API.OrderExt[];
  // [OrderStatus.REJECTED]: API.OrderExt[];
};

type OrderActions = {
  setOrders: (orderState: OrderStatus.NEW, orders: API.OrderExt[]) => void;
};

const useOrderStore = create<OrderStoreState & { actions: OrderActions }>()(
  immer((set) => ({
    // ALL: [],
    [OrderStatus.NEW]: [],
    // [OrderStatus.FILLED]: [],
    // [OrderStatus.CANCELLED]: [],
    // [OrderStatus.REJECTED]: [],
    actions: {
      /**
       * Only update the new orders for now;
       */
      setOrders: (orderState: OrderStatus.NEW, orders: API.OrderExt[]) => {
        set((state) => {
          (state as any)[orderState] = orders;
        });
      },
    },
  }))
);

const useNewOrders = () => {
  return useOrderStore((state) => state[OrderStatus.NEW]);
};

export { useOrderStore };
