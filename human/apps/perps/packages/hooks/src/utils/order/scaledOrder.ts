import {
  API,
  DistributionType,
  PaxeerNetworkOrder,
  OrderType,
  MarginMode,
} from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { getOrderPrice } from "./orderPrice";

/**
 * priceStep = (maxPrice - minPrice) / (totalOrders - 1)
 * price[i] = minPrice + i × priceStep
 */
export function calcScaledOrderPrices(inputs: {
  start_price: string;
  end_price: string;
  total_orders: string;
  quote_dp: number;
}) {
  const { start_price, end_price, total_orders, quote_dp } = inputs;

  const startPrice = new Decimal(start_price);
  const endPrice = new Decimal(end_price);
  const totalOrders = Number(total_orders);

  const priceStep = endPrice.minus(startPrice).div(totalOrders - 1);

  const prices = [];

  for (let i = 0; i < totalOrders; i++) {
    prices[i] = startPrice.plus(priceStep.mul(i)).todp(quote_dp).toString();
  }

  return prices;
}

export function getScaledOrderSkew(inputs: {
  skew: string;
  distribution_type: DistributionType;
  total_orders: string;
}) {
  const { skew, distribution_type, total_orders } = inputs;

  if (distribution_type === DistributionType.FLAT) {
    return new Decimal(1);
  } else if (distribution_type === DistributionType.ASCENDING) {
    return new Decimal(total_orders);
  } else if (distribution_type === DistributionType.DESCENDING) {
    return new Decimal(1).div(total_orders);
  }

  return new Decimal(skew);
}

/**
 * slope = (skew - 1) / (totalOrders - 1)
 * weights[i] = 1 + slope × i
 */
export function calcScaledOrderWeights(inputs: {
  total_orders?: string;
  distribution_type?: DistributionType;
  skew?: string;
}) {
  const { total_orders, distribution_type, skew } = inputs;

  const weights: Decimal[] = [];

  if (
    isInvalidTotalOrders(total_orders) ||
    isInvalidSkew(skew, distribution_type)
  ) {
    return {
      weights: [],
      sumWeights: zero,
      minWeight: zero,
    };
  }

  const totalOrders = Number(total_orders);

  const _skew = getScaledOrderSkew({
    skew: skew!,
    distribution_type: distribution_type!,
    total_orders: total_orders!,
  });

  const slope = _skew.minus(1).div(totalOrders - 1);

  for (let i = 0; i < totalOrders; i++) {
    weights[i] = new Decimal(1).plus(slope.mul(i));
  }

  const sumWeights = weights.reduce((acc, cur) => acc.plus(cur), zero);

  // Find the min weight
  const minWeight = weights.reduce(
    (min, current) => (current.lt(min) ? current : min),
    weights?.[0],
  );

  return {
    weights,
    sumWeights,
    minWeight,
  };
}

/**
 * qty[i] = (weights[i] / sum(weights)) × totalAmount
 */
export function calcScaledOrderQtys(inputs: {
  order_quantity: string;
  total_orders: string;
  distribution_type: DistributionType;
  skew: string;
  base_dp: number;
  base_tick: number;
}) {
  const {
    order_quantity,
    total_orders,
    distribution_type,
    skew,
    base_dp,
    base_tick,
  } = inputs;

  const qtys = [];

  const totalOrders = Number(total_orders);

  const { weights, sumWeights } = calcScaledOrderWeights({
    total_orders,
    distribution_type,
    skew,
  });

  for (let i = 0; i < totalOrders; i++) {
    let qty: Decimal | string = new Decimal(order_quantity)
      .mul(weights[i])
      .div(sumWeights);

    if (base_tick > 1) {
      // format quantity to base_tick, if base_tick is 10, 11 => 10, 115 => 110
      qty = qty.div(base_tick).todp(0, Decimal.ROUND_DOWN).mul(base_tick);
    } else {
      qty = qty.todp(base_dp);
    }

    qtys[i] = qty.toString();
  }

  return qtys;
}

export function calcScaledOrderBatchBody(
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
) {
  if (isInvalidScaledOrderInput(order)) {
    return [];
  }

  try {
    const { base_dp, quote_dp, base_tick } = symbolInfo;

    const {
      symbol,
      side,
      order_quantity,
      start_price,
      end_price,
      total_orders,
      distribution_type,
      skew,
      reduce_only,
      visible_quantity,
      margin_mode,
    } = order;

    const prices = calcScaledOrderPrices({
      start_price: start_price!,
      end_price: end_price!,
      total_orders: total_orders!,
      quote_dp,
    });

    const qtys = calcScaledOrderQtys({
      order_quantity: order_quantity!,
      total_orders: total_orders!,
      distribution_type: distribution_type!,
      skew: skew!,
      base_dp,
      base_tick,
    });

    const now = Date.now();
    const orders = prices.map((price, index) => {
      const subOrder: Partial<PaxeerNetworkOrder> = {
        symbol,
        side,
        // this order type is scaled order, so we need to set the order type to limit
        order_type: OrderType.LIMIT,
        order_quantity: qtys[index],
        order_price: price,
        reduce_only,
        margin_mode: margin_mode || MarginMode.CROSS, // Default to CROSS if not provided for backward compatibility
        // it will be used for identify the scaled order from ws
        client_order_id: `scaled_${index}_${now}`,
      };
      // if visible_quantity is 0, set visible_quantity to 0
      if (visible_quantity === 0) {
        subOrder.visible_quantity = visible_quantity;
      }
      return subOrder;
    });

    return orders;
  } catch (error) {
    console.error("calcScaledOrdersBody error", error);
    return [];
  }
}

/**
 * avg_order_price = sum(order_price_i * qty_i) / sum(qty_i)
 */
export function calcScaledOrderAvgPrice(
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
  askAndBid: number[],
) {
  if (isInvalidScaledOrderInput(order)) {
    return null;
  }

  try {
    const orders = calcScaledOrderBatchBody(order, symbolInfo);

    const sumQtys = orders.reduce((acc, order) => {
      return acc.plus(new Decimal(order.order_quantity!));
    }, zero);

    const totalNational = orders.reduce((acc, order) => {
      const orderPrice = getOrderPrice(order, askAndBid);
      return acc.plus(new Decimal(orderPrice).mul(order.order_quantity!));
    }, zero);

    return totalNational.div(sumQtys).todp(symbolInfo.quote_dp).toNumber();
  } catch (error) {
    console.error("calcScaledOrderAvgOrderPrice error", error);
    return null;
  }
}

export function calcScaledOrderMinTotalAmount(
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
  askAndBid: number[],
) {
  try {
    const minTotalAmount_baseMin = calcScaledOrderMinTotalAmountByBaseMin(
      order,
      symbolInfo,
    );

    const minTotalAmount_minNotional =
      calcScaledOrderMinTotalAmountByMinNotional(order, symbolInfo, askAndBid);

    const minTotalAmount = Math.max(
      minTotalAmount_baseMin,
      minTotalAmount_minNotional,
    );

    return minTotalAmount;
  } catch (error) {
    console.error("calcScaledOrderMinTotalAmount error", error);
    return null;
  }
}
/**
 * minWeight / sum(weights)) × totalAmount ≥ base_min
 * totalAmount ≥ base_min × (sum(weights) / minWeight), value need to ceil
 * totalAmount ≥ max ((min_notional x sum(weights)) / (weights[i] x order_price[i]))
 */
export function calcScaledOrderMinTotalAmountByBaseMin(
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
) {
  const { total_orders, distribution_type, skew } = order;
  const { base_min, base_dp } = symbolInfo;

  const { sumWeights, minWeight } = calcScaledOrderWeights({
    total_orders,
    distribution_type,
    skew,
  });

  const minTotalAmount = new Decimal(base_min)
    .mul(sumWeights.div(minWeight))
    .todp(base_dp, Decimal.ROUND_UP)
    .toNumber();

  return minTotalAmount;
}

export function calcScaledOrderMinTotalAmountByMinNotional(
  order: Partial<PaxeerNetworkOrder>,
  symbolInfo: API.SymbolExt,
  askAndBid: number[],
) {
  const { base_dp, min_notional } = symbolInfo;

  const orders = calcScaledOrderBatchBody(order, symbolInfo);

  const { total_orders, distribution_type, skew } = order;

  const { weights, sumWeights } = calcScaledOrderWeights({
    total_orders,
    distribution_type,
    skew,
  });

  const minQtys = orders.map((order, i) => {
    const orderPrice = getOrderPrice(order, askAndBid);
    return new Decimal(min_notional)
      .mul(sumWeights)
      .div(new Decimal(weights[i]).mul(orderPrice));
  });

  // Find the max minQty
  const max_minQty = minQtys.reduce(
    (max, current) => (current.gt(max) ? current : max),
    minQtys?.[0],
  );

  return max_minQty.todp(base_dp, Decimal.ROUND_UP).toNumber();
}

function isInvalidSkew(skew?: string, distribution_type?: DistributionType) {
  const skewNum = Number(skew);
  return (
    !distribution_type ||
    (distribution_type === DistributionType.CUSTOM &&
      (!skew || skewNum <= 0 || skewNum > 100))
  );
}

function isInvalidTotalOrders(total_orders?: string) {
  const totalOrders = Number(total_orders);
  return !total_orders || totalOrders < 2 || totalOrders > 20;
}

function isInvalidScaledOrderInput(order: Partial<PaxeerNetworkOrder>) {
  const {
    start_price,
    end_price,
    order_quantity,
    total_orders,
    distribution_type,
    skew,
  } = order;

  return (
    !start_price ||
    !end_price ||
    !order_quantity ||
    isInvalidTotalOrders(total_orders) ||
    isInvalidSkew(skew, distribution_type)
  );
}
