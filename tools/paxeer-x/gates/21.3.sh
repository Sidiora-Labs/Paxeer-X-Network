#!/usr/bin/env bash
set -euo pipefail
log=$(mktemp)
trap 'rm -f "$log"' EXIT
set +e
timeout 15m cargo test --locked --manifest-path interop/Cargo.toml -p layerx-bridge-relayer --test outbound_isolation 2>&1 | tee "$log"
result=${PIPESTATUS[0]}
set -e
if ((result != 0)); then exit "$result"; fi
python3 - "$log" <<'PY_GATE'
from pathlib import Path
import re
import sys
text = Path(sys.argv[1]).read_text()
required = 'outbound_isolation_captured_production_boundaries'
if not re.search(r'^test ' + required + r' \.\.\. ok$', text, re.M):
    raise SystemExit('required captured production boundary case did not pass')
if re.search(r'^test .* \.\.\. ignored', text, re.M):
    raise SystemExit('outbound boundary case was skipped')
print('PAXEER_X_GATE tests=1 skipped=0')
PY_GATE
