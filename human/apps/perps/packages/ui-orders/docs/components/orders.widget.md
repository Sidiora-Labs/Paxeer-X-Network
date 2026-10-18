# orders.widget

## Overview

Main orders widget: forwards ref to order list and composes `useOrdersScript` with the Orders tab UI.

## Exports

### `TabType` (enum)

| Value | Description |
|-------|-------------|
| `all` | All orders. |
| `pending` | Pending / incomplete. |
| `tp_sl` | TP/SL tab. |
| `filled` | Filled. |
| `cancelled` | Cancelled. |
| `rejected` | Rejected. |
| `orderHistory` | Order history. |

### `OrdersWidgetProps`

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `current` | `TabType` | No | Initial active tab. |
| `pnlNotionalDecimalPrecision` | `number` | No | Decimal precision for PnL/notional. |
| `sharePnLConfig` | `SharePnLConfig` | No | Config for share PnL modal. |

### `OrdersWidget`

`forwardRef<OrderListInstance, OrdersWidgetProps>` — renders `<Orders {...state} />` where state comes from `useOrdersScript`. Ref exposes `download()` for the order list.

## Usage example

```tsx
import { OrdersWidget, TabType } from "@paxeer-network/ui-orders";

<OrdersWidget
  current={TabType.pending}
  pnlNotionalDecimalPrecision={2}
  sharePnLConfig={shareConfig}
  ref={orderListRef}
/>
```
