# symbolContext

## Overview

React context holding symbol metadata for formatting and validation (decimal places, ticks, base/quote, min/max quote).

## Exports

### `SymbolContextState`

| Property | Type | Description |
|----------|------|-------------|
| `base_dp` | `number` | Base asset decimal places. |
| `quote_dp` | `number` | Quote asset decimal places. |
| `base_tick` | `number` | Base tick size. |
| `quote_tick` | `number` | Quote tick size. |
| `base` | `string` | Base symbol. |
| `quote` | `string` | Quote symbol. |
| `symbol` | `string` | Full symbol. |
| `origin` | `API.SymbolExt` | Full symbol info. |
| `quote_min` | `number` | Min quote size. |
| `quote_max` | `number` | Max quote size. |

### `SymbolContext`, `useSymbolContext()`

Context and hook to read the state.

## Usage example

```tsx
const { quote_dp, base_dp, base_tick } = useSymbolContext();
const formatted = value.toFixed(quote_dp);
```
