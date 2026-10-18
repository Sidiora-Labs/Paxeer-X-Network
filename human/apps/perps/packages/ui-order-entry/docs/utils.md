# utils.ts

## Overview

Utility functions for BBO (best bid/offer) order type detection, order level/side mapping, scaled order result messages, and safe number parsing.

## Exports

### BBOStatus (enum)

| Member | Value | Description |
|--------|-------|-------------|
| `ON` | `"on"` | BBO mode on |
| `OFF` | `"off"` | BBO mode off |
| `DISABLED` | `"disabled"` | BBO disabled (e.g. when TPSL or FOK/IOC/Post-only) |

### isBBOOrder(options)

If `order_type` is provided, checks both `order_type` and `order_type_ext`; otherwise only checks `order_type_ext`. Returns whether the order is a BBO (ASK/BID) order.

| Parameter | Type | Description |
|----------|------|-------------|
| `options.order_type` | `OrderType` (optional) | Order type |
| `options.order_type_ext` | `OrderType` (optional) | Order type extension (e.g. ASK, BID) |

**Returns:** `boolean`

### getOrderTypeByBBO(value, size)

Maps BBO order type and side to OrderType (ASK or BID).

| Parameter | Type | Description |
|----------|------|-------------|
| `value` | `BBOOrderType` | BBO type (e.g. COUNTERPARTY1, QUEUE5) |
| `size` | `OrderSide` | BUY or SELL |

**Returns:** `OrderType.ASK` or `OrderType.BID`

### getOrderLevelByBBO(value)

Maps BBO order type to order level (ONE or FIVE).

| Parameter | Type | Description |
|----------|------|-------------|
| `value` | `BBOOrderType` | BBO type |

**Returns:** `OrderLevel.ONE` or `OrderLevel.FIVE`

### getScaledPlaceOrderMessage(result)

Builds i18n message for scaled order submit result (fully successful, all failed, or partially successful).

| Parameter | Type | Description |
|----------|------|-------------|
| `result` | `any` | API result with `data.rows` and `row.success` |

**Returns:** `string` (translated message) or undefined

### safeNumber(val)

Coerces value to number; returns 0 if NaN.

| Parameter | Type | Description |
|----------|------|-------------|
| `val` | `number \| string` | Input value |

**Returns:** `number`

## Usage example

```ts
import {
  BBOStatus,
  isBBOOrder,
  getOrderTypeByBBO,
  getOrderLevelByBBO,
  getScaledPlaceOrderMessage,
  safeNumber,
} from "./utils";
```
