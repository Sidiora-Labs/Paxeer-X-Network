# dateTime

## Overview

Time utilities: convert milliseconds to [h, m, s], format timestamp as `yyyy-mm-dd hh:MM:ss`, and subtract days from a date.

## Exports

### timeConvertString

Converts a duration in milliseconds to hours, minutes, and seconds.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| time | number | Yes | Duration in milliseconds |

**Returns:** `[number, number, number]` — `[hours, minutes, seconds]` (each integer).

---

### timestampToString

Formats a timestamp as `yyyy-mm-dd hh:MM:ss`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| timestamp | number | Yes | Unix timestamp in ms |

**Returns:** `string` — e.g. `"2025-02-25 12:30:45"`.

---

### subtractDaysFromCurrentDate

Subtracts a number of days from a given date (or current date if not provided).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| days | number | Yes | Number of days to subtract |
| startDate | Date | No | Base date (default: new Date()) |

**Returns:** `Date` — new date instance.

## Usage example

```typescript
import {
  timeConvertString,
  timestampToString,
  subtractDaysFromCurrentDate,
} from "@paxeer-network/utils";

timeConvertString(3661000);           // [1, 1, 1]
timestampToString(Date.now());        // "2025-02-25 12:30:45"
subtractDaysFromCurrentDate(7);       // Date 7 days ago
subtractDaysFromCurrentDate(1, new Date("2025-02-25"));
```
