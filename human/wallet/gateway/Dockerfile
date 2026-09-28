FROM node:22-bookworm-slim AS build
ENV COREPACK_HOME=/corepack
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /app
COPY human/wallet ./
RUN corepack install
RUN pnpm install --frozen-lockfile
RUN pnpm --filter ./gateway... run --if-present build
RUN find . -name node_modules -type d -prune -exec rm -rf {} + && pnpm install --frozen-lockfile --prod --offline

FROM node:22-bookworm-slim
ENV NODE_ENV=production
ENV COREPACK_HOME=/corepack
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
ENV COREPACK_ENABLE_NETWORK=0
RUN corepack enable
COPY --from=build /corepack /corepack
WORKDIR /app
COPY --from=build --chown=node:node /app ./
RUN chmod -R a+rX /corepack
USER node
EXPOSE 8080
CMD ["pnpm", "--dir", "gateway", "start"]
