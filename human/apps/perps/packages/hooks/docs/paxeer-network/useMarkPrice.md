# useMarkPrice

## Overview

Subscribes to the mark price WebSocket topic for a symbol and returns the latest mark price.

## Exports

### `useMarkPrice(symbol: string)`

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | `string` | Yes | Trading symbol (e.g. `BTC_PERP`) |

#### Returns

`{ data: number }` — current mark price (0 until first message).

#### Usage example

```ts
import { useMarkPrice } from "@paxeer-network/hooks";

const { data: markPrice } = useMarkPrice("BTC_PERP");
```
