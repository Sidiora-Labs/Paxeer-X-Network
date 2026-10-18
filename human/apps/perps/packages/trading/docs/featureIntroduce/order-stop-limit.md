---
name: order-stop-limit-feature-intro
description: Stop-limit (`STOP_LIMIT`) — CROSS/ISOLATED × BUY/SELL × TP/SL matrix (16 tasks); algo STOP + limit leg; opening bracket axis documented vs `canSetTPSLPrice` gate.
---

# Stop-limit orders (`STOP_LIMIT`)

All **16** `TASK-STOP_LIMIT-*` scenarios (same TP/SL axis as LIMIT/MARKET). For `TP_ONLY` / `SL_ONLY` / `TP_AND_SL`, hooks **do not** attach opening bracket TP/SL to `STOP_LIMIT` — see each **step 9** and [`useOrderEntry.ts`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts). Matrix: [scenario-matrix.md](./scenario-matrix.md).


## TASK-STOP_LIMIT-CROSS-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Opening bracket TP/SL (`NONE`)**
   - Keep **`id="order_entry_tpsl"`** **off**. Parent order is **`STOP_LIMIT`** — use **stop** `trigger_price` plus post-trigger **limit** price (`order_price_input`); not [`BracketOrder`](../../../types/src/order.ts) opening TP/SL.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `SL_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_AND_SL` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Opening bracket TP/SL (`NONE`)**
   - Keep **`id="order_entry_tpsl"`** **off**. Parent order is **`STOP_LIMIT`** — use **stop** `trigger_price` plus post-trigger **limit** price (`order_price_input`); not [`BracketOrder`](../../../types/src/order.ts) opening TP/SL.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `SL_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-CROSS-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_AND_SL` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Opening bracket TP/SL (`NONE`)**
   - Keep **`id="order_entry_tpsl"`** **off**. Parent order is **`STOP_LIMIT`** — use **stop** `trigger_price` plus post-trigger **limit** price (`order_price_input`); not [`BracketOrder`](../../../types/src/order.ts) opening TP/SL.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `SL_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_AND_SL` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `NONE` — leave opening bracket TP/SL off (`id="order_entry_tpsl"`); payload uses this order type’s own fields only. |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Opening bracket TP/SL (`NONE`)**
   - Keep **`id="order_entry_tpsl"`** **off**. Parent order is **`STOP_LIMIT`** — use **stop** `trigger_price` plus post-trigger **limit** price (`order_price_input`); not [`BracketOrder`](../../../types/src/order.ts) opening TP/SL.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `SL_ONLY` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `SL_ONLY` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |

## TASK-STOP_LIMIT-ISOLATED-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.STOP_LIMIT` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | `TP_AND_SL` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. Use this Task for **matrix parity / negative expectations** (no bracket legs on submit). |
| Algo | `algo_type: STOP`, child `type: LIMIT` |
| Trigger | `trigger_price`, `trigger_price_type: MARK_PRICE` |
| Submit API | **`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts)) |

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `trigger_price_type` | Typically `MARK_PRICE` |
| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

9. **Matrix TP/SL variant `TP_AND_SL` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`STOP_LIMIT`**, [`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464) **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate.

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Limit price | `id="order_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |
