# type.ts

## type.ts Responsibilities

Defines shared types and enums for the markets package: favorite instance type from `useMarkets`, sort configuration, column getter type, and tab name enums for markets and funding UIs.

## type.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| FavoriteInstance | type | Favorite API from hooks | Return type of `useMarkets()[1]` |
| SortType | type | Sort state | sortKey + sortOrder |
| GetColumns | type | Column factory | (favorite, isFavoriteList) => Column[] |
| MarketsPageTab | enum | Page-level tab | Markets, Funding |
| MarketsTabName | enum | Markets sub-tab | Favorites, Recent, All, Rwa, NewListing |
| FundingTabName | enum | Funding sub-tab | Overview, Comparison |

## FavoriteInstance

- **Input/Output**: Not applicable (type alias).
- **Definition**: `ReturnType<typeof useMarkets>[1]`. Used wherever the markets package needs the favorite list API (add/remove/reorder).

## SortType Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| sortKey | string | No | Property key to sort by |
| sortOrder | SortOrder | No | From `@paxeer-network/ui` (e.g. asc/desc) |

## GetColumns

- **Input**: `favorite: FavoriteInstance`, `isFavoriteList: boolean`.
- **Output**: `Column[]` from `@paxeer-network/ui`. Used to build table columns for market lists with or without favorite actions.

## MarketsPageTab Enum Values

| Value | String |
|-------|--------|
| Markets | "markets" |
| Funding | "funding" |

## MarketsTabName Enum Values

| Value | String |
|-------|--------|
| Favorites | "favorites" |
| Recent | "recent" |
| All | "all" |
| Rwa | "rwa" |
| NewListing | "newListing" |

## FundingTabName Enum Values

| Value | String |
|-------|--------|
| Overview | "overview" |
| Comparison | "comparison" |

## Dependencies

- **Upstream**: `@paxeer-network/hooks` (useMarkets), `@paxeer-network/ui` (SortOrder, Column).
- **Downstream**: Used by list scripts, column definitions, and tab UIs across components and pages.

## type.ts Example

```typescript
import { MarketsPageTab, MarketsTabName, FundingTabName, SortType, GetColumns } from "@paxeer-network/markets";

const tab: MarketsPageTab = MarketsPageTab.Markets;
const sort: SortType = { sortKey: "symbol", sortOrder: "asc" };
const marketsSubTab: MarketsTabName = MarketsTabName.Favorites;
const fundingSubTab: FundingTabName = FundingTabName.Overview;
```
