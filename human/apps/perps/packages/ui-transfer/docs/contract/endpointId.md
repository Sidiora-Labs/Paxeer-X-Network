# endpointId.ts

> Location: `packages/ui-transfer/src/contract/endpointId.ts`

## Overview

LayerZero V2 Chain ID to Endpoint ID mapping. Used for cross-chain config (e.g. ULN confirmations). Mainnet IDs in 30xxx range, testnet in 40xxx.

## Exports

### CHAIN_ID_TO_ENDPOINT_ID

`Record<number | string, number>` — mapping from chain ID to LayerZero endpoint ID (EVM and Solana chains).

### getEndpointId(chainId: number | string): number | undefined

Returns the LayerZero Endpoint ID for the given chain ID, or `undefined` if not found.

## Usage example

```ts
import { CHAIN_ID_TO_ENDPOINT_ID, getEndpointId } from "@paxeer-network/ui-transfer";

const eid = getEndpointId(1);   // 30101 (Ethereum)
const eidSol = getEndpointId(101); // 30168 (Solana)
```
