# tpslSimpleDialog

## Overview

Simple TP-only or SL-only flow: dialog/sheet with a single trigger price (and quantity for partial). Registers `TPSLSimpleSheetId` and `TPSLSimpleDialogId`. Optional link to open the advanced TPSL dialog.

## Files

| File | Language | Description |
|------|----------|-------------|
| tpslSimpleDialog.widget | TSX | `TPSLSimpleDialogWidget`, sheet/dialog IDs, registration |
| tpslSimpleDialog.ui | TSX | `TPSLSimpleDialogUI` – form UI |
| tpslSimpleDialog.script | TSX | `useTPSLSimpleDialog` – builder state and confirm |

## Exports

- `TPSLSimpleDialogWidget`, `TPSLSimpleSheetId`, `TPSLSimpleDialogId`
- `TPSLSimpleDialogUI`, `useTPSLSimpleDialog`

### TPSLSimpleDialogWidget props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| type | `"tp" \| "sl"` | Yes | TP or SL only |
| triggerPrice | `number` | No | Pre-filled trigger price |
| symbol | `string` | Yes | Symbol |
| close | `() => void` | No | Close callback |
| onComplete | `() => void` | No | Success callback |
| showAdvancedTPSLDialog | `() => void` | No | Open advanced TPSL |

## Usage example

```tsx
import { TPSLSimpleDialogWidget, TPSLSimpleDialogId } from "@paxeer-network/ui-tpsl";
import { modal } from "@paxeer-network/ui";

modal.show(TPSLSimpleDialogId, {
  type: "tp",
  triggerPrice: 1.5,
  symbol: "PERP_BTC_USDC",
  onComplete: () => refresh(),
});
```
