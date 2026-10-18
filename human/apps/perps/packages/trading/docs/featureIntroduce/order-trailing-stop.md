---
name: order-trailing-stop-feature-intro
description: Trailing stop (`TRAILING_STOP`) — 16 matrix tasks; algo route; TP/SL axis for parity; opening bracket not wired in hooks.
---

# Trailing stop orders (`TRAILING_STOP`)

All **16** `TASK-TRAILING_STOP-*` scenarios: [scenario-matrix.md](./scenario-matrix.md).


## TASK-TRAILING_STOP-CROSS-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off**.

10. **Submit**
   - `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `SL_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off**.

10. **Submit**
   - `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `SL_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-CROSS-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off**.

10. **Submit**
   - `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `SL_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off**.

10. **Submit**
   - `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `SL_ONLY` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-TRAILING_STOP-ISOLATED-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.TRAILING_STOP` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: TRAILING_STOP`, leg `type: MARKET` |
| Callback | `callback_value` and/or `callback_rate` (UI % → creator divides by 100) |
| Submit API | **`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |
| Opening bracket fields | **Not** from this flow (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

9. **Matrix TP/SL `TP_AND_SL` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes.

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |
