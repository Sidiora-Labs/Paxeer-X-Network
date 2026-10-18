# AbstractWalletProvider / useAbstractWallet

## Overview

**AbstractWalletProvider** uses **useLoginWithAbstract** and **useAbstractClient** / **useGlobalWalletSignerAccount** from `@abstract-foundation/agw-react`, and **useAccount** from Wagmi to build **IWalletState** (label "AGW", provider from connector, additionalInfo.AGWAddress). **useAbstractWallet** exposes connect (login), disconnect (logout), wallet, connectedChain (from AbstractChainsMap and network), and isConnected.

## Context value (AbstractWalletContextValue)

| Field | Type | Description |
|-------|------|-------------|
| connect | `() => void` | Start Abstract login. |
| isConnected | `boolean` | Client and connector ready. |
| disconnect | `() => void` | Logout. |
| wallet | `IWalletState \| null` | AGW wallet with address. |
| connectedChain | `ConnectedChain \| undefined` | Abstract chain id and namespace. |

## Usage example

```tsx
const { connect, disconnect, wallet, connectedChain, isConnected } =
  useAbstractWallet();
connect();
```
