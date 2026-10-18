# utils/swr.ts

## Responsibility of utils/swr.ts

Provides a key generator for SWR infinite/paginated queries: builds query strings with size, page, start_date, end_date, and sort. Used by referral history and similar list APIs.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| generateKeyFun | function | Key factory | Returns a function (pageIndex, previousPageData) => key \| null |

## generateKeyFun Input (args)

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| path | string | Yes | API path (e.g. `/v1/referral/referral_history`) |
| startDate | string | No | YYYY-MM-dd |
| endDate | string | No | YYYY-MM-dd |
| size | number | No | Default 100 |
| page | number | No | Page number |
| sort | string | No | Sort parameter |

## Returned Function Behavior

- **Input**: (pageIndex, previousPageData).
- **Output**: `${path}?size=...&page=...` (and optional start_date, end_date, sort), or `null` when previousPageData has no rows (end of list).

## utils/swr.ts Example

```typescript
import { generateKeyFun } from "./swr";

const keyFn = generateKeyFun({
  path: "/v1/referral/referral_history",
  size: 10,
  startDate: "2025-01-01",
  endDate: "2025-01-31",
});
// keyFn(0, null) => "/v1/referral/referral_history?size=10&page=1"
// keyFn(1, { rows: [...] }) => "...&page=2"
```
