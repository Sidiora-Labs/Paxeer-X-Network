# AddEvmWallet

## Overview

**AddEvmWallet** is an “Add EVM wallet” row used in **RenderNonPrivyWallet**. It shows the list of Wagmi connectors in a collapsible area and calls `connect({ walletType: WalletConnectType.EVM, connector })` when the user selects one. Auto-opens when `targetWalletType === WalletType.EVM` and auto-closes after a short delay.

## Exports

- **AddEvmWallet** – Function component; no props.
