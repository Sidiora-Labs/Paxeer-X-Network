# Markets Package — Root Index

## Module Overview

The `packages/markets/src` directory contains the **markets** UI and logic for the paxeer-network web app: market lists, favorites, funding views, symbol info, and related components. It provides reusable markets components and the main markets home page.

## Module Responsibilities

- **Markets listing**: Full/side/dropdown/horizontal market lists and sheets.
- **Favorites**: Favorites tabs, dropdown, list, and persistence.
- **Funding**: Funding overview, comparison, and rate hints.
- **Symbol**: Symbol info bar (compact/full), symbol display, RWA tooltips.
- **Page**: Markets home page (Markets / Funding tabs) and header/data list.
- **Types & utils**: Shared types (FavoriteInstance, SortType, tab enums), constants, sorting/search helpers.

## Key Entities

| Entity | Type | Role | Entry |
|--------|------|------|--------|
| MarketsProvider | React context + component | Provides symbol, search, comparison props to markets tree | `components/marketsProvider` |
| MarketsHomePage | Component | Main markets page with Markets/Funding tabs | `pages/home/page` |
| FavoriteInstance, SortType, GetColumns | Types | Used by list columns and sorting | `type.ts` |
| MarketsPageTab, MarketsTabName, FundingTabName | Enums | Tab identifiers for markets/funding UI | `type.ts` |
| getPagedData, sortList, useSort, searchBySymbol, useSize | Functions/hooks | List pagination, sort, search, window size | `utils.ts` |

## Top-Level Files

| File | Language | Responsibility | Entry / Link |
|------|----------|----------------|--------------|
| type.ts | TypeScript | Types and enums for markets (FavoriteInstance, SortType, tab enums) | [type.md](type.md) |
| constant.ts | TypeScript | Storage keys for side markets tab/sort | [constant.md](constant.md) |
| utils.ts | TypeScript | Sort, pagination, search, window size utilities | [utils.md](utils.md) |
| version.ts | TypeScript | Package version and global __PAXEER_NETWORK_VERSION__ | [version.md](version.md) |
| entry (index.ts) | TypeScript | Package re-exports and public API | [entry.md](entry.md) |
| icons.tsx | TSX | SVG icon components for markets UI | [icons.md](icons.md) |

## Subdirectories

| Directory | Responsibility | Index |
|-----------|-----------------|--------|
| components | Markets list, favorites, funding, symbol, sheet, dropdown, etc. | [components/index.md](components/index.md) |
| pages | Home page, markets header, markets data list, funding | [pages/index.md](pages/index.md) |
| deprecated | Legacy new listing list, favorites list, recent list, data table | [deprecated/index.md](deprecated/index.md) |

## Search Keywords / Aliases

markets, market list, favorites, funding, symbol info, MarketsProvider, MarketsHomePage, sort, search, tab, RWA, new listing, side markets, drop down markets, horizontal markets, markets sheet.
