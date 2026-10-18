# orders.script

## Overview

Hook that builds state for the Orders widget and forwards ref to the order list for imperative API (e.g. download).

## Exports

### `UseOrdersScriptOptions`

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `current` | `TabType` | No | Initial tab. |
| `pnlNotionalDecimalPrecision` | `number` | No | PnL/notional decimal precision. |
| `ref` | `ForwardedRef<OrderListInstance>` | Yes | Ref forwarded to order list. |
| `sharePnLConfig` | `SharePnLConfig` | No | Share PnL config. |

### `useOrdersScript(props): OrdersBuilderState`

Returns `{ current, pnlNotionalDecimalPrecision, orderListRef, sharePnLConfig }` and assigns to `ref` an object with `download()` that calls `orderListRef.current?.download?.()`.

### `OrdersBuilderState`

Return type of `useOrdersScript` — state passed to `Orders` UI.

## Usage example

```typescript
const ref = useRef<OrderListInstance>(null);
const state = useOrdersScript({
  current: TabType.filled,
  pnlNotionalDecimalPrecision: 2,
  ref,
});
// state.orderListRef, state.current, etc.
// ref.current?.download();
```
