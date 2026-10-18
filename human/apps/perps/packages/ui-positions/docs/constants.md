# constants

## Overview

Shared constants for the positions UI: sort storage key and liquidation price display threshold.

## Exports

| Name | Type | Description |
| ---- | ----- | ----------- |
| `TRADING_POSITIONS_SORT_STORAGE_KEY` | `string` | Session storage key for positions and position history sort preferences (`"paxeer-network_trading_positions_sort"`). |
| `LIQ_DISTANCE_THRESHOLD` | `number` | Threshold (e.g. 10) used to hide or show liquidation price; when relative distance from mark price exceeds this, UI may show `"--"`. |

## Usage example

```typescript
import {
  TRADING_POSITIONS_SORT_STORAGE_KEY,
  LIQ_DISTANCE_THRESHOLD,
} from "./constants";
```
