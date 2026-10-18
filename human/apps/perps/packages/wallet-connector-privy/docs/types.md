# types

## Overview

Shared types, enums, and chain maps for the wallet-connector-privy package: network, wallet types, connect props, init configs for Privy/Wagmi/Solana/Abstract, and chain ID mappings.

## Exports

### Enums

| Name | Values | Description |
|------|--------|-------------|
| **Network** | `mainnet`, `testnet` | Network environment. |
| **WalletType** | `EVM`, `SOL`, `Abstract` | Wallet chain type. |
| **WalletConnectType** | `EVM`, `SOL`, `privy`, `Abstract` | Connector type used for connection. |
| **WalletChainTypeEnum** | `onlyEVM`, `onlySOL`, `EVM_SOL` | Supported chain combination. |

### Interfaces

#### SolanaInitialProps

Props for Solana wallet adapter setup.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| network | `WalletAdapterNetwork` | No | Devnet/Mainnet. |
| endPoint | `string` | No | RPC endpoint. |
| mainnetRpc | `string` | No | Mainnet RPC URL. |
| devnetRpc | `string` | No | Devnet RPC URL. |
| wallets | `Adapter[]` | No | Wallet adapters. |
| onError | `(error, adapter?) => void` | No | Error handler. |

#### ConnectProps

Parameters for initiating a wallet connection.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| walletType | `WalletConnectType` | Yes | EVM, SOL, privy, or Abstract. |
| extraType | `string` | No | Extra type (e.g. login method). |
| connector | `Connector` | No | Wagmi connector (for EVM). |
| walletAdapter | `WalletAdapter` | No | Solana adapter (for SOL). |

#### InitPrivy

Configuration for Privy provider.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| appid | `string` | Yes | Privy app ID. |
| config | `object` | No | appearance (partial), loginMethods. |

#### InitWagmi

Configuration for Wagmi.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| connectors | `CreateConnectorFn[]` | No | Custom connectors. |
| storage | `Storage` | No | Custom storage. |

#### InitSolana

Configuration for Solana wallet adapter.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| mainnetRpc | `string` | No | Mainnet RPC. |
| devnetRpc | `string` | No | Devnet RPC. |
| wallets | `Adapter[]` | Yes | Adapters. |
| onError | `(error, adapter?) => void` | Yes | Error handler. |

#### InitAbstract

Configuration for Abstract Global Wallet.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| queryClient | `QueryClient` | No | React Query client. |

#### ConnectorWalletType

Flags to enable/disable each connector.

| Field | Type | Description |
|-------|------|-------------|
| disableWagmi | `boolean` | No | Disable Wagmi (EVM). |
| disablePrivy | `boolean` | No | Disable Privy. |
| disableSolana | `boolean` | No | Disable Solana. |
| disableAGW | `boolean` | No | Disable Abstract. |

#### WalletChainTypeConfig

Which chain types are supported by current config.

| Field | Type | Description |
|-------|------|-------------|
| hasEvm | `boolean` | EVM chains supported. |
| hasSol | `boolean` | Solana supported. |
| hasAbstract | `boolean` | Abstract supported. |

#### IWalletState

Extends `WalletState` with optional chain info (namespace + id).

### Constants (Maps)

- **SolanaChains** – `Map<WalletAdapterNetwork, number>` (Solana chain IDs).
- **SolanaChainsMap** – `Map<Network \| WalletAdapterNetwork, number>`.
- **AbstractChainsMap** – `Map<Network, number>` (Abstract chain IDs).

## Usage example

```typescript
import {
  Network,
  WalletType,
  WalletConnectType,
  ConnectProps,
  InitPrivy,
  SolanaChainsMap,
} from "@paxeer-network/wallet-connector-privy";

const connectProps: ConnectProps = {
  walletType: WalletConnectType.PRIVY,
  extraType: "email",
};
const chainId = SolanaChainsMap.get(Network.mainnet);
```
