# symbolLeverage.widget.tsx

> Location: `packages/ui-leverage/src/symbolLeverage/symbolLeverage.widget.tsx`

## Overview

Symbol-level leverage editor widget. Composes `useSymbolLeverageScript` and `SymbolLeverage` so one component can be used in sheet/dialog or inline.

## Exports

### SymbolLeverageWidget

| Name | Type | Description |
|------|------|-------------|
| `SymbolLeverageWidget` | `FC<SymbolLeverageWidgetProps>` | Widget component |
| `SymbolLeverageWidgetProps` | type | Props for the widget |

### SymbolLeverageWidgetProps

Extends **SymbolLeverageScriptOptions** and adds optional **close**:

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | string | Yes | Symbol to edit |
| `curLeverage` | number | Yes | Current leverage |
| `side` | OrderSide | No | Long/short |
| `close` | () => void | No | Called when sheet/dialog should close (e.g. after save) |

## Usage example

```tsx
import {
  SymbolLeverageWidget,
  SymbolLeverageDialogId,
} from "@paxeer-network/ui-leverage";
import { modal } from "@paxeer-network/ui";

modal.show(SymbolLeverageDialogId, {
  symbol: "PERP_BTC_USDC",
  curLeverage: 10,
});

// Or inline
<SymbolLeverageWidget
  symbol="PERP_ETH_USDC"
  curLeverage={5}
  close={() => setOpen(false)}
/>
```
