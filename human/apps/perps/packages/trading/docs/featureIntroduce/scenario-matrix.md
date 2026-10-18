---
name: feature-introduce-scenario-matrix
description: Full inventory of 96 documentation tasks (TASK-*) — margin × order type × side × TP/SL (NONE/TP_ONLY/SL_ONLY/TP_AND_SL) for all six order families; deep links to detail docs.
---

# Scenario matrix (96 `TASK-*` IDs)

Each row links to a **dedicated section** in the target doc. **LIMIT** and **MARKET** implement opening bracket TP/SL in hooks. **STOP_LIMIT**, **STOP_MARKET**, **SCALED**, and **TRAILING_STOP** use the **same TP/SL column** for matrix parity; for non-`NONE` values, hooks **do not** persist bracket TP/SL on those types ([`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts)) — see the linked section’s **step 9**.

## `LIMIT` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-LIMIT-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-LIMIT-CROSS-BUY-NONE](./order-limit.md#task-limit-cross-buy-none) |
| `TASK-LIMIT-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-LIMIT-CROSS-BUY-TP_ONLY](./order-limit.md#task-limit-cross-buy-tp_only) |
| `TASK-LIMIT-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-LIMIT-CROSS-BUY-SL_ONLY](./order-limit.md#task-limit-cross-buy-sl_only) |
| `TASK-LIMIT-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-LIMIT-CROSS-BUY-TP_AND_SL](./order-limit.md#task-limit-cross-buy-tp_and_sl) |
| `TASK-LIMIT-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-LIMIT-CROSS-SELL-NONE](./order-limit.md#task-limit-cross-sell-none) |
| `TASK-LIMIT-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-LIMIT-CROSS-SELL-TP_ONLY](./order-limit.md#task-limit-cross-sell-tp_only) |
| `TASK-LIMIT-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-LIMIT-CROSS-SELL-SL_ONLY](./order-limit.md#task-limit-cross-sell-sl_only) |
| `TASK-LIMIT-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-LIMIT-CROSS-SELL-TP_AND_SL](./order-limit.md#task-limit-cross-sell-tp_and_sl) |
| `TASK-LIMIT-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-LIMIT-ISOLATED-BUY-NONE](./order-limit.md#task-limit-isolated-buy-none) |
| `TASK-LIMIT-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-LIMIT-ISOLATED-BUY-TP_ONLY](./order-limit.md#task-limit-isolated-buy-tp_only) |
| `TASK-LIMIT-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-LIMIT-ISOLATED-BUY-SL_ONLY](./order-limit.md#task-limit-isolated-buy-sl_only) |
| `TASK-LIMIT-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-LIMIT-ISOLATED-BUY-TP_AND_SL](./order-limit.md#task-limit-isolated-buy-tp_and_sl) |
| `TASK-LIMIT-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-LIMIT-ISOLATED-SELL-NONE](./order-limit.md#task-limit-isolated-sell-none) |
| `TASK-LIMIT-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-LIMIT-ISOLATED-SELL-TP_ONLY](./order-limit.md#task-limit-isolated-sell-tp_only) |
| `TASK-LIMIT-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-LIMIT-ISOLATED-SELL-SL_ONLY](./order-limit.md#task-limit-isolated-sell-sl_only) |
| `TASK-LIMIT-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-LIMIT-ISOLATED-SELL-TP_AND_SL](./order-limit.md#task-limit-isolated-sell-tp_and_sl) |

## `MARKET` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-MARKET-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-MARKET-CROSS-BUY-NONE](./order-market.md#task-market-cross-buy-none) |
| `TASK-MARKET-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-MARKET-CROSS-BUY-TP_ONLY](./order-market.md#task-market-cross-buy-tp_only) |
| `TASK-MARKET-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-MARKET-CROSS-BUY-SL_ONLY](./order-market.md#task-market-cross-buy-sl_only) |
| `TASK-MARKET-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-MARKET-CROSS-BUY-TP_AND_SL](./order-market.md#task-market-cross-buy-tp_and_sl) |
| `TASK-MARKET-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-MARKET-CROSS-SELL-NONE](./order-market.md#task-market-cross-sell-none) |
| `TASK-MARKET-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-MARKET-CROSS-SELL-TP_ONLY](./order-market.md#task-market-cross-sell-tp_only) |
| `TASK-MARKET-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-MARKET-CROSS-SELL-SL_ONLY](./order-market.md#task-market-cross-sell-sl_only) |
| `TASK-MARKET-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-MARKET-CROSS-SELL-TP_AND_SL](./order-market.md#task-market-cross-sell-tp_and_sl) |
| `TASK-MARKET-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-MARKET-ISOLATED-BUY-NONE](./order-market.md#task-market-isolated-buy-none) |
| `TASK-MARKET-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-MARKET-ISOLATED-BUY-TP_ONLY](./order-market.md#task-market-isolated-buy-tp_only) |
| `TASK-MARKET-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-MARKET-ISOLATED-BUY-SL_ONLY](./order-market.md#task-market-isolated-buy-sl_only) |
| `TASK-MARKET-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-MARKET-ISOLATED-BUY-TP_AND_SL](./order-market.md#task-market-isolated-buy-tp_and_sl) |
| `TASK-MARKET-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-MARKET-ISOLATED-SELL-NONE](./order-market.md#task-market-isolated-sell-none) |
| `TASK-MARKET-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-MARKET-ISOLATED-SELL-TP_ONLY](./order-market.md#task-market-isolated-sell-tp_only) |
| `TASK-MARKET-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-MARKET-ISOLATED-SELL-SL_ONLY](./order-market.md#task-market-isolated-sell-sl_only) |
| `TASK-MARKET-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-MARKET-ISOLATED-SELL-TP_AND_SL](./order-market.md#task-market-isolated-sell-tp_and_sl) |

## `STOP_LIMIT` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-STOP_LIMIT-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-STOP_LIMIT-CROSS-BUY-NONE](./order-stop-limit.md#task-stop_limit-cross-buy-none) |
| `TASK-STOP_LIMIT-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-STOP_LIMIT-CROSS-BUY-TP_ONLY](./order-stop-limit.md#task-stop_limit-cross-buy-tp_only) |
| `TASK-STOP_LIMIT-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-STOP_LIMIT-CROSS-BUY-SL_ONLY](./order-stop-limit.md#task-stop_limit-cross-buy-sl_only) |
| `TASK-STOP_LIMIT-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-STOP_LIMIT-CROSS-BUY-TP_AND_SL](./order-stop-limit.md#task-stop_limit-cross-buy-tp_and_sl) |
| `TASK-STOP_LIMIT-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-STOP_LIMIT-CROSS-SELL-NONE](./order-stop-limit.md#task-stop_limit-cross-sell-none) |
| `TASK-STOP_LIMIT-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-STOP_LIMIT-CROSS-SELL-TP_ONLY](./order-stop-limit.md#task-stop_limit-cross-sell-tp_only) |
| `TASK-STOP_LIMIT-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-STOP_LIMIT-CROSS-SELL-SL_ONLY](./order-stop-limit.md#task-stop_limit-cross-sell-sl_only) |
| `TASK-STOP_LIMIT-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-STOP_LIMIT-CROSS-SELL-TP_AND_SL](./order-stop-limit.md#task-stop_limit-cross-sell-tp_and_sl) |
| `TASK-STOP_LIMIT-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-STOP_LIMIT-ISOLATED-BUY-NONE](./order-stop-limit.md#task-stop_limit-isolated-buy-none) |
| `TASK-STOP_LIMIT-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-STOP_LIMIT-ISOLATED-BUY-TP_ONLY](./order-stop-limit.md#task-stop_limit-isolated-buy-tp_only) |
| `TASK-STOP_LIMIT-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-STOP_LIMIT-ISOLATED-BUY-SL_ONLY](./order-stop-limit.md#task-stop_limit-isolated-buy-sl_only) |
| `TASK-STOP_LIMIT-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-STOP_LIMIT-ISOLATED-BUY-TP_AND_SL](./order-stop-limit.md#task-stop_limit-isolated-buy-tp_and_sl) |
| `TASK-STOP_LIMIT-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-STOP_LIMIT-ISOLATED-SELL-NONE](./order-stop-limit.md#task-stop_limit-isolated-sell-none) |
| `TASK-STOP_LIMIT-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-STOP_LIMIT-ISOLATED-SELL-TP_ONLY](./order-stop-limit.md#task-stop_limit-isolated-sell-tp_only) |
| `TASK-STOP_LIMIT-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-STOP_LIMIT-ISOLATED-SELL-SL_ONLY](./order-stop-limit.md#task-stop_limit-isolated-sell-sl_only) |
| `TASK-STOP_LIMIT-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-STOP_LIMIT-ISOLATED-SELL-TP_AND_SL](./order-stop-limit.md#task-stop_limit-isolated-sell-tp_and_sl) |

## `STOP_MARKET` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-STOP_MARKET-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-STOP_MARKET-CROSS-BUY-NONE](./order-stop-market.md#task-stop_market-cross-buy-none) |
| `TASK-STOP_MARKET-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-STOP_MARKET-CROSS-BUY-TP_ONLY](./order-stop-market.md#task-stop_market-cross-buy-tp_only) |
| `TASK-STOP_MARKET-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-STOP_MARKET-CROSS-BUY-SL_ONLY](./order-stop-market.md#task-stop_market-cross-buy-sl_only) |
| `TASK-STOP_MARKET-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-STOP_MARKET-CROSS-BUY-TP_AND_SL](./order-stop-market.md#task-stop_market-cross-buy-tp_and_sl) |
| `TASK-STOP_MARKET-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-STOP_MARKET-CROSS-SELL-NONE](./order-stop-market.md#task-stop_market-cross-sell-none) |
| `TASK-STOP_MARKET-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-STOP_MARKET-CROSS-SELL-TP_ONLY](./order-stop-market.md#task-stop_market-cross-sell-tp_only) |
| `TASK-STOP_MARKET-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-STOP_MARKET-CROSS-SELL-SL_ONLY](./order-stop-market.md#task-stop_market-cross-sell-sl_only) |
| `TASK-STOP_MARKET-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-STOP_MARKET-CROSS-SELL-TP_AND_SL](./order-stop-market.md#task-stop_market-cross-sell-tp_and_sl) |
| `TASK-STOP_MARKET-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-STOP_MARKET-ISOLATED-BUY-NONE](./order-stop-market.md#task-stop_market-isolated-buy-none) |
| `TASK-STOP_MARKET-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-STOP_MARKET-ISOLATED-BUY-TP_ONLY](./order-stop-market.md#task-stop_market-isolated-buy-tp_only) |
| `TASK-STOP_MARKET-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-STOP_MARKET-ISOLATED-BUY-SL_ONLY](./order-stop-market.md#task-stop_market-isolated-buy-sl_only) |
| `TASK-STOP_MARKET-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-STOP_MARKET-ISOLATED-BUY-TP_AND_SL](./order-stop-market.md#task-stop_market-isolated-buy-tp_and_sl) |
| `TASK-STOP_MARKET-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-STOP_MARKET-ISOLATED-SELL-NONE](./order-stop-market.md#task-stop_market-isolated-sell-none) |
| `TASK-STOP_MARKET-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-STOP_MARKET-ISOLATED-SELL-TP_ONLY](./order-stop-market.md#task-stop_market-isolated-sell-tp_only) |
| `TASK-STOP_MARKET-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-STOP_MARKET-ISOLATED-SELL-SL_ONLY](./order-stop-market.md#task-stop_market-isolated-sell-sl_only) |
| `TASK-STOP_MARKET-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-STOP_MARKET-ISOLATED-SELL-TP_AND_SL](./order-stop-market.md#task-stop_market-isolated-sell-tp_and_sl) |

## `SCALED` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-SCALED-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-SCALED-CROSS-BUY-NONE](./order-scaled.md#task-scaled-cross-buy-none) |
| `TASK-SCALED-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-SCALED-CROSS-BUY-TP_ONLY](./order-scaled.md#task-scaled-cross-buy-tp_only) |
| `TASK-SCALED-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-SCALED-CROSS-BUY-SL_ONLY](./order-scaled.md#task-scaled-cross-buy-sl_only) |
| `TASK-SCALED-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-SCALED-CROSS-BUY-TP_AND_SL](./order-scaled.md#task-scaled-cross-buy-tp_and_sl) |
| `TASK-SCALED-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-SCALED-CROSS-SELL-NONE](./order-scaled.md#task-scaled-cross-sell-none) |
| `TASK-SCALED-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-SCALED-CROSS-SELL-TP_ONLY](./order-scaled.md#task-scaled-cross-sell-tp_only) |
| `TASK-SCALED-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-SCALED-CROSS-SELL-SL_ONLY](./order-scaled.md#task-scaled-cross-sell-sl_only) |
| `TASK-SCALED-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-SCALED-CROSS-SELL-TP_AND_SL](./order-scaled.md#task-scaled-cross-sell-tp_and_sl) |
| `TASK-SCALED-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-SCALED-ISOLATED-BUY-NONE](./order-scaled.md#task-scaled-isolated-buy-none) |
| `TASK-SCALED-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-SCALED-ISOLATED-BUY-TP_ONLY](./order-scaled.md#task-scaled-isolated-buy-tp_only) |
| `TASK-SCALED-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-SCALED-ISOLATED-BUY-SL_ONLY](./order-scaled.md#task-scaled-isolated-buy-sl_only) |
| `TASK-SCALED-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-SCALED-ISOLATED-BUY-TP_AND_SL](./order-scaled.md#task-scaled-isolated-buy-tp_and_sl) |
| `TASK-SCALED-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-SCALED-ISOLATED-SELL-NONE](./order-scaled.md#task-scaled-isolated-sell-none) |
| `TASK-SCALED-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-SCALED-ISOLATED-SELL-TP_ONLY](./order-scaled.md#task-scaled-isolated-sell-tp_only) |
| `TASK-SCALED-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-SCALED-ISOLATED-SELL-SL_ONLY](./order-scaled.md#task-scaled-isolated-sell-sl_only) |
| `TASK-SCALED-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-SCALED-ISOLATED-SELL-TP_AND_SL](./order-scaled.md#task-scaled-isolated-sell-tp_and_sl) |

## `TRAILING_STOP` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
| `TASK-TRAILING_STOP-CROSS-BUY-NONE` | CROSS | BUY | NONE | [TASK-TRAILING_STOP-CROSS-BUY-NONE](./order-trailing-stop.md#task-trailing_stop-cross-buy-none) |
| `TASK-TRAILING_STOP-CROSS-BUY-TP_ONLY` | CROSS | BUY | TP_ONLY | [TASK-TRAILING_STOP-CROSS-BUY-TP_ONLY](./order-trailing-stop.md#task-trailing_stop-cross-buy-tp_only) |
| `TASK-TRAILING_STOP-CROSS-BUY-SL_ONLY` | CROSS | BUY | SL_ONLY | [TASK-TRAILING_STOP-CROSS-BUY-SL_ONLY](./order-trailing-stop.md#task-trailing_stop-cross-buy-sl_only) |
| `TASK-TRAILING_STOP-CROSS-BUY-TP_AND_SL` | CROSS | BUY | TP_AND_SL | [TASK-TRAILING_STOP-CROSS-BUY-TP_AND_SL](./order-trailing-stop.md#task-trailing_stop-cross-buy-tp_and_sl) |
| `TASK-TRAILING_STOP-CROSS-SELL-NONE` | CROSS | SELL | NONE | [TASK-TRAILING_STOP-CROSS-SELL-NONE](./order-trailing-stop.md#task-trailing_stop-cross-sell-none) |
| `TASK-TRAILING_STOP-CROSS-SELL-TP_ONLY` | CROSS | SELL | TP_ONLY | [TASK-TRAILING_STOP-CROSS-SELL-TP_ONLY](./order-trailing-stop.md#task-trailing_stop-cross-sell-tp_only) |
| `TASK-TRAILING_STOP-CROSS-SELL-SL_ONLY` | CROSS | SELL | SL_ONLY | [TASK-TRAILING_STOP-CROSS-SELL-SL_ONLY](./order-trailing-stop.md#task-trailing_stop-cross-sell-sl_only) |
| `TASK-TRAILING_STOP-CROSS-SELL-TP_AND_SL` | CROSS | SELL | TP_AND_SL | [TASK-TRAILING_STOP-CROSS-SELL-TP_AND_SL](./order-trailing-stop.md#task-trailing_stop-cross-sell-tp_and_sl) |
| `TASK-TRAILING_STOP-ISOLATED-BUY-NONE` | ISOLATED | BUY | NONE | [TASK-TRAILING_STOP-ISOLATED-BUY-NONE](./order-trailing-stop.md#task-trailing_stop-isolated-buy-none) |
| `TASK-TRAILING_STOP-ISOLATED-BUY-TP_ONLY` | ISOLATED | BUY | TP_ONLY | [TASK-TRAILING_STOP-ISOLATED-BUY-TP_ONLY](./order-trailing-stop.md#task-trailing_stop-isolated-buy-tp_only) |
| `TASK-TRAILING_STOP-ISOLATED-BUY-SL_ONLY` | ISOLATED | BUY | SL_ONLY | [TASK-TRAILING_STOP-ISOLATED-BUY-SL_ONLY](./order-trailing-stop.md#task-trailing_stop-isolated-buy-sl_only) |
| `TASK-TRAILING_STOP-ISOLATED-BUY-TP_AND_SL` | ISOLATED | BUY | TP_AND_SL | [TASK-TRAILING_STOP-ISOLATED-BUY-TP_AND_SL](./order-trailing-stop.md#task-trailing_stop-isolated-buy-tp_and_sl) |
| `TASK-TRAILING_STOP-ISOLATED-SELL-NONE` | ISOLATED | SELL | NONE | [TASK-TRAILING_STOP-ISOLATED-SELL-NONE](./order-trailing-stop.md#task-trailing_stop-isolated-sell-none) |
| `TASK-TRAILING_STOP-ISOLATED-SELL-TP_ONLY` | ISOLATED | SELL | TP_ONLY | [TASK-TRAILING_STOP-ISOLATED-SELL-TP_ONLY](./order-trailing-stop.md#task-trailing_stop-isolated-sell-tp_only) |
| `TASK-TRAILING_STOP-ISOLATED-SELL-SL_ONLY` | ISOLATED | SELL | SL_ONLY | [TASK-TRAILING_STOP-ISOLATED-SELL-SL_ONLY](./order-trailing-stop.md#task-trailing_stop-isolated-sell-sl_only) |
| `TASK-TRAILING_STOP-ISOLATED-SELL-TP_AND_SL` | ISOLATED | SELL | TP_AND_SL | [TASK-TRAILING_STOP-ISOLATED-SELL-TP_AND_SL](./order-trailing-stop.md#task-trailing_stop-isolated-sell-tp_and_sl) |

## Cross-links

- [Margin modes](./margin-modes.md) · [Direction / side](./direction-and-side.md) · [Bracket TP/SL](./tpsl-bracket.md)
- [Feature introduce index](./index.md)
- Hooks: [useOrderEntry](../../../hooks/docs/next/useOrderEntry/useOrderEntry.md)
