import { API, OrderSide, MarginMode } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { IMRFactorPower } from "../constants";
import {
  positionNotionalWithOrder_by_symbol,
  positionQtyWithOrders_by_symbol,
} from "./positionNotional";
import { getQtyFromPositions, getQtyFromOrdersBySide } from "./positionUtils";

/**
 * Calculate initial margin for a single symbol.
 * Formula: cross_position_notional_with_orders_i * cross_IMR_i
 */
function calculateSymbolInitialMargin(params: {
  symbol: string;
  positionQty: number;
  buyOrdersQty: number;
  sellOrdersQty: number;
  markPrice: number;
  IMR_Factors: { [key: string]: number };
  symbolInfo: any;
  symbolMaxLeverage: number;
}): number {
  const {
    symbol,
    positionQty,
    buyOrdersQty,
    sellOrdersQty,
    markPrice,
    IMR_Factors,
    symbolInfo,
    symbolMaxLeverage,
  } = params;

  // Formula: cross_position_qty_with_orders_i = max[abs(pos + buy), abs(pos - sell)]
  const positionQtyWithOrders = positionQtyWithOrders_by_symbol({
    positionQty,
    buyOrdersQty,
    sellOrdersQty,
  });

  // Formula: cross_position_notional_with_orders_i = abs(mark_price * qty_with_orders)
  const positionNotionalWithOrders = positionNotionalWithOrder_by_symbol({
    markPrice,
    positionQtyWithOrders,
  });

  const markPriceDecimal = new Decimal(markPrice);

  // Formula: cross_IMR_i = Max(1/leverage, baseIMR, IMR_Factor * |posNotional + orderNotional|^(4/5))
  // Cross Order Notional = mark_price * (buyOrdersQty - sellOrdersQty)
  // Buy orders increase exposure (+), sell orders decrease exposure (-)
  const imr = IMR({
    positionNotional: markPriceDecimal.mul(positionQty).toNumber(),
    ordersNotional: markPriceDecimal
      .mul(new Decimal(buyOrdersQty).sub(sellOrdersQty))
      .toNumber(),
    maxLeverage: symbolMaxLeverage,
    IMR_Factor: IMR_Factors[symbol],
    baseIMR: symbolInfo[symbol]("base_imr", 0),
  });

  return positionNotionalWithOrders.mul(imr).toNumber();
}

function IMR(inputs: {
  maxLeverage: number;
  baseIMR: number;
  IMR_Factor: number;
  positionNotional: number;
  ordersNotional: number;
  IMR_factor_power?: number;
}): number {
  const {
    maxLeverage,
    baseIMR,
    IMR_Factor,
    positionNotional,
    ordersNotional: orderNotional,
    IMR_factor_power = IMRFactorPower,
  } = inputs;

  const imr =
    IMR_Factor === 0
      ? 0
      : new Decimal(IMR_Factor)
          .mul(
            new Decimal(positionNotional)
              .add(orderNotional)
              .abs()
              .toPower(IMR_factor_power),
          )
          .toNumber();
  return Math.max(1 / maxLeverage, baseIMR, imr);
}

/**
 * Calculate total initial margin with orders for cross margin positions.
 * Formula: total_initial_margin_with_orders = sum(cross_position_notional_with_orders_i * cross_IMR_i)
 *
 * @param positions - All positions (will be filtered to cross margin only)
 * @param orders - All orders (will be filtered to cross margin only). When
 * omitted, pending quantities aggregated on cross positions are used instead.
 * @param markPrices - Mark prices by symbol
 * @param symbolInfo - Symbol info accessor
 * @param IMR_Factors - IMR factors by symbol
 * @param maxLeverageBySymbol - Symbol leverage map (symbol + margin mode)
 */
export function totalInitialMarginWithQty(inputs: {
  positions: API.Position[];
  orders?: API.Order[];
  markPrices: { [key: string]: number };
  symbolInfo: any;
  IMR_Factors: { [key: string]: number };
  maxLeverageBySymbol?: Record<string, number>;
}): number {
  const {
    positions,
    orders,
    markPrices,
    IMR_Factors,
    symbolInfo,
    maxLeverageBySymbol,
  } = inputs;

  // Filter to cross margin only (isolated margin is excluded)
  const crossPositions = positions.filter(
    (p) => p.margin_mode !== MarginMode.ISOLATED,
  );
  const crossOrders = (orders ?? []).filter(
    (o) => o.margin_mode !== MarginMode.ISOLATED,
  );
  const usePositionPendingQty = orders === undefined;
  const crossLeverageBySymbol = crossPositions.reduce<Record<string, number>>(
    (acc, position) => {
      if (!acc[position.symbol] && position.leverage) {
        acc[position.symbol] = position.leverage;
      }
      return acc;
    },
    {},
  );

  // Extract all symbols from both positions and orders
  // (orders-only symbols should also be included in calculation)
  const symbols = extractSymbols(crossPositions, crossOrders);

  // Calculate initial margin for each symbol and sum
  return symbols
    .map((symbol) => {
      const positionQty = getQtyFromPositions(crossPositions, symbol);
      const markPrice = markPrices[symbol] || 0;
      const buyOrdersQty = usePositionPendingQty
        ? getPendingOrderQtyFromPositions(
            crossPositions,
            symbol,
            "pending_long_qty",
          )
        : getQtyFromOrdersBySide(crossOrders, symbol, OrderSide.BUY);
      const sellOrdersQty = usePositionPendingQty
        ? getPendingOrderQtyFromPositions(
            crossPositions,
            symbol,
            "pending_short_qty",
          )
        : getQtyFromOrdersBySide(crossOrders, symbol, OrderSide.SELL);
      const symbolMaxLeverage =
        crossLeverageBySymbol[symbol] ?? maxLeverageBySymbol?.[symbol] ?? 1;

      return calculateSymbolInitialMargin({
        symbol,
        positionQty,
        buyOrdersQty,
        sellOrdersQty,
        markPrice,
        IMR_Factors,
        symbolInfo,
        symbolMaxLeverage,
      });
    })
    .reduce((acc, margin) => acc.add(margin), zero)
    .toNumber();
}

function getPendingOrderQtyFromPositions(
  positions: API.Position[],
  symbol: string,
  field: "pending_long_qty" | "pending_short_qty",
): number {
  return positions
    .filter((position) => position.symbol === symbol)
    .reduce(
      (total, position) => total.add(Math.max(position[field] ?? 0, 0)),
      zero,
    )
    .toNumber();
}

function extractSymbols(
  positions: Pick<API.Position, "symbol">[],
  orders: Pick<API.Order, "symbol">[],
): string[] {
  const symbols = new Set<string>();

  positions.forEach((item) => {
    symbols.add(item.symbol);
  });

  orders.forEach((item) => {
    symbols.add(item.symbol);
  });

  return Array.from(symbols);
}
