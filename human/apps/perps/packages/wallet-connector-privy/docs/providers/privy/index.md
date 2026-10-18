# privy

## Overview

Privy integration: **PrivyWalletProvider** wraps the app with Privy’s provider and exposes **usePrivyWallet** for connect, disconnect, embedded EVM/Solana wallets, switchChain, linkedAccount, exportWallet, createEvmWallet, and createSolanaWallet. **initPrivyProvider** (initPrivyProvider.tsx) is used to configure and mount the Privy provider with appId and config.

## Files

| File | Description |
|------|-------------|
| [privyWalletProvider](./privyWalletProvider.md) | Context and usePrivyWallet. |
| [initPrivyProvider](./initPrivyProvider.md) | Privy provider init and config. |
| [index](./index.md) | Re-exports. |
