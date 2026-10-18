import { useEffect, useMemo, useRef } from "react";
import { pathOr, omit } from "ramda";
import { type SWRConfiguration } from "swr";
import {
  AlgoOrderRootType,
  EMPTY_OBJECT,
  OrderStatus,
  type API,
} from "@paxeer-network/types";
import { useApiStatusStore } from "../../next/apiStatus/apiStatus.store";
import { CalculatorScope } from "../../types";
import { useCalculatorService } from "../../useCalculatorService";
import { createGetter } from "../../utils/createGetter";
import { useAppStore } from "../appStore";
import { PositionCalculator } from "../calculator/positions";
import { useOrderStream } from "../paxeerNetworkHooks";
import { POSITION_EMPTY, usePositionStore } from "./usePosition.store";
import { findPositionTPSLFromOrders, findTPSLFromOrder } from "./utils";

// import { usePosition } from "./usePosition";

/**
 * Price mode for PnL calculations in position streams
 * @typedef {("markPrice" | "lastPrice")} PriceMode
 * - markPrice: Uses mark price for unrealized PnL calculations (default)
 * - lastPrice: Uses last traded price (index price) for unrealized PnL calculations
 */
export type PriceMode = "markPrice" | "lastPrice";

/**
 * Calculator scopes registered for position calculations
 * These scopes enable real-time updates across position, mark price, and index price changes
 */
const scopes = [
  CalculatorScope.POSITION,
  CalculatorScope.MARK_PRICE,
  CalculatorScope.INDEX_PRICE,
];

/**
 * Real-time position stream hook with WebSocket integration
 *
 * Subscribes to position updates via WebSocket and provides real-time position data with automatic
 * calculations for unrealized PnL, ROI, and aggregated portfolio metrics. Integrates TP/SL orders
 * and supports both full portfolio view and single symbol tracking.
 *
 * **Key Features:**
 * - Real-time WebSocket updates for positions
 * - Automatic integration of TP/SL (take-profit/stop-loss) orders
 * - Dual price mode support (mark price vs last price)
 * - Optional pending order inclusion
 * - Calculator service integration for real-time PnL updates
 * - Aggregated portfolio metrics (total collateral, value, ROI)
 *
 * **Price Calculation Modes:**
 * - markPrice (default): Uses mark price for unrealized PnL - recommended for margin calculations
 * - lastPrice: Uses last traded price (index price) - useful for more conservative PnL views
 *
 * **Data Flow:**
 * 1. Subscribes to position store (WebSocket-driven)
 * 2. Fetches related TP/SL orders via useOrderStream
 * 3. Registers calculator for real-time price updates (single symbol only)
 * 4. Merges position data with TP/SL information
 * 5. Applies price mode transformations
 * 6. Filters positions based on includedPendingOrder flag
 *
 * @param {string} [symbol="all"] - Trading symbol to filter positions, or "all" for entire portfolio
 *   - "all": Returns all positions across all symbols (calculator not registered)
 *   - "BTC-PERP": Returns only BTC-PERP position (calculator registered for real-time updates)
 *
 * @param {Object} [options] - Configuration options extending SWR configuration
 * @param {PriceMode} [options.calcMode] - Price calculation mode: "markPrice" or "lastPrice"
 *   - markPrice: Uses mark_price field for unrealized_pnl (default, matches exchange calculations)
 *   - lastPrice: Uses unrealized_pnl_index field based on last traded price
 * @param {boolean} [options.includedPendingOrder=false] - Include positions with only pending orders
 *   - false: Only returns positions with non-zero position_qty
 *   - true: Returns positions with position_qty !== 0 OR pending_long_qty/pending_short_qty !== 0
 *
 * @returns {readonly [PositionData, PositionInfoGetter, LoadingState]} Tuple containing:
 *   - [0] PositionData object:
 *     - rows: Array of position objects with TP/SL information
 *     - aggregated: Aggregated metrics (total unrealized PnL, ROI, etc.)
 *     - totalCollateral: Total collateral across all positions
 *     - totalValue: Total portfolio value
 *     - totalUnrealizedROI: Total unrealized ROI percentage
 *   - [1] PositionInfoGetter: Memoized getter function for aggregated data access
 *   - [2] LoadingState object:
 *     - loading: Loading status (deprecated, use isLoading)
 *     - isLoading: Current loading state of position data
 *
 * @example
 * // Get all positions with mark price calculation
 * const [{ rows, aggregated, totalCollateral }] = usePositionStream();
 *
 * @example
 * // Get single symbol position with last price calculation
 * const [{ rows }] = usePositionStream("BTC-PERP", {
 *   calcMode: "lastPrice"
 * });
 *
 * @example
 * // Include pending orders in results
 * const [{ rows }, getter, { isLoading }] = usePositionStream("all", {
 *   includedPendingOrder: true
 * });
 *
 * @example
 * // Access specific position with TP/SL data
 * const [{ rows }] = usePositionStream("ETH-PERP");
 * const position = rows[0];
 * console.log(position.full_tp_sl.tp_trigger_price); // Full position TP price
 * console.log(position.partial_tp_sl.order_num); // Number of partial TP/SL orders
 */
export const usePositionStream = (
  symbol: string = "all",
  options?: SWRConfiguration & {
    calcMode?: PriceMode;
    includedPendingOrder?: boolean;
  },
) => {
  const { calcMode } = options || {};

  const { includedPendingOrder = false } = options || {};

  // Position calculator instance for single symbol tracking
  // Only instantiated when a specific symbol is provided (not "all")
  const positionCalculator = useRef<PositionCalculator | null>(null);
  const calculatorService = useCalculatorService();

  /**
   * Subscribe to TP/SL orders that are related to current positions
   * Filters for incomplete orders with POSITIONAL_TP_SL or TP_SL types
   * keeplive ensures WebSocket connection stays active for real-time updates
   */
  const [tpslOrders] = useOrderStream(
    {
      symbol: symbol === "all" ? undefined : symbol,
      status: OrderStatus.INCOMPLETE,
      includes: [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
      size: 500,
    },
    {
      keeplive: true,
    },
  );

  // Track API loading status for position endpoint
  const { positions: positionStatus } = useApiStatusStore(
    (state) => state.apis,
  );

  /**
   * Register position calculator for real-time updates when tracking a single symbol
   * Calculator is NOT registered for "all" mode to avoid unnecessary computations
   * Registers across three scopes: POSITION, MARK_PRICE, INDEX_PRICE
   * This enables automatic recalculation when any of these values change via WebSocket
   */
  useEffect(() => {
    if (symbol === "all") {
      return;
    }
    positionCalculator.current = new PositionCalculator(symbol);
    for (const scope of scopes) {
      calculatorService.register(scope, positionCalculator.current);
    }
    // Cleanup: unregister calculator on unmount or symbol change
    return () => {
      for (const scope of scopes) {
        calculatorService.unregister(scope, positionCalculator.current!);
      }
    };
  }, [symbol]);

  /**
   * Fetch position data from the store
   * Returns tuple: [rows array, aggregated data without rows]
   * Falls back to POSITION_EMPTY if no data exists for the symbol
   */
  const formattedPositions: [
    API.PositionTPSLExt[] | null,
    Omit<API.PositionsTPSLExt, "rows">,
  ] = usePositionStore((state) => {
    const positions = state.positions[symbol] ?? POSITION_EMPTY;

    return [positions.rows, omit(["rows"], positions)];
  });

  // Extract portfolio-level metrics from app store
  const { totalCollateral, totalValue, totalUnrealizedROI } = useAppStore(
    (state) => state.portfolio,
  );

  /**
   * Transform aggregated data based on selected price calculation mode
   * - markPrice mode: Returns data as-is (uses mark_price based fields)
   * - lastPrice mode: Remaps index-based fields to standard field names
   *   This allows consistent field access regardless of calculation mode
   */
  const aggregated = useMemo(() => {
    const data = formattedPositions[1];
    if (!data) {
      return {};
    }

    if (calcMode === "markPrice") {
      return data;
    }

    // Remap index-based PnL fields for lastPrice mode
    const { total_unreal_pnl_index, unrealPnlROI_index, ...rest } = data;

    return {
      ...rest,
      unrealPnL: total_unreal_pnl_index,
      total_unreal_pnl: total_unreal_pnl_index,
      unrealPnlROI: unrealPnlROI_index,
    };
  }, [calcMode, formattedPositions]);

  /**
   * Process and transform position rows through multiple stages:
   * 1. Filter based on pending order inclusion setting
   * 2. Apply price mode transformations (lastPrice vs markPrice)
   * 3. Merge with TP/SL order data
   */
  let rows = formattedPositions[0];
  {
    // Initialize empty array if no position data exists
    if (!rows) {
      rows = [];
    }

    /**
     * Filter positions based on includedPendingOrder flag
     * - Default (false): Only show positions with actual position quantity
     * - True: Show positions with either position_qty OR pending orders
     * This prevents empty rows from cluttering the UI
     */
    if (!includedPendingOrder) {
      rows = rows.filter((item) => item.position_qty !== 0);
    } else {
      rows = rows.filter(
        (item) =>
          item.position_qty !== 0 ||
          item.pending_long_qty !== 0 ||
          item.pending_short_qty !== 0,
      );
    }

    /**
     * Apply lastPrice mode transformations
     * Remaps index-based PnL fields to standard field names for each position
     * This ensures UI components can use consistent field names
     */
    if (calcMode === "lastPrice") {
      rows = rows.map((item) => {
        const {
          unrealized_pnl_index,
          unrealized_pnl_ROI_index,

          ...rust
        } = item;

        return {
          ...rust,
          unrealized_pnl: unrealized_pnl_index ?? 0,
          unsettled_pnl_ROI: unrealized_pnl_ROI_index ?? 0,
        };
      });
    }

    /**
     * Merge TP/SL order data into position rows
     * Each position gets two TP/SL categories:
     * - full_tp_sl: Orders that close the entire position
     * - partial_tp_sl: Orders that close only part of the position
     * Only processes when TP/SL orders exist to optimize performance
     */
    if (Array.isArray(tpslOrders) && tpslOrders.length) {
      rows = rows.map((item) => {
        // Find TP/SL orders matching this position's symbol
        const { fullPositionOrder, partialPositionOrders } =
          findPositionTPSLFromOrders(tpslOrders, item.symbol, item.margin_mode);

        // Extract TP/SL prices from full position order
        const full_tp_sl = fullPositionOrder
          ? findTPSLFromOrder(fullPositionOrder)
          : undefined;

        // Extract first partial position order (UI typically shows one at a time)
        const partialPossitionOrder =
          partialPositionOrders && partialPositionOrders.length
            ? partialPositionOrders[0]
            : undefined;
        const partial_tp_sl = partialPossitionOrder
          ? findTPSLFromOrder(partialPossitionOrder)
          : undefined;

        return {
          ...item,
          full_tp_sl: {
            tp_trigger_price: full_tp_sl?.tp_trigger_price,
            sl_trigger_price: full_tp_sl?.sl_trigger_price,
            algo_order: fullPositionOrder,
          },
          partial_tp_sl: {
            order_num: partialPositionOrders?.length ?? 0,
            tp_trigger_price: partial_tp_sl?.tp_trigger_price,
            sl_trigger_price: partial_tp_sl?.sl_trigger_price,
            algo_order: partialPossitionOrder,
          },
        };
      });
    }
  }

  /**
   * Create memoized getter function for accessing aggregated data
   * Provides performance optimization for nested property access
   */
  const positionInfoGetter = createGetter(
    aggregated as Omit<API.PositionInfo, "rows">,
    1,
  );

  /**
   * Return tuple with three elements:
   * 1. Position data object with processed rows and aggregated metrics
   * 2. Getter function for optimized aggregated data access
   * 3. Loading state object
   */
  return [
    {
      rows, // Processed position rows with TP/SL data
      aggregated: formattedPositions?.[1] ?? EMPTY_OBJECT,
      totalCollateral, // Portfolio-wide collateral
      totalValue, // Total portfolio value
      totalUnrealizedROI, // Total unrealized ROI across all positions
    },
    positionInfoGetter,
    {
      /**
       * @deprecated use `isLoading` instead
       */
      loading: positionStatus.loading,
      isLoading: positionStatus.loading,
    },
  ] as const;
};

/**
 * Helper function to extract unsettled PnL from position stream result
 * Uses ramda's pathOr to safely access nested property with fallback to 0
 * @deprecated Consider using direct property access with optional chaining
 */
export const pathOr_unsettledPnLPathOr = pathOr(0, [
  0,
  "aggregated",
  "unsettledPnL",
]);
