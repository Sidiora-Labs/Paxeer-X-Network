#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 0 ]] || { echo '103.6.4 accepts no selector arguments' >&2; exit 2; }
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
native=${LAYERX_TEST_NATIVE_BIN_DIR:-$root/build/bin}
for binary in layerxd layerx-genesis-build; do
    [[ -x "$native/$binary" ]] || { echo "missing genuine native fixture: $native/$binary" >&2; exit 2; }
done
[[ $(id -u) -eq 0 ]] || { echo '103.6.4 requires the real root-owned fixture isolation' >&2; exit 2; }
[[ -n ${PAXEER_X_EVIDENCE_DIR:-} ]] || { echo 'PAXEER_X_EVIDENCE_DIR is required' >&2; exit 2; }
log="$PAXEER_X_EVIDENCE_DIR/103.6.4-contract.log"
native_log="$PAXEER_X_EVIDENCE_DIR/103.6.4-native.log"
lint_log="$PAXEER_X_EVIDENCE_DIR/103.6.4-clippy.log"
set +e
timeout 5m make --no-print-directory test-program-artifacts test-batch-wal-recovery >"$native_log" 2>&1
status=$?
set -e
cat "$native_log"
((status == 0)) || exit "$status"
set +e
timeout 20m cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-agent-boundary --all-targets -- --test-threads=1 >"$log" 2>&1
status=$?
set -e
cat "$log"
((status == 0)) || exit "$status"
set +e
timeout 5m cargo clippy --locked --manifest-path platform/Cargo.toml -p layerx-platform-agent-boundary --all-targets -- -D warnings >"$lint_log" 2>&1
status=$?
set -e
cat "$lint_log"
((status == 0)) || exit "$status"
python3 - "$log" <<'PY'
import re
import sys
from pathlib import Path
output = Path(sys.argv[1]).read_text()
required = {
    'real_node_boundary_serves_the_component_contract',
    'persisted_submission_attempt_is_not_repeated_after_connectivity_returns',
    'real_program_simulation_executes_without_committing',
    'malformed_program_call_is_refused_before_a_following_send',
    'real_program_call_refusal_artifacts_are_bound_and_replay_after_restart',
    'webhook_credential_configuration_refuses_shared_or_invalid_material',
    'registry_proof_forwarding_requires_its_plane_and_preserves_native_refusal',
    'real_readiness_requires_live_lni_and_bounds_saturated_sessions',
}
passed = set(re.findall(r'^test ([^\s]+) \.\.\. ok$', output, re.M))
missing = required - passed
if missing:
    raise SystemExit('required real contract cases absent: ' + ', '.join(sorted(missing)))
summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;[^\n]*$', output, re.M)
if not summaries or any(int(failed) or int(ignored) for _, failed, ignored in summaries):
    raise SystemExit('missing complete non-skipped task results')
count = sum(int(passed) for passed, _, _ in summaries)
if count < len(required):
    raise SystemExit('real contract corpus is incomplete')
print(f'PAXEER_X_GATE tests={count} skipped=0')
PY
