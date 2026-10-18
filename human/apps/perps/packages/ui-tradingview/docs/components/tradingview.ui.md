# TradingviewUI

## Overview

Renders the chart shell: top bar (time interval, display control, line type, indicators, settings, fullscreen), and the chart container. Uses lazy-loaded subcomponents for TimeInterval, LineType, and Desktop/Mobile display control. When `tradingViewScriptSrc` is missing, shows `NoTradingview` instead.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `chartRef` | `React.Ref<HTMLDivElement>` | Yes | Ref for chart container div |
| `interval` | `string` | No | Current resolution (e.g. "1", "15") |
| `changeDisplaySetting` | `(DisplayControlSettingInterface) => void` | Yes | Update display toggles |
| `displayControlState` | `DisplayControlSettingInterface` | Yes | Current display state |
| `changeInterval` | `(string) => void` | Yes | Set resolution |
| `lineType` | `string` | Yes | Chart type id |
| `changeLineType` | `(string) => void` | Yes | Set chart type |
| `openChartSetting` | `() => void` | Yes | Open chart properties |
| `openChartIndicators` | `() => void` | Yes | Open indicators dialog |
| `symbol` | `string` | No | Symbol |
| `onFullScreenChange` | `() => void` | Yes | Toggle fullscreen |
| `classNames` | `{ root?, content? }` | No | CSS classes |
| `fullscreen` | `boolean` | No | Fullscreen state |
| `tradingViewScriptSrc` | `string` | No | If falsy, shows NoTradingview |

## Usage example

```tsx
<TradingviewUI
  chartRef={chartRef}
  interval={interval}
  changeDisplaySetting={setDisplayControlState}
  displayControlState={displayControlState}
  changeInterval={changeInterval}
  lineType={lineType}
  changeLineType={changeLineType}
  openChartSetting={openChartSetting}
  openChartIndicators={openChartIndicators}
  onFullScreenChange={toggleFullscreen}
  fullscreen={fullscreen}
  tradingViewScriptSrc={scriptSrc}
/>
```
