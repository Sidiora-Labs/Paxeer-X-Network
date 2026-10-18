# AddSolanaWallet

## Overview

**AddSolanaWallet** is an “Add Solana wallet” row used in **RenderNonPrivyWallet**. It shows the list of Solana wallet adapters and calls `connect({ walletType: WalletConnectType.SOL, walletAdapter })` when the user selects one. Auto-opens when `targetWalletType === WalletType.SOL` and auto-closes after a short delay.

## Exports

- **AddSolanaWallet** – Function component; no props.
