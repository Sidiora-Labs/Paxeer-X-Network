import { useCallback, useMemo, useState } from "react";
import {
  useLocalStorage,
  useMarkPrice,
  usePositionStream,
  useSubAccountMutation,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { API, MarginMode, OrderSide, OrderType } from "@paxeer-network/types";
import { useScreen } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";

export interface UseReversePositionScriptOptions {
  position: API.PositionExt | API.PositionTPSLExt;
  onSuccess?: () => void;
  onError?: (error: any) => void;
  calcMode?: "markPrice" | "lastPrice";
}

// Validation error types
export type ReversePositionValidationError = "belowMin" | null;

// Maximum number of orders per batch request
const MAX_BATCH_ORDER_SIZE = 20;

/**
 * Hook for managing reverse position enabled state
 * Separates desktop and mobile preferences internally
 */
export const useReversePositionEnabled = () => {
  const { isMobile, isDesktop } = useScreen();

  // Preference storage for desktop and mobile separately
  const [desktopEnabled, setDesktopEnabled] = useLocalStorage(
    "paxeerNetwork_reverse_position_enabled_desktop",
    true,
  );
  const [mobileEnabled, setMobileEnabled] = useLocalStorage(
    "paxeerNetwork_reverse_position_enabled_mobile",
    false,
  );

  // Get current preference based on platform
  const isEnabled = useMemo(() => {
    if (isMobile) {
      return mobileEnabled;
    }
    if (isDesktop) {
      return desktopEnabled;
    }
    return false;
  }, [isMobile, isDesktop, desktopEnabled, mobileEnabled]);

  // Set enabled state based on current platform
  const setEnabled = useCallback(
    (enabled: boolean) => {
      if (isMobile) {
        setMobileEnabled(enabled);
      } else if (isDesktop) {
        setDesktopEnabled(enabled);
      }
    },
    [isMobile, isDesktop, setMobileEnabled, setDesktopEnabled],
  );

  return {
    isEnabled,
    setEnabled,
  };
};

export const useReversePositionScript = (
  options?: UseReversePositionScriptOptions,
) => {
  const { position, onSuccess, onError } = options || {};
  const { isEnabled, setEnabled } = useReversePositionEnabled();

  // Get pnlNotionalDecimalPrecision from localStorage
  const [pnlNotionalDecimalPrecision] = useLocalStorage(
    "pnlNotionalDecimalPrecision",
    2,
  );

  const symbolsInfo = useSymbolsInfo();
  const symbol = position?.symbol || "";
  const { data: symbolMarketPrice } = useMarkPrice(symbol);
  const symbolInfo = symbolsInfo?.[symbol];

  // Get position stream data for unrealized PnL
  const calcMode = options?.calcMode || "markPrice";
  const [positionData] = usePositionStream(symbol, {
    calcMode,
    includedPendingOrder: false,
  });
  const rawPositionRows = useDataTap(positionData?.rows, { fallbackData: [] });

  // Find the current position and extract unrealized_pnl
  const unrealizedPnL = useMemo(() => {
    if (!rawPositionRows || !symbol) return position?.unrealized_pnl;
    const currentPosition = rawPositionRows.find(
      (p) => p.symbol === symbol && p.position_qty !== 0,
    );
    return currentPosition?.unrealized_pnl ?? position?.unrealized_pnl;
  }, [rawPositionRows, symbol]);

  // Get base_min and base_max from symbol info
  const baseMin = useMemo(() => {
    if (!symbolInfo) return 0;
    return symbolInfo("base_min") || 0;
  }, [symbolInfo]);

  const baseMax = useMemo(() => {
    if (!symbolInfo) return 0;
    return symbolInfo("base_max") || 0;
  }, [symbolInfo]);

  const baseDp = useMemo(() => {
    if (!symbolInfo) return 6;
    return symbolInfo("base_dp") || 6;
  }, [symbolInfo]);

  // Calculate reverse order quantity
  const positionQty = useMemo(() => {
    if (!position) return 0;
    return Math.abs(position.position_qty);
  }, [position]);

  // Determine current position side
  const isLong = useMemo(() => {
    if (!position) return false;
    return position.position_qty > 0;
  }, [position]);

  const reverseQty = positionQty;

  // Validation: check if reverse quantity is below minimum (highest priority)
  const validationError = useMemo<ReversePositionValidationError>(() => {
    if (!position || !symbolInfo) return null;
    if (baseMin > 0 && reverseQty < baseMin) {
      return "belowMin";
    }
    return null;
  }, [position, symbolInfo, reverseQty, baseMin]);

  // Calculate split orders if quantity exceeds base_max
  const splitOrders = useMemo(() => {
    if (!position || !symbolInfo || baseMax <= 0 || reverseQty <= 0) {
      return { needsSplit: false, orders: [] };
    }

    const buildOrder = (qty: number, side: OrderSide, reduceOnly: boolean) => {
      return {
        symbol: position.symbol,
        order_type: OrderType.MARKET,
        side,
        order_quantity: new Decimal(qty).todp(baseDp).toString(),
        reduce_only: reduceOnly,
        // Use position's margin_mode or default to CROSS for backward compatibility
        margin_mode: position.margin_mode || MarginMode.CROSS,
      };
    };

    const closeSide = isLong ? OrderSide.SELL : OrderSide.BUY;
    const openSide = isLong ? OrderSide.SELL : OrderSide.BUY;

    // If reverse quantity doesn't exceed base_max, no split needed
    if (reverseQty <= baseMax) {
      return {
        needsSplit: false,
        orders: [
          buildOrder(reverseQty, closeSide, true),
          buildOrder(reverseQty, openSide, false),
        ],
      };
    }

    // Calculate split orders
    const orders: {
      symbol: string;
      order_type: OrderType;
      side: OrderSide;
      order_quantity: string;
      reduce_only: boolean;
      margin_mode: MarginMode;
    }[] = [];
    const perOrderQty = baseMax;
    const numOrders = Math.ceil(reverseQty / baseMax);

    for (let i = 0; i < numOrders - 1; i++) {
      orders.push(buildOrder(perOrderQty, closeSide, true));
    }
    orders.push(
      buildOrder(reverseQty - perOrderQty * (numOrders - 1), closeSide, true),
    );
    for (let i = 0; i < numOrders - 1; i++) {
      orders.push(buildOrder(perOrderQty, openSide, false));
    }

    // Last order gets the remaining quantity
    orders.push(
      buildOrder(reverseQty - perOrderQty * (numOrders - 1), openSide, false),
    );

    return {
      needsSplit: true,
      orders,
    };
  }, [position, symbolInfo, reverseQty, baseMax, baseDp]);

  console.log("splitOrders", reverseQty, baseMax, splitOrders);

  // Local state to track the entire reverse position process
  // This is needed because when orders are split into multiple batches,
  // the mutation's isMutating only reflects the current batch
  const [isReversing, setIsReversing] = useState(false);

  // Batch order mutation - always use batch API
  const [doBatchCreateOrder] = useSubAccountMutation(
    "/v1/batch-order",
    "POST",
    {
      accountId: position?.account_id,
    },
  );

  // Reverse position logic
  const reversePosition = useCallback(async () => {
    if (!position || positionQty === 0) {
      // toast.error("No position to reverse");
      return false;
    }

    // Don't allow if validation error exists
    if (validationError) {
      return false;
    }

    setIsReversing(true);
    try {
      // INSERT_YOUR_CODE
      // Check if splitOrders.orders length > MAX_BATCH_ORDER_SIZE
      const ordersArray = splitOrders.orders;
      if (ordersArray.length > MAX_BATCH_ORDER_SIZE) {
        for (let i = 0; i < ordersArray.length; i += MAX_BATCH_ORDER_SIZE) {
          const batch = ordersArray.slice(i, i + MAX_BATCH_ORDER_SIZE);
          const result = await doBatchCreateOrder({
            orders: batch,
            symbol: position.symbol,
          });
          // because create order rate limit is 10 orders per second, so we need to wait for the next batch
          // add 10% buffer to the wait time
          await new Promise((resolve) =>
            setTimeout(resolve, batch.length * 110),
          );
          // Optional: check result and handle possible errors (stop if failed)
          if (!result || result.error) {
            throw result?.error || new Error("Batch order failed");
          }
        }
      } else {
        const result = await doBatchCreateOrder({
          orders: ordersArray,
          symbol: position.symbol,
        });
        if (!result || result.error) {
          throw result?.error || new Error("Batch order failed");
        }
      }
      onSuccess?.();

      return true;
    } catch (error: any) {
      onError?.(error);
      return false;
    } finally {
      setIsReversing(false);
    }
  }, [
    position,
    positionQty,
    reverseQty,
    isLong,
    doBatchCreateOrder,
    splitOrders,
    symbolInfo,
    validationError,
    onSuccess,
    onError,
  ]);

  // Get display information
  const displayInfo = useMemo(() => {
    if (!position || !symbolInfo) {
      return null;
    }

    const base = symbolInfo("base");
    const quote = symbolInfo("quote");
    const baseDp = symbolInfo("base_dp");
    const quoteDp = symbolInfo("quote_dp");
    const leverage = position.leverage || 1;

    return {
      symbol: position.symbol,
      base,
      quote,
      baseDp,
      quoteDp,
      positionQty: new Decimal(positionQty).todp(baseDp).toString(),
      reverseQty: new Decimal(reverseQty).todp(baseDp).toString(),
      markPrice: symbolMarketPrice
        ? new Decimal(symbolMarketPrice).todp(quoteDp).toString()
        : "--",
      leverage,
      isLong,
      unrealizedPnL,
      pnlNotionalDecimalPrecision,
    };
  }, [
    position,
    symbolMarketPrice,
    symbolInfo,
    positionQty,
    reverseQty,
    isLong,
    unrealizedPnL,
    pnlNotionalDecimalPrecision,
  ]);

  return {
    isEnabled,
    setEnabled,
    reversePosition,
    isReversing,
    displayInfo,
    positionQty,
    reverseQty,
    isLong,
    validationError,
    splitOrders,
  };
};

export type ReversePositionState = ReturnType<typeof useReversePositionScript>;
