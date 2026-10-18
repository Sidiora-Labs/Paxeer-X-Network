# useWallet

## Overview

**useWallet** is the main hook that aggregates wallet state from **useWagmiWallet**, **useSolanaWallet**, **usePrivyWallet**, and **useAbstractWallet**, and syncs it with **useStorageChain** and **ConnectorKey** in localStorage. It exposes a single `wallet`, `connectedChain`, `namespace`, `connect`, `setChain`, `switchWallet`, `disconnect`, and `onDisconnect` API.

## Returns

| Name | Type | Description |
|------|------|-------------|
| connect | `(params: ConnectProps) => void` | Connect by wallet type (EVM, SOL, PRIVY, ABSTRACT). |
| wallet | `WalletState \| null` | Current active wallet state. |
| connectedChain | `any` | Current chain (id, namespace). |
| setChain | `(chain: { chainId }) => Promise<boolean \| undefined>` | Switch to chain; may open connect drawer if wallet not connected. |
| namespace | `ChainNamespace \| null` | evm or solana. |
| switchWallet | `(walletType: WalletType) => void` | Switch active wallet (EVM/SOL/Abstract) and update storage chain. |
| disconnect | `(walletType: WalletConnectType) => Promise<void>` | Disconnect by connector type. |
| onDisconnect | `(params?: any) => Promise<any>` | Disconnect current wallet (used by WalletConnectorContext). |

## Usage example

```tsx
const { connect, wallet, setChain, switchWallet, onDisconnect } = useWallet();

connect({ walletType: WalletConnectType.PRIVY, extraType: "email" });
await setChain({ chainId: 421614 });
switchWallet(WalletType.SOL);
await onDisconnect();
```
