import { order as orderPerp } from "@paxeer-network/perp";
import { API, PaxeerNetworkOrder, OrderSide } from "@paxeer-network/types";
import { OrderType } from "@paxeer-network/types";
import { AlgoOrderType } from "@paxeer-network/types";
import {
  Decimal,
  getTPSLDirection,
  resolveTPSLOrderType,
  todpIfNeed,
  zero,
} from "@paxeer-network/utils";

export type UpdateOrderKey =
  | "tp_trigger_price"
  | "tp_offset_percentage"
  | "tp_offset_percentage_from_mark"
  | "tp_pnl"
  | "tp_offset"
  | "tp_offset_from_mark"
  | "quantity"
  | "sl_trigger_price"
  | "sl_offset_percentage"
  | "sl_offset_percentage_from_mark"
  | "sl_pnl"
  | "sl_offset"
  | "sl_offset_from_mark";

/**
 * offset -> TP/SL price
 */
export function offsetToPrice(inputs: {
  qty: number;
  offset: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
}) {
  const { qty, offset, entryPrice, orderType, orderSide } = inputs;

  if (!offset) return;

  if (orderSide === OrderSide.BUY) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(entryPrice).add(new Decimal(offset)).toNumber();
    }

    return new Decimal(entryPrice).minus(new Decimal(offset)).toNumber();
  }

  if (orderSide === OrderSide.SELL) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(entryPrice).minus(new Decimal(offset)).toNumber();
    }

    return new Decimal(entryPrice).add(new Decimal(offset)).toNumber();
  }
}

export function priceToOffset(
  inputs: {
    qty: number;
    price: number;
    entryPrice: number;
    orderSide: OrderSide;
    orderType: AlgoOrderType;
  },
  options: { symbol?: Pick<API.SymbolExt, "quote_dp"> } = {},
) {
  const { qty, price, entryPrice, orderType, orderSide } = inputs;
  const { symbol } = options;
  let decimal: Decimal;

  if (orderSide === OrderSide.BUY) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      decimal = new Decimal(price).minus(new Decimal(entryPrice));
    }

    decimal = new Decimal(price).minus(new Decimal(entryPrice));
  }

  if (orderSide === OrderSide.SELL) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      decimal = new Decimal(price).minus(new Decimal(entryPrice));
    }

    decimal = new Decimal(entryPrice).minus(new Decimal(price));
  }

  if (symbol) {
    return decimal!.abs().todp(symbol.quote_dp, Decimal.ROUND_UP).toNumber();
  }

  return decimal!.abs().toNumber();
}

export function offsetPercentageToPrice(inputs: {
  qty: number;
  percentage: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
}) {
  const { qty, percentage, entryPrice, orderType, orderSide } = inputs;

  if (!percentage) return;

  if (orderSide === OrderSide.BUY) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(1)
        .add(new Decimal(percentage))
        .mul(new Decimal(entryPrice))
        .toNumber();
    }

    return new Decimal(1)
      .minus(new Decimal(percentage))
      .mul(new Decimal(entryPrice))
      .toNumber();
  }

  if (orderSide === OrderSide.SELL) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(1)
        .minus(new Decimal(percentage))
        .mul(new Decimal(entryPrice))
        .toNumber();
    }

    return new Decimal(1)
      .add(new Decimal(percentage))
      .mul(new Decimal(entryPrice))
      .toNumber();
  }
}

export function priceToOffsetPercentage(inputs: {
  qty: number;
  price: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
}) {
  const { qty, price, entryPrice, orderType, orderSide } = inputs;

  const direction = getTPSLDirection({
    side: orderSide,
    type: orderType === AlgoOrderType.TAKE_PROFIT ? "tp" : "sl",
    closePrice: price,
    orderPrice: entryPrice,
  });

  if (orderSide === OrderSide.BUY) {
    if (entryPrice === 0) return 0;
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(price)
        .div(new Decimal(entryPrice))
        .minus(1)
        .abs()
        .mul(direction)
        .toDecimalPlaces(4, Decimal.ROUND_DOWN)
        .toNumber();
    }

    return new Decimal(1)
      .minus(new Decimal(price).div(new Decimal(entryPrice)))
      .abs()
      .mul(direction)
      .toDecimalPlaces(4, Decimal.ROUND_DOWN)
      .toNumber();
  }

  if (orderSide === OrderSide.SELL) {
    if (entryPrice === 0) return 0;

    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(1)
        .minus(new Decimal(price).div(new Decimal(entryPrice)))
        .abs()
        .mul(direction)
        .toDecimalPlaces(4, Decimal.ROUND_DOWN)
        .toNumber();
    }

    return new Decimal(price)
      .div(new Decimal(entryPrice))
      .minus(1)
      .abs()
      .mul(direction)
      .toDecimalPlaces(4, Decimal.ROUND_DOWN)
      .toNumber();
  }
}

/**
 * pnl -> TP/SL price
 */
export function pnlToPrice(inputs: {
  qty: number;
  pnl: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
}) {
  const { qty, pnl, entryPrice, orderType, orderSide } = inputs;

  if (!pnl) {
    return;
  }

  if (qty === 0) return;

  if (orderSide === OrderSide.BUY) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(entryPrice)
        .plus(new Decimal(pnl).div(new Decimal(qty)))
        .toNumber();
    }

    return new Decimal(entryPrice)
      .add(new Decimal(pnl).div(new Decimal(qty)))
      .toNumber();
  }
  if (orderSide === OrderSide.SELL) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      return new Decimal(entryPrice)
        .add(new Decimal(pnl).div(new Decimal(qty)))
        .toNumber();
    }

    return new Decimal(entryPrice)
      .add(new Decimal(pnl).div(new Decimal(qty)))
      .toNumber();
  }
}

/**
 * TP/SL price -> pnl
 * @price trigger_price
 * @entryPrice calculate price, maybe markPrice/limitPrice/order.price
 */
export function priceToPnl(
  inputs: {
    qty: number;
    price: number;
    entryPrice: number;
    orderSide: OrderSide;
    orderType: AlgoOrderType;
  },
  options: { symbol?: Pick<API.SymbolExt, "quote_dp"> } = {},
): number {
  const { qty, price, entryPrice, orderType, orderSide } = inputs;
  const { symbol } = options;
  let decimal = zero;
  // const qty =
  //   orderSide === OrderSide.BUY ? Math.abs(_qty) : Math.abs(_qty) * -1;

  if (orderSide === OrderSide.BUY) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      decimal = new Decimal(qty).mul(
        new Decimal(price).minus(new Decimal(entryPrice)),
      );
    }

    decimal = new Decimal(qty).mul(
      new Decimal(price).minus(new Decimal(entryPrice)),
    );
  }

  if (orderSide === OrderSide.SELL) {
    if (orderType === AlgoOrderType.TAKE_PROFIT) {
      decimal = new Decimal(qty).mul(
        new Decimal(price).minus(new Decimal(entryPrice)),
      );
    }

    decimal = new Decimal(qty).mul(
      new Decimal(price).minus(new Decimal(entryPrice)),
    );
  }

  if (symbol) {
    // return decimal.todp(symbol.quote_dp, Decimal.ROUND_DOWN).toNumber();
    return decimal.todp(2, Decimal.ROUND_UP).toNumber();
  }

  return decimal.toNumber();
}

export function priceToROI(inputs: {
  values: Partial<PaxeerNetworkOrder>;
  markPrice?: number;
  symbolLeverage?: number;
}) {
  const { values, markPrice, symbolLeverage } = inputs;

  const orderPrice =
    values.order_type === OrderType.MARKET
      ? markPrice
      : Number(values.order_price);

  if (symbolLeverage && orderPrice) {
    let tp_ROI: number | undefined;
    let sl_ROI: number | undefined;
    const tpTriggerPrice = Number(values.tp_trigger_price);

    if (values.tp_pnl && values.tp_trigger_price && tpTriggerPrice > 0) {
      tp_ROI = orderPerp.tpslROI({
        side: values.side!,
        type: "tp",
        closePrice: tpTriggerPrice,
        orderPrice,
        leverage: symbolLeverage,
      });
    }

    const slTriggerPrice = Number(values.sl_trigger_price);

    if (values.sl_pnl && values.sl_trigger_price && slTriggerPrice > 0) {
      sl_ROI = orderPerp.tpslROI({
        side: values.side!,
        type: "sl",
        closePrice: slTriggerPrice,
        orderPrice,
        leverage: symbolLeverage,
      });
    }

    return {
      tp_ROI: tp_ROI?.toString(),
      sl_ROI: sl_ROI?.toString(),
    };
  }

  return {
    tp_ROI: undefined,
    sl_ROI: undefined,
  };
}

/** @deprecated use priceToROI instead */
export function calcTPSL_ROI(inputs: {
  pnl: number | string;
  qty: number | string;
  price: number | string;
}) {
  const qtyNum = Number(inputs.qty);
  const priceNum = Number(inputs.price);
  if (qtyNum === 0 || priceNum === 0) return "0";
  // ROI = pnl / (abs(quantity) * price)
  return new Decimal(inputs.pnl)
    .div(new Decimal(qtyNum).abs().mul(new Decimal(priceNum)))
    .toString();
}

// function formatPrice(price: number | string, symbol?: API.SymbolExt) {
//   if (typeof price !== "string") {
//     price = `${price}`;
//   }

//   if (price.endsWith(".") || !symbol) {
//     return price;
//   }
//   return new Decimal(Number(price))
//     .todp(symbol.quote_dp, Decimal.ROUND_UP)
//     .toNumber();
// }

function checkTPSLOrderTypeIsMarket(
  key: string,
  values: Partial<PaxeerNetworkOrder>,
) {
  const keyPrefix = key.slice(0, 3);
  const orderTypeKey = `${keyPrefix}order_type` as keyof PaxeerNetworkOrder;
  return (
    resolveTPSLOrderType(
      values[orderTypeKey] as OrderType | undefined,
      values[`${keyPrefix}order_price` as keyof PaxeerNetworkOrder] as
        | string
        | undefined,
    ) !== OrderType.LIMIT
  );
}

export function tpslCalculateHelper(
  key: string,
  inputs: {
    key: string;
    value: string | number | boolean;
    entryPrice: number | string;
    qty: number | string;
    orderSide: OrderSide;
    markPrice: number | string;
    values: Partial<PaxeerNetworkOrder>;
  },
  options: { symbol?: Pick<API.SymbolExt, "quote_dp"> } = {},
) {
  const { symbol } = options;
  // if not need to be computed, return the value directly
  if (
    key !== "quantity" &&
    key !== "tp_trigger_price" &&
    key !== "sl_trigger_price" &&
    key !== "tp_pnl" &&
    key !== "sl_pnl" &&
    key !== "tp_offset" &&
    key !== "sl_offset" &&
    key !== "tp_offset_percentage" &&
    key !== "sl_offset_percentage" &&
    key !== "tp_offset_from_mark" &&
    key !== "sl_offset_from_mark" &&
    key !== "tp_offset_percentage_from_mark" &&
    key !== "sl_offset_percentage_from_mark" &&
    key !== "tp_order_price" &&
    key !== "sl_order_price" &&
    key !== "tp_order_type" &&
    key !== "sl_order_type"
    // key !== "tp_enable" &&
    // key !== "sl_enable"
  ) {
    return {
      [key]: inputs.value,
    };
  }

  const orderType = key.startsWith("tp_")
    ? AlgoOrderType.TAKE_PROFIT
    : AlgoOrderType.STOP_LOSS;
  const keyPrefix = key.slice(0, 3);

  const qty = Number(key === "quantity" ? inputs.value : inputs.qty);
  const markPrice = inputs.markPrice ?? inputs.entryPrice;

  // console.log("key", key, inputs.value, inputs.values, inputs.qty);

  if (
    qty === 0 &&
    (key === "tp_pnl" ||
      key === "sl_pnl" ||
      key === "tp_trigger_price" ||
      key === "sl_trigger_price")
  ) {
    return {
      // [`${keyPrefix}trigger_price`]: "",
      // [`${keyPrefix}order_price`]: "",
      // [`${keyPrefix}offset`]: "",
      // [`${keyPrefix}offset_percentage`]: "",
      [`${keyPrefix}pnl`]: "",
      [key]: inputs.value,
    };
  }
  let trigger_price,
    offset,
    offset_percentage,
    offset_from_mark,
    offset_percentage_from_mark,
    pnl,
    order_price,
    tpsl_order_type = resolveTPSLOrderType(
      inputs.values[`${keyPrefix}order_type` as keyof PaxeerNetworkOrder] as
        | OrderType
        | undefined,
      inputs.values[`${keyPrefix}order_price` as keyof PaxeerNetworkOrder] as
        | string
        | undefined,
    );

  const entryPrice = new Decimal(inputs.entryPrice)
    .todp(options.symbol?.quote_dp ?? 2, Decimal.ROUND_UP)
    .toNumber();

  switch (key) {
    case "tp_trigger_price":
    case "sl_trigger_price": {
      trigger_price = inputs.value;
      // if trigger price is empty and the key is `*_trigger_price`, reset the offset and pnl
      // if order_type is market, set trigger_price to order_price
      if (checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        if (inputs.value === "") {
          return {
            [`${keyPrefix}trigger_price`]: trigger_price,
            [`${keyPrefix}offset`]: "",
            [`${keyPrefix}offset_percentage`]: "",
            [`${keyPrefix}offset_from_mark`]: "",
            [`${keyPrefix}offset_percentage_from_mark`]: "",
            [`${keyPrefix}pnl`]: "",
            [`${keyPrefix}ROI`]: "",
          };
        }
      } else {
        order_price =
          inputs.values[`${keyPrefix}order_price` as keyof PaxeerNetworkOrder] ?? "";
      }

      break;
    }

    case "tp_pnl":
    case "sl_pnl": {
      pnl = inputs.value;
      if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        order_price = pnlToPrice({
          qty,
          pnl: Number(inputs.value),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        });
        trigger_price =
          inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
          order_price;
      } else {
        trigger_price = pnlToPrice({
          qty,
          pnl: Number(inputs.value),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        });
      }

      break;
    }

    case "tp_offset":
    case "sl_offset": {
      offset = inputs.value;
      if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        order_price = offsetToPrice({
          qty,
          offset: Number(inputs.value),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType:
            key === "tp_offset"
              ? AlgoOrderType.TAKE_PROFIT
              : AlgoOrderType.STOP_LOSS,
        });

        trigger_price =
          inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
          order_price;
      } else {
        trigger_price = offsetToPrice({
          qty,
          offset: Number(inputs.value),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType:
            key === "tp_offset"
              ? AlgoOrderType.TAKE_PROFIT
              : AlgoOrderType.STOP_LOSS,
        });
      }
      break;
    }

    case "tp_offset_from_mark":
    case "sl_offset_from_mark": {
      offset_from_mark = inputs.value;
      if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        order_price = offsetToPrice({
          qty,
          offset: Number(inputs.value),
          entryPrice: markPrice as number,

          orderSide: inputs.orderSide,
          orderType:
            key === "tp_offset_from_mark"
              ? AlgoOrderType.TAKE_PROFIT
              : AlgoOrderType.STOP_LOSS,
        });
        // order_price = new Decimal(markPrice).add(Number(inputs.value)).toNumber();
        trigger_price =
          inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
          order_price;
      } else {
        trigger_price = offsetToPrice({
          qty,
          offset: Number(inputs.value),
          // entryPrice,
          entryPrice: markPrice as number,
          orderSide: inputs.orderSide,
          orderType:
            key === "tp_offset_from_mark"
              ? AlgoOrderType.TAKE_PROFIT
              : AlgoOrderType.STOP_LOSS,
        });
      }
      break;
    }

    case "tp_order_price":
    case "sl_order_price": {
      order_price = inputs.value;
      trigger_price =
        inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
        order_price;
      break;
    }

    case "tp_order_type":
    case "sl_order_type": {
      tpsl_order_type = inputs.value as OrderType;
      trigger_price =
        (inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] as
          | string
          | number
          | undefined) ?? "";
      if (tpsl_order_type === OrderType.MARKET) {
        order_price = "";
      } else {
        order_price = trigger_price;
      }

      break;
    }

    case "tp_offset_percentage":
    case "sl_offset_percentage": {
      offset_percentage = inputs.value;
      // console.log("offset_percentage", offset_percentage);
      if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        order_price = offsetPercentageToPrice({
          qty,
          percentage: Number(`${inputs.value}`.replace(/\.0{0,2}$/, "")),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        });
        trigger_price =
          inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
          order_price;
      } else {
        trigger_price = offsetPercentageToPrice({
          qty,
          percentage: Number(`${inputs.value}`.replace(/\.0{0,2}$/, "")),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        });
      }
      break;
    }

    case "tp_offset_percentage_from_mark":
    case "sl_offset_percentage_from_mark": {
      offset_percentage_from_mark = inputs.value;
      /** Base price for % is mark, not the main order's limit entry price. */
      const markBase = Number(markPrice);
      if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
        order_price = offsetPercentageToPrice({
          qty,
          percentage: Number(`${inputs.value}`.replace(/\.0{0,2}$/, "")),
          entryPrice: markBase,
          orderSide: inputs.orderSide,
          orderType,
        });
        trigger_price =
          inputs.values[`${keyPrefix}trigger_price` as keyof PaxeerNetworkOrder] ??
          order_price;
      } else {
        trigger_price = offsetPercentageToPrice({
          qty,
          percentage: Number(`${inputs.value}`.replace(/\.0{0,2}$/, "")),
          entryPrice: markBase,
          orderSide: inputs.orderSide,
          orderType,
        });
      }
      break;
    }
  }
  if (!trigger_price && checkTPSLOrderTypeIsMarket(key, inputs.values)) {
    return {
      [`${keyPrefix}trigger_price`]: "",
      [`${keyPrefix}offset`]: "",
      [`${keyPrefix}offset_percentage`]: "",
      [`${keyPrefix}offset_from_mark`]: "",
      [`${keyPrefix}offset_percentage_from_mark`]: "",
      [`${keyPrefix}pnl`]: "",
      [`${keyPrefix}ROI`]: "",
      [key]: inputs.value,
    };
  }
  // console.log('key', {
  //   key,
  //   trigger_price,
  //   order_price,
  //   pnl,
  // })
  let calcPrice = trigger_price;
  if (tpsl_order_type && tpsl_order_type === OrderType.LIMIT) {
    calcPrice = order_price;
  } else if (!checkTPSLOrderTypeIsMarket(key, inputs.values)) {
    calcPrice = order_price;
  }

  if (calcPrice) {
    pnl =
      pnl ??
      priceToPnl(
        {
          qty,
          price: Number(calcPrice),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        },
        options,
      );
    offset =
      offset ??
      priceToOffset(
        {
          qty,
          price: Number(calcPrice),
          entryPrice,
          orderSide: inputs.orderSide,
          orderType,
        },
        options,
      );
    offset_percentage =
      offset_percentage ??
      priceToOffsetPercentage({
        qty,
        price: Number(calcPrice),
        entryPrice,
        orderSide: inputs.orderSide,
        orderType,
      });
  }
  // const

  return {
    [`${keyPrefix}trigger_price`]: trigger_price
      ? todpIfNeed(trigger_price as number, symbol?.quote_dp ?? 2)
      : "",
    [`${keyPrefix}order_type`]: tpsl_order_type ?? OrderType.MARKET,
    [`${keyPrefix}order_price`]: order_price
      ? todpIfNeed(order_price as number, symbol?.quote_dp ?? 2)
      : "",

    [`${keyPrefix}offset`]: offset ?? "",
    [`${keyPrefix}offset_percentage`]: offset_percentage ?? "",
    [`${keyPrefix}offset_from_mark`]:
      offset_from_mark ??
      inputs.values[`${keyPrefix}offset_from_mark` as keyof PaxeerNetworkOrder] ??
      "",
    [`${keyPrefix}offset_percentage_from_mark`]:
      offset_percentage_from_mark ??
      inputs.values[
        `${keyPrefix}offset_percentage_from_mark` as keyof PaxeerNetworkOrder
      ] ??
      "",
    [`${keyPrefix}pnl`]: pnl ?? "",
    // [`${keyPrefix}ROI`]: calcROI({
    //   pnl: Number(pnl ?? 0),
    //   qty,
    //   price: Number(trigger_price!),
    // }),
  };
}
