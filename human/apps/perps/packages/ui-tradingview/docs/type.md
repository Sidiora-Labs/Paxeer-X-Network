# type

## Overview

Defines public TypeScript interfaces and types for the TradingView widget and UI: widget props, UI props, display control state, and locale. Re-exports chart mode and color config from the adapter.

## Exports

### TradingviewLocaleCode

Type alias for TradingView language code (from adapter `LanguageCode`).

### TradingviewWidgetPropsInterface

Props for the main `TradingviewWidget` component.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `symbol` | `string` | No | Trading symbol |
| `mode` | `ChartMode` | No | Chart mode (BASIC, ADVANCED, UNLIMITED, MOBILE) |
| `scriptSRC` | `string` | No | TradingView script URL |
| `library_path` | `string` | No | Charting library path |
| `overrides` | `Overrides` | No | Chart overrides |
| `studiesOverrides` | `StudyOverrides` | No | Studies overrides |
| `customCssUrl` | `string` | No | Custom CSS URL |
| `colorConfig` | `ColorConfigInterface` | No | Chart color configuration |
| `libraryPath` | `string` | No | Alias for library path |
| `fullscreen` | `boolean` | No | Fullscreen state |
| `theme` | `string` | No | Theme name |
| `loadingScreen` | `LoadingScreenOptions` | No | Loading screen options |
| `locale` | `TradingviewLocaleCode \| ((localeCode: LocaleCode) => TradingviewLocaleCode)` | No | Locale or function from app locale |
| `onFullScreenChange` | `(isFullScreen: boolean) => void` | No | Fullscreen change callback |
| `classNames` | `{ root?: string; content?: string }` | No | CSS class names |
| `enabled_features` | `string[]` | No | Extra enabled features |
| `disabled_features` | `string[]` | No | Extra disabled features |
| `customIndicatorsGetter` | ChartingLibraryWidgetOptions["custom_indicators_getter"] | No | Custom indicators getter |

### TradingviewUIPropsInterface

Props for the internal `TradingviewUI` component (chart ref, interval, display control, line type, callbacks).

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `tradingViewScriptSrc` | `string` | No | Script URL |
| `chartRef` | `React.Ref<HTMLDivElement>` | Yes | Ref for chart container |
| `interval` | `string` | No | Resolution string |
| `changeDisplaySetting` | `(setting: DisplayControlSettingInterface) => void` | Yes | Update display settings |
| `displayControlState` | `DisplayControlSettingInterface` | Yes | Current display toggles |
| `changeInterval` | `(newInterval: string) => void` | Yes | Change resolution |
| `lineType` | `string` | Yes | Chart line type id |
| `changeLineType` | `(newLineType: string) => void` | Yes | Change chart type |
| `openChartSetting` | `() => void` | Yes | Open chart properties |
| `openChartIndicators` | `() => void` | Yes | Open indicators dialog |
| `symbol` | `string` | No | Symbol |
| `onFullScreenChange` | `() => void` | Yes | Toggle fullscreen |
| `classNames` | `{ root?: string; content?: string }` | No | CSS class names |
| `fullscreen` | `boolean` | No | Fullscreen state |

### DisplayControlSettingInterface

Toggles for which overlays to show on the chart.

| Property | Type | Description |
|----------|------|-------------|
| `position` | `boolean` | Show positions |
| `buySell` | `boolean` | Show buy/sell (filled) |
| `limitOrders` | `boolean` | Show limit orders |
| `stopOrders` | `boolean` | Show stop orders |
| `tpsl` | `boolean` | Show TP/SL |
| `positionTpsl` | `boolean` | Show position TP/SL |
| `trailingStop` | `boolean` | Show trailing stop |

## Usage example

```tsx
import { TradingviewWidget } from "@paxeer-network/ui-tradingview";
import type { TradingviewWidgetPropsInterface } from "@paxeer-network/ui-tradingview";

const props: TradingviewWidgetPropsInterface = {
  symbol: "PERP_BTC_USDC",
  mode: 1,
  scriptSRC: "https://.../charting_library.js",
  locale: (localeCode) => (localeCode === "tc" ? "zh_TW" : localeCode),
};
<TradingviewWidget {...props} />;
```
