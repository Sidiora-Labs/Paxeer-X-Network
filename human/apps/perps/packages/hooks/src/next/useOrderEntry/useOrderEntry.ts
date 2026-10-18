import { useEffect, useMemo, useState } from "react";
import {
  OrderSide,
  OrderType,
  MarginMode,
  type API,
  type PaxeerNetworkOrder,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import type { OrderValidationResult } from "../../services/orderCreator/interface";
import { useOrderStore, type FullOrderState } from "./orderEntry.store";
import { useMemoizedFn } from "../../shared/useMemoizedFn";
import { useNativeOrderEntry } from "../../native/useNativeOrderEntry";
import { useSyncExternalStore } from "react";
import {
  getNativeSnapshot,
  subscribeNativeSnapshot,
} from "@paxeer-network/paxeer-backend/runtime";
import {
  nativeSymbols,
  toNativeSymbol,
} from "@paxeer-network/paxeer-backend/nativeMetadata";
type Options = { initialOrder?: Record<string, any>; [key: string]: any };
export type SubmitOrderOptions = {
  resetOnSuccess?: boolean;
  /**
   * Server-configured USDC borrow limit (`negative_usdc_threshold`). When
   * omitted or unavailable (loading/failed request), the guard falls back to
   * DEFAULT_USDC_BORROW_LIMIT (50,000) instead of being skipped.
   */
  usdcBorrowLimit?: number;
};

export type OrderEntryReturn = {
  submit: (options?: SubmitOrderOptions) => Promise<{
    success: boolean;
    data: Record<string, any>;
    timestamp: number;
  }>;
  reset: () => void;
  resetErrors: () => void;
  resetMetaState: () => void;
  formattedOrder: Partial<FullOrderState>;
  maxQty: number;
  maxQtys: {
    maxBuy: number;
    maxSell: number;
  };
  /**
   * The estimated liquidation price.
   */
  estLiqPrice: number | null;
  /**
   * Current position quantity for the symbol (signed: positive=Long, negative=Short).
   */
  currentPosition: number;
  /**
   * The estimated liquidation price distance.
   */
  estLiqPriceDistance: number | null;
  /**
   * The estimated leverage after order creation.
   */
  estLeverage: number | null;
  estSlippage: number | null;
  helper: {
    /**
     * @deprecated Use `validate` instead.
     */
    validator: () => Promise<PaxeerNetworkOrder>;
    /**
     * Function to validate the order.
     * @returns {Promise<OrderValidationResult | null>} The validation result.
     */
    validate: (otherErrors?: OrderValidationResult) => Promise<PaxeerNetworkOrder>;
    getProjectedUSDCBorrow: (order: Partial<PaxeerNetworkOrder>) => number | null;
  };
  freeCollateral: number;
  /**
   * set a single value to the order data;
   * @param key
   * @param value
   * @returns
   */
  setValue: (
    key: keyof FullOrderState,
    value: any,
    options?: {
      shouldUpdateLastChangedField?: boolean;
    },
  ) => void;
  setValues: (
    values: Partial<FullOrderState> | Record<string, unknown>,
  ) => void;
  /**
   * Raw merge setter for externally computed bundles (e.g. Advanced TPSL).
   *
   * Unlike `setValues`, this intentionally skips `calculate()` to avoid overwriting computed TPSL fields.
   */
  setValuesRaw: (
    values: Partial<FullOrderState> | Record<string, unknown>,
  ) => void;
  symbolInfo: API.SymbolExt;
  /**
   * Meta state including validation and submission status.
   */
  metaState: {
    dirty: { [K in keyof PaxeerNetworkOrder]?: boolean };
    submitted: boolean;
    validated: boolean;
    errors: OrderValidationResult | null;
  };
  /**
   * Indicates if a mutation (order creation) is in progress.
   */
  isMutating: boolean;

  markPrice?: number;
  symbolLeverage?: number;
};

/**
 * Custom hook for managing order entry in the paxeer-network application.
 *
 * @param {string} symbol - The symbol for which the order is being created. This parameter is required.
 *
 * @param {Options} options - Additional options for configuring the order entry.
 *
 * @returns {OrderEntryReturn} An object containing various actions and state related to order entry.
 *
 * @throws {Error} Throws an error if the symbol is not provided or is not a string.
 *
 * @example
 * ```typescript
 * const {
 *   submit,
 *   formattedOrder, //
 *   setValue,
 *   setValues,
 *   symbolInfo,
 *   metaState,
 *   isMutating,
 *  // maxQty, freeCollateral ... same as v1
 * } = useOrderEntry('BTC_USDC_PERP', options);
 *
 * // update the order type
 * setValue('order_type', OrderType.LIMIT);
 * // update the order price
 * setValue('order_price', '70000');
 * // update the order quantity
 * setValue('order_quantity', 1);
 *
 * // how to set TP/SL
 * setValue('tp_trigger_price', '71000'); // directly set TP trigger price
 * // or set the tp pnl, the TP trigger price will be calculated based on the current order price
 * // setValue('tp_pnl', '300'); // you can also set tp_offset or tp_offset_percentage, same as the usage of useTPSL hook;
 * // SL is similar to TP setting
 * setValue('sl_price', '69000');
 *
 * // Submit the order data to the backend, and reset the hook state after the order is successfully created.
 * // Note: If the order data is invalid, an error will be thrown.
 * // If you want to retain the current order data after a successful order creation,
 * // you can pass {resetOnSuccess: false} to `submit` function to prevent the hook from automatically resetting the order status.
 * // Of course, you can also call `reset()` to manually reset the order status and use `resetMetaState()` to clear the error state.
 * await submit();
 * ```
 */
const useOrderEntry = (
  symbol: string,
  options: Options = {},
): OrderEntryReturn => {
  const entry = useOrderStore((state) => state.entry);
  const actions = useOrderStore((state) => state.actions);
  const native = useSyncExternalStore(
    subscribeNativeSnapshot,
    getNativeSnapshot,
    getNativeSnapshot,
  );
  const [meta, setMeta] = useState<OrderEntryReturn["metaState"]>({
    dirty: {},
    submitted: false,
    validated: false,
    errors: null,
  });
  useEffect(() => {
    actions.initOrder(symbol, {
      side: options.initialOrder?.side,
      order_type: options.initialOrder?.order_type,
      margin_mode: options.initialOrder?.margin_mode,
    });
  }, [symbol]);
  const formattedOrder = {
    ...entry,
    symbol,
    tp_trigger_price: entry.tp_trigger_price || undefined,
    sl_trigger_price: entry.sl_trigger_price || undefined,
    tp_order_price: entry.tp_order_price || undefined,
    sl_order_price: entry.sl_order_price || undefined,
  };
  const info = nativeSymbols().find((row) => row.symbol === symbol);
  const markPrice = native.markets.find(
    (row) => row.symbol === toNativeSymbol(symbol),
  )?.price;
  const price = Number(entry.order_price) || markPrice;
  const symbolInfo = {
    ...info,
    base: toNativeSymbol(symbol),
    quote: "USDC",
    base_dp: 8,
    quote_dp: 6,
  } as unknown as API.SymbolExt;
  const currentPosition = native.positions
    .filter((row) => Number(row.marketId) === Number(info?.marketId))
    .reduce(
      (total, row) =>
        total +
        (Number(row.sizeUsd) / Number(row.entryPrice)) * (row.isLong ? 1 : -1),
      0,
    );
  const reset = () => {
    actions.initOrder(symbol, {
      side: entry.side,
      order_type: entry.order_type,
      margin_mode: entry.margin_mode,
    });
    setMeta({ dirty: {}, submitted: false, validated: false, errors: null });
  };
  const nativeEntry = useNativeOrderEntry(
    formattedOrder,
    native.leverage[symbol],
    reset,
  );
  const merge = (
    values: Partial<FullOrderState> | Record<string, any>,
    calculate = true,
  ) => {
    const next = { ...entry, ...values, symbol };
    if (calculate) {
      const p = Number(
        next.order_type === OrderType.MARKET ? markPrice : next.order_price,
      );
      if (p > 0) {
        if ("total" in values && !("order_quantity" in values))
          next.order_quantity = new Decimal(String(next.total || 0))
            .div(p)
            .toFixed(8);
        else if (
          "order_quantity" in values ||
          "order_price" in values ||
          "order_type" in values
        )
          next.total = next.order_quantity
            ? new Decimal(String(next.order_quantity)).mul(p).toFixed(6)
            : "";
      }
    }
    actions.updateOrder(next);
    setMeta((state) => ({
      ...state,
      dirty: {
        ...state.dirty,
        ...Object.fromEntries(Object.keys(values).map((key) => [key, true])),
      },
      errors: null,
    }));
  };
  const validate = async () => {
    await nativeEntry.helper.validate();
    setMeta((state) => ({ ...state, validated: true, errors: null }));
    return formattedOrder as PaxeerNetworkOrder;
  };
  const submit: OrderEntryReturn["submit"] = async (opts) => {
    setMeta((state) => ({ ...state, submitted: true }));
    await validate();
    return nativeEntry.submit(opts);
  };
  const setValue = useMemoizedFn((key: keyof FullOrderState, value: any) =>
    merge({ [key]: value }),
  );
  const setValues = useMemoizedFn(
    (values: Partial<FullOrderState> | Record<string, unknown>) =>
      merge(values),
  );
  const setValuesRaw = useMemoizedFn(
    (values: Partial<FullOrderState> | Record<string, unknown>) =>
      merge(values, false),
  );
  return {
    ...nativeEntry,
    submit,
    reset,
    resetErrors: () => setMeta((state) => ({ ...state, errors: null })),
    resetMetaState: () =>
      setMeta({ dirty: {}, submitted: false, validated: false, errors: null }),
    formattedOrder,
    currentPosition,
    helper: {
      validate,
      validator: validate,
      getProjectedUSDCBorrow: () => null,
    },
    setValue,
    setValues,
    setValuesRaw,
    symbolInfo,
    metaState: meta,
    markPrice,
  };
};
export { useOrderEntry };
