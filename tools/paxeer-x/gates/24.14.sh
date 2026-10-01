#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
: "${PAXEER_X_RUNTIME_EVIDENCE:?private evidence directory required}"
case "${1:-verify}" in
build)
    : "${PAXEER_X_RUNTIME_NATIVE_LIBRARY:?source-bound native archive required}"
    : "${PAXEER_X_RUNTIME_NATIVE_REVISION:?archive producer revision required}"
    : "${PAXEER_X_RUNTIME_PROGRAMS_LIBRARY:?source-bound Programs archive required}"
    mkdir -p "$PAXEER_X_RUNTIME_EVIDENCE"
    chmod 0700 "$PAXEER_X_RUNTIME_EVIDENCE"
    [[ -z $(git status --porcelain) ]] || { echo 'refuse dirty source' >&2; exit 1; }
    [[ -z $(git diff --name-only "$PAXEER_X_RUNTIME_NATIVE_REVISION" HEAD -- src include programs cmd/layerxd) ]] || { echo 'native archive source differs' >&2; exit 1; }
    cc -std=c17 -O2 -ffunction-sections -fdata-sections -Iinclude -Itests/daemon \
        tests/daemon/lxp_test_runtime_fixture.c -Wl,--gc-sections -Wl,--start-group \
        "$PAXEER_X_RUNTIME_NATIVE_LIBRARY" "$PAXEER_X_RUNTIME_PROGRAMS_LIBRARY" -Wl,--end-group \
        -lcrypto -lsqlite3 -pthread -ldl -lm -o "$PAXEER_X_RUNTIME_EVIDENCE/runtime-client"
    python3 - "$PAXEER_X_RUNTIME_EVIDENCE" "$PAXEER_X_RUNTIME_NATIVE_REVISION" <<'PY'
import hashlib, json, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1]); binary = root / 'runtime-client'
value = {'version': 1, 'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
         'native_source_revision': sys.argv[2], 'path': str(binary.resolve()), 'sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}
(root / 'client-manifest.json').write_text(json.dumps(value) + '\n')
(root / 'client-manifest.json').chmod(0o600)
PY
    ;;
verify)
    exec timeout 20m python3 tests/daemon/paxeer_x_runtime_fixture_test.py
    ;;
*) echo 'usage: 24.14.sh build|verify' >&2; exit 2 ;;
esac
