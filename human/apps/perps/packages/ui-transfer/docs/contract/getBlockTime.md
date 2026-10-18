# getBlockTime.ts

> Location: `packages/ui-transfer/src/contract/getBlockTime.ts`

## Overview

Returns the average block time for a chain. Dispatches to EVM or Solana implementation based on `isSolana(chainId)`.

## Exports

### getBlockTime(inputs): Promise\<number\>

| Parameter      | Type             | Description  |
| -------------- | ---------------- | ------------ |
| inputs.chainId | number \| string | Chain ID     |
| inputs.chain   | API.Chain        | Chain config |

**Returns:** Average block time in seconds (or 0 if unavailable).

## Usage example

```ts
import { getBlockTime } from "@paxeer-network/ui-transfer";

const time = await getBlockTime({ chainId: 1, chain });
```
