# wagmi

## Overview

Wagmi integration for EVM: **InitWagmiProvider** creates the Wagmi config (chains, connectors, storage) and wraps the app with **WagmiProvider**. **WagmiWalletProvider** exposes **useWagmiWallet** for connectors, connect, disconnect, wallet, connectedChain, setChain, and isConnected.

## Files

| File | Description |
|------|-------------|
| [wagmiWalletProvider](./wagmiWalletProvider.md) | useWagmiWallet context. |
| [initWagmiProvider](./initWagmiProvider.md) | createConfig and WagmiProvider. |
| [index](./index.md) | Re-exports. |
