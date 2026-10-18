# @paxeer-network/wallet-connector

## Overview

This package provides a unified wallet connector for paxeer-network that supports both **EVM** (via Web3 Onboard) and **Solana** (via wallet-adapter). It exposes a single provider and context so apps can connect, disconnect, and switch chains across both ecosystems.

## Top-level files

| File | Language | Description |
|------|-----------|-------------|
| [entry](entry.md) | TypeScript | Package public API: re-exports `WalletConnectorProvider`. |
| [provider](provider.md) | React/TSX | Root `WalletConnectorProvider` component and props. |
| [main](main.md) | React/TSX | Main connector logic: bridges EVM/Solana and provides `WalletConnectorContext`. |
| [SolanaProvider](SolanaProvider.md) | React/TSX | Solana wallet-adapter provider and context. |
| [initEvm](initEvm.md) | React/TSX | EVM onboarding initialization (fetch chain info, init Web3 Onboard). |
| [useEvm](useEvm.md) | React/TSX | Hook wrapping Web3 Onboard `useConnectWallet` / `useSetChain` for EVM. |
| [useSOL](useSOL.md) | React/TSX | Hook for Solana wallet connect/disconnect and wallet state. |
| [types](types.md) | TypeScript | Shared types: `ConnectorInitOptions`, `SolanaInitialProps`, `EvmInitialProps`. |
| [config](config.md) | TypeScript | EVM init config, `initConfig`, and Solana chain ID enums/maps. |
| [chains](chains.md) | TypeScript | `NetworkInterface`, `getChainsArray()` from `@paxeer-network/types`. |
| [version](version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration. |

## Directory structure

All source files live under `packages/wallet-connector/src/` with no subdirectories. The docs mirror that: this index plus one detail document per file.

## Usage

Wrap your app with `WalletConnectorProvider` and pass optional `solanaInitial` and `evmInitial` props. Consume the connector state via `@paxeer-network/hooks` (e.g. `WalletConnectorContext`).

```tsx
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";

<WalletConnectorProvider
  solanaInitial={{ network: WalletAdapterNetwork.Devnet }}
  evmInitial={{ apiKey: "…", skipInit: false }}
>
  {children}
</WalletConnectorProvider>
```
