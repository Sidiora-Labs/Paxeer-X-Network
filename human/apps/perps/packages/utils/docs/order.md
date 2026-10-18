# order

## Overview

Order-related helpers: trailing stop price from algo order params, BBO order type from order type/side/level, and TP/SL direction from side, type, and prices.

## Exports

### getTrailingStopPrice

Computes the trailing stop price for an algo order. Uses `callback_value` or `callback_rate` with `extreme_price` and side (long: stop below extreme; short: stop above extreme).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| order | API.AlgoOrderExt | Yes | Algo order with `side`, `extreme_price`, `callback_value`, `callback_rate` |

**Returns:** `number` — trailing stop price, or `0` if `extreme_price` is missing or no callback value/rate.

---

### getBBOType

Maps order type, side, and level to a BBO order type (queue vs counterparty, 1 or 5).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| options.type | OrderType | Yes | ASK or BID |
| options.side | OrderSide | Yes | BUY or SELL |
| options.level | OrderLevel | Yes | ONE or FIVE |

**Returns:** `BBOOrderType` — e.g. `COUNTERPARTY1`, `QUEUE1`, `COUNTERPARTY5`, `QUEUE5`.

---

### getTPSLDirection

Determines direction (`1` or `-1`) for TP/SL based on side, type (`"tp"` or `"sl"`), close price, and order price.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| inputs.side | OrderSide | Yes | BUY or SELL |
| inputs.type | "tp" \| "sl" | Yes | Take profit or stop loss |
| inputs.closePrice | number | Yes | Close price |
| inputs.orderPrice | number | Yes | Order price |

**Returns:** `number` — `1` or `-1`.

## Usage example

```typescript
import { getTrailingStopPrice, getBBOType, getTPSLDirection } from "@paxeer-network/utils";
import { OrderSide, OrderType, OrderLevel } from "@paxeer-network/types";

const stopPrice = getTrailingStopPrice(algoOrder);

const bboType = getBBOType({
  type: OrderType.ASK,
  side: OrderSide.BUY,
  level: OrderLevel.ONE,
});

const dir = getTPSLDirection({
  side: OrderSide.BUY,
  type: "tp",
  closePrice: 100,
  orderPrice: 99,
});
```
