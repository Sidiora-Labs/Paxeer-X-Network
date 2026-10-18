# MobileDisplayControl

## Overview

Mobile variant of display control: dropdown trigger (DisplaySettingIcon + CaretIcon) and a full-width grid of options (position, limitOrders, stopOrders, tpsl, positionTpsl, buySell, trailingStop). Each cell toggles the corresponding key in `displayControlState` on click. Uses `IProps` and `DisplayControl` from `./common`.

## Props (IProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `displayControlState` | `DisplayControlSettingInterface` | Yes | Current toggles |
| `changeDisplayControlState` | `(state: DisplayControlSettingInterface) => void` | Yes | Update toggles |

## Usage example

```tsx
<MobileDisplayControl displayControlState={state} changeDisplayControlState={setState} />
```
