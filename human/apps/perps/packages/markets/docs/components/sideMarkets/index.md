# sideMarkets — Component Module

## sideMarkets Responsibilities

Exports the side markets panel: UI component, script hook, and widget. Used to show a compact list of markets in a side panel (e.g. sidebar) with tab/sort persisted via constant.ts storage keys.

## sideMarkets Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| SideMarkets | Component | UI | Renders side panel list |
| useSideMarketsScript | Hook | Logic | Data, tab, sort, persistence |
| SideMarketsWidget | Component | Widget | Composes script + SideMarkets UI |

## SideMarkets Input/Output

- **Input**: Props from useSideMarketsScript (tabs, selected tab, sort, list data, etc.).
- **Output**: Side panel UI with market list and tab/sort controls.

## useSideMarketsScript

- **Input**: Optional props (e.g. onSymbolSelect).
- **Output**: Tab/sort state (possibly from localStorage via SIDE_MARKETS_SEL_TAB_KEY, SIDE_MARKETS_TAB_SORT_STORAGE_KEY), list data, and handlers.

## Dependencies

- **Upstream**: constant.ts (storage keys), type.ts (MarketsTabName, etc.), utils, @paxeer-network/hooks.
- **Downstream**: Layouts that render a side panel for markets.

## sideMarkets Example

```tsx
import { SideMarketsWidget } from "@paxeer-network/markets";

<SideMarketsWidget />
```
