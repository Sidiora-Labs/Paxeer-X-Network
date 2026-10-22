FROM rust:1.91.1-bookworm AS build
WORKDIR /src
COPY . .
RUN cargo build --release --locked --manifest-path services/platform/Cargo.toml -p layerx-oracle-feeder --bin price-sidecar

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/services/platform/target/release/price-sidecar /usr/local/bin/price-sidecar
ENTRYPOINT ["/usr/local/bin/price-sidecar"]
