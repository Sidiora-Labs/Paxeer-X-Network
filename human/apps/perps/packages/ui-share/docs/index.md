# @paxeer-network/ui-share

## Overview

This package provides share PnL (Profit and Loss) UI components and widgets for paxeer-network. It includes dialog and bottom sheet widgets for sharing trading position results, poster generation, and desktop/mobile layouts.

## Directory structure

| Directory | Description |
|-----------|--------------|
| [types](./types/index.md) | TypeScript types for share PnL (layout, config, entity, options). |
| [sharePnL](./sharePnL/index.md) | Share PnL feature: script hook, UI components, widgets, poster, carousel, desktop/mobile content. |

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| [version](./version.md) | TypeScript | Exposes package version and sets `window.__PAXEER_NETWORK_VERSION__['@paxeer-network/ui-share']`. |
| [entry](./entry.md) | TypeScript | Package entry: registers Share PnL dialog/sheet with `@paxeer-network/ui`, re-exports sharePnL and types. |

## Exports (from package entry)

- **IDs**: `SharePnLDialogId`, `SharePnLBottomSheetId`
- **Widgets**: `SharePnLDialogWidget`, `SharePnLBottomSheetWidget` (via `./sharePnL`)
- **Types**: `SharePnLConfig`, `SharePnLOptions`, `SharePnLParams`
