# totalValue.ts

## totalValue.ts Responsibilities

Computes **Total Value** of the user account (USDC-denominated): USDC holding + non-USDC holdings (at index price) + total isolated position margin + total unsettlement PnL.

## totalValue.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| totalValue | function | Formula | Total value = USDCHolding + non-USDC value + totalIsolatedPositionMargin + totalUnsettlementPnL. |
| TotalValueInputs | type | Input type | Input shape for totalValue (totalUnsettlementPnL, USDCHolding, nonUSDCHolding, totalIsolatedPositionMargin?). |

## totalValue Input and Output

- **Input**: totalUnsettlementPnL, USDCHolding, nonUSDCHolding (holding, indexPrice)[], totalIsolatedPositionMargin (optional, default 0).
- **Output**: `Decimal`.

## totalValue Parameter Table

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| totalUnsettlementPnL | number | Yes | — | Total unsettled PnL (cross + isolated). |
| USDCHolding | number | Yes | — | USDC holding quantity. |
| nonUSDCHolding | { holding, indexPrice }[] | Yes | — | Non-USDC holdings and index prices. |
| totalIsolatedPositionMargin | number | No | 0 | Sum of isolated position margins. |

## totalValue Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal, zero).

## totalValue Example

```typescript
import { totalValue } from "@paxeer-network/perp";

const value = totalValue({
  totalUnsettlementPnL: -18.34,
  USDCHolding: 2000,
  nonUSDCHolding: [{ holding: 1000, indexPrice: 1.001 }],
  totalIsolatedPositionMargin: 500,
});
// value = 2000 + 1001 + 500 - 18.34 = 3482.66 (Decimal)
```
