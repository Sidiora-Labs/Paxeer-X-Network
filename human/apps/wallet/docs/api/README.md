# Wallet app server routes

Every server route lives under `src/app/api` and runs inside the Next.js app. Proxies forward only to fixed or configured upstream origins, bound path segments and query parameters, and convert upstream failures into public error codes through `src/server/http.ts`.

| Route | Methods | Upstream or store | Notes |
| --- | --- | --- | --- |
| `/api/wallet/[...path]` | GET, HEAD; POST, PUT, PATCH, DELETE answer 405 | `BLOCKSCOUT_UPSTREAM_BASE` | Explorer v2 API proxy; 600 requests per minute per client; in-process response cache and in-flight deduplication |
| `/api/sdk/[...path]` | GET | `SIDIORA_SDK_UPSTREAM`, or the Sidiora SDK default in the route | 120 requests per minute per client |
| `/api/sidiora/metadata` | GET | Sidiora batch metadata | `?addresses=` with 1 to 100 comma-separated addresses; shared cache 24 hours |
| `/api/sidiora/logo/[...path]` | GET | Sidiora logo origin | 200 requests per minute per client; cached 30 days, immutable |
| `/api/candle/[...path]`, `/api/candle/cv/[...path]`, `/api/candle/pax/[...path]`, `/api/candle/sid/[...path]` | GET | Fixed market data origins in each route | JSON proxy through `src/server/json-proxy.ts` |
| `/api/pns/api/v1/addresses-lookup`, `/api/pns/api/v1/addresses/[address]`, `/api/pns/api/v1/domains-lookup`, `/api/pns/api/v1/domains/[name]`, `/api/pns/api/v1/domains/[name]/events` | GET | Name service indexer | Fixed routes in `src/server/wallet-read-proxy.ts` |
| `/api/points/balance/[address]` | GET | Points indexer | `src/server/wallet-read-proxy.ts` |
| `/api/fx/latest/USD` | GET | Exchange rate origin | `src/server/wallet-read-proxy.ts` |
| `/api/media` | GET | `?url=` restricted to `ALLOWED_MEDIA_ORIGINS` | Image content types only, at most 1 MiB |
| `/api/token-icon/[address]` | GET | S3-compatible object store | Image content types only |
| `/api/token-metadata` | GET | S3-compatible object store | Launchpad token metadata |
| `/api/chat` | POST | Chat completions with `OPENAI_API_KEY` | 20 requests per minute per client |
| `/api/push/subscribe` | POST, DELETE | Push subscription store under `PUSH_DATA_DIR` | |
| `/api/push/send` | GET, POST | Push campaigns | Requires `PUSH_ADMIN_KEY` |
| `/api/push/notify-tx` | POST | Transaction notifications | Requires `PUSH_ADMIN_KEY` |
| `/api/push/cron` | GET, POST | Campaign scheduler | Requires `PUSH_ADMIN_KEY` |
| `/api/health` | GET | Process, upstream and push configuration checks | `?mode=liveness` or `?mode=readiness` (default) |

## Rate limiting

`src/lib/rateLimit.ts` provides `createRateLimiter` and the shared `sdkLimiter`, `walletLimiter` and `sidioraLimiter`. Windows are one minute, clients are keyed by `trustedClientIdentity` from `src/server/http.ts`, and counters persist in an atomic JSON store at `RATE_LIMIT_STORE_PATH`.

## Push administration

Administrative push routes call `requirePushAdmin`, which accepts the key either in the `x-push-admin-key` header or as a bearer token, never both, compares it in constant time, and answers 503 when `PUSH_ADMIN_KEY` is unset or shorter than 32 characters.
