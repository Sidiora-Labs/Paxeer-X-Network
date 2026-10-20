# syntax=docker/dockerfile:1.7
#
# Hyperpax-UI production image.
#
# Three stages:
#   1. deps    — install production-only node_modules with pnpm.
#   2. builder — run `next build` to produce .next/standalone + .next/static.
#   3. runner  — minimal Node 22 alpine, copy the standalone bundle + static + public.
#
# Why standalone?
#   `output: 'standalone'` in next.config.js traces only the deps the app
#   actually imports and emits a self-contained `server.js`. The final
#   image is ~300 MB instead of ~1 GB.
#
# Why pnpm?
#   The repo lockfile is pnpm-lock.yaml. Using npm/yarn here would resolve
#   different versions and silently drift from local dev / CI.
#
# Why a non-root user?
#   Defense in depth. If anything ever escapes the Next.js sandbox, the
#   process can't write outside /app or read host paths it doesn't own.

ARG NODE_VERSION=22-alpine

# ─────────────────────────────────────────────────────────────
# Stage 1: deps — install node_modules from the locked manifest.
# ─────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION} AS deps
WORKDIR /app

# libc6-compat: a handful of native deps in the wagmi/viem dependency
# graph expect glibc symbols. Alpine ships musl, so the compat shim is
# the cheapest fix; the alternative is switching to debian-slim which
# adds ~70 MB.
RUN apk add --no-cache libc6-compat

# Enable pnpm via corepack and pin to the version the repo uses locally.
# Corepack ships in node:22 — no separate install needed.
RUN corepack enable && corepack prepare pnpm@9 --activate

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# ─────────────────────────────────────────────────────────────
# Stage 2: builder — produce the standalone artifact.
# ─────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION} AS builder
WORKDIR /app

RUN apk add --no-cache libc6-compat
RUN corepack enable && corepack prepare pnpm@9 --activate

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Disable Next.js telemetry in CI/Docker builds.
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production

# IMPORTANT: NEXT_PUBLIC_* vars are baked into the client bundle at
# build time. They must be provided here, not at `docker run` time.
# We accept them as build args (or fall back to safe defaults) and
# re-export them so `next build` can see them.
ARG NEXT_PUBLIC_PAXEER_RPC_URL=https://public-mainnet.rpcpaxeer.online/evm
ARG NEXT_PUBLIC_PAXEER_RPC_URLS=
ARG NEXT_PUBLIC_NETWORK_STATUS_WS_URL=
ARG NEXT_PUBLIC_LOG_LEVEL=warn
ARG NEXT_PUBLIC_PERPS_GATEWAY_URL=
ARG NEXT_PUBLIC_MARKET_PROXY_HOST=
ARG NEXT_PUBLIC_PERPS_PRECOMPILE=
ARG NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE=
ARG NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE=
ARG NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE=
ARG NEXT_PUBLIC_PECOR_STOP_PRECOMPILE=
ARG NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE=
ARG NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE=
ARG NEXT_PUBLIC_RELEASE_SPOT_OPEN=
ARG NEXT_PUBLIC_PAXSCAN_API_URL=
ARG NEXT_PUBLIC_PAXSCAN_WS_URL=
ARG NEXT_PUBLIC_PAXSCAN_BASE_URL=
ARG NEXT_PUBLIC_PAXEER_WALLET_API=
ARG NEXT_PUBLIC_SUPABASE_URL=
ARG NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=
ARG NEXT_PUBLIC_AUTH_REDIRECT_URL=
ARG NEXT_PUBLIC_WALLET_IFRAME_ORIGIN=
ARG NEXT_PUBLIC_VERCEL_GIT_COMMIT_SHA=
ENV NEXT_PUBLIC_PAXEER_RPC_URL=$NEXT_PUBLIC_PAXEER_RPC_URL \
    NEXT_PUBLIC_PAXEER_RPC_URLS=$NEXT_PUBLIC_PAXEER_RPC_URLS \
    NEXT_PUBLIC_NETWORK_STATUS_WS_URL=$NEXT_PUBLIC_NETWORK_STATUS_WS_URL \
    NEXT_PUBLIC_LOG_LEVEL=$NEXT_PUBLIC_LOG_LEVEL \
    NEXT_PUBLIC_PERPS_GATEWAY_URL=$NEXT_PUBLIC_PERPS_GATEWAY_URL \
    NEXT_PUBLIC_MARKET_PROXY_HOST=$NEXT_PUBLIC_MARKET_PROXY_HOST \
    NEXT_PUBLIC_PERPS_PRECOMPILE=$NEXT_PUBLIC_PERPS_PRECOMPILE \
    NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE=$NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE \
    NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE=$NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE \
    NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE=$NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE \
    NEXT_PUBLIC_PECOR_STOP_PRECOMPILE=$NEXT_PUBLIC_PECOR_STOP_PRECOMPILE \
    NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE=$NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE \
    NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE=$NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE \
    NEXT_PUBLIC_RELEASE_SPOT_OPEN=$NEXT_PUBLIC_RELEASE_SPOT_OPEN \
    NEXT_PUBLIC_PAXSCAN_API_URL=$NEXT_PUBLIC_PAXSCAN_API_URL \
    NEXT_PUBLIC_PAXSCAN_WS_URL=$NEXT_PUBLIC_PAXSCAN_WS_URL \
    NEXT_PUBLIC_PAXSCAN_BASE_URL=$NEXT_PUBLIC_PAXSCAN_BASE_URL \
    NEXT_PUBLIC_PAXEER_WALLET_API=$NEXT_PUBLIC_PAXEER_WALLET_API \
    NEXT_PUBLIC_SUPABASE_URL=$NEXT_PUBLIC_SUPABASE_URL \
    NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=$NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY \
    NEXT_PUBLIC_AUTH_REDIRECT_URL=$NEXT_PUBLIC_AUTH_REDIRECT_URL \
    NEXT_PUBLIC_WALLET_IFRAME_ORIGIN=$NEXT_PUBLIC_WALLET_IFRAME_ORIGIN \
    NEXT_PUBLIC_VERCEL_GIT_COMMIT_SHA=$NEXT_PUBLIC_VERCEL_GIT_COMMIT_SHA

RUN pnpm build

# ─────────────────────────────────────────────────────────────
# Stage 3: runner — minimal runtime.
# ─────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION} AS runner
WORKDIR /app

# Same libc compat as the builder so any traced native deps still load.
RUN apk add --no-cache libc6-compat tini

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# Non-root user. uid/gid 1001 matches common Docker conventions and is
# unlikely to collide with host users.
RUN addgroup -g 1001 -S nodejs && \
    adduser -S -u 1001 -G nodejs nextjs

# next-pwa writes sw.js + workbox-*.js into public/ during build, so
# we copy public/ from the builder rather than the source tree.
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

EXPOSE 3000

# tini reaps zombies and forwards SIGTERM cleanly to node, which
# matters on a shared host where pid 1 semantics are fragile.
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["node", "server.js"]

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/api/build-id || exit 1
