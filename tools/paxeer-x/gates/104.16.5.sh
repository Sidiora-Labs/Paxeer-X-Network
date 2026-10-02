#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
fail() {
    echo "104.16.5: $1" >&2
    echo "PAXEER_X_GATE tests=0 skipped=0"
    exit "${2:-1}"
}
if [ -n "$(git status --porcelain=v1 --untracked-files=normal)" ]; then
    fail "source tree is dirty"
fi
for name in CARGO_TARGET_DIR PAXEER_X_EVIDENCE_DIR PAXEER_X_PACKAGE_SNAPSHOT PAXEER_X_RELEASE_SOURCE_DIGEST PAXEER_X_NPM_REGISTRY; do
    [ -n "${!name:-}" ] || fail "$name is not set"
done
[ -d "$PAXEER_X_PACKAGE_SNAPSHOT" ] || fail "published package snapshot $PAXEER_X_PACKAGE_SNAPSHOT is missing"
[ -d "$PAXEER_X_EVIDENCE_DIR" ] || fail "evidence directory $PAXEER_X_EVIDENCE_DIR is missing"
bin="$CARGO_TARGET_DIR/release/layerx-platform-benchmark"
[ -f "$bin" ] && [ -x "$bin" ] || fail "prebuilt benchmark $bin is missing"
built=$(stat -c %Y "$bin")
[ "$built" -ge "$(git log -1 --format=%ct HEAD)" ] || fail "prebuilt benchmark is older than HEAD"
for source in platform/tools/benchmark/src/main.rs platform/tools/benchmark/Cargo.toml platform/Cargo.toml platform/Cargo.lock; do
    [ "$built" -ge "$(stat -c %Y "$source")" ] || fail "prebuilt benchmark is older than $source"
done
revision=$(git rev-parse --verify 'HEAD^{commit}')
evidence=$(mktemp -d "$PAXEER_X_EVIDENCE_DIR/104.16.5-${revision:0:12}.XXXXXX")
out="$evidence/ten-line-benchmark.json"
set +e
"$bin" --repo-root "$PWD" --snapshot "$PAXEER_X_PACKAGE_SNAPSHOT" \
    --release-source-digest "$PAXEER_X_RELEASE_SOURCE_DIGEST" --registry "$PAXEER_X_NPM_REGISTRY" --out "$out"
code=$?
set -e
echo "benchmark exit $code; evidence $evidence"
exec python3 - "$out" "$code" <<'PY'
import json
import sys

out, code = sys.argv[1], int(sys.argv[2])
try:
    with open(out, encoding='utf-8') as stream:
        record = json.load(stream)
except FileNotFoundError:
    record = None
except ValueError:
    print('104.16.5: benchmark artifact is malformed', file=sys.stderr)
    print('PAXEER_X_GATE tests=0 skipped=0')
    sys.exit(1)
measured = []
if isinstance(record, dict):
    for side in ('seller', 'buyer'):
        entry = record.get(side)
        if isinstance(entry, dict) and type(entry.get('lines')) is int and entry.get('sha256') and entry.get('packages'):
            measured.append((side, entry['lines']))
            print(side + ' ' + str(entry.get('source')) + ' lines=' + str(entry['lines']))
print('PAXEER_X_GATE tests=' + str(len(measured)) + ' skipped=0')
if code:
    print('104.16.5: benchmark failed with exit ' + str(code), file=sys.stderr)
    sys.exit(code)
if (record is None or len(measured) != 2 or record.get('threshold') != 10 or record.get('passed') is not True
        or any(lines > 10 for _, lines in measured)):
    print('104.16.5: benchmark passed without a consistent two-sided artifact', file=sys.stderr)
    sys.exit(1)
PY
