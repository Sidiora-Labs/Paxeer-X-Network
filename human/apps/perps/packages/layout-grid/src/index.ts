/** @paxeer-network/layout-grid */

export { gridStrategy } from "./gridStrategy";
export type {
  GridLayoutModel,
  GridLayoutItem,
  GridLayoutRule,
  GridLayoutItemSpec,
  // GridLayoutBreakpointKey,
  GridLayoutPreset,
  GridConfig,
} from "./types";
export {
  createDefaultGridLayout,
  serializeGridLayout,
  deserializeGridLayout,
  DEFAULT_BREAKPOINTS,
  DEFAULT_COLS,
} from "./utils/gridLayoutUtils";
export {
  createTradingGridLayout,
  createTradingGridLayoutFromPreset,
} from "./utils/tradingGridLayout";
export {
  getDefaultGridPresets,
  DEFAULT_GRID_PRESETS,
} from "./utils/defaultPresets";
export type { GridPresetContextValue } from "./GridPresetContext";
export { GridLayoutSwitcher } from "./GridLayoutSwitcher";
export type { GridLayoutSwitcherProps } from "./GridLayoutSwitcher";

export {
  registerLayoutGridPlugin,
  type LayoutGridPluginOptions,
} from "./plugin";
