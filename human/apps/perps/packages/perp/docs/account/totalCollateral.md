# totalCollateral.ts

## totalCollateral.ts Responsibilities

Computes **Total Collateral** (USDC-denominated): USDC part (holding + pendingShortQty − isolatedOrderFrozen) + non-USDC holdings (capped, with collateralRatio and indexPrice) + unsettlement PnL. Optional totalCrossUnsettledPnL overrides unsettlement PnL for free-collateral-style calculations.

## totalCollateral.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| totalCollateral | function | Formula | Total collateral value. |

## totalCollateral Parameter Table

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| USDCHolding | number | Yes | — | USDC holding. |
| nonUSDCHolding | { holding, indexPrice, collateralCap, collateralRatio }[] | Yes | — | Non-USDC; value = min(holding, cap) × ratio × indexPrice. |
| unsettlementPnL | number | Yes | — | Total unsettled PnL. |
| usdcBalancePendingShortQty | number | No | 0 | USDC frozen for pending short. |
| usdcBalanceIsolatedOrderFrozen | number | No | 0 | USDC frozen for isolated orders. |
| totalCrossUnsettledPnL | number | No | — | If set, used instead of unsettlementPnL. |

## totalCollateral Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal, zero).

## totalCollateral Example

```typescript
import { totalCollateral } from "@paxeer-network/perp";
import { Decimal } from "@paxeer-network/utils";

const collateral = totalCollateral({
  USDCHolding: 2000,
  nonUSDCHolding: [{ holding: 1000, indexPrice: 1.001, collateralCap: 1000, collateralRatio: new Decimal(0) }],
  unsettlementPnL: -18.34,
});
// collateral = 2000 + 0 - 18.34 = 1981.66 (Decimal)
```
