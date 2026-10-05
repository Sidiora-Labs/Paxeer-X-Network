# Wallet gateway load test

`wallet.js` is a k6 script for the wallet gateway. Run it from `human/wallet`. `API_BASE` sets the gateway base (default `http://localhost:8787`).

## Health only

```sh
k6 run --vus 50 --duration 30s gateway/loadtest/wallet.js
```

Without `PAXEER_JWT` the script calls only `/healthz`.

## Authenticated

```sh
PAXEER_JWT="<Supabase access token>" k6 run --vus 100 --duration 60s gateway/loadtest/wallet.js
```

With a token it also calls `/v1/wallet/me` with that bearer token.

## Ramp

```sh
k6 run gateway/loadtest/wallet.js
```

Runs 30 s at 10 VUs, 60 s at 100, 60 s at 500 and 30 s at 10. k6 exits non-zero when a threshold fails:

| Metric | Threshold |
| --- | --- |
| HTTP errors | < 1% |
| `/healthz` p95 / p99 | < 50 ms / < 200 ms |
| `/v1/wallet/me` p95 / p99 | < 200 ms / < 500 ms |
| `/v1/wallet/me` errors | < 1% |

The script sends no transactions.
