# positionTPSL

## Overview

Core position TPSL flow: widget, UI, builder hook, and confirm dialog. Registers `TPSLSheetId` and `TPSLDialogId` with the shared modal/sheet system and provides the main form for creating/editing position-level TP/SL orders.

## Files

| File | Language | Description |
|------|----------|-------------|
| [tpsl.widget](./tpsl.widget.md) | TSX | `TPSLWidget`, `TPSLDialogId`, `TPSLSheetId`, `TPSLWidgetProps`; registers sheet/dialog |
| [tpsl.ui](./tpsl.ui.md) | TSX | `TPSL` – presentational form (order info, position type, TP/SL rows, PnL, actions) |
| [useTPSL.script](./useTPSL.script.md) | TSX | `useTPSLBuilder`, `TPSLBuilderOptions`, `TPSLBuilderState` – state and submit logic |
| [positionTpslConfirm](./positionTpslConfirm.md) | TSX | `PositionTPSLConfirm` – confirm modal content (summary + optional “don’t ask again”) |

## Exports (from index)

- `TPSLWidget`, `TPSLDialogId`, `TPSLSheetId`, `TPSLWidgetProps`, `PositionTPSLConfirm`
- All exports from `tpsl.widget`, `tpsl.ui`, `useTPSL.script`
