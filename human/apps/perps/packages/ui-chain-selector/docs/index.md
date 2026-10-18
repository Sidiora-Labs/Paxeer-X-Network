# ui-chain-selector (src)

Overview of the `@paxeer-network/ui-chain-selector` package source. This module provides chain selector UI components, types, and scripts for switching networks (mainnet/testnet) and registering dialog/sheet presets.

## Directory layout

All source files live under `src/` (no subdirectories).

## Files

| File | Language | Description |
|------|----------|-------------|
| [version.ts](./version.md) | TypeScript | Package version and global `__PAXEER_NETWORK_VERSION__` registration. |
| [type.ts](./type.md) | TypeScript | Chain item type and `ChainType` enum. |
| [index.ts](#package-entry) | TypeScript | Package entry: re-exports widget, dialog/sheet IDs. |
| [chainSelector.script.ts](./chainSelector.script.md) | TypeScript | Hook `useChainSelectorScript` and chain/tab/recent logic. |
| [chainSelector.ui.tsx](./chainSelector.ui.tsx.md) | React/TSX | Presentational components: `ChainSelector`, `ChainItem`, `RecentChainItem`. |
| [chainSelector.widget.tsx](./chainSelector.widget.tsx.md) | React/TSX | `ChainSelectorWidget` and dialog/sheet registration. |

## Package entry

`index.ts` re-exports the public API:

- **ChainSelectorWidget** – Widget that wires script state to UI.
- **ChainSelectorDialogId** – ID for the simple dialog preset.
- **ChainSelectorSheetId** – ID for the simple sheet preset.

Usage: open the selector via `openSimpleDialog(ChainSelectorDialogId, ...)` or `openSimpleSheet(ChainSelectorSheetId, ...)` from `@paxeer-network/ui`.
