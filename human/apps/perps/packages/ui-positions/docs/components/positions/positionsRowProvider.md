# positionsRowProvider

## Overview

Provider that wraps each position row and provides close-order state: quantity, price, type, submit, and symbol info. Uses `usePositionClose` and feeds `PositionsRowContext`.

## Props

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `position` | `API.PositionExt \| API.PositionTPSLExt` | Yes | Row position. |
| `mutatePositions` | `() => void` | No | Called after successful close (e.g. refetch). |
| `children` | `ReactNode` | Yes | Row content (table cells or mobile cell). |

## Usage example

```tsx
<PositionsRowProvider position={record} mutatePositions={refetch}>
  {children}
</PositionsRowProvider>
```
