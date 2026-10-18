# tpsl.widget

## Overview

Composes the position TPSL form with the builder hook and registers the TPSL sheet and dialog with the shared UI modal system. This is the component rendered when opening TPSL via `TPSLSheetId` or `TPSLDialogId`.

## Exports

### TPSLWidget

React component that uses `useTPSLBuilder` and renders `TPSL` with cancel/complete callbacks.

#### Props (TPSLWidgetProps)

Extends `TPSLBuilderOptions` and `TPSLProps`:

| Name | Type | Required | Description |
|------|------|----------|-------------|
| onTPSLTypeChange | `(type: AlgoOrderRootType) => void` | No | Callback when TPSL type changes |
| onCancel | `() => void` | No | Called when user cancels |
| onComplete | `() => void` | No | Called after successful submit |
| close | `() => void` | No | Programmatic close (e.g. from modal) |
| (TPSLBuilderOptions) | — | — | symbol, position, order, isEditing, positionType, onConfirm, close |
| (TPSLProps) | — | — | withTriggerPrice |

### TPSLSheetId

Constant: `"TPSLSheetId"`. Used with `registerSimpleSheet` to show TPSL in a sheet.

### TPSLDialogId

Constant: `"TPSLDialogId"`. Used with `registerSimpleDialog`; content width `oui-w-[420px]`.

## Usage example

```tsx
import { TPSLWidget, TPSLDialogId } from "@paxeer-network/ui-tpsl";
import { modal } from "@paxeer-network/ui";

modal.show(TPSLDialogId, {
  symbol: position.symbol,
  position,
  baseDP: 4,
  quoteDP: 2,
  onComplete: () => refresh(),
});
```
