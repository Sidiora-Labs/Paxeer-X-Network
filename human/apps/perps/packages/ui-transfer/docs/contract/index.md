# contract

> Location: `packages/ui-transfer/src/contract`

## Overview

Utilities for chain-specific block time, confirmation counts, and LayerZero endpoint IDs. Used to estimate transaction confirmation time and support cross-chain config.

## Files

| File                                                   | Language   | Description                                                  |
| ------------------------------------------------------ | ---------- | ------------------------------------------------------------ |
| [endpointId.ts](./endpointId.md)                       | TypeScript | Chain ID → LayerZero Endpoint ID mapping and `getEndpointId` |
| [getBlockTime.ts](./getBlockTime.md)                   | TypeScript | Get average block time for a chain (EVM or Solana)           |
| [getEvmBlockTime.ts](./getEvmBlockTime.md)             | TypeScript | EVM average block time from recent blocks                    |
| [getSolanaBlockTime.ts](./getSolanaBlockTime.md)       | TypeScript | Solana block time from performance samples                   |
| [getChainConfirmations.ts](./getChainConfirmations.md) | TypeScript | Deposit confirmations via paxeer-network Receive OApp ULN config    |
| [useTransactionTime.ts](./useTransactionTime.md)       | TypeScript | Hook: estimated transaction time (blockTime × confirmations) |
