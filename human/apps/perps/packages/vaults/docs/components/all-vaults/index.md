# all-vaults

## Overview

All vaults list UI: grid or list view, sortable columns (TVL, APY, deposits, PnL), and per-row deposit/withdraw. Uses store vault list and vault-card script for LP data and actions.

## Files

| File | Language | Description |
|------|----------|-------------|
| [all-vaults.widget](./all-vaults-widget.md) | TSX | AllVaultsWidget: picks desktop/mobile by useScreen, uses useVaultInfoState |
| [all-vaults.desktop](./all-vaults-desktop.md) | TSX | Desktop layout with view toggle and VaultsList |
| [all-vaults.mobile](./all-vaults-mobile.md) | TSX | Mobile layout |
| [vaults-list](./vaults-list.md) | TSX | VaultsList: sortable table, VaultListRow with deposit/withdraw buttons |
| [view-mode-toggle](./view-mode-toggle.md) | TSX | ViewModeToggle: grid/list toggle button |
| [index](./all-vaults-index.md) | TypeScript | Re-exports widget, desktop, view-mode-toggle, vaults-list |
