#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$root"
target="${CARGO_TARGET_DIR:-/root/lx-target/interop}"
test_name=migration_v2_connected_runs_authenticated_mapping_and_funded_settlement
complete='MIGRATION_V2_CONNECTED_COMPLETE ethereum=1 solana=1 funded=2 skipped=0'
if [[ ! -d "$target/debug/deps" ]]; then
    echo 'migration-v2 gate: compiled test directory required' >&2
    echo 'PAXEER_X_GATE tests=0 skipped=0'
    exit 1
fi
shopt -s nullglob
candidates=()
for file in "$target"/debug/deps/migration_v2_connected-*; do
    if [[ -f "$file" && -x "$file" && ! -L "$file" && "${file##*/}" =~ ^migration_v2_connected-[0-9a-f]+$ ]]; then
        candidates+=("$file")
    fi
done
if ((${#candidates[@]} != 1)); then
    echo 'migration-v2 gate: exactly one genuine compiled test executable required' >&2
    echo 'PAXEER_X_GATE tests=0 skipped=0'
    exit 1
fi
umask 077
mkdir -p "$root/.logs/paxeer-x"
log="$(mktemp "$root/.logs/paxeer-x/migration-v2-connected.XXXXXX.log")"
trap 'gate_status=$?; echo "MIGRATION_V2_CONNECTED_GATE exit=$gate_status log=$log"' EXIT
set +e
"${candidates[0]}" "$test_name" --exact --nocapture --test-threads=1 >"$log" 2>&1
status=$?
set -e
cat "$log"
echo "MIGRATION_V2_CONNECTED_TEST exit=$status"
if ((status != 0)); then
    exit "$status"
fi
python3 - "$log" "$test_name" "$complete" <<'PY'
from pathlib import Path
import re
import sys

lines = Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
require = lambda condition: condition or sys.exit("migration-v2 gate: actual coverage refused")
require(sum(line == sys.argv[3] for line in lines) == 1)
require(sum(line == "running 1 test" for line in lines) == 1)
require(sum(re.fullmatch(r"test result: ok\. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in .+", line) is not None for line in lines) == 1)
require(sum("test " + sys.argv[2] + " ..." in line for line in lines) == 1)
records = [line for line in lines if line.startswith("PAXEER_X_GATE ")]
require(len(records) == 1)
match = re.fullmatch(r"PAXEER_X_GATE tests=([1-9][0-9]*) skipped=0", records[0])
require(match is not None)
PY
