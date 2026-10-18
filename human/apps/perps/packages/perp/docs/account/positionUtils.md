# positionUtils.ts

## positionUtils.ts Responsibilities

Utilities for **position and order quantities** and **notional by symbol**: get position qty for a symbol from positions list; get order qty by side for a symbol; get combined position + orders notional for a symbol (using mark price).

## positionUtils.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| getQtyFromPositions | function | Helper | Position qty for symbol from positions array (0 if not found). |
| getQtyFromOrdersBySide | function | Helper | Sum of order quantities for symbol and side (BUY/SELL). |
| getPositonsAndOrdersNotionalBySymbol | function | Helper | \|positionQty × markPrice + (buyQty + sellQty) × markPrice\|. |

## getQtyFromPositions Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positions | API.Position[] | Yes | Positions list. |
| symbol | string | Yes | Symbol. |

## getQtyFromOrdersBySide Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| orders | API.Order[] | Yes | Orders list. |
| symbol | string | Yes | Symbol. |
| side | OrderSide | Yes | BUY or SELL. |

## getPositonsAndOrdersNotionalBySymbol Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positions | API.Position[] | Yes | Positions. |
| orders | API.Order[] | Yes | Orders. |
| symbol | string | Yes | Symbol. |
| markPrice | number | Yes | Mark price. |

## Dependencies

- **Upstream**: `@paxeer-network/types` (API, OrderSide), `@paxeer-network/utils` (Decimal), ordersFilter (buy/sell by symbol).

## Example

```typescript
import {
  getQtyFromPositions,
  getQtyFromOrdersBySide,
  getPositonsAndOrdersNotionalBySymbol,
} from "@paxeer-network/perp";
import { OrderSide } from "@paxeer-network/types";

const positionQty = getQtyFromPositions(positions, "BTC-PERP");
const buyQty = getQtyFromOrdersBySide(orders, "BTC-PERP", OrderSide.BUY);
const notional = getPositonsAndOrdersNotionalBySymbol({
  positions,
  orders,
  symbol: "BTC-PERP",
  markPrice: 50000,
});
```
