# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS sdk
WORKDIR /src/human/wallet
ENV NEXT_TELEMETRY_DISABLED=1 \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0

RUN corepack enable \
    && corepack prepare pnpm@12.6.0 --activate
COPY agent/sdk/typescript /src/agent/sdk/typescript
COPY human/wallet /src/human/wallet
RUN pnpm install --frozen-lockfile --filter ./sdk... \
    && pnpm --filter ./sdk... run build

FROM node:22-alpine AS builder
WORKDIR /src/human/apps/wallet
ENV NEXT_TELEMETRY_DISABLED=1 \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0

RUN corepack enable \
    && corepack prepare pnpm@12.6.0 --activate
COPY --from=sdk /src /src
COPY human/apps/wallet/package.json human/apps/wallet/pnpm-lock.yaml human/apps/wallet/pnpm-workspace.yaml human/apps/wallet/.npmrc ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY human/apps/wallet ./

ARG NEXT_PUBLIC_PAXEER_WALLET_API
ARG NEXT_PUBLIC_PAXEER_RPC_URL
ARG NEXT_PUBLIC_SUPABASE_URL
ARG NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY
ARG NEXT_PUBLIC_AUTH_REDIRECT_URL
ARG NEXT_PUBLIC_PAXEER_HUMAN_API
ARG NEXT_PUBLIC_PAXEER_EXPLORER_URL
ARG NEXT_PUBLIC_PAXEER_ATTESTOR_URL
ARG NEXT_PUBLIC_PNS_API_BASE
ARG NEXT_PUBLIC_POINTS_API_BASE
ARG NEXT_PUBLIC_MARKET_DATA_API
ARG NEXT_PUBLIC_FX_RATES_API
ENV NEXT_PUBLIC_PAXEER_WALLET_API=$NEXT_PUBLIC_PAXEER_WALLET_API \
    NEXT_PUBLIC_PAXEER_RPC_URL=$NEXT_PUBLIC_PAXEER_RPC_URL \
    NEXT_PUBLIC_SUPABASE_URL=$NEXT_PUBLIC_SUPABASE_URL \
    NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=$NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY \
    NEXT_PUBLIC_AUTH_REDIRECT_URL=$NEXT_PUBLIC_AUTH_REDIRECT_URL \
    NEXT_PUBLIC_PAXEER_HUMAN_API=$NEXT_PUBLIC_PAXEER_HUMAN_API \
    NEXT_PUBLIC_PAXEER_EXPLORER_URL=$NEXT_PUBLIC_PAXEER_EXPLORER_URL \
    NEXT_PUBLIC_PAXEER_ATTESTOR_URL=$NEXT_PUBLIC_PAXEER_ATTESTOR_URL \
    NEXT_PUBLIC_PNS_API_BASE=$NEXT_PUBLIC_PNS_API_BASE \
    NEXT_PUBLIC_POINTS_API_BASE=$NEXT_PUBLIC_POINTS_API_BASE \
    NEXT_PUBLIC_MARKET_DATA_API=$NEXT_PUBLIC_MARKET_DATA_API \
    NEXT_PUBLIC_FX_RATES_API=$NEXT_PUBLIC_FX_RATES_API

RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=3001

RUN apk add --no-cache nginx tini \
    && addgroup --system --gid 1001 nodejs \
    && adduser --system --uid 1001 nextjs \
    && mkdir -p /data/paxport /tmp/nginx/client-body /tmp/nginx/proxy /tmp/nginx/fastcgi /tmp/nginx/uwsgi /tmp/nginx/scgi \
    && chown -R nextjs:nodejs /data/paxport /tmp/nginx

COPY --from=builder --chown=nextjs:nodejs /src/human/apps/wallet/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /src/human/apps/wallet/public ./human/apps/wallet/public
COPY --from=builder --chown=nextjs:nodejs /src/human/apps/wallet/.next/static ./human/apps/wallet/.next/static
COPY --chown=nextjs:nodejs human/apps/wallet/deployment/nginx.conf /etc/nginx/nginx.conf
COPY --chown=nextjs:nodejs --chmod=0755 human/apps/wallet/deployment/start-wallet.sh /app/start-wallet.sh

USER nextjs
EXPOSE 3001

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD node -e "fetch('http://127.0.0.1:3001/api/health?mode=liveness').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"

ENTRYPOINT ["/sbin/tini", "-g", "--"]
CMD ["/app/start-wallet.sh"]
