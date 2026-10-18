# chains.ts

## Overview

Helpers to get EVM chain list from `@paxeer-network/types` `chainsInfoMap`, exposed as a uniform array shape (id, token, label, rpcUrl, blockExplorerUrl).

## Exports

### `NetworkInterface`

Interface describing a network entry in the UI or config.

| Property | Type | Description |
|----------|------|-------------|
| `name` | `string` | Network name. |
| `logo` | `string` | Logo URL or identifier. |
| `chainId` | `string` | Chain ID. |
| `label` | `string` | Display label. |
| `token` | `string` | Native token symbol. |
| `requestRpc` | `string` | RPC URL for requests. |
| `explorerUrls` | `string[]` | Block explorer URLs. |

---

### `getChainsArray()`

Builds an array of chain descriptors from `chainsInfoMap`.

**Returns:** `Array<{ id: string; token: string; label: string; rpcUrl: string; blockExplorerUrl: string }>`

Each item is derived from `chain.chainInfo`: `chainId`, `nativeCurrency.symbol`, `chainName`, first `rpcUrls[0]`, first `blockExplorerUrls[0]`.

## Usage example

```ts
import { getChainsArray, type NetworkInterface } from "./chains";

const chains = getChainsArray();
// e.g. [{ id, token, label, rpcUrl, blockExplorerUrl }, ...]
```
