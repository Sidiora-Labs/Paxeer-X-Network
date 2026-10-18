import { AlgoOrderRootType, API, OrderType } from "@paxeer-network/types";
import {
  getTPSLLeg,
  isActiveTPSLLeg,
  isTPSLTriggered,
} from "@paxeer-network/utils";

export type TPSLEditableOrderValues = {
  quantity: number | string;
  tp_trigger_price: number | string;
  tp_order_type: OrderType;
  tp_order_price: number | string;
  sl_trigger_price: number | string;
  sl_order_type: OrderType;
  sl_order_price: number | string;
};

const editableKeys = [
  "quantity",
  "tp_trigger_price",
  "tp_order_type",
  "tp_order_price",
  "sl_trigger_price",
  "sl_order_type",
  "sl_order_price",
] as const satisfies readonly (keyof TPSLEditableOrderValues)[];

type TPSLLeg = "tp" | "sl";

const getOrderType = (child?: {type?:unknown}|unknown): OrderType | undefined => {
  if (!child) return undefined;
  return (child as {type?:unknown}).type === OrderType.LIMIT ? OrderType.LIMIT : OrderType.MARKET;
};

/** Use the execution type configured on this server-owned child. */
export function getTPSLEditOrderType(
  order: API.AlgoOrder | undefined,
  leg: TPSLLeg,
): OrderType {
  if (!order) return OrderType.MARKET;
  return getOrderType(getTPSLLeg(order, leg)) ?? OrderType.MARKET;
}

/** Only an existing, explicitly inactive leg may change execution type. */
export function isTPSLOrderTypeLocked(
  order: API.AlgoOrder | undefined,
  leg: TPSLLeg,
): boolean {
  if (!order) return false;
  const child = getTPSLLeg(order, leg);
  return !child || child.is_activated !== false || isTPSLTriggered(child);
}

export function getTPSLEditableOrderValues(
  order: API.AlgoOrder,
): TPSLEditableOrderValues {
  const tpOrder = getTPSLLeg(order, "tp");
  const slOrder = getTPSLLeg(order, "sl");
  const tpOrderType = getTPSLEditOrderType(order, "tp");
  const slOrderType = getTPSLEditOrderType(order, "sl");

  return {
    quantity:
      order.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
        ? 0
        : (order.quantity ?? ""),
    tp_trigger_price: isActiveTPSLLeg(tpOrder)
      ? tpOrder!.trigger_price!.toString()
      : "",
    tp_order_type: tpOrderType,
    tp_order_price:
      tpOrderType === OrderType.LIMIT && isActiveTPSLLeg(tpOrder)
        ? (tpOrder?.price?.toString() ?? "")
        : "",
    sl_trigger_price: isActiveTPSLLeg(slOrder)
      ? slOrder!.trigger_price!.toString()
      : "",
    sl_order_type: slOrderType,
    sl_order_price:
      slOrderType === OrderType.LIMIT && isActiveTPSLLeg(slOrder)
        ? (slOrder?.price?.toString() ?? "")
        : "",
  };
}

export function getChangedTPSLEditableOrderValues(
  previous: TPSLEditableOrderValues,
  next: TPSLEditableOrderValues,
): Partial<TPSLEditableOrderValues> {
  return editableKeys.reduce<Partial<TPSLEditableOrderValues>>(
    (changes, key) => {
      if (!Object.is(previous[key], next[key])) {
        Object.assign(changes, { [key]: next[key] });
      }
      return changes;
    },
    {},
  );
}
