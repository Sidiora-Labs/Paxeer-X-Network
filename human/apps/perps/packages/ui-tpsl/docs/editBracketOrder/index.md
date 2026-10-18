# editBracketOrder

## Overview

Edit an existing bracket (algo) order: load TP/SL from child orders, edit via `useOrderEntry` and `useTpslPriceChecker`, submit via PUT `/v1/algo/order`. Registers `EditBracketOrderSheetId` and `EditBracketOrderDialogId`.

## Files

| File | Language | Description |
|------|----------|-------------|
| editBracketOrder.widget | TSX | `EditBracketOrderWidget`, sheet/dialog IDs, registration |
| editBracketOrder.ui | TSX | `EditBracketOrderUI` – form and actions |
| editBracketOrder.script | TSX | `useEditBracketOrder`, `useEditOrderMaxQty` – state and submit |

## Exports

- `EditBracketOrderWidget`, `EditBracketOrderSheetId`, `EditBracketOrderDialogId`

### EditBracketOrderWidget props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| order | `API.AlgoOrderExt` | Yes | Bracket order to edit |
| close | `() => void` | No | Close callback |

## Usage example

```tsx
import { EditBracketOrderWidget, EditBracketOrderDialogId } from "@paxeer-network/ui-tpsl";
import { modal } from "@paxeer-network/ui";

modal.show(EditBracketOrderDialogId, { order: bracketOrder, close: hide });
```
