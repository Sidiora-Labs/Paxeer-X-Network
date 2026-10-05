#!/usr/bin/env bash
# Sandbox lease escrow: every layerx-programs-sandbox test target with the host
# lifecycle surface enabled, the program-derived escrow funding, ceiling, refund and
# conservation tests required by name, then strict clippy over the crate. The aggregate
# make programs-test matrix stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.2-test.log" 2>&1
test_code=$?
cat "$logs/104.34.2-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.2-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.2-clippy.log"

output=$(<"$logs/104.34.2-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    escrow::tests::funding_lands_in_the_host_derived_account_and_unfunded_leases_never_execute
    escrow::tests::escrow_exhaustion_mid_execution_stops_every_later_charge
    escrow::tests::zero_usage_lease_refunds_the_entire_escrow_through_one_transfer
    escrow::tests::refund_attempted_twice_is_refused_and_leaves_conservation_intact
    escrow::tests::exhaustion_refuses_before_execution
    escrow::tests::underpayment_and_overpayment_are_refused_before_kernel_commit
    escrow::tests::zero_usage_conserves_the_whole_refund
    escrow::tests::repeated_refund_is_refused_by_terminal_commitment
    escrow::tests::canonical_state_preserves_conservation_and_replay_marker
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
