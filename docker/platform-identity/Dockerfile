FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-platform-identity

FROM alpine:3.22 AS base
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx
COPY --from=build /src/platform/target/release/layerx-identity /usr/local/bin/layerx-identity

FROM base AS env-init
RUN apk add --no-cache setpriv
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/platform-identity/files.tsv /etc/layerx/files.tsv
COPY --chmod=0555 platform/hosted/identity/env-init.sh /usr/local/bin/identity-env-init
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/identity-env-init"]
