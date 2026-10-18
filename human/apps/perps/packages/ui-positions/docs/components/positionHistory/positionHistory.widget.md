# positionHistory.widget

## Overview

Container components for position history: `PositionHistoryWidget` (desktop) and `MobilePositionHistoryWidget` (optional `classNames` for root/content/cell).

## Exports

### PositionHistoryProps

- `onSymbolChange?`, `symbol?`, `pnlNotionalDecimalPrecision?`, `sharePnLConfig?`, `enableSortingStorage?`

### PositionHistoryWidget

- **Props**: `PositionHistoryProps`
- **Behavior**: Uses `usePositionHistoryScript`, renders `PositionHistory` with state and `sharePnLConfig`.

### MobilePositionHistoryWidget

- **Props**: `PositionHistoryProps & { classNames?: { root?; content?; cell? } }`
- **Behavior**: Same script, renders `MobilePositionHistory` with `classNames`.

## Usage example

```tsx
<PositionHistoryWidget symbol={symbol} onSymbolChange={onSymbolChange} />
<MobilePositionHistoryWidget symbol={symbol} classNames={{ root: "my-root" }} />
```
