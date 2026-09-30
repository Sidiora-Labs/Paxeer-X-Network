FROM rust:1.91.1-bookworm@sha256:c1e5f19e773b7878c3f7a805dd00a495e747acbdc76fb2337a4ebf0418896b33 AS build
RUN apt-get update \
    && apt-get install --yes --no-install-recommends libssl-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
ENV CARGO_BUILD_JOBS=4 CARGO_TARGET_DIR=/src/.gas-station-target
COPY . .
RUN cargo build --locked --manifest-path interop/Cargo.toml --release --package layerx-gas-station --bin paxeer-gas-station

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 4020 layerx \
    && useradd --system --uid 4020 --gid 4020 --home-dir /nonexistent --shell /usr/sbin/nologin layerx
COPY --from=build /src/.gas-station-target/release/paxeer-gas-station /usr/local/bin/paxeer-gas-station
COPY --chmod=0555 docker/gas-station/init.sh /usr/local/bin/gas-station-init
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gas-station-init"]
