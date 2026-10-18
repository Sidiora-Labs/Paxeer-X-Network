# ui-leverage (src)

Package entry and leverage UI: account-level and symbol-level leverage editors, dialogs/sheets, and scripts.

## Directory overview

| Directory | Description |
|-----------|-------------|
| [symbolLeverage](./symbolLeverage/index.md) | Symbol-specific leverage editor: widget, UI, and script with position/margin calculations |

## Top-level files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Package entry: exports `LeverageEditor`, `Leverage`, `useLeverageScript`, symbol leverage; registers dialog/sheet IDs | [entry.md](./entry.md) |
| `version.ts` | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration | [version.md](./version.md) |
| `leverage.ui.tsx` | React/TSX | Leverage UI: `Leverage`, `LeverageInput`, `LeverageSlider`, `LeverageHeader`, `LeverageSelector`, `LeverageFooter` | [leverage.ui.md](./leverage.ui.md) |
| `leverage.script.ts` | TypeScript | `useLeverageScript` hook for account-level leverage state and handlers | [leverage.script.md](./leverage.script.md) |
| `leverage.widget.tsx` | React/TSX | `LeverageEditor` widget composing leverage UI and script | [leverage.widget.md](./leverage.widget.md) |
