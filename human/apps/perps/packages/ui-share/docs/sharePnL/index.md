# sharePnL

## Overview

Share PnL feature: hook for state, desktop/mobile UI components, dialog and bottom sheet widgets, poster generation, carousel for backgrounds, and utilities for poster data and local config.

## Files

| File | Language | Description |
|------|----------|--------------|
| [module](./module.md) | TypeScript | Re-exports `useSharePnLScript`, `SharePnLBottomSheetWidget`, `SharePnLDialogWidget`. |
| [sharePnL.ui](./sharePnL.ui.md) | TSX | `DesktopSharePnL` and `MobileSharePnL` components. |
| [sharePnL.script](./sharePnL.script.md) | TSX | `useSharePnLScript` hook and `SharePnLState` type. |
| [sharePnL.widget](./sharePnL.widget.md) | TSX | `SharePnLDialogWidget` and `SharePnLBottomSheetWidget`. |

## Subdirectories

| Directory | Description |
|-----------|--------------|
| [utils](./utils/index.md) | Poster data builder, PnL config save/load (localStorage). |
| [mobile](./mobile/index.md) | Mobile Share PnL content (carousel, options, share button). |
| [desktop](./desktop/index.md) | Desktop Share PnL content, format/options, message, carousel, bottom buttons. |
| [carousel](./carousel/index.md) | Carousel component and context for poster backgrounds. |
| [poster](./poster/index.md) | Poster canvas component and ref API. |
