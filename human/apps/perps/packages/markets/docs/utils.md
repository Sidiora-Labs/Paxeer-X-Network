# utils.ts

## utils.ts Responsibilities

Provides list pagination, sorting, search-by-symbol, and window size utilities used by markets lists and tables. Supports numeric, date, and string comparison for sorting; symbol and displayName for search.

## utils.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| getPagedData | function | Pagination | Returns one page of a list by pageSize and pageIndex |
| sortList | function | Sort | Sorts list by SortType (sortKey, sortOrder) |
| useSort | hook | Sort state | Manages sort state and getSortedList |
| searchBySymbol | function | Search | Filters and ranks list by symbol/displayName |
| useSize | hook | Window size | Returns current window width and height |

## getPagedData

- **Input**: `list: any[]`, `pageSize: number`, `pageIndex: number` (1-based).
- **Output**: `any[]` — the slice of `list` for that page. Returns `[]` if page is out of range.

## sortList

- **Input**: `list: any[]`, `sort?: SortType` (sortKey, sortOrder from `type.ts`).
- **Output**: New array sorted by `sortKey`; respects `sortOrder` (asc/desc). Uses internal `compareValues` (numbers, dates, strings).

## useSort

- **Input**: `initialSort?: SortType`, `onSortChange?: (sort?: SortType) => void`.
- **Output**: `{ sort, onSort, getSortedList }`. `onSort` accepts `TableSort` from UI; `getSortedList(list)` returns sorted list.

## searchBySymbol

- **Input**: `list: T[]` (items with `symbol` and optional `displayName`), `searchValue?: string`, `formatString?: string` (for `formatSymbol`).
- **Output**: `T[]` — filtered and ordered: exact match first, then starts-with, then other matches; each group sorted by symbol/displayName.

## useSize

- **Input**: None.
- **Output**: `{ width, height }` — current `window.innerWidth` and `window.innerHeight`; updates on resize.

## Dependencies

- **Upstream**: `react`, `@paxeer-network/ui` (TableSort), `@paxeer-network/utils` (formatSymbol), `./type` (SortType).
- **Downstream**: List/table components that need pagination, sort, or search.

## utils.ts Example

```typescript
import { getPagedData, sortList, useSort, searchBySymbol, useSize } from "@paxeer-network/markets";

const page = getPagedData(allItems, 20, 1);
const sorted = sortList(items, { sortKey: "symbol", sortOrder: "asc" });
const filtered = searchBySymbol(markets, "PERP");

function MyList() {
  const { sort, onSort, getSortedList } = useSort();
  const { width, height } = useSize();
  const data = getSortedList(filteredList);
  return /* ... */;
}
```
