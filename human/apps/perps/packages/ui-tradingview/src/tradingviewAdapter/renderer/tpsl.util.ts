import { i18n } from "@paxeer-network/i18n";
import {
  Decimal,
  getTPSLQuantity,
  getTPSLEstimatePrice,
  isPositionalTPSL,
} from "@paxeer-network/utils";
import {
  AlgoType,
  OrderInterface,
  OrderType,
  PositionSide,
  SideType,
} from "../type";

export const EST_TPSL_PNL_DECIMAL = 2;

export const textDash = "--";

export const BracketAlgoType = [AlgoType.BRACKET, AlgoType.STOP_BRACKET];
export const TpslAlgoType = [AlgoType.POSITIONAL_TP_SL, AlgoType.TP_SL];
export const isTpOrder = (order: OrderInterface) =>
  order.root_algo_order_id !== order.algo_order_id &&
  order.algo_type === AlgoType.TAKE_PROFIT;

export const isSlOrder = (order: OrderInterface) =>
  order.root_algo_order_id !== order.algo_order_id &&
  order.algo_type === AlgoType.STOP_LOSS;

export const isTpslOrder = (order: OrderInterface) =>
  order.root_algo_order_id !== order.algo_order_id &&
  (order.algo_type === AlgoType.TAKE_PROFIT ||
    order.algo_type === AlgoType.STOP_LOSS);

export const isBracketAlgoType = (algoType: AlgoType | undefined | null) =>
  !!algoType && BracketAlgoType.includes(algoType);

export const formatOrderNo = (orderNo?: number) => {
  return orderNo ? String(orderNo).padStart(2, "0") : "";
};

export const isPositionTpsl = (order: OrderInterface) =>
  isPositionalTPSL(order);
export const isActivatedPositionTpsl = (order: OrderInterface) =>
  isPositionTpsl(order) && order.is_activated;

export const isActivatedTpslOrder = (order: OrderInterface) =>
  isTpslOrder(order) && order.is_activated;

export const isActivatedQuantityTpsl = (order: OrderInterface) =>
  order.root_algo_order_algo_type === AlgoType.TP_SL ||
  (isBracketAlgoType(order.root_algo_order_algo_type) && order.is_activated);

export const getTpslTag = (
  order: OrderInterface,
  quantityTpslNoMap: Map<number, number>,
) => {
  const algoType = order.algo_type;
  // @ts-ignore
  const suffix = {
    [AlgoType.TAKE_PROFIT]: i18n.t("tpsl.takeProfit"),
    [AlgoType.STOP_LOSS]: i18n.t("tpsl.stopLoss"),
  }[algoType];

  if (!suffix) {
    return null;
  }

  return suffix;
};

export const buildQuantityTpslNoMap = (orders: OrderInterface[]) => {
  const quantityTpslNoMap = new Map<number, number>();

  let idx = 1;
  [...orders]
    .reverse()
    .filter(isActivatedQuantityTpsl)
    .forEach((order) => {
      if (
        order.root_algo_order_id &&
        !quantityTpslNoMap.has(order.root_algo_order_id)
      ) {
        quantityTpslNoMap.set(order.root_algo_order_id, idx++);
      }
    });

  return quantityTpslNoMap;
};

export const getTpslEstPnl = (tpslOrder: OrderInterface, position: any) => {
  const quantity = getTPSLQuantity(tpslOrder, position?.balance);
  const price = getTPSLEstimatePrice(tpslOrder);
  if (quantity == null || price == null || !position)
    return { estPnl: undefined, quantity, openPrice: undefined };
  const sideFlag = tpslOrder.side === SideType.SELL ? 1 : -1;

  // Use mark price for PnL calculation when available, fallback to entry price
  const priceRef = position.open;
  const estPnl = new Decimal(price)
    .minus(priceRef)
    .times(quantity)
    .times(sideFlag)
    .toString();

  return { estPnl, quantity, openPrice: priceRef };
};

export const formatPnl = (pnl: number | string | undefined) => {
  return pnl !== undefined && pnl !== ""
    ? new Decimal(pnl).todp(EST_TPSL_PNL_DECIMAL, Decimal.ROUND_FLOOR)
    : textDash;
};

/**
 * Derives position side from balance.
 * Positive/zero balance → LONG, Negative balance → SHORT
 */
export const getPositionSide = (balance: number): PositionSide => {
  return balance >= 0 ? PositionSide.LONG : PositionSide.SHORT;
};

/**
 * Determines TPSL order type based on target price, mark price, and position side.
 *
 * @param targetPrice - User-dragged TPSL trigger price
 * @param markPrice - Current mark price (fallback to entry price if unavailable)
 * @param positionSide - LONG or SHORT (derived from position.balance sign)
 * @returns "tp" for take-profit, "sl" for stop-loss
 *
 * Logic:
 *   Long:  target > mark → TP,  target < mark → SL
 *   Short: target < mark → TP,  target > mark → SL
 */
export const determineTpslType = (
  targetPrice: number,
  markPrice: number,
  positionSide: PositionSide,
): "tp" | "sl" => {
  if (positionSide === PositionSide.LONG) {
    return targetPrice > markPrice ? "tp" : "sl";
  }
  // Short
  return targetPrice < markPrice ? "tp" : "sl";
};
