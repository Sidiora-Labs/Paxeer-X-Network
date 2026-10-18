# marketsProvider/index.tsx

## marketsProvider Responsibilities

Provides `MarketsContext` and `MarketsProvider` to supply the markets tree with current symbol, search value, search handlers, and optional comparison/nav props. Used as the wrapper for the markets home page and any screen that needs shared markets state.

## marketsProvider Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MarketsContext | React context | Context instance | Holds MarketsContextState |
| MarketsProviderProps | type | Props for provider | symbol, onSymbolChange, navProps, comparisonProps |
| MarketsProvider | FC | Provider component | Wraps children with context value |
| useMarketsContext | hook | Consumer | Returns current context state |

## MarketsProviderProps

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| symbol | string | No | Current symbol |
| onSymbolChange | (symbol: API.Symbol) => void | No | Called when symbol changes |
| navProps | object | No | Mobile nav: logo, routerAdapter, leftNav |
| comparisonProps | object | No | exchangesName, exchangesIconSrc for funding comparison |

## MarketsContextState

- **symbol**, **onSymbolChange**: Current symbol and change callback.
- **searchValue**, **onSearchValueChange**, **clearSearchValue**: Search state and handlers.
- **comparisonProps**: Pass-through for comparison UI.

## MarketsProvider Input/Output

- **Input**: Props (symbol, onSymbolChange, navProps, comparisonProps, children).
- **Output**: Renders `MarketsContext.Provider` with memoized value; no direct UI other than children.

## useMarketsContext

- **Input**: None (must be used under MarketsProvider).
- **Output**: Current `MarketsContextState`. Throws or returns partial state if used outside provider (context default is `{}`).

## Dependencies

- **Upstream**: react, @paxeer-network/types (API, RouterAdapter), @paxeer-network/ui-scaffold (LeftNavProps).
- **Downstream**: MarketsHomePage, and any component that needs symbol/search (e.g. header, data list, symbol bar).

## marketsProvider Example

```tsx
import { MarketsProvider, useMarketsContext } from "@paxeer-network/markets";

<MarketsProvider
  symbol={symbol}
  onSymbolChange={setSymbol}
  comparisonProps={{ exchangesName: "paxeer-network", exchangesIconSrc: "/paxeer-network.png" }}
>
  <MarketsHeaderWidget />
  <MarketsDataListWidget />
</MarketsProvider>

function SearchConsumer() {
  const { searchValue, onSearchValueChange, clearSearchValue } = useMarketsContext();
  return <input value={searchValue} onChange={(e) => onSearchValueChange(e.target.value)} />;
}
```
