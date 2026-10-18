# editorPopover

## Overview

Provides **PositionTPSLPopover**, a clickable wrapper (or custom children) that opens the TPSL dialog via `modal.show(TPSLDialogId, ...)`. Used as the “Edit TPSL” entry point from position rows or detail views.

## Exports

### PositionTPSLPopover

React component that opens the TPSL dialog on click with the given position and optional order (for editing).

#### Props

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| position | `API.Position` | Yes | — | Current position |
| order | `API.AlgoOrder` | No | — | Existing algo order when editing |
| label | `string` | No | — | Button label when using default button |
| baseDP | `number` | No | — | Base asset decimal places |
| quoteDP | `number` | No | — | Quote asset decimal places |
| buttonProps | `ButtonProps` | No | — | Props for the default button |
| isEditing | `boolean` | No | — | Whether opening for edit |
| children | `ReactNode` | No | — | Custom trigger; if absent, uses a default outlined button |

#### Behavior

- On click, calls `modal.show(TPSLDialogId, { order, symbol, baseDP, quoteDP, positionType, isEditing, position })`.
- If `isEditing` and order is positional TPSL, uses `PositionType.FULL`, else `PositionType.PARTIAL`.

## Usage example

```tsx
import { PositionTPSLPopover } from "@paxeer-network/ui-tpsl";

<PositionTPSLPopover
  position={position}
  order={order}
  label="Edit TP/SL"
  baseDP={4}
  quoteDP={2}
  isEditing={!!order}
/>
```
