# The kernel app image (human/wallet/deploy/human.toml): the union of
# docker/platform-node, platform-core, platform-authority,
# platform-agent-boundary, paxeer, interop-mirror and human-service plus layerx-mcp,
# one machine running the node pod under docker/kernel/init.sh.
ARG NATIVE_RUNTIME_BASE=registry.fly.io/paxeer-human-service@sha256:8cd734a37546f94c66ae3efc2220513018a4a27c4a0938433f0a49ac126d141a
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
# The relay archive origin of docker/kernel/init.sh (task 4.6): its codec.
RUN make LXP_REVISION="$LXP_REVISION" layerx-archive-codec && test -x build/bin/layerx-archive-codec
# The value loop of tools/bringup/value-loop.sh (task 1.6): the node probe and the credit signer.
RUN make LXP_REVISION="$LXP_REVISION" build/tests/bridge/sign-credit && test -x build/tests/bridge/sign-credit
RUN cargo build --locked --release --manifest-path platform/hosted/node/tests/probe/Cargo.toml

# The light credit of tools/bringup/value-loop.sh and the light profile of
# tools/bringup/kernel-genesis.sh (task 1.6): the Go custody proof tool.
FROM docker.io/golang:1.25.6-bookworm@sha256:2f768d462dbffbb0f0b3a5171009f162945b086f326e0b2a8fd5d29c3219ff14 AS custody-proof
WORKDIR /src
COPY . .
RUN make custody-proof-build && test -x build/bin/layerx-custody-proof

FROM ${NATIVE_RUNTIME_BASE} AS native-runtime
ARG LXP_REVISION
LABEL org.opencontainers.image.revision="$LXP_REVISION" \
      io.layerx.kernel.profile="native"
ENV LAYERX_KERNEL_PROFILE=native
COPY --from=native-artifacts --chmod=0555 /bin/layerxd /usr/local/bin/layerxd
COPY --from=native-artifacts --chmod=0555 /bin/layerx-genesis-build /usr/local/bin/layerx-genesis-build
COPY --from=native-artifacts --chmod=0555 /bin/layerx-handover /usr/local/bin/layerx-handover
COPY --from=native-artifacts --chmod=0555 /bin/layerxctl /usr/local/bin/layerxctl
COPY --from=native-artifacts --chmod=0555 /bin/layerx-guarantor /usr/local/bin/layerx-guarantor
COPY --from=native-artifacts --chmod=0555 /bin/layerx-module-registry /usr/local/bin/layerx-module-registry
COPY --from=native-artifacts --chmod=0555 /bin/layerx-archive-codec /usr/local/bin/layerx-archive-codec
COPY --from=native-artifacts --chmod=0555 /bin/layerx-core-boundary /usr/local/bin/layerx-core-boundary
COPY --from=native-artifacts --chmod=0555 /bin/layerx-receipt-authority /usr/local/bin/layerx-receipt-authority
COPY --from=native-artifacts --chmod=0555 /bin/layerx-runtime-clock /usr/local/bin/layerx-runtime-clock
COPY --from=native-artifacts --chmod=0555 /bin/layerx-agent-boundary /usr/local/bin/layerx-agent-boundary
COPY --from=native-artifacts --chmod=0555 /bin/layerx-paxeer-boundary /usr/local/bin/layerx-paxeer-boundary
COPY --from=native-artifacts --chmod=0555 /bin/layerx-custody-proof /usr/local/bin/layerx-custody-proof
COPY --from=native-artifacts --chmod=0555 /bin/layerx-mirror-signer /usr/local/bin/layerx-mirror-signer
COPY --from=native-artifacts --chmod=0555 /bin/layerx-mirror-publisher /usr/local/bin/layerx-mirror-publisher
COPY --from=native-artifacts --chmod=0555 /bin/layerx-mirror-verify /usr/local/bin/layerx-mirror-verify
RUN --mount=type=bind,from=native-artifacts,target=/native-artifacts,ro \
    --mount=type=bind,source=.,target=/source,ro \
    python3 - "$LXP_REVISION" <<'PY_VERIFY'
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import struct
import subprocess
import sys


def refuse(message):
    raise SystemExit("native artifact manifest refused: " + message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            refuse("duplicate JSON field")
        result[key] = value
    return result


def admit_runtime_linkage(executable):
    size = executable.stat().st_size
    if size < 64 or size > 536870912:
        refuse("executable ELF size is outside bounds")
    with executable.open("rb") as stream:
        header = struct.unpack("<16sHHIQQQIHHHHHH", stream.read(64))
        ident, kind, machine, version = header[:4]
        if ident[:7] != b"\x7fELF\x02\x01\x01" or kind not in (2, 3) or machine != 62 or version != 1:
            refuse("executable must be a little-endian x86-64 ELF")
        table, entry_size, count = header[5], header[9], header[10]
        if header[8] != 64 or entry_size != 56 or not 1 <= count <= 1024:
            refuse("unsupported ELF program-header layout")
        if table < 64 or table + count * entry_size > size:
            refuse("ELF program-header table is outside the file")
        interpreter = None
        dynamic_segments = []
        for index in range(count):
            stream.seek(table + index * entry_size)
            segment = struct.unpack("<IIQQQQQQ", stream.read(entry_size))
            segment_kind, offset, length = segment[0], segment[2], segment[5]
            if offset > size or length > size - offset:
                refuse("ELF segment is outside the file")
            if segment_kind == 3:
                if interpreter is not None or not 2 <= length <= 4096:
                    refuse("invalid ELF interpreter segment")
                stream.seek(offset)
                value = stream.read(length)
                if value[-1:] != b"\0" or b"\0" in value[:-1]:
                    refuse("invalid ELF interpreter string")
                try:
                    interpreter = value[:-1].decode("ascii")
                except UnicodeDecodeError:
                    refuse("ELF interpreter is not ASCII")
            elif segment_kind == 2:
                if length == 0 or length % 16 or length > 1048576:
                    refuse("invalid ELF dynamic segment")
                dynamic_segments.append((offset, length))
        if len(dynamic_segments) > 1:
            refuse("multiple ELF dynamic segments")
        needed = False
        for offset, length in dynamic_segments:
            stream.seek(offset)
            terminated = False
            for _ in range(length // 16):
                tag, value = struct.unpack("<qQ", stream.read(16))
                if tag == 0:
                    terminated = True
                    break
                if tag == 1:
                    needed = True
            if not terminated:
                refuse("unterminated ELF dynamic segment")
        if interpreter is None:
            if needed:
                refuse("ELF dependencies have no runtime interpreter")
            return
        loader = "/lib64/ld-linux-x86-64.so.2"
        if interpreter != loader:
            refuse("ELF interpreter differs from the pinned runtime loader")
    try:
        result = subprocess.run(
            [loader, "--list", str(executable)],
            capture_output=True, text=True, timeout=30, check=False,
            env={"PATH": "/usr/bin:/bin", "LC_ALL": "C"},
        )
    except (OSError, subprocess.TimeoutExpired):
        refuse("runtime loader admission failed for " + executable.name)
    if result.returncode != 0 or result.stderr.strip() or "not found" in result.stdout:
        sys.stderr.write(result.stdout)
        sys.stderr.write(result.stderr)
        refuse("runtime dependency mismatch for " + executable.name)


revision = sys.argv[1]
if re.fullmatch(r"[0-9a-f]{40}", revision) is None or revision == "0" * 40:
    refuse("LXP_REVISION must identify the candidate source")
manifest_path = Path("/native-artifacts/manifest.json")
if not manifest_path.is_file() or manifest_path.is_symlink():
    refuse("manifest must be a regular file")
if manifest_path.stat().st_size > 33554432:
    refuse("manifest exceeds 32 MiB")
manifest = json.loads(manifest_path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
if not isinstance(manifest, dict) or manifest.get("schema") != "layerx.native-artifacts.v1":
    refuse("unsupported schema")
if manifest.get("candidate_revision") != revision:
    refuse("candidate revision mismatch")
required = {
    "layerxd", "layerx-genesis-build", "layerx-handover", "layerxctl",
    "layerx-guarantor", "layerx-module-registry", "layerx-archive-codec",
    "layerx-core-boundary", "layerx-receipt-authority", "layerx-runtime-clock",
    "layerx-agent-boundary", "layerx-paxeer-boundary", "layerx-custody-proof",
    "layerx-mirror-signer", "layerx-mirror-publisher", "layerx-mirror-verify",
}
artifacts = manifest.get("artifacts")
if not isinstance(artifacts, dict) or set(artifacts) != required:
    refuse("artifact set differs from the native profile")
source_root = Path("/source").resolve()
verified_sources = {}
for name, record in artifacts.items():
    if not isinstance(record, dict):
        refuse("invalid artifact record")
    expected = record.get("sha256")
    producer = record.get("producer_revision")
    if not isinstance(expected, str) or re.fullmatch(r"[0-9a-f]{64}", expected) is None:
        refuse("invalid executable digest")
    if not isinstance(producer, str) or re.fullmatch(r"[0-9a-f]{40}", producer) is None or producer == "0" * 40:
        refuse("missing original producer revision")
    executable = Path("/usr/local/bin") / name
    if not executable.is_file() or executable.is_symlink() or not os.access(executable, os.X_OK):
        refuse("missing regular executable " + name)
    with executable.open("rb") as stream:
        actual = hashlib.file_digest(stream, "sha256").hexdigest()
    if actual != expected:
        refuse("executable digest mismatch for " + name)
    admit_runtime_linkage(executable)
    inputs = record.get("source_inputs")
    if not isinstance(inputs, dict) or not inputs:
        refuse("missing producer source closure for " + name)
    for relative, digest in inputs.items():
        path = PurePosixPath(relative)
        if path.is_absolute() or not path.parts or any(part in (".", "..") for part in path.parts) or str(path) != relative:
            refuse("invalid source input path")
        if not isinstance(digest, str) or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
            refuse("invalid source input digest")
        source = source_root / path
        if source.is_symlink() or not source.is_file() or not source.resolve().is_relative_to(source_root):
            refuse("source input is not a regular candidate file")
        if relative not in verified_sources:
            with source.open("rb") as stream:
                verified_sources[relative] = hashlib.file_digest(stream, "sha256").hexdigest()
        if verified_sources[relative] != digest:
            refuse("producer source differs from candidate for " + name)
PY_VERIFY
COPY cmd/layerx-guarantor/settlement.py cmd/layerx-guarantor/publication.py \
     cmd/layerx-guarantor/authorization.py /opt/layerx/guarantor/
COPY platform/hosted/human/recipient_binding.py platform/hosted/human/onboarding_socket.py \
     platform/hosted/human/provision.py /opt/layerx/human/
COPY migrations/0007_history_index.sql /opt/layerx/migrations/0007_history_index.sql
COPY contracts/config/checkpoint-settlement.json /opt/layerx/checkpoint-settlement.json
COPY platform/hosted/node/bootstrap.sh platform/hosted/node/supervisor.sh \
     platform/hosted/node/sequencer-env.sh platform/hosted/node/genesis-modules.conf \
     platform/hosted/node/genesis_fees.py platform/hosted/node/genesis-module-fees.json \
     platform/hosted/node/data_directory.py platform/hosted/node/guarantor.sh \
     platform/hosted/node/checkpoint-authority.py /opt/layerx/
COPY precompiles/layerxanchor/abi.json /opt/layerx/precompiles/layerxanchor/abi.json
COPY platform/hosted/node/signer /opt/layerx/signer
COPY platform/hosted/paxeer/evm.py /opt/layerx/paxeer/evm.py
COPY platform/hosted/tests/publication-policy.py /opt/layerx/publication-policy.py
COPY platform/relay_archive/__init__.py platform/relay_archive/runtime.py \
     platform/relay_archive/store.py platform/relay_archive/protocol.py \
     platform/relay_archive/forward.py platform/relay_archive/peers.py /opt/layerx/relay_archive/
COPY --chmod=0555 platform/relay_archive/install.sh /opt/layerx/relay_archive/install.sh
COPY --chmod=0555 docker/kernel/init.sh /usr/local/bin/kernel-init
RUN chmod 0555 /opt/layerx/bootstrap.sh /opt/layerx/supervisor.sh /opt/layerx/guarantor.sh \
    && rm -f /usr/local/bin/layerx-node-probe /usr/local/bin/sign-credit \
        /usr/local/bin/layerx-human-service /usr/local/bin/layerx-human-components \
        /usr/local/bin/layerx-human-onboarding /usr/local/bin/layerx-human-kms \
        /usr/local/bin/layerx-explorer-index /usr/local/bin/layerx-human-identity-provider \
        /usr/local/bin/layerx-human-security-provider /usr/local/bin/layerx-human-movement-provider \
        /usr/local/bin/layerx-agentd /usr/local/bin/layerx-mcp /usr/local/bin/human-entrypoint
ENTRYPOINT ["/usr/local/bin/kernel-init"]

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
# The relay archive origin of docker/kernel/init.sh (task 4.6): the codec, the
# runtime modules and install.sh --config-only that renders its config.
COPY --from=build /src/build/bin/layerx-archive-codec /usr/local/bin/layerx-archive-codec
COPY --from=build /src/platform/relay_archive/__init__.py /src/platform/relay_archive/runtime.py \
     /src/platform/relay_archive/store.py /src/platform/relay_archive/protocol.py \
     /src/platform/relay_archive/forward.py /src/platform/relay_archive/peers.py /opt/layerx/relay_archive/
COPY --from=build --chmod=0555 /src/platform/relay_archive/install.sh /opt/layerx/relay_archive/install.sh
# The value loop of tools/bringup/value-loop.sh (task 1.6).
COPY --from=build /src/platform/hosted/node/tests/probe/target/release/layerx-node-probe \
     /src/build/tests/bridge/sign-credit /usr/local/bin/
COPY --from=custody-proof /src/build/bin/layerx-custody-proof /usr/local/bin/layerx-custody-proof
COPY --chmod=0555 docker/kernel/init.sh /usr/local/bin/kernel-init
ENTRYPOINT ["/usr/local/bin/kernel-init"]
