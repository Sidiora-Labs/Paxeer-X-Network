# useTPSLAdvanced.script

## Overview

Hook that wires an existing order into `useOrderEntry` for advanced TPSL editing, runs `useTpslPriceChecker` for SL price, and provides validation + submit that calls `onSubmit(formattedOrder)` on success.

## Exports

### useTPSLAdvanced(props)

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| order | `PaxeerNetworkOrder` | Yes | Order to edit |
| setOrderValue | `(key: string, value: any) => void` | Yes | Set a single field on the order |
| onSubmit | `(formattedOrder: PaxeerNetworkOrder) => void` | Yes | Called with formatted order on valid submit |
| onClose | `() => void` | Yes | Close callback |
| symbolLeverage | `number` | No | Leverage for display |

**Returns:** order, formattedOrder, symbolInfo, slPriceError, estLiqPrice, estLiqPriceDistance, setValue, setValues, onSubmit (wrapped with validation), onClose, metaState, symbolLeverage.

- Uses `useOrderEntry(order.symbol, { initialOrder: ... })` with TP/SL fields from `order`.
- On submit, validates (including SL price error); on success calls `props.onSubmit(formattedOrder)`.

## Usage example

```tsx
const state = useTPSLAdvanced({
  order,
  setOrderValue,
  onSubmit: (next) => { updateOrder(next); onClose(); },
  onClose,
});
```
