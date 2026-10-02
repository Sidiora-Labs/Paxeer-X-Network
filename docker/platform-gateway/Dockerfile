FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-platform-gateway --bin layerx-gateway

FROM alpine:3.22
RUN apk add --no-cache ca-certificates libgcc openssl python3 su-exec && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx
COPY --from=build /src/platform/target/release/layerx-gateway /usr/local/bin/layerx-gateway
COPY docker/platform-gateway/entrypoint.sh /usr/local/bin/layerx-gateway-entrypoint
RUN chmod 0755 /usr/local/bin/layerx-gateway-entrypoint
USER 0:0
ENTRYPOINT ["/usr/local/bin/layerx-gateway-entrypoint"]
