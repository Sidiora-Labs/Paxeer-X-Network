# hooks/useReferralHistory.ts

## Responsibility of useReferralHistory.ts

Fetches paginated referral history from `/v1/referral/referral_history` with optional size, startDate, endDate, page. Returns [rows, { total, isLoading, refresh, loadMore, meta }] as a tuple. Supports fetchAll (no pagination params) and enabled flag.

## useReferralHistory Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| params.size | number | No | Page size (default 10) |
| params.startDate | string | No | YYYY-MM-dd |
| params.endDate | string | No | YYYY-MM-dd |
| params.initialSize | number | No | Not inferable from code |
| params.page | number | No | Page number |
| params.fetchAll | boolean | No | Omit page/size in query |
| params.enabled | boolean | No | Default true; false disables request |

## Return Value

Tuple: [rows: ReferralHistoryItem[] | null, { total, isLoading, refresh, loadMore, meta }]. loadMore is a no-op in current code.

## ReferralHistoryItem Fields

account_id, user_address, referral_code, volume, referral_rebate, direct_bonus_rebate, date.

## Dependencies

- react (useMemo)
- @paxeer-network/hooks (usePrivateQuery)

## hooks/useReferralHistory.ts Example

```typescript
const [rows, { total, isLoading, refresh }] = useReferralHistory({
  size: 10,
  page: 1,
  startDate: "2025-01-01",
  endDate: "2025-01-31",
});
```
