# assets – directory index

## Directory responsibility

Assets page and convert flow: account-grouped holdings table (desktop/mobile), convert page for asset conversion. Exposes AssetsPage, AssetsWidget, AssetsDataTableWidget, AssetsTable, AssetsDataTable, AssetsTableMobile.

## Key entities

| Entity | File | Responsibility |
|--------|------|----------------|
| AssetsPage | page.tsx | Page wrapper |
| AssetsWidget / AssetsDataTableWidget | assetsPage/assets.widget.tsx | Widget and data table widget |
| AssetsTable / AssetsDataTable | assetsPage/assets.ui.desktop.tsx | Desktop table |
| AssetsTableMobile | assetsPage/assets.ui.mobile.tsx | Mobile table |
| type.ts | type.ts | ConvertedAssets, ConvertRecord, ConvertTransaction, etc. |
| convertPage | convertPage/* | Convert UI and script |

## Files

| File | Language | Link |
|------|----------|------|
| index.tsx | TSX | [index.md](index.md) |
| page.tsx | TSX | [page.md](page.md) |
| type.ts | TypeScript | [type.md](type.md) |
| assetsPage/assets.widget.tsx | TSX | [assetsPage/assets.widget.md](assetsPage/assets.widget.md) |
| assetsPage/assets.ui.desktop.tsx | TSX | [assetsPage/assets.ui.desktop.md](assetsPage/assets.ui.desktop.md) |
| assetsPage/assets.ui.mobile.tsx | TSX | [assetsPage/assets.ui.mobile.md](assetsPage/assets.ui.mobile.md) |
| assetsPage/assets.script.ts | TypeScript | [assetsPage/assets.script.md](assetsPage/assets.script.md) |
| assetsPage/assets.column.tsx | TSX | [assetsPage/assets.column.md](assetsPage/assets.column.md) |
| convertPage/convert.widget.tsx | TSX | [convertPage/convert.widget.md](convertPage/convert.widget.md) |
| convertPage/convert.ui.desktop.tsx | TSX | [convertPage/convert.ui.desktop.md](convertPage/convert.ui.desktop.md) |
| convertPage/convert.ui.mobile.tsx | TSX | [convertPage/convert.ui.mobile.md](convertPage/convert.ui.mobile.md) |
| convertPage/convert.script.ts | TypeScript | [convertPage/convert.script.md](convertPage/convert.script.md) |
| convertPage/convert.column.tsx | TSX | [convertPage/convert.column.md](convertPage/convert.column.md) |

## Subdirectories

- [assetsPage](assetsPage/index.md) – Assets table widget, UI, script, columns.
- [convertPage](convertPage/index.md) – Convert flow widget, UI, script, columns.
