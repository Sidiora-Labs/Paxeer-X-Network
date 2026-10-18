import { useEffect, useMemo, useRef } from "react";
import {
  useAccount,
  useComputedLTV,
  useConvertThreshold,
  useEventEmitter,
  useLocalStorage,
  useMarginModeBySymbol,
  useMarginRatio,
  useMemoizedFn,
  useOrderEntry,
  usePaxeerNetworkContext,
  useTpslPriceChecker,
  normalizeUSDCBorrowLimit,
} from "@paxeer-network/hooks";
import { useCanTrade } from "@paxeer-network/react-app";
import {
  DistributionType,
  OrderLevel,
  OrderSide,
  OrderType,
  PositionType,
  ORDER_ENTRY_EST_LIQ_PRICE_CHANGE,
} from "@paxeer-network/types";
import { Decimal, removeTrailingZeros } from "@paxeer-network/utils";
import { useAskAndBid } from "./hooks/useAskAndBid";
import { useBBOState } from "./hooks/useBBOState";
import { useFocusAndBlur } from "./hooks/useFocusAndBlur";
import { usePriceInputContainer } from "./hooks/usePriceInputContainer";
import { InputType } from "./types";
import { BBOStatus, isBBOOrder, safeNumber } from "./utils";

export type OrderEntryScriptInputs = {
  symbol: string;
};

export const PAXEER_NETWORK_ORDER_SOUND_ALERT_KEY = "paxeerNetwork_order_sound_alert";
export const PAXEER_NETWORK_ORDER_SOUND_OPTION_KEY = "paxeerNetwork_order_sound_option";

export type OrderEntryScriptReturn = ReturnType<typeof useOrderEntryScript>;

const ORDER_ENTRY_EST_LIQ_ACTIVE_WINDOW_MS = 3 * 1000;

export const useOrderEntryScript = (inputs: OrderEntryScriptInputs) => {
  /** Active user window for treating estLiqPrice as coming from an actively edited order. */
  const { symbol } = inputs;
  const [localOrderType, setLocalOrderType] = useLocalStorage(
    "paxeer-network-order-entry-order-type",
    OrderType.LIMIT,
  );
  const [localOrderSide, setLocalOrderSide] = useLocalStorage(
    "paxeer-network-order-entry-order-side",
    OrderSide.BUY,
  );

  const { notification } = usePaxeerNetworkContext();

  const orderFilledConfig = notification?.orderFilled;
  const defaultSoundValue =
    orderFilledConfig?.defaultSoundValue ??
    orderFilledConfig?.soundOptions?.[0]?.value;

  const [soundAlert, setSoundAlert] = useLocalStorage<boolean>(
    PAXEER_NETWORK_ORDER_SOUND_ALERT_KEY,
    orderFilledConfig?.defaultOpen ?? false,
  );

  const [initialSoundValue] = useLocalStorage<string | null>(
    PAXEER_NETWORK_ORDER_SOUND_OPTION_KEY,
    defaultSoundValue ?? null,
  );
  void initialSoundValue;

  const canTrade = useCanTrade();
  const { state: accountState } = useAccount();
  const { marginMode, isPermissionlessListing } = useMarginModeBySymbol(symbol);
  const walletAddress = accountState?.address;

  const {
    formattedOrder,
    setValue,
    setValues: setOrderValues,
    setValuesRaw: setOrderValuesRaw,
    symbolInfo,
    symbolLeverage,
    ...state
  } = useOrderEntry(symbol, {
    initialOrder: {
      symbol,
      order_type: localOrderType,
      position_type: PositionType.PARTIAL,
      side: localOrderSide,
      margin_mode: marginMode,
    },
  });

  const [tpslSwitch, setTpslSwitch] = useLocalStorage(
    "paxeer-network-order-entry-tp_sl-switch",
    false,
  );

  const { currentLeverage } = useMarginRatio();
  const ee = useEventEmitter();
  const triggerPriceInputRef = useRef<HTMLInputElement | null>(null);
  const priceInputRef = useRef<HTMLInputElement | null>(null);
  const activatedPriceInputRef = useRef<HTMLInputElement | null>(null);
  /** Tracks last time the user interacted with core order inputs (used to gate estLiqPrice emission). */
  const lastUserActiveTimeRef = useRef<number>(Date.now());

  const { bboStatus, bboType, setBBOType, onBBOChange, toggleBBO } =
    useBBOState({
      tpslSwitch,
      order_type: formattedOrder.order_type,
      order_type_ext: formattedOrder.order_type_ext,
      side: formattedOrder.side,
      setOrderValues,
    });

  const {
    currentFocusInput,
    lastScaledOrderPriceInput,
    lastQuantityInputType,
    onFocus,
    onBlur,
  } = useFocusAndBlur({
    base_tick: symbolInfo?.base_tick,
    order_type: formattedOrder.order_type,
    order_quantity: formattedOrder.order_quantity,
    setValue,
  });

  // cancel TP/SL
  const cancelTP_SL = () => {
    // if(formattedOrder.)
    setOrderValues({
      tp_trigger_price: "",
      sl_trigger_price: "",
      position_type: PositionType.PARTIAL,
    });
  };

  const enableTP_SL = () => {
    setOrderValues({
      order_type_ext: undefined,
      position_type: PositionType.PARTIAL,
    });
  };

  const setOrderValue = useMemoizedFn(
    (
      key: keyof typeof formattedOrder | string,
      value: unknown,
      options?: {
        shouldUpdateLastChangedField?: boolean;
      },
    ) => {
      if (key === "order_type") {
        setLocalOrderType(value as OrderType);
      }
      if (key === "side") {
        setLocalOrderSide(value as OrderSide);
      }

      if (
        (key === "reduce_only" && value) ||
        (key === "order_type" &&
          (value === OrderType.STOP_LIMIT || value === OrderType.STOP_MARKET))
      ) {
        // cancelTP_SL();

        const data: Record<string, unknown> = {
          tp_trigger_price: "",
          sl_trigger_price: "",
          [key]: value,
        };

        if (key === "order_type") {
          (data as Record<string, unknown>)["order_type_ext"] = "";
        }

        setOrderValues(data as Partial<typeof formattedOrder>);

        return;
      }

      if (key === "order_type" && value !== OrderType.LIMIT) {
        const data: Record<string, unknown> = {
          level: undefined,
          order_type_ext: undefined,
          [key]: value,
        };

        setOrderValues(data as Partial<typeof formattedOrder>);

        return;
      }

      if (key === "order_type" && value === OrderType.SCALED) {
        const data: Record<string, unknown> = {
          distribution_type: DistributionType.FLAT,
          [key]: value,
        };
        setOrderValues(data as Partial<typeof formattedOrder>);
        return;
      }

      setValue(
        key as keyof typeof formattedOrder,
        value as (typeof formattedOrder)[keyof typeof formattedOrder],
        options,
      );
    },
  );

  const manualSetOrderValue = useMemoizedFn(
    (
      key: keyof typeof formattedOrder | string,
      value: unknown,
      options?: {
        shouldUpdateLastChangedField?: boolean;
      },
    ) => {
      // Manually triggered updates should mark the user as active for estLiqPrice window calculations.
      lastUserActiveTimeRef.current = Date.now();
      console.log("manualSetOrderValue----", key, value, options);
      setOrderValue(key, value, options);
    },
  );

  const onTPSLSwitchChanged = (state: boolean) => {
    setTpslSwitch(state);
    if (state) {
      enableTP_SL();
    } else {
      cancelTP_SL();
    }
  };

  useEffect(() => {
    const updateOrderPrice = (price: string) => {
      setValue("order_price", price);
    };
    ee.on("update:orderPrice", updateOrderPrice);

    return () => {
      ee.off("update:orderPrice", updateOrderPrice);
    };
  }, []);

  useEffect(() => {
    const focusInputElement = (target: HTMLInputElement | null) => {
      requestAnimationFrame(() => {
        target?.focus();
      });
    };

    // handle orderbook item click event
    const orderBookItemClickHandler = (item: number[]) => {
      const price = removeTrailingZeros(item[0]);
      const { order_type, order_type_ext } = formattedOrder;

      // handle trigger price input, focus on trigger price input
      if (
        currentFocusInput.current === InputType.TRIGGER_PRICE &&
        (order_type === OrderType.STOP_LIMIT ||
          order_type === OrderType.STOP_MARKET)
      ) {
        setValue("trigger_price", price);
        focusInputElement(triggerPriceInputRef.current);
        return;
      }

      // handle bbo order, unselect bbo and set order price, focus on order price input
      if (isBBOOrder({ order_type, order_type_ext })) {
        setBBOType(undefined);

        setOrderValues({
          order_type_ext: undefined,
          level: undefined,
        });

        requestAnimationFrame(() => {
          // Since BBO will update the price when unselected, we should set order price in requestAnimationFrame
          // We can't call setValue directly here because it's inside a requestAnimationFrame, and the formattedOrder accessed inside setValue would be the old value
          // setValue("order_price", price);
          ee.emit("update:orderPrice", price);
        });

        focusInputElement(priceInputRef.current);
        return;
      }

      // handle limit order and stop limit order, set order price and focus on order price input
      if (
        order_type === OrderType.STOP_LIMIT ||
        order_type === OrderType.LIMIT
      ) {
        setValue("order_price", price);
        focusInputElement(priceInputRef.current);
        return;
      }

      // handle stop market order, set trigger price and focus on trigger price input
      if (order_type === OrderType.STOP_MARKET) {
        setValue("trigger_price", price);
        focusInputElement(triggerPriceInputRef.current);
        return;
      }

      // handle market order, set order type to limit
      if (order_type === OrderType.MARKET) {
        // unselect bbo
        setBBOType(undefined);

        // You can't call setValue twice here , the second value will override the first, so you need to combine them into a single setValues call
        setOrderValues({
          order_type: OrderType.LIMIT,
          order_price: price,
        });

        focusInputElement(priceInputRef.current);
        return;
      }

      if (
        order_type === OrderType.SCALED &&
        lastScaledOrderPriceInput.current
      ) {
        const field =
          lastScaledOrderPriceInput.current === InputType.START_PRICE
            ? "start_price"
            : "end_price";
        setValue(field, price);
        focusInputElement(priceInputRef.current);
        return;
      }

      // handle trailing stop order, set activated price and focus on activated price input
      if (order_type === OrderType.TRAILING_STOP) {
        setValue("activated_price", price);
        focusInputElement(activatedPriceInputRef.current);
        return;
      }

      // default, set order price and focus on order price input
      setValue("order_price", price);
      focusInputElement(priceInputRef.current);
    };

    ee.on("orderbook:item:click", orderBookItemClickHandler);

    return () => {
      ee.off("orderbook:item:click", orderBookItemClickHandler);
    };
    // Please do not modify this deps lightly, because `setValue` also relies on these state internally
  }, [formattedOrder, symbolInfo]);

  useEffect(() => {
    // after switching symbol, all the input number should be cleared (price, qty, TP/SL, etc)
    state.reset();
    state.resetMetaState();
    // reset last quantity input type
    lastQuantityInputType.current = InputType.NONE;
  }, [symbol]);

  // if scaled order, and distribution_type is not set, set it to flat
  useEffect(() => {
    if (
      formattedOrder.order_type === OrderType.SCALED &&
      !formattedOrder.distribution_type
    ) {
      setValue("distribution_type", DistributionType.FLAT);
    }
  }, [formattedOrder.order_type, formattedOrder.distribution_type]);

  const isSymbolPostOnly =
    (symbolInfo as { status?: string } | undefined)?.status === "POST_ONLY";

  // check if the symbol is in POST_ONLY mode,
  // and fix order type to limit if the order type is market or stop market
  useEffect(() => {
    if (
      isSymbolPostOnly &&
      (formattedOrder.order_type === OrderType.MARKET ||
        formattedOrder.order_type === OrderType.STOP_MARKET)
    ) {
      setLocalOrderType(OrderType.LIMIT);
      setOrderValues({
        order_type: OrderType.LIMIT,
        order_type_ext: undefined,
      });
    }
  }, [isSymbolPostOnly, formattedOrder.order_type, setOrderValues]);

  const currentLtv = useComputedLTV();
  const { negative_usdc_threshold } = useConvertThreshold();
  // While the threshold is loading or the request failed, fall back to the
  // default borrow limit so the guard never silently skips.
  const usdcBorrowLimit = normalizeUSDCBorrowLimit(negative_usdc_threshold);
  const askAndBid = useAskAndBid();

  const fillMiddleValue = () => {
    if (bboStatus === BBOStatus.ON) {
      toggleBBO();
    }
    if (formattedOrder.order_type === OrderType.LIMIT) {
      const [bestAsk = 0, bestBid = 0] = askAndBid;
      const midPrice = new Decimal(safeNumber(bestAsk))
        .add(safeNumber(bestBid))
        .div(2)
        .toNumber();
      // 1. Since BBO will update the price when unselected, we should set order price in raf
      // 2. raf is mainly used to solve the timing problem caused by React state update, ensuring that the orderPrice is triggered after the state is fully updated to avoid accessing expired state values.
      requestAnimationFrame(() => {
        ee.emit("update:orderPrice", midPrice);
      });
    }
  };

  const { priceInputContainerRef, priceInputContainerWidth } =
    usePriceInputContainer({
      order_type_ext: formattedOrder.order_type_ext,
    });

  /**
   * Same rules as est-liq broadcast: when estimate is missing or inconsistent with
   * mark/side, treat as no liq for SL validation. Otherwise SL vs liq checks can fire
   * while the UI shows no usable est. liq. price.
   */
  const effectiveEstLiqPriceForSlCheck = useMemo(() => {
    const estLiqPrice = state.estLiqPrice;
    if (
      estLiqPrice == null ||
      formattedOrder.side == null ||
      state.markPrice == null
    ) {
      return null;
    }
    if (
      (formattedOrder.side === OrderSide.BUY &&
        estLiqPrice > state.markPrice) ||
      (formattedOrder.side === OrderSide.SELL && estLiqPrice < state.markPrice)
    ) {
      return null;
    }
    if (!Number.isFinite(estLiqPrice) || estLiqPrice <= 0) {
      return null;
    }
    return estLiqPrice;
  }, [state.estLiqPrice, state.markPrice, formattedOrder.side]);

  const slPriceError = useTpslPriceChecker({
    slPrice: formattedOrder.sl_trigger_price,
    liqPrice: effectiveEstLiqPriceForSlCheck,
    side: formattedOrder.side,
    markPrice: state.markPrice,
    currentPosition: state.currentPosition,
    orderQuantity: Number(formattedOrder.order_quantity),
  });

  useEffect(() => {
    if (formattedOrder.reduce_only) {
      setTpslSwitch(false);
    }
  }, [formattedOrder.reduce_only]);

  useEffect(() => {
    if (tpslSwitch) {
      setOrderValue("reduce_only", false);
    }
  }, [tpslSwitch]);

  /**
   * Broadcast estimated liquidation price for TradingView chart liquidation line (avoids parent state / callback loops).
   * Includes a user-activity flag so downstream consumers can decide whether to treat this estLiqPrice as active.
   */
  useEffect(() => {
    const estLiqPrice = effectiveEstLiqPriceForSlCheck;

    const lastActive = lastUserActiveTimeRef.current;
    const now = Date.now();
    const isUserActive =
      now - lastActive <= ORDER_ENTRY_EST_LIQ_ACTIVE_WINDOW_MS;

    ee.emit(ORDER_ENTRY_EST_LIQ_PRICE_CHANGE, {
      symbol,
      estLiqPrice,
      isUserActive,
    });
  }, [ee, symbol, effectiveEstLiqPriceForSlCheck]);

  useEffect(() => {
    setOrderValue("margin_mode", marginMode);
  }, [marginMode]);

  return {
    ...state,
    slPriceError: slPriceError ?? undefined,
    side: formattedOrder.side as OrderSide,
    type: formattedOrder.order_type as OrderType,
    level: formattedOrder.level as OrderLevel,
    formattedOrder,
    setOrderValue,
    manualSetOrderValue,
    setOrderValues,
    setOrderValuesRaw,
    // account-level leverage (for other consumers)
    currentLeverage,
    // symbol-level leverage & margin mode for this order entry
    symbolLeverage,
    marginMode,

    // cancelTP_SL,
    // enableTP_SL,
    tpslSwitch,
    setTpslSwitch: onTPSLSwitchChanged,
    symbolInfo,
    onFocus,
    onBlur,

    priceInputRef,
    priceInputContainerRef,
    priceInputContainerWidth,
    triggerPriceInputRef,
    activatedPriceInputRef,
    lastQuantityInputType,

    canTrade,
    bboStatus,
    bboType,
    onBBOChange,
    toggleBBO,
    currentLtv,
    usdcBorrowLimit,
    fillMiddleValue,
    symbol,
    soundAlert,
    setSoundAlert,
    currentFocusInput,
    walletAddress,
    isPermissionlessListing,
    isSymbolPostOnly,
  };
};
