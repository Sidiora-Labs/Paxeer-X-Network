# tpsl.ts

## tpsl.ts responsibility

Provides TP/SL copy: mode (full/partial), TP/SL price and trigger, PnL and offset, add/cancel all, drag to set, order/trigger prices, position TP/SL and entire position, est. PnL, take profit/stop loss, cancel confirm, advanced settings, TP/SL order confirm, position type tips, advanced ROI text, total est. TP/SL PnL, risk reward ratio, and validation messages (required, min/max, price vs order price, close to liq. price, cross liq. price).

## tpsl.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| tpsl | object | Key-value map | Keys under "tpsl.*" |
| TPSL | type | typeof tpsl | Type export |

## tpsl.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Labels | tpsl.tp, tpsl.sl, tpsl.tpPrice, tpsl.slTriggerPrice |
| Position type | tpsl.positionType.full.tips, tpsl.positionType.partial.tips |
| Validation | tpsl.validate.tpOrderPrice.error.required, tpsl.validate.slTriggerPrice.warning.closeToLiqPrice |

## tpsl.ts Example

```typescript
t("tpsl.takeProfit");
t("tpsl.positionType.full.tips");
t("tpsl.validate.tpTriggerPrice.error.required");
```
