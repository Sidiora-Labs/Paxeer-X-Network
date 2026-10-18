# VaultDepositAndWithdraw

## Overview

Combined deposit/withdraw UI: Tabs (deposit | withdraw) with TabPanel content; deposit tab renders VaultDepositWidget, withdraw tab renders VaultWithdrawWidget. Registered as a simple dialog (desktop) and simple sheet (mobile) via `registerSimpleDialog` / `registerSimpleSheet`. Shown by vault card “Deposit”/“Withdraw” via modal.show with vaultId and activeTab.

## Exports

- **VaultDepositAndWithdrawWithDialogId**: string — Dialog id for modal.show.
- **VaultDepositAndWithdrawWithSheetId**: string — Sheet id for modal.show.
- **VaultDepositAndWithdraw**: FC component.

## Props (VaultDepositAndWithdrawProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| activeTab | "deposit" \| "withdraw" | No | Initial tab (default "deposit") |
| close | () => void | No | Close callback |
| vaultId | string | Yes | Vault id for deposit/withdraw forms |

## Usage example

```tsx
modal.show(VaultDepositAndWithdrawWithDialogId, { activeTab: "withdraw", vaultId: "..." });
```
