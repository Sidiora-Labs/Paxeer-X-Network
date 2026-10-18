# DesktopDisplayControl

## Overview

Desktop variant of display control: dropdown with a list of options and a `Switch` for each (position, buySell, limitOrders, stopOrders, tpsl, positionTpsl, trailingStop). Uses `IProps` and `DisplayControl` from `./common`.

## Props (IProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `displayControlState` | `DisplayControlSettingInterface` | Yes | Current toggles |
| `changeDisplayControlState` | `(state: DisplayControlSettingInterface) => void` | Yes | Update toggles |

## Usage example

```tsx
<DesktopDisplayControl displayControlState={state} changeDisplayControlState={setState} />
```
