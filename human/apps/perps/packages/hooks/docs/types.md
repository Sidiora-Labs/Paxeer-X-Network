# types

## Overview

Calculator and scheduler types used by the paxeer-network calculator layer: context interface, scope enum, calculator interface, and scheduler interface.

## Type list

### `CalculatorCtx`

Context passed to calculators: account info, symbols info, funding rates, portfolio, tokens info, and methods `get`, `outputToValue`, `saveOutput`, `clearCache`. Includes `isReady` getter.

#### Fields

| Field | Type | Description |
|-------|------|-------------|
| `accountInfo` | `API.AccountInfo \| undefined` | Account info |
| `symbolsInfo` | `Record<string, API.SymbolExt> \| undefined` | Symbols metadata |
| `fundingRates` | `Record<string, API.FundingRate> \| undefined` | Funding rates by symbol |
| `portfolio` | `Portfolio \| undefined` | Portfolio data |
| `tokensInfo` | `API.Token[] \| undefined` | Token list |
| `get` | `(fn) => T` | Run a function with current output |
| `outputToValue` | `() => any` | Get current output value |
| `saveOutput` | `(name, data) => void` | Save output by name |
| `clearCache` | `() => void` | Clear cache |
| `isReady` | `boolean` | Whether context is ready |

### `CalculatorScope`

Enum of calculation scopes: `MARK_PRICE`, `INDEX_PRICE`, `POSITION`, `ORDER`, `TICK_PRICE`, `ORDER_BOOK`, `PORTFOLIO`, `MARKET`.

### `Calculator<T>`

Calculator definition: `name`, `calc(scope, data, ctx)`, `cache(result)`, `update(data, scope)`, optional `destroy()`.

### `CalculatorScheduler`

Scheduler that runs calculators: `calc(scope, calculators, data, ctx)` (async) and `update(scope, calculators, data)`.

## Usage example

```ts
import type { CalculatorCtx, CalculatorScope, Calculator } from "@paxeer-network/hooks";

const myCalculator: Calculator<number> = {
  name: "myCalc",
  calc: (scope, data, ctx) => (ctx.isReady ? 1 : null),
  cache: () => {},
  update: () => {},
};
```
