# provider.tsx

## Overview

Root React provider for the wallet connector. Composes `SolanaProvider`, `InitEvm`, and `Main` so a single provider can drive both Solana and EVM wallet state and expose it via `WalletConnectorContext` from `@paxeer-network/hooks`.

## Exports

### `WalletConnectorProviderProps`

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `solanaInitial` | `SolanaInitialProps` | No | Props passed to SolanaProvider (network, RPCs, wallets, onError). |
| `evmInitial` | `EvmInitialProps` | No | Props passed to InitEvm (apiKey, options, skipInit). |
| `children` | `ReactNode` | Yes | App tree. |

---

### `WalletConnectorProvider`

React component that:

1. Wraps children in `SolanaProvider` with `solanaInitial` (or `{}`).
2. Wraps that in `InitEvm` with `evmInitial` (or `{}`).
3. Renders `Main` with `solanaNetwork` from `solanaInitial?.network` (default `WalletAdapterNetwork.Devnet`) and passes `children`.

`Main` provides `WalletConnectorContext` (connect, disconnect, setChain, wallet, namespace, etc.).

## Usage example

```tsx
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";

<WalletConnectorProvider
  solanaInitial={{
    network: WalletAdapterNetwork.Devnet,
    devnetRpc: "https://api.devnet.solana.com",
    onError: (err, adapter) => console.error(err),
  }}
  evmInitial={{
    apiKey: "your-onboard-api-key",
    options: { theme: "dark" },
    skipInit: false,
  }}
>
  <App />
</WalletConnectorProvider>
```
