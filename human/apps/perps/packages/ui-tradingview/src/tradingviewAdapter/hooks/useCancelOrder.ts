import { useCallback } from "react";
import { useOrderStream } from "@paxeer-network/hooks";
import { OrderStatus } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { BracketAlgoType, TpslAlgoType } from "../renderer/tpsl.util";

export default function useCancelOrder() {
  const [
    pendingOrders,
    { cancelOrder, cancelAlgoOrder, cancelTPSLChildOrder, updateTPSLOrder },
  ] = useOrderStream({
    status: OrderStatus.INCOMPLETE,
  });
  return useCallback(
    (order: any) => {
      if (order.algo_order_id) {
        if (TpslAlgoType.includes(order.root_algo_order_algo_type)) {
          if (
            order.parent_algo_order_id &&
            order.parent_algo_order_id !== order.root_algo_order_id
          ) {
            return updateTPSLOrder(order.root_algo_order_id, [
              {
                order_id: order.parent_algo_order_id,
                child_orders: [
                  { order_id: order.algo_order_id, is_activated: false },
                ],
              },
            ] as any);
          }
          // check if tpsl active
          const rootOrder = pendingOrders?.find(
            (item) => item.algo_order_id === order.root_algo_order_id,
          );
          if (!rootOrder)
            return Promise.reject(
              new Error("TP/SL order is no longer available"),
            );
          const isEditActivated = rootOrder.child_orders.every(
            (item: any) => !!item.trigger_price,
          );
          if (isEditActivated) {
            return cancelTPSLChildOrder(
              order.algo_order_id,
              order.root_algo_order_id,
            );
          }
          return cancelAlgoOrder(rootOrder.algo_order_id, order.symbol).then();
        }
        return cancelAlgoOrder(order.algo_order_id, order.symbol).then();
      }
      return cancelOrder(order.order_id, order.symbol).then();
    },
    [
      cancelOrder,
      cancelAlgoOrder,
      cancelTPSLChildOrder,
      updateTPSLOrder,
      pendingOrders,
    ],
  );
}
