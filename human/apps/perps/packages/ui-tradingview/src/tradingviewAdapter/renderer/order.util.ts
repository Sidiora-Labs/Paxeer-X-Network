import { OrderInterface } from "../type";

export const CHART_QTY_DECIMAL = 4;

export const getOrderId = (order: OrderInterface | null | undefined) => {
  if (order === null || order === undefined) {
    return undefined;
  }
  return order.algo_order_id || order.order_id;
};
