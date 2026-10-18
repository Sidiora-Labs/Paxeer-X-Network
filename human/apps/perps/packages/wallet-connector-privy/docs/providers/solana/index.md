# solana

## Overview

Solana wallet adapter integration: **InitSolanaProvider** provides **ConnectionProvider** and **WalletProvider** with RPC and adapters, and syncs `solanaInfo` (rpcUrl, network) with context. **SolanaWalletProvider** wires **useWallet** from `@solana/wallet-adapter-react` with **useSolanaWalletStore** and exposes **useSolanaWallet** (wallets, connect, disconnect, wallet, connectedChain, isConnected).

## Files

| File | Description |
|------|-------------|
| [solanaWalletProvider](./solanaWalletProvider.md) | useSolanaWallet context. |
| [initSolanaProvider](./initSolanaProvider.md) | ConnectionProvider, WalletProvider, solanaInfo. |
| [index](./index.md) | Re-exports. |
