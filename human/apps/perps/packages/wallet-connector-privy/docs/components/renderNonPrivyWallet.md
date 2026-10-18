# RenderNonPrivyWallet

## Overview

**RenderNonPrivyWallet** is the “My Wallet” view when the user is connected via non-Privy connectors (Wagmi, Solana, Abstract). It shows **StorageChainNotCurrentWalletType** when applicable, a list of **WalletCard**s for each connected wallet type, and add-wallet rows (AddEvmWallet, AddSolanaWallet, AddAbstractWallet) for types that are supported but not yet connected.

## Exports

- **RenderNonPrivyWallet** – Function component; no props.

## Usage example

```tsx
// Used inside ConnectDrawer when connectorKey !== "privy"
{connectorKey !== "privy" && <RenderNonPrivyWallet />}
```
