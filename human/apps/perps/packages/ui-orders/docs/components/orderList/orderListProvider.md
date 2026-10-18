# orderListProvider

## Overview

Provider that implements `OrderListContextState`: maps cancel/edit by order/algo ID and symbol to the parent callbacks, and provides `checkMinNotional` using `getMinNotional` and symbol info.

## Exports

### `OrderListProviderProps`

| Prop | Type | Description |
|------|------|-------------|
| `cancelOrder` | `(orderId: number, symbol: string) => Promise<any>` | Cancel regular order. |
| `editOrder` | `(orderId: string, order: OrderEntity) => Promise<any>` | Update regular order. |
| `cancelAlgoOrder` | `(orderId: number, symbol: string) => Promise<any>` | Cancel algo order. |
| `editAlgoOrder` | `(orderId: string, order: OrderEntity) => Promise<any>` | Update algo order. |

### `OrderListProvider`

`FC<PropsWithChildren<OrderListProviderProps>>`. Wraps children with `OrderListContext.Provider`; value includes `onCancelOrder` (handles root vs child algo), `editOrder`, `editAlgoOrder`, and `checkMinNotional`.

## Usage example

```tsx
<OrderListProvider
  cancelOrder={cancelOrder}
  editOrder={updateOrder}
  cancelAlgoOrder={cancelAlgoOrder}
  editAlgoOrder={updateAlgoOrder}
>
  {children}
</OrderListProvider>
```
