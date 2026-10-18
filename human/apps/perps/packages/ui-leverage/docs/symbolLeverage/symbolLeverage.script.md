# symbolLeverage.script.tsx

> Location: `packages/ui-leverage/src/symbolLeverage/symbolLeverage.script.tsx`

## Overview

Hook for symbol-level leverage editing. Uses `useSymbolLeverage` for max leverage and update, and an internal `useCalc` to compute position, max position notional/leverage, and over-margin / over-max-leverage flags. On save, shows a confirmation modal then calls update; supports optional `close` callback.

## Exports

### useSymbolLeverageScript

| Signature | Description |
|-----------|-------------|
| `useSymbolLeverageScript(options: SymbolLeverageScriptOptions & UseLeverageScriptOptions)` | Returns symbol leverage state and handlers for UI |

### SymbolLeverageScriptOptions

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `symbol` | string | Yes | Symbol to edit (e.g. PERP_BTC_USDC) |
| `side` | OrderSide | No | Long/short; inferred from position if omitted |
| `curLeverage` | number | Yes | Current leverage for the symbol (display value) |

### UseLeverageScriptOptions

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `close` | () => void | No | Called after successful save |

### SymbolLeverageScriptReturns

Return type of `useSymbolLeverageScript`. Includes all fields needed by `SymbolLeverage` UI:

- **Shared with account script**: leverageLevers, currentLeverage, value, marks, onLeverageChange, onLeverageIncrease, onLeverageReduce, onInputChange, onInputBlur, isReduceDisabled, isIncreaseDisabled, disabled, step, onCancel, onSave, isLoading, showSliderTip, setShowSliderTip, maxLeverage, toggles.
- **Symbol-specific**: symbol, maxPositionNotional, maxPositionLeverage, overMaxPositionLeverage, overRequiredMargin, isBuy, isMobile.

`onSave` opens a confirm modal; on OK it calls update and then `close` + success toast.

## Internal helpers

- **generateLeverageLeversForSelector(max)** — Preset toggles (e.g. 10x → [1,3,5,8,10], 50x → [1,10,20,35,50]).
- **generateLeverageLevers(max)** — Slider marks (e.g. 10x → 1..10, 20x → [1,5,10,15,20], etc.).
- **useCalc({ symbol, leverage, maxLeverage })** — Uses account info, positions, mark prices, portfolio to compute: position, maxPositionLeverage, maxPositionNotional, overMaxPositionLeverage, overRequiredMargin (from free collateral with hypothetical leverage).

## Usage example

```tsx
import { useSymbolLeverageScript } from "@paxeer-network/ui-leverage";
import { SymbolLeverage } from "./symbolLeverage.ui";

const state = useSymbolLeverageScript({
  symbol: "PERP_BTC_USDC",
  curLeverage: 10,
  close: () => {},
});
return <SymbolLeverage {...state} />;
```
