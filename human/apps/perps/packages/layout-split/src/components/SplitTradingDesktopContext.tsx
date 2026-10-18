/**
 * React Context for trading desktop props passed from TradingWidget via SplitTradingDesktopChrome.
 * Any component under SplitTradingDesktopChrome (e.g. SplitNodeRenderer, SortNodeRenderer, panel content)
 * can use useSplitTradingDesktopContext() to access TradingState and layout-related props without prop drilling.
 */
import React from "react";
import type { DesktopLayoutProps } from "../types";

/** Context value: full desktop layout props (TradingState + layout injection). Null when outside provider. */
export const SplitTradingDesktopContext =
  React.createContext<DesktopLayoutProps | null>(null);

/** Display name for React DevTools. */
SplitTradingDesktopContext.displayName = "SplitTradingDesktopContext";

/**
 * Hook to read trading desktop props from context.
 * Returns null when used outside SplitTradingDesktopChrome (e.g. in tests or non-split layout).
 * @returns DesktopLayoutProps or null
 */
export function useSplitTradingDesktopContext(): DesktopLayoutProps | null {
  return React.useContext(SplitTradingDesktopContext);
}
