#!/usr/bin/env bash
# Task 2.2 gate: recovered finality against authenticated historical guarantor membership, driven live.
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
: "${PAXEER_X_EVIDENCE_DIR:?private evidence directory required}"
case "${1:-verify}" in
build)
    [[ -z $(git status --porcelain) ]] || { echo 'refuse dirty source' >&2; exit 7; }
    mkdir -p "$PAXEER_X_EVIDENCE_DIR"
    chmod 0700 "$PAXEER_X_EVIDENCE_DIR"
    python3 - "$PAXEER_X_EVIDENCE_DIR" <<'PY'
import hashlib, json, os, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
artifacts = {}
for name, path in (('layerx-guarantor', 'build/bin/layerx-guarantor'),
                   ('lxp_test_daemon_finality_authority', 'build/tests/lxp_test_daemon_finality_authority')):
    target = pathlib.Path(path).resolve()
    if not target.is_file() or not os.access(target, os.X_OK):
        raise SystemExit('missing built executable ' + path)
    artifacts[name] = {'path': str(target), 'sha256': hashlib.sha256(target.read_bytes()).hexdigest()}
manifest = root / 'finality-supplemental.json'
manifest.unlink(missing_ok=True)
with manifest.open('x') as stream:
    json.dump({'version': 1, 'source_revision': revision, 'artifacts': artifacts}, stream, sort_keys=True)
    stream.write('\n')
manifest.chmod(0o600)
print('PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST=' + str(manifest))
PY
    ;;
verify)
    : "${PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST:=$PAXEER_X_EVIDENCE_DIR/finality-supplemental.json}"
    export PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST
    exec timeout 1800s env PYTHONDONTWRITEBYTECODE=1 python3 tests/daemon/paxeer_x_historical_finality.py
    ;;
*) echo 'usage: 2.2.sh build|verify' >&2; exit 2 ;;
esac
