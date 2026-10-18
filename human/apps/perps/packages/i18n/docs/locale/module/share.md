# share.ts

## share.ts responsibility

Provides PnL share copy: share PnL, long/short labels, PnL display format (ROI & PnL, ROI, PnL), optional info to share (open/close price and time, message placeholder and max length), and copy image success/failed messages.

## share.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| share | object | Key-value map | Keys under "share.pnl.*" |
| Share | type | typeof share | Type export |

## share.ts Example

```typescript
t("share.pnl.sharePnl");
t("share.pnl.displayFormat.roi&Pnl");
t("share.pnl.optionalInfo.message.placeholder");
```
