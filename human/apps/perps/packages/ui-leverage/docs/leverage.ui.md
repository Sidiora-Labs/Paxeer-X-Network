# leverage.ui.tsx

> Location: `packages/ui-leverage/src/leverage.ui.tsx`

## Overview

Presentational React components for the account-level leverage editor: header, input with +/- buttons, preset toggles, slider with marks, and footer with Cancel/Save. Uses `LeverageScriptReturns` for props (from `leverage.script`).

## Exports

### Leverage (main container)

**Leverage** — Renders the full leverage form: header, input, selector, slider, footer.

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| (all from `LeverageScriptReturns`) | — | — | See [leverage.script.md](./leverage.script.md) |

### LeverageInput

Input row with reduce/increase icons and a numeric input (integer, 0 dp). Extends `LeverageProps` with optional `classNames.input`, `classNames.unit`, and `onInputBlur`.

### LeverageHeader

Displays current leverage with label "Current: {value}x". Props: `Pick<LeverageProps, "currentLeverage">`.

### LeverageSelector

Quick-select toggles for preset leverage values (e.g. 5, 10, 20, 50, 100). Props:

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `value` | number | Yes | Current selected leverage |
| `onLeverageChange` | (value: number) => void | Yes | Callback when an option is clicked |
| `toggles` | number[] | Yes | Array of preset values to show |

### LeverageSlider

Slider with step 1, marks, and tip showing "Nx". Props:

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `maxLeverage` | number | No | 0 | Max leverage for slider |
| `value` | number | Yes | — | Current value |
| `onLeverageChange` | (value: number) => void | Yes | — | Called on drag |
| `setShowSliderTip` | (value: boolean) => void | Yes | — | Show/hide tooltip |
| `showSliderTip` | boolean | Yes | — | Whether tip is visible |
| `className` | string | No | — | Root class |
| `onValueCommit` | (value: number[]) => void | No | — | Called when drag ends |
| `leverageLevers` | number[] | Yes | — | Marks on the track |
| `marks` | { label: string; value: number }[] | No | — | Slider marks |

### LeverageFooter

Cancel and Save buttons. Uses `onCancel`, `onSave`, `isLoading`, `disabled`; optional `isMobile` for button size.

## Types

- **LeverageProps** — Same as `LeverageScriptReturns` (see leverage.script).
- **LeverageHeaderProps** — `Pick<LeverageProps, "currentLeverage">`.
- **LeverageSliderProps** — See table above.

## Usage example

```tsx
import { Leverage, useLeverageScript } from "@paxeer-network/ui-leverage";

function LeverageForm() {
  const state = useLeverageScript();
  return <Leverage {...state} />;
}
```
