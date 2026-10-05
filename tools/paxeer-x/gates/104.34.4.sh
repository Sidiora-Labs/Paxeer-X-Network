#!/usr/bin/env bash
# Incremental usage settlement: every layerx-programs-sandbox test target with the
# host settlement surface enabled, requiring the per-activity debit, offline receipt,
# failed-work charge and long-lease conservation tests by name, then strict clippy
# over the crate. The aggregate make programs-test matrix stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.4-test.log" 2>&1
test_code=$?
cat "$logs/104.34.4-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.4-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.4-clippy.log"

output=$(<"$logs/104.34.4-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    usage::tests::incremental::each_activity_debits_the_escrow_in_its_own_settlement_and_publishes_the_running_total
    usage::tests::incremental::failed_activity_charges_the_work_performed_before_failure_and_nothing_beyond
    usage::tests::incremental::exhausted_activity_charges_only_the_work_metered_before_its_ceiling
    usage::tests::incremental::long_lease_usage_receipts_sum_exactly_to_the_escrow_debit
    usage::tests::incremental::refused_settlement_leaves_usage_escrow_and_ledger_exactly_as_committed
    usage::tests::long_lease_receipt_chain_conserves_every_charge
    usage::tests::active_at_expiry_settles_exact_occupancy_and_exhausted_escrow
    escrow::tests::escrow_exhaustion_mid_execution_stops_every_later_charge
    execute::tests::sandbox_call_is_ordinary_metered_execution_charged_to_the_lease
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
