# PrivyWalletProvider / usePrivyWallet

## Overview

**PrivyWalletProvider** composes Privy’s auth with **useWallets** (EVM) and **useSolanaWallets** (Solana), maps them to **WalletState**-like objects with chain id/namespace, and exposes connect, disconnect, switchChain, linkedAccount, exportWallet, createEvmWallet, and createSolanaWallet via **usePrivyWallet**.

## Context value (PrivyWalletContextValue)

| Field | Type | Description |
|-------|------|-------------|
| connect | `() => void` | Trigger Privy login. |
| walletEVM | `WalletStatePrivy \| null` | Embedded EVM wallet state. |
| walletSOL | `WalletStatePrivy \| null` | Embedded Solana wallet state. |
| isConnected | `boolean` | User authenticated with Privy. |
| switchChain | `(chainId: number) => Promise<any>` | Switch embedded EVM chain. |
| linkedAccount | `{ type, address } \| null` | Linked account (email, google, etc.). |
| exportWallet | `(type?) => any` | Export embedded wallet. |
| createEvmWallet | `() => Promise<any>` | Create embedded EVM wallet. |
| createSolanaWallet | `() => Promise<any>` | Create embedded Solana wallet. |
| disconnect | `() => Promise<void>` | Logout Privy. |

## Usage example

```tsx
const {
  connect,
  walletEVM,
  walletSOL,
  isConnected,
  switchChain,
  linkedAccount,
  createEvmWallet,
  disconnect,
} = usePrivyWallet();
```
