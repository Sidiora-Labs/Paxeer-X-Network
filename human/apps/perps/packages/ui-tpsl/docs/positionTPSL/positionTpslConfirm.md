# positionTpslConfirm

## Overview

Content component for the TPSL submit confirmation modal: shows symbol, side, position type, order qty, TP/SL trigger and order prices, and a “Don’t ask again” checkbox backed by `useLocalStorage("paxeer-network_order_confirm")`.

## Exports

### PositionTPSLConfirmProps

| Name | Type | Required | Description |
|------|------|----------|-------------|
| symbol | `string` | Yes | Symbol |
| qty | `number` | Yes | Order quantity |
| tpPrice | `number` | No | TP trigger price |
| slPrice | `number` | No | SL trigger price |
| maxQty | `number` | Yes | Max position qty |
| side | `OrderSide` | Yes | Order side |
| baseDP | `number` | Yes | Base decimals |
| quoteDP | `number` | Yes | Quote decimals |
| isEditing | `boolean` | No | Edit mode |
| isPositionTPSL | `boolean` | No | Position-level TPSL (full) |
| orderInfo | `ComputedAlgoOrder` | Yes | Full order for display |

### PositionTPSLConfirm

React component that renders the confirm dialog body (badges, position type, order qty, TP/SL prices, checkbox).

## Usage example

```tsx
<PositionTPSLConfirm
  symbol={order.symbol}
  qty={Number(order.quantity)}
  maxQty={maxQty}
  tpPrice={Number(order.tp_trigger_price)}
  slPrice={Number(order.sl_trigger_price)}
  side={order.side}
  baseDP={2}
  quoteDP={2}
  orderInfo={order}
/>
```
