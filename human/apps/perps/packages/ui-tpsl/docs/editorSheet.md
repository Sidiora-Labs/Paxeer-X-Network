# editorSheet

## Overview

Provides **PositionTPSLSheet**, a sheet (bottom drawer) wrapper for the position TPSL form. Used when the app shows TPSL in a sheet instead of a dialog. Integrates with `useModal().hide` for cancel and passes through `TPSLWidget` props.

## Exports

### PositionTPSLSheet

React component that renders the TPSL form inside a sheet and wires cancel to the modal `hide` function.

#### Props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| order | `API.AlgoOrder` | No | Existing algo order when editing |
| symbolInfo | `API.SymbolExt` | Yes | Symbol info accessor |
| isEditing | `boolean` | No | Whether the sheet is for editing an existing order |
| (TPSLWidgetProps) | — | — | All props from `TPSLWidget` (symbol, position, onCancel, onComplete, etc.) |

#### Behavior

- If `isEditing` and `order?.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL`, defaults `positionType` to `PositionType.FULL`, otherwise `PositionType.PARTIAL`.
- `onCancel` is set to `hide` from `useModal()`.

## Usage example

```tsx
import { PositionTPSLSheet } from "@paxeer-network/ui-tpsl";

<PositionTPSLSheet
  symbol={symbol}
  symbolInfo={symbolInfo}
  position={position}
  isEditing={!!order}
  order={order}
  onComplete={() => { /* refresh */ }}
/>
```
