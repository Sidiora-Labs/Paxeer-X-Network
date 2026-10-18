# index (package entry)

## Overview

Entry point for `@paxeer-network/ui-share`. Registers the Share PnL dialog and bottom sheet with `@paxeer-network/ui` and re-exports the share PnL module and public types.

## Registration

- **Share PnL Dialog**: `registerSimpleDialog(SharePnLDialogId, SharePnLDialogWidget, { classNames: { content: "!oui-max-w-[624px] oui-p-0" } })`
- **Share PnL Bottom Sheet**: `registerSimpleSheet(SharePnLBottomSheetId, SharePnLBottomSheetWidget, { title: i18n.t("share.pnl.sharePnl"), classNames: { body: "oui-pb-4 oui-pt-0" } })`

## Exports

| Export | Description |
|--------|-------------|
| `SharePnLDialogId` | Dialog ID for opening the desktop Share PnL dialog. |
| `SharePnLBottomSheetId` | Sheet ID for opening the mobile Share PnL bottom sheet. |
| `SharePnLConfig` | Full config type for Share PnL (options + params). |
| `SharePnLOptions` | Options (fonts, backgrounds, layout, colors). |
| `SharePnLParams` | Params (entity, refCode, refSlogan, refLink). |

All exports from `./sharePnL` (e.g. `useSharePnLScript`, `SharePnLDialogWidget`, `SharePnLBottomSheetWidget`) are re-exported.

## Usage example

```typescript
import {
  SharePnLDialogId,
  SharePnLBottomSheetId,
  openSimpleDialog,
  openSimpleSheet,
} from "@paxeer-network/ui";

// Desktop: open Share PnL dialog
openSimpleDialog(SharePnLDialogId, { pnl: { entity, ...options } });

// Mobile: open Share PnL bottom sheet
openSimpleSheet(SharePnLBottomSheetId, { pnl: { entity, ...options } });
```
