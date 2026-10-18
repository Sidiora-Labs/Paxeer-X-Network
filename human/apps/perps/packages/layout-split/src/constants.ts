import type { SplitLayoutBreakpointKey, SplitLayoutBreakpoints } from "./types";

/** Breakpoint key order (largest to smallest). */
export const BREAKPOINT_KEYS: SplitLayoutBreakpointKey[] = [
  "lg",
  "md",
  "sm",
  "xs",
];

/** Default breakpoint widths (min width in px; aligned with layout-grid). */
export const BREAKPOINT_VALUES: SplitLayoutBreakpoints = {
  lg: 1680,
  md: 1440,
  sm: 1280,
  xs: 768,
};

/** @deprecated Use BREAKPOINT_KEYS */
export const SPLIT_BREAKPOINT_ORDER = BREAKPOINT_KEYS;

/** @deprecated Use BREAKPOINT_VALUES */
export const DEFAULT_SPLIT_BREAKPOINTS = BREAKPOINT_VALUES;

/** Viewport-based breakpoint keys for responsive split layout. */
export type ViewportBreakpointKey = SplitLayoutBreakpointKey;

/** Viewport breakpoint values (max width in px). */
export const VIEWPORT_BREAKPOINTS = BREAKPOINT_VALUES;

/** @deprecated Use BREAKPOINT_KEYS */
export const VIEWPORT_BREAKPOINT_ORDER = BREAKPOINT_KEYS;

/** LocalStorage key for selected split preset id. */
export const SPLIT_PRESET_ID_STORAGE_KEY =
  "paxeerNetwork_trading_desktop_split_preset";

/** Base key for per-preset layout persistence; actual key is `${SPLIT_LAYOUT_STORAGE_KEY_PREFIX}_${presetId}`. */
export const SPLIT_LAYOUT_STORAGE_KEY_PREFIX =
  "paxeerNetwork_trading_desktop_layout_split";
