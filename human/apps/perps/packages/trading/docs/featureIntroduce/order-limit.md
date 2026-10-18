---

## name: order-limit-feature-intro
description: Limit order (`OrderType.LIMIT`) scenarios—CROSS/ISOLATED × BUY/SELL × bracket TP/SL variants; UI steps, testids, and routing via Limit or BracketLimit creators.

# Limit orders (`LIMIT`)

All **16** `TASK-LIMIT-*` scenarios are below. Inventory and cross-links: [scenario-matrix.md](./scenario-matrix.md). Shared concepts: [margin-modes.md](./margin-modes.md), [direction-and-side.md](./direction-and-side.md), [tpsl-bracket.md](./tpsl-bracket.md).

## TASK-LIMIT-CROSS-BUY-NONE

### Scenario summary


| Dimension       | Value                                                                                                                                                                                             |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                      |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                 |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                       |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price`                                                                                                                                                  |
| Submit API      | `POST /v1/order` — plain limit via `[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` (`[getCreateOrderUrl](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` — typical fields:


| Field            | Meaning                       |
| ---------------- | ----------------------------- |
| `symbol`         | Active symbol                 |
| `order_type`     | `LIMIT`                       |
| `order_price`    | Limit price                   |
| `order_quantity` | Size                          |
| `side`           | `BUY`                         |
| `margin_mode`    | `CROSS`                       |
| `reduce_only`    | Usually `false` for open long |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Keep order-level TP/SL disabled (`NONE`)**
  - Leave the master TP/SL switch **off**: `Switch` `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`). Do not set `tp_trigger_price` / `sl_trigger_price`.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-buy-button`    |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-BUY-TP_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set take-profit only (`TP_ONLY`)**
  - Turn **on** the master switch: `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`).
  - Fill **take-profit trigger** via `data-testid="oui-testid-orderEntry-tpsl-tpPrice-input"` (or PnL / offset modes via `oui-testid-orderEntry-tpsl-tpPnl-input`, `oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button`).
  - Leave **stop-loss** inputs empty so `sl_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-buy-button`    |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-BUY-SL_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set stop-loss only (`SL_ONLY`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill **stop-loss** via `data-testid="oui-testid-orderEntry-tpsl-slPrice-input"` (or `oui-testid-orderEntry-tpsl-slPnl-input`, `oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button`).
  - Leave **take-profit** inputs empty so `tp_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-buy-button`    |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-BUY-TP_AND_SL

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **TP + SL**                                                                                                                                                                                                                                                   |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill TP: `oui-testid-orderEntry-tpsl-tpPrice-input` (and related TP controls).
  - Fill SL: `oui-testid-orderEntry-tpsl-slPrice-input` (and related SL controls).
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-buy-button`    |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-SELL-NONE

### Scenario summary


| Dimension       | Value                                                                                                                                                                                             |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                      |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                 |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                     |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price`                                                                                                                                                  |
| Submit API      | `POST /v1/order` — plain limit via `[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` (`[getCreateOrderUrl](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` — typical fields:


| Field            | Meaning                        |
| ---------------- | ------------------------------ |
| `symbol`         | Active symbol                  |
| `order_type`     | `LIMIT`                        |
| `order_price`    | Limit price                    |
| `order_quantity` | Size                           |
| `side`           | `SELL`                         |
| `margin_mode`    | `CROSS`                        |
| `reduce_only`    | Usually `false` for open short |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Keep order-level TP/SL disabled (`NONE`)**
  - Leave the master TP/SL switch **off**: `Switch` `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`). Do not set `tp_trigger_price` / `sl_trigger_price`.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-sell-button`   |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-SELL-TP_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set take-profit only (`TP_ONLY`)**
  - Turn **on** the master switch: `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`).
  - Fill **take-profit trigger** via `data-testid="oui-testid-orderEntry-tpsl-tpPrice-input"` (or PnL / offset modes via `oui-testid-orderEntry-tpsl-tpPnl-input`, `oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button`).
  - Leave **stop-loss** inputs empty so `sl_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-sell-button`   |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-SELL-SL_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set stop-loss only (`SL_ONLY`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill **stop-loss** via `data-testid="oui-testid-orderEntry-tpsl-slPrice-input"` (or `oui-testid-orderEntry-tpsl-slPnl-input`, `oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button`).
  - Leave **take-profit** inputs empty so `tp_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-sell-button`   |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-CROSS-SELL-TP_AND_SL

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `CROSS`, field `margin_mode`                                                                                                                                                                                                                                            |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **TP + SL**                                                                                                                                                                                                                                                   |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `CROSS`                                                                                                |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to CROSS**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill TP: `oui-testid-orderEntry-tpsl-tpPrice-input` (and related TP controls).
  - Fill SL: `oui-testid-orderEntry-tpsl-slPrice-input` (and related SL controls).
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                   |
| ------------------ | ------------------------------------------ |
| Margin entry       | `oui-testid-orderEntry-margin-mode`        |
| Margin value       | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`    |
| Side               | `oui-testid-orderEntry-side-sell-button`   |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`  |
| Quantity           | `id="order_quantity_input"`                |
| Limit price        | `id="order_price_input"`                   |
| TP/SL master       | `id="order_entry_tpsl"`                    |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit             | `id="order-entry-submit-button"`           |


## TASK-LIMIT-ISOLATED-BUY-NONE

### Scenario summary


| Dimension       | Value                                                                                                                                                                                             |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                   |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                 |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                       |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price`                                                                                                                                                  |
| Submit API      | `POST /v1/order` — plain limit via `[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` (`[getCreateOrderUrl](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` — typical fields:


| Field            | Meaning                       |
| ---------------- | ----------------------------- |
| `symbol`         | Active symbol                 |
| `order_type`     | `LIMIT`                       |
| `order_price`    | Limit price                   |
| `order_quantity` | Size                          |
| `side`           | `BUY`                         |
| `margin_mode`    | `ISOLATED`                    |
| `reduce_only`    | Usually `false` for open long |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Keep order-level TP/SL disabled (`NONE`)**
  - Leave the master TP/SL switch **off**: `Switch` `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`). Do not set `tp_trigger_price` / `sl_trigger_price`.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-buy-button`       |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-BUY-TP_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set take-profit only (`TP_ONLY`)**
  - Turn **on** the master switch: `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`).
  - Fill **take-profit trigger** via `data-testid="oui-testid-orderEntry-tpsl-tpPrice-input"` (or PnL / offset modes via `oui-testid-orderEntry-tpsl-tpPnl-input`, `oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button`).
  - Leave **stop-loss** inputs empty so `sl_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-buy-button`       |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-BUY-SL_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set stop-loss only (`SL_ONLY`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill **stop-loss** via `data-testid="oui-testid-orderEntry-tpsl-slPrice-input"` (or `oui-testid-orderEntry-tpsl-slPnl-input`, `oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button`).
  - Leave **take-profit** inputs empty so `tp_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-buy-button`       |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-BUY-TP_AND_SL

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open long → API `side: BUY`                                                                                                                                                                                                                                             |
| TP/SL (bracket) | Bracket — **TP + SL**                                                                                                                                                                                                                                                   |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `BUY`                                                                                                  |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open long                                                                          |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select long (BUY)**
  - Click `data-testid="oui-testid-orderEntry-side-buy-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill TP: `oui-testid-orderEntry-tpsl-tpPrice-input` (and related TP controls).
  - Fill SL: `oui-testid-orderEntry-tpsl-slPrice-input` (and related SL controls).
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-buy-button`       |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-SELL-NONE

### Scenario summary


| Dimension       | Value                                                                                                                                                                                             |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                   |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                 |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                     |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price`                                                                                                                                                  |
| Submit API      | `POST /v1/order` — plain limit via `[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` (`[getCreateOrderUrl](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[LimitOrderCreator](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)` — typical fields:


| Field            | Meaning                        |
| ---------------- | ------------------------------ |
| `symbol`         | Active symbol                  |
| `order_type`     | `LIMIT`                        |
| `order_price`    | Limit price                    |
| `order_quantity` | Size                           |
| `side`           | `SELL`                         |
| `margin_mode`    | `ISOLATED`                     |
| `reduce_only`    | Usually `false` for open short |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Keep order-level TP/SL disabled (`NONE`)**
  - Leave the master TP/SL switch **off**: `Switch` `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`). Do not set `tp_trigger_price` / `sl_trigger_price`.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-sell-button`      |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-SELL-TP_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set take-profit only (`TP_ONLY`)**
  - Turn **on** the master switch: `**id="order_entry_tpsl"`** (`[tpsl.tsx](../../../ui-order-entry/src/components/tpsl.tsx)`).
  - Fill **take-profit trigger** via `data-testid="oui-testid-orderEntry-tpsl-tpPrice-input"` (or PnL / offset modes via `oui-testid-orderEntry-tpsl-tpPnl-input`, `oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button`).
  - Leave **stop-loss** inputs empty so `sl_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-sell-button`      |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-SELL-SL_ONLY

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty)                                                                                                                                                                                                                |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL and set stop-loss only (`SL_ONLY`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill **stop-loss** via `data-testid="oui-testid-orderEntry-tpsl-slPrice-input"` (or `oui-testid-orderEntry-tpsl-slPnl-input`, `oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button`).
  - Leave **take-profit** inputs empty so `tp_trigger_price` is not sent.
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-sell-button`      |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


## TASK-LIMIT-ISOLATED-SELL-TP_AND_SL

### Scenario summary


| Dimension       | Value                                                                                                                                                                                                                                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Margin mode     | `ISOLATED`, field `margin_mode`                                                                                                                                                                                                                                         |
| Order type      | `OrderType.LIMIT`                                                                                                                                                                                                                                                       |
| Direction       | Open short → API `side: SELL`                                                                                                                                                                                                                                           |
| TP/SL (bracket) | Bracket — **TP + SL**                                                                                                                                                                                                                                                   |
| Submit API      | `POST /v1/algo/order` — bracket via `[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` when `tp_trigger_price` and/or `sl_trigger_price` are set (`[isBracketOrder](../../../hooks/src/next/useOrderEntry/helper.ts)`). |


### Payload fields (aligned with code)

`[BracketLimitOrderCreator](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts)` (extends limit + bracket validation) — typical fields:


| Field                                   | Meaning                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `symbol`                                | Active symbol                                                                                          |
| `order_type`                            | `LIMIT`                                                                                                |
| `order_price`                           | Limit price                                                                                            |
| `order_quantity`                        | Size                                                                                                   |
| `side`                                  | `SELL`                                                                                                 |
| `margin_mode`                           | `ISOLATED`                                                                                             |
| `reduce_only`                           | Usually `false` for open short                                                                         |
| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket)                                                                             |
| `algo_type`                             | `BRACKET` on algo payload                                                                              |
| `child_orders`                          | Built by bracket builder (`[baseCreator.ts](../../../hooks/src/services/orderCreator/baseCreator.ts)`) |


### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
  - Choosing a symbol and opening the order panel: `**packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.
2. **Set margin mode to ISOLATED**
  - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` (`[LeverageBadge.tsx](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)`).
  - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` (`[marginModeSwitch.ui.tsx](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)`).
  - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` (`[marginModeSettings.ui.tsx](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)`).
3. **Select order type — limit**
  - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` (`[orderTypeSelect/index.tsx](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)`).
  - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`).
4. **Select short (SELL)**
  - Click `data-testid="oui-testid-orderEntry-side-sell-button"` (`[header/index.tsx](../../../ui-order-entry/src/components/header/index.tsx)`).
5. **Confirm reduce-only for this scenario**
  - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` (`[reduceOnlySwitch/index.tsx](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)`). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).
6. **Enter quantity**
  - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` (`[quantityInput.tsx](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)`).
  - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` (`[quantitySlider/index.tsx](../../../ui-order-entry/src/components/quantitySlider/index.tsx)`).
7. **Enter limit price**
  - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` (`[priceInput.tsx](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)`).
8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
  - Turn **on** `**id="order_entry_tpsl"`**.
  - Fill TP: `oui-testid-orderEntry-tpsl-tpPrice-input` (and related TP controls).
  - Fill SL: `oui-testid-orderEntry-tpsl-slPrice-input` (and related SL controls).
9. **Submit**
  - `**id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` (`[orderEntry.ui.tsx](../../../ui-order-entry/src/orderEntry.ui.tsx)`).

### Selector quick reference


| Purpose            | Selector                                      |
| ------------------ | --------------------------------------------- |
| Margin entry       | `oui-testid-orderEntry-margin-mode`           |
| Margin value       | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit`       |
| Side               | `oui-testid-orderEntry-side-sell-button`      |
| Reduce only        | `oui-testid-orderEntry-reduceOnly-switch`     |
| Quantity           | `id="order_quantity_input"`                   |
| Limit price        | `id="order_price_input"`                      |
| TP/SL master       | `id="order_entry_tpsl"`                       |
| TP price input     | `oui-testid-orderEntry-tpsl-tpPrice-input`    |
| SL price input     | `oui-testid-orderEntry-tpsl-slPrice-input`    |
| Submit             | `id="order-entry-submit-button"`              |


