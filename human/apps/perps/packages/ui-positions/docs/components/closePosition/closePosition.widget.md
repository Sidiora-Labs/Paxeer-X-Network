# closePosition.widget

## Overview

Renders close-position UI for the current row (from `usePositionsRowContext`). Uses `useScreen()`: desktop shows `DesktopClosePosition`, mobile shows `MobileClosePosition`.

## Props

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `type` | from `ClosePositionScriptProps` | No | Optional close type. |

## Usage example

```tsx
// Inside a position row (provider already set)
<ClosePositionWidget />
```
