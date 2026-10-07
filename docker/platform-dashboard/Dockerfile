FROM rust:1.91.1-bookworm AS build
WORKDIR /src
COPY . .
RUN cargo build --manifest-path platform/Cargo.toml --locked --release -p layerx-platform-dashboard --bin layerx-dashboard

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/platform/target/release/layerx-dashboard /usr/local/bin/layerx-dashboard
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/platform-dashboard/files.tsv /etc/layerx/files.tsv
RUN install -d -o 65532 -g 65532 -m 0700 /run/layerx
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/layerx-dashboard"]
