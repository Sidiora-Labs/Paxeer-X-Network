# leverage.widget.tsx

> Location: `packages/ui-leverage/src/leverage.widget.tsx`

## Overview

Account-level leverage editor widget. Composes `useLeverageScript` and `Leverage` so a single component can be used in dialogs/sheets or inline.

## Exports

### LeverageEditor

| Name | Type | Description |
|------|------|-------------|
| `LeverageEditor` | `FC<LeverageEditorProps>` | Widget component |
| `LeverageEditorProps` | type | Props for the widget |

### LeverageEditorProps

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `close` | () => void | No | Callback when dialog/sheet should close (e.g. after save) |

## Usage example

```tsx
import { LeverageEditor, LeverageWidgetWithDialogId } from "@paxeer-network/ui-leverage";
import { modal } from "@paxeer-network/ui";

// Open as registered dialog
modal.show(LeverageWidgetWithDialogId);

// Or render inline with close handler
<LeverageEditor close={() => setOpen(false)} />
```
