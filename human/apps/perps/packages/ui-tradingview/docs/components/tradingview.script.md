# useTradingviewScript

## Overview

Hook that loads the TradingView script, creates and manages the `Widget` instance, wires the datafeed and broker, creates the renderer (positions, orders, executions, TP/SL), and returns all state and handlers needed by `TradingviewUI` (chartRef, interval, lineType, displayControlState, changeInterval, changeLineType, changeDisplaySetting, openChartSetting, openChartIndicators, onFullScreenChange, fullscreen, classNames, tradingViewScriptSrc).

## Parameters

Accepts `TradingviewWidgetPropsInterface` (symbol, mode, scriptSRC, libraryPath, overrides, studiesOverrides, customCssUrl, colorConfig, locale, enabled_features, disabled_features, customIndicatorsGetter, etc.).

## Returns

Object with: `tradingViewScriptSrc`, `chartRef`, `changeDisplaySetting`, `displayControlState`, `interval`, `changeInterval`, `lineType`, `changeLineType`, `openChartSetting`, `openChartIndicators`, `symbol`, `onFullScreenChange`, `classNames`, `fullscreen`.

## Usage example

```ts
const state = useTradingviewScript({
  symbol: "PERP_BTC_USDC",
  scriptSRC: "...",
  libraryPath: "...",
  mode: ChartMode.ADVANCED,
});
return <TradingviewUI {...state} ref={ref} />;
```
