# providers

## Overview

Wallet providers and their initializers: **Privy** (PrivyWalletProvider, initPrivyProvider), **Wagmi** (WagmiWalletProvider, initWagmiProvider), **Solana** (SolanaWalletProvider, initSolanaProvider), **Abstract** (AbstractWalletProvider, initAbstractProvider). Each provider exposes a context and hook for connect, disconnect, wallet state, and chain.

## Directories

| Directory | Description |
|-----------|-------------|
| [privy](./privy/index.md) | Privy auth and embedded wallets (EVM + Solana). |
| [wagmi](./wagmi/index.md) | Wagmi config and EVM connectors. |
| [solana](./solana/index.md) | Solana wallet adapter and store wiring. |
| [abstractWallet](./abstractWallet/index.md) | Abstract Global Wallet (AGW) client and login. |
