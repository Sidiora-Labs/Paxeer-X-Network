---
name: order-market-feature-intro
description: Market order (`OrderType.MARKET`) scenarios—numbered UI steps, testids, slippage, bracket TP/SL variants; routing via Market or BracketMarket creators.
---

# Market orders (`MARKET`)

All **16** `TASK-MARKET-*` scenarios: [scenario-matrix.md](./scenario-matrix.md). Shared concepts: [margin-modes.md](./margin-modes.md), [direction-and-side.md](./direction-and-side.md), [tpsl-bracket.md](./tpsl-bracket.md).


## TASK-MARKET-CROSS-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price` |
| Submit API | `POST /v1/order` — plain market ([`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave **`id="order_entry_tpsl"`** **off**. Do not set bracket trigger fields.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP only (`TP_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-tpPrice-input`; leave SL empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — SL only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-slPrice-input`; leave TP empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **TP + SL** |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill both TP and SL inputs.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price` |
| Submit API | `POST /v1/order` — plain market ([`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave **`id="order_entry_tpsl"`** **off**. Do not set bracket trigger fields.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP only (`TP_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-tpPrice-input`; leave SL empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — SL only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-slPrice-input`; leave TP empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-CROSS-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `CROSS`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **TP + SL** |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `CROSS` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill both TP and SL inputs.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-CROSS` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-BUY-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price` |
| Submit API | `POST /v1/order` — plain market ([`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave **`id="order_entry_tpsl"`** **off**. Do not set bracket trigger fields.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-BUY-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP only (`TP_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-tpPrice-input`; leave SL empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-BUY-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — SL only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-slPrice-input`; leave TP empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-BUY-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open long → API `side: BUY` |
| TP/SL (bracket) | Bracket — **TP + SL** |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `BUY` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill both TP and SL inputs.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-buy-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-SELL-NONE

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Off — no `tp_trigger_price` / `sl_trigger_price` |
| Submit API | `POST /v1/order` — plain market ([`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave **`id="order_entry_tpsl"`** **off**. Do not set bracket trigger fields.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-SELL-TP_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **TP only** (`tp_trigger_price` set; SL empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP only (`TP_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-tpPrice-input`; leave SL empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-SELL-SL_ONLY

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **SL only** (`sl_trigger_price` set; TP empty) |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — SL only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-slPrice-input`; leave TP empty.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |

## TASK-MARKET-ISOLATED-SELL-TP_AND_SL

### Scenario summary

| Dimension | Value |
| --------- | ----- |
| Margin mode | `ISOLATED`, field `margin_mode` |
| Order type | `OrderType.MARKET` |
| Direction | Open short → API `side: SELL` |
| TP/SL (bracket) | Bracket — **TP + SL** |
| Submit API | `POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)). |

### Payload fields (aligned with code)

[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts):

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `SELL` |
| `margin_mode` | `ISOLATED` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |
| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |

### UI steps (desktop-first; mobile noted)

1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later.

2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).

3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option.

4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).

5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`).

6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx)).

7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx)).

8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill both TP and SL inputs.

9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-ISOLATED` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-sell-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |
