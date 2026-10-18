# wallet-connector-privy

## Overview

This package provides a unified wallet connection layer that integrates **Privy**, **Wagmi** (EVM), **Solana** wallet adapters, and **Abstract Global Wallet (AGW)**. It exposes a single provider and hooks for connecting, switching chains, and managing multi-chain wallet state.

## Directory structure

| Directory | Description |
|-----------|-------------|
| [components](./components/index.md) | UI components: connect drawer, wallet cards, connector areas, user center |
| [hooks](./hooks/index.md) | `useWallet` and related hooks |
| [providers](./providers/index.md) | Privy, Wagmi, Solana, Abstract wallet providers and init |
| [stores](./stores/index.md) | Zustand store for Solana wallet state |

## Root-level files

| File | Language | Description |
|------|----------|-------------|
| [package-exports (index.ts)](./package-exports.md) | TypeScript | Main package exports |
| [provider](./provider.md) | TSX | `WalletConnectorPrivyProvider` and context |
| [types](./types.md) | TypeScript | Enums, interfaces, chain maps |
| [util](./util.md) | TypeScript | Wallet icons and chain-type helpers |
| [version](./version.md) | TypeScript | Package version and `__PAXEER_NETWORK_VERSION__` |
| [main](./main.md) | TSX | Main layout and `WalletConnectorContext` wiring |
| [injectUsercenter](./injectUsercenter.md) | TSX | UI extension registration for UserCenter |
