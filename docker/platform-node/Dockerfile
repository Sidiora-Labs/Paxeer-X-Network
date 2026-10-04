ARG LXP_REVISION
FROM rust:1.91.1-bookworm@sha256:c1e5f19e773b7878c3f7a805dd00a495e747acbdc76fb2337a4ebf0418896b33 AS build
WORKDIR /src
ENV CARGO_BUILD_JOBS=4 MAKEFLAGS=-j4
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential libssl-dev libsqlite3-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
COPY . .
ARG LXP_REVISION
RUN printf '%s\n' "$LXP_REVISION" | grep -Eq '^[0-9a-f]{40}$' \
    && rustup show active-toolchain \
    && make LXP_REVISION="$LXP_REVISION" layerxd layerx-genesis-build layerx-handover layerxctl layerx-guarantor layerx-module-registry \
    && test -x build/bin/layerx-guarantor && test -x build/bin/layerxd && test -x build/bin/layerx-genesis-build \
    && test -x build/bin/layerx-handover && test -x build/bin/layerxctl && test -x build/bin/layerx-module-registry

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
ARG LXP_REVISION
LABEL org.opencontainers.image.revision="$LXP_REVISION"
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates libssl3 libsqlite3-0 openssl socat bash coreutils jq python3 python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 4020 layerx && useradd -M -u 4020 -g 4020 -s /usr/sbin/nologin layerx \
    && mkdir -p /opt/layerx/migrations /var/lib/layerx /run/layerx \
    && chown 4020:4020 /var/lib/layerx /run/layerx
COPY --from=build /src/build/bin/layerx-guarantor /usr/local/bin/layerx-guarantor
COPY --from=build /src/cmd/layerx-guarantor/settlement.py /opt/layerx/guarantor/settlement.py
COPY --from=build /src/cmd/layerx-guarantor/publication.py /opt/layerx/guarantor/publication.py
COPY --from=build /src/cmd/layerx-guarantor/authorization.py /opt/layerx/guarantor/authorization.py
COPY --from=build /src/platform/hosted/human/recipient_binding.py /opt/layerx/human/recipient_binding.py
COPY --from=build /src/platform/hosted/human/onboarding_socket.py /opt/layerx/human/onboarding_socket.py
COPY --from=build /src/platform/hosted/human/provision.py /opt/layerx/human/provision.py
COPY --from=build /src/platform/hosted/human/material.py /src/platform/hosted/human/provision.py /usr/local/lib/layerx-human/
COPY --from=build /src/cmd/layerx-guarantor/requirements.txt /opt/layerx/guarantor/requirements.txt
RUN python3 -m venv /opt/layerx/guarantor/venv \
    && /opt/layerx/guarantor/venv/bin/pip install --no-cache-dir -r /opt/layerx/guarantor/requirements.txt
COPY --from=build /src/build/bin/layerxd /usr/local/bin/layerxd
COPY --from=build /src/build/bin/layerx-genesis-build /usr/local/bin/layerx-genesis-build
COPY --from=build /src/build/bin/layerx-handover /usr/local/bin/layerx-handover
COPY --from=build /src/build/bin/layerxctl /usr/local/bin/layerxctl
COPY --from=build /src/build/bin/layerx-module-registry /usr/local/bin/layerx-module-registry
COPY --from=build /src/migrations/0007_history_index.sql /opt/layerx/migrations/0007_history_index.sql
COPY --from=build /src/contracts/config/checkpoint-settlement.json /opt/layerx/checkpoint-settlement.json
COPY --from=build /src/platform/hosted/node/bootstrap.sh /opt/layerx/bootstrap.sh
COPY --from=build /src/platform/hosted/node/genesis-modules.conf /opt/layerx/genesis-modules.conf
COPY --from=build /src/platform/hosted/node/genesis_fees.py /opt/layerx/genesis_fees.py
COPY --from=build /src/platform/hosted/node/genesis-module-fees.json /opt/layerx/genesis-module-fees.json
COPY --from=build /src/platform/hosted/node/data_directory.py /opt/layerx/data_directory.py
COPY --from=build /src/platform/hosted/node/supervisor.sh /opt/layerx/supervisor.sh
COPY --from=build /src/platform/hosted/node/reset_state.py /opt/layerx/reset_state.py
COPY --from=build /src/platform/hosted/node/guarantor.sh /opt/layerx/guarantor.sh
COPY --from=build /src/platform/hosted/node/checkpoint-authority.py /opt/layerx/checkpoint-authority.py
COPY --from=build /src/precompiles/layerxanchor/abi.json /opt/layerx/precompiles/layerxanchor/abi.json
COPY --from=build /src/precompiles/layerxcustody/abi.json /opt/layerx/precompiles/layerxcustody/abi.json
COPY --from=build /src/precompiles/addr/abi.json /opt/layerx/precompiles/addr/abi.json
COPY --from=build /src/platform/hosted/node/signer /opt/layerx/signer
RUN chmod 0755 /opt/layerx/bootstrap.sh /opt/layerx/supervisor.sh /opt/layerx/guarantor.sh
USER 4020:4020
ENTRYPOINT ["/opt/layerx/supervisor.sh"]
