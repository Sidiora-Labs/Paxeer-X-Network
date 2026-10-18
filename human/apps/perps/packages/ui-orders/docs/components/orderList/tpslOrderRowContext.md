# tpslOrderRowContext

## Overview

Per-row context for TPSL orders: order, TP/SL trigger prices, order prices, TP/SL PnL, cancel/update handlers, and related position. Used by desktop TPSL cells and edit UI.

## Exports

### `TPSLOrderRowContextState`

| Property | Type | Description |
|----------|------|-------------|
| `order` | `API.AlgoOrderExt` | The algo order. |
| `tp_trigger_price`, `sl_trigger_price` | `number \| undefined` | Trigger prices. |
| `tp_order_price`, `sl_order_price` | `number \| OrderType \| undefined` | Order prices. |
| `tpPnL`, `slPnL` | `number \| undefined` | Computed PnL. |
| `onCancelOrder` | `(order: API.AlgoOrderExt) => Promise<void>` | Cancel algo order. |
| `onUpdateOrder` | `(order, params) => Promise<void>` | Update algo order. |
| `getRelatedPosition` | `(symbol: string) => API.PositionTPSLExt \| undefined` | Get position for symbol. |
| `position` | `API.PositionTPSLExt \| undefined` | Related position when available. |

### `TPSLOrderRowContext`, `useTPSLOrderRowContext()`

Context and hook to read the state.

### `TPSLOrderRowProvider`

`FC<PropsWithChildren<{ order: API.AlgoOrderExt }>>`. Computes trigger prices and PnL via `calcTPSLPnL` (uses hooks for position and mutations), provides state to children.

## Usage example

```tsx
<TPSLOrderRowProvider order={order}>
  <TriggerPriceCell />
  <TP_SLEditButton />
</TPSLOrderRowProvider>
```
