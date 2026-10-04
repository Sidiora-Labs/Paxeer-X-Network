ARG REGISTRY_RUNTIME_BASE=base
FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-platform-registry --bin layerx-program-registry --bin layerx-cgroup-exec

FROM alpine:3.22 AS base
ARG LXP_REVISION
LABEL org.opencontainers.image.revision="$LXP_REVISION"
RUN apk add --no-cache bubblewrap ca-certificates libgcc openssl python3 util-linux && addgroup -g 4030 layerx && adduser -D -H -u 4030 -G layerx layerx
COPY platform/hosted/human/material.py platform/hosted/human/provision.py /usr/local/lib/layerx-human/
COPY --from=build /src/platform/target/release/layerx-program-registry /usr/local/bin/layerx-program-registry
COPY --from=build /src/platform/target/release/layerx-cgroup-exec /usr/bin/layerx-cgroup-exec

FROM ${REGISTRY_RUNTIME_BASE} AS fly
RUN apk add --no-cache curl e2fsprogs util-linux
COPY --chmod=0555 docker/platform-registry/init.sh /usr/local/bin/registry-fly-init
ENTRYPOINT ["/usr/local/bin/registry-fly-init"]

FROM base
USER 4030:4030
CMD ["layerx-program-registry"]
