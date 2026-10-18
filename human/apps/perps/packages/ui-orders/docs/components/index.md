# components

## Overview

UI building blocks for the orders experience: main Orders widget (tabs + desktop list), order list (desktop/mobile), share PnL button, and symbol context/provider.

## Files and subdirectories

| File / Directory | Language | Description |
|------------------|----------|-------------|
| [orders.widget](orders.widget.md) | TSX | `OrdersWidget` and `TabType`; composes script and Orders UI. |
| [orders.ui](orders.ui.md) | TSX | `Orders` — tab panels (All, Pending, TP/SL, Filled, Cancelled, Rejected) with lazy desktop order list. |
| [orders.script](orders.script.md) | TS | `useOrdersScript`, `OrdersBuilderState`; ref forwarding for download. |
| [orderList](orderList/index.md) | — | Desktop/mobile order list, script, context, provider, columns, edit/cancel. |
| [provider](provider/index.md) | — | `SymbolProvider`, `SymbolContext`, `useSymbolContext`. |
| [shareButton](shareButton/index.md) | — | `ShareButton`, `ShareButtonWidget`, `useShareButtonScript`. |
