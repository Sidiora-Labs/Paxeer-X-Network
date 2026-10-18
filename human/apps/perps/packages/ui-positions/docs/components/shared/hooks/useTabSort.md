# useTabSort

## Overview

Persists sort state per tab (Positions vs Position History) in session storage. Used by positions and position history to remember sort key and order.

## Exports

### PositionsTabName (enum)

- `Positions = "positions"`
- `PositionHistory = "positionHistory"`

### useTabSort(options)

- **Parameters**: `{ storageKey: string }` (e.g. `TRADING_POSITIONS_SORT_STORAGE_KEY`).
- **Returns**: `{ tabSort, onTabSort }`
  - `tabSort`: `Record<PositionsTabName, SortType>` – current sort per tab.
  - `onTabSort(type)`: Returns `(sort?: SortType) => void` to update that tab’s sort.

## Usage example

```tsx
const { tabSort, onTabSort } = useTabSort({ storageKey: TRADING_POSITIONS_SORT_STORAGE_KEY });
const sort = tabSort[PositionsTabName.Positions];
const setSort = onTabSort(PositionsTabName.Positions);
```
