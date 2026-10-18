# components — Directory Index

## Directory Responsibilities

The `components` folder holds reusable markets UI: market lists (full, side, dropdown, horizontal, sheet, expand, collapse), favorites (tabs, dropdown, list, empty), funding (overview, comparison, rate hint), symbol info (bar, full bar, display, RWA tooltips), search input, and the MarketsProvider context. It does not contain the top-level page layout; that lives under `pages/home`.

## Key Entities

| Entity | Location | Role | Dependency |
|--------|----------|------|-------------|
| MarketsProvider | marketsProvider/index.tsx | Context + component for symbol, search, comparison | @paxeer-network/types, ui-scaffold |
| MarketsList / MarketsListWidget | marketsList | Table/list of markets | useMarketsListScript |
| SideMarkets / SideMarketsWidget | sideMarkets | Side panel markets list | useSideMarketsScript |
| SymbolInfoBar / SymbolInfoBarWidget | symbolInfoBar | Compact symbol info bar | useSymbolInfoBarScript |
| FundingOverview / FundingWidget | fundingOverview | Funding overview table | fundingOverview.script |
| FundingComparison | fundingComparison | Funding comparison view | useEXchanges |

## Files and Exports by Subdirectory

| Subdirectory | Main exports | Language | Detail doc |
|--------------|--------------|----------|------------|
| marketsProvider | MarketsProvider, MarketsContext, useMarketsContext, MarketsProviderProps | TSX | [marketsProvider/index.md](marketsProvider/index.md) |
| marketsList | MarketsList, useMarketsListScript, MarketsListWidget | TS/TSX | [marketsList/index.md](marketsList/index.md) |
| marketsListFull | Full markets list widget and script | TS/TSX | — |
| favoritesListFull | Favorites list full widget and script | TS/TSX | — |
| favoritesDropdownMenu | Favorites dropdown menu | TS/TSX | — |
| favoritesTabs | Favorites tabs widget and script | TS/TSX | — |
| favoritesEmpty | Empty state for favorites | TSX | — |
| expandMarkets | Expand markets widget and script | TS/TSX | — |
| collapseMarkets | Collapse markets | TSX | — |
| subMenuMarkets | Sub-menu markets widget and script | TS/TSX | — |
| sideMarkets | SideMarkets, useSideMarketsScript, SideMarketsWidget | TS/TSX | [sideMarkets/index.md](sideMarkets/index.md) |
| dropDownMarkets | Dropdown markets widget and script | TS/TSX | — |
| marketsSheet | Markets sheet widget and script | TS/TSX | — |
| symbolInfoBar | SymbolInfoBar, useSymbolInfoBarScript, SymbolInfoBarWidget | TS/TSX | [symbolInfoBar/index.md](symbolInfoBar/index.md) |
| symbolInfoBarFull | Full symbol info bar widget and script | TS/TSX | — |
| symbolDisplay | SymbolDisplay component | TSX | — |
| horizontalMarkets | Horizontal markets widget and script | TS/TSX | — |
| fundingOverview | Funding overview widget and script | TS/TSX | — |
| fundingComparison | Funding comparison widget and script | TS/TSX | — |
| fundingRateHint | Funding rate hint widget and script | TS/TSX | — |
| searchInput | Search input component | TSX | — |
| rwaDotTooltip | RWA dot tooltip | TSX | — |
| rwaTab | RWA tab | TSX | — |
| shared | column.tsx, useTabSort, useFavoritesExtraProps | TS/TSX | — |

## Subdirectories (Links)

- [marketsProvider](marketsProvider/index.md)
- [marketsList](marketsList/index.md)
- [sideMarkets](sideMarkets/index.md)
- [symbolInfoBar](symbolInfoBar/index.md)
- (Other component folders: see table above; detail docs can be added per file when needed.)
