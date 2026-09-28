# syntax=docker/dockerfile:1.7

ARG SELKIES_COMMIT=49946919c9f97415df7dd36649391da493d262d0

FROM debian:bookworm-slim AS selkies-source
ARG SELKIES_COMMIT

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --filter=blob:none https://github.com/selkies-project/selkies.git /src/selkies \
    && git -C /src/selkies checkout "${SELKIES_COMMIT}"

FROM node:22-bookworm-slim AS selkies-web

COPY --from=selkies-source /src/selkies /src/selkies
COPY browser-plane/selkies-hash-token.mjs /tmp/selkies-hash-token.mjs

RUN node /tmp/selkies-hash-token.mjs \
        /src/selkies/addons/selkies-web-core/selkies-ws-core.js \
        /src/selkies/addons/selkies-dashboard/index.html \
    && cd /src/selkies/addons/selkies-web-core \
    && npm install \
    && npm run build \
    && cd /src/selkies/addons/selkies-dashboard \
    && cp ../selkies-web-core/dist/selkies-core.js src/ \
    && npm install \
    && SELKIES_INJECT=1 SELKIES_MODE=websockets npm run build \
    && mkdir -p /opt/selkies-web/src /opt/selkies-web/assets \
    && cp -a dist/. /opt/selkies-web/ \
    && cp ../selkies-web-core/dist/selkies-core.js /opt/selkies-web/src/ \
    && cp ../universal-touch-gamepad/universalTouchGamepad.js /opt/selkies-web/src/ \
    && cp -a ../selkies-web-core/dist/jsdb /opt/selkies-web/

FROM python:3.12-bookworm AS selkies-wheel

WORKDIR /src/selkies
COPY --from=selkies-source /src/selkies ./
COPY --from=selkies-web /opt/selkies-web ./src/selkies/selkies_web

RUN python -m pip install --no-cache-dir build \
    && python -m build --wheel

FROM node:22-bookworm-slim

WORKDIR /srv/browser-plane

ENV NODE_ENV=production \
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium \
    BROWSER_CHROMIUM_NO_SANDBOX=1 \
    BROWSER_PLANE_HOST=0.0.0.0 \
    BROWSER_PLANE_PORT=3101 \
    BROWSER_PLANE_MAX_SESSIONS=1 \
    BROWSER_STREAM_ENABLED=1 \
    BROWSER_STREAM_ORIGIN=http://127.0.0.1:8081/api/browser-stream/ \
    BROWSER_STREAM_WIDTH=780 \
    BROWSER_STREAM_HEIGHT=1592 \
    BROWSER_STREAM_MAX_WIDTH=2560 \
    BROWSER_STREAM_MAX_HEIGHT=2560 \
    DISPLAY=:99 \
    SELKIES_ADDR=0.0.0.0 \
    SELKIES_PORT=8081 \
    SELKIES_SUBFOLDER=/api/browser-stream/ \
    SELKIES_MODE=websockets \
    SELKIES_ENABLE_DUAL_MODE=false \
    SELKIES_ENABLE_HTTPS=false \
    SELKIES_ENABLE_BASIC_AUTH=false \
    SELKIES_ALLOWED_ORIGINS=null \
    SELKIES_ENCODER=h264enc \
    SELKIES_FRAMERATE=60-60 \
    SELKIES_VIDEO_CRF=18-18 \
    SELKIES_VIDEO_FULLCOLOR=false \
    SELKIES_VIDEO_STREAMING_MODE=true \
    SELKIES_USE_CPU=true \
    SELKIES_USE_PAINT_OVER_QUALITY=true \
    SELKIES_VIDEO_PAINTOVER_CRF=14-14 \
    SELKIES_VIDEO_PAINTOVER_BURST_FRAMES=5-5 \
    SELKIES_ENABLE_RESIZE=true \
    SELKIES_IS_MANUAL_RESOLUTION_MODE=false \
    SELKIES_MANUAL_WIDTH=0 \
    SELKIES_MANUAL_HEIGHT=0 \
    SELKIES_SCALING_DPI=192 \
    SELKIES_FORCE_ALIGNED_RESOLUTION=true \
    SELKIES_USE_CSS_SCALING=false \
    SELKIES_USE_BROWSER_CURSORS=true \
    SELKIES_AUDIO_ENABLED=false \
    SELKIES_MICROPHONE_ENABLED=false \
    SELKIES_GAMEPAD_ENABLED=false \
    SELKIES_CLIPBOARD_ENABLED=false \
    SELKIES_CLIPBOARD_IN_ENABLED=false \
    SELKIES_CLIPBOARD_OUT_ENABLED=false \
    SELKIES_COMMAND_ENABLED=false \
    SELKIES_FILE_TRANSFERS=none \
    SELKIES_SECOND_SCREEN=false \
    SELKIES_ENABLE_SHARING=false \
    SELKIES_ENABLE_COLLAB=false \
    SELKIES_ENABLE_SHARED=false \
    SELKIES_UI_SHOW_LOGO=false \
    SELKIES_UI_SHOW_SIDEBAR=false \
    SELKIES_UI_SHOW_CORE_BUTTONS=false \
    SELKIES_UI_SIDEBAR_SHOW_KEYBOARD_BUTTON=false

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        chromium \
        fonts-liberation \
        fonts-noto-color-emoji \
        libpulse0 \
        libx11-xcb1 \
        libxcb-dri3-0 \
        libxdamage1 \
        libxext6 \
        libxfixes3 \
        libxkbcommon-dev \
        libxtst6 \
        openbox \
        pkg-config \
        pulseaudio \
        python3 \
        python3-dev \
        python3-pip \
        tini \
        x11-utils \
        x11-xkb-utils \
        x11-xserver-utils \
        xcvt \
        xvfb \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 1001 browser \
    && useradd --system --uid 1001 --gid browser --create-home browser \
    && mkdir -p /tmp/.X11-unix \
    && chmod 1777 /tmp/.X11-unix

COPY --from=selkies-wheel /src/selkies/dist/*.whl /tmp/
RUN python3 -m pip install --break-system-packages --no-cache-dir /tmp/selkies-*.whl \
    && rm /tmp/selkies-*.whl

COPY --chown=browser:browser browser-plane/package.json browser-plane/pnpm-lock.yaml ./
RUN corepack enable \
    && pnpm install --frozen-lockfile --prod --ignore-scripts --ignore-workspace

COPY --chown=browser:browser browser-plane/provider-source.mjs browser-plane/security.mjs browser-plane/session.mjs browser-plane/server.mjs browser-plane/start.sh ./
RUN chmod 0755 /srv/browser-plane/start.sh

USER browser
EXPOSE 3101 8081

HEALTHCHECK --interval=20s --timeout=4s --start-period=10s --retries=3 \
  CMD ["node", "-e", "const fs=require('fs');const key=fs.readFileSync(process.env.BROWSER_PLANE_INTERNAL_KEY_FILE,'utf8').trim();Promise.all([fetch('http://127.0.0.1:3101/health',{headers:{'x-browser-plane-key':key}}),fetch('http://127.0.0.1:8081/api/browser-stream/api/health')]).then(rs=>{if(rs.some(r=>!r.ok))process.exit(1)}).catch(()=>process.exit(1))"]

ENTRYPOINT ["/usr/bin/tini", "-g", "--"]
CMD ["/srv/browser-plane/start.sh"]
