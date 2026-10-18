# WagmiWalletProvider / useWagmiWallet

## Overview

**WagmiWalletProvider** uses Wagmi’s **useConnect**, **useDisconnect**, **useAccount**, and **useSwitchChain** and exposes a unified interface: connectors list, connect(connector), disconnect, wallet state, connectedChain (id + namespace), setChain(chainId), and isConnected. When Wagmi is disabled via context, it returns no-op functions and empty connectors.

## Context value (WagmiWalletContextValue)

| Field | Type | Description |
|-------|------|-------------|
| connectors | `Connector[]` | Available Wagmi connectors. |
| connect | `(args) => void` | Connect with connector. |
| wallet | `any` | Current account/chain as wallet state. |
| connectedChain | `{ id, namespace } \| null` | Current EVM chain. |
| setChain | `(chainId: number) => Promise<any>` | Switch chain. |
| disconnect | `() => void` | Disconnect. |
| isConnected | `boolean` | Whether connected. |

## Usage example

```tsx
const { connectors, connect, wallet, setChain, disconnect, isConnected } =
  useWagmiWallet();
connect({ connector: connectors[0] });
```
