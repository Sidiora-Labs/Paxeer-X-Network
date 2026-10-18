# components

## Overview

React components and hooks for the TradingView chart: main widget, UI layout, script loading, top bar, time interval, line type, display control (desktop/mobile), and fallback when script is not configured.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `tradingview.widget.tsx` | TSX | Main widget: forwardRef component composing useTradingviewScript + TradingviewUI | [tradingview.widget.md](./tradingview.widget.md) |
| `tradingview.ui.tsx` | TSX | Chart UI: top bar, interval/line-type/display controls, chart container, lazy subcomponents | [tradingview.ui.md](./tradingview.ui.md) |
| `tradingview.script.ts` | TypeScript | useTradingviewScript: script load, Widget creation, broker/renderer, display/interval/line state | [tradingview.script.md](./tradingview.script.md) |
| `topBar/index.tsx` | TSX | TopBar: simple wrapper for toolbar children | [topBar/index.md](./topBar/index.md) |
| `noTradingview/index.tsx` | TSX | NoTradingview: fallback UI when script URL is missing, with i18n and links | [noTradingview/index.md](./noTradingview/index.md) |
| `timeInterval/index.tsx` | TSX | TimeInterval + Desktop/Mobile variants: resolution selector with dropdown on mobile | [timeInterval/index.md](./timeInterval/index.md) |
| `lineType/index.tsx` | TSX | LineType: dropdown for chart type (bars, candles, line, area, etc.) | [lineType/index.md](./lineType/index.md) |
| `displayControl/index.tsx` | TSX | Re-exports DesktopDisplayControl and MobileDisplayControl | [displayControl/index.md](./displayControl/index.md) |
| `displayControl/displayControl.mobile.tsx` | TSX | MobileDisplayControl: dropdown grid of display toggles | [displayControl/displayControl.mobile.md](./displayControl/displayControl.mobile.md) |
| `displayControl/displayControl.desktop.tsx` | TSX | DesktopDisplayControl: dropdown with switches per option | [displayControl/displayControl.desktop.md](./displayControl/displayControl.desktop.md) |
| `displayControl/common.tsx` | TSX | IProps and DisplayControl type for display control components | [displayControl/common.md](./displayControl/common.md) |
