#!/usr/bin/env bash
# Sandbox teardown: every layerx-programs-sandbox test target with the host settlement
# surface enabled, requiring the protocol-swept destruction tests (full escrow, exhausted
# escrow, active at expiry, multi-batch cohort, finality, refusal atomicity and host parity)
# by name, then strict clippy over the crate. The aggregate make programs-test matrix
# stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.5-test.log" 2>&1
test_code=$?
cat "$logs/104.34.5-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.5-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.5-clippy.log"

output=$(<"$logs/104.34.5-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    expiry::teardown::full_escrow_destruction_drops_the_namespace_and_refunds_every_unit
    expiry::teardown::exhausted_escrow_destruction_refunds_nothing_and_refuses_a_refund_root
    expiry::teardown::active_at_expiry_destruction_settles_final_occupancy_before_refund
    expiry::teardown::cohort_spanning_several_batches_is_destroyed_deterministically_with_carry_forward
    expiry::teardown::destroyed_lease_is_final_and_only_its_terminal_record_is_readable
    expiry::teardown::refused_teardown_leaves_state_storage_and_meter_untouched
    expiry::teardown::host_teardown_and_storage_teardown_settle_identical_protocol_state
    expiry::source_cases::cohort_is_ordered_bounded_and_carried_across_batches
    expiry::source_cases::queue_refuses_duplicate_and_unbounded_sweep_admission
    lease::tests::destroyed_lease_cannot_be_revived_by_any_activity_path_or_forged_state
    usage::tests::active_at_expiry_settles_exact_occupancy_and_exhausted_escrow
    usage::tests::untouched_full_escrow_has_zero_final_usage_and_full_refund
)
missing=0
for name in "${required[@]}"; do
    if ! grep -qxF "test $name ... ok" <<<"$output"; then
        echo "required test did not pass: $name" >&2
        missing=1
    fi
done

status=0
((test_code == 0)) || { echo "cargo test exited $test_code" >&2; status=1; }
((clippy_code == 0)) || { echo "cargo clippy exited $clippy_code" >&2; status=1; }
((missing == 0)) || status=1
((tests >= ${#required[@]})) || { echo "executed $tests tests, expected at least ${#required[@]}" >&2; status=1; }
((skipped == 0)) || { echo "$skipped tests were ignored" >&2; status=1; }
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$status"
