# utils

## Directory Responsibility

Utility functions, shared types, SWR key generator, chart helpers, and decimal formatting for the affiliate package. Does not contain React components or hooks.

## Files

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| types.ts | TypeScript | DateRange, SummaryFilter, BarDayFilter, IconProps | DateRange, SummaryFilter, BarDayFilter, IconProps | [types.md](types.md) |
| utils.ts | TypeScript | Referral link, copy, date parse/format, generateData | generateReferralLink, copyText, parseTime, formatDateTimeToUTC, formatYMDTime, formatHMTime, formatMdTime, compareDate, generateData | [utils.md](utils.md) |
| decimal.ts | TypeScript | Dollar-formatted commify | refCommify | [decimal.md](decimal.md) |
| swr.ts | TypeScript | Pagination key generator for SWR | generateKeyFun | [swr.md](swr.md) |
| chartUtils.ts | TypeScript | Fill volume chart data by date | fillData | [chartUtils.md](chartUtils.md) |
| mockData.ts | TypeScript | Mock referral info for development | MockData | [mockData.md](mockData.md) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| generateReferralLink | utils.ts | Build referral URL with `?ref=code` |
| refCommify | decimal.ts | Format number as dollar string with commify |
| generateKeyFun | swr.ts | Build paginated API key for SWR |
| fillData | chartUtils.ts | Fill missing dates in volume chart data |
