import { API } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { positionQtyWithOrders_by_symbol } from "./positionNotional";
import { positionNotionalWithOrder_by_symbol } from "./positionNotional";
import { getQtyFromPositions } from "./positionUtils";

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
    IMR_factor_power = 4 / 5,
  } = inputs;
  return Math.max(
    1 / maxLeverage,
    baseIMR,
    new Decimal(IMR_Factor)
      .mul(
        new Decimal(positionNotional)
          .add(orderNotional)
          .abs()
          .toPower(IMR_factor_power),
      )
      .toNumber(),
  );
}

//=========== max qty ==================

// function otherIM(inputs: {}): number {}

/**
 * Total margin used by other symbols (except the current symbol).
 */
export function otherIMs(inputs: {
  // the position list for other symbols except the current symbol
  positions: API.Position[];
  markPrices: { [key: string]: number };
  symbolInfo: any;
  IMR_Factors: { [key: string]: number };
}): number {
  const {
    // orders,
    positions,
    IMR_Factors,
    symbolInfo,
    markPrices,
  } = inputs;

  const symbols = positions.map((item) => item.symbol);

  return symbols
    .reduce((acc, cur) => {
      const symbol = cur;

      if (typeof markPrices[symbol] === "undefined") {
        console.warn("markPrices[%s] is undefined", symbol);
        return acc;
      }

      const markPriceDecimal = new Decimal(markPrices[symbol] || 0);

      const position = positions.find((item) => item.symbol === symbol);

      const positionQty = getQtyFromPositions(positions, symbol);
      const positionNotional = markPriceDecimal.mul(positionQty).toNumber();

      const buyOrdersQty = position!.pending_long_qty;
      const sellOrdersQty = position!.pending_short_qty;

      const ordersNotional = markPriceDecimal
        .mul(new Decimal(buyOrdersQty).add(sellOrdersQty))
        .toNumber();

      const IMR_Factor = IMR_Factors[symbol];

      // IMR_Factor is possible to be 0
      if (typeof IMR_Factor === "undefined") {
        console.warn("IMR_Factor is not found:", symbol);
        return acc;
      }

      const imr = IMR({
        // Use symbol + mode leverage from position directly.
        maxLeverage: position?.leverage || 1,
        IMR_Factor,
        baseIMR: symbolInfo[symbol]("base_imr", 0),
        positionNotional,
        ordersNotional,
      });

      const positionQtyWithOrders = positionQtyWithOrders_by_symbol({
        positionQty,
        buyOrdersQty,
        sellOrdersQty,
      });

      const positionNotionalWithOrders = positionNotionalWithOrder_by_symbol({
        markPrice: markPrices[symbol] || 0,
        positionQtyWithOrders,
      });

      return acc.add(positionNotionalWithOrders.mul(imr));
    }, zero)
    .toNumber();
}
