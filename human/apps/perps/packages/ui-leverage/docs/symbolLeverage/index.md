# symbolLeverage

Symbol-specific leverage editor: UI, script (with position/margin and max leverage calculations), and widget; registered sheet and dialog IDs.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Exports widget, props, sheet/dialog IDs; registers SymbolLeverage sheet and dialog | [entry.md](./entry.md) |
| `symbolLeverage.ui.tsx` | React/TSX | `SymbolLeverage` component: symbol header, leverage input/slider/selector, tips, warnings, footer | [symbolLeverage.ui.md](./symbolLeverage.ui.md) |
| `symbolLeverage.script.tsx` | TypeScript/React | `useSymbolLeverageScript` hook: symbol leverage state, position/margin checks, confirm modal on save | [symbolLeverage.script.md](./symbolLeverage.script.md) |
| `symbolLeverage.widget.tsx` | React/TSX | `SymbolLeverageWidget` composing script + UI | [symbolLeverage.widget.md](./symbolLeverage.widget.md) |

## Exports (from package)

- **SymbolLeverageWidget**, **SymbolLeverageWidgetProps**
- **SymbolLeverageSheetId**, **SymbolLeverageDialogId**
- Sheet/dialog registrations with title `leverage.adjustedLeverage`
