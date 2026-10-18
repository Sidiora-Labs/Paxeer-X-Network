# ordersFilter.ts

## ordersFilter.ts Responsibilities

Filters orders by symbol and side: **buyOrdersFilter_by_symbol** returns orders for the given symbol with BUY side; **sellOrdersFilter_by_symbol** returns orders for the given symbol with SELL side. Algo orders are not automatically excluded; caller may filter separately if needed.

## ordersFilter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| buyOrdersFilter_by_symbol | function | Filter | orders where symbol === symbol && side === BUY. |
| sellOrdersFilter_by_symbol | function | Filter | orders where symbol === symbol && side === SELL. |

## Parameter Table (both functions)

| Name | Type | Required | Description |
|------|------|----------|-------------|
| orders | API.Order[] | Yes | All orders. |
| symbol | string | Yes | Symbol to filter. |

## Dependencies

- **Upstream**: `@paxeer-network/types` (API, OrderSide).

## Example

```typescript
import { buyOrdersFilter_by_symbol, sellOrdersFilter_by_symbol } from "@paxeer-network/perp";

const buyOrders = buyOrdersFilter_by_symbol(orders, "BTC-PERP");
const sellOrders = sellOrdersFilter_by_symbol(orders, "BTC-PERP");
```
