# RenderPrivyWallet

## Overview

**RenderPrivyWallet** is the “My Wallet” view when the user is connected via Privy. It shows linked account info, logout, optional Abstract warning, and a list of wallet cards (EVM/Solana) with create-EVM-wallet and create-Solana-wallet entries when the user has no wallet of that type.

## Exports

- **RenderPrivyWallet** – Function component; no props.

## Behavior

- Renders linked account (address + Privy type icon) and a “Logout” link.
- If storage chain is Abstract or target is Abstract, shows a warning that Privy does not support Abstract.
- Builds wallet list from Privy EVM/Solana wallets and “add” entries (CreateEVMWallet, CreateSOLWallet) when enabled by connector and chain type config.
- Each wallet is a **WalletCard** with isPrivy=true; switching active wallet calls `switchWallet`.

## Usage example

```tsx
// Used inside ConnectDrawer when connectorKey === "privy"
{connectorKey === "privy" && <RenderPrivyWallet />}
```
