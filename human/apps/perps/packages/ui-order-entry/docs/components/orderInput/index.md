# orderInput

## Overview

Main order input area: by order type shows trigger price (stop), limit price (limit/stop limit + BBO), quantity and total, or scaled order inputs, or trailing stop inputs.

## Files

| File | Description |
|------|-------------|
| [OrderInput (index.tsx)](OrderInput.md) | Dispatches to ScaledOrderInput, TrailingStopInput, or PriceInput + TriggerPriceInput + QtyAndTotalInput |
| [limit/](limit/index.md) | PriceInput, limit price suffix, BBO order type select |
| [stop/](stop/index.md) | TriggerPriceInput |
| [qtyAndTotal/](qtyAndTotal/index.md) | QtyAndTotalInput, quantity/total inputs, total type select |
| [scaledOrder/](scaledOrder/index.md) | ScaledOrderInput, start/end price, total orders, skew, distribution |
| [trailingStop/](trailingStop/index.md) | TrailingStopInput, activated price, callback rate/value |
