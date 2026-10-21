FROM node:24.6.0-bookworm-slim AS build
WORKDIR /src
COPY apps/web/package.json apps/web/package-lock.json /src/apps/web/
COPY apps/web/packages/layerx-ui /src/apps/web/packages/layerx-ui
COPY services/platform/hosted/dashboard/web /src/services/platform/hosted/dashboard/web
COPY scripts/ci/developer-dashboard-lock.mjs /src/scripts/ci/developer-dashboard-lock.mjs
RUN node scripts/ci/developer-dashboard-lock.mjs
RUN npm --prefix /src/apps/web ci --ignore-scripts --no-audit --no-fund && \
    npm --prefix /src/apps/web run build:ui && \
    ln -s ../../../../../apps/web/node_modules /src/services/platform/hosted/dashboard/web/node_modules
WORKDIR /src/services/platform/hosted/dashboard/web
RUN npm run test:static && npm run build

FROM node:24.6.0-bookworm-slim
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0
WORKDIR /app
COPY --from=build /src/services/platform/hosted/dashboard/web/.next/standalone ./
COPY --from=build /src/services/platform/hosted/dashboard/web/.next/static ./services/platform/hosted/dashboard/web/.next/static
WORKDIR /app/services/platform/hosted/dashboard/web
USER 65532:65532
CMD ["node", "server.js"]
