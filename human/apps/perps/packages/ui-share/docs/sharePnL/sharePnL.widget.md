# sharePnL.widget

## Overview

Two widgets used by the package entry: one for the Share PnL dialog (desktop) and one for the bottom sheet (mobile). Each uses `useSharePnLScript` and renders the corresponding UI component.

## Components

### SharePnLBottomSheetWidget

Renders mobile Share PnL inside a bottom sheet.

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| hide | () => void | No | Callback to close the sheet. |
| pnl | SharePnLOptions & SharePnLParams | No | Config and entity for Share PnL. |

### SharePnLDialogWidget

Renders desktop Share PnL inside a dialog.

#### Props

Same as `SharePnLBottomSheetWidget`: `hide`, `pnl`.

## Usage example

```tsx
// Typically used via openSimpleDialog / openSimpleSheet with SharePnLDialogId / SharePnLBottomSheetId
<SharePnLDialogWidget pnl={{ entity, backgroundImages: [...], ... }} hide={onClose} />;
<SharePnLBottomSheetWidget pnl={{ entity, ... }} hide={onClose} />;
```
