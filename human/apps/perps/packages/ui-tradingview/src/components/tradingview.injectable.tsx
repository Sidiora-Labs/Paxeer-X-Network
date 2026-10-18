import { injectable } from "@paxeer-network/ui";
import type { IChartingLibraryWidget } from "../tradingviewAdapter/charting_library";
import type { TradingviewUIPropsInterface } from "../type";
import { TradingviewChart } from "./tradingview.chart";

export const InjectableTradingviewDesktop =
  injectable<TradingviewUIPropsInterface>(
    TradingviewChart,
    "TradingView.Desktop",
  );

/**
 * Props for the `Trading.Chart.Overlay` interceptor target.
 *
 * This is a React layer rendered above the chart DOM once the widget is ready —
 * NOT a TradingView overlay indicator (the studies/indicators system). A plugin
 * uses it to draw custom content (e.g. grid-bot bands) on top of the existing
 * chart, without replacing the chart itself.
 */
export type ChartOverlayProps = {
  /** Live TradingView widget instance, available only after the chart is ready. */
  widget: IChartingLibraryWidget;
  /** Current symbol; changes when the user switches trading pairs. */
  symbol: string;
  /**
   * The overlay container is `pointer-events-none`, so clicks/pass-through reach
   * the chart underneath and the user can still pan/zoom the chart. Interactive
   * content (draggable handles, buttons) must opt back in with `pointer-events-auto`.
   */
};

/**
 * `Trading.Chart.Overlay` — a React layer that floats above the chart DOM.
 *
 * Default renders nothing, so behavior is unchanged when no plugin is registered.
 * A plugin intercepts this target to draw custom content (e.g. grid-bot bands)
 * using the live `IChartingLibraryWidget` exposed via {@link ChartOverlayProps}.
 *
 * Note: this is distinct from `TradingView.Desktop`, which replaces the whole
 * chart. This target leaves the chart in place and only layers content on top.
 */
export const InjectableChartOverlay = injectable<ChartOverlayProps>(
  () => null,
  "Trading.Chart.Overlay",
);
