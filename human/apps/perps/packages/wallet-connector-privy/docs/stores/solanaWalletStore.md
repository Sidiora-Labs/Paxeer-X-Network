# solanaWalletStore

## Overview

Zustand store that holds the current Solana wallet (normalized to **Wallet** shape with provider, accounts, chain), connecting/error state, and **walletMethods** (select, connectSolana, signMessage, signTransaction, sendTransaction, disconnectSolana, etc.). **connect(walletName)** selects the adapter and builds the store’s wallet from the adapter and **solanaInfo**; **disconnect** clears it.

## State (useSolanaWalletStore.getState())

| Field | Type | Description |
|-------|------|-------------|
| wallet | `Wallet \| null` | Current Solana wallet. |
| isConnecting | `boolean` | Connecting in progress. |
| error | `Error \| null` | Last error. |
| isManual | `boolean` | Manual connect flag. |
| walletMethods | `WalletMethods \| null` | Injected by SolanaWalletProvider. |
| pendingWalletName | `string \| null` | Wallet name being connected. |

## Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| connect | `(walletName: string) => Promise<Wallet>` | Select adapter by name, connect, and set wallet. |
| disconnect | `() => Promise<void>` | Disconnect adapter and clear wallet. |
| setWallet | `(wallet \| null) => void` | Set wallet directly. |
| setError | `(error \| null) => void` | Set error. |
| setIsManual | `(isManual: boolean) => void` | Set manual flag. |
| setWalletMethods | `(methods: WalletMethods) => void` | Set methods from provider. |
| setPendingWalletName | `(name \| null) => void` | Set pending name. |

## Usage example

```tsx
const { wallet, connect, disconnect, setWalletMethods } = useSolanaWalletStore();
await connect("Phantom");
```
