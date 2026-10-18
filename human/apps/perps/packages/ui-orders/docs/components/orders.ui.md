# orders.ui

## Overview

Tabbed Orders UI: renders `Tabs` with panels for All, Pending, TP/SL, Filled, Cancelled, Rejected. Each panel uses a lazy-loaded `DesktopOrderListWidget` with the appropriate `type` and `ordersStatus`.

## Exports

### `Orders`

`React.FC<OrdersBuilderState>`. Receives script state (e.g. `current`, `pnlNotionalDecimalPrecision`, `orderListRef`, `sharePnLConfig`) and renders contained tabs with lazy desktop order list instances.

## Props (from OrdersBuilderState)

| Prop | Type | Description |
|------|------|-------------|
| `current` | `TabType` | Default tab. |
| `pnlNotionalDecimalPrecision` | `number` | PnL/notional precision. |
| `orderListRef` | `RefObject<OrderListInstance>` | Ref for download. |
| `sharePnLConfig` | `SharePnLConfig` | Share PnL modal config. |

## Usage example

```tsx
const state = useOrdersScript({ current: TabType.all, ref });
return <Orders {...state} />;
```
