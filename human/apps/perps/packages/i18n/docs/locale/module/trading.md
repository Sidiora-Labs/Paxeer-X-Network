# trading.ts

## trading.ts responsibility

Provides trading UI copy: layout options (advanced, markets, left/right/top/bottom), order book and last trades, portfolio settings (decimal precision, unreal PnL basis, reverse button), order book columns and tooltips (mark price, spread, middle price), faucet, asset/margin labels and formulas (free collateral, margin ratio, maintenance margin, leverage), risk rate, funding rate, and RWA market hours messaging.

## trading.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| trading | object | Key-value map | Keys under "trading.*" |
| Trading | type | typeof trading | Type export |

## trading.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Layout | trading.layout, trading.layout.advanced, trading.layout.markets |
| Order book | trading.orderBook, trading.orderBook.markPrice.tooltip |
| Asset & margin | trading.asset.freeCollateral, trading.asset.marginRatio.formula |
| Funding | trading.fundingRate.predFundingRate, trading.fundingRate.estimatedFundingFee |
| RWA | trading.rwa.marketHours, trading.rwa.tooltip.description.open |

## trading.ts Example

```typescript
t("trading.orderBook");
t("trading.asset.freeCollateral.tooltip");
t("trading.fundingRate.predFundingRate");
```
