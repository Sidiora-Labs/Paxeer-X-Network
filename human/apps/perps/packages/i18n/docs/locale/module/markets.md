# markets.ts

## markets.ts responsibility

Provides markets list copy: favorites, recent, new listings, all markets, open interest, top gainers/losers, search placeholder, favorites dropdown and tabs, column labels (market, 24h change/volume, last, mark, index, OI), funding comparison columns, and symbol info bar tooltips (last price, mark, index, volume, pred funding rate).

## markets.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| markets | object | Key-value map | Keys under "markets.*" |
| Markets | type | typeof markets | Type export |

## markets.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Lists | markets.favorites, markets.recent, markets.allMarkets |
| Columns | markets.column.market, markets.column.24hChange |
| Funding | markets.funding.column.estFunding, markets.funding.column.lastFunding |
| Symbol bar | markets.symbolInfoBar.lastPrice.tooltip, markets.symbolInfoBar.Mark.tooltip |

## markets.ts Example

```typescript
t("markets.search.placeholder");
t("markets.column.market&Volume");
t("markets.symbolInfoBar.predFundingRate.tooltip");
```
