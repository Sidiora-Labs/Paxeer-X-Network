# The kernel app image (human/wallet/deploy/human.toml): the union of
# docker/platform-node, platform-core, platform-authority,
# platform-agent-boundary, paxeer, interop-mirror and human-service plus layerx-mcp,
# one machine running the node pod under docker/kernel/init.sh.
FROM rust:1.91.1-bookworm@sha256:c1e5f19e773b7878c3f7a805dd00a495e747acbdc76fb2337a4ebf0418896b33 AS build
WORKDIR /src
ARG CARGO_BUILD_JOBS=4
ENV CARGO_BUILD_JOBS=${CARGO_BUILD_JOBS} MAKEFLAGS=-j${CARGO_BUILD_JOBS}
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential libssl-dev libsqlite3-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
COPY . .
ARG LXP_REVISION=unknown
RUN rustup show active-toolchain \
    && make LXP_REVISION="$LXP_REVISION" layerxd layerx-genesis-build layerx-handover layerxctl layerx-guarantor layerx-module-registry \
    && test -x build/bin/layerx-guarantor && test -x build/bin/layerxd && test -x build/bin/layerx-genesis-build \
    && test -x build/bin/layerx-handover && test -x build/bin/layerxctl && test -x build/bin/layerx-module-registry
RUN cargo build --locked --release --manifest-path platform/Cargo.toml \
        -p layerx-platform-core -p layerx-platform-authority -p layerx-runtime-clock -p layerx-platform-agent-boundary \
        -p layerx-platform-paxeer-boundary
RUN CARGO_TARGET_DIR=/src/.lane-target cargo build --locked --manifest-path interop/Cargo.toml --release \
        --package layerx-mirror --bin layerx-mirror-publisher --bin layerx-mirror-verify \
    && CARGO_TARGET_DIR=/src/.lane-target cargo build --locked --manifest-path interop/Cargo.toml --release \
        --package layerx-mirror-signer --bin layerx-mirror-signer
RUN cargo build --locked --release --manifest-path human/Cargo.toml -p layerx-human-service -p layerx-human-kms -p layerx-explorer-index --bins
RUN cargo build --locked --release --manifest-path agent/Cargo.toml -p layerx-agentd --bin layerx-agentd -p layerx-mcp --bin layerx-mcp
RUN cargo build --locked --release --manifest-path human/Cargo.toml -p layerx-human-identity-provider -p layerx-human-security-provider -p layerx-human-movement-provider --bins

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates libssl3 libsqlite3-0 openssl socat bash coreutils jq curl util-linux \
        python3 python3-venv python3-cryptography \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 4020 layerx && useradd -M -u 4020 -g 4020 -s /usr/sbin/nologin layerx \
    && mkdir -p /opt/layerx/migrations /opt/layerx/paxeer /etc/layerx/trust
RUN setpriv --help | grep -- --pdeathsig
COPY --from=build /src/cmd/layerx-guarantor/requirements.txt /opt/layerx/guarantor/requirements.txt
RUN python3 -m venv /opt/layerx/guarantor/venv \
    && /opt/layerx/guarantor/venv/bin/pip install --no-cache-dir -r /opt/layerx/guarantor/requirements.txt
COPY --from=build /src/build/bin/layerx-guarantor /src/build/bin/layerxd /src/build/bin/layerx-genesis-build \
     /src/build/bin/layerx-handover /src/build/bin/layerx-module-registry /usr/local/bin/
COPY --from=build /src/cmd/layerx-guarantor/settlement.py /src/cmd/layerx-guarantor/publication.py \
     /src/cmd/layerx-guarantor/authorization.py /opt/layerx/guarantor/
COPY --from=build /src/platform/hosted/human/recipient_binding.py /src/platform/hosted/human/onboarding_socket.py \
     /src/platform/hosted/human/provision.py /opt/layerx/human/
COPY --from=build /src/migrations/0007_history_index.sql /opt/layerx/migrations/0007_history_index.sql
COPY --from=build /src/contracts/config/checkpoint-settlement.json /opt/layerx/checkpoint-settlement.json
COPY --from=build /src/platform/hosted/node/bootstrap.sh /src/platform/hosted/node/genesis-modules.conf \
     /src/platform/hosted/node/genesis_fees.py /src/platform/hosted/node/genesis-module-fees.json \
     /src/platform/hosted/node/data_directory.py /src/platform/hosted/node/supervisor.sh \
     /src/platform/hosted/node/guarantor.sh /src/platform/hosted/node/checkpoint-authority.py /opt/layerx/
COPY --from=build /src/precompiles/layerxanchor/abi.json /opt/layerx/precompiles/layerxanchor/abi.json
COPY --from=build /src/platform/hosted/node/signer /opt/layerx/signer
COPY --from=build /src/platform/hosted/paxeer/evm.py /opt/layerx/paxeer/evm.py
COPY --from=build /src/platform/hosted/tests/publication-policy.py /opt/layerx/publication-policy.py
RUN chmod 0755 /opt/layerx/bootstrap.sh /opt/layerx/supervisor.sh /opt/layerx/guarantor.sh
COPY --from=build /src/platform/target/release/layerx-core-boundary /src/platform/target/release/layerx-receipt-authority \
     /src/platform/target/release/layerx-runtime-clock /src/platform/target/release/layerx-agent-boundary \
     /src/platform/target/release/layerx-paxeer-boundary /usr/local/bin/
COPY --from=build /src/.lane-target/release/layerx-mirror-publisher /src/.lane-target/release/layerx-mirror-verify \
     /src/.lane-target/release/layerx-mirror-signer /usr/local/bin/
COPY --from=build /src/human/target/release/layerx-human-service /src/human/target/release/layerx-human-components \
     /src/human/target/release/layerx-human-onboarding /src/human/target/release/layerx-human-kms \
     /src/human/target/release/layerx-explorer-index /src/human/target/release/layerx-human-identity-provider \
     /src/human/target/release/layerx-human-security-provider /src/human/target/release/layerx-human-movement-provider \
     /src/agent/target/release/layerx-agentd /src/agent/target/release/layerx-mcp /usr/local/bin/
COPY --from=build /src/cmd/layerxctl/target/release/layerxctl /usr/local/bin/layerxctl
COPY platform/hosted/human/provision.py platform/hosted/human/owner_native.py platform/hosted/human/owner_kms.py \
     platform/hosted/human/onboarding_socket.py platform/hosted/human/onboarding_bootstrap.py \
     platform/hosted/human/onboarding_material.py platform/hosted/human/material.py \
     platform/hosted/human/recipient_binding.py platform/hosted/human/recipient_check.py /usr/local/lib/layerx-human/
COPY --chmod=0555 docker/human-service/entrypoint.sh /usr/local/bin/human-entrypoint
COPY --chmod=0555 docker/kernel/init.sh /usr/local/bin/kernel-init
ENTRYPOINT ["/usr/local/bin/kernel-init"]
