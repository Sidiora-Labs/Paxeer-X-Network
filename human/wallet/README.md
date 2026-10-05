# Paxeer X wallet workspace

One pnpm workspace holding the Paxeer X wallet's TypeScript packages.

## Layout

| Path | Package | Contents |
| --- | --- | --- |
| `gateway/` | `@paxeer/api` | HTTP wallet gateway: the `/v1/wallet`, `/v1/agent` and `/v1/agents` routes, database migrations under `gateway/migrations`, test suites under `gateway/test`, a load test script under `gateway/loadtest` |
| `sdk/` | `@paxeer/wallet` | Browser and React SDK for the wallet gateway, test suites under `sdk/test` |
| `demo/` | `@paxeer/demo` | Web reference app built on the SDK |
| `deploy/env` | | Every environment variable name the wallet services read, one per line with its purpose, grouped by service; no values |
| `tsconfig.base.json` | | Compiler options shared by every package |

Other directories are not pnpm packages:

| Path | Contents |
| --- | --- |
| `attestor/` | Go module: the threshold attestor server (`cmd/attestor`), its load generator (`cmd/loadtest`) and the key dealer (`dealer`) |
| `ceremony/` | Go module: the `ceremony` command (`plan`, `deliver`, `archive`, `rehearse`, `move`) for custody key ceremonies |
| `schema/attestor-api` | Attestor API contract (`v1.kvx`) and golden examples |
| `deploy/` | Attestor, gateway, endpoint and human service configuration files and the cutover material |
| `docs/` | Cutover notes, runbooks and the security review |

## Install and test

Requires pnpm and the runtime version named in the `engines` field of `package.json`.

```sh
cd human/wallet
pnpm install
pnpm -r --filter ./gateway --filter ./sdk build
pnpm -r test
```

The gateway suites load `gateway/test/setup.ts`, which supplies synthetic environment values so no configuration file is needed to run them.

From the repository root, `scripts/wallet/scan-secrets.sh human/wallet` scans the workspace for key-shaped, token-shaped and credentialed connection-string content and exits non-zero on a match.

## Configuration

Configuration is read from environment variables only; no environment file is kept in the repository. The gateway validates its variables at startup in `gateway/src/env.ts`. Purposes are listed in `deploy/env`.

Gateway:

- `NODE_ENV`, `PORT`, `LOG_LEVEL`, `API_WORKERS`, `CORS_ORIGINS`
- `SUPABASE_URL`, `WALLET_IDENTITY_BINDING_TENANT`, `WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE`
- `DATABASE_URL`, `DATABASE_POOL_MAX`
- `WALLET_MASTER_KEY`, `WALLET_MASTER_KEY_VERSION`
- `HYPERPAXEER_CHAIN_ID`, `HYPERPAXEER_RPC_URL`, `HYPERPAXEER_EXPLORER_URL`
- `RPC_URLS`, `RPC_LAG_THRESHOLD_BLOCKS`, `RPC_HEALTH_INTERVAL_MS`, `RPC_TIMEOUT_MS`
- `POLICY_MAX_TX_VALUE_WEI`, `POLICY_MAX_DAILY_VALUE_WEI`, `POLICY_RATE_LIMIT_PER_MINUTE`
- `RATE_LIMIT_CLIENT_PER_MINUTE`, `RATE_LIMIT_ACCOUNT_PER_MINUTE`
- `AGENT_JWT_SECRET`, `AGENT_TOKEN_TTL_SECONDS`, `AGENT_CHALLENGE_TTL_SECONDS`, `AGENT_REQUEST_MAX_TTL_SECONDS`, `AGENT_DEFAULT_FROZEN`, `AGENT_DEFAULT_MODE`, `AGENT_DEFAULT_MAX_TX_VALUE_WEI`, `AGENT_DEFAULT_MAX_DAILY_VALUE_WEI`, `AGENT_DEFAULT_RATE_LIMIT_PER_MINUTE`, `AGENT_DEFAULT_MAX_APPROVE_WEI`
- `LAYERX_VAULT_ADDRESS`, `LAYERX_USDL_ADDRESS`, `LAYERX_USDL_DECIMALS`, `LAYER_X_DB_URI`, `LAYERX_SYNC_INTERVAL_MS`
- `ACTION_CONFIRMATIONS`, `ACTION_RECEIPT_TIMEOUT_MS`, `ACTION_WORKER_INTERVAL_MS`, `ACTION_RECONCILE_INTERVAL_MS`, `ACTION_FEE_BUMP_PERCENT`
- `WALLET_CUSTODY_INVENTORY_FILE`, `WALLET_CUSTODY_INVENTORY_PUBLIC_KEY_FILE`
- `ATTESTOR_ENDPOINTS`, `ATTESTOR_CLIENT_CERT_FILE`, `ATTESTOR_CLIENT_KEY_FILE`, `ATTESTOR_CA_FILE`, `ATTESTOR_QUORUM`, `ATTESTOR_HEALTH_INTERVAL_MS`, `ATTESTOR_TIMEOUT_MS`
- `SPONSOR_PRIVATE_KEY_FILE`, `ACCOUNT_SETUP_GAS_CAP_WEI`

Demo:

- `NEXT_PUBLIC_PAXEER_WALLET_API`
- `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`
- `NEXT_PUBLIC_AUTH_REDIRECT_URL`
- `NEXT_PUBLIC_SITE_URL`
