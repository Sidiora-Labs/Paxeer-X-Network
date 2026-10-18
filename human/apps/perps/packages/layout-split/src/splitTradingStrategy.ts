import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { SplitTradingLayout } from "./components/SplitTradingLayout";

/**
 * Split trading layout strategy.
 *
 * Implements the full trading desktop layout (chart, orderbook, order entry,
 * data list, markets) with resizable split panels and drag-to-reorder order
 * entry sub-panels. All state is managed internally by the Renderer.
 *
 * Pass this as `layoutStrategy` to DesktopLayout (via the interceptor in
 * registerLayoutSplitPlugin) so LayoutHost calls SplitTradingLayout as the
 * Renderer with the PanelRegistry built by trading-next.
 */
export const splitTradingStrategy: LayoutStrategy<Record<string, unknown>> = {
  id: "split-trading",
  displayName: "Split Trading Layout",
  /** No layout model needed — state is managed internally by SplitTradingLayout. */
  defaultLayout: () => ({}),
  serialize: () => "{}",
  deserialize: () => ({}),
  /** SplitTradingLayout implements LayoutRendererProps<Record<string, unknown>>. */
  Renderer: SplitTradingLayout as LayoutStrategy<
    Record<string, unknown>
  >["Renderer"],
};
