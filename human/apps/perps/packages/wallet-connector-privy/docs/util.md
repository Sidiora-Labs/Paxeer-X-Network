# util

## Overview

Utility constants and helpers for wallet icons and chain-type detection. Uses paxeer-network OSS URLs for EVM wallet images and defines a chain-type resolver from chain ID.

## Exports

### Constants

| Name | Type | Description |
|------|------|-------------|
| **PaxeerNetworkOSS** | `string` | Base URL `https://oss.paxeer.network`. |
| **PrivyConnectorImagePath** | `string` | Path for Privy-related images. |
| **EVMWalletImage** | `string` | Path for EVM wallet images. |

### Functions

#### getWalletIcon(type: string): string | undefined

Returns the icon URL for a given wallet type (e.g. `metamask`, `walletconnect`). Uses a built-in map of known EVM wallet names to OSS URLs.

| Parameter | Type | Description |
|-----------|------|-------------|
| type | `string` | Wallet identifier (case-insensitive). |

#### getChainType(chainId: number): WalletType

Determines whether a chain ID belongs to Abstract, Solana, or EVM.

| Parameter | Type | Description |
|-----------|------|-------------|
| chainId | `number` | Chain ID. |

**Returns:** `WalletType.ABSTRACT`, `WalletType.SOL`, or `WalletType.EVM`.

## Usage example

```typescript
import { getWalletIcon, getChainType, PrivyConnectorImagePath } from "@paxeer-network/wallet-connector-privy";
import { WalletType } from "./types";

const icon = getWalletIcon("metamask");
const chainType = getChainType(421614); // e.g. Arbitrum Sepolia -> EVM
```
