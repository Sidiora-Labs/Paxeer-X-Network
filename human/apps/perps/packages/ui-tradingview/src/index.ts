/**
 * Side-effect: augment InterceptorTargetPropsMap for typed interceptor props.
 */
import "./interceptorTargets";

export { TradingviewWidget } from "./components/tradingview.widget";
export { TradingviewUI } from "./components/tradingview.ui";
export {
  InjectableTradingviewDesktop,
  InjectableChartOverlay,
} from "./components/tradingview.injectable";
export type { ChartOverlayProps } from "./components/tradingview.injectable";
export { useTradingviewScript } from "./components/tradingview.script";
/**
 * Export display-control interceptor target + props for plugin consumers.
 */
export {
  DesktopDisplayControlMenuListTarget,
  DesktopDisplayControl,
  MobileDisplayControl,
} from "./components/displayControl";
export type {
  DesktopDisplayControlMenuItem,
  DesktopDisplayControlMenuListProps,
} from "./components/displayControl/common";
export * from "./type";
