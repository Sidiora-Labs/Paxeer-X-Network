# orderListContext

## Overview

React context for order list actions and validation: cancel order, edit order, edit algo order, and min-notional check. Consumed by cells and edit flows.

## Exports

### `OrderListContextState`

| Property | Type | Description |
|----------|------|-------------|
| `onCancelOrder` | `(order: API.Order \| API.AlgoOrder) => Promise<any>` | Cancel a single order/algo order. |
| `editOrder` | `(id: string, order: OrderEntity) => Promise<any>` | Update a regular order. |
| `editAlgoOrder` | `(id: string, order: OrderEntity) => Promise<any>` | Update an algo order. |
| `checkMinNotional` | `(symbol, price?, qty?) => string \| undefined` | Returns error message if below min notional. |

### `OrderListContext`

`createContext<OrderListContextState>`.

### `useOrderListContext()`

Returns current `OrderListContextState`.

### `OrderListProviderProps`

Props for the provider: `cancelOrder`, `editOrder`, `cancelAlgoOrder`, `editAlgoOrder` (API-style by orderId/symbol or orderId/order).

## Usage example

```tsx
const { onCancelOrder, editOrder, checkMinNotional } = useOrderListContext();
await onCancelOrder(order);
const err = checkMinNotional(symbol, price, qty);
```
