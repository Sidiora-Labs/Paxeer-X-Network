# symbolLeverage/index.ts

> Location: `packages/ui-leverage/src/symbolLeverage/index.ts`

## Overview

Entry for the symbol-leverage submodule. Exports the symbol leverage widget and its props, registers the sheet and dialog with shared UI, and defines the public sheet/dialog IDs.

## Exports

### Components & IDs

- **SymbolLeverageWidget**, **SymbolLeverageWidgetProps** — From `symbolLeverage.widget`.
- **SymbolLeverageSheetId** — `"SymbolLeverageSheetId"` (used for mobile sheet).
- **SymbolLeverageDialogId** — `"SymbolLeverageDialogId"` (used for desktop dialog).

### Side effects

- **registerSimpleSheet**(SymbolLeverageSheetId, SymbolLeverageWidget, { title: "leverage.adjustedLeverage" }).
- **registerSimpleDialog**(SymbolLeverageDialogId, SymbolLeverageWidget, { title: "leverage.adjustedLeverage", classNames: { content: "oui-w-[420px]" } }).

## Usage example

```tsx
import {
  SymbolLeverageWidget,
  SymbolLeverageSheetId,
  SymbolLeverageDialogId,
} from "@paxeer-network/ui-leverage";
import { modal } from "@paxeer-network/ui";

modal.show(SymbolLeverageDialogId, { symbol: "PERP_BTC_USDC", curLeverage: 10 });
```
