/**
 * @paxeer-network/layout-core
 *
 * Core layout strategy protocol and runtime glue for paxeer-network layout system.
 * This package provides the interface definitions and utilities that all
 * layout strategies must implement, without binding to any specific third-party
 * layout library.
 */

// Types
export type {
  LayoutModel,
  PanelRegistry,
  OnLayoutChange,
  OnLayoutPersist,
  LayoutStrategy,
  LayoutRendererProps,
  LayoutHostProps,
  StrategyResolverOptions,
} from "./types";

// Trading panel IDs (used by layout strategies; component mapping in trading package)
export { TRADING_PANEL_IDS, getTradingPanelIds } from "./tradingPanelIds";
export type { TradingPanelId } from "./tradingPanelIds";

// Components
export { LayoutHost } from "./LayoutHost";

// Hooks
export { useLayoutPersistence } from "./hooks/useLayoutPersistence";
export {
  useLayoutRuleManager,
  type UseLayoutRuleManagerResult,
} from "./hooks/useLayoutRuleManager";

// Utils
export { resolveStrategy } from "./utils/strategyResolver";
export {
  LayoutRuleManager,
  type LayoutPreset,
  type LayoutRuleManagerOptions,
} from "./utils/LayoutRuleManager";
