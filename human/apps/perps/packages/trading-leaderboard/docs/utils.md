# utils.ts

## Overview

Date and campaign range utilities: date ranges, formatting, and splitting campaign periods into weekly ranges. Uses `date-fns` for date logic.

## Exports

### Functions

| Name | Description |
|------|-------------|
| `getDateRange(offsetDay)` | Returns `{ from, to }` for the last `offsetDay` days from today. |
| `formatDateRange(date)` | Formats a date to `"yyyy-MM-dd"`. |
| `formatCampaignDate(date)` | Formats for campaign display (e.g. `"Jan 15, 2025 12:00"`) in UTC. |
| `formatUpdateDate(timestamp)` | Formats a timestamp to `"yyyy-MM-dd HH:mm"`. |
| `splitCampaignByWeeks(campaignDateRange)` | Splits `start_time`–`end_time` into weekly periods; returns array of `WeeklyDateRange`. |
| `getCurrentWeeklyRange(weeklyRanges, targetDate?)` | Returns the weekly range containing `targetDate` (default now), or null. |
| `getCurrentOrAllTimeRange(weeklyRanges, targetDate?)` | Current weekly range or fallback to "All time" / last range. |

### Types

| Name | Description |
|------|-------------|
| `WeeklyDateRange` | `{ from: Date; to: Date; label: string }` — e.g. `"Week 1"`. |

## Usage example

```typescript
import {
  getDateRange,
  formatDateRange,
  splitCampaignByWeeks,
  getCurrentOrAllTimeRange,
  type WeeklyDateRange,
} from "./utils";

const range = getDateRange(30);
const weeks = splitCampaignByWeeks({
  start_time: "2025-01-01",
  end_time: "2025-01-31",
});
const current = getCurrentOrAllTimeRange(weeks);
```
