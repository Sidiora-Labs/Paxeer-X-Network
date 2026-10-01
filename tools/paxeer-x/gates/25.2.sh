#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
log=$(mktemp) || exit 1
trap 'rm -f "$log"' EXIT
GOFLAGS="${GOFLAGS:-} -json" timeout 10m go test ./tools/flyci/controller -run '^TestReconcileActualAssignment' -count=1 >"$log" 2>&1
code=$?
cat "$log"
python3 - "$log" <<'PY'
import json, sys
passed, skipped = set(), set()
for line in open(sys.argv[1]):
    try:
        event = json.loads(line)
    except ValueError:
        continue
    name = event.get('Test', '')
    if name.startswith('TestReconcileActualAssignment') and '/' not in name:
        if event.get('Action') == 'pass':
            passed.add(name)
        elif event.get('Action') == 'skip':
            skipped.add(name)
print(f'PAXEER_X_GATE tests={len(passed)} skipped={len(skipped)}')
PY
exit "$code"
