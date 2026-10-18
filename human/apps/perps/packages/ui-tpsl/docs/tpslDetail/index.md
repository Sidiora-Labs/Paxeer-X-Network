# tpslDetail

## Overview

TPSL order detail view for a position: lists full-position and partial TPSL orders, supports edit (opens TPSL dialog/sheet) and cancel (single or all). Uses main-account or sub-account order stream and registers `TPSLDetailDialogId` and `TPSLDetailSheetId`.

## Files

| File | Language | Description |
|------|----------|-------------|
| tpslDetail.widget | TSX | `TPSLDetailWidget`, `TPSLDetailProps`, dialog/sheet IDs and registration |
| tsplDetail.ui | TSX | `TPSLDetailUI` – detail layout and order tables |
| tpslDetail.script | TSX | `useTPSLDetail` – order streams, cancel, edit/add TPSL dialog |
| tpslDetailProvider | TSX | Context provider for symbol and position |
| ordersTable / ordersTable.mobile | TSX | Order table (desktop / mobile) |
| components/ | TSX | type, qty, estPnl, common, triggerPrice, orderPrice |

## Exports

- `TPSLDetailWidget`, `TPSLDetailDialogId`, `TPSLDetailSheetId`, `TPSLDetailProps`

## Usage example

```tsx
import { TPSLDetailWidget, TPSLDetailDialogId } from "@paxeer-network/ui-tpsl";
import { modal } from "@paxeer-network/ui";

modal.show(TPSLDetailDialogId, {
  position,
  order: algoOrder,
  baseDP: 4,
  quoteDP: 2,
});
```
