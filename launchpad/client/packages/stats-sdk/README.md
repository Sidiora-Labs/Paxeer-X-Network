# stats-sdk

New local TypeScript implementation derived from `core/stats-workers/internal/httpapi/pool_stats.go` and `internal/store/store.go`, not a recovered third-party package. This package supplies the application's `httpClient.configure({ baseUrl })` import and typed `getPoolStats` and `getPoolStatsBatch` methods. Other statistics endpoints are not implemented here.

Configure the service host root before requesting data. Monetary and decimal fields remain strings. HTTP failures retain status and response body; malformed JSON or incompatible DTOs reject. Requests have a default ten-second timeout (configurable from 1 to 120000 milliseconds) and accept a caller AbortSignal. Batch results preserve the server's missing-pool behavior.

Runtime dependencies: none. Typecheck tool: TypeScript 5.9.3 (Apache-2.0, upstream microsoft/TypeScript). Build emits JavaScript and declarations into `dist`; package exports point to that compiled output. Tests use Node's built-in test runner against real loopback HTTP servers and the built SDK (Node 24.16.0 verified). Run build before test.
