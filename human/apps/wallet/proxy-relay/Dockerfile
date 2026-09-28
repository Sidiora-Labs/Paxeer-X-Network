# syntax=docker/dockerfile:1.7

FROM node:22-alpine
WORKDIR /srv/proxy-relay

ENV NODE_ENV=production \
    PORT=8080

COPY proxy-relay/package.json proxy-relay/pnpm-lock.yaml ./
RUN corepack enable \
    && pnpm install --frozen-lockfile --prod --ignore-scripts --ignore-workspace \
    && chown -R node:node /srv/proxy-relay

COPY --chown=node:node proxy-relay/server.mjs ./

USER node
EXPOSE 8080

HEALTHCHECK --interval=20s --timeout=5s --start-period=30s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:8080/api/proxy/health').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"

CMD ["node", "server.mjs"]
