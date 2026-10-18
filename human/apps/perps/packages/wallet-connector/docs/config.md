# config.ts

## Overview

EVM (Web3 Onboard) initialization configuration and Solana chain ID constants. Defines default wallets (injected + Binance), app metadata, and a map from Solana network to numeric chain IDs.

## Exports

### `initConfig(apiKey?, options?)`

Creates and returns the Onboard API instance for EVM.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `apiKey` | `string` | No | Block Native / Onboard API key. |
| `options` | `InitOptions` | No | Merged with defaults (connect, wallets, chains, appMetadata, accountCenter, theme). |

**Returns:** `OnboardAPI`

**Default behavior:**

- `connect.autoConnectLastWallet: true`
- `wallets`: injected module + Binance connector
- `chains`: `[]` (often filled by `initEvm` from API)
- `appMetadata`: name "paxeer-network", recommended wallets (Coinbase, MetaMask, etc.), agreement URLs
- `accountCenter`: desktop and mobile disabled
- `theme`: `"dark"`

---

### `SolanaChainIdEnum`

Numeric chain IDs used for Solana in the unified connector.

| Member | Value |
|--------|--------|
| `MAINNET` | `900900900` |
| `DEVNET` | `901901901` |

---

### `SolanaChains`

`Map<WalletAdapterNetwork, number>` from Solana network to chain ID.

| Key | Value |
|-----|--------|
| `WalletAdapterNetwork.Devnet` | `901901901` |
| `WalletAdapterNetwork.Mainnet` | `900900900` |

## Usage example

```ts
import { initConfig, SolanaChains, SolanaChainIdEnum } from "./config";

const api = initConfig("api-key", { theme: "light" });

const devnetId = SolanaChains.get(WalletAdapterNetwork.Devnet); // 901901901
const isSolana = Array.from(SolanaChains.values()).includes(chainId);
```
