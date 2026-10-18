# types.ts

## Overview

Shared TypeScript types for the wallet connector: optional init options for Web3 Onboard and props for Solana and EVM initial configuration.

## Exports

### `ConnectorInitOptions`

Type alias for partial Web3 Onboard init options. Makes the following keys optional: `apiKey`, `connect`, `wallets`, `chains`, `appMetadata`, `accountCenter`, `theme`. Used when passing custom options into the EVM connector without repeating required fields.

| Base type | Optional keys |
|-----------|----------------|
| `InitOptions` (from `@web3-onboard/core`) | `apiKey`, `connect`, `wallets`, `chains`, `appMetadata`, `accountCenter`, `theme` |

---

### `SolanaInitialProps`

Props for initializing the Solana wallet-adapter layer. Extends `PropsWithChildren`.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `network` | `WalletAdapterNetwork` | No | Mainnet or Devnet. |
| `mainnetRpc` | `string` | No | RPC URL for mainnet. |
| `devnetRpc` | `string` | No | RPC URL for devnet. |
| `wallets` | `Adapter[]` | No | Custom wallet adapters. |
| `onError` | `(error: WalletError, adapter?: Adapter) => void` | No | Wallet error handler (e.g. not ready). |
| `children` | `ReactNode` | Yes | Child components. |

---

### `EvmInitialProps`

Props for initializing the EVM (Web3 Onboard) layer. Extends `PropsWithChildren`.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `apiKey` | `string` | No | API key for Onboard (if needed). |
| `options` | `ConnectorInitOptions` | No | Override Onboard init options. |
| `skipInit` | `boolean` | No | Skip board initialization if already done. |
| `children` | `ReactNode` | Yes | Child components. |

## Usage example

```ts
import type {
  ConnectorInitOptions,
  SolanaInitialProps,
  EvmInitialProps,
} from "@paxeer-network/wallet-connector";

const solanaProps: SolanaInitialProps = {
  network: WalletAdapterNetwork.Devnet,
  devnetRpc: "https://api.devnet.solana.com",
};

const evmProps: EvmInitialProps = {
  apiKey: "your-key",
  options: { theme: "light" },
  skipInit: false,
};
```
