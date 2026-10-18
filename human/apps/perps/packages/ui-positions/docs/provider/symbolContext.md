# symbolContext

## Overview

React context holding symbol formatting and metadata (decimal places, ticks, base/quote, min/max) for the current position row.

## Exports

### SymbolContextState (interface)

| Property | Type | Description |
| -------- | ----- | ----------- |
| `base_dp` | `number` | Base asset decimal places. |
| `quote_dp` | `number` | Quote asset decimal places. |
| `base_tick` | `number` | Base tick size. |
| `quote_tick` | `number` | Quote tick size. |
| `base` | `string` | Base symbol. |
| `quote` | `string` | Quote symbol. |
| `symbol` | `string` | Full symbol. |
| `origin` | `API.SymbolExt` | Raw symbol info. |
| `quote_min` | `number` | Quote min. |
| `quote_max` | `number` | Quote max. |

### SymbolContext

- React context instance (default value empty object cast to `SymbolContextState`).

### useSymbolContext()

- **Returns**: Current `SymbolContextState` from nearest `SymbolProvider`.

## Usage example

```typescript
import { SymbolContext, useSymbolContext } from "./symbolContext";
const { quote_dp, base_tick } = useSymbolContext();
```
