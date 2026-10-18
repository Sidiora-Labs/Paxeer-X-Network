# request

## Overview

Generic HTTP client used by the vault API: configurable timeout, retry, request/response interceptors, and handling of JSON API responses with a `success` field. Exposes `RequestClient` class and a default instance used by `api.ts`.

## Exports

### Types

| Name | Description |
|------|-------------|
| `ApiResponse<T>` | `{ success: boolean; data: T; message?; code? }` |
| `QueryParams` | `Record<string, any>` for query string |
| `RequestInterceptor` | `(config) => config \| Promise<config)` |
| `ResponseInterceptor<T>` | `(response, data) => T \| Promise<T>` |
| `SimpleRequestConfig` | Extends RequestInit with `baseURL`, `params`, `timeout`, `retry`, `retryDelay`, `validateStatus`, `data` |
| `VaultsApiError` | Error with `code?`, `status?`, `response?` |

### Class

- **RequestClient**: Methods `request`, `get`, `post`, `put`, `delete`, `patch`; `addRequestInterceptor`, `addResponseInterceptor`. Default config: timeout 10s, retry 3, validateStatus 2xx.

### Default export

- Default instance of `RequestClient` (used as `requestClient` in api.ts).

## Usage example

```typescript
import requestClient from "./request";
const data = await requestClient.get("/v1/public/strategy_vault/vault/info", {
  baseURL: "https://api.paxeer.network",
  params: { status: "live" },
});
```
