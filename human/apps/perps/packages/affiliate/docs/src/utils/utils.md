# utils.ts

## Overview

Utility functions for referral links, clipboard copy, and date/time formatting (UTC, YMD, HM, Md) and comparison. Also provides `generateData` to build time-series data for charts.

## Exports

| Function | Description |
|----------|-------------|
| `generateReferralLink(referralLinkUrl, code)` | Returns URL with `?ref=code` |
| `copyText(content)` | Copies to clipboard and shows toast |
| `parseTime(time)` | Parses number or string to Date or null |
| `formatDateTimeToUTC(input)` | Returns `yyyy-MM-dd HH:mm:ss 'UTC'` |
| `formatYMDTime(time)` | Returns `yyyy-MM-dd` |
| `formatHMTime(time)` | Returns `hh:mm` |
| `formatMdTime(time)` | Returns `MM-dd` |
| `compareDate(d1, d2)` | Compares dates by yyyy-mm-dd |
| `generateData(itemCount, data, timeKey, valueKey)` | Builds `[string, number][]` for charts |

## Usage Example

```ts
import { generateReferralLink, formatYMDTime, copyText } from "./utils";
const link = generateReferralLink("https://paxeer.network/", "ABC");
await copyText(link);
const ymd = formatYMDTime(Date.now());
```
