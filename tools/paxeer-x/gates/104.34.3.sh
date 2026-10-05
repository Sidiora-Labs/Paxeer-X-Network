#!/usr/bin/env bash
# Lease-scoped sandbox execution: every layerx-programs-sandbox test target with the
# host lifecycle surface enabled, requiring the execution, hostile-escape and ceiling
# tests by name, then strict clippy over the crate. The aggregate make programs-test
# matrix stays release scope.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
crate=(--locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi)

timeout 25m cargo test "${crate[@]}" --lib --tests >"$logs/104.34.3-test.log" 2>&1
test_code=$?
cat "$logs/104.34.3-test.log"
timeout 10m cargo clippy "${crate[@]}" --all-targets --no-deps -- -D warnings >"$logs/104.34.3-clippy.log" 2>&1
clippy_code=$?
cat "$logs/104.34.3-clippy.log"

output=$(<"$logs/104.34.3-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    execute::tests::capabilities_are_derived_and_contain_no_escape_authority
    execute::tests::adjacent_leases_cannot_observe_the_same_runtime_namespace
    execute::tests::hostile_authority_families_are_absent_by_construction
    execute::tests::hostile_images_cannot_emit_or_call_an_unleased_program
    execute::tests::sandbox_call_is_ordinary_metered_execution_charged_to_the_lease
    execute::tests::hostile_sandbox_image_escapes_are_refused_with_no_state_leaving_the_lease
    execute::tests::ceiling_exhaustion_is_typed_and_distinct_from_program_failure
    execute::tests::sandbox_namespace_is_isolated_from_neighbour_tenant_and_shared_state
    execute::tests::cumulative_ceiling_refuses_a_later_run_instead_of_extending_the_lease
    execute::tests::lease_state_and_program_failure_are_distinct_from_ceiling_exhaustion
    lease::tests::exceeding_every_lease_bound_closes_with_its_typed_result_and_never_extends
    lease::tests::lease_declarations_are_bounded_in_lifetime_escrow_and_every_resource
    real_bound_receipt_closes_intrinsically_and_refuses_mismatch_and_regression
    lease_prefix_is_host_accessible_isolated_and_addressable_as_one_unit
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
