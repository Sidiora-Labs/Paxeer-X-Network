import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { compose, head, includes, omit, pick } from "ramda";
import { useDebouncedCallback } from "use-debounce";
import { order as orderUtils } from "@paxeer-network/perp";
import {
  API,
  MarginMode,
  OrderEntity,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  SDKError,
} from "@paxeer-network/types";
import { Decimal, getPrecisionByNumber } from "@paxeer-network/utils";
import { useCollateral } from "../paxeer-network/useCollateral";
import { useMarginModeBySymbol } from "../paxeer-network/useMarginModes";
// import { availableOrderTypes } from "../utils/createOrder";
import { useMarkPrice } from "../paxeer-network/useMarkPrice";
import { useMaxQty } from "../paxeer-network/useMaxQty";
import { usePositions } from "../paxeer-network/usePositionStream/usePosition.store";
import { useSymbolsInfo } from "../paxeer-network/useSymbolsInfo";
import { OrderFactory } from "../services/orderCreator/factory";
import { useEventEmitter } from "../useEventEmitter";
import { useMutation } from "../useMutation";
import {
  baseInputHandle,
  getCalculateHandler,
  orderEntityFormatHandle,
} from "../utils/orderEntryHelper";

export type UseOrderEntryOptions = {
  commify?: boolean;
  // Whether to observe the orderbook,  if it is a limit order, the orderbook will automatically calculate the est. liq. price when it is updated.
  watchOrderbook?: boolean;
  validate?: (
    data: OrderEntity,
  ) => { [P in keyof OrderEntity]?: string } | null | undefined;
};

export type UseOrderEntryMetaState = {
  errors:
    | { [P in keyof OrderEntity]?: { type: string; message: string } }
    | null
    | undefined;
  dirty: { [P in keyof OrderEntity]?: boolean } | null | undefined;
  submitted: boolean;
};

export type UseOrderEntryReturn = {
  // Maximum open position
  maxQty: number;
  freeCollateral: number;
  markPrice: number;
  estLiqPrice?: number | null;
  estLeverage?: number | null;
  //@Deprecated
  onSubmit: (order: OrderEntity) => Promise<any>;
  submit: () => Promise<OrderEntity>;
  // order: data,
  submitting: boolean;
  formattedOrder: Partial<OrderEntity>;
  helper: {
    calculate: (
      values: Partial<PaxeerNetworkOrder>,
      field: keyof OrderEntity,
      value: any,
    ) => Partial<OrderEntity>;
    validator: (values: Partial<OrderEntity>) => any;
    // clearErrors: () => void;
    // setValues: (values: Partial<OrderEntity>) => void;
  };
  // formState: any;
  metaState: UseOrderEntryMetaState;
  symbolConfig: API.SymbolExt;
};

export type OrderParams = Required<
  Pick<OrderEntity, "side" | "order_type" | "symbol">
> &
  Partial<Omit<OrderEntity, "side" | "symbol" | "order_type">>;

/**
 * Create Order
 * @example
 * ```tsx
 * import { useOrderEntry } from "@paxeer-network/hooks";
 * import {OrderSide, OrderType} from '@paxeer-network/types';
 *
 * const { formattedOrder, onSubmit, helper } = useOrderEntry({
 *  symbol: "PERP_ETH_USDC",
 *  side: OrderSide.BUY,
 *  order_type: OrderType.LIMIT,
 *  order_price: 10000,
 *  order_quantity: 1,
 * },{
 *  // **Note:** it's required
 *  watchOrderbook: true,
 * });
 * ```
 */
export function useOrderEntry(
  order: OrderParams,
  options?: UseOrderEntryOptions,
): UseOrderEntryReturn;
/**
 * @deprecated
 */
export function useOrderEntry(
  symbol: string,
  side: OrderSide,
  reduceOnly: boolean,
): UseOrderEntryReturn;
export function useOrderEntry(
  symbolOrOrder: string | OrderParams,
  sideOrOptions?: OrderSide | UseOrderEntryOptions,
  reduceOnly?: boolean,
  options?: UseOrderEntryOptions,
): UseOrderEntryReturn {
  // if symbolOrOrder is string, then it's deprecated
  let isNewVersion = false;
  if (typeof symbolOrOrder === "object") {
    isNewVersion = true;

    if (!symbolOrOrder.symbol) {
      throw new SDKError("Symbol is required");
    }

    if (!symbolOrOrder.side) {
      throw new SDKError("Order side is required");
    }

    if (!symbolOrOrder.order_type) {
      throw new SDKError("Order type is required");
    }
  }

  const prevOrderData = useRef<Partial<OrderEntity> | null>(null);
  // Cache data from the last calculate
  const orderDataCache = useRef<Partial<OrderEntity> | null>(null);
  //
  const notSupportData = useRef<Partial<OrderEntity>>({});

  const [errors, setErrors] = useState<any>(null);

  const ee = useEventEmitter();

  const positions = usePositions();

  const fieldDirty = useRef<{ [K in keyof OrderEntity]?: boolean }>({});
  const submitted = useRef<boolean>(false);
  const askAndBid = useRef<number[]>([]); // 0: ask0, 1: bid0

  const onOrderBookUpdate = useDebouncedCallback((data: number[]) => {
    askAndBid.current = data;
  }, 200);

  const { freeCollateral, totalCollateral, accountInfo } = useCollateral();

  const symbolInfo = useSymbolsInfo();
  // const tokenInfo = useTokenInfo();

  const symbol = useMemo(() => {
    if (typeof symbolOrOrder === "string") {
      return symbolOrOrder;
    }
    return symbolOrOrder.symbol!;
  }, [symbolOrOrder]);

  const optionsValue = useMemo(() => {
    if (typeof sideOrOptions === "object") {
      return sideOrOptions;
    }

    return options;
  }, [sideOrOptions]);

  const isReduceOnly = useMemo<boolean>(() => {
    if (typeof reduceOnly === "boolean") {
      return reduceOnly;
    }

    if (typeof symbolOrOrder === "object") {
      return !!symbolOrOrder.reduce_only;
    }

    return false;
  }, [symbolOrOrder, reduceOnly]);

  const sideValue = useMemo(() => {
    if (typeof symbolOrOrder === "object") {
      return symbolOrOrder.side;
    }

    // if (typeof sideOrOptions === "string") {
    return sideOrOptions as OrderSide;
    // }
  }, [symbolOrOrder, sideOrOptions]);

  const baseDP = useMemo(
    () => getPrecisionByNumber(symbolInfo[symbol]("base_tick", 0)),
    [symbolInfo],
  );
  const quoteDP = useMemo(() => {
    return getPrecisionByNumber(symbolInfo[symbol]("quote_tick", 0));
  }, [symbolInfo]);

  const baseIMR = useMemo(
    () => symbolInfo[symbol]("base_imr", 0),
    [symbolInfo],
  );
  const baseMMR = useMemo(
    () => symbolInfo[symbol]("base_mmr", 0),
    [symbolInfo],
  );

  const { data: markPrice } = useMarkPrice(symbol);
  // const markPrice = 1;

  const diffOrderEntry = (
    prev: Partial<OrderParams>,
    current: Partial<OrderParams>,
  ): { key: keyof OrderParams; value: any; preValue: any } | null => {
    if (!prev) return null;
    let key, value, preValue;
    const keys = Object.keys(current) as (keyof OrderParams)[];

    for (let i = 0; i < keys.length; i++) {
      const k = keys[i];

      const preveValue = prev[k];
      const currentValue = current[k];

      if (
        typeof preveValue === "undefined" &&
        typeof currentValue === "undefined"
      )
        continue;

      // if (k === "order_quantity") {
      //   preveValue = Number(preveValue);
      //   currentValue = Number(currentValue);
      // }

      if (preveValue !== currentValue) {
        key = k;
        value = currentValue;
        preValue = preveValue;
        break;
      }
    }

    if (!key) return null;

    return { key, value, preValue };
  };

  const { marginMode: symbolMarginMode } = useMarginModeBySymbol(symbol);
  const marginMode =
    typeof symbolOrOrder === "object" && symbolOrOrder.margin_mode
      ? symbolOrOrder.margin_mode
      : symbolMarginMode;
  const maxQty = useMaxQty(symbol, sideValue, {
    reduceOnly: isReduceOnly,
    marginMode: marginMode ?? MarginMode.CROSS,
  });

  const parseString2Number = (
    order: OrderParams & Record<string, any>,
    key: keyof OrderParams,
    dp?: number,
  ) => {
    if (typeof order[key] !== "string") return;
    // fix: delete the comma then remove the forward one of the string
    // first find the difference between current value and previous value

    if (order[key] && (order[key] as string).startsWith(".")) {
      (order[key] as string) = `0${order[key]}`;
    }

    // order[`${key}_origin`] = order[key];
    (order[key] as string) = (order[key] as string).replace(/,/g, "");

    // format input by decimal precision
    if (dp && (order[key] as string).length > 0) {
      const hasPoint = `${order[key]}`.includes(".");
      const endOfPoint = `${order[key]}`.endsWith(".");
      const decimalPart = `${order[key]}`.split(".");
      if (hasPoint && !endOfPoint) {
        (order[key] as string) = `${decimalPart[0]}.${decimalPart[1].slice(
          0,
          quoteDP,
        )}`;
      }
    }
  };

  // just for performance optimization
  const needParse = useMemo(() => {
    if (typeof symbolOrOrder === "string") {
      return null;
    }
    return pick([
      "order_price",
      "side",
      "order_quantity",
      "visible_quantity",
      "order_type",
      "order_type_ext",
      "symbol",
      "total",
      "reduce_only",
      "trigger_price",
    ])(
      //@ts-ignore
      symbolOrOrder,
    );
  }, [symbolOrOrder]);

  const parsedData = useMemo<OrderParams | null>(() => {
    if (typeof symbolOrOrder === "string") {
      return null;
    }
    // clean comma

    if (typeof symbolOrOrder.order_quantity === "string") {
      parseString2Number(symbolOrOrder, "order_quantity");
    } else if (typeof symbolOrOrder.order_quantity === "number") {
      symbolOrOrder.order_quantity = new Decimal(symbolOrOrder.order_quantity)
        .toDecimalPlaces(baseDP)
        .toString();
    }

    if (typeof symbolOrOrder.order_price === "string") {
      parseString2Number(symbolOrOrder, "order_price", quoteDP);
    }

    if (typeof symbolOrOrder.total === "string") {
      parseString2Number(symbolOrOrder, "total", quoteDP);
    }

    if (typeof symbolOrOrder.trigger_price === "string") {
      parseString2Number(symbolOrOrder, "trigger_price", quoteDP);
    }

    // if (typeof symbolOrOrder.trigger_price === "string") {
    //   symbolOrOrder.trigger_price = symbolOrOrder.trigger_price.replace(
    //     /,/g,
    //     ""
    //   );
    // }

    return symbolOrOrder;
  }, [
    needParse?.order_price,
    needParse?.order_quantity,
    needParse?.total,
    needParse?.trigger_price,
    needParse?.order_type,
    needParse?.order_type_ext,
    needParse?.symbol,
    needParse?.reduce_only,
    needParse?.side,
    needParse?.visible_quantity,
    quoteDP,
    baseDP,
  ]);

  const isAlgoOrder =
    parsedData?.order_type === OrderType.STOP_LIMIT ||
    parsedData?.order_type === OrderType.STOP_MARKET ||
    parsedData?.order_type === OrderType.CLOSE_POSITION;

  const [doCreateOrder, { isMutating }] = useMutation<OrderEntity, any>(
    isAlgoOrder ? "/v1/algo/order" : "/v1/order",
  );

  // const maxQty = 3;

  const createOrder = (values: Partial<OrderEntity>): Promise<OrderEntity> => {
    if (!values.symbol) {
      throw new SDKError("Symbol is error");
    }

    if (!values.side) {
      throw new SDKError("Order side is error");
    }

    if (!values || typeof values.order_type === "undefined") {
      throw new SDKError("Order type is error");
    }

    const orderCreator = OrderFactory.create(
      // @ts-ignore
      values.order_type_ext ? values.order_type_ext : values.order_type,
    );

    if (!orderCreator) {
      return Promise.reject(new SDKError("Order creator is null"));
    }

    return new Promise((resolve, reject) => {
      return orderCreator
        .validate(values, {
          symbol: symbolInfo[symbol](),
          // token: tokenInfo[symbol](),
          maxQty,
          markPrice: markPrice,
        })
        .then((errors: any) => {
          submitted.current = true;

          if (
            errors.order_price ||
            errors.order_quantity ||
            errors.trigger_price ||
            errors.total
          ) {
            setErrors(errors);
            reject(
              errors.order_price?.message || errors.order_quantity?.message,
            );
            // throw new SDKError(
            //   errors.order_price?.message ||
            //     errors.order_quantity?.message ||
            //     "order validation error"
            // );
          } else {
            const data = orderCreator.create(values as OrderEntity, {
              symbol: symbolInfo[symbol](),
              maxQty,
              markPrice: markPrice,
            });

            // console.log("------------------", values, data);

            return doCreateOrder(
              omit(["order_type_ext"], {
                // ...values,
                // ...omit(["order_price"], values),
                ...data,
              }),
            ).then((res) => {
              // resolve(res);
              if (res && res.success) {
                // TODO: remove when the WS service is fixed

                // if (Array.isArray(res.data.rows)) {
                //   ee.emit("algoOrder:cache", {
                //     ...res.data.rows[0],
                //     trigger_price: data.trigger_price,
                //   });
                // }

                resolve(res.data);
              } else {
                reject(res);
              }
            }, reject);
          }
        });
    });
  };

  /**
   * submit form，validate values
   * @param values
   * @returns
   */
  const onSubmit = (values: OrderEntity) => {
    if (typeof reduceOnly === "boolean" && reduceOnly && !values.reduce_only) {
      return Promise.reject(
        new SDKError(
          "The reduceOny parameter of hook does not match your order data",
        ),
      );
    }
    return createOrder({
      ...values,
      symbol,
    });
  };

  const submit = useCallback(() => {
    if (!parsedData) {
      throw new SDKError("Function is not supported, please use onSubmit()");
    }
    return createOrder(parsedData);
  }, [parsedData]);

  const calculate = useCallback(
    (
      values: Partial<PaxeerNetworkOrder>,
      field: keyof PaxeerNetworkOrder,
      value: any,
    ): Partial<OrderEntity> => {
      const fieldHandler = getCalculateHandler(field);
      const newValues = compose(
        head,
        orderEntityFormatHandle(baseDP, quoteDP),
        fieldHandler,
        baseInputHandle,
      )([
        values,
        field,
        value,
        markPrice,
        { base_dp: baseDP, quote_dp: quoteDP } as API.SymbolExt,
      ]);

      return newValues as Partial<OrderEntity>;
    },
    [markPrice],
  );

  // const estLiqPrice = useMemo(() => {}, []);

  const validator = (values: any) => {
    // @ts-ignore
    const creator = OrderFactory.create(values.order_type);

    return creator?.validate(values, {
      symbol: symbolInfo[symbol](),
      // token: tokenInfo[symbol](),
      maxQty,
      markPrice: markPrice,
    });
  };

  const formattedOrder = useMemo<Partial<OrderEntity>>(() => {
    if (!parsedData) {
      return notSupportData.current;
    }
    // prevOrderData.current = symbolOrOrder;

    if (!prevOrderData.current) {
      // prevOrderData.current = {
      //   ...symbolOrOrder,
      //   total: "",
      // };

      prevOrderData.current = parsedData;
      orderDataCache.current = {
        ...parsedData,
        total: "",
      };

      return orderDataCache.current as Partial<OrderEntity>;
    }

    // diff order entry
    const item = diffOrderEntry(prevOrderData.current, parsedData);

    // console.log(prevOrderData.current, symbolOrOrder, item);
    // console.log(item);

    if (!item) {
      return orderDataCache.current as Partial<OrderEntity>;
    }

    // set the field dirty
    if (typeof parsedData.order_price !== "undefined") {
      fieldDirty.current.order_price = true;
    }
    if (typeof parsedData.order_quantity !== "undefined") {
      fieldDirty.current.order_quantity = true;
    }

    const values = calculate(parsedData, item.key as any, item.value);

    values.isStopOrder = values.order_type?.startsWith("STOP") || false;

    values.total = values.total || "";

    prevOrderData.current = parsedData;
    orderDataCache.current = values;

    return values;
  }, [parsedData, markPrice]);

  /// validator order info
  useEffect(() => {
    if (!markPrice || symbolInfo.isNil) return;
    // validate order data;
    validator(formattedOrder)
      ?.then((err) => {
        setErrors(err);
      })
      .catch((err) => {});
  }, [
    formattedOrder.order_quantity,
    formattedOrder.total,
    formattedOrder.trigger_price,
    formattedOrder.order_type,
    markPrice,
  ]);

  //====== update orderbook ask0/bid0 ======
  useEffect(() => {
    if (isNewVersion) {
      if (!optionsValue?.watchOrderbook) {
        throw new SDKError(
          "In order to calculate the estimated liquidation price, the `options.watchOrderbook` parameter must be set to true.",
        );
      }
    } else {
      if (!optionsValue?.watchOrderbook) {
        return;
      }
    }

    ee.on("orderbook:update", onOrderBookUpdate);

    return () => {
      ee.off("orderbook:update", onOrderBookUpdate);
    };
  }, [optionsValue?.watchOrderbook]);

  useEffect(() => {
    askAndBid.current = [];
  }, [parsedData?.symbol]);

  //====== end ======

  const getPriceAndQty = (
    symbolOrOrder: OrderEntity,
  ): { quantity: number; price: number } | null => {
    let quantity = Number(symbolOrOrder.order_quantity);
    const orderPrice = Number(symbolOrOrder.order_price);

    if (isNaN(quantity) || quantity <= 0) {
      return null;
    }

    if (!!options?.watchOrderbook && askAndBid.current.length === 0) {
      throw new SDKError(
        "Please check if you are using the `useOrderbookStream` hook or if the orderBook has data.",
      );
    }

    if (
      (symbolOrOrder.order_type === OrderType.LIMIT ||
        symbolOrOrder.order_type === OrderType.STOP_LIMIT) &&
      isNaN(orderPrice)
    )
      return null;

    /**
     * price
     * if order_type = market order,
     order side = long, then order_price_i = ask0
     order side = short, then order_price_i = bid0
     if order_type = limit order
     order side = long
     limit_price >= ask0, then order_price_i = ask0
     limit_price < ask0, then order_price_i = limit_price
     order side = short
     limit_price <= bid0, then order_price_i = bid0
     limit_price > ask0, then order_price_i = ask0
     */
    let price: number | undefined;

    if (
      symbolOrOrder.order_type === OrderType.MARKET ||
      symbolOrOrder.order_type === OrderType.STOP_MARKET
    ) {
      if (symbolOrOrder.side === OrderSide.BUY) {
        price = askAndBid.current[0];
      } else {
        price = askAndBid.current[1];
      }
    } else {
      // LIMIT order
      if (symbolOrOrder.side === OrderSide.BUY) {
        if (orderPrice >= askAndBid.current[0]) {
          price = askAndBid.current[0];
        } else {
          price = orderPrice;
        }
      } else {
        if (orderPrice <= askAndBid.current[1]) {
          price = askAndBid.current[1];
        } else {
          price = orderPrice;
        }
      }
    }

    if (symbolOrOrder.side === OrderSide.SELL) {
      quantity = -quantity;
    }

    return { price, quantity };
  };

  const estLiqPrice = useMemo(() => {
    if (!accountInfo || !parsedData || !markPrice) return null;

    const result = getPriceAndQty(formattedOrder as OrderEntity);
    if (result === null) return null;
    const { price, quantity } = result;
    if (!price || !quantity) return null;

    const orderFee = orderUtils.orderFee({
      qty: quantity,
      price,
      futuresTakeFeeRate: Number(accountInfo["futures_taker_fee_rate"]) / 10000,
    });

    const liqPrice = orderUtils.estLiqPrice({
      markPrice,
      baseIMR,
      baseMMR,
      totalCollateral,
      positions: positions == null ? [] : positions,
      IMR_Factor: accountInfo["imr_factor"][symbol],
      orderFee,
      newOrder: {
        qty: quantity,
        price,
        symbol: parsedData.symbol!,
      },
    });

    // console.log("********", liqPrice, markPrice, totalCollateral, result);

    if (liqPrice <= 0) return null;

    return liqPrice;
  }, [
    markPrice,
    baseIMR,
    baseMMR,
    totalCollateral,
    formattedOrder?.order_price,
    formattedOrder?.order_quantity,
    formattedOrder?.total,
    formattedOrder?.trigger_price,
    accountInfo,
  ]);

  const estLeverage = useMemo(() => {
    if (!accountInfo || !parsedData) return null;
    const result = getPriceAndQty(formattedOrder as OrderEntity);
    if (result === null || !result.price || !result.quantity) return null;

    const leverage = orderUtils.estLeverage({
      totalCollateral,
      positions: positions === null ? [] : positions,
      newOrder: {
        symbol: parsedData.symbol,
        qty: result.quantity,
        price: result.price,
      },
    });

    return leverage;
  }, [
    baseIMR,
    baseMMR,
    totalCollateral,
    positions,
    formattedOrder?.order_price,
    formattedOrder?.order_quantity,
    formattedOrder?.total,
    formattedOrder?.trigger_price,
  ]);

  return {
    maxQty,
    freeCollateral,
    markPrice,
    onSubmit,
    submit,
    submitting: isMutating,
    formattedOrder,
    // errors,
    estLiqPrice,
    estLeverage,
    helper: {
      //@ts-ignore
      calculate,
      validator,
      // clearErrors,
    },
    metaState: {
      dirty: fieldDirty.current,
      submitted: submitted.current,
      errors,
    },
    symbolConfig: symbolInfo[symbol](),
  };
}
