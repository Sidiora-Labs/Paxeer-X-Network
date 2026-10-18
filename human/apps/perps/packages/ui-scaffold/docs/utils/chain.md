# chain

> Location: `packages/ui-scaffold/src/utils/chain.ts`

## Overview

Provides a helper to check whether a given chain ID is supported by the current chain list (e.g. from `useChains()`).

## Exports

### `checkChainSupport(chainId, chains)`

Returns whether `chainId` exists in the provided list of chains.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| chainId | `number \| string` | Yes | Chain ID to look up (string is parsed to number). |
| chains | `API.Chain[]` | Yes | List of chains (from `@paxeer-network/types`). |

#### Returns

`boolean` — `true` if some chain has `network_infos.chain_id === chainId`.

## Usage example

```typescript
import { checkChainSupport } from "@paxeer-network/ui-scaffold/utils/chain";
import { API } from "@paxeer-network/types";

const chains: API.Chain[] = [/* from useChains() */];
const supported = checkChainSupport(421614, chains);
```
