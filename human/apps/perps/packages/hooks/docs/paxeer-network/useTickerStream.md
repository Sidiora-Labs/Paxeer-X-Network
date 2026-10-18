# useTickerStream

## Overview

Subscribes to the ticker stream for a given symbol and merges live ticker data (24h open/high/low/close/volume, change) with static market info, mark price, index price, and open interest. Returns a single merged market info object or the static info when ticker is not yet available.

## Exports

### `useTickerStream(symbol: string)`

Returns merged market info for the symbol, including 24h stats and `change` / `24h_change` when ticker data is present.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | `string` | Yes | Trading symbol (e.g. `BTC_PERP`). Throws `SDKError` if empty. |

#### Returns

`API.MarketInfo & { change?: number; "24h_change"?: number }` — market info with optional 24h change fields, or `null` when base info is not loaded.

#### Usage example

```ts
import { useTickerStream } from "@paxeer-network/hooks";

const ticker = useTickerStream("BTC_PERP");
// ticker.mark_price, ticker["24h_close"], ticker.change, etc.
```
