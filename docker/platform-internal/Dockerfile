FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-platform-internal

FROM alpine:3.22 AS base
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx
COPY --from=build /src/platform/target/release/layerx-kms /src/platform/target/release/layerx-event-source /usr/local/bin/

FROM base AS fly
RUN apk add --no-cache setpriv
COPY --chmod=0555 platform/hosted/internal/fly-init.sh /usr/local/bin/internal-fly-init
ENTRYPOINT ["/usr/local/bin/internal-fly-init"]

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS fly-prebuilt
ARG SOURCE_REVISION
ARG PACKAGING_REVISION
ARG KMS_SHA256
ARG EVENTS_SHA256
LABEL org.opencontainers.image.revision=$SOURCE_REVISION io.layerx.packaging.revision=$PACKAGING_REVISION io.layerx.kms.sha256=$KMS_SHA256 io.layerx.events.sha256=$EVENTS_SHA256
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 openssl util-linux passwd && rm -rf /var/lib/apt/lists/* && groupadd --gid 4020 layerx && useradd --uid 4020 --gid 4020 --no-create-home layerx
COPY artifacts/layerx-kms artifacts/layerx-event-source /usr/local/bin/
RUN test -n "$SOURCE_REVISION" && test -n "$PACKAGING_REVISION" && test -n "$KMS_SHA256" && test -n "$EVENTS_SHA256" && printf '%s  %s\n' "$KMS_SHA256" /usr/local/bin/layerx-kms "$EVENTS_SHA256" /usr/local/bin/layerx-event-source | sha256sum -c -
COPY platform/hosted/internal/fly-init.sh /usr/local/bin/internal-fly-init
RUN chmod 0755 /usr/local/bin/layerx-kms /usr/local/bin/layerx-event-source && chmod 0555 /usr/local/bin/internal-fly-init
ENTRYPOINT ["/usr/local/bin/internal-fly-init"]

FROM base
USER 4020:4020
ENTRYPOINT ["/usr/local/bin/layerx-kms"]
