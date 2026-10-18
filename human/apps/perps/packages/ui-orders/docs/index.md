# ui-orders (src)

Overview of the `@paxeer-network/ui-orders` package source. This directory contains the orders UI: main Orders widget with tabs, desktop/mobile order lists, share button, symbol provider, and shared types/utilities.

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| [entry](entry.md) | TypeScript | Package entry point; re-exports `OrdersWidget`, `TabType`, order list widgets, and hooks. |
| [type](type.md) | TypeScript | `EditType` enum for order edit fields (quantity, price, triggerPrice, etc.). |
| [version](version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration. |

## Directories

| Directory | Description |
|-----------|-------------|
| [utils](utils/index.md) | Order badges, status, gray-cell, bracket PnL, notional, and API type conversion helpers. |
| [components](components/index.md) | Orders widget, order list (desktop/mobile), share button, and symbol provider. |
