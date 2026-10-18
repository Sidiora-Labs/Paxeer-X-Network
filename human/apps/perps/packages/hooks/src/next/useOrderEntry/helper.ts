import { order as orderUtils } from "@paxeer-network/perp";
import {
  AlgoOrderRootType,
  API,
  MarginMode,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { OrderMetadataConfig } from "../../paxeerNetworkContext";
import { OrderFactory } from "../../services/orderCreator/factory";
import { getOrderPrice } from "../../utils/order/orderPrice";
import {
  calcScaledOrderAvgPrice,
  calcScaledOrderBatchBody,
} from "../../utils/order/scaledOrder";

export const getCreateOrderUrl = (order: Partial<PaxeerNetworkOrder>): string => {
  const isAlgoOrder =
    order?.order_type === OrderType.STOP_LIMIT ||
    order?.order_type === OrderType.STOP_MARKET ||
    order?.order_type === OrderType.TRAILING_STOP ||
    order?.order_type === OrderType.CLOSE_POSITION ||
    (order.algo_type && order.algo_type === AlgoOrderRootType.BRACKET) ||
    isBracketOrder(order);

  // scaled order will create multiple orders, so it is a batch order
  if (order.order_type === OrderType.SCALED) {
    return "/v1/batch-order";
  }

  return isAlgoOrder ? "/v1/algo/order" : "/v1/order";
};

export const getOrderCreator = (order: Partial<PaxeerNetworkOrder>) => {
  let type;
  if (isBracketOrder(order)) {
    type = `${AlgoOrderRootType.BRACKET}:${order.order_type}`;
  } else if (order.order_type === OrderType.LIMIT) {
    type = order.order_type_ext || order.order_type;
  } else {
    type = order.order_type;
  }
  return OrderFactory.create(type!);
};

export const tpslFields = [
  "tp_trigger_price",
  "sl_trigger_price",
  "tp_pnl",
  "sl_pnl",
  "tp_offset",
  "sl_offset",
  "tp_offset_percentage",
  "sl_offset_percentage",
  "tp_offset_from_mark",
  "sl_offset_from_mark",
  "tp_offset_percentage_from_mark",
  "sl_offset_percentage_from_mark",
] as (keyof PaxeerNetworkOrder)[];

export const isBracketOrder = (order: Partial<PaxeerNetworkOrder>): boolean => {
  // if (order.sl_enable || order.tp_enable) {
  //   return true;
  // }
  return !!order.tp_trigger_price || !!order.sl_trigger_price;
};

export const hasTPSL = (order: Partial<PaxeerNetworkOrder>): boolean => {
  return tpslFields.some((field) => !!order[field]);
};

export const getPriceAndQty = (
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
  askAndBid: number[],
) => {
  let quantity = Number(order.order_quantity);
  const orderPrice = Number(order.order_price);

  if (isNaN(quantity) || quantity <= 0) {
    return null;
  }

  if (askAndBid.length === 0) {
    console.warn(
      "Please check if you are using the `useOrderbookStream` hook or if the orderBook has data.",
    );
    return null;
  }

  if (
    (order.order_type === OrderType.LIMIT ||
      order.order_type === OrderType.STOP_LIMIT) &&
    isNaN(orderPrice)
  ) {
    return null;
  }

  let price: number | null;

  if (order.order_type === OrderType.SCALED) {
    price = calcScaledOrderAvgPrice(order, symbolInfo, askAndBid);
    const orders = calcScaledOrderBatchBody(order, symbolInfo);

    const sumQtys = orders.reduce((acc, order) => {
      return acc.plus(new Decimal(order.order_quantity!));
    }, zero);

    quantity = sumQtys.todp(symbolInfo.base_dp).toNumber();

    // revalidate quantity
    if (!quantity || isNaN(quantity)) {
      return null;
    }
  } else {
    price = getOrderPrice(order, askAndBid);
  }

  if (!price || isNaN(price)) {
    return null;
  }

  if (order.side === OrderSide.SELL) {
    quantity = -quantity;
  }

  return { price, quantity };
};

export const calcEstLiqPrice = (
  order: Partial<PaxeerNetworkOrder>,
  askAndBid: number[],
  inputs: {
    futures_taker_fee_rate: number;
    imr_factor: number;
    symbol: string;
    totalCollateral: number;
    markPrice: number;
    positions: API.PositionTPSLExt[] | null;
    symbolInfo: API.SymbolExt;
    sumUnitaryFunding: number;
    symbolLeverage?: number;
  },
) => {
  const { symbolInfo } = inputs;
  const result = getPriceAndQty(order, symbolInfo, askAndBid);

  if (!result) return null;

  const { price, quantity } = result;
  if (!price || !quantity) return null;

  const {
    symbol,
    imr_factor,
    markPrice,
    totalCollateral,
    futures_taker_fee_rate,
    positions,
    symbolLeverage,
    sumUnitaryFunding,
  } = inputs;

  const orderFee = orderUtils.orderFee({
    qty: quantity,
    price,
    futuresTakeFeeRate: Number(futures_taker_fee_rate) / 10000,
  });

  let liqPrice = 0;

  if (order.margin_mode === MarginMode.CROSS) {
    liqPrice = orderUtils.estLiqPrice({
      markPrice,
      baseIMR: symbolInfo.base_imr,
      baseMMR: symbolInfo.base_mmr,
      totalCollateral,
      positions: positions == null ? [] : positions,
      IMR_Factor: imr_factor,
      orderFee,
      newOrder: {
        qty: quantity,
        price,
        symbol,
      },
    });
  } else {
    let isolatedPositionMargin = 0,
      costPosition = 0,
      positionQty = 0,
      lastSumUnitaryFunding = 0,
      leverage = symbolLeverage ?? 1;

    if (positions) {
      const position = positions.find(
        (p) => p.symbol === symbol && p.margin_mode === MarginMode.ISOLATED,
      );
      if (position) {
        isolatedPositionMargin = position.margin ?? 0;
        costPosition = position.cost_position ?? 0;
        positionQty = position.position_qty ?? 0;
        lastSumUnitaryFunding = position.last_sum_unitary_funding ?? 0;
        leverage = position.leverage ? Number(position.leverage) : 1;
      }
    }

    liqPrice = orderUtils.estLiqPriceIsolated({
      isolatedPositionMargin,
      costPosition,
      positionQty,
      sumUnitaryFunding,
      lastSumUnitaryFunding,
      markPrice,
      baseIMR: symbolInfo.base_imr,
      baseMMR: symbolInfo.base_mmr,
      IMR_Factor: imr_factor,
      leverage,
      newOrder: {
        symbol,
        qty: quantity,
        price,
      },
    });
  }

  if (liqPrice <= 0) return null;

  return liqPrice;
};

export const calcEstLeverage = (
  order: Partial<PaxeerNetworkOrder>,
  askAndBid: number[],
  inputs: {
    totalCollateral: number;
    positions: any;
    symbol: string;
    symbolInfo: API.SymbolExt;
  },
) => {
  const { totalCollateral, positions, symbol, symbolInfo } = inputs;

  const result = getPriceAndQty(order, symbolInfo, askAndBid);

  if (!result) return null;

  const { price, quantity } = result;
  if (!price || !quantity) return null;

  return orderUtils.estLeverage({
    totalCollateral,
    positions,
    newOrder: {
      symbol,
      qty: result.quantity,
      price: result.price,
    },
  });
};

export function isBBOOrder(options: {
  order_type?: OrderType;
  order_type_ext?: OrderType;
}) {
  const { order_type, order_type_ext } = options;

  return (
    order_type === OrderType.LIMIT &&
    [OrderType.ASK, OrderType.BID].includes(order_type_ext!)
  );
}

export function appendOrderMetadata(
  order: Partial<PaxeerNetworkOrder> | Partial<PaxeerNetworkOrder>[],
  orderMetadata?: OrderMetadataConfig,
) {
  if (Array.isArray(order)) {
    return order.map((item) => ({
      ...item,
      ...(typeof orderMetadata === "function"
        ? orderMetadata(item)
        : orderMetadata),
    }));
  }

  return {
    ...order,
    ...(typeof orderMetadata === "function"
      ? orderMetadata(order)
      : orderMetadata),
  };
}
