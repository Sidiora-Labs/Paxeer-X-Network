# Paxeer X wallet app

The Next.js wallet web app (`@paxeer/wallet-app`) of Paxeer X Network, installable as a PWA. It uses the wallet SDK in [`../../wallet/sdk`](../../wallet/sdk/README.md) (`@paxeer/wallet`) for embedded and injected wallets, and adds the unified account views under `/account` and the network surfaces under `/surfaces` (exchange, bridge, launchpad, fees, web data).

## Scripts

Run from this directory. Node 20 is pinned in `.nvmrc`.

- `pnpm install --frozen-lockfile` installs dependencies from `pnpm-lock.yaml`.
- `pnpm dev` starts the development server on port 3099.
- `pnpm build` builds the PWA assets (`scripts/build-pwa.mjs`) and then the Next.js app.
- `pnpm run type-check` runs the TypeScript check; `pnpm lint` runs ESLint.
- `pnpm test` (`vitest run`) runs the unit suite; `pnpm run test:wallet-app` runs the Playwright suite in `playwright.wallet-app.config.ts`.
- `pnpm run generate:swap-abis` regenerates the swap ABI index; `pnpm run check:swap-abis` fails on drift.
- `pnpm run release:check` runs `scripts/release-check.sh`.
- `scripts/scan-secrets.sh [dir]` scans the app tree, or `dir`, for committed secrets and local artefacts; it prints `path:line: class` for every hit without the matched value and exits 1 on any hit, 0 when clean. `scripts/scan-secrets.test.sh` exercises it against generated fixtures.

The production image is built from [`docker/wallet-pwa/Dockerfile`](../../../docker/wallet-pwa/Dockerfile) at the repository root; it runs the standalone Next.js server behind nginx with `deployment/start-wallet.sh` and `deployment/nginx.conf`.

## Documentation

- [docs/architecture](docs/architecture/README.md): entry point, routes, providers, server edge
- [docs/architecture/storage-forensics.md](docs/architecture/storage-forensics.md): browser storage registry
- [docs/api](docs/api/README.md): server routes under `src/app/api`
- [docs/development](docs/development/README.md) and [docs/deployment](docs/deployment/README.md)
- [docs/operations/runbooks.md](docs/operations/runbooks.md): recovery runbooks

## Configuration

Every value is supplied through the environment at build or run time; no environment file is committed. `NEXT_PUBLIC_*` values are inlined at build time, and the Dockerfile accepts the network ones as build arguments.

| Variable | Scope | Purpose |
| --- | --- | --- |
| `NEXT_PUBLIC_PAXEER_WALLET_API` | build | Wallet gateway base |
| `NEXT_PUBLIC_PAXEER_RPC_URL` | build | Chain RPC base |
| `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` | build | Supabase project for sign-in |
| `NEXT_PUBLIC_AUTH_REDIRECT_URL` | build | Sign-in redirect target |
| `NEXT_PUBLIC_PAXEER_HUMAN_API` | build | Human service base used by the account views |
| `NEXT_PUBLIC_PAXEER_EXPLORER_URL` | build | Explorer base used by the account views and transfer status |
| `NEXT_PUBLIC_PAXEER_ATTESTOR_URL` | build | Attestor base |
| `NEXT_PUBLIC_PNS_API_BASE` | build | Name service indexer base; name lookups return nothing when unset |
| `NEXT_PUBLIC_POINTS_API_BASE` | build | Points indexer base |
| `NEXT_PUBLIC_MARKET_DATA_API` | build | Market data base for PAX and listed token prices; price reads fail when unset |
| `NEXT_PUBLIC_FX_RATES_API` | build | Fiat exchange rate base; amounts stay in USD when unset |
| `NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN` | build | Storage origin allowed as an image source by the content security policy |
| `NEXT_PUBLIC_CHAIN_ID` | build | Chain ID, default 125 |
| `NEXT_PUBLIC_PAXEER_GAS_SPONSOR`, `NEXT_PUBLIC_PAXEER_GAS_PAYMASTER` | build | Gas sponsor and paymaster used by the fee surface |
| `BLOCKSCOUT_UPSTREAM_BASE` | run | Explorer backend behind the same-origin `/api/wallet` route |
| `SIDIORA_SDK_UPSTREAM` | run | Optional override of the `/api/sdk` upstream |
| `S3_ENDPOINT_URL`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_REGION` | run | Object store behind `/api/token-icon` and `/api/token-metadata` |
| `OPENAI_API_KEY` | run | `/api/chat` credential |
| `VAPID_PRIVATE_KEY`, `NEXT_PUBLIC_VAPID_PUBLIC_KEY`, `VAPID_SUBJECT` | run, build | Web push keys; generate with `scripts/generate-vapid-keys.js` |
| `PUSH_ADMIN_KEY` | run | Push administration routes |
| `PUSH_DATA_DIR`, `RATE_LIMIT_STORE_PATH` | run | Push store directory and rate-limit store file |
| `TRUSTED_PROXY_SECRET` | run | Shared secret between the edge proxy and the app |
| `SENTRY_DSN`, `NEXT_PUBLIC_SENTRY_DSN` | run, build | Error reporting; reporting is off when unset |

Each configured `NEXT_PUBLIC_*` network base (`src/pwa/config.ts`) joins the content security policy connect list in `src/lib/security/csp.ts`.

`.env` and `.env.*` are ignored by git, together with build output, native intermediates, signing files and editor and OS artefacts; see `.gitignore`.
