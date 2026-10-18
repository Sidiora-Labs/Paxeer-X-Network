# deprecated — Directory Index

## Directory Responsibilities

The `deprecated` folder contains legacy markets UI components that are still exported from the package but are not the preferred implementation. New code should use the components under `components/` and `pages/home/` instead.

## Key Entities

| Entity | Location | Role | Replacement |
|--------|----------|------|-------------|
| NewListingList | newListingList | New listings list widget | components/marketsListFull or list + filter |
| FavoritesList | favoritesList | Favorites list widget | components/favoritesListFull |
| RecentList | recentList | Recent list widget | components/marketsList or sideMarkets with recent tab |
| DataTable | dataTable | Generic data table / useSort | components/*.ui + utils useSort |

## Subdirectories and Files

| Subdirectory | Main files | Language | Description |
|--------------|------------|----------|-------------|
| newListingList | index.tsx, widget.tsx, *.ui.tsx, *.script.ts | TS/TSX | New listing list UI and script |
| favoritesList | index.tsx, widget.tsx, *.ui.tsx, *.script.ts | TS/TSX | Favorites list UI and script |
| recentList | index.tsx, widget.tsx, *.ui.tsx, *.script.ts | TS/TSX | Recent list UI and script |
| dataTable | index.tsx, useSort.tsx | TSX | Table component and sort hook (useSort also in utils) |

## Note

These modules are re-exported from the package entry (`index.ts`) for backward compatibility. Prefer the non-deprecated components and `utils.useSort` for new code.
