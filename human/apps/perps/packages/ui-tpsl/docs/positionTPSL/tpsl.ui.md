# tpsl.ui

## Overview

Presentational TPSL form: order info, position type (Full/Partial), quantity (for partial), TP/SL input rows, PnL summary, and Cancel/Confirm buttons. Consumes state from `useTPSLBuilder` and displays validation errors and SL price warnings.

## Exports

### TPSLProps

| Name | Type | Required | Description |
|------|------|----------|-------------|
| close | `() => void` | No | Programmatic close |
| onCancel | `() => void` | No | Cancel callback |
| onComplete | `() => void` | No | Success callback |
| withTriggerPrice | `boolean` | No | Whether trigger price is pre-filled |

### TPSL

React component that renders the full TPSL form. Receives `TPSLBuilderState & TPSLProps`: symbolInfo, TPSL_OrderEntity, setOrderValue, setQuantity, errors, validated, status, position, estLiqPrice, slPriceError, etc. Renders:

- Order info (symbol, qty, price, leverage)
- Position type selector (when not editing)
- Quantity input for partial position
- TP and SL input rows via `TPSLInputRowWidget`
- PnL info and Cancel/Confirm buttons with loading state

## Usage example

```tsx
// Usually used via TPSLWidget; direct usage:
const state = useTPSLBuilder(options);
<TPSL {...state} onCancel={hide} onComplete={onComplete} />
```
