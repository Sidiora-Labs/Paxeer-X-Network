# getSolanaBlockTime.ts

> Location: `packages/ui-transfer/src/contract/getSolanaBlockTime.ts`

## Overview

Computes Solana average block time from recent performance samples (60 samples). Mainnet routes through paxeer-network `/v1/solana-rpc-proxy` with paxeer-network key signing (requires an initialized Account with `accountId` and paxeer-network key); devnet connects directly to the chain public RPC. HTTP requests time out after 8s and do not retry on HTTP 429.

## Exports

### getSolanaBlockTime(chain): Promise\<number\>

| Parameter | Type      | Description                                              |
| --------- | --------- | -------------------------------------------------------- |
| chain     | API.Chain | Solana chain config (`public_rpc_url` is used on devnet) |

**Returns:** Average block time in seconds, or `0` if the account is not ready / the RPC request fails / no usable samples.
