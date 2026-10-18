# generalLeaderboard.script.ts

## Overview

Script hook and helpers for the general leaderboard: tab state (volume/pnl), date filter (with optional campaign weekly ranges), and search. Uses `DateRange`, `LeaderboardTab`, and utils from `../../../utils` and `../../../type`.

## Exports

### `useGeneralLeaderboardScript(options?)`

Main hook. Returns filter state, search state, `activeTab`, `onTabChange`, `useCampaignDateRange`, `weeklyRanges`, `currentOrAllTimeRange`.

#### Options (`GeneralLeaderboardScriptOptions`)

| Field | Type | Description |
|-------|------|-------------|
| `campaignDateRange` | `{ start_time; end_time }?` | If set, weekly ranges are computed and used for filter |

### `useCampaignWeeklyRanges(campaignDateRange)`

Returns `WeeklyDateRange[]` from `splitCampaignByWeeks(campaignDateRange)`.

### `useCurrentWeeklyRange(campaignDateRange)`

Returns `{ weeklyRanges, currentRange, currentOrAllTime, isInCurrentWeek, getCurrentRangeForDate }`.

### Constants / types

- `FilterDays`: `[7, 14, 30, 90]`
- `TFilterDays`: union of those numbers
- `GeneralLeaderboardScriptReturn`: return type of `useGeneralLeaderboardScript`
