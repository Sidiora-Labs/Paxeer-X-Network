#!/bin/sh
set -eu
if [ -z "${PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS:-}" ]; then
    echo "actual native terminal-v5 corpus missing" >&2
    exit 78
fi
python3 - "$PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS" <<'CORPUS'
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as source:
        corpus = json.load(source)
    required = {(abi, outcome) for abi in (3, 4)
                for outcome in ("success", "failure", "resource", "callback", "settlement")}
    observed = {(row["guest_abi"], row["outcome"]) for row in corpus["cases"]}
    if observed != required:
        print("actual native terminal-v5 corpus lacks required ten outcome cases", file=sys.stderr)
        sys.exit(78)
except (OSError, ValueError, KeyError, TypeError):
    print("actual native terminal-v5 corpus unavailable or malformed", file=sys.stderr)
    sys.exit(78)
CORPUS
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
exec mvn -o -q -f "$repo_root/platform/sdk/jvm/pom.xml" -Pconformance surefire:test \
    -Dtest=ProgramsContractTest -DfailIfNoTests=true -Dlayerx.repo.root="$repo_root"
