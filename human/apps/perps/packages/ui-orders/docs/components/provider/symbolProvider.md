# symbolProvider

## Overview

Provider that fills `SymbolContext` from `useSymbolsInfo()[symbol]`: maps symbol info to `SymbolContextState` (base_dp, quote_dp, ticks, base, quote, origin, quote_min/max) and wraps children.

## Exports

### `SymbolProvider`

`FC<PropsWithChildren<FormatterProviderProps>>`.

### `FormatterProviderProps`

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | `string` | Yes | Current symbol (e.g. PERP_BTC_USDC). |

## Usage example

```tsx
<SymbolProvider symbol={order.symbol}>
  <PriceCell order={order} />
</SymbolProvider>
```
