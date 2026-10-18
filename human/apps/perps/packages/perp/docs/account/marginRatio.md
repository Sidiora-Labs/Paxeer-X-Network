# marginRatio.ts

## marginRatio.ts Responsibilities

Computes **total margin ratio**: total collateral divided by total position notional (sum of |position_qty × mark_price| over positions). Returns 0 if totalCollateral is 0 or total position notional is 0.

## marginRatio.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| totalMarginRatio | function | Formula | totalCollateral / totalPositionNotional. |

## totalMarginRatio Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| totalCollateral | number | Yes | Total collateral value. |
| markPrices | Record<string, number> | Yes | Mark price by symbol. |
| positions | API.Position[] | Yes | Positions. |

## totalMarginRatio Optional Parameter

| Name | Type | Description |
|------|------|-------------|
| dp | number | Optional decimal places for result. |

## totalMarginRatio Dependencies

- **Upstream**: `@paxeer-network/types` (API), `@paxeer-network/utils` (Decimal, zero).

## Example

```typescript
import { totalMarginRatio } from "@paxeer-network/perp";

const ratio = totalMarginRatio(
  { totalCollateral: 1981.66, markPrices: { "BTC-PERP": 25986.2, "ETH-PERP": 1638.41 }, positions },
  4
);
```
