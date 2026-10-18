# orderEntry.ui.tsx (OrderEntry)

## Overview

Main order entry UI component. Renders header (side + order type), available balance, order inputs (price/trigger/quantity/total, scaled or trailing stop), quantity slider, submit button, asset info, TP/SL section, reduce-only switch, sound alert, and additional options (FOK/IOC/Post-only, confirm). Uses `OrderEntryProvider` and integrates with confirm dialogs and TPSL advanced sheet.

## Props

Extends `OrderEntryScriptReturn` with:

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `containerRef` | `React.RefObject<HTMLDivElement>` | No | Ref for the root container |
| `disableFeatures` | `("slippageSetting" \| "feesInfo")[]` | No | Features to disable |

All other props come from `useOrderEntryScript`: `side`, `formattedOrder`, `setOrderValue`, `setOrderValues`, `symbolInfo`, `maxQty`, `freeCollateral`, `helper`, `submit`, `metaState`, `bboStatus`, `bboType`, `onBBOChange`, `toggleBBO`, `currentLtv`, `fillMiddleValue`, `soundAlert`, `setSoundAlert`, `currentFocusInput`, etc.

## Usage example

```tsx
import { useOrderEntryScript } from "./orderEntry.script";
import { OrderEntry } from "./orderEntry.ui";

const state = useOrderEntryScript({ symbol: "PERP_BTC_USDC" });
<OrderEntry {...state} />
```
