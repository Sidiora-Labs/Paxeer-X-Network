FROM node:22-bookworm-slim AS build
ENV COREPACK_HOME=/corepack
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable && corepack prepare pnpm@12.6.0 --activate
WORKDIR /app
COPY human/wallet/package.json human/wallet/pnpm-lock.yaml human/wallet/pnpm-workspace.yaml human/wallet/tsconfig.base.json ./
COPY human/wallet/gateway/package.json human/wallet/gateway/tsconfig.json ./gateway/
COPY human/wallet/sdk/package.json ./sdk/
COPY human/wallet/demo/package.json ./demo/
RUN pnpm install --frozen-lockfile --filter ./gateway...
COPY human/wallet/gateway/src ./gateway/src
COPY human/wallet/gateway/migrations ./gateway/migrations
RUN pnpm --filter ./gateway run build
RUN find . -name node_modules -type d -prune -exec rm -rf {} + && pnpm install --frozen-lockfile --prod --filter ./gateway...

FROM node:22-bookworm-slim
ARG RAILWAY_GIT_COMMIT_SHA
ARG SOURCE_REVISION=$RAILWAY_GIT_COMMIT_SHA
ARG SOURCE_TREE
# A Railway build has the commit but no git tree; every other build names both.
RUN test "${#SOURCE_REVISION}" -eq 40 \
    && { test "${#SOURCE_TREE}" -eq 40 || { test -n "$RAILWAY_GIT_COMMIT_SHA" && test -z "$SOURCE_TREE"; }; } \
    && case "$SOURCE_REVISION$SOURCE_TREE" in *[!0-9a-f]*) exit 1;; esac
LABEL org.opencontainers.image.revision="$SOURCE_REVISION" \
      network.paxeer.source-tree="$SOURCE_TREE"
RUN apt-get update && apt-get install -y --no-install-recommends gosu \
    && rm -rf /var/lib/apt/lists/*
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build --chown=node:node /app/node_modules ./node_modules
COPY --from=build --chown=node:node /app/gateway/node_modules ./gateway/node_modules
COPY --from=build --chown=node:node /app/gateway/package.json ./gateway/package.json
COPY --from=build --chown=node:node /app/gateway/dist ./gateway/dist
COPY --from=build --chown=node:node /app/gateway/migrations ./gateway/migrations
COPY --chmod=0555 docker/wallet-gateway/entrypoint.sh /usr/local/bin/wallet-entrypoint
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/wallet-gateway/files.tsv /etc/layerx/files.tsv
ENV LAYERX_FILES_OWNER=node:node
USER root
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/wallet-entrypoint"]
CMD ["node", "/app/gateway/dist/index.js"]
