# maxQtyIsolated.ts

## maxQtyIsolated.ts Responsibilities

Computes **maximum tradeable quantity for isolated margin**: considers available balance, leverage, current order reference price, position qty, pending long/sell orders, frozen margin, and symbol max notional. For same-direction or no position uses a simplified formula; for opposite direction (e.g. buy when short) uses a binary search (max 30 iterations) to find max qty such that frozen margin ≤ available balance and open notional ≤ max notional.

## maxQtyIsolated.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| maxQtyForIsolatedMargin | function | Formula | Max tradeable qty for isolated margin. |

## maxQtyForIsolatedMargin Parameter Table (key fields)

| Name | Type | Required | Description |
|------|------|----------|-------------|
| symbol | string | Yes | Symbol. |
| orderSide | OrderSide | Yes | BUY or SELL. |
| currentOrderReferencePrice | number | Yes | Reference price for the new order. |
| availableBalance | number | Yes | USDC available for isolated. |
| leverage | number | Yes | Isolated leverage. |
| baseIMR | number | Yes | Base IMR. |
| IMR_Factor | number | Yes | IMR factor. |
| markPrice | number | Yes | Mark price. |
| positionQty | number | Yes | Current position qty. |
| pendingLongOrders | { referencePrice, quantity }[] | Yes | Pending long orders (excl. current). |
| pendingSellOrders | { referencePrice, quantity }[] | Yes | Pending sell orders (excl. current). |
| isoOrderFrozenLong | number | Yes | Frozen margin for long orders. |
| isoOrderFrozenShort | number | Yes | Frozen margin for short orders. |
| symbolMaxNotional | number | Yes | Max notional for symbol. |
| epsilon | number | No | Precision for binary search (default 1). |

## maxQtyForIsolatedMargin Execution Flow

1. Compute max_notional = min((1/(leverage×IMR_Factor))^(5/4), symbolMaxNotional).
2. BUY: if positionQty ≥ 0 use simplified (max by balance and by notional); else binary search.
3. SELL: if positionQty ≤ 0 use simplified; else binary search.
4. Binary search: interval [left, right]; mid = (left+right)/2; if frozen ≤ balance and open notional ≤ max notional then left = mid else right = mid; early exit on precision.

## Dependencies

- **Upstream**: `@paxeer-network/types` (OrderSide), `@paxeer-network/utils` (Decimal).

## Example

```typescript
import { maxQtyForIsolatedMargin } from "@paxeer-network/perp";
import { OrderSide } from "@paxeer-network/types";

const maxQty = maxQtyForIsolatedMargin({
  symbol: "BTC-PERP",
  orderSide: OrderSide.BUY,
  currentOrderReferencePrice: 99900,
  availableBalance: 1000,
  leverage: 25,
  baseIMR: 0.04,
  IMR_Factor: 0.0000001,
  markPrice: 100000,
  positionQty: 5,
  pendingLongOrders: [],
  pendingSellOrders: [],
  isoOrderFrozenLong: 0,
  isoOrderFrozenShort: 0,
  symbolMaxNotional: 10059467.44,
});
```
