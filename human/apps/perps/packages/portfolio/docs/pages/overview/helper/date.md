# date.ts (overview helper)

## Overview

Date range helper for overview filters: normalizes `from`/`to` so same-day ranges get end at 23:00.

## Exports

- **`parseDateRangeForFilter(dateRange)`**  
  `dateRange: { from: Date; to?: Date }`. Returns `[from, to]`; if `from` and `to` are the same day, `to` is set to end of day (23:00). Uses `date-fns` (differenceInDays, setHours).

## Usage example

```ts
const [start, end] = parseDateRangeForFilter({ from: new Date(), to: new Date() });
```
