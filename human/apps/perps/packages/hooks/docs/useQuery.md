# useQuery

## Overview

Public API query hook built on SWR. Uses `apiBaseUrl` from `useConfig`; throws if `PaxeerNetworkConfigProvider` is not present. Supports optional `formatter` and standard SWR options.

## Exports

### `useQuery<T>`

Generic hook for public (unauthenticated) API requests.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | `Parameters<SWRHook>[0]` | Yes | SWR key (e.g. URL string) |
| `options` | `useQueryOptions<T>` | No | `formatter` and SWR options |

#### Returns

`SWRResponse<T>` — standard SWR response (`data`, `error`, `mutate`, `isLoading`, etc.).

#### Usage example

```ts
import { useQuery } from "@paxeer-network/hooks";

const { data, error, mutate } = useQuery<API.MarketInfo>(
  "/v1/public/futures/BTC_PERP",
  { revalidateOnFocus: false }
);
```
