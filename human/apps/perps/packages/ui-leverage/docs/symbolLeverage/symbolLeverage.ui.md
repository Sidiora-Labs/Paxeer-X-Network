# symbolLeverage.ui.tsx

> Location: `packages/ui-leverage/src/symbolLeverage/symbolLeverage.ui.tsx`

## Overview

React component for editing leverage per symbol. Shows symbol icon and name, long/short badge, current leverage, then reuses account leverage UI pieces (`LeverageHeader`, `LeverageInput`, `LeverageSelector`, `LeverageSlider`, `LeverageFooter`) plus symbol-specific tips and warnings (max available leverage, actual position leverage, over required margin, over max position leverage).

## Exports

### SymbolLeverage

Single component that accepts **SymbolLeverageScriptReturns** (from `useSymbolLeverageScript`). Renders:

1. Header row: TokenIcon, symbol name (formatted), Long/Short badge, leverage badge (Cross Nx).
2. Divider.
3. Leverage form: LeverageHeader, LeverageInput, LeverageSelector, LeverageSlider.
4. Tips (max available leverage, actual position leverage).
5. Warnings (over required margin, over max position leverage) when applicable.
6. LeverageFooter (Cancel / Save).

### Internal

- **LeverageBadge** — Displays "Cross" and leverage value (e.g. "5x").

## Props (SymbolLeverageScriptReturns)

All props come from `useSymbolLeverageScript`: symbol, currentLeverage, value, onLeverageChange, onSave, onCancel, disabled, isLoading, showSliderTip, setShowSliderTip, maxLeverage, toggles, leverageLevers, marks, plus symbol-specific: maxPositionNotional, maxPositionLeverage, overMaxPositionLeverage, overRequiredMargin, isBuy, isMobile.

## Usage example

```tsx
import { SymbolLeverageWidget } from "@paxeer-network/ui-leverage";

<SymbolLeverageWidget
  symbol="PERP_BTC_USDC"
  curLeverage={10}
  side={OrderSide.BUY}
  close={() => modal.close()}
/>
```
