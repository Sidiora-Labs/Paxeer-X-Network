import { useCallback, useEffect, useState, useMemo } from "react";
import { compose, head } from "ramda";
import {
  type API,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  MarginMode,
} from "@paxeer-network/types";
import { priceToROI } from "../../paxeer-network/useTakeProfitAndStopLoss/tp_slUtils";
import { OrderCreator } from "../../services/orderCreator/interface";
import {
  baseInputHandle,
  getCalculateHandler,
} from "../../utils/orderEntryHelper";
import { hasTPSL } from "./helper";
import { type FullOrderState } from "./orderEntry.store";

const initialOrderState = {
  order_price: "",
  order_quantity: "",
  trigger_price: "",
  tp_trigger_price: "",
  sl_trigger_price: "",
  total: "",
  symbol: "",
  side: OrderSide.BUY,
  order_type: OrderType.LIMIT,
  margin_mode: MarginMode.CROSS,
};

const normalizeInitialOrder = (
  order?: Partial<FullOrderState>,
): Omit<Partial<FullOrderState>, "symbol"> | undefined => {
  if (!order) {
    return undefined;
  }

  const rest = { ...order };
  delete rest.symbol;
  return rest;
};

const useOrderEntryNextInternal = (
  symbol: string,
  options: {
    /**
     * initial order state, default is buy limit order
     *
     */
    initialOrder?: Omit<Partial<FullOrderState>, "symbol">;
    symbolInfo?: API.SymbolExt;
    symbolLeverage?: number;
  } = {},
) => {
  const { symbolInfo, symbolLeverage } = options;
  const initialOrder = normalizeInitialOrder(options.initialOrder);

  // Use local state instead of global zustand store to avoid state pollution
  // between different instances (e.g., OrderEntry form and TPSL edit modal)
  const [orderEntity, setOrderEntity] = useState<FullOrderState>(
    () =>
      ({
        ...initialOrderState,
        ...initialOrder,
        symbol,
      }) as FullOrderState,
  );

  // Local actions that work with local state
  const actions = useMemo(
    () => ({
      initOrder: (
        sym: string,
        opts?: {
          side?: OrderSide;
          order_type?: OrderType;
          margin_mode?: MarginMode;
        },
      ) => {
        setOrderEntity({
          ...initialOrderState,
          symbol: sym,
          side: opts?.side ?? OrderSide.BUY,
          order_type: opts?.order_type ?? OrderType.LIMIT,
          margin_mode: opts?.margin_mode ?? MarginMode.CROSS,
        } as FullOrderState);
      },
      updateOrder: (order: Partial<FullOrderState>) => {
        setOrderEntity((prev) => ({ ...prev, ...order, symbol }));
      },
      updateOrderByKey: <K extends keyof FullOrderState>(
        key: K,
        value: FullOrderState[K],
      ) => {
        setOrderEntity((prev) => ({
          ...prev,
          [key]: key === "symbol" ? symbol : value,
          symbol,
        }));
      },
      resetOrder: (_order?: Partial<FullOrderState>) => {
        setOrderEntity((prev) => ({
          ...prev,
          order_price: "",
          order_quantity: "",
          trigger_price: "",
          total: "",
          tp_trigger_price: "",
          tp_pnl: "",
          tp_offset: "",
          tp_offset_percentage: "",
          tp_offset_from_mark: "",
          tp_offset_percentage_from_mark: "",
          sl_trigger_price: "",
          sl_pnl: "",
          sl_offset: "",
          sl_offset_percentage: "",
          sl_offset_from_mark: "",
          sl_offset_percentage_from_mark: "",
        }));
      },
      hasTP_SL: () => {
        const order = orderEntity;
        return (
          typeof order.tp_trigger_price !== "undefined" ||
          typeof order.sl_trigger_price !== "undefined"
        );
      },
    }),
    [orderEntity, symbol],
  );

  // Initialize order when symbol changes
  useEffect(() => {
    actions.initOrder(symbol, initialOrder);
    if (initialOrder) {
      actions.updateOrder({
        ...initialOrder,
        symbol,
      });
    }
  }, [symbol]);

  // console.log("orderEntity", orderEntity);

  const calculate = useCallback(
    (
      values: Partial<FullOrderState>,
      fieldName: keyof FullOrderState,
      value: any,
      markPrice: number,
      config: API.SymbolExt,
    ): Partial<FullOrderState> => {
      // console.log("calculate", values, fieldName, value, options);
      const fieldHandler = getCalculateHandler(fieldName);

      const newValues = compose<any, any, any, Partial<FullOrderState>>(
        head,
        // orderEntityFormatHandle(baseDP, quoteDP),
        fieldHandler,
        baseInputHandle,
      )([values, fieldName, value, markPrice, config]);

      // if fieldName is quantity/price,recalculate the tp/sl

      return newValues as Partial<FullOrderState>;
    },
    [],
  );

  const setValue = (
    key: keyof FullOrderState,
    value: any,
    additional?: {
      markPrice: number;
    },
  ) => {
    if (!symbolInfo) {
      actions.updateOrderByKey(key, value);
      console.warn("[PAXEER_NETWORK]:symbolInfo is required to calculate the order");
      return;
    }

    /** Use local state directly to avoid stale closure. */
    const currentEntry = orderEntity;
    const { markPrice } = additional ?? { markPrice: 0 };

    let newValues = calculate(
      { ...currentEntry },
      key,
      value,
      markPrice,
      symbolInfo,
    );

    /// if the order type is market or stop market, recalculate the total use mark price
    if (key === "order_type") {
      if (value === OrderType.MARKET || value === OrderType.STOP_MARKET) {
        newValues = calculate(
          newValues,
          "order_price",
          markPrice,
          markPrice,
          symbolInfo,
        );
      }
    }

    if (
      (key === "order_quantity" || key === "order_price") &&
      hasTPSL(newValues)
    ) {
      newValues = calculateTPSL(key, newValues, markPrice, symbolInfo);
    }

    const { tp_ROI, sl_ROI } = priceToROI({
      values: newValues,
      symbolLeverage,
      markPrice,
    });
    if (tp_ROI) {
      newValues.tp_ROI = tp_ROI;
    }
    if (sl_ROI) {
      newValues.sl_ROI = sl_ROI;
    }

    actions.updateOrder(newValues);

    return newValues;
  };

  const calculateTPSL = (
    key: string,
    newValues: Partial<FullOrderState>,
    markPrice: number,
    symbolInfo: API.SymbolExt,
  ) => {
    if (key === "order_price") {
      if (typeof newValues.tp_pnl !== "undefined") {
        newValues = calculate(
          newValues,
          "tp_pnl",
          newValues.tp_pnl,
          markPrice,
          symbolInfo,
        );
      }
      if (typeof newValues.sl_pnl !== "undefined") {
        newValues = calculate(
          newValues,
          "sl_pnl",
          newValues.sl_pnl,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.tp_offset_from_mark !== undefined &&
        newValues.tp_offset_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "tp_offset_from_mark",
          newValues.tp_offset_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.tp_offset_percentage_from_mark !== undefined &&
        newValues.tp_offset_percentage_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "tp_offset_percentage_from_mark",
          newValues.tp_offset_percentage_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.sl_offset_from_mark !== undefined &&
        newValues.sl_offset_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "sl_offset_from_mark",
          newValues.sl_offset_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.sl_offset_percentage_from_mark !== undefined &&
        newValues.sl_offset_percentage_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "sl_offset_percentage_from_mark",
          newValues.sl_offset_percentage_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
    } else {
      if (typeof newValues.tp_trigger_price !== "undefined") {
        newValues = calculate(
          newValues,
          "tp_trigger_price",
          newValues.tp_trigger_price,
          markPrice,
          symbolInfo,
        );
      }

      if (typeof newValues.sl_trigger_price !== "undefined") {
        newValues = calculate(
          newValues,
          "sl_trigger_price",
          newValues.sl_trigger_price,
          markPrice,
          symbolInfo,
        );
      }

      // Re-derive trigger from mark-based offsets when qty changes (same as trigger recompute path).
      if (
        newValues.tp_offset_from_mark !== undefined &&
        newValues.tp_offset_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "tp_offset_from_mark",
          newValues.tp_offset_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.tp_offset_percentage_from_mark !== undefined &&
        newValues.tp_offset_percentage_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "tp_offset_percentage_from_mark",
          newValues.tp_offset_percentage_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.sl_offset_from_mark !== undefined &&
        newValues.sl_offset_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "sl_offset_from_mark",
          newValues.sl_offset_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
      if (
        newValues.sl_offset_percentage_from_mark !== undefined &&
        newValues.sl_offset_percentage_from_mark !== ""
      ) {
        newValues = calculate(
          newValues,
          "sl_offset_percentage_from_mark",
          newValues.sl_offset_percentage_from_mark!,
          markPrice,
          symbolInfo,
        );
      }
    }

    return newValues;
  };

  const setValues = (
    values: Partial<FullOrderState>,
    additional?: {
      markPrice: number;
    },
  ) => {
    if (!symbolInfo) {
      actions.updateOrder(values);
      console.warn("[PAXEER_NETWORK]:symbolInfo is required to calculate the order");
      return;
    }

    /** Use local state directly to avoid stale closure. */
    let newValues: Partial<FullOrderState> = { ...orderEntity };

    Object.keys(values).forEach((key) => {
      newValues = calculate(
        newValues,
        key as keyof FullOrderState,
        values[key as keyof FullOrderState],
        additional?.markPrice ?? 0,
        symbolInfo!,
      );
    });

    actions.updateOrder(newValues);

    return newValues;
  };

  /**
   * Raw merge setter for externally computed TPSL bundles.
   *
   * IMPORTANT: This intentionally skips `calculate()` to avoid overwriting user/advanced computed
   * fields (e.g. `sl_trigger_price`) via chained recomputation from other derived inputs.
   */
  const setValuesRaw = (values: Partial<FullOrderState>) => {
    if (!symbolInfo) {
      actions.updateOrder(values);
      // Raw merge path still updates store, but derived calculations are skipped when `symbolInfo` is missing.
      console.warn(
        "[PAXEER_NETWORK]:symbolInfo missing; skipping derived order calculations",
      );
      return;
    }

    /** Use local state directly to avoid stale closure. */
    const newValues: Partial<FullOrderState> = {
      ...orderEntity,
      ...values,
    };

    actions.updateOrder(newValues);

    return newValues;
  };

  const onMarkPriceUpdated = useCallback(
    (markPrice: number, baseOn: string[] = []) => {
      if (!options.symbolInfo) return;
      /** Use local state directly to avoid stale closure. */
      let newValues: Partial<FullOrderState> = { ...orderEntity };

      if (baseOn.length === 0) {
        newValues = calculate(
          { ...orderEntity },
          "order_price",
          markPrice,
          markPrice,
          options.symbolInfo!,
        );
      } else {
        baseOn.forEach((key) => {
          newValues = calculate(
            { ...newValues },
            key as keyof FullOrderState,
            orderEntity[key as keyof FullOrderState],
            markPrice,
            options.symbolInfo!,
          );
        });
      }

      if (hasTPSL(newValues)) {
        const { tp_ROI, sl_ROI } = priceToROI({
          values: newValues,
          symbolLeverage,
          markPrice,
        });
        if (tp_ROI) {
          newValues.tp_ROI = tp_ROI;
        }
        if (sl_ROI) {
          newValues.sl_ROI = sl_ROI;
        }
      }

      actions.updateOrder(newValues);
    },
    [actions, calculate, options.symbolInfo, symbolLeverage, orderEntity],
  );

  const validate = (
    order: Partial<PaxeerNetworkOrder>,
    creator: OrderCreator<any>,
    options: {
      maxQty: number;
      markPrice: number;
      estSlippage?: number | null;
      askAndBid?: number[];
    },
  ) => {
    return creator?.validate(order, {
      ...options,
      symbol: symbolInfo!,
    });
  };

  const generateOrder = (
    creator: OrderCreator<any>,
    options: {
      maxQty: number;
      markPrice: number;
      estSlippage?: number | null;
      askAndBid?: number[];
    },
  ) => {
    const order = creator.create(
      {
        ...orderEntity,
        symbol,
      },
      {
        ...options,
        symbol: symbolInfo!,
      },
    );

    return order;
  };

  const submitOrder = useCallback(() => {
    // const order = OrderFactory.create(values.type);
    // order.submit(values);
  }, [orderEntity]);

  const resetOrder = (order?: Partial<PaxeerNetworkOrder>) => {
    actions.resetOrder(order);
  };

  return {
    formattedOrder: orderEntity,
    setValue,
    setValues,
    setValuesRaw,
    submit: submitOrder,
    reset: resetOrder,
    generateOrder,
    validate,
    onMarkPriceChange: onMarkPriceUpdated,
  } as const;
};

export { useOrderEntryNextInternal };
