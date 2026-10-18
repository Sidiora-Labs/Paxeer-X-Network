---
name: order-scaled-feature-intro
description: Scaled orders (`SCALED`) — 16 matrix tasks; batch API; TP/SL axis documented; opening bracket not applied in hooks for `SCALED`.
---

# Scaled orders (`SCALED`)

All **16** `TASK-SCALED-*` scenarios: [scenario-matrix.md](./scenario-matrix.md).


## TASK-SCALED-CROSS-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off** ([`tpsl-bracket.md`](./tpsl-bracket.md)).

10. **Confirm & submit**
   - Review scaled confirmation dialog if enabled ([`scaledOrderConfirm.ui.tsx`](../../../ui-order-entry/src/components/dialog/scaledOrderConfirm/scaledOrderConfirm.ui.tsx)).
   - Submit: `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `SL_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off** ([`tpsl-bracket.md`](./tpsl-bracket.md)).

10. **Confirm & submit**
   - Review scaled confirmation dialog if enabled ([`scaledOrderConfirm.ui.tsx`](../../../ui-order-entry/src/components/dialog/scaledOrderConfirm/scaledOrderConfirm.ui.tsx)).
   - Submit: `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `SL_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-CROSS-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off** ([`tpsl-bracket.md`](./tpsl-bracket.md)).

10. **Confirm & submit**
   - Review scaled confirmation dialog if enabled ([`scaledOrderConfirm.ui.tsx`](../../../ui-order-entry/src/components/dialog/scaledOrderConfirm/scaledOrderConfirm.ui.tsx)).
   - Submit: `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `SL_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off** ([`tpsl-bracket.md`](./tpsl-bracket.md)).

10. **Confirm & submit**
   - Review scaled confirmation dialog if enabled ([`scaledOrderConfirm.ui.tsx`](../../../ui-order-entry/src/components/dialog/scaledOrderConfirm/scaledOrderConfirm.ui.tsx)).
   - Submit: `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `SL_ONLY` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-SCALED-ISOLATED-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.SCALED` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Creator | [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew` |
| Submit API | **`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side).

### Selector quick reference

| Purpose | Selector / id |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Start / end price | `order_start_price_input`, `order_end_price_input` |
| Total orders | `order_total_orders_input` |
| Skew | `order_skew_input` |
| Quantity / total | `order_quantity_input`, `order_total_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |
