# constant.ts

## constant.ts Responsibilities

Exports storage keys used by the side markets UI for persisting tab selection and sort order in local storage.

## constant.ts Exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| SIDE_MARKETS_TAB_SORT_STORAGE_KEY | string | Storage key | Persists side markets tab sort preference |
| SIDE_MARKETS_SEL_TAB_KEY | string | Storage key | Persists selected tab in side markets |

## Constant Values

| Constant | Value |
|----------|--------|
| SIDE_MARKETS_TAB_SORT_STORAGE_KEY | "paxeer-network_side_markets_tab_sort" |
| SIDE_MARKETS_SEL_TAB_KEY | "paxeer-network_side_markets_sel_tab_key" |

## Input/Output

- **Input**: None (constants only).
- **Output**: String keys for `localStorage` (or equivalent) used by side markets components.

## Dependencies

- **Upstream**: None.
- **Downstream**: Side markets component(s) that read/write tab and sort state.

## constant.ts Example

```typescript
import { SIDE_MARKETS_TAB_SORT_STORAGE_KEY, SIDE_MARKETS_SEL_TAB_KEY } from "@paxeer-network/markets";

localStorage.setItem(SIDE_MARKETS_SEL_TAB_KEY, "favorites");
const savedSort = localStorage.getItem(SIDE_MARKETS_TAB_SORT_STORAGE_KEY);
```
