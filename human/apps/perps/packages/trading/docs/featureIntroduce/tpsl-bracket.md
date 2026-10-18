---
name: feature-introduce-tpsl-bracket
description: Bracket TP/SL on LIMIT and MARKET only—`BracketOrder` fields, child orders (`TAKE_PROFIT` / `STOP_LOSS`), and routing to `/v1/algo/order` when bracket is attached.
---

# Bracket TP/SL (LIMIT / MARKET only)

Bracket-style take-profit / stop-loss is only **persisted from the order entry form** for **`LIMIT`** and **`MARKET`** in [`useOrderEntry`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (`canSetTPSLPrice`). **STOP_LIMIT**, **STOP_MARKET**, **SCALED**, and **TRAILING_STOP** do **not** receive opening bracket TP/SL through that path — the [scenario matrix](./scenario-matrix.md) still lists `TP_ONLY` / `SL_ONLY` / `TP_AND_SL` for those types for **documentation and negative-test** coverage (see each `order-*.md` **step 9**).

## `BracketOrder` fields (form / payload)

Relevant fields on [`BracketOrder`](../../../types/src/order.ts) (subset):

| Field | Role |
| ----- | ---- |
| `tp_trigger_price`, `sl_trigger_price` | Trigger levels for TP / SL legs |
| `tp_order_price`, `sl_order_price` | Limit prices when leg is limit-style |
| `tp_order_type`, `sl_order_type` | Leg order type (e.g. limit vs market) |
| `position_type` | `FULL` / `PARTIAL` sizing semantics |
| PnL / offset / ROI helpers | `tp_pnl`, `tp_offset`, `sl_offset`, etc. |

Child structure is built via [`baseCreator.ts`](../../../hooks/src/services/orderCreator/baseCreator.ts) (`parseBracketOrder` and related). Child rows use [`AlgoOrderType`](../../../types/src/order.ts): **`TAKE_PROFIT`** and **`STOP_LOSS`**.

## Routing

[`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts) sends bracket / algo-shaped bodies to **`POST /v1/algo/order`**. Plain limit/market **without** bracket may use **`POST /v1/order`**.

## UI

Master TP/SL switch: **`id="order_entry_tpsl"`** ([`tpsl.tsx`](../../../ui-order-entry/src/components/tpsl.tsx)) — no `data-testid` on the switch today. When enabled, bracket fields are populated according to the form.

## Related docs

- [Scenario matrix](./scenario-matrix.md) — all types share the same TP/SL column; only LIMIT/MARKET attach bracket legs via hooks
- [order-limit.md](./order-limit.md), [order-market.md](./order-market.md), [order-stop-limit.md](./order-stop-limit.md), [order-stop-market.md](./order-stop-market.md), [order-scaled.md](./order-scaled.md), [order-trailing-stop.md](./order-trailing-stop.md)
