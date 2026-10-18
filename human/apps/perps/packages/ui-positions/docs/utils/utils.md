# utils

## Overview

Date range and day-boundary helpers used for position history filters and date pickers.

## Exports

| Function | Description |
| -------- | ----------- |
| `parseDateRangeForFilter(dateRange)` | Normalizes `{ from, to? }`; if same day, sets `to` to end of day. Returns `[from, to]`. |
| `offsetStartOfDay(date?)` | Returns date with time set to 00:00:00.000, or same value if null/undefined. |
| `offsetEndOfDay(date?)` | Returns date with time set to 23:59:59.999, or same value if null/undefined. |
| `formatDatePickerRange(option)` | Maps `{ from?, to? }` to `{ from: offsetStartOfDay(from), to: offsetEndOfDay(to ?? from) }`. |
| `areDatesEqual(date1, date2)` | Returns true if both dates have the same timestamp. |

## Usage example

```typescript
import {
  parseDateRangeForFilter,
  offsetStartOfDay,
  formatDatePickerRange,
  areDatesEqual,
} from "./utils";
```
