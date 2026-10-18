# otherIMs.ts

## otherIMs.ts Responsibilities

Computes **total initial margin used by other symbols** (excluding the current symbol). Used as input to max qty formulas: (total collateral − otherIMs) is the collateral available for the current symbol. Uses position qty and pending long/short qty from positions and IMR per symbol.

## otherIMs.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| otherIMs | function | Formula | Sum of position notional with orders × IMR for all symbols in the given positions list. |

## otherIMs Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positions | API.Position[] | Yes | Positions (other symbols; typically exclude current symbol when used for max qty). |
| markPrices | Record<string, number> | Yes | Mark price by symbol. |
| symbolInfo | any | Yes | base_imr etc. by symbol. |
| IMR_Factors | Record<string, number> | Yes | IMR factor by symbol. |

## otherIMs Dependencies

- **Upstream**: `@paxeer-network/types` (API), `@paxeer-network/utils` (Decimal, zero), positionNotional (positionQtyWithOrders_by_symbol, positionNotionalWithOrder_by_symbol), positionUtils (getQtyFromPositions).

## Example

```typescript
import { otherIMs } from "@paxeer-network/perp";

const others = otherIMs({
  positions: positionsFilteredToOtherSymbols,
  markPrices: { "ETH-PERP": 2000 },
  symbolInfo: getSymbolInfo,
  IMR_Factors: { "ETH-PERP": 0.0000003754 },
});
```
