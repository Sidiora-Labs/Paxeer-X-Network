# freeCollateral.ts

## freeCollateral.ts Responsibilities

Computes **Free Collateral** for cross margin: total collateral value minus total initial margin with orders (cross margin only). Result is floored at zero.

## freeCollateral.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| freeCollateral | function | Formula | Free collateral = max(0, totalCollateral − totalInitialMarginWithOrders). |
| FreeCollateralInputs | type | Input type | totalCollateral (Decimal), totalInitialMarginWithOrders (number). |

## freeCollateral Input and Output

- **Input**: totalCollateral (Decimal), totalInitialMarginWithOrders (number).
- **Output**: `Decimal` (≥ 0).

## freeCollateral Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| totalCollateral | Decimal | Yes | Total collateral value. |
| totalInitialMarginWithOrders | number | Yes | Total initial margin including orders (cross only). |

## freeCollateral Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal, zero). totalCollateral from totalCollateral(); totalInitialMarginWithOrders from totalInitialMarginWithQty() (cross only).

## freeCollateral Example

```typescript
import { freeCollateral } from "@paxeer-network/perp";
import { Decimal } from "@paxeer-network/utils";

const free = freeCollateral({
  totalCollateral: new Decimal(2050),
  totalInitialMarginWithOrders: 1500,
});
// free = 550 (Decimal)
```
