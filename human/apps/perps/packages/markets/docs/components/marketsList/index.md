# marketsList — Component Module

## marketsList Responsibilities

Exports the markets list UI component, script hook, and widget that display a sortable/searchable list of markets (and optional favorites behavior). Used on the markets tab and anywhere a compact or full list of markets is needed.

## marketsList Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MarketsList | Component | UI | Renders the list table from script data |
| useMarketsListScript | Hook | Logic | Data, sort, search, columns for MarketsList |
| MarketsListWidget | Component | Widget | Composes script + MarketsList UI |

## MarketsList Input/Output

- **Input**: Props from useMarketsListScript (list data, columns, sort, onSort, etc.).
- **Output**: Renders table/list UI; typically used via MarketsListWidget.

## useMarketsListScript

- **Input**: Optional config (e.g. favorite instance, isFavoriteList) depending on implementation.
- **Output**: List data, sort state, search state, columns (GetColumns), and handlers for the list UI.

## MarketsListWidget

- **Input**: Optional props passed to script and UI (e.g. className).
- **Output**: Full list widget with script and UI composed.

## Dependencies

- **Upstream**: type.ts (GetColumns, SortType), utils (useSort, searchBySymbol), @paxeer-network/hooks (useMarkets), @paxeer-network/ui (Column, TableSort).
- **Downstream**: Markets data list page, side/dropdown markets when reusing list logic.

## marketsList Example

```tsx
import { MarketsListWidget, MarketsList, useMarketsListScript } from "@paxeer-network/markets";

<MarketsListWidget className="oui-mt-4" />

// Or compose manually:
function CustomList() {
  const script = useMarketsListScript();
  return <MarketsList {...script} />;
}
```
