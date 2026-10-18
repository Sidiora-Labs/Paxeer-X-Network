# hooks/useMultiLevelReferees.ts

## Responsibility of useMultiLevelReferees.ts

Fetches paginated referee list from `/v1/referral/multi_level/referee_list` with optional page, pageSize, or fetchAll. Only runs when account status >= EnableTrading and enabled is true. Formatter adds network_size, combined volume, and commission to each row. Returns response plus rows and meta.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| RefereeDataType | type | Row shape | account_id, address, bind_code, volume, commission, direct_*, indirect_*, etc. |
| RefereePaginationMeta | type | Meta | total, current_page, records_per_page |
| UseMultiLevelRefereesParams | type | Params | enabled?, fetchAll?, page?, pageSize? |
| useMultiLevelReferees | function | Hook | Returns { ...response, isLoading, rows, meta } |
| UseMultiLevelRefereesReturns | type | Return type | ReturnType<typeof useMultiLevelReferees> |

## useMultiLevelReferees Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| params.enabled | boolean | No | Default true; false disables query |
| params.fetchAll | boolean | No | Omit pagination (used by mobile) |
| params.page | number | No | Page number |
| params.pageSize | number | No | Page size |

## Dependencies

- react (useMemo)
- @paxeer-network/hooks (useAccount, usePrivateQuery)
- @paxeer-network/types (AccountStatusEnum)

## hooks/useMultiLevelReferees.ts Example

```typescript
const { rows, meta, isLoading } = useMultiLevelReferees({ page: 1, pageSize: 10 });
```
