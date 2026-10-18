# abstractWallet

## Overview

Abstract Global Wallet (AGW) integration: **InitAbstractProvider** wraps the app with **AbstractWalletProvider** from `@abstract-foundation/agw-react` with the chain (abstract or abstractTestnet) from network. **AbstractWalletProvider** (local) exposes **useAbstractWallet** for connect (login), disconnect (logout), wallet (IWalletState with AGW address), connectedChain, and isConnected.

## Files

| File | Description |
|------|-------------|
| [abstractWalletProvider](./abstractWalletProvider.md) | useAbstractWallet context. |
| [initAbstractProvider](./initAbstractProvider.md) | AGW provider with chain. |
| [index](./index.md) | Re-exports. |
