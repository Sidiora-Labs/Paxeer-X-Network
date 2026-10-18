# ui-order-entry

> Location: `packages/ui-order-entry/src`

## Overview

Order entry UI package for the paxeer-network trading interface. Provides the main order entry form (limit, market, stop, scaled, trailing stop), TP/SL, slippage, fees, and related dialogs and widgets.

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| [entry (index.ts)](entry.md) | TypeScript | Package public exports |
| [types.ts](types.md) | TypeScript | Input focus and quantity input type enums |
| [utils.ts](utils.md) | TypeScript | BBO helpers, scaled order messages, safe number |
| [version.ts](version.md) | TypeScript | Package version registration |
| [orderEntry.ui.tsx](orderEntry.ui.md) | React/TSX | Main OrderEntry UI component |
| [orderEntry.script.ts](orderEntry.script.md) | TypeScript | useOrderEntryScript hook and order state |
| [orderEntry.widget.tsx](orderEntry.widget.md) | React/TSX | OrderEntryWidget wrapper |
| [orderEntryProvider.tsx](orderEntryProvider.md) | React/TSX | Order entry context provider |
| [orderEntryContext.tsx](orderEntryContext.md) | React/TSX | Order entry context and useOrderEntryContext |

## Subdirectories

- [hooks/](hooks/index.md) – Hooks for BBO, focus/blur, price input container, ask/bid
- [components/](components/index.md) – Order type select, header, order input, TPSL, fee, dialogs, etc.
