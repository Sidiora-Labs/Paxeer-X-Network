# tpslPostionType

## Overview

Position type selector for TPSL: **Partial** vs **Full** position. Shows tooltip with explanation and either a read-only label (when `disableSelector`) or a select dropdown.

## Exports

### TPSLPositionTypeWidget

React component. Props = `PositionTypeProps` (from script): value (`PositionType`), onChange, disableSelector.

### PositionTypeProps

| Name | Type | Required | Description |
|------|------|----------|-------------|
| value | `PositionType` | Yes | Current value (PARTIAL / FULL) |
| onChange | `(key: string, value: PositionType) => void` | Yes | Callback with field key and new value |
| disableSelector | `boolean` | No | If true, show text only (no dropdown) |

### TPSLPositionTypeUI

Presentational component; receives `ReturnType<typeof useTPSLPositionTypeScript>`.

## Usage example

```tsx
<TPSLPositionTypeWidget
  value={PositionType.PARTIAL}
  onChange={(key, value) => setOrderValue(key, value)}
  disableSelector={isEditing}
/>
```
