# utils/utils.ts

## Responsibility of utils/utils.ts

Referral link generation, clipboard copy with toast, date parsing/formatting (UTC and local), date comparison, and time-series data generation for charts.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| generateReferralLink | function | URL | Builds `{base}?ref={code}` |
| copyText | function | Clipboard | Copies text and shows toast |
| parseTime | function | Date | Parses number or string to Date or null |
| formatDateTimeToUTC | function | Date | Returns `yyyy-MM-dd HH:mm:ss 'UTC'` |
| formatYMDTime | function | Date | Returns `yyyy-MM-dd` |
| formatHMTime | function | Date | Returns `hh:mm` |
| formatMdTime | function | Date | Returns `MM-dd` |
| compareDate | function | Date | Equality by yyyy-mm-dd |
| generateData | function | Chart | Fills `[date, value][]` for last N days from raw data |

## generateReferralLink Parameters and Return

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| referralLinkUrl | string | Yes | Base URL (e.g. origin) |
| code | string | Yes | Referral code |

Returns: `string` — full referral URL with `?ref=code`.

## copyText Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| content | string | Yes | Text to copy |

Returns: `Promise<void>`. Shows success/failed toast via i18n.

## parseTime / format* Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| time / input | number \| string \| undefined | Timestamp or date string |

## generateData Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| itemCount | number | Number of days to generate |
| data | any[] \| null \| undefined | Raw data array |
| timeKey | string | Property name for date in each item |
| valueKey | string | Property name for value in each item |

Returns: `[string, number][]` — date string and value per day, reverse chronological then reversed to chronological.

## Dependencies

- date-fns (format, toDate)
- @paxeer-network/i18n, @paxeer-network/ui (toast)

## utils/utils.ts Example

```typescript
import { generateReferralLink, copyText, formatYMDTime, generateData } from "./utils";

const url = generateReferralLink("https://app.example.com", "ABC123");
await copyText(url);

const labels = generateData(7, apiData, "date", "volume");
```
