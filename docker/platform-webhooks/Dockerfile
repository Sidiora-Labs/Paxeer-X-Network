FROM rust:1.91.1-bookworm AS build
WORKDIR /src
COPY . .
RUN cargo build --manifest-path platform/Cargo.toml --locked --release -p layerx-platform-webhooks --bin layerx-webhooks

FROM debian:bookworm-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/platform/target/release/layerx-webhooks /usr/local/bin/layerx-webhooks

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS env-init-prebuilt
ARG SOURCE_REVISION
ARG PACKAGING_REVISION
ARG WEBHOOKS_SHA256
LABEL org.opencontainers.image.revision=$SOURCE_REVISION io.layerx.packaging.revision=$PACKAGING_REVISION io.layerx.webhooks.sha256=$WEBHOOKS_SHA256
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 util-linux && rm -rf /var/lib/apt/lists/*
COPY artifacts/layerx-webhooks /usr/local/bin/layerx-webhooks
RUN test -n "$SOURCE_REVISION" && test -n "$PACKAGING_REVISION" && test -n "$WEBHOOKS_SHA256" && printf '%s  %s\n' "$WEBHOOKS_SHA256" /usr/local/bin/layerx-webhooks | sha256sum -c -
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/platform-webhooks/files.tsv /etc/layerx/files.tsv
COPY platform/hosted/webhooks/env-init.sh /usr/local/bin/webhooks-env-init
RUN chmod 0755 /usr/local/bin/layerx-webhooks && chmod 0555 /usr/local/bin/webhooks-env-init
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/webhooks-env-init"]

FROM base AS env-init
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/platform-webhooks/files.tsv /etc/layerx/files.tsv
COPY --chmod=0555 platform/hosted/webhooks/env-init.sh /usr/local/bin/webhooks-env-init
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/webhooks-env-init"]
