# SolanaWalletProvider / useSolanaWallet

## Overview

**SolanaWalletProvider** bridges `@solana/wallet-adapter-react`’s **useWallet** with **useSolanaWalletStore**: it sets **walletMethods** (select, connect, signMessage, signTransaction, sendTransaction, disconnect, solanaInfo) on the store and syncs the adapter’s wallet/publicKey into the store’s normalized **Wallet** shape. **useSolanaWallet** returns wallets list, connect(walletName), disconnect, wallet, connectedChain, and isConnected.

## Context value (SolanaWalletContextValue)

| Field | Type | Description |
|-------|------|-------------|
| wallets | `any[]` | Wallet adapter list (with .adapter). |
| connect | `(walletName: string) => Promise<any>` | Select and connect adapter. |
| wallet | `any` | Current wallet (from store). |
| disconnect | `() => void` | Disconnect and clear store. |
| connectedChain | `{ id, namespace } \| null` | Solana chain id. |
| isConnected | `boolean` | Whether a wallet is connected. |

## Usage example

```tsx
const { wallets, connect, wallet, disconnect, isConnected } = useSolanaWallet();
await connect("Phantom");
```
