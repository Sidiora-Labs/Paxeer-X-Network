FROM rust:1.91.1-bookworm AS build
WORKDIR /src
COPY . .
RUN cargo build --release --locked --manifest-path platform/Cargo.toml -p layerx-oracle-feeder

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/platform/target/release/layerx-oracle-feeder /usr/local/bin/layerx-oracle-feeder
ENTRYPOINT ["/usr/local/bin/layerx-oracle-feeder"]
