# shareButton

## Overview

Share PnL button and widget: opens a share PnL modal with order info (symbol, PnL, side, open price, quantity, ref code, leverage). Renders nothing when `sharePnLConfig` is null.

## Files

| File | Description |
|------|-------------|
| [shareButton.ui](shareButton.ui.md) | `ShareButton` — button that calls `showModal` on click. |
| [shareButton.widget](shareButton.widget.md) | `ShareButtonWidget` — composes script and UI; props: order, sharePnLConfig, modalId, iconSize. |
| [shareButton.script](shareButton.script.md) | `useShareButtonScript` — builds modal payload (pnl entity, refCode, leverage) and returns `showModal`, `iconSize`, `sharePnLConfig`. |

## Exports (from index)

- `ShareButton`, `ShareButtonWidget`
