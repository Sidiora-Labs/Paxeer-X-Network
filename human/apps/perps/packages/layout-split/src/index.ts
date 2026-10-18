/**
 * @paxeer-network/layout-split
 *
 * Split layout with fixed JSX approach - no rule-based layout definitions.
 * Provides split-pane layout with resizable panels using react-resizable-panels.
 */

export { SplitLayout } from "./components/SplitLayout";
export type {
  SplitLayoutProps,
  PanelConstraints,
} from "./components/SplitLayout";

export type {
  SplitLayoutModel,
  SplitLayoutNode,
  SplitLayoutBreakpointKey,
  SplitLayoutBreakpoints,
  SplitLayoutChildConstraints,
  DesktopLayoutProps,
} from "./types";

export {
  createDefaultSplitLayout,
  getSortableIdForChild,
  updateOrderAtPath,
  serializeSplitLayout,
  deserializeSplitLayout,
} from "./utils/splitLayoutUtils";

export {
  SplitLayoutConfigProvider,
  useSplitLayoutConfig,
} from "./SplitLayoutConfigContext";
export type {
  SplitLayoutConfigValue,
  SplitLayoutConfigProviderProps,
} from "./SplitLayoutConfigContext";

export {
  registerLayoutSplitPlugin,
  type LayoutSplitPluginOptions,
} from "./plugin";

export {
  SplitTradingDesktopChrome,
  type SplitTradingDesktopChromeProps,
} from "./components/SplitTradingDesktopChrome";

export { SplitInlinedLayout } from "./components/SplitInlinedLayout";

export {
  SplitTradingLayout,
  type SplitTradingLayoutProps,
  type SplitLayoutPanelRegistry,
  type SplitLayoutPanelRegistryEntry,
} from "./components/SplitTradingLayout";

export { splitTradingStrategy } from "./splitTradingStrategy";

export { useSplitTradingDesktopContext } from "./components/SplitTradingDesktopContext";

export { TradingSplitLayout } from "./components/TradingSplitLayout";

export {
  TradingSortablePanel,
  type TradingSortablePanelProps,
} from "./components/TradingSortablePanel";

export {
  DEFAULT_SPLIT_BREAKPOINTS,
  SPLIT_BREAKPOINT_ORDER,
  VIEWPORT_BREAKPOINTS,
  VIEWPORT_BREAKPOINT_ORDER,
  type ViewportBreakpointKey,
} from "./constants";

export { useViewportBreakpoint } from "./hooks/useViewportBreakpoint";

export {
  useSplitLayout,
  type UseSplitLayoutOptions,
  type UseSplitLayoutReturn,
  type MarketLayoutPosition,
  type PanelSize,
  ORDER_ENTRY_MIN_WIDTH,
  ORDER_ENTRY_MAX_WIDTH,
  ORDERBOOK_MIN_WIDTH,
  ORDERBOOK_MAX_WIDTH,
  ORDERBOOK_MIN_HEIGHT,
  ORDERBOOK_MAX_HEIGHT,
  TRADINGVIEW_MIN_HEIGHT,
  TRADINGVIEW_MIN_WIDTH,
  DATA_LIST_MAX_HEIGHT,
  DATA_LIST_INITIAL_HEIGHT,
  SPACE,
  SYMBOL_INFO_BAR_HEIGHT,
} from "./hooks/useSplitLayout";
