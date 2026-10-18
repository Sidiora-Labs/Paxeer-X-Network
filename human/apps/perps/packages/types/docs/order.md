# order

## Overview

Order-related enums and types: order type, side, status, algo order root/child types, trigger price type, position type, BBO order type, distribution type, and entity interfaces for building/submitting orders (OrderEntity, AlgoOrderEntity, BracketOrder, etc.).

## Exports

### Enums

| Enum | Description |
|------|-------------|
| `OrderType` | LIMIT, MARKET, IOC, FOK, POST_ONLY, ASK, BID, STOP_LIMIT, STOP_MARKET, CLOSE_POSITION, SCALED, TRAILING_STOP |
| `BBOOrderType` | counterparty1/5, queue1/5 |
| `OrderLevel` | ONE(0) … FIVE(4) |
| `AlgoOrderRootType` | TP_SL, POSITIONAL_TP_SL, STOP, BRACKET, TRAILING_STOP |
| `TriggerPriceType` | MARK_PRICE |
| `PositionType` | FULL, PARTIAL |
| `AlgoOrderType` | TAKE_PROFIT, STOP_LOSS |
| `OrderSide` | BUY, SELL |
| `PositionSide` | LONG, SHORT |
| `OrderStatus` | NEW, FILLED, PARTIAL_FILLED, CANCELLED, REPLACED, COMPLETED, INCOMPLETE, REJECTED, OPEN (deprecated) |
| `DistributionType` | flat, ascending, descending, custom |
| `TrailingCallbackType` | value, rate |

### Interfaces / Types

| Name | Description |
|------|-------------|
| `OrderExt` | `{ total: string }` |
| `BaseOrder` | symbol, order_type, order_price, order_quantity, side, reduce_only, visible_quantity, client_order_id, level, etc. |
| `ScaledOrder` | start_price, end_price, total_orders, distribution_type, skew |
| `TrailingStopOrder` | activated_price, callback_value, callback_rate |
| `RegularOrder` | BaseOrder + OrderExt + ScaledOrder + TrailingStopOrder |
| `AlgoOrder` | BaseOrder + quantity, type, price, algo_type, trigger_price_type, trigger_price, child_orders |
| `BracketOrder` | AlgoOrder + TP/SL fields (tp_pnl, tp_trigger_price, sl_trigger_price, etc.) |
| `PaxeerNetworkOrder` | RegularOrder & AlgoOrder & BracketOrder |
| `AlgoOrderChildOrders` | symbol, algo_type, child_orders |
| `ChildOrder` | symbol, algo_type, side, type, trigger_price, price?, reduce_only |
| `OrderEntity` | Order entity for submission (symbol, order_type, order_price, order_quantity, side, trigger_price, client_order_id, etc.) |
| `BaseAlgoOrderEntity<T>` | OrderEntity + algo_type, child_orders, algo_order_id, trigger_price_type, etc. |
| `AlgoOrderEntity<T>` | Conditional type based on AlgoOrderRootType (TP_SL, POSITIONAL_TP_SL, or STOP/BRACKET/TRAILING_STOP) |
| `TPSLOrderEntry` | AlgoOrderEntity for TP_SL with optional side, type, trigger_price |
| `BracketOrderEntry` | AlgoOrderEntity for BRACKET with optional side |

### Utility types

| Name | Description |
|------|-------------|
| `Optional<T, K>` | Omit K from T and make K partial |
| `RequireKeys<T, K>` | Require keys K on T, rest partial |

## Usage example

```typescript
import {
  OrderType,
  OrderSide,
  OrderStatus,
  AlgoOrderRootType,
  OrderEntity,
  AlgoOrderEntity,
} from "@paxeer-network/types";
```
