# utils/chartUtils.ts

## Responsibility of utils/chartUtils.ts

Fills volume chart data for a given number of days: creates one entry per day with date and volume (or 0), merges in existing data from an optional array. Used for affiliate/trader volume charts.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| fillData | function | Chart data | Returns VolChartDataItem[] for the last N days with opacity by volume |

## fillData Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| days | number | Yes | Number of days to generate |
| origin | VolChartDataItem[] \| undefined | No | Existing data to merge (by date) |

Returns: `VolChartDataItem[]` — each item has date (yyyy-MM-dd), volume, and opacity (1 if volume > 0 else 0).

## Dependencies

- date-fns (format, subDays)
- @paxeer-network/chart (VolChartDataItem)

## utils/chartUtils.ts Example

```typescript
import { fillData } from "./chartUtils";

const chartData = fillData(30, apiVolumes);
```
