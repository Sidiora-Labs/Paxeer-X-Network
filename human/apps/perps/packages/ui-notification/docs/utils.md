# utils

## Overview

Utility functions for announcement data: UTC time formatting, sorting by updated time, deduplication by ID, symbol extraction from messages, and filtering listing/delisting announcements by market list.

## Exports

### `getTimeString(timestamp: number): string`

Formats a UTC timestamp as a readable string: time (e.g. `h:mm aa`) and date (e.g. `MMM dd`).

| Parameter   | Type     | Description        |
|------------|----------|--------------------|
| `timestamp`| `number` | UTC timestamp (ms) |

**Returns**: `string` – e.g. `"2:30 PM (UTC) on Jan 15"`.

---

### `sortDataByUpdatedTime(list: API.AnnouncementRow[]): API.AnnouncementRow[]`

Sorts announcements by `updated_time` descending (newest first). Mutates and returns the same array.

| Parameter | Type                     | Description      |
|----------|--------------------------|------------------|
| `list`   | `API.AnnouncementRow[]`  | List to sort     |

---

### `filterDuplicateArrayById(list: API.AnnouncementRow[]): API.AnnouncementRow[]`

Removes duplicates by `announcement_id`, keeping the first occurrence of each ID.

| Parameter | Type                     | Description      |
|----------|--------------------------|------------------|
| `list`   | `API.AnnouncementRow[]`  | List to dedupe   |

**Returns**: New array without duplicate IDs.

---

### `extractSymbolsFromMessage(message: string): string[]`

Extracts ticker symbols from text in the form `$TICKER` (e.g. `$GOOGL`, `$TSLA`). Returns unique, uppercase symbols.

| Parameter | Type     | Description   |
|----------|----------|---------------|
| `message`| `string` | Raw message   |

**Returns**: `string[]` – e.g. `["GOOGL", "TSLA"]`.

---

### `isSymbolInMarketList(symbol: string, marketList: API.MarketInfoExt[]): boolean`

Returns whether any market’s `symbol` starts with `PERP_${symbol}_`.

| Parameter    | Type                    | Description     |
|-------------|-------------------------|-----------------|
| `symbol`    | `string`                | Ticker symbol   |
| `marketList`| `API.MarketInfoExt[]`   | Market list     |

---

### `shouldShowListingDelistingAnnouncement(item: API.AnnouncementRow, marketList: API.MarketInfoExt[]): boolean`

For non–listing/delisting types, returns `true`. For listing/delisting, returns `true` only if every `$TICKER` in the message exists in the market list (via `PERP_<TICKER>_*`). Used to hide listing/delisting announcements for symbols not in the current market list.

| Parameter    | Type                    | Description        |
|-------------|-------------------------|--------------------|
| `item`      | `API.AnnouncementRow`   | Announcement row   |
| `marketList`| `API.MarketInfoExt[]`   | Market list        |

## Usage example

```typescript
import {
  getTimeString,
  sortDataByUpdatedTime,
  filterDuplicateArrayById,
  extractSymbolsFromMessage,
  isSymbolInMarketList,
  shouldShowListingDelistingAnnouncement,
} from "./utils";

const timeStr = getTimeString(Date.now());
const sorted = sortDataByUpdatedTime([...rows]);
const unique = filterDuplicateArrayById(sorted);
const symbols = extractSymbolsFromMessage("New $GOOGL & $TSLA");
const show = shouldShowListingDelistingAnnouncement(row, marketList);
```
