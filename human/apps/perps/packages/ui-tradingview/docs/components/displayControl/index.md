# displayControl

## Overview

Re-exports desktop and mobile display-control components used to toggle chart overlays (position, buy/sell, limit/stop orders, TP/SL, position TP/SL, trailing stop).

## Exports

| Export | Description |
|--------|-------------|
| `DesktopDisplayControl` | Dropdown with Switch per option |
| `MobileDisplayControl` | Dropdown with grid of toggles |

## Usage example

```tsx
import { DesktopDisplayControl, MobileDisplayControl } from "./displayControl";
<DesktopDisplayControl displayControlState={state} changeDisplayControlState={setState} />
```
