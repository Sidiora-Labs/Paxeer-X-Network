# rwaDotTooltip

## Overview

Tooltip that shows a colored dot and RWA trading hours message. Uses `isCurrentlyTrading(record.rwaNextClose, record.rwaStatus)` to show "market hours" (success) or "outside market hours" (danger). Renders nothing when `record.isRwa` is false.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `record` | object | Yes | Must include `isRwa`, `rwaNextClose`, `rwaStatus` |

## Usage example

```tsx
import { RwaDotTooltip } from "@paxeer-network/markets";

<RwaDotTooltip record={row} />
```
