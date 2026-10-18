# components

## Overview

UI components for the wallet connector: connect drawer, wallet cards, connector selection (Privy / EVM / Solana / Abstract), user center, drawer shell, icons, PWA dialog, and link-device.

## Files

| File | Language | Description |
|------|----------|-------------|
| [userCenter](./userCenter.md) | TSX | UserCenter and MwebUserCenter – connect button and address display. |
| [connectDrawer](./connectDrawer.md) | TSX | ConnectDrawer – sheet with connect vs “My Wallet” content. |
| [walletCard](./walletCard.md) | TSX | WalletCard – single wallet card with address, type, copy, disconnect/export. |
| [drawer](./drawer.md) | TSX | Drawer – portal-based slide-out panel. |
| [common](./common.md) | TSX | RenderPrivyTypeIcon, RenderWalletIcon, EVMChainPopover. |
| [icons](./icons.md) | TSX | MoreIcon, DisconnectIcon, CloseIcon, ArrowRightIcon, etc. |
| [switchNetworkTips](./switchNetworkTips.md) | TSX | StorageChainNotCurrentWalletType tip. |
| [renderPrivyWallet](./renderPrivyWallet.md) | TSX | RenderPrivyWallet – Privy “My Wallet” view and create EVM/SOL. |
| [renderNonPrivyWallet](./renderNonPrivyWallet.md) | TSX | RenderNonPrivyWallet – non-Privy wallet list and add-wallet entries. |
| [pwaDilaog](./pwaDilaog.md) | TSX | PwaDialog – PWA install prompt. |
| [linkDevice](./linkDevice.md) | TSX | LinkDeviceMobile – link device flow entry. |
| [renderConnector](./renderConnector/index.md) | TSX | Connector selection (Privy, EVM, Solana, Abstract). |
| [renderNonPrivyWallet/addEvmWallet](./renderNonPrivyWallet/addEvmWallet.md) | TSX | Add EVM wallet entry. |
| [renderNonPrivyWallet/addSolanaWallet](./renderNonPrivyWallet/addSolanaWallet.md) | TSX | Add Solana wallet entry. |
| [renderNonPrivyWallet/addAbstractWallet](./renderNonPrivyWallet/addAbstractWallet.md) | TSX | Add Abstract wallet entry. |
