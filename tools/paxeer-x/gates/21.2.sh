#!/usr/bin/env bash
set -euo pipefail
log=$(mktemp)
trap 'rm -f "$log"' EXIT
set +e
GOFLAGS="${GOFLAGS:-} -v" timeout 10m go test ./modules/layerxbridge/keeper -run '^TestBridgeOutReleaseCapBoundary$' -count=1 2>&1 | tee "$log"
result=${PIPESTATUS[0]}
set -e
if ((result != 0)); then exit "$result"; fi
python3 - "$log" <<'PY'
from pathlib import Path
import re
import sys
text = Path(sys.argv[1]).read_text()
if not re.search(r'^--- PASS: TestBridgeOutReleaseCapBoundary\s', text, re.M):
    raise SystemExit('required keeper case did not pass')
passed = len(re.findall(r'^\s*--- PASS:', text, re.M))
skipped = len(re.findall(r'^\s*--- SKIP:', text, re.M))
print(f'PAXEER_X_GATE tests={passed} skipped={skipped}')
if skipped:
    raise SystemExit('required keeper corpus was skipped')
PY
