#!/usr/bin/env bash
# Sandbox lease protocol state: every layerx-programs-sandbox test target with the
# host lifecycle surface enabled, then strict clippy over the crate. The aggregate
# make programs-test matrix stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.1-test.log" 2>&1
test_code=$?
cat "$logs/104.34.1-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.1-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.1-clippy.log"

output=$(<"$logs/104.34.1-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    lease::tests::namespace_is_deterministic_isolated_and_resource_excess_is_typed
    lease::tests::fee_schedule_is_frozen_in_canonical_lease_state
    lease::tests::transition_matrix_refuses_every_undeclared_edge
    lease::tests::principal_concurrency_and_declaration_bounds_are_enforced
    lease::tests::transition_path_admits_exactly_the_declared_edges_from_every_reached_state
    lease::tests::public_transition_refuses_activities_owned_by_their_dedicated_paths
    lease::tests::destroyed_lease_cannot_be_revived_by_any_activity_path_or_forged_state
    lease::tests::exceeding_every_lease_bound_closes_with_its_typed_result_and_never_extends
    lease::tests::lease_declarations_are_bounded_in_lifetime_escrow_and_every_resource
    lease::tests::host_lifecycle_activities_are_one_way_replay_safe_and_decodable
    real_receipts_drive_every_lifecycle_edge_and_destroyed_never_revives
    real_bound_receipt_closes_intrinsically_and_refuses_mismatch_and_regression
    real_request_evidence_enforces_principal_concurrency_and_expiry
    real_receipts_expire_funded_and_active_leases_only_at_their_expiry_batch
    lease_prefix_is_host_accessible_isolated_and_addressable_as_one_unit
    altered_real_activity_and_header_evidence_are_refused
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
