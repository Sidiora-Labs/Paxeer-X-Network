# Widget

## Overview

Class that creates and owns the TradingView widget instance. Handles options (datafeed, broker_factory, overrides, interval, saved_data, settings_adapter), chart key for persistence, auto-save on onAutoSaveNeeded and onVisibleRangeChanged, setSymbol/setResolution/executeActionById/changeLineType, and subscribeClick/unsubscribeClick. Removes datafeed, broker, and instance on remove().

## WidgetOptions

fullscreen?, autosize?, symbol, overrides?, studiesOverrides?, theme?, loadingScreen?, interval, locale, timezone?, container, libraryPath, customCssUrl?, customFontFamily?, datafeed, positionControlCallback?, getBroker?, customIndicatorsGetter?.

## WidgetProps

options: WidgetOptions; chartKey?; mode?; onClick?; enabled_features?; disabled_features?.

## Methods

- `remove()`: Unsubscribe click, datafeed.remove(), broker.remove(), instance.remove(), cancel debounced saves.
- `updateOverrides(overrides)`: Apply overrides to instance.
- `setSymbol(symbol, interval?, callback?)`: Set symbol on chart ready.
- `setResolution(resolution)`: Set resolution on active chart.
- `executeActionById(actionId)`: Execute chart action (e.g. chartProperties, insertIndicator).
- `changeLineType(lineType)`: Set chart type.
- `subscribeClick(onClick)` / `unsubscribeClick()`: Forward click to iframe document.

## Usage example

```ts
const widget = new Widget({ options, chartKey: "SDK_Tradingview", mode: ChartMode.ADVANCED });
// later: widget.setSymbol(symbol); widget.remove();
```
