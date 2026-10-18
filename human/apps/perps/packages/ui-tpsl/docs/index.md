# @paxeer-network/ui-tpsl

> Location: `packages/ui-tpsl/src`

## Overview

`@paxeer-network/ui-tpsl` provides React UI components and widgets for Take Profit / Stop Loss (TPSL) and bracket order management in the paxeer-network trading UI. It supports position-level TPSL, simple TP/SL dialogs, advanced TPSL settings, order detail views, and bracket order editing.

## Package exports (index.ts)

The main entry re-exports:

- **PositionTPSLPopover** – Popover/button to open TPSL edit (from `editorPopover`)
- **PositionTPSLSheet** – Sheet wrapper for TPSL form (from `editorSheet`)
- **TPSLPositionTypeWidget** – Position type selector (Full/Partial)
- **tpslAdvanced** – Advanced TPSL UI and hooks
- **tpslDetail** – TPSL detail dialog/sheet
- **tpslSimpleDialog** – Simple TP or SL dialog/sheet
- **editBracketOrder** – Edit bracket order dialog/sheet
- **positionTPSL** – Core TPSL widget, dialog/sheet IDs, confirm component
- **CloseToLiqPriceIcon** – Icon + tooltip for SL price near liquidation

## Directory structure

| Directory | Description |
|-----------|-------------|
| [components](./components/index.md) | Shared TPSL components (input row, position type, order info, PnL, icons) |
| [positionTPSL](./positionTPSL/index.md) | Core position TPSL form, widget, and confirm dialog |
| [tpslAdvanced](./tpslAdvanced/index.md) | Advanced TPSL settings UI and hook |
| [tpslDetail](./tpslDetail/index.md) | TPSL order detail view (dialog/sheet) |
| [tpslSimpleDialog](./tpslSimpleDialog/index.md) | Simple TP-only or SL-only dialog/sheet |
| [editBracketOrder](./editBracketOrder/index.md) | Edit bracket order dialog/sheet |
| [pnlInput](./pnlInput/index.md) | PnL-based input builder and UI |

## Root-level files

| File | Language | Description |
|------|----------|-------------|
| [version](./version.md) | TypeScript | Package version registration on `window.__PAXEER_NETWORK_VERSION__` |
| [editorSheet](./editorSheet.md) | TSX | `PositionTPSLSheet` – sheet wrapper for TPSL form |
| [editorPopover](./editorPopover.md) | TSX | `PositionTPSLPopover` – trigger to open TPSL dialog |
