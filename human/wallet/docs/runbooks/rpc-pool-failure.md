# RPC pool failure

The gateway broadcasts, estimates gas, simulates and allocates nonces through
its RPC pool (`human/wallet/gateway/src/rpc/pool.ts`). The pool is configured
by `RPC_URLS` (comma-separated chain RPC URLs, defaulting to the public
mainnet hosts listed in `docs/site/docs/reference/public-rpc.md`),
`RPC_LAG_THRESHOLD_BLOCKS` (default 20), `RPC_HEALTH_INTERVAL_MS` (default
5000) and `RPC_TIMEOUT_MS` (default 8000). Every health interval it reads
each endpoint's head; an endpoint that errors is `down`, one that trails the
best head by more than the lag threshold is `lagging`, and only `healthy`
endpoints serve requests, with failover between them. Balance and chain reads
in `src/chainReads.ts` and `src/chain.ts` still use the single
`HYPERPAXEER_RPC_URL` (observation 2.2.1). The attestors read binding nonces
through `ATTESTOR_RPC_URL`, and the shared endpoint relays `eth_` methods to
`LAYERX_GATEWAY_PAXEER_RPC_URL`.

## Trigger

- `curl -sS https://<gateway-base>/readyz` answers 503 with
  `components.rpc_pool.state` `down` and reason `rpc_pool_unavailable`, or
  with `healthy` of one and the other endpoints `down` or `lagging`.
- Sign routes answer the pool's `no healthy RPC endpoint` error.
- Reads fail while the pool is healthy: `HYPERPAXEER_RPC_URL` is failing.

## Preconditions

- The chain itself is producing blocks: at least one RPC you control answers
  a rising `eth_blockNumber`.
- The candidate endpoints answer chain id 125.

## Commands

1. Read the pool's own view of every endpoint.

   ```sh
   curl -sS https://<gateway-base>/readyz | python3 -c 'import json,sys; r=json.load(sys.stdin)["components"]["rpc_pool"]; print(r["state"], r["healthy"]); [print(e["state"], e["head"], e["error"]) for e in r["endpoints"]]'
   ```

   Expected: one line with the pool state and healthy count, then one line per
   endpoint with its state, head and last error.

2. Check each candidate endpoint directly for chain id and head.

   ```sh
   curl -sS -X POST -H 'content-type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' <rpc-url>
   curl -sS -X POST -H 'content-type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' <rpc-url>
   ```

   Expected: `"result":"0x7d"`, then a head within the lag threshold of the
   highest head among the candidates. Individual nodes can trail the head by
   thousands of blocks; keep only those within the threshold.

3. Replace the pool with the endpoints that passed step 2, at least two. The
   secret change restarts the gateway machines one by one.

   ```sh
   flyctl secrets set RPC_URLS=<rpc-url-1>,<rpc-url-2>,<rpc-url-3> --app paxeer-wallet-gateway
   ```

   Expected: `Secrets are deployed` and a rolling restart of every machine.

4. When reads fail, point `HYPERPAXEER_RPC_URL` at one endpoint that passed
   step 2.

   ```sh
   flyctl secrets set HYPERPAXEER_RPC_URL=<rpc-url-1> --app paxeer-wallet-gateway
   ```

   Expected: `Secrets are deployed`.

5. When binding nonce reads at the attestors fail (policy refusals for
   `lx_bind` naming the RPC), set `ATTESTOR_RPC_URL` on each attestor, one at a
   time, with the attestor check between each.

   ```sh
   flyctl secrets set ATTESTOR_RPC_URL=<rpc-url-1> --app paxeer-attestor-<N>
   tools/wallet/check-live.sh attestors
   ```

   Expected: `Secrets are deployed`, then `pass quorum` after each node.

## Readiness check that proves recovery

```sh
curl -sS https://<gateway-base>/readyz
```

The route answers 200 with `components.rpc_pool.state` `up` and `healthy` of
two or more, and `tools/wallet/check-live.sh gateway` (added by task 4.1)
passes. Record revision, command, exit code and log path in
`spec/paxeer-x-wallet/qualification.kvx`, with the count of endpoints and no
URL.

## Rollback

Set `RPC_URLS`, `HYPERPAXEER_RPC_URL` or `ATTESTOR_RPC_URL` back to the value
the operator held before step 3 (`flyctl secrets list` shows only digests), or unset `RPC_URLS` to return the pool to its default
list.

```sh
flyctl secrets unset RPC_URLS --app paxeer-wallet-gateway
```
