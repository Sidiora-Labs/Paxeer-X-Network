#!/usr/bin/env bash
# Sandbox snapshot and restore under the renter's authority: structural checks on the
# production snapshot path, every layerx-programs-sandbox test target with the host
# lifecycle surface enabled, then strict clippy over the crate. The aggregate
# make programs-test matrix stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)
source_file=programs/crates/layerx-programs-sandbox/src/snapshot.rs
suite=programs/crates/layerx-programs-sandbox/tests/snapshot.rs

structure=0
for needle in \
    'hash_bytes(HashAlgorithm::Sha256, &bytes)' \
    '.commit_snapshot_storage(candidate_storage, storage_bytes)' \
    '.charge_storage_write(' \
    'return Err(SnapshotRefusal::NotSnapshotOwner);' \
    'return Err(SnapshotRefusal::DigestMismatch);' \
    'live_namespace_cells(&candidate_storage, target.namespace())? != supplied.namespace_cells'; do
    if ! grep -qF -- "$needle" "$source_file"; then
        echo "snapshot path is missing: $needle" >&2
        structure=1
    fi
done
validate_line=$(grep -n 'validate_restore(' "$source_file" | head -n 1 | cut -d: -f1)
activate_line=$(grep -n '\.transition(activation, evidence)' "$source_file" | head -n 1 | cut -d: -f1)
if [[ -z $validate_line || -z $activate_line ]] || ((validate_line >= activate_line)); then
    echo "restore does not verify the supplied state before activating the target lease" >&2
    structure=1
fi
((structure == 0)) && echo "structure: digest-bound commit, metered persistence and restore, owner and digest refusals, verify-before-activate, namespace read-back verified"

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.6-test.log" 2>&1
test_code=$?
cat "$logs/104.34.6-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.6-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.6-clippy.log"

output=$(<"$logs/104.34.6-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

mapfile -t required < <(sed -n '/^#\[test\]$/{n;s/^fn \([a-z0-9_]*\)(.*/\1/p}' "$suite")
missing=0
if ((${#required[@]} < 5)); then
    echo "snapshot suite declares ${#required[@]} tests, expected at least 5" >&2
    missing=1
fi
for name in "${required[@]}" lease::tests::public_transition_refuses_activities_owned_by_their_dedicated_paths; do
    if ! grep -qxF "test $name ... ok" <<<"$output"; then
        echo "required test did not pass: $name" >&2
        missing=1
    fi
done

status=0
((structure == 0)) || status=1
((test_code == 0)) || { echo "cargo test exited $test_code" >&2; status=1; }
((clippy_code == 0)) || { echo "cargo clippy exited $clippy_code" >&2; status=1; }
((missing == 0)) || status=1
((tests >= ${#required[@]})) || { echo "executed $tests tests, expected at least ${#required[@]}" >&2; status=1; }
((skipped == 0)) || { echo "$skipped tests were ignored" >&2; status=1; }
echo "aggregate make programs-test remains release scope; not run or credited here"
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$status"
