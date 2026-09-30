FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-reference-ramp

FROM alpine:3.22 AS base
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx
COPY --from=build /src/platform/target/release/layerx-reference-ramp /usr/local/bin/layerx-reference-ramp

FROM base AS fly
RUN apk add --no-cache jq setpriv
COPY --chmod=0555 platform/ramps/fly-init.sh /usr/local/bin/ramp-fly-init
ENTRYPOINT ["/usr/local/bin/ramp-fly-init"]

FROM base
USER 4020:4020
ENTRYPOINT ["/usr/local/bin/layerx-reference-ramp", "/run/layerx-ramp/config/config.json"]
