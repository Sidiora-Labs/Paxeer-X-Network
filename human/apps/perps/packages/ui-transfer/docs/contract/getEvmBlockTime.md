# getEvmBlockTime.ts

> Location: `packages/ui-transfer/src/contract/getEvmBlockTime.ts`

## Overview

Computes average EVM block time from two blocks 25 apart (span average) via the chain's public RPC, with one retry on transient RPC errors.

## Exports

### getEvmBlockTime(chain: API.Chain): Promise<number>

| Parameter | Type      | Description                               |
| --------- | --------- | ----------------------------------------- |
| chain     | API.Chain | Chain with `network_infos.public_rpc_url` |

**Returns:** Average block time in seconds.
