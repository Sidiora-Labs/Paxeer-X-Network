# components

## Overview

Shared TPSL UI building blocks: order info, position type selector, TP/SL input row, quantity, PnL info, order price type, and close-to-liq price icon. Used by positionTPSL, tpslAdvanced, and related flows.

## Files

| File | Language | Description |
|------|----------|-------------|
| [closeLiqPriceIcon](./closeLiqPriceIcon.md) | TSX | `CloseToLiqPriceIcon` – warning icon + tooltip/alert for SL near liq price |
| orderInfo | TSX | Order summary (symbol, qty, price, leverage) |
| pnlInfo | TSX | PnL display for TP/SL |
| orderPriceType | TSX | Order price type selector |
| tpslQty | TSX | TPSL quantity input |
| tpslPostionType/ | TSX | `TPSLPositionTypeWidget` – Full/Partial selector (widget, UI, script) |
| tpslInputRow/ | TSX | `TPSLInputRowWidget`, `useTPSLInputRowScript`, `TPSLInputRowUI` – single TP or SL row |

## Public exports (from package)

- `TPSLPositionTypeWidget` (tpslPostionType)
- `CloseToLiqPriceIcon` (closeLiqPriceIcon)
- `TPSLInputRowWidget`, `useTPSLInputRowScript`, `TPSLInputRowUI` (tpslInputRow)
