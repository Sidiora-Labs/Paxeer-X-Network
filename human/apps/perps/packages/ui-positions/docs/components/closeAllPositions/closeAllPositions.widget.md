# closeAllPositions.widget

## Overview

Button/UI to trigger close-all-positions flow. Optional symbol filter and onSuccess callback.

## Props (CloseAllPositionsWidgetProps)

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `className` | `string` | No | Root class. |
| `style` | `CSSProperties` | No | Root style. |
| `symbol` | `string` | No | If set, only close positions for this symbol. |
| `onSuccess` | `() => void` | No | Called after successful close all. |

## Usage example

```tsx
<CloseAllPositionsWidget symbol={symbol} onSuccess={refetch} />
```
