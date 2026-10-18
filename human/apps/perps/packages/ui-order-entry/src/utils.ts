import { i18n } from "@paxeer-network/i18n";
import {
  BBOOrderType,
  OrderLevel,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";

export enum BBOStatus {
  ON = "on",
  OFF = "off",
  DISABLED = "disabled",
}

/**
 * if provide order_type, check order_type and order_type_ext, otherswise only check order_type_ext
 */
export function isBBOOrder(options: {
  order_type?: OrderType;
  order_type_ext?: OrderType;
}) {
  const { order_type, order_type_ext } = options;

  const isBBO = [OrderType.ASK, OrderType.BID].includes(order_type_ext!);

  if (order_type) {
    return order_type === OrderType.LIMIT && isBBO;
  }

  return isBBO;
}

export function getOrderTypeByBBO(value: BBOOrderType, size: OrderSide) {
  if (
    [BBOOrderType.COUNTERPARTY1, BBOOrderType.COUNTERPARTY5].includes(value)
  ) {
    return size === OrderSide.BUY ? OrderType.ASK : OrderType.BID;
  }

  if ([BBOOrderType.QUEUE1, BBOOrderType.QUEUE5].includes(value)) {
    return size === OrderSide.BUY ? OrderType.BID : OrderType.ASK;
  }
}

export function getOrderLevelByBBO(value: BBOOrderType) {
  if ([BBOOrderType.COUNTERPARTY1, BBOOrderType.QUEUE1].includes(value)) {
    return OrderLevel.ONE;
  }

  if ([BBOOrderType.COUNTERPARTY5, BBOOrderType.QUEUE5].includes(value)) {
    return OrderLevel.FIVE;
  }
}

export function getScaledPlaceOrderMessage(result: any) {
  const rows = result?.data?.rows || [];

  if (rows.length > 0) {
    const totalCount = rows.length;
    const successCount = rows.filter((row: any) => row.success).length;

    // fully successful
    if (successCount === totalCount) {
      return i18n.t("orderEntry.scaledOrder.fullySuccessful", {
        total: totalCount,
      });
    }

    // all failed
    if (successCount === 0) {
      return i18n.t("orderEntry.scaledOrder.allFailed");
    }

    // partially successful
    return i18n.t("orderEntry.scaledOrder.partiallySuccessful", {
      successCount,
      total: totalCount,
    });
  }
}

export const safeNumber = (val: number | string) => {
  return Number.isNaN(Number(val)) ? 0 : Number(val);
};
