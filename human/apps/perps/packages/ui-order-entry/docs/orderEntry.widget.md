# orderEntry.widget.tsx (OrderEntryWidget)

## Overview

Wrapper component that calls `useOrderEntryScript` and renders `OrderEntry` with the returned state. Easiest way to embed the full order entry in a page.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | `string` | Yes | Trading symbol |
| `containerRef` | `React.RefObject<HTMLDivElement>` | No | Ref for root container |
| `disableFeatures` | `("slippageSetting" \| "feesInfo")[]` | No | Features to disable |

## Usage example

```tsx
import { OrderEntryWidget } from "@paxeer-network/ui-order-entry";

<OrderEntryWidget symbol="PERP_BTC_USDC" />
```
