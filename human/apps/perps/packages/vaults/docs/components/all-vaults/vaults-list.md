# VaultsList

## Overview

Sortable table of vaults: pool name, TVL, all-time return, my deposits, all-time PnL, account balance, and operate (deposit/withdraw). Each row uses `useVaultCardScript` for LP data and actions. Supports sort by TVL or APY; shows status tag and “View more” link.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| vaults | VaultInfo[] | Yes | List of vaults to display |

## Usage example

```tsx
<VaultsList vaults={vaultInfoData} />
```
