FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path interop/Cargo.toml -p layerx-interop-service --bin layerx-interop-gateway

FROM alpine:3.22
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx && \
	install -d -o 4020 -g 4020 -m 0700 /run/layerx
COPY --from=build /src/interop/target/release/layerx-interop-gateway /usr/local/bin/layerx-interop-gateway
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/interop-gateway/files.tsv /etc/layerx/files.tsv
USER 4020:4020
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/layerx-interop-gateway"]
