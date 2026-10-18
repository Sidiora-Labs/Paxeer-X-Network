# tradingView.ts

## tradingView.ts responsibility

Provides TradingView chart copy: time intervals (1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h, 6h, 12h, 1D, 3D, 1W, 1M, more), line types (bars, candles, hollow candles, line, area, baseline), display controls (buy/sell, limit orders, stop orders), and no-script messages for custom TradingView license setup.

## tradingView.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| tradingView | object | Key-value map | Keys under "tradingView.*" |
| TradingView | type | typeof tradingView | Type export |

## tradingView.ts Example

```typescript
t("tradingView.timeInterval.1h");
t("tradingView.lineType.candles");
t("tradingView.noScriptSrc");
```
