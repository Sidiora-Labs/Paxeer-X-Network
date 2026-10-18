# leverage.script.ts

> Location: `packages/ui-leverage/src/leverage.script.ts`

## Overview

Hook that wires account-level leverage UI to `useLeverage` from `@paxeer-network/hooks`: current/max leverage, presets, slider state, and save/cancel handlers with toast feedback.

## Exports

### useLeverageScript

| Name | Type | Description |
|------|------|-------------|
| `useLeverageScript` | `(options?: UseLeverageScriptOptions) => LeverageScriptReturns` | Main hook |

### UseLeverageScriptOptions

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `close` | () => void | No | Called after successful save (e.g. close dialog/sheet) |

### LeverageScriptReturns

Return type of `useLeverageScript`. Used as props for `Leverage` and related UI.

| Property | Type | Description |
|----------|------|-------------|
| `leverageLevers` | number[] | Slider mark values from `useLeverage` |
| `currentLeverage` | number \| undefined | Current account leverage |
| `value` | number | Local editing value (input/slider) |
| `marks` | SliderMarks | Marks for slider (e.g. `[{ label: "5x", value: 5 }]`) |
| `onLeverageChange` | (leverage: number) => void | Set local value |
| `onLeverageIncrease` | MouseEventHandler | Increment by 1 |
| `onLeverageReduce` | MouseEventHandler | Decrement by 1 |
| `onInputChange` | ChangeEventHandler | Parse input and set value |
| `isReduceDisabled` | boolean | True when value <= 1 |
| `isIncreaseDisabled` | boolean | True when value >= maxLeverage |
| `disabled` | boolean | True when invalid or out of range |
| `step` | number | Derived from marks (for slider) |
| `onCancel` | () => void \| undefined | Same as options.close |
| `onSave` | () => Promise<void> | Calls `update({ leverage })`, then close + toast |
| `isLoading` | boolean | From useLeverage |
| `showSliderTip` | boolean | Slider tooltip visibility |
| `setShowSliderTip` | (value: boolean) => void | Set tooltip visibility |
| `maxLeverage` | number | Max allowed leverage |
| `toggles` | number[] | Presets (e.g. [5, 10, 20, 50, 100] filtered by max) |

## Usage example

```typescript
import { useLeverageScript } from "@paxeer-network/ui-leverage";

function MyForm() {
  const state = useLeverageScript({
    close: () => modal.close(),
  });
  return (
    <Leverage
      {...state}
      // state includes value, onLeverageChange, onSave, etc.
    />
  );
}
```
