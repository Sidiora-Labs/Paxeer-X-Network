# account/ — Account-Level Formulas

## Directory Responsibilities

The `account/` directory contains **account-level** margin and collateral formulas for perp trading: total value, total collateral, free collateral (and USDC-only variant), initial margin (IMR), maintenance margin ratio (MMR), max quantity (cross and isolated), margin ratio, leverage, available balance, LTV, max withdrawal, and max add/reduce for isolated margin.

## Boundaries

- **In scope**: Formulas that take holdings, positions, orders, mark prices, and symbol/IMR config and return a single account-level or per-symbol metric (value, collateral, margin, qty, ratio).
- **Out of scope**: Position-level PnL or liquidation price (see `positions/`), order execution, WebSocket data.

## Key Entities

| Entity | File | Role |
|--------|------|------|
| totalValue | totalValue.ts | Total account value (USDC + non-USDC holdings + isolated margin + unsettlement PnL). |
| totalCollateral | totalCollateral.ts | Total collateral (USDC + non-USDC discounted + unsettlement PnL). |
| freeCollateral | freeCollateral.ts | Free collateral = total collateral − total initial margin with orders (cross only). |
| freeCollateralUSDCOnly | freeCollateralUSDCOnly.ts | Free collateral backed only by USDC (minus non-USDC collateral value). |
| IMR | imr.ts | Initial margin rate for a symbol. |
| totalInitialMarginWithQty | initialMargin.ts | Total initial margin with orders (cross margin only). |
| otherIMs | otherIMs.ts | Total initial margin used by other symbols (for max qty). |
| maxQty / maxQtyByLong / maxQtyByShort | maxQty.ts | Max order quantity (cross margin). |
| maxQtyForIsolatedMargin | maxQtyIsolated.ts | Max tradeable quantity for isolated margin. |
| totalMarginRatio | marginRatio.ts | Total margin ratio (collateral / total position notional). |
| currentLeverage | leverage.ts | Current account leverage from total margin ratio. |
| totalUnrealizedROI | unrealizedROI.ts | Total unrealized ROI. |
| availableBalance / availableBalanceForIsolatedMargin | availableBalance.ts | Available balance; isolated variant for isolated margin. |
| MMR | mmr.ts | Total maintenance margin ratio. |
| collateralRatio / collateralContribution | collateral.ts | Collateral ratio and contribution for non-USDC. |
| LTV | ltv.ts | LTV calculation. |
| maxWithdrawalUSDC / maxWithdrawalOtherCollateral / calcMinimumReceived | maxWithdrawal.ts | Max withdrawal (USDC / other collateral) and min received with slippage. |
| maxAdd / maxReduce | maxAddReduce.ts | Max margin add/reduce for isolated position. |
| positionNotionalWithOrder_by_symbol / positionQtyWithOrders_by_symbol | positionNotional.ts | Position notional and qty with orders by symbol. |
| buyOrdersFilter_by_symbol / sellOrdersFilter_by_symbol | ordersFilter.ts | Filter orders by symbol and side. |
| getQtyFromPositions / getQtyFromOrdersBySide / getPositonsAndOrdersNotionalBySymbol | positionUtils.ts | Get position qty, order qty by side, and notional by symbol. |
| groupOrdersBySymbol / extractSymbols | groupOrders.ts | Group orders by symbol; extract unique symbols from positions and orders. |

## Files in This Directory

| File | Language | Summary | Entry symbols |
|------|----------|---------|---------------|
| [totalValue.ts](totalValue.md) | TypeScript | Total value formula | totalValue, TotalValueInputs |
| [freeCollateral.ts](freeCollateral.md) | TypeScript | Free collateral | freeCollateral, FreeCollateralInputs |
| [freeCollateralUSDCOnly.ts](freeCollateralUSDCOnly.md) | TypeScript | Free collateral USDC only | freeCollateralUSDCOnly |
| [totalCollateral.ts](totalCollateral.md) | TypeScript | Total collateral | totalCollateral |
| [positionNotional.ts](positionNotional.md) | TypeScript | Position notional/qty with orders | positionNotionalWithOrder_by_symbol, positionQtyWithOrders_by_symbol |
| [imr.ts](imr.md) | TypeScript | IMR for symbol | IMR |
| [ordersFilter.ts](ordersFilter.md) | TypeScript | Filter orders by symbol/side | buyOrdersFilter_by_symbol, sellOrdersFilter_by_symbol |
| [positionUtils.ts](positionUtils.md) | TypeScript | Position/order qty and notional helpers | getQtyFromPositions, getQtyFromOrdersBySide, getPositonsAndOrdersNotionalBySymbol |
| [initialMargin.ts](initialMargin.md) | TypeScript | Total initial margin with orders (cross) | totalInitialMarginWithQty |
| [groupOrders.ts](groupOrders.md) | TypeScript | Group orders by symbol; extract symbols | groupOrdersBySymbol, extractSymbols |
| [otherIMs.ts](otherIMs.md) | TypeScript | Other symbols' initial margin | otherIMs |
| [maxQty.ts](maxQty.md) | TypeScript | Max order qty (cross) | maxQty, maxQtyByLong, maxQtyByShort, MaxQtyInputs |
| [maxQtyIsolated.ts](maxQtyIsolated.md) | TypeScript | Max qty isolated margin | maxQtyForIsolatedMargin |
| [marginRatio.ts](marginRatio.md) | TypeScript | Total margin ratio | totalMarginRatio |
| [unrealizedROI.ts](unrealizedROI.md) | TypeScript | Total unrealized ROI | totalUnrealizedROI |
| [leverage.ts](leverage.md) | TypeScript | Current leverage | currentLeverage |
| [availableBalance.ts](availableBalance.md) | TypeScript | Available balance (cross/isolated) | availableBalance, availableBalanceForIsolatedMargin |
| [mmr.ts](mmr.md) | TypeScript | Total MMR | MMR |
| [collateral.ts](collateral.md) | TypeScript | Collateral ratio and contribution | collateralRatio, collateralContribution |
| [ltv.ts](ltv.md) | TypeScript | LTV | LTV |
| [maxWithdrawal.ts](maxWithdrawal.md) | TypeScript | Max withdrawal and min received | maxWithdrawalUSDC, maxWithdrawalOtherCollateral, calcMinimumReceived |
| [maxAddReduce.ts](maxAddReduce.md) | TypeScript | Max add/reduce isolated margin | maxAdd, maxReduce |

## Subdirectories

None.
