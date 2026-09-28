# Wallet API load tests

[k6](https://k6.io) scripts for benchmarking the wallet API.

## Quick smoke (local docker compose, no auth)

```bash
k6 run --vus 50 --duration 30s gateway/loadtest/wallet.js
```

Hits only `/healthz`. Useful for catching boot-time regressions in <30s.

## Ramp test (capacity planning)

```bash
k6 run gateway/loadtest/wallet.js
```

Runs the full 3-minute ramp: 10 → 100 → 500 → 10 VUs. Pass/fail thresholds:

| Metric | Threshold |
| --- | --- |
| Total HTTP errors | < 1% |
| `/healthz` p95 | < 50 ms |
| `/healthz` p99 | < 200 ms |
| `/v1/wallet/me` p95 | < 200 ms |
| `/v1/wallet/me` p99 | < 500 ms |
| `/v1/wallet/me` errors | < 1% |

`k6 run` exits non-zero on threshold violation — wire into CI to catch
perf regressions.

## Authenticated test

Grab a real Supabase access token in the browser devtools (`localStorage` →
`sb-*-auth-token` → `access_token`), then:

```bash
export PAXEER_JWT="eyJ...your-supabase-access-token..."
k6 run --vus 100 --duration 60s gateway/loadtest/wallet.js
```

This exercises the full hot path: JWT verify (JWKS-cached after first hit) +
Postgres lookup + response serialisation.

## Hit a remote host

```bash
API_BASE=https://wallet.example k6 run gateway/loadtest/wallet.js
```

## Tx-send load test

NOT INCLUDED — sending real txs hits the chain and costs gas. If you want
to load-test the send path, do it against a HyperPaxeer testnet with a
disposable wallet master key and a funded faucet account, or wire viem to
an `anvil --fork` instance pointed at your chain's RPC.

## What "good" looks like (18 vCPU box, 12 workers)

Roughly:

| Load | RPS | p95 latency | Notes |
| --- | --- | --- | --- |
| 10 VU healthz | ~3,000 | < 5 ms | warm-up baseline |
| 100 VU healthz | ~20,000 | < 15 ms | well below capacity |
| 500 VU /me  | ~5,000  | < 50 ms | jwks cached, pg buffer cache hits |

If you see p99 > 1s or error rate > 1% at 500 VU, something is wrong —
likely the pg pool is exhausted (raise `DATABASE_POOL_MAX`) or you're
running with `API_WORKERS=1` (you've left ~95% of the CPU idle).
