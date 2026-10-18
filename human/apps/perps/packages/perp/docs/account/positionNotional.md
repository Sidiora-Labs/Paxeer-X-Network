# positionNotional.ts

## positionNotional.ts Responsibilities

Helpers for **position notional** and **position quantity with orders** for a single symbol: `positionNotionalWithOrder_by_symbol` (markPrice × positionQtyWithOrders) and `positionQtyWithOrders_by_symbol` (max(|positionQty + buyOrdersQty|, |positionQty − sellOrdersQty|)).

## positionNotional.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| positionNotionalWithOrder_by_symbol | function | Formula | markPrice × positionQtyWithOrders. |
| positionQtyWithOrders_by_symbol | function | Formula | max(|pos + buy|, |pos − sell|). |

## positionNotionalWithOrder_by_symbol Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| markPrice | number | Yes | Mark price. |
| positionQtyWithOrders | number | Yes | From positionQtyWithOrders_by_symbol. |

## positionQtyWithOrders_by_symbol Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positionQty | number | Yes | Current position qty. |
| buyOrdersQty | number | Yes | Total buy order qty. |
| sellOrdersQty | number | Yes | Total sell order qty. |

## Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal).

## Example

```typescript
import {
  positionQtyWithOrders_by_symbol,
  positionNotionalWithOrder_by_symbol,
} from "@paxeer-network/perp";

const qtyWithOrders = positionQtyWithOrders_by_symbol({
  positionQty: 0.2,
  buyOrdersQty: 0.3,
  sellOrdersQty: 0,
});
const notional = positionNotionalWithOrder_by_symbol({
  markPrice: 25986.2,
  positionQtyWithOrders: qtyWithOrders,
});
```
