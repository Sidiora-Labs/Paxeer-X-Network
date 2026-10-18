# entry (index.ts) — Package Public API

## index.ts Responsibilities

Re-exports the public API of the markets package: page components (MarketsHeader, MarketsDataList), list and favorites components, funding components, UI components (collapse, side, dropDown, sheet, symbolInfoBar, horizontalMarkets), deprecated components, types, and providers (MarketsHomePage, MarketsProvider, SymbolDisplay).

## index.ts Exports (Re-exports)

| Export | Source | Description |
|--------|--------|-------------|
| pages/home/marketsHeader | module | Markets header widget and related |
| pages/home/marketsDataList | module | Markets data list widget |
| components/marketsListFull | module | Full markets list |
| components/favoritesListFull | module | Full favorites list |
| components/favoritesDropdownMenu | module | Favorites dropdown menu |
| components/favoritesTabs | module | Favorites tabs |
| components/expandMarkets | module | Expand markets |
| components/subMenuMarkets | module | Sub-menu markets |
| components/marketsList | module | Markets list |
| components/fundingOverview | module | Funding overview |
| components/fundingComparison | module | Funding comparison |
| components/collapseMarkets | module | Collapse markets |
| components/sideMarkets | module | Side markets |
| components/dropDownMarkets | module | Dropdown markets |
| components/marketsSheet | module | Markets sheet |
| components/symbolInfoBar | module | Symbol info bar |
| components/symbolInfoBarFull | module | Full symbol info bar |
| components/horizontalMarkets | module | Horizontal markets |
| deprecated/newListingList | module | New listing list (deprecated) |
| deprecated/favoritesList | module | Favorites list (deprecated) |
| deprecated/recentList | module | Recent list (deprecated) |
| type | module | Types and enums |
| MarketsHomePage | named | From pages/home/page |
| MarketsProvider | named | From components/marketsProvider |
| SymbolDisplay | named | From components/symbolDisplay |

## Dependencies

- **Upstream**: All listed component and type modules under `src`.
- **Downstream**: Apps that import from `@paxeer-network/markets`.

## entry Example

```typescript
import {
  MarketsHomePage,
  MarketsProvider,
  SymbolDisplay,
  MarketsHeaderWidget,
  MarketsDataListWidget,
  MarketsList,
  MarketsListWidget,
  useMarketsListScript,
} from "@paxeer-network/markets";
import type { MarketsPageTab, SortType } from "@paxeer-network/markets";
```
