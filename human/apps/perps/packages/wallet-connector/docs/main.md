# main.tsx

## Overview

Core connector component that bridges Solana and EVM. Uses `useSOL` and `useEvm`, derives a single `namespace` (solana vs evm), and provides `WalletConnectorContext` with a unified `connect`, `disconnect`, `setChain`, `wallet`, `connectedChain`, and `namespace`.

## Component

### `Main`

**Props:** `React.PropsWithChildren<{ solanaNetwork: WalletAdapterNetwork }>`

- Renders `WalletConnectorContext.Provider` with a memoized value.
- Children are the app tree that consumes the context from `@paxeer-network/hooks`.

## Context value (WalletConnectorContextState)

| Field | Type | Description |
|-------|------|-------------|
| `connect` | `(options: any) => Promise<any>` | Connect by chain: Solana chain IDs use Solana adapter; otherwise EVM Onboard. |
| `disconnect` | `() => Promise<void>` | Disconnect current wallet (EVM or Solana based on `namespace`). |
| `connecting` | `boolean` | Whether a connection is in progress. |
| `wallet` | `any` | Current wallet (Solana or EVM shape). |
| `setChain` | `(chain: any) => void` | Switch chain; may trigger connect if switching namespace. |
| `connectedChain` | `any` | Current chain (id, namespace). |
| `namespace` | `ChainNamespace \| null` | `ChainNamespace.solana` or `ChainNamespace.evm`. |
| `chains` | `any[]` | Chain list (currently empty array). |
| `settingChain` | `boolean` | Whether chain switch is in progress. |

## Behavior

- **connect(options):** If `options.chainId` is in `SolanaChains`, connects Solana; otherwise connects EVM (with optional `autoSelect`).
- **disconnect:** Delegates to EVM or Solana based on `namespace`.
- **setChain(chain):** Resolves `chainId` (hex or number). If same EVM namespace, calls `evm.changeChain`; if different namespace, calls `connect({ chainId })`.
- **namespace sync:** When both Solana and EVM are connected, prefers Solana if the last connect was Solana, otherwise EVM; updates `namespace` and disconnects the other side.

## Usage example

```tsx
// Main is used internally by WalletConnectorProvider. Consumers use context:

import { useWalletConnector } from "@paxeer-network/hooks";

function MyComponent() {
  const { connect, disconnect, wallet, namespace, setChain } = useWalletConnector();
  // connect({ chainId: 901901901 }) for Solana devnet
  // setChain({ chainId: "0x1" }) for Ethereum mainnet
}
```
