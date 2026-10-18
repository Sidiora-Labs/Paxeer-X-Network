import { utils } from "@paxeer-network/hooks";
import { i18n } from "@paxeer-network/i18n";
import { MarginMode, OrderSide } from "@paxeer-network/types";
import {
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  OrderStatus,
  OrderType,
} from "@paxeer-network/types";
import {
  isPositionalTPSL,
  getTPSLLeg,
  getTPSLQuantity,
  isEntirePositionTPSL,
} from "@paxeer-network/utils";
import { Decimal } from "@paxeer-network/utils";

export const upperCaseFirstLetter = (str: string) => {
  if (str === undefined) return str;
  if (str.length === 0) return str;
  if (str.length === 1) return str.charAt(0).toUpperCase();
  return str.charAt(0).toUpperCase() + str.toLowerCase().slice(1);
};

export const normalizeMarginMode = (
  value?: MarginMode | 1 | 0 | null,
): MarginMode | undefined => {
  if (value === 1 || value === MarginMode.ISOLATED) {
    return MarginMode.ISOLATED;
  }

  if (value === 0 || value === MarginMode.CROSS) {
    return MarginMode.CROSS;
  }

  return undefined;
};

const getMarginModeBadges = (record: any): string[] => {
  const marginMode = normalizeMarginMode(record?.margin_mode);

  if (marginMode === MarginMode.ISOLATED) {
    return [i18n.t("marginMode.isolated")];
  }

  if (marginMode === MarginMode.CROSS) {
    return [i18n.t("marginMode.cross")];
  }

  return [];
};

/**
 * order_type: LIMIT、MARKET、CLOSE_POSITION
 * algo_type: STOP、TPSL、positional_TPSL、BRACKET
 */
const getOrderTypeBadges = (record: any): string[] => {
  const orderType = record.type;
  const algoType = record.algo_type;

  if (typeof orderType !== "undefined") {
    const list: string[] = [];

    if (!!record.parent_algo_type) {
      if (algoType === AlgoOrderType.STOP_LOSS) {
        const types = isPositionalTPSL(record)
          ? [i18n.t("common.position"), i18n.t("tpsl.sl")]
          : [i18n.t("tpsl.sl")];
        list.push(...types);
      }

      if (algoType === AlgoOrderType.TAKE_PROFIT) {
        const types = isPositionalTPSL(record)
          ? [i18n.t("common.position"), i18n.t("tpsl.tp")]
          : [i18n.t("tpsl.tp")];
        list.push(...types);
      }

      return list;
    }

    const type =
      typeof orderType === "string" ? orderType.replace("_ORDER", "") : "";

    // bbo(ask, bid) order as a limit type
    if ([OrderType.ASK, OrderType.BID].includes(orderType)) {
      return [i18n.t("orderEntry.orderType.limit")];
    }

    if (
      record.algo_order_id === undefined ||
      (record.algo_order_id && algoType === "BRACKET")
    ) {
      const typeMap = {
        [OrderType.LIMIT]: i18n.t("orderEntry.orderType.limit"),
        [OrderType.MARKET]: i18n.t("orderEntry.orderType.market"),
        [OrderType.POST_ONLY]: i18n.t("orderEntry.orderType.postOnly"),
        [OrderType.IOC]: i18n.t("orderEntry.orderType.ioc"),
        [OrderType.FOK]: i18n.t("orderEntry.orderType.fok"),
      };

      return [
        typeMap[type as keyof typeof typeMap] || upperCaseFirstLetter(type),
      ];
    }

    if (algoType === AlgoOrderRootType.TRAILING_STOP) {
      return [i18n.t("orderEntry.orderType.trailingStop")];
    }

    // stop limit, stop market
    if (type) {
      const typeMap = {
        [OrderType.LIMIT]: i18n.t("orderEntry.orderType.stopLimit"),
        [OrderType.MARKET]: i18n.t("orderEntry.orderType.stopMarket"),
      };
      return [typeMap[type as keyof typeof typeMap] || type];
    }
  }

  if (typeof algoType !== "undefined") {
    const list: string[] = [];

    if (algoType === AlgoOrderRootType.POSITIONAL_TP_SL) {
      list.push(i18n.t("common.position"));
    }

    const tpOrder = record?.child_orders?.find(
      (order: any) =>
        order.algo_type === AlgoOrderType.TAKE_PROFIT && !!order.trigger_price,
    );

    const slOrder = record?.child_orders?.find(
      (order: any) =>
        order.algo_type === AlgoOrderType.STOP_LOSS && !!order.trigger_price,
    );

    if (tpOrder || slOrder) {
      list.push(
        tpOrder && slOrder
          ? i18n.t("common.tpsl")
          : tpOrder
            ? i18n.t("tpsl.tp")
            : i18n.t("tpsl.sl"),
      );
    }

    return list;
  }

  return [];
};

export function parseBadgesFor(record: any): undefined | string[] {
  const orderTypeBadges = getOrderTypeBadges(record);
  const marginModeBadges = getMarginModeBadges(record);
  const badges = [...orderTypeBadges, ...marginModeBadges];

  return badges.length ? badges : undefined;
}

export function grayCell(record: any): boolean {
  return (
    (record as API.Order).status === OrderStatus.CANCELLED ||
    (record as API.AlgoOrder).algo_status === OrderStatus.CANCELLED
  );
}

export function getOrderStatus(record: any) {
  return (record as API.Order).status || (record as API.AlgoOrder).algo_status;
}

export function isGrayCell(status?: string): boolean {
  return status === OrderStatus.CANCELLED;
}

function findBracketTPSLOrder(order: API.AlgoOrderExt) {
  if (order.algo_type !== AlgoOrderRootType.BRACKET) {
    return {
      tpOrder: undefined,
      slOrder: undefined,
    };
  }

  const innerOrder = order.child_orders?.[0];
  if (!innerOrder)
    return {
      tpOrder: undefined,
      slOrder: undefined,
    };

  const tpOrder = innerOrder?.child_orders?.find(
    (item) => item.algo_type === AlgoOrderType.TAKE_PROFIT,
  );

  const slOrder = innerOrder?.child_orders?.find(
    (item) => item.algo_type === AlgoOrderType.STOP_LOSS,
  );

  return {
    tpOrder,
    slOrder,
  };
}

export function calcBracketRoiAndPnL(order: API.AlgoOrderExt) {
  const defaultCallback = {
    pnl: {
      tpPnL: undefined,
      slPnL: undefined,
    },
    // roi: {
    //   tpRoi: undefined,
    //   slRoi: undefined,
    // },
  };
  const { tpOrder, slOrder } = findBracketTPSLOrder(order);
  if (!tpOrder && !slOrder) return defaultCallback;

  if (typeof order.price === undefined || !order.price) return defaultCallback;

  const quantity =
    order.side === OrderSide.BUY ? order.quantity : order.quantity * -1;

  const tpPnL =
    tpOrder?.trigger_price &&
    utils.priceToPnl({
      qty: quantity,
      price: tpOrder?.trigger_price,
      entryPrice: order.price,
      // @ts-ignore
      orderSide: order.side,
      // @ts-ignore
      orderType: tpOrder.algo_type,
    });
  const slPnL =
    slOrder?.trigger_price &&
    utils.priceToPnl({
      qty: quantity,
      // trigger price
      price: slOrder?.trigger_price,
      //
      entryPrice: order.price,
      // @ts-ignore
      orderSide: order.side,
      // @ts-ignore
      orderType: slOrder.algo_type,
    });

  // UI not show ROI, so we don't need to calculate ROI
  // const tpRoi = tpPnL
  //   ? utils.calcTPSL_ROI({
  //       pnl: tpPnL,
  //       qty: order.quantity,
  //       price: order.price,
  //     })
  //   : undefined;
  // const slRoi = slPnL
  //   ? utils.calcTPSL_ROI({
  //       pnl: slPnL,
  //       qty: order.quantity,
  //       price: order.price,
  //     })
  //   : undefined;

  return {
    pnl: {
      tpPnL,
      slPnL,
    },
    // roi: {
    //   tpRoi,
    //   slRoi,
    // },
  };
}

export function areDatesEqual(date1?: Date, date2?: Date): boolean {
  if (!date1 || !date2) return false;
  return (
    date1.getFullYear() === date2.getFullYear() &&
    date1.getMonth() === date2.getMonth() &&
    date1.getDate() === date2.getDate()
  );
}

export function isTrailingStopOrder(order: API.AlgoOrderExt) {
  return order.algo_type === OrderType.TRAILING_STOP;
}

export function getNotional(order: API.AlgoOrderExt, dp = 2) {
  if (order.price && order.quantity) {
    return new Decimal(order.price)
      .mul(order.quantity)
      .toFixed(dp, Decimal.ROUND_DOWN);
  }

  return 0;
}
/**
 * api order type ==> orderEntry type
 */
export function convertApiOrderTypeToOrderEntryType(order: API.AlgoOrderExt) {
  if (order.algo_type === OrderType.TRAILING_STOP) {
    return order.algo_type;
  }

  const isAlgoOrder = order.algo_order_id !== undefined;

  if (isAlgoOrder && order.algo_type !== AlgoOrderRootType.BRACKET) {
    return `STOP_${order.type}` as OrderType;
  }

  return order.type;
}

export function getPositionalQuantityText(order: API.AlgoOrder): string {
  if (isEntirePositionTPSL(order)) return i18n.t("tpsl.entirePosition");
  if (!order.child_orders?.length)
    return String(getTPSLQuantity(order) ?? "--");
  return (
    (["tp", "sl"] as const)
      .flatMap((leg) => {
        const child = getTPSLLeg(order, leg);
        if (!child?.trigger_price) return [];
        const quantity = isEntirePositionTPSL(child)
          ? i18n.t("tpsl.entirePosition")
          : (getTPSLQuantity(child) ?? "--");
        return [`${i18n.t(leg === "tp" ? "tpsl.tp" : "tpsl.sl")}: ${quantity}`];
      })
      .join(" / ") || "--"
  );
}
