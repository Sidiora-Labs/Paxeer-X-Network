# VaultCard (vaultCard.ui)

## Overview

Presentational vault card: status tag, title, description (with parseMarkdownLinks), TVL, all-time return (formatAllTimeReturn), LP deposits/earnings, account balance, deposit/withdraw buttons (AuthGuard), “View more” link. Props match the return type of `useVaultCardScript` (VaultCardScript).

## Exports

### formatAllTimeReturn(status, vaultAge, lifetimeApy): string

- Returns `"--"` for pre_launch or vault age &lt; 7 days; `">10000%"` when lifetimeApy &gt; 100; otherwise formatted percentage.

### VaultCard

- **Props**: VaultCardScript (title, vaultInfo, lpInfo, description, isEVMConnected, isSOLConnected, openDepositAndWithdraw, availableBalance, openVaultWebsite, icon, isButtonsDisabled).
- **Behavior**: Renders card layout with status, numbers, and actions.
