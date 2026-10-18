# index.ts (package exports)

## Overview

Public API of `@paxeer-network/ui-order-entry`. Re-exports the main order entry components, hook, and widgets.

## Exports

| Export | Description |
|--------|-------------|
| `OrderEntry` | Main order entry UI component (from orderEntry.ui) |
| `useOrderEntryScript` | Hook that wires order state, BBO, TPSL, and submit (from orderEntry.script) |
| `OrderEntryWidget` | Widget that composes useOrderEntryScript + OrderEntry (from orderEntry.widget) |
| `OrderConfirmDialog` | Order confirmation dialog (from components/dialog/confirm.ui) |
| `AdditionalInfo` | FOK/IOC/Post-only and confirm options (from components/additional/additionalInfo) |
| `LTVRiskTooltipWidget` | LTV risk tooltip widget (from components/LTVRiskTooltip) |
| `FeesWidget` | Fees display widget (from components/fee) |

## Usage example

```tsx
import {
  OrderEntryWidget,
  useOrderEntryScript,
  OrderConfirmDialog,
  FeesWidget,
} from "@paxeer-network/ui-order-entry";

<OrderEntryWidget symbol="PERP_BTC_USDC" />

const state = useOrderEntryScript({ symbol: "PERP_BTC_USDC" });
<OrderEntry {...state} />
```
