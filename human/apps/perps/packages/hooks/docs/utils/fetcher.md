# fetcher.ts — Fetcher and useQueryOptions

## fetcher.ts Responsibility

Provides the default SWR fetcher and related types: `fetcher` (uses `get` from `@paxeer-network/net` with optional formatter), `useQueryOptions` type (SWR config + formatter), and `noCacheConfig` (dedupingInterval 0, revalidate on mount and if stale). Used by `useQuery` and other SWR-based hooks for public API requests.

## fetcher.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| fetcher | function | Fetcher | (url, init?, queryOptions?) => get(url, init, queryOptions?.formatter). |
| useQueryOptions | type | Config type | SWRConfiguration & { formatter?: (data: any) => T }. |
| noCacheConfig | object | SWR config | { dedupingInterval: 0, revalidateOnMount: true, revalidateIfStale: true }. |

## fetcher Function Signature and Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| url | string | Yes | Request URL. |
| init | RequestInit | No | Fetch init (default {}). |
| queryOptions | useQueryOptions<any> | No | SWR options; formatter transforms response. |

- **Output**: Promise from `get(url, init, queryOptions?.formatter)`.

## fetcher Dependencies

- **Upstream**: `get` from `@paxeer-network/net`, SWRConfiguration from `swr`.
- **Downstream**: useQuery and other hooks that pass this fetcher to SWR.

## fetcher Example

```ts
import { fetcher, noCacheConfig, useQueryOptions } from "@paxeer-network/hooks";

const options: useQueryOptions<{ list: Item[] }> = {
  ...noCacheConfig,
  formatter: (data) => ({ list: data?.data?.list ?? [] }),
};

const { data } = useSWR("/v1/public/stats", fetcher, options);
```
