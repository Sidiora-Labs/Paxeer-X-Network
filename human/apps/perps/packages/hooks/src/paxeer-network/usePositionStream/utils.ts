import { MarginMode } from "@paxeer-network/types";
import { OrderType } from "@paxeer-network/types";
import {
  API,
  AlgoOrderEntity,
  AlgoOrderType,
  AlgoOrderRootType,
  OrderStatus,
} from "@paxeer-network/types";

export const findTPSLFromOrder = (
  order: API.AlgoOrder,
): {
  tp_trigger_price?: number;
  sl_trigger_price?: number;
} => {
  let tp_trigger_price;

  let sl_trigger_price;

  const tpOrder = order?.child_orders?.find(
    (order: any) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
  );
  const slOrder = order?.child_orders?.find(
    (order: any) => order.algo_type === AlgoOrderType.STOP_LOSS,
  );

  if (tpOrder) {
    tp_trigger_price = tpOrder.trigger_price;
    // take_profit_qty =  tpOrder.quantity ;
  }
  if (slOrder) {
    sl_trigger_price = slOrder.trigger_price;
    // stop_loss_qty = slOrder.quantity;
  }

  return {
    tp_trigger_price,
    sl_trigger_price,
  };
};

export const findTPSLOrderPriceFromOrder = (
  order: API.AlgoOrder,
): {
  tp_order_price: OrderType | number | undefined;
  sl_order_price: OrderType | number | undefined;
} => {
  let tp_order_price: OrderType | number | undefined;
  let sl_order_price: OrderType | number | undefined;

  const tpOrder = order?.child_orders?.find(
    (order: any) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
  );
  const slOrder = order?.child_orders?.find(
    (order: any) => order.algo_type === AlgoOrderType.STOP_LOSS,
  );
  if (tpOrder) {
    if (tpOrder.trigger_price) {
      if (tpOrder.type === OrderType.LIMIT) {
        tp_order_price = tpOrder.price;
      } else {
        tp_order_price = OrderType.MARKET;
      }
    }
  }
  if (slOrder) {
    if (slOrder.trigger_price) {
      if (slOrder.type === OrderType.LIMIT) {
        sl_order_price = slOrder.price;
      } else {
        sl_order_price = OrderType.MARKET;
      }
    }
  }
  return {
    tp_order_price,
    sl_order_price,
  };
};

export const findPositionTPSLFromOrders = (
  orders: API.AlgoOrder[],
  symbol: string,
  marginMode: MarginMode = MarginMode.CROSS,
): {
  fullPositionOrder?: API.AlgoOrder;
  partialPositionOrders?: API.AlgoOrder[];
} => {
  const fullPositionOrder = orders?.find((order) => {
    return (
      order.symbol === symbol &&
      order.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL &&
      (order.margin_mode ?? MarginMode.CROSS) === marginMode &&
      (order.root_algo_status === OrderStatus.NEW ||
        order.root_algo_status === OrderStatus.REPLACED ||
        order.root_algo_status === OrderStatus.PARTIAL_FILLED)
    );
  });
  const partialPositionOrders = orders
    ?.filter((order) => {
      return (
        order.symbol === symbol &&
        (order.margin_mode ?? MarginMode.CROSS) === marginMode &&
        order.algo_type === AlgoOrderRootType.TP_SL &&
        (order.root_algo_status === OrderStatus.NEW ||
          order.root_algo_status === OrderStatus.REPLACED ||
          order.root_algo_status === OrderStatus.PARTIAL_FILLED)
      );
    })
    .sort((a, b) => {
      return b.created_time - a.created_time;
    });

  return {
    fullPositionOrder,
    partialPositionOrders,
  };
};
