# orderList

## Overview

Order list feature: desktop table and mobile list, with script (data, filters, cancel/edit), context/provider for cancel/edit and min-notional check, TPSL row context, and formatting hook. Exposes desktop/mobile presentational components and widgets plus `useOrderListScript` and `OrderListInstance`.

## Exports (from index)

- `DesktopOrderList`, `MobileOrderList` (UI)
- `DesktopOrderListWidget`, `MobileOrderListWidget` (widgets)
- `useOrderListScript`, `OrderListInstance` (script and ref API)

## Files and subdirectories

| File / Directory | Description |
|-----------------|-------------|
| [orderList.widget](orderList.widget.md) | Widget props and Desktop/Mobile order list widgets. |
| [orderList.ui](orderList.ui.md) | Desktop and mobile order list UI components. |
| [orderList.script](orderList.script.md) | `useOrderListScript`, filter state, pagination, order stream, ref API. |
| [orderListContext](orderListContext.md) | Context and types for cancel/edit and min-notional check. |
| [orderListProvider](orderListProvider.md) | Provider that implements cancel/edit and checkMinNotional. |
| [tpslOrderRowContext](tpslOrderRowContext.md) | TPSL row context: order, trigger prices, PnL, cancel/update, position. |
| [useFormatOrderHistory](useFormatOrderHistory.md) | Hook to format order history (expand TP/SL child orders for display). |
| [desktop](desktop/index.md) | Columns, cells, edit/cancel, TPSL price, bracket price, hooks. |
| [mobile](mobile/index.md) | Mobile list, order cell, edit sheet, cancel/edit buttons, bracket price. |
