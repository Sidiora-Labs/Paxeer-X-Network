# maxQty.ts

## maxQty.ts Responsibilities

Computes **max order quantity** for cross margin: **maxQtyByLong** and **maxQtyByShort** from total collateral, other IMs, leverage, base IMR, mark price, position/order qtys, IMR factor, and taker fee. **maxQty** dispatches by side (BUY → maxQtyByLong, SELL → maxQtyByShort). Result is capped by baseMaxQty and two formula factors (leverage-based and IMR-factor-based); reduce-only handling is not implemented in this file (caller should enforce).

## maxQty.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| maxQty | function | Dispatcher | maxQtyByLong or maxQtyByShort by side. |
| maxQtyByLong | function | Formula | Max long quantity. |
| maxQtyByShort | function | Formula | Max short quantity. |
| MaxQtyInputs | type | Input | baseMaxQty, totalCollateral, otherIMs, maxLeverage, baseIMR, markPrice, positionQty, buyOrdersQty, sellOrdersQty, IMR_Factor, takerFeeRate. |
| ResultOptions | type | Options | e.g. dp for decimal places. |

## maxQty / maxQtyByLong / maxQtyByShort Parameter Table (key fields)

| Name | Type | Required | Description |
|------|------|----------|-------------|
| baseMaxQty | number | Yes | Symbol base max qty (/v1/public/info.base_max). |
| totalCollateral | number | Yes | From totalCollateral. |
| otherIMs | number | Yes | From otherIMs (other symbols’ IM). |
| maxLeverage | number | Yes | Symbol leverage. |
| baseIMR | number | Yes | Base IMR. |
| markPrice | number | Yes | Mark price. |
| positionQty | number | Yes | Current position qty. |
| buyOrdersQty | number | Yes | Long orders qty. |
| sellOrdersQty | number | Yes | Short orders qty (long only uses for short formula). |
| IMR_Factor | number | Yes | IMR factor. |
| takerFeeRate | number | Yes | Taker fee rate. |

## maxQty Errors and Edge Cases

| Scenario | Condition | Behavior |
|----------|-----------|----------|
| totalCollateral === 0 | — | Returns 0. |
| IMR_Factor === 0 | — | Only factor_1 used (no factor_2). |
| try/catch | Any throw | Returns 0. |

## Dependencies

- **Upstream**: `@paxeer-network/types` (OrderSide), `@paxeer-network/utils` (Decimal).

## Example

```typescript
import { maxQty, maxQtyByLong, MaxQtyInputs } from "@paxeer-network/perp";
import { OrderSide } from "@paxeer-network/types";

const inputs: MaxQtyInputs = {
  symbol: "BTC-PERP",
  baseMaxQty: 20,
  totalCollateral: 1981.66,
  otherIMs: 491.523,
  maxLeverage: 10,
  baseIMR: 0.1,
  markPrice: 25986.2,
  positionQty: 0.2,
  buyOrdersQty: 0.3,
  sellOrdersQty: 0,
  IMR_Factor: 0.0000002512,
  takerFeeRate: 8,
};
const maxLong = maxQtyByLong(inputs);
const maxShort = maxQtyByShort(inputs);
const max = maxQty(OrderSide.BUY, inputs);
```
