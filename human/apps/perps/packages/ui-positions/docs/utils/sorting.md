# sorting

## Overview

Provides list sorting by a key and order (`asc`/`desc`), and a React hook `useSort` that integrates with table sort UI (`TableSort`).

## Exports

### sortList(list, sort?)

- **Parameters**
  - `list`: `any[]` – Array to sort (not mutated).
  - `sort`: `SortType | undefined` – `{ sortKey, sortOrder }`; if omitted, returns list as-is.
- **Returns**: New sorted array.
- **Description**: Compares values by type (numbers, dates, strings) and applies `sortOrder`.

### useSort(initialSort?, onSortChange?)

- **Parameters**
  - `initialSort`: `SortType | undefined` – Initial sort state.
  - `onSortChange`: `(sort?: SortType) => void` – Called when sort changes (e.g. for persistence).
- **Returns**: `{ sort, onSort, getSortedList }`
  - `sort`: Current `SortType` or undefined.
  - `onSort`: Handler for table `TableSort` (maps to `SortType`).
  - `getSortedList(list)`: Returns sorted copy of `list` with current `sort`.

## Usage example

```typescript
import { useSort, sortList } from "./sorting";
const { sort, onSort, getSortedList } = useSort(undefined, (s) => setStoredSort(s));
const sorted = getSortedList(positions);
```
