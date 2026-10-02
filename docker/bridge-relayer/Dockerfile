FROM rust@sha256:c1e5f19e773b7878c3f7a805dd00a495e747acbdc76fb2337a4ebf0418896b33 AS build
RUN apt-get update \
    && apt-get install --yes --no-install-recommends libssl-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
ENV CARGO_BUILD_JOBS=2 CARGO_TARGET_DIR=/src/.lane-target
ENV PATH=/usr/local/rustup/toolchains/1.91.1-x86_64-unknown-linux-gnu/bin:/usr/local/cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV RUSTC=/usr/local/rustup/toolchains/1.91.1-x86_64-unknown-linux-gnu/bin/rustc
COPY . .
RUN cargo build --locked --manifest-path interop/Cargo.toml --release --package layerx-bridge-relayer --bin layerx-bridge-relayer --bin layerx-bridge-cosign
RUN cargo build --locked --manifest-path interop/Cargo.toml --release --package layerx-mirror-signer --bin layerx-mirror-signer

FROM debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -m 0755 /usr/share/layerx/roots \
    && for pem in /usr/share/ca-certificates/mozilla/*.crt; do \
        name=$(basename "$pem" .crt); \
        openssl x509 -in "$pem" -outform DER -out "/usr/share/layerx/roots/$name.der"; \
    done \
    && groupadd --system --gid 4100 bridge \
    && useradd --system --uid 4101 --gid 4100 --home-dir /nonexistent --shell /usr/sbin/nologin bridge-signer \
    && groupadd --system --gid 4102 bridge-relayer \
    && useradd --system --uid 4102 --gid 4102 --groups bridge --home-dir /nonexistent --shell /usr/sbin/nologin bridge-relayer
COPY --from=build /src/.lane-target/release/layerx-bridge-relayer /usr/local/bin/layerx-bridge-relayer
COPY --from=build /src/.lane-target/release/layerx-bridge-cosign /usr/local/bin/layerx-bridge-cosign
COPY --from=build /src/.lane-target/release/layerx-mirror-signer /usr/local/bin/layerx-mirror-signer
COPY interop/deploy/bridge-relayer/signer-keys.json /etc/layerx/bridge-signer/keys.json
COPY docker/bridge-relayer/entrypoint.sh /usr/local/bin/bridge-relayer-entrypoint
ENTRYPOINT ["/usr/local/bin/bridge-relayer-entrypoint"]
