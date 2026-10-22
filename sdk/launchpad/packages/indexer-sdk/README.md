# indexer-sdk

Configure `httpClient` with the stats-workers service base URL before calling
`getRecentTrades(poolAddress)`. An independently configured `HttpClient` exposes
the same operation as `recentTrades(poolAddress)`.

Both methods GET `/stats/{poolAddress}/transactions` and adapt the service's
`transactions` array to `{ pool: poolAddress, trades }`. The service's default
page contains up to 50 transactions, newest first; an empty page returns
`trades: []`. Amounts, prices and fees remain decimal strings. Invalid transaction
responses are rejected, and HTTP errors and abort signals propagate to callers.

The launchpad API's removed `/bff/token/{pool}/trades` route is no longer used.
