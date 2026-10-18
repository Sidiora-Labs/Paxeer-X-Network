# tpslInputRow

## Overview

Single row for TP or SL: trigger price, PnL/Offset/Offset%/ROI inputs, order type (Market/Limit), and order price. Composes `useTPSLInputRowScript` and `TPSLInputRowUI`. Used by position TPSL and advanced TPSL forms.

## Exports

### TPSLInputRowWidget

React component. Props = `Props` from script.

### useTPSLInputRowScript(props)

**Props:** type (`"tp" \| "sl"`), values (trigger_price, PnL, Offset, Offset%, order_price, order_type), onChange, rootOrderPrice, symbol, side, quote_dp, positionType, errors, hideOrderPrice, disableOrderTypeSelector, symbolLeverage, inputWarnNode, etc.

**Returns:** props plus `roi` (computed ROI from `order.tpslROI` when applicable).

### TPSLInputRowUI

Presentational component; receives `ReturnType<typeof useTPSLInputRowScript>`.

## Usage example

```tsx
<TPSLInputRowWidget
  type="tp"
  symbol={symbol}
  side={OrderSide.BUY}
  rootOrderPrice={averageOpenPrice}
  values={{ trigger_price, PnL, order_price, order_type: OrderType.MARKET, ... }}
  quote_dp={2}
  positionType={PositionType.PARTIAL}
  errors={errors}
  onChange={setOrderValue}
/>
```
