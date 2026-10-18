# sharePnL/index

## Overview

Re-exports the Share PnL script hook and the two widgets used by the package entry for dialog and bottom sheet registration.

## Exports

| Export | Description |
|--------|-------------|
| `useSharePnLScript` | Hook that builds entity, base/quote decimals, referral info, share options, and hide callback from `pnl` and `hide` props. |
| `SharePnLBottomSheetWidget` | Widget that uses `useSharePnLScript` and renders `MobileSharePnL`. |
| `SharePnLDialogWidget` | Widget that uses `useSharePnLScript` and renders `DesktopSharePnL`. |

## Usage example

```typescript
import {
  useSharePnLScript,
  SharePnLDialogWidget,
  SharePnLBottomSheetWidget,
} from "@paxeer-network/ui-share";
```
