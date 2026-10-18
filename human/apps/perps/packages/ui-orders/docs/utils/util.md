# util

## Overview

Order-related helpers: badge parsing by order/algo type, status and gray-cell logic, bracket TP/SL PnL, date comparison, trailing-stop detection, notional calculation, and API order type to order-entry type conversion.

## Exports

### `upperCaseFirstLetter(str: string): string`

Returns the string with the first character uppercased and the rest lowercased.

### `parseBadgesFor(record: any): undefined | string[]`

Builds a list of badge labels for an order/algo record (e.g. Limit, Market, SL, TP, Trailing Stop, Post Only). Uses `record.type`, `record.algo_type`, `record.parent_algo_type`, and `record.child_orders`; returns i18n keys via `i18n.t()`.

### `grayCell(record: any): boolean`

Returns whether the row should be styled as gray (cancelled order or cancelled algo order).

### `getOrderStatus(record: any)`

Returns `record.status` (order) or `record.algo_status` (algo order).

### `isGrayCell(status?: string): boolean`

Returns whether the given status is cancelled.

### `calcBracketRoiAndPnL(order: API.AlgoOrderExt)`

Finds TP/SL child orders of a bracket order and computes TP/SL PnL from trigger price and entry price. Returns `{ pnl: { tpPnL, slPnL } }`.

### `areDatesEqual(date1?: Date, date2?: Date): boolean`

Compares only year, month, and date (ignores time).

### `isTrailingStopOrder(order: API.AlgoOrderExt): boolean`

Returns whether the order is a trailing-stop algo order.

### `getNotional(order: API.AlgoOrderExt, dp?: number)`

Returns `price * quantity` formatted with given decimal places (default 2), or `0` if missing price/quantity.

### `convertApiOrderTypeToOrderEntryType(order: API.AlgoOrderExt)`

Maps API order/algo type to order-entry type (e.g. trailing stop unchanged; non-bracket algo → `STOP_${order.type}`; otherwise `order.type`).

## Usage example

```typescript
import {
  parseBadgesFor,
  grayCell,
  getOrderStatus,
  calcBracketRoiAndPnL,
  getNotional,
  convertApiOrderTypeToOrderEntryType,
} from "@paxeer-network/ui-orders"; // from internal path utils/util

const badges = parseBadgesFor(order);
const isGray = grayCell(order);
const { pnl } = calcBracketRoiAndPnL(bracketOrder);
const notional = getNotional(order, 2);
const entryType = convertApiOrderTypeToOrderEntryType(order);
```
