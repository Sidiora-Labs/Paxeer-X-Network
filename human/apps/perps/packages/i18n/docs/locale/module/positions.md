# positions.ts

## positions.ts responsibility

Provides position-related copy: close position, funding fee title/tooltip, liquidation, position history, close all (options: all / profit / loss), column labels (liq. price, unreal PnL, margin), limit/market close descriptions and errors, history status and type (closed, ADL, liquidated), liquidation table columns and tooltips, reverse position flow, and adjust margin labels.

## positions.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| positions | object | Key-value map | Keys under "positions.*" |
| Positions | type | typeof positions | Type export |

## positions.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Close | positions.closePosition, positions.closeAll, positions.limitClose.description |
| Columns | positions.column.liqPrice, positions.column.unrealPnl.tooltip |
| History | positions.history.status.closed, positions.history.type.liquidated |
| Reverse | positions.reverse.title, positions.reverse.reverseToLong |
| Adjust margin | positions.adjustMargin.title, positions.adjustMargin.add |

## positions.ts Example

```typescript
t("positions.closePosition");
t("positions.reverse.description");
t("positions.adjustMargin.success");
```
