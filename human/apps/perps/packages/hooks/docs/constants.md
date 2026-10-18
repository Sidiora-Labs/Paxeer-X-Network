# constants

## Overview

Default orderbook tick sizes and symbol depths for specific symbols.

## Exports

| Name                    | Type                            | Description                                  |
| ----------------------- | ------------------------------- | -------------------------------------------- |
| `DEFAULT_TICK_SIZES`    | `Record<PropertyKey, string>`   | Default tick sizes (e.g. PERP_BTC_USDC: "1") |
| `DEFAULT_SYMBOL_DEPTHS` | `Record<PropertyKey, number[]>` | Default depth levels per symbol              |

## Usage example

```ts
import {
  DEFAULT_TICK_SIZES,
  DEFAULT_SYMBOL_DEPTHS,
} from "@paxeer-network/hooks";

const tick = DEFAULT_TICK_SIZES["PERP_BTC_USDC"]; // "1"
```
