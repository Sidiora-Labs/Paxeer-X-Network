# imr.ts

## imr.ts Responsibilities

Computes **Initial Margin Rate (IMR)** for a symbol: Max(1 / maxLeverage, baseIMR, IMR_Factor × |positionNotional + ordersNotional|^IMR_factor_power). Used for cross-margin initial margin calculations.

## imr.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| IMR | function | Formula | IMR for a symbol. |

## IMR Parameter Table

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| maxLeverage | number | Yes | — | Effective symbol leverage. |
| baseIMR | number | Yes | — | Base IMR. |
| IMR_Factor | number | Yes | — | IMR factor. |
| positionNotional | number | Yes | — | Position notional. |
| ordersNotional | number | Yes | — | Order notional. |
| IMR_factor_power | number | No | IMRFactorPower (4/5) | Exponent. |

## IMR Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal), `../constants` (IMRFactorPower).

## IMR Example

```typescript
import { account } from "@paxeer-network/perp";

const imr = account.IMR({
  maxLeverage: 10,
  baseIMR: 0.1,
  IMR_Factor: 0.0000002512,
  positionNotional: 5197.2,
  ordersNotional: 0,
});
// imr = Max(0.1, 0.1, ...) (number)
```
