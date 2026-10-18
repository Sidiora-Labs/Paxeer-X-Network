# orderEntry.ts

## orderEntry.ts responsibility

Provides order entry copy: buy/long and sell/short, reduce only, order types (limit, market, stop limit, stop market, post-only, IOC, FOK, scaled, trailing stop), order size and initial margin, scaled order fields (start/end price, skew, total orders/quantity, trailing rate/value), quantity distribution types and descriptions, BBO options, order confirm and hidden order tooltip, max buy/sell, TP/SL (mark price) and tips, est. ROI/PnL, form validation errors (quantity, price, trigger, TP/SL, total, margin, slippage), margin mode (cross/isolated), and reminders (reduce-only, max qty).

## orderEntry.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| orderEntry | object | Key-value map | Keys under "orderEntry.*", "marginMode.*" |
| OrderEntry | type | typeof orderEntry | Type export |

## orderEntry.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Order types | orderEntry.orderType.limit, orderEntry.orderType.trailingStop |
| Scaled | orderEntry.startPrice, orderEntry.skew, orderEntry.totalOrders |
| Validation | orderEntry.orderQuantity.error.min, orderEntry.triggerPrice.error.required |
| Margin mode | marginMode.cross, marginMode.isolatedMarginDescription |

## orderEntry.ts Example

```typescript
t("orderEntry.buyLong");
t("orderEntry.orderType.postOnly.tooltip");
t("orderEntry.orderQuantity.error.required");
```
