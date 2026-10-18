# orderList.script

## Overview

Core order list logic: `useOrderListScript` wires order stream, pagination, filters (side, status, date range), cancel/edit/cancel-all, and ref-based download. Also exports filter helpers and date-range formatting.

## Exports

### `OrderListInstance`

| Method | Description |
|--------|-------------|
| `download?` | Triggers table export (filename with timestamp). |

### `useOrderListScriptOptions`

| Property | Type | Description |
|----------|------|-------------|
| `type` | `TabType` | Tab type. |
| `ordersStatus` | `OrderStatus` | Optional status filter. |
| `symbol` | `string` | Optional symbol filter. |
| `enableLoadMore` | `boolean` | Use load-more instead of server pagination. |
| `onSymbolChange` | `(symbol: API.Symbol) => void` | Symbol change callback. |
| `sharePnLConfig` | `SharePnLConfig` | Share PnL config. |
| `filterConfig` | `{ side?, range? }` | Initial side/date filter. |
| `pnlNotionalDecimalPrecision` | `number` | PnL/notional precision. |
| `ref` | `ForwardedRef<OrderListInstance>` | Ref for download. |

### `useOrderListScript(props)`

Returns state for order list UI: `dataSource`, `isLoading`, `loadMore`, `cancelOrder`, `updateOrder`, `cancelAlgoOrder`, `updateAlgoOrder`, `pagination`, `manualPagination`, `filterItems`, `onFilter`, `onCancelAll`, `onSymbolChange`, `sharePnLConfig`, `symbolsInfo`, `filterDays`, `updateFilterDays`, `symbol`, etc. Uses `useOrderStream`, `usePagination`, and internal `useFilter`.

### `OrdersBuilderState`, `FilterState`

Return types of `useOrderListScript` and internal `useFilter`.

### `parseDateRangeForFilter`, `formatDatePickerRange`

Helpers for date range normalization and picker range formatting.

## Usage example

```typescript
const state = useOrderListScript({
  type: TabType.filled,
  ordersStatus: OrderStatus.FILLED,
  symbol: "PERP_BTC_USDC",
  ref: orderListRef,
});
// state.dataSource, state.onCancelAll, state.pagination, etc.
```
