# Deployment

## Container image

The image is defined in [`docker/wallet-pwa/Dockerfile`](../../../../../docker/wallet-pwa/Dockerfile) and is built from the repository root, because it also copies `agent/sdk/typescript` and `human/wallet` to build the wallet SDK:

```sh
docker build -f docker/wallet-pwa/Dockerfile \
  --build-arg NEXT_PUBLIC_PAXEER_WALLET_API=... \
  --build-arg NEXT_PUBLIC_PAXEER_RPC_URL=... \
  -t wallet-pwa .
```

The build stage accepts the public network variables as build arguments: `NEXT_PUBLIC_PAXEER_WALLET_API`, `NEXT_PUBLIC_PAXEER_RPC_URL`, `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`, `NEXT_PUBLIC_AUTH_REDIRECT_URL`, `NEXT_PUBLIC_PAXEER_HUMAN_API`, `NEXT_PUBLIC_PAXEER_EXPLORER_URL`, `NEXT_PUBLIC_PAXEER_ATTESTOR_URL`, `NEXT_PUBLIC_PNS_API_BASE`, `NEXT_PUBLIC_POINTS_API_BASE`, `NEXT_PUBLIC_MARKET_DATA_API` and `NEXT_PUBLIC_FX_RATES_API`.

The runtime stage runs `deployment/start-wallet.sh` under `tini`: it starts the standalone Next.js server on loopback and nginx with `deployment/nginx.conf` in front of it, and exits when either process stops. The image health check calls `/wallet/api/health?mode=liveness` on the Next.js server.

Run-time server variables (`BLOCKSCOUT_UPSTREAM_BASE`, S3, VAPID, push, chat, Sentry and `TRUSTED_PROXY_SECRET`) are set on the container; the full list is in the [app README](../../README.md#configuration).

## Other descriptors

- `railway.json` builds with the same Dockerfile and restarts on failure, at most three retries.
- `nixpacks.toml` installs with `pnpm install --frozen-lockfile`, builds with `npm run build` and starts with `npm run start`.
- `ecosystem.config.cjs` runs `next start` under pm2 from the app directory.

## After a deployment

`GET /wallet/api/health?mode=readiness` reports process, upstream and push configuration checks; `?mode=liveness` reports only that the process answers. Recovery procedures are in [operations/runbooks.md](../operations/runbooks.md).
