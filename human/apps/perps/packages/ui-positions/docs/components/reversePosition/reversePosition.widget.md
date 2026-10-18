# reversePosition.widget

## Overview

Dialog component for reversing a position. Registered as `ReversePositionDialogId`. Can be opened with position and optional `close`, `resolve`, `reject` for imperative control.

## Props (ReversePositionWidgetProps)

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `position` | `API.PositionExt \| API.PositionTPSLExt` | Yes | Position to reverse. |
| `close` | `() => void` | No | Close handler. |
| `resolve` | `(value?: any) => void` | No | Resolve promise on success. |
| `reject` | `(reason?: any) => void` | No | Reject on cancel/error. |

## Exports

- `ReversePositionWidget`, `ReversePositionDialogId`
- Dialog is registered via `registerSimpleDialog(ReversePositionDialogId, ReversePositionWidget, { size: "sm", title: ... })`.

## Usage example

```tsx
const { open } = useModal();
open(ReversePositionDialogId, { position: record });
// or
<ReversePositionWidget position={position} resolve={resolve} reject={reject} />
```
