# groupOrders.ts

## groupOrders.ts Responsibilities

**groupOrdersBySymbol**: groups orders by symbol (symbol → Order[]). **extractSymbols**: returns unique symbols from positions and orders arrays. Used for iterating per-symbol margin and notional calculations.

## groupOrders.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| groupOrdersBySymbol | function | Helper | { [symbol]: API.Order[] }. |
| extractSymbols | function | Helper | Unique symbols from positions and orders. |

## groupOrdersBySymbol Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| orders | API.Order[] | Yes | Orders list. |

## extractSymbols Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positions | Pick<API.Position, "symbol">[] | Yes | Positions (or position-like with symbol). |
| orders | Pick<API.Order, "symbol">[] | Yes | Orders (or order-like with symbol). |

## Dependencies

- **Upstream**: `@paxeer-network/types` (API).

## Example

```typescript
import { groupOrdersBySymbol, extractSymbols } from "@paxeer-network/perp";

const bySymbol = groupOrdersBySymbol(orders);
const symbols = extractSymbols(positions, orders);
```
