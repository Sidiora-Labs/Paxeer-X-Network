# entry (index.ts)

## Overview

Package entry point. Re-exports the public API of `@paxeer-network/ui-orders`: main widget, tab type, script hook, and order list components/hooks.

## Exports

### `OrdersWidget`

ForwardRef component that renders the full orders experience (tabs: All, Pending, TP/SL, Filled, Cancelled, Rejected) and delegates to desktop order list.

### `TabType`

Enum of tab values: `all`, `pending`, `tp_sl`, `filled`, `cancelled`, `rejected`, `orderHistory`.

### `useOrdersScript`

Hook that builds state for the Orders UI and exposes `OrderListInstance` via ref (e.g. `download()`).

### `OrdersBuilderState`

Type: `ReturnType<typeof useOrdersScript>` — state passed from script to Orders UI.

### Order list re-exports

All exports from `./components/orderList`: `DesktopOrderList`, `MobileOrderList`, `DesktopOrderListWidget`, `MobileOrderListWidget`, `useOrderListScript`, `OrderListInstance`.

## Usage example

```tsx
import {
  OrdersWidget,
  TabType,
  useOrdersScript,
  DesktopOrderListWidget,
  useOrderListScript,
} from "@paxeer-network/ui-orders";

function App() {
  const ref = useRef<OrderListInstance>(null);
  return (
    <OrdersWidget
      current={TabType.pending}
      pnlNotionalDecimalPrecision={2}
      ref={ref}
    />
  );
}
```
