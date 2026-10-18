# chartUtils.ts

## Overview

Fills volume chart data for a given number of days, merging with optional existing `VolChartDataItem[]` from `@paxeer-network/chart`. Ensures one entry per day with date, volume, and opacity.

## Exports

| Function | Description |
|----------|-------------|
| `fillData(days, origin?)` | Returns `VolChartDataItem[]` of length `days`, filled/merged with `origin` by date |

## Usage Example

```ts
import { fillData } from "./chartUtils";
const chartData = fillData(30, apiVolumeData);
```
