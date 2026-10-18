# orderList.widget

## Overview

Desktop and mobile order list widgets. Desktop version uses `useOrderListScript` and forwards ref for download; mobile version supports load-more, filters, and optional classNames.

## Exports

### `DesktopOrderListWidgetProps`

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `type` | `TabType` | Yes | Tab type (all, pending, tp_sl, filled, etc.). |
| `ordersStatus` | `OrderStatus` | No | Filter by order status. |
| `symbol` | `string` | No | If set, fetch and show only this symbol's orders. |
| `onSymbolChange` | `(symbol: API.Symbol) => void` | No | Called when symbol changes. |
| `pnlNotionalDecimalPrecision` | `number` | No | PnL/notional precision. |
| `sharePnLConfig` | `SharePnLConfig` | No | Share PnL config. |
| `testIds` | `{ tableBody?: string }` | No | Test IDs for table body. |

### `DesktopOrderListWidget`

`forwardRef<OrderListInstance, DesktopOrderListWidgetProps>`. Composes `useOrderListScript` and `DesktopOrderList`; ref exposes `download()`.

### `MobileOrderListWidget`

Props: `type`, `ordersStatus`, `symbol`, `onSymbolChange`, `sharePnLConfig`, `classNames?`, `showFilter?`, `filterConfig?` (side, range). Uses `useOrderListScript` with `enableLoadMore: true` and renders `MobileOrderList`.

## Usage example

```tsx
<DesktopOrderListWidget
  type={TabType.pending}
  ordersStatus={OrderStatus.INCOMPLETE}
  symbol={symbol}
  ref={orderListRef}
/>
<MobileOrderListWidget type={TabType.filled} showFilter filterConfig={{ side: "BUY" }} />
```
