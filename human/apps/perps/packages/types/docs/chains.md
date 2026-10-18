# chains

## Overview

Chain configuration types and presets for EVM (and Solana) networks. Defines `Chain`, `ChainInfo`, `NativeCurrency`, a global `chainsInfoMap`, testnet/mainnet chain lists, and chain namespace enums.

## Exports

### Interfaces

#### Chain

| Property | Type | Description |
|----------|------|-------------|
| id | number | Chain ID |
| chainNameShort | string | Short name |
| chainLogo | string | Logo URL or key |
| chainInfo | ChainInfo | Standard chain info |
| minGasBalance | number | Min gas balance threshold |
| minCrossGasBalance | number | Min cross-chain gas |
| maxPrepayCrossGas | number | Max prepay cross gas |
| blockExplorerName | string | Explorer name |
| chainName | string | Full name |
| requestRpc | string | RPC URL for requests |

#### ChainInfo

| Property | Type | Description |
|----------|------|-------------|
| chainId | string | Hex chain ID |
| chainName | string | Chain name |
| nativeCurrency | NativeCurrency | Native token info |
| rpcUrls | string[] | RPC URLs |
| blockExplorerUrls | string[] | Explorer URLs |

#### NativeCurrency

| Property | Type | Description |
|----------|------|-------------|
| name | string | Currency name |
| symbol | string | Symbol (e.g. ETH) |
| decimals | number | Decimals |
| fix | number | Display precision |

### Chain presets (constants)

Exported chain objects conforming to `Chain`: Ethereum, Avalanche, Fuji, BNB, Fantom, Polygon, Arbitrum, Optimism, zkSyncEra, PolygonzkEVM, Linea, Base, Mantle, ArbitrumGoerli, ArbitrumSepolia, OptimismGoerli, OptimismSepolia, BaseSepolia, MantleSepolia, PolygonAmoy, Sei, StoryOdysseyTestnet, SolanaDevnet.

### Other exports

| Name | Type | Description |
|------|------|-------------|
| chainsInfoMap | Map<number, Chain> | Map from chain ID to Chain preset |
| TestnetChains | array | Testnet configs (Arbitrum Sepolia, Solana Devnet, Story, etc.) |
| defaultMainnetChains | Chain[] | [Arbitrum, Base, Optimism] |
| defaultTestnetChains | Chain[] | [ArbitrumSepolia] |
| ChainNamespace | enum | evm = "EVM", solana = "SOL" |
| AbstractChains | Set<number> | 2741, 11124 |
| SolanaChains | Set<number> | 901901901, 900900900 |
| StoryTestnet | object | Story Odyssey Testnet config (name, public_rpc_url, chain_id, currency_symbol, explorer_base_url, vault_address) |

## Usage example

```typescript
import {
  Chain as ChainConfig,
  ChainInfo,
  chainsInfoMap,
  ArbitrumSepolia,
  defaultMainnetChains,
} from "@paxeer-network/types";
const chain = chainsInfoMap.get(421614);
```
