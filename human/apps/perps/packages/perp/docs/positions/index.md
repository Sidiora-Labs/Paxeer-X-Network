# positions/ — Position-Level Formulas

## Directory Responsibilities

The `positions/` directory contains **position-level** formulas: notional (single and total), unrealized PnL (single and total), unsettlement PnL, liquidation price (cross-margin and isolated), position maintenance margin rate (MMR) and maintenance margin, take profit / stop loss estimates, and max position notional/leverage.

## Boundaries

- **In scope**: Formulas that take one or more positions (and optionally mark prices, collateral, orders) and return notional, PnL, liquidation price, maintenance margin, or TP/SL metrics.
- **Out of scope**: Account-level collateral or free collateral (see `account/`), order placement.

## Key Entities

| Entity | File | Role |
|--------|------|------|
| notional / totalNotional | notional.ts | Single and total position notional. |
| unrealizedPnL / totalUnrealizedPnL / unrealizedPnLROI | unrealizedPnL.ts | Unrealized PnL and ROI. |
| unsettlementPnL / totalUnsettlementPnL | unsettlementPnL.ts | Position and total unsettlement PnL. |
| liqPrice | liqPrice.ts | Cross-margin position liquidation price (binary search). |
| liquidationPriceIsolated | liquidationPriceIsolated.ts | Isolated margin liquidation price. |
| MMR | positionMMR.ts | Position maintenance margin rate. |
| maintenanceMargin | maintenanceMargin.ts | Position maintenance margin. |
| estPnLForTP / estPriceForTP / estOffsetForTP / estPriceFromOffsetForTP / estPnLForSL | takeProfit.ts | TP/SL PnL and price/offset helpers. |
| maxPositionNotional / maxPositionLeverage | maxPosition.ts | Max position notional and max leverage from notional. |

## Files in This Directory

| File | Language | Summary | Entry symbols |
|------|----------|---------|---------------|
| [notional.ts](notional.md) | TypeScript | Notional (single/total) | notional, totalNotional |
| [unrealizedPnL.ts](unrealizedPnL.md) | TypeScript | Unrealized PnL and ROI | unrealizedPnL, totalUnrealizedPnL, unrealizedPnLROI |
| [unsettlementPnL.ts](unsettlementPnL.md) | TypeScript | Unsettlement PnL | unsettlementPnL, totalUnsettlementPnL |
| [liqPrice.ts](liqPrice.md) | TypeScript | Cross-margin liq price | liqPrice |
| [positionMMR.ts](positionMMR.md) | TypeScript | Position MMR | MMR |
| [maintenanceMargin.ts](maintenanceMargin.md) | TypeScript | Position maintenance margin | maintenanceMargin |
| [takeProfit.ts](takeProfit.md) | TypeScript | TP/SL PnL and price/offset | estPnLForTP, estPriceForTP, estOffsetForTP, estPriceFromOffsetForTP, estPnLForSL |
| [maxPosition.ts](maxPosition.md) | TypeScript | Max position notional/leverage | maxPositionNotional, maxPositionLeverage |
| [liquidationPriceIsolated.ts](liquidationPriceIsolated.md) | TypeScript | Isolated liq price | liquidationPriceIsolated |

## Subdirectories

None.
