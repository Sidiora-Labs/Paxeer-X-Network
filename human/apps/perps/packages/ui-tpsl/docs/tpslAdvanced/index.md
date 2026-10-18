# tpslAdvanced

## Overview

Advanced TPSL settings: UI and hook for editing an existing order’s TP/SL with side switch, position type, TP/SL rows, PnL info, and submit/cancel. Registered as sheet and dialog (`TPSLAdvancedSheetId`, `TPSLAdvancedDialogId`).

## Files

| File | Language | Description |
|------|----------|-------------|
| [tpslAdvanced.widget](./tpslAdvanced.widget.md) | TSX | `TPSLAdvancedWidget`, sheet/dialog IDs, registration |
| [tpslAdvanced.ui](./tpslAdvanced.ui.md) | TSX | `TPSLAdvancedUI`, `ArrowRightIcon` – form UI |
| [useTPSLAdvanced.script](./useTPSLAdvanced.script.md) | TS | `useTPSLAdvanced` – order state, validation, submit |

## Exports

- From `tpslAdvanced.ui`: `TPSLAdvancedUI`, `ArrowRightIcon`
- From `useTPSLAdvanced.script`: `useTPSLAdvanced`
- From `tpslAdvanced.widget`: `TPSLAdvancedWidget`, `TPSLAdvancedSheetId`, `TPSLAdvancedDialogId`
