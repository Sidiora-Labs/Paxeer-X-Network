# type.ts (root)

## Overview

Shared types for the leaderboard feature: date range and leaderboard tab enum.

## Types

### `DateRange`

| Field | Type | Description |
|-------|------|-------------|
| `from` | `Date?` | Start of range |
| `to` | `Date?` | End of range |
| `label` | `string?` | Optional label (e.g. "Week 1") |

### `LeaderboardTab` (enum)

| Value | Description |
|-------|-------------|
| `Volume` | `"volume"` — sort by volume |
| `Pnl` | `"pnl"` — sort by PnL |

## Usage example

```typescript
import { DateRange, LeaderboardTab } from "./type";

const range: DateRange = { from: new Date(), to: new Date(), label: "Today" };
const tab = LeaderboardTab.Volume;
```
