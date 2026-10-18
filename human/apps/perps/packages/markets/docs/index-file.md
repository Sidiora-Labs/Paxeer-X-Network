# index.ts (package exports)

## Overview

Main entry point for `@paxeer-network/markets`. Re-exports page components, list/sheet/tab components, deprecated components, and shared types.

## Exports

### Pages

- `MarketsHomePage` – Markets home page (tabs: Markets / Funding)
- From `./pages/home/marketsHeader`: markets header exports
- From `./pages/home/marketsDataList`: markets data list exports

### Components

- From `./components/marketsListFull`
- From `./components/favoritesListFull`
- From `./components/favoritesDropdownMenu`
- From `./components/favoritesTabs`
- From `./components/expandMarkets`
- From `./components/subMenuMarkets`
- From `./components/marketsList`
- From `./components/fundingOverview`
- From `./components/fundingComparison`
- From `./components/collapseMarkets`
- From `./components/sideMarkets`
- From `./components/dropDownMarkets`
- From `./components/marketsSheet`
- From `./components/symbolInfoBar`
- From `./components/symbolInfoBarFull`
- From `./components/horizontalMarkets`

### Providers & display

- `MarketsProvider` – Context provider for symbol, search, and comparison props
- `SymbolDisplay` – Displays symbol with optional token icon and display name

### Types

- From `./type` (e.g. `MarketsPageTab`, `SortType`, `GetColumns`, etc.)

### Deprecated

- From `./deprecated/newListingList`
- From `./deprecated/favoritesList`
- From `./deprecated/recentList`

## Usage example

```tsx
import {
  MarketsHomePage,
  MarketsProvider,
  SymbolDisplay,
  type MarketsPageTab,
} from "@paxeer-network/markets";
```
