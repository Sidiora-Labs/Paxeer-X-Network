# positionsRowContext

## Overview

React context for a single position row: quantity, price, order type, side, position data, close order state, submit handler, and TP/SL algo orders.

## Exports

### PositionsRowContextState (interface)

| Property | Type | Description |
| -------- | ----- | ----------- |
| `quantity` | `string` | Close quantity input. |
| `price` | `string` | Limit price (when type is Limit). |
| `type` | `OrderType` | Market or Limit. |
| `side` | `OrderSide` | Derived from position. |
| `position` | `API.PositionExt \| API.PositionTPSLExt` | Current row position. |
| `updateQuantity` | `(value: string) => void` | Update quantity (with symbol rules). |
| `updatePriceChange` | `(value: string) => void` | Update price. |
| `updateOrderType` | `(value: OrderType) => void` | Set type (and reset price for Market). |
| `closeOrderData` | `any` | From usePositionClose. |
| `onSubmit` | `() => Promise<any>` | Submit close order. |
| `submitting` | `boolean` | Submit in progress. |
| `tpslOrder` | `API.AlgoOrder \| undefined` | Full TP/SL order. |
| `partialTPSLOrder` | `API.AlgoOrder \| undefined` | Partial TP/SL. |
| `quoteDp`, `baseDp`, `baseTick` | `number \| undefined` | Symbol formatting. |
| `errors` | `any` | Validation/API errors. |

### PositionsRowContext

- React context instance.

### usePositionsRowContext()

- **Returns**: Current `PositionsRowContextState`.

## Usage example

```tsx
const { quantity, updateQuantity, onSubmit, position } = usePositionsRowContext();
```
