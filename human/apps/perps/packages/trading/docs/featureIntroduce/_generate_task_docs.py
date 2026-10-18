#!/usr/bin/env python3
"""One-off generator for featureIntroduce TASK-* sections. Run from repo root or this dir."""
from __future__ import annotations

import os

OUT = os.path.dirname(os.path.abspath(__file__))

# --- shared fragments ---

PRE = """1. **Prerequisite — symbol and order entry**
   - Choosing a symbol and opening the order panel: **`packages/ui-order-entry` has no single shared `data-testid`** for this; document against the hosting trading page or add testids at the shell later."""

MARGIN = {
    "CROSS": """2. **Set margin mode to CROSS**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose cross: `data-testid="oui-testid-marginModeSwitch-option-CROSS"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-cross"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).""",
    "ISOLATED": """2. **Set margin mode to ISOLATED**
   - Open margin entry in header: `data-testid="oui-testid-orderEntry-margin-mode"` ([`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx)).
   - In the sheet, choose isolated: `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` ([`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx)).
   - *Alternate path (settings)*: `data-testid="oui-testid-marginModeSettings-set-isolated"` ([`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx)).""",
}

SIDE = {
    "BUY": """4. **Select long (BUY)**
   - Click `data-testid="oui-testid-orderEntry-side-buy-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).""",
    "SELL": """4. **Select short (SELL)**
   - Click `data-testid="oui-testid-orderEntry-side-sell-button"` ([`header/index.tsx`](../../../ui-order-entry/src/components/header/index.tsx)).""",
}

REDUCE = """5. **Confirm reduce-only for this scenario**
   - If shown: `data-testid="oui-testid-orderEntry-reduceOnly-switch"` ([`reduceOnlySwitch/index.tsx`](../../../ui-order-entry/src/components/reduceOnlySwitch/index.tsx)). For **open** long/short scenarios, leave it **off** (`reduce_only === false`)."""

QTY = """6. **Enter quantity**
   - Quantity field: **no `data-testid`**, use `id="order_quantity_input"` / `name="order_quantity_input"` ([`quantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/qtyAndTotal/quantityInput.tsx)).
   - Or max / slider: `data-testid="oui-testid-orderEntry-maxQty-value-button"`, `data-testid="oui-testid-orderEntry-maxQty-value"` ([`quantitySlider/index.tsx`](../../../ui-order-entry/src/components/quantitySlider/index.tsx))."""

LIMIT_TYPE = """3. **Select order type — limit**
   - **Desktop:** click `data-testid="oui-testid-orderEntry-orderType-limit"` inside `data-testid="oui-testid-orderEntry-orderType-desktop"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → choose the limit option (label is i18n; match by `OrderType.LIMIT`)."""

MARKET_TYPE = """3. **Select order type — market**
   - **Desktop:** inside `data-testid="oui-testid-orderEntry-orderType-desktop"`, click `data-testid="oui-testid-orderEntry-orderType-market"` ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → market option."""

LIMIT_PRICE = """7. **Enter limit price**
   - **No `data-testid`** on the price input; use `id="order_price_input"` / `name="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx))."""

SLIPPAGE = """7. **(Optional) Slippage** (market only)
   - **No `data-testid`** on the slippage row; classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` ([`slippageCell.tsx`](../../../ui-order-entry/src/components/slippage/components/slippageCell.tsx))."""

TPSL_OFF_LIM = """8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave the master TP/SL switch **off**: `Switch` **`id="order_entry_tpsl"`** ([`tpsl.tsx`](../../../ui-order-entry/src/components/tpsl.tsx)). Do not set `tp_trigger_price` / `sl_trigger_price`."""

TPSL_TP_ONLY_LIM = """8. **Enable TP/SL and set take-profit only (`TP_ONLY`)**
   - Turn **on** the master switch: **`id="order_entry_tpsl"`** ([`tpsl.tsx`](../../../ui-order-entry/src/components/tpsl.tsx)).
   - Fill **take-profit trigger** via `data-testid="oui-testid-orderEntry-tpsl-tpPrice-input"` (or PnL / offset modes via `oui-testid-orderEntry-tpsl-tpPnl-input`, `oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button`).
   - Leave **stop-loss** inputs empty so `sl_trigger_price` is not sent."""

TPSL_SL_ONLY_LIM = """8. **Enable TP/SL and set stop-loss only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**.
   - Fill **stop-loss** via `data-testid="oui-testid-orderEntry-tpsl-slPrice-input"` (or `oui-testid-orderEntry-tpsl-slPnl-input`, `oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button`).
   - Leave **take-profit** inputs empty so `tp_trigger_price` is not sent."""

TPSL_BOTH_LIM = """8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**.
   - Fill TP: `oui-testid-orderEntry-tpsl-tpPrice-input` (and related TP controls).
   - Fill SL: `oui-testid-orderEntry-tpsl-slPrice-input` (and related SL controls)."""

TPSL_OFF_MKT = """8. **Keep order-level TP/SL disabled (`NONE`)**
   - Leave **`id="order_entry_tpsl"`** **off**. Do not set bracket trigger fields."""

TPSL_TP_ONLY_MKT = """8. **Enable TP/SL — TP only (`TP_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-tpPrice-input`; leave SL empty."""

TPSL_SL_ONLY_MKT = """8. **Enable TP/SL — SL only (`SL_ONLY`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill `oui-testid-orderEntry-tpsl-slPrice-input`; leave TP empty."""

TPSL_BOTH_MKT = """8. **Enable TP/SL — TP and SL (`TP_AND_SL`)**
   - Turn **on** **`id="order_entry_tpsl"`**; fill both TP and SL inputs."""

SUBMIT_BUY_9 = """9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx))."""

SUBMIT_SELL_9 = """9. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx))."""

SUBMIT_BUY_10 = """10. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-buy` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx))."""

SUBMIT_SELL_10 = """10. **Submit**
   - **`id="order-entry-submit-button"`**; class `paxeer-network-order-entry-submit-button-sell` ([`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx))."""


def anchor(task_id: str) -> str:
    # Match GitHub-style heading slugs: lowercase, keep underscores (e.g. STOP_LIMIT).
    return task_id.lower()


CANSET_TPSL_REF = "[`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts) (around lines 447–464)"


def bracket_cell_advanced_order(tpsl: str) -> str:
    """STOP_*, SCALED, TRAILING: same matrix axis as LIMIT/MARKET; hooks only allow bracket fields on LIMIT/MARKET."""
    if tpsl == "NONE":
        return "`NONE` — leave opening bracket TP/SL off (`id=\"order_entry_tpsl\"`); payload uses this order type’s own fields only."
    return (
        f"`{tpsl}` (matrix axis) — opening bracket TP/SL **is not applied** to this `order_type` in hooks: "
        f"{CANSET_TPSL_REF} returns `false` for `tpslFields` when `order_type` is not `LIMIT` or `MARKET`. "
        "Use this Task for **matrix parity / negative expectations** (no bracket legs on submit)."
    )


def scenario_table(
    order_type: str,
    margin: str,
    side: str,
    tpsl: str,
    extra_rows: list[tuple[str, str]] | None = None,
    submit_api: str | None = None,
    advanced_order: bool = False,
) -> str:
    dir_en = "Open long → API `side: BUY`" if side == "BUY" else "Open short → API `side: SELL`"
    if advanced_order:
        tpsl_cell = bracket_cell_advanced_order(tpsl)
    elif tpsl == "NA":
        tpsl_cell = "`N/A` (legacy; prefer explicit NONE/TP_* on advanced orders)"
    elif tpsl == "NONE":
        tpsl_cell = "Off — no `tp_trigger_price` / `sl_trigger_price`"
    elif tpsl == "TP_ONLY":
        tpsl_cell = "Bracket — **TP only** (`tp_trigger_price` set; SL empty)"
    elif tpsl == "SL_ONLY":
        tpsl_cell = "Bracket — **SL only** (`sl_trigger_price` set; TP empty)"
    else:
        tpsl_cell = "Bracket — **TP + SL**"
    lines = [
        "| Dimension | Value |",
        "| --------- | ----- |",
        f"| Margin mode | `{margin}`, field `margin_mode` |",
        f"| Order type | `{order_type}` |",
        f"| Direction | {dir_en} |",
        f"| TP/SL (bracket) | {tpsl_cell} |",
    ]
    if extra_rows:
        for k, v in extra_rows:
            lines.append(f"| {k} | {v} |")
    if submit_api:
        lines.append(f"| Submit API | {submit_api} |")
    return "\n".join(lines)


def section_limit(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-LIMIT-{market}-{side}-{tpsl}"
    a = anchor(tid)
    if tpsl in ("NONE",):
        api = "`POST /v1/order` — plain limit via [`LimitOrderCreator`](../../../hooks/src/services/orderCreator/limitOrderCreator.ts) ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts))."
        creator = "[`LimitOrderCreator`](../../../hooks/src/services/orderCreator/limitOrderCreator.ts)"
    else:
        api = "`POST /v1/algo/order` — bracket via [`BracketLimitOrderCreator`](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts) when `tp_trigger_price` and/or `sl_trigger_price` are set ([`isBracketOrder`](../../../hooks/src/next/useOrderEntry/helper.ts))."
        creator = "[`BracketLimitOrderCreator`](../../../hooks/src/services/orderCreator/bracketLimitOrderCreator.ts) (extends limit + bracket validation)"

    tpsl_block = {
        "NONE": TPSL_OFF_LIM,
        "TP_ONLY": TPSL_TP_ONLY_LIM,
        "SL_ONLY": TPSL_SL_ONLY_LIM,
        "TP_AND_SL": TPSL_BOTH_LIM,
    }[tpsl]

    submit = SUBMIT_BUY_9 if side == "BUY" else SUBMIT_SELL_9

    payload = f"""| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `LIMIT` |
| `order_price` | Limit price |
| `order_quantity` | Size |
| `side` | `{side}` |
| `margin_mode` | `{market}` |
| `reduce_only` | { 'Usually `false` for open long' if side == 'BUY' else 'Usually `false` for open short' } |"""
    if tpsl != "NONE":
        payload += "\n| `tp_trigger_price` / `sl_trigger_price` | Set per scenario (bracket) |\n| `algo_type` | `BRACKET` on algo payload |\n| `child_orders` | Built by bracket builder ([`baseCreator.ts`](../../../hooks/src/services/orderCreator/baseCreator.ts)) |"

    body = f"""## {tid}

### Scenario summary

{scenario_table("OrderType.LIMIT", market, side, tpsl, submit_api=api)}

### Payload fields (aligned with code)

{creator} — typical fields:

{payload}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

{LIMIT_TYPE}

{SIDE[side]}

{REDUCE}

{QTY}

{LIMIT_PRICE}

{tpsl_block}

{submit}

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-{market}` |
| Order type — limit | `oui-testid-orderEntry-orderType-limit` |
| Side | `oui-testid-orderEntry-side-{'buy' if side == 'BUY' else 'sell'}-button` |
| Reduce only | `oui-testid-orderEntry-reduceOnly-switch` |
| Quantity | `id="order_quantity_input"` |
| Limit price | `id="order_price_input"` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP price input | `oui-testid-orderEntry-tpsl-tpPrice-input` |
| SL price input | `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |
"""
    return body


def section_market(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-MARKET-{market}-{side}-{tpsl}"
    if tpsl == "NONE":
        api = "`POST /v1/order` — plain market ([`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts))."
        creator = "[`MarketOrderCreator`](../../../hooks/src/services/orderCreator/marketOrderCreator.ts)"
    else:
        api = "`POST /v1/algo/order` — bracket market ([`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts))."
        creator = "[`BracketMarketOrderCreator`](../../../hooks/src/services/orderCreator/bracketMarketOrderCreator.ts)"

    tpsl_block = {
        "NONE": TPSL_OFF_MKT,
        "TP_ONLY": TPSL_TP_ONLY_MKT,
        "SL_ONLY": TPSL_SL_ONLY_MKT,
        "TP_AND_SL": TPSL_BOTH_MKT,
    }[tpsl]
    submit = SUBMIT_BUY_9 if side == "BUY" else SUBMIT_SELL_9

    payload = f"""| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `MARKET` |
| `order_quantity` | Size |
| `side` | `{side}` |
| `margin_mode` | `{market}` |
| `slippage` | From UI; `baseOrder` scales percent if applicable |"""
    if tpsl != "NONE":
        payload += "\n| Bracket fields | `tp_trigger_price` / `sl_trigger_price` (and leg prices/types) per scenario |"

    body = f"""## {tid}

### Scenario summary

{scenario_table("OrderType.MARKET", market, side, tpsl, submit_api=api)}

### Payload fields (aligned with code)

{creator}:

{payload}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

{MARKET_TYPE}

{SIDE[side]}

{REDUCE}

{QTY}

{SLIPPAGE}

{tpsl_block}

{submit}

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Margin entry | `oui-testid-orderEntry-margin-mode` |
| Margin value | `oui-testid-marginModeSwitch-option-{market}` |
| Order type — market | `oui-testid-orderEntry-orderType-market` |
| Side | `oui-testid-orderEntry-side-{'buy' if side == 'BUY' else 'sell'}-button` |
| Quantity | `id="order_quantity_input"` |
| Slippage | classes `oui-orderEntry-slippage`, `oui-slippage-edit-btn` |
| TP/SL master | `id="order_entry_tpsl"` |
| TP / SL inputs | `oui-testid-orderEntry-tpsl-tpPrice-input`, `oui-testid-orderEntry-tpsl-slPrice-input` |
| Submit | `id="order-entry-submit-button"` |
"""
    return body


def _stop_tpsl_step(tpsl: str, stop_kind: str) -> str:
    """Step text for bracket column: NONE vs TP_* (hooks gate)."""
    leg = (
        "post-trigger **limit** price (`order_price_input`)"
        if stop_kind == "STOP_LIMIT"
        else "**market** leg (no `order_price_input`)"
    )
    if tpsl == "NONE":
        return f"""9. **Opening bracket TP/SL (`NONE`)**
   - Keep **`id="order_entry_tpsl"`** **off**. Parent order is **`{stop_kind}`** — use **stop** `trigger_price` plus {leg}; not [`BracketOrder`](../../../types/src/order.ts) opening TP/SL."""
    return f"""9. **Matrix TP/SL variant `{tpsl}` vs hooks**
   - The scenario matrix uses the same **TP/SL axis** as LIMIT/MARKET. For **`{stop_kind}`**, {CANSET_TPSL_REF} **blocks** persisting `tpslFields` unless `order_type` is `LIMIT` or `MARKET`.
   - **Automation:** do not expect `tp_trigger_price` / `sl_trigger_price` on the submitted STOP payload from this entry path; treat bracket inputs as **out of scope / negative test** until product changes the gate."""


def section_stop_limit(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-STOP_LIMIT-{market}-{side}-{tpsl}"
    extra = [
        ("Algo", "`algo_type: STOP`, child `type: LIMIT`"),
        ("Trigger", "`trigger_price`, `trigger_price_type: MARK_PRICE`"),
    ]
    payload_extra = ""
    if tpsl != "NONE":
        payload_extra = "\n| Opening bracket fields | **Not** produced from this flow for `STOP_LIMIT` (see step 9). |"

    body = f"""## {tid}

### Scenario summary

{scenario_table(
        "OrderType.STOP_LIMIT",
        market,
        side,
        tpsl,
        extra,
        submit_api="**`POST /v1/algo/order`** ([`StopLimitOrderCreator`](../../../hooks/src/services/orderCreator/stopLimitOrderCreator.ts), [`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts))",
        advanced_order=True,
    )}

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` ([`AlgoOrderRootType.STOP`](../../../types/src/order.ts)) |
| `type` | `LIMIT` (executed leg) |
| `trigger_price` | Stop trigger |
| `price` (as `price` on algo entity) | Limit price after trigger |
| `quantity` | Size |
| `side` | `{side}` |
| `margin_mode` | `{market}` |
| `trigger_price_type` | Typically `MARK_PRICE` |{payload_extra}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

3. **Select order type — stop limit**
   - **Desktop:** open `data-testid="oui-testid-orderEntry-orderType-advanced-select"` inside `data-testid="oui-testid-orderEntry-orderType-advanced"` and choose **Stop limit** (`OrderType.STOP_LIMIT`) ([`orderTypeSelect/index.tsx`](../../../ui-order-entry/src/components/orderTypeSelect/index.tsx)).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop limit entry.

{SIDE[side]}

{REDUCE}

{QTY}

7. **Enter trigger price**
   - `id="order_trigger_price_input"` / `name="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **Enter limit price (post-trigger leg)**
   - `id="order_price_input"` ([`priceInput.tsx`](../../../ui-order-entry/src/components/orderInput/limit/priceInput.tsx)).

{_stop_tpsl_step(tpsl, "STOP_LIMIT")}

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
"""
    return body


def section_stop_market(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-STOP_MARKET-{market}-{side}-{tpsl}"
    extra = [
        ("Algo", "`algo_type: STOP`, child `type: MARKET`"),
        ("Trigger", "`trigger_price`, `trigger_price_type: MARK_PRICE`"),
    ]
    payload_extra = ""
    if tpsl != "NONE":
        payload_extra = "\n| Opening bracket fields | **Not** produced from this flow for `STOP_MARKET` (see step 9). |"

    body = f"""## {tid}

### Scenario summary

{scenario_table(
        "OrderType.STOP_MARKET",
        market,
        side,
        tpsl,
        extra,
        submit_api="**`POST /v1/algo/order`** ([`StopMarketOrderCreator`](../../../hooks/src/services/orderCreator/stopMarketOrderCreator.ts))",
        advanced_order=True,
    )}

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `STOP` |
| `type` | `MARKET` |
| `trigger_price` | Stop trigger |
| `quantity` | Size |
| `side` | `{side}` |
| `margin_mode` | `{market}` |{payload_extra}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

3. **Select order type — stop market**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Stop market** (`OrderType.STOP_MARKET`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → stop market.

{SIDE[side]}

{REDUCE}

{QTY}

7. **Enter trigger price**
   - `id="order_trigger_price_input"` ([`triggerPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/stop/triggerPriceInput.tsx)).

8. **No limit price row for market leg**
   - Market stop does not use `order_price_input` for the executed leg (see [`StopMarketOrderCreator`](../../../hooks/src/services/orderCreator/stopMarketOrderCreator.ts): `type: MARKET`).

{_stop_tpsl_step(tpsl, "STOP_MARKET")}

10. **Submit**
   - **`id="order-entry-submit-button"`** (buy / sell classes as in [`orderEntry.ui.tsx`](../../../ui-order-entry/src/orderEntry.ui.tsx)).

### Selector quick reference

| Purpose | Selector |
| ------- | -------- |
| Advanced order type | `oui-testid-orderEntry-orderType-advanced-select` |
| Trigger price | `id="order_trigger_price_input"` |
| Quantity | `id="order_quantity_input"` |
| TP/SL master | `id="order_entry_tpsl"` (opening bracket; see step 9) |
| Submit | `id="order-entry-submit-button"` |
"""
    return body


def _scaled_tpsl_tail(tpsl: str) -> str:
    if tpsl == "NONE":
        return """9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off** ([`tpsl-bracket.md`](./tpsl-bracket.md)).

10. **Confirm & submit**
   - Review scaled confirmation dialog if enabled ([`scaledOrderConfirm.ui.tsx`](../../../ui-order-entry/src/components/dialog/scaledOrderConfirm/scaledOrderConfirm.ui.tsx)).
   - Submit: `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`)."""
    return f"""9. **Matrix TP/SL `{tpsl}` vs hooks (`SCALED`)**
   - Matrix uses the same four TP/SL labels as LIMIT/MARKET. For **`SCALED`**, {CANSET_TPSL_REF} does **not** persist opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off; automation should not expect bracket fields on the batch payload.

10. **Confirm & submit**
   - Same as `NONE`: complete scaled fields, then `id="order-entry-submit-button"` (buy / sell classes per side)."""


def _trailing_tpsl_tail(tpsl: str) -> str:
    if tpsl == "NONE":
        return """9. **No opening bracket TP/SL (`NONE`)**
   - Keep `id="order_entry_tpsl"` **off**.

10. **Submit**
   - `id="order-entry-submit-button"` (buy: `paxeer-network-order-entry-submit-button-buy`, sell: `paxeer-network-order-entry-submit-button-sell`)."""
    return f"""9. **Matrix TP/SL `{tpsl}` vs hooks (`TRAILING_STOP`)**
   - For **`TRAILING_STOP`**, {CANSET_TPSL_REF} blocks opening bracket TP/SL. Keep **`id="order_entry_tpsl"`** off.

10. **Submit**
   - Same as `NONE`: `id="order-entry-submit-button"` with side-specific classes."""


def section_scaled(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-SCALED-{market}-{side}-{tpsl}"
    extra = [
        (
            "Creator",
            "[`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts) — `orders[]`, `total_orders`, `distribution_type`, optional `skew`",
        ),
    ]
    payload_note = ""
    if tpsl != "NONE":
        payload_note = "\n| Opening bracket fields | **Not** from this flow (see step 9). |"
    body = f"""## {tid}

### Scenario summary

{scenario_table(
        "OrderType.SCALED",
        market,
        side,
        tpsl,
        extra,
        submit_api="**`POST /v1/batch-order`** ([`getCreateOrderUrl`](../../../hooks/src/next/useOrderEntry/helper.ts), [`ScaledOrderCreator`](../../../hooks/src/services/orderCreator/scaledOrderCreator.ts))",
        advanced_order=True,
    )}

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `order_type` | `SCALED` |
| `side` | `{side}` |
| `margin_mode` | `{market}` |
| `order_quantity` | Total size (sum of child orders) |
| `total_orders` | Count 2–20 |
| `distribution_type` | Distribution curve |
| `skew` | Required when distribution is custom |
| `orders` | Child order list (built in creator) |{payload_note}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

3. **Select order type — scaled**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Scaled order** (`OrderType.SCALED`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"` → scaled.

{SIDE[side]}

{REDUCE}

6. **Price range**
   - Start: `id="order_start_price_input"` ([`scaledPriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledPriceInput.tsx)).
   - End: `id="order_end_price_input"`.

7. **Total orders & distribution**
   - `id="order_total_orders_input"` ([`totalOrdersInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/totalOrdersInput.tsx)).
   - Distribution: radio inputs `id="distribution-type-<type>"` ([`quantityDistributionInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/quantityDistributionInput.tsx)).
   - If custom: `id="order_skew_input"` ([`skewInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/skewInput.tsx)).

8. **Quantity / total**
   - `id="order_quantity_input"` and/or `id="order_total_input"` per form mode ([`scaledQuantityInput.tsx`](../../../ui-order-entry/src/components/orderInput/scaledOrder/scaledQuantityInput.tsx)).

{_scaled_tpsl_tail(tpsl)}

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
"""
    return body


def section_trailing(market: str, side: str, tpsl: str) -> str:
    tid = f"TASK-TRAILING_STOP-{market}-{side}-{tpsl}"
    extra = [
        ("Algo", "`algo_type: TRAILING_STOP`, leg `type: MARKET`"),
        ("Callback", "`callback_value` and/or `callback_rate` (UI % → creator divides by 100)"),
    ]
    payload_note = ""
    if tpsl != "NONE":
        payload_note = "\n| Opening bracket fields | **Not** from this flow (see step 9). |"
    body = f"""## {tid}

### Scenario summary

{scenario_table(
        "OrderType.TRAILING_STOP",
        market,
        side,
        tpsl,
        extra,
        submit_api="**`POST /v1/algo/order`** ([`TrailingStopOrderCreator`](../../../hooks/src/services/orderCreator/trailingStopOrderCreator.ts))",
        advanced_order=True,
    )}

### Payload fields (aligned with code)

| Field | Meaning |
| ----- | ------- |
| `symbol` | Active symbol |
| `algo_type` | `TRAILING_STOP` |
| `type` | `MARKET` |
| `quantity` | Size |
| `side` | `{side}` |
| `margin_mode` | `{market}` |
| `activated_price` | Optional activation price |
| `callback_value` | Absolute callback |
| `callback_rate` | Rate string after `/100` in creator |{payload_note}

### UI steps (desktop-first; mobile noted)

{PRE}

{MARGIN[market]}

3. **Select order type — trailing stop**
   - **Desktop:** `data-testid="oui-testid-orderEntry-orderType-advanced-select"` → **Trailing stop** (`OrderType.TRAILING_STOP`).
   - **Mobile:** `data-testid="oui-testid-orderEntry-orderType-button"`.

{SIDE[side]}

{REDUCE}

{QTY}

7. **Activation price (optional)**
   - `id="order_activated_price_input"` ([`activePriceInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/activePriceInput.tsx)).

8. **Callback**
   - **Rate (%):** `id="order_callback_rate_input"` OR **value:** `id="order_callback_value_input"` ([`trailingCallbackInput.tsx`](../../../ui-order-entry/src/components/orderInput/trailingStop/trailingCallbackInput.tsx)). Do not set both if product forbids; follow validation messages.

{_trailing_tpsl_tail(tpsl)}

### Selector quick reference

| Purpose | id / selector |
| ----- | ------------- |
| Advanced type | `oui-testid-orderEntry-orderType-advanced-select` |
| Activated price | `order_activated_price_input` |
| Callback rate / value | `order_callback_rate_input`, `order_callback_value_input` |
| TP/SL master | `id="order_entry_tpsl"` (see step 9) |
| Submit | `id="order-entry-submit-button"` |
"""
    return body


def write_limit():
    margins = ["CROSS", "ISOLATED"]
    sides = ["BUY", "SELL"]
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-limit-feature-intro
description: Limit order (`OrderType.LIMIT`) scenarios—CROSS/ISOLATED × BUY/SELL × bracket TP/SL variants; UI steps, testids, and routing via Limit or BracketLimit creators.
---

# Limit orders (`LIMIT`)

All **16** `TASK-LIMIT-*` scenarios are below. Inventory and cross-links: [scenario-matrix.md](./scenario-matrix.md). Shared concepts: [margin-modes.md](./margin-modes.md), [direction-and-side.md](./direction-and-side.md), [tpsl-bracket.md](./tpsl-bracket.md).

"""
    ]
    for m in margins:
        for s in sides:
            for t in tpsls:
                parts.append(section_limit(m, s, t))
    path = os.path.join(OUT, "order-limit.md")
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_market():
    margins = ["CROSS", "ISOLATED"]
    sides = ["BUY", "SELL"]
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-market-feature-intro
description: Market order (`OrderType.MARKET`) scenarios—numbered UI steps, testids, slippage, bracket TP/SL variants; routing via Market or BracketMarket creators.
---

# Market orders (`MARKET`)

All **16** `TASK-MARKET-*` scenarios: [scenario-matrix.md](./scenario-matrix.md). Shared concepts: [margin-modes.md](./margin-modes.md), [direction-and-side.md](./direction-and-side.md), [tpsl-bracket.md](./tpsl-bracket.md).

"""
    ]
    for m in margins:
        for s in sides:
            for t in tpsls:
                parts.append(section_market(m, s, t))
    path = os.path.join(OUT, "order-market.md")
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_stop_limit():
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-stop-limit-feature-intro
description: Stop-limit (`STOP_LIMIT`) — CROSS/ISOLATED × BUY/SELL × TP/SL matrix (16 tasks); algo STOP + limit leg; opening bracket axis documented vs `canSetTPSLPrice` gate.
---

# Stop-limit orders (`STOP_LIMIT`)

All **16** `TASK-STOP_LIMIT-*` scenarios (same TP/SL axis as LIMIT/MARKET). For `TP_ONLY` / `SL_ONLY` / `TP_AND_SL`, hooks **do not** attach opening bracket TP/SL to `STOP_LIMIT` — see each **step 9** and [`useOrderEntry.ts`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts). Matrix: [scenario-matrix.md](./scenario-matrix.md).

"""
    ]
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                parts.append(section_stop_limit(m, s, t))
    with open(os.path.join(OUT, "order-stop-limit.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_stop_market():
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-stop-market-feature-intro
description: Stop-market (`STOP_MARKET`) — 16 matrix tasks; STOP + market leg; TP/SL column for doc parity (bracket not wired in hooks for this type).
---

# Stop-market orders (`STOP_MARKET`)

All **16** `TASK-STOP_MARKET-*` scenarios. Matrix: [scenario-matrix.md](./scenario-matrix.md).

"""
    ]
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                parts.append(section_stop_market(m, s, t))
    with open(os.path.join(OUT, "order-stop-market.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_scaled():
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-scaled-feature-intro
description: Scaled orders (`SCALED`) — 16 matrix tasks; batch API; TP/SL axis documented; opening bracket not applied in hooks for `SCALED`.
---

# Scaled orders (`SCALED`)

All **16** `TASK-SCALED-*` scenarios: [scenario-matrix.md](./scenario-matrix.md).

"""
    ]
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                parts.append(section_scaled(m, s, t))
    with open(os.path.join(OUT, "order-scaled.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_trailing():
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    parts = [
        """---
name: order-trailing-stop-feature-intro
description: Trailing stop (`TRAILING_STOP`) — 16 matrix tasks; algo route; TP/SL axis for parity; opening bracket not wired in hooks.
---

# Trailing stop orders (`TRAILING_STOP`)

All **16** `TASK-TRAILING_STOP-*` scenarios: [scenario-matrix.md](./scenario-matrix.md).

"""
    ]
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                parts.append(section_trailing(m, s, t))
    with open(os.path.join(OUT, "order-trailing-stop.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_scenario_matrix():
    def link_limit(tid: str) -> str:
        return f"[{tid}](./order-limit.md#{anchor(tid)})"

    def link_market(tid: str) -> str:
        return f"[{tid}](./order-market.md#{anchor(tid)})"

    def link_sl(tid: str) -> str:
        return f"[{tid}](./order-stop-limit.md#{anchor(tid)})"

    def link_sm(tid: str) -> str:
        return f"[{tid}](./order-stop-market.md#{anchor(tid)})"

    def link_sc(tid: str) -> str:
        return f"[{tid}](./order-scaled.md#{anchor(tid)})"

    def link_tr(tid: str) -> str:
        return f"[{tid}](./order-trailing-stop.md#{anchor(tid)})"

    rows_limit = []
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]:
                tid = f"TASK-LIMIT-{m}-{s}-{t}"
                rows_limit.append(
                    f"| `{tid}` | {m} | {s} | {t} | {link_limit(tid)} |"
                )
    rows_market = []
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]:
                tid = f"TASK-MARKET-{m}-{s}-{t}"
                rows_market.append(
                    f"| `{tid}` | {m} | {s} | {t} | {link_market(tid)} |"
                )
    tpsls = ["NONE", "TP_ONLY", "SL_ONLY", "TP_AND_SL"]
    rows_sl = []
    rows_sm = []
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                tid1 = f"TASK-STOP_LIMIT-{m}-{s}-{t}"
                rows_sl.append(
                    f"| `{tid1}` | {m} | {s} | {t} | {link_sl(tid1)} |"
                )
                tid2 = f"TASK-STOP_MARKET-{m}-{s}-{t}"
                rows_sm.append(
                    f"| `{tid2}` | {m} | {s} | {t} | {link_sm(tid2)} |"
                )
    rows_sc = []
    rows_tr = []
    for m in ["CROSS", "ISOLATED"]:
        for s in ["BUY", "SELL"]:
            for t in tpsls:
                tid1 = f"TASK-SCALED-{m}-{s}-{t}"
                rows_sc.append(
                    f"| `{tid1}` | {m} | {s} | {t} | {link_sc(tid1)} |"
                )
                tid2 = f"TASK-TRAILING_STOP-{m}-{s}-{t}"
                rows_tr.append(
                    f"| `{tid2}` | {m} | {s} | {t} | {link_tr(tid2)} |"
                )

    md = f"""---
name: feature-introduce-scenario-matrix
description: Full inventory of 96 documentation tasks (TASK-*) — margin × order type × side × TP/SL (NONE/TP_ONLY/SL_ONLY/TP_AND_SL) for all six order families; deep links to detail docs.
---

# Scenario matrix (96 `TASK-*` IDs)

Each row links to a **dedicated section** in the target doc. **LIMIT** and **MARKET** implement opening bracket TP/SL in hooks. **STOP_LIMIT**, **STOP_MARKET**, **SCALED**, and **TRAILING_STOP** use the **same TP/SL column** for matrix parity; for non-`NONE` values, hooks **do not** persist bracket TP/SL on those types ([`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts)) — see the linked section’s **step 9**.

## `LIMIT` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_limit)}

## `MARKET` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_market)}

## `STOP_LIMIT` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_sl)}

## `STOP_MARKET` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_sm)}

## `SCALED` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_sc)}

## `TRAILING_STOP` (16)

| Task ID | Margin | Side | TP/SL | Detail doc |
| ------- | ------ | ---- | ----- | ---------- |
{chr(10).join(rows_tr)}

## Cross-links

- [Margin modes](./margin-modes.md) · [Direction / side](./direction-and-side.md) · [Bracket TP/SL](./tpsl-bracket.md)
- [Feature introduce index](./index.md)
- Hooks: [useOrderEntry](../../../hooks/docs/next/useOrderEntry/useOrderEntry.md)
"""
    with open(os.path.join(OUT, "scenario-matrix.md"), "w", encoding="utf-8") as f:
        f.write(md)


def main():
    write_limit()
    write_market()
    write_stop_limit()
    write_stop_market()
    write_scaled()
    write_trailing()
    write_scenario_matrix()
    print("Wrote 96 TASK docs: order-limit, order-market, order-stop-*, order-scaled, order-trailing-stop, scenario-matrix")


if __name__ == "__main__":
    main()
