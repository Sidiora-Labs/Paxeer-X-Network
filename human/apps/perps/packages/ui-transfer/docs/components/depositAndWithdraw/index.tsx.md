# index.tsx (depositAndWithdraw)

> Location: `packages/ui-transfer/src/components/depositAndWithdraw/index.tsx`

## Overview

Main export: `DepositAndWithdraw` — tabbed deposit/withdraw UI. Exports dialog and sheet IDs and registers the component with `registerSimpleDialog` and `registerSimpleSheet`.

## Exports

- **DepositAndWithdrawWithDialogId** — string ID for dialog
- **DepositAndWithdrawWithSheetId** — string ID for sheet
- **DepositAndWithdraw** — FC component
- **DepositAndWithdrawProps** — type

## Props

| Prop | Type | Required | Default | Description |
| ---- | ---- | -------- | ------- | ----------- |
| activeTab | "deposit" \| "withdraw" | No | "deposit" | Initial tab |
| close | () => void | No | — | Close callback |

## Usage example

```tsx
import { DepositAndWithdraw, DepositAndWithdrawWithDialogId } from "@paxeer-network/ui-transfer";
<DepositAndWithdraw activeTab="withdraw" close={() => {}} />
```
