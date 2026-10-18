# index.ts

> Location: `packages/ui-leverage/src/index.ts`

## Overview

Package entry point. Re-exports account-level and symbol-level leverage components and hooks, and registers simple dialog/sheet presets for the leverage editor.

## Exports

### Components & widgets

- **LeverageEditor**, **LeverageEditorProps** — Account-level leverage editor widget (from `leverage.widget`).
- **Leverage**, **LeverageSlider**, **LeverageHeader** — Presentational leverage UI (from `leverage.ui`).
- **LeverageProps**, **LeverageSliderProps**, **LeverageHeaderProps** — Props types for the above.
- **useLeverageScript**, **LeverageScriptReturns** — Hook for account leverage state (from `leverage.script`).
- **SymbolLeverageWidget**, **SymbolLeverageWidgetProps** — Symbol-level leverage widget (from `symbolLeverage`).
- **SymbolLeverageSheetId**, **SymbolLeverageDialogId** — Registered sheet/dialog IDs for symbol leverage.
- **SymbolLeverage**-related exports — All symbol leverage exports via `export * from "./symbolLeverage"`.

### Constants (Dialog/Sheet IDs)

| Name | Value | Description |
|------|--------|-------------|
| `LeverageWidgetWithDialogId` | `"LeverageWidgetWithDialog"` | Dialog ID for account leverage editor |
| `LeverageWidgetWithSheetId` | `"LeverageWidgetWithSheet"` | Sheet ID for account leverage editor |

### Side effects

- **registerSimpleDialog**(LeverageWidgetWithDialogId, LeverageEditor, { title, size: "md" })
- **registerSimpleSheet**(LeverageWidgetWithSheetId, LeverageEditor, { title })

## Usage example

```tsx
import {
  LeverageEditor,
  Leverage,
  useLeverageScript,
  LeverageWidgetWithDialogId,
  LeverageWidgetWithSheetId,
} from "@paxeer-network/ui-leverage";
import { modal } from "@paxeer-network/ui";

// Open account leverage in dialog
modal.show(LeverageWidgetWithDialogId);

// Or use the widget directly with script
function MyLeverageForm() {
  const state = useLeverageScript({ close: () => {} });
  return <Leverage {...state} />;
}
```
