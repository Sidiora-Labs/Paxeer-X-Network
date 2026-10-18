/**
 * useSplitLayout - Hook for computing split layout state and sizes
 *
 * Mirrors the layout computation logic from trading.script.tsx but for the layout-split plugin.
 * Computes sizes via localStorage persistence and returns layout state values.
 */
import { useMemo } from "react";
import { useLocalStorage, useMediaQuery } from "@paxeer-network/hooks";
import { TRADING_PANEL_IDS } from "@paxeer-network/layout-core";
import type { SplitLayoutBreakpointKey } from "../types";
import { useViewportBreakpoint } from "./useViewportBreakpoint";

// Layout dimension constants (from trading.script.tsx)
export const ORDER_ENTRY_MIN_WIDTH = 280;
export const ORDER_ENTRY_MAX_WIDTH = 360;

export const ORDERBOOK_MIN_WIDTH = 280;
export const ORDERBOOK_MAX_WIDTH = 732;

export const ORDERBOOK_MIN_HEIGHT = 464;
export const ORDERBOOK_MAX_HEIGHT = 728;

export const TRADINGVIEW_MIN_HEIGHT = 320;
export const TRADINGVIEW_MIN_WIDTH = 540;

export const DATA_LIST_MAX_HEIGHT = 800;
export const DATA_LIST_INITIAL_HEIGHT = 350;

export const SPACE = 8;
/** Baseline reservation for symbol info chrome (aligned with trading-next DesktopLayout sans countdown). */
export const SYMBOL_INFO_BAR_HEIGHT = 56;

// Storage keys
const PAXEER_NETWORK_ORDER_ENTRY_SIDE_MARKETS_LAYOUT =
  "paxeerNetwork_order_entry_side_markets_layout";
const PAXEER_NETWORK_SIDE_MARKETS_MODE_KEY = "paxeerNetwork_side_markets_mode";
const PAXEER_NETWORK_HORIZONTAL_MARKETS_LAYOUT = "paxeerNetwork_horizontal_markets_layout";

const PAXEER_NETWORK_MAIN_SPLIT_SIZE = "paxeerNetwork_main_split_size";
const PAXEER_NETWORK_DATA_LIST_SPLIT_SIZE = "paxeerNetwork_datalist_split_size";
const PAXEER_NETWORK_ORDERBOOK_SPLIT_SIZE = "paxeerNetwork_orderbook_split_size";
const PAXEER_NETWORK_DATA_LIST_SPLIT_HEIGHT_SM = "paxeerNetwork_datalist_split_height_sm";
const PAXEER_NETWORK_ORDERBOOK_SPLIT_HEIGHT_SM = "paxeerNetwork_orderbook_split_height_sm";
const PAXEER_NETWORK_ORDER_ENTRY_EXTRA_HEIGHT = "paxeerNetwork_order_entry_extra_height";

export type MarketLayoutPosition = "left" | "top" | "bottom" | "hide";
export type PanelSize = "small" | "middle" | "large";

export interface UseSplitLayoutOptions {
  symbol?: string;
  /** Initial layout side (order entry left/right) */
  initialLayout?: "left" | "right";
  /** Initial market layout position */
  initialMarketLayout?: MarketLayoutPosition;
  /** Whether user can trade */
  canTrade?: boolean;
  /** First time depositor */
  isFirstTimeDeposit?: boolean;
  /** Disable features */
  disableFeatures?: Record<string, boolean>;
  /** Navigate callback */
  onRouteChange?: (route: { href: string; name: string }) => void;
}

export interface UseSplitLayoutReturn {
  // Layout position state
  layout: "left" | "right";
  setLayout: (layout: "left" | "right") => void;
  marketLayout: MarketLayoutPosition;
  setMarketLayout: (layout: MarketLayoutPosition) => void;

  // Breakpoint state
  breakpoint: SplitLayoutBreakpointKey;
  max2XL: boolean; // max-width: 1279px
  min3XL: boolean; // min-width: 1440px
  max4XL: boolean; // max-width: 1680px
  horizontalDraggable: boolean;

  // Panel size state (for collapsible markets)
  panelSize: PanelSize;
  setPanelSize: (size: PanelSize) => void;
  marketsWidth: number;

  // Split sizes (persisted)
  mainSplitSize: string;
  setMainSplitSize: (size: string) => void;
  orderBookSplitSize: string;
  setOrderbookSplitSize: (size: string) => void;
  dataListSplitSize: string;
  setDataListSplitSize: (size: string) => void;
  dataListSplitHeightSM: string;
  setDataListSplitHeightSM: (size: string) => void;
  orderBookSplitHeightSM: string;
  setOrderbookSplitHeightSM: (size: string) => void;

  // Extra height for order entry
  extraHeight: number;
  setExtraHeight: (height: number) => void;

  // Data list height
  dataListHeight: number;

  // Trading view max height
  tradingviewMaxHeight: number;

  // Layout tree (for backward compat with LayoutHost)
  layoutTree: SplitLayoutNode;

  // Constants
  symbolInfoBarHeight: number;
  space: number;
  orderbookMinWidth: number;
  orderbookMaxWidth: number;
  orderbookMinHeight: number;
  orderbookMaxHeight: number;
  tradingviewMinHeight: number;
  orderEntryMinWidth: number;
  orderEntryMaxWidth: number;
}

/** Simplified split layout node for backward compat */
export type SplitLayoutNode =
  | {
      type: "panel";
      id: string;
      size?: string;
      minSize?: string;
      maxSize?: string;
      disabled?: boolean;
    }
  | {
      type: "split";
      orientation: "horizontal" | "vertical";
      children: SplitLayoutNode[];
      size?: string;
    }
  | {
      type: "sort";
      orientation: "horizontal" | "vertical";
      children: SplitLayoutNode[];
      size?: string;
    };

/**
 * Builds a simplified layout tree for the current breakpoint and layout settings
 */
function buildLayoutTree(
  layout: "left" | "right",
  marketLayout: MarketLayoutPosition,
  max2XL: boolean,
): SplitLayoutNode {
  // For max2XL (≤1279px), use vertical layout
  if (max2XL) {
    return buildSMLayoutTree(layout, marketLayout);
  }
  return buildLGLayoutTree(layout, marketLayout);
}

function buildLGLayoutTree(
  layout: "left" | "right",
  marketLayout: MarketLayoutPosition,
): SplitLayoutNode {
  const orderEntrySort: SplitLayoutNode = {
    type: "sort",
    orientation: "vertical",
    children: [
      { type: "panel", id: TRADING_PANEL_IDS.MARGIN },
      { type: "panel", id: TRADING_PANEL_IDS.ASSETS },
      { type: "panel", id: TRADING_PANEL_IDS.ORDER_ENTRY },
    ],
  };

  const tvObSplit: SplitLayoutNode = {
    type: "split",
    orientation: "horizontal",
    children: [
      { type: "panel", id: TRADING_PANEL_IDS.TRADING_VIEW },
      { type: "panel", id: TRADING_PANEL_IDS.ORDERBOOK },
    ],
  };

  const mainCol: SplitLayoutNode = {
    type: "split",
    orientation: "vertical",
    children: [
      {
        type: "panel",
        id: TRADING_PANEL_IDS.SYMBOL_INFO_BAR,
        size: "fixed",
        disabled: true,
      },
      tvObSplit,
      { type: "panel", id: TRADING_PANEL_IDS.DATA_LIST },
    ],
  };

  if (marketLayout === "top") {
    return {
      type: "split",
      orientation: "vertical",
      children: [
        {
          type: "panel",
          id: TRADING_PANEL_IDS.HORIZONTAL_MARKETS,
          size: "fixed",
          disabled: true,
        },
        layout === "left"
          ? {
              type: "split",
              orientation: "horizontal",
              children: [orderEntrySort, mainCol],
            }
          : {
              type: "split",
              orientation: "horizontal",
              children: [mainCol, orderEntrySort],
            },
      ],
    };
  }

  if (marketLayout === "bottom") {
    return layout === "left"
      ? {
          type: "split",
          orientation: "horizontal",
          children: [
            orderEntrySort,
            {
              type: "split",
              orientation: "vertical",
              children: [
                mainCol,
                {
                  type: "panel",
                  id: TRADING_PANEL_IDS.HORIZONTAL_MARKETS,
                  size: "fixed",
                  disabled: true,
                },
              ],
            },
          ],
        }
      : {
          type: "split",
          orientation: "horizontal",
          children: [
            {
              type: "split",
              orientation: "vertical",
              children: [
                mainCol,
                {
                  type: "panel",
                  id: TRADING_PANEL_IDS.HORIZONTAL_MARKETS,
                  size: "fixed",
                  disabled: true,
                },
              ],
            },
            orderEntrySort,
          ],
        };
  }

  // marketLayout === "left" or "hide"
  if (marketLayout === "hide") {
    return layout === "left"
      ? {
          type: "split",
          orientation: "horizontal",
          children: [orderEntrySort, mainCol],
        }
      : {
          type: "split",
          orientation: "horizontal",
          children: [mainCol, orderEntrySort],
        };
  }

  // marketLayout === "left"
  return {
    type: "split",
    orientation: "horizontal",
    children: [
      {
        type: "panel",
        id: TRADING_PANEL_IDS.MARKETS,
        size: "fixed",
        disabled: true,
      },
      layout === "left"
        ? {
            type: "split",
            orientation: "horizontal",
            children: [orderEntrySort, mainCol],
          }
        : {
            type: "split",
            orientation: "horizontal",
            children: [mainCol, orderEntrySort],
          },
    ],
  };
}

function buildSMLayoutTree(
  layout: "left" | "right",
  marketLayout: MarketLayoutPosition,
): SplitLayoutNode {
  const orderEntrySort: SplitLayoutNode = {
    type: "sort",
    orientation: "vertical",
    children: [
      { type: "panel", id: TRADING_PANEL_IDS.MARGIN },
      { type: "panel", id: TRADING_PANEL_IDS.ASSETS },
      { type: "panel", id: TRADING_PANEL_IDS.ORDER_ENTRY },
    ],
  };

  const tvObSplit: SplitLayoutNode = {
    type: "split",
    orientation: "vertical",
    children: [
      { type: "panel", id: TRADING_PANEL_IDS.TRADING_VIEW },
      { type: "panel", id: TRADING_PANEL_IDS.ORDERBOOK },
    ],
  };

  const mainCol: SplitLayoutNode = {
    type: "split",
    orientation: "vertical",
    children: [
      {
        type: "panel",
        id: TRADING_PANEL_IDS.SYMBOL_INFO_BAR,
        size: "fixed",
        disabled: true,
      },
      tvObSplit,
      { type: "panel", id: TRADING_PANEL_IDS.DATA_LIST },
    ],
  };

  if (marketLayout === "top") {
    return {
      type: "split",
      orientation: "vertical",
      children: [
        {
          type: "panel",
          id: TRADING_PANEL_IDS.HORIZONTAL_MARKETS,
          size: "fixed",
          disabled: true,
        },
        {
          type: "split",
          orientation: "vertical",
          children: [orderEntrySort, mainCol],
        },
      ],
    };
  }

  if (marketLayout === "hide") {
    return {
      type: "split",
      orientation: "vertical",
      children: [orderEntrySort, mainCol],
    };
  }

  // marketLayout === "left" or "bottom"
  if (marketLayout === "bottom") {
    return {
      type: "split",
      orientation: "vertical",
      children: [
        mainCol,
        {
          type: "panel",
          id: TRADING_PANEL_IDS.HORIZONTAL_MARKETS,
          size: "fixed",
          disabled: true,
        },
      ],
    };
  }

  // marketLayout === "left"
  return {
    type: "split",
    orientation: "vertical",
    children: [
      {
        type: "split",
        orientation: "horizontal",
        children: [orderEntrySort, mainCol],
      },
    ],
  };
}

/**
 * Hook for computing split layout state and sizes
 */
export function useSplitLayout(
  options: UseSplitLayoutOptions = {},
): UseSplitLayoutReturn {
  const {
    initialLayout = "right",
    initialMarketLayout = "left",
    canTrade = true,
  } = options;

  // Media queries
  const max2XL = useMediaQuery("(max-width: 1279px)");
  const min3XL = useMediaQuery("(min-width: 1440px)");
  const max4XL = useMediaQuery("(max-width: 1680px)");

  // Breakpoint
  const breakpoint = useViewportBreakpoint();

  // Layout position state (order entry side)
  const [layout, setLayout] = useLocalStorage<"left" | "right">(
    PAXEER_NETWORK_ORDER_ENTRY_SIDE_MARKETS_LAYOUT,
    initialLayout,
  );

  // Market layout position state
  const [marketLayout, setMarketLayout] = useLocalStorage<MarketLayoutPosition>(
    PAXEER_NETWORK_HORIZONTAL_MARKETS_LAYOUT,
    initialMarketLayout,
  );

  // Markets panel size (small/middle/large)
  const [panelSize, setPanelSize] = useLocalStorage<PanelSize>(
    PAXEER_NETWORK_SIDE_MARKETS_MODE_KEY,
    "large",
  );

  // Compute markets width based on panel size
  const marketsWidth = useMemo(() => {
    switch (panelSize) {
      case "small":
        return 0;
      case "middle":
        return 70;
      case "large":
        return 280;
      default:
        return 0;
    }
  }, [panelSize]);

  // Horizontal draggable (only on lg+ screens)
  const horizontalDraggable = min3XL;

  // Split sizes (persisted)
  const [mainSplitSize, setMainSplitSize] = useLocalStorage(
    PAXEER_NETWORK_MAIN_SPLIT_SIZE,
    `${ORDER_ENTRY_MIN_WIDTH}px`,
  );

  const [orderBookSplitSize, setOrderbookSplitSize] = useLocalStorage(
    PAXEER_NETWORK_ORDERBOOK_SPLIT_SIZE,
    "280px",
  );

  const [dataListSplitSize, setDataListSplitSize] = useLocalStorage(
    PAXEER_NETWORK_DATA_LIST_SPLIT_SIZE,
    `${DATA_LIST_INITIAL_HEIGHT}px`,
  );

  const [dataListSplitHeightSM, setDataListSplitHeightSM] = useLocalStorage(
    PAXEER_NETWORK_DATA_LIST_SPLIT_HEIGHT_SM,
    "350px",
  );

  const [orderBookSplitHeightSM, setOrderbookSplitHeightSM] = useLocalStorage(
    PAXEER_NETWORK_ORDERBOOK_SPLIT_HEIGHT_SM,
    "280px",
  );

  const [extraHeight, setExtraHeight] = useLocalStorage(
    PAXEER_NETWORK_ORDER_ENTRY_EXTRA_HEIGHT,
    0,
  );

  // Trading view max height
  const tradingviewMaxHeight = max2XL ? 1200 : 600;

  // Data list height
  const dataListHeight = canTrade ? 379 : 277;

  // Compute markets collapse state
  const memoizedPanelSize = useMemo<PanelSize>(() => {
    const normalized = panelSize === "large" ? "large" : "middle";
    return horizontalDraggable ? normalized : "middle";
  }, [horizontalDraggable, panelSize]);

  // Build layout tree for backward compat
  const layoutTree = useMemo(
    () => buildLayoutTree(layout, marketLayout, max2XL),
    [layout, marketLayout, max2XL],
  );

  return {
    // Layout position
    layout,
    setLayout,
    marketLayout,
    setMarketLayout,

    // Breakpoint
    breakpoint,
    max2XL,
    min3XL,
    max4XL,
    horizontalDraggable,

    // Markets panel
    panelSize: memoizedPanelSize,
    setPanelSize: (size: PanelSize) => setPanelSize(size),
    marketsWidth,

    // Split sizes
    mainSplitSize,
    setMainSplitSize,
    orderBookSplitSize,
    setOrderbookSplitSize,
    dataListSplitSize,
    setDataListSplitSize,
    dataListSplitHeightSM,
    setDataListSplitHeightSM,
    orderBookSplitHeightSM,
    setOrderbookSplitHeightSM,

    // Extra height
    extraHeight: extraHeight as number,
    setExtraHeight: setExtraHeight as (height: number) => void,

    // Data list
    dataListHeight,

    // Trading view
    tradingviewMaxHeight,

    // Layout tree (for compat)
    layoutTree,

    // Constants
    symbolInfoBarHeight: SYMBOL_INFO_BAR_HEIGHT,
    space: SPACE,
    orderbookMinWidth: ORDERBOOK_MIN_WIDTH,
    orderbookMaxWidth: ORDERBOOK_MAX_WIDTH,
    orderbookMinHeight: ORDERBOOK_MIN_HEIGHT,
    orderbookMaxHeight: ORDERBOOK_MAX_HEIGHT,
    tradingviewMinHeight: TRADINGVIEW_MIN_HEIGHT,
    orderEntryMinWidth: ORDER_ENTRY_MIN_WIDTH,
    orderEntryMaxWidth: ORDER_ENTRY_MAX_WIDTH,
  };
}
