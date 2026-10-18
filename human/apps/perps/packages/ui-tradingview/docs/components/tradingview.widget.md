# TradingviewWidget

## Overview

Main entry component for the TradingView chart. It is a `forwardRef` component that composes `useTradingviewScript` (script loading, Widget instance, broker, renderers, state) and `TradingviewUI` (layout and controls), and forwards the ref to the root DOM element.

## Props

Uses `TradingviewWidgetPropsInterface` from `../type` (see [type.md](../type.md)).

## Usage example

```tsx
import { TradingviewWidget } from "@paxeer-network/ui-tradingview";

<TradingviewWidget
  ref={chartRef}
  symbol="PERP_BTC_USDC"
  scriptSRC={chartingLibraryUrl}
  libraryPath={libraryPath}
  mode={1}
/>
```
