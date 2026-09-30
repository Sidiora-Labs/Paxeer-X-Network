FROM rust:1.91.1-alpine3.22 AS build
WORKDIR /src
RUN apk add --no-cache musl-dev openssl-dev openssl-libs-static pkgconfig
COPY . .
RUN cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-indexer
RUN for dir in precompiles/*/; do \
		name="$(basename "$dir")"; \
		[ -f "$dir/abi.json" ] || continue; \
		mkdir -p "/abi/$name" && cp "$dir/abi.json" "/abi/$name/"; \
	done

FROM alpine:3.22 AS base
RUN apk add --no-cache ca-certificates libgcc openssl && addgroup -g 4020 layerx && adduser -D -H -u 4020 -G layerx layerx && \
	mkdir -p /usr/local/share/layerx && \
	openssl x509 -in /usr/share/ca-certificates/mozilla/ISRG_Root_X1.crt -outform DER -out /usr/local/share/layerx/isrg-root-x1.der
COPY --from=build /src/platform/target/release/layerx-indexer /usr/local/bin/
COPY --from=build /abi /usr/local/share/layerx/precompiles

FROM base AS fly
RUN apk add --no-cache setpriv curl jq sqlite
COPY --chmod=0555 platform/hosted/indexer/fly-init.sh /usr/local/bin/indexer-fly-init
ENTRYPOINT ["/usr/local/bin/indexer-fly-init"]
CMD ["serve"]

FROM base
USER 4020:4020
ENTRYPOINT ["/usr/local/bin/layerx-indexer"]
