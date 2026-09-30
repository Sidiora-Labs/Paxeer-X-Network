FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-platform-identity

FROM alpine:3.22 AS base
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx
COPY --from=build /src/platform/target/release/layerx-identity /usr/local/bin/layerx-identity

FROM base AS fly
RUN apk add --no-cache setpriv
COPY --chmod=0555 platform/hosted/identity/fly-init.sh /usr/local/bin/identity-fly-init
ENTRYPOINT ["/usr/local/bin/identity-fly-init"]

FROM base
USER 4020:4020
ENTRYPOINT ["/usr/local/bin/layerx-identity"]
