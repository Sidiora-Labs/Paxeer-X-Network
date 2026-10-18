# freeCollateralUSDCOnly.ts

## freeCollateralUSDCOnly.ts Responsibilities

Computes **Free Collateral (USDC Only)**: the part of free collateral backed only by USDC (and unsettled PnL), i.e. free collateral minus the value of non-USDC collateral (capped holding × index price × discount).

## freeCollateralUSDCOnly.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| freeCollateralUSDCOnly | function | Formula | max(0, freeCollateral − Σ(non-USDC holding value)). |
| FreeCollateralUSDCOnlyInputs | type | Input type | freeCollateral, nonUSDCHolding (holding, indexPrice, collateralCap, collateralRatio)[]. |

## freeCollateralUSDCOnly Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| freeCollateral | Decimal | Yes | From freeCollateral(). |
| nonUSDCHolding | { holding, indexPrice, collateralCap, collateralRatio }[] | Yes | Non-USDC tokens; each contributes min(holding, collateralCap) × indexPrice × collateralRatio. |

## freeCollateralUSDCOnly Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal, zero).

## freeCollateralUSDCOnly Example

```typescript
import { freeCollateralUSDCOnly } from "@paxeer-network/perp";
import { Decimal } from "@paxeer-network/utils";

const usdcOnly = freeCollateralUSDCOnly({
  freeCollateral: new Decimal(550),
  nonUSDCHolding: [{ holding: 1, indexPrice: 2000, collateralCap: 10, collateralRatio: new Decimal(0.9) }],
});
// usdcOnly = max(0, 550 - 1800) = 0 (Decimal)
```
