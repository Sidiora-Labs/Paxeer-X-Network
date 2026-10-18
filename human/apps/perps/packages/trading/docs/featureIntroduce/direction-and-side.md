---
name: feature-introduce-direction-side
description: Maps `OrderSide` BUY/SELL to long/short intent, `reduce_only` behavior, and order-entry UI selectors for buy vs sell.
---

# Direction and side (`BUY` / `SELL`)

API and form state use [`OrderSide`](../../../types/src/order.ts): **`BUY`** and **`SELL`**.

## Long / short mapping (perpetuals convention)

| Intent (product language) | `side` | Notes |
| ------------------------- | ------ | ----- |
| Open long | `BUY` | Increase long exposure when not `reduce_only` |
| Open short | `SELL` | Increase short exposure when not `reduce_only` |

This folder uses **open long = `BUY`**, **open short = `SELL`**. Do not confuse with [`PositionSide`](../../../types/src/order.ts) (`LONG` / `SHORT`), which appears in position views and other APIs.

## `reduce_only`

When **`reduce_only` is true**, the order may only reduce an existing position; effective direction is **closing** rather than opening. Document each `TASK-*` with the intended `reduce_only` value (most “open” scenarios use **`false`**).

## UI selectors

| Side | `data-testid` | Source |
| ---- | ------------- | ------ |
| Buy / long | `oui-testid-orderEntry-side-buy-button` | [`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx) |
| Sell / short | `oui-testid-orderEntry-side-sell-button` | same |

## Related docs

- [Feature introduce index](./index.md)
- [Scenario matrix](./scenario-matrix.md)
