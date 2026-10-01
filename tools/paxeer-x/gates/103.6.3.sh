#!/usr/bin/env bash
# Full layerx-platform-authority qualification: every test target against the
# real layerxd sequencer and replica from build/bin, then strict clippy over all
# targets. Counts are summed over every test binary. The two cases fed by the
# router readiness harness (external LNI fixtures) run under gate 15.1.
# paxeer-x-services: authority
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
manifest=(--locked --manifest-path platform/Cargo.toml -p layerx-platform-authority --all-targets)

for binary in build/bin/layerxd build/bin/layerx-genesis-build; do
    if [[ ! -x $binary ]]; then
        echo "missing real node binary: $binary" >&2
        echo "PAXEER_X_GATE tests=0 skipped=0"
        exit 1
    fi
done

timeout 25m cargo test "${manifest[@]}" -- \
    --skip router_authority_readiness_schema_restart_contract \
    --skip authority_lni_readiness_case >"$logs/103.6.3-test.log" 2>&1
test_code=$?
cat "$logs/103.6.3-test.log"
timeout 10m cargo clippy "${manifest[@]}" -- -D warnings >"$logs/103.6.3-clippy.log" 2>&1
clippy_code=$?
cat "$logs/103.6.3-clippy.log"

output=$(<"$logs/103.6.3-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    real_node_authority_serves_verified_facts_and_reflects_replica_loss
    real_replica_readiness_relay_and_refusals_without_sequencer
    real_tls_client_and_durable_refusals
    real_server_wire_statuses_remain_distinct
    real_client_balance_context_uses_registry_and_verified_header
    maintained_batch_requires_signed_maintenance_and_explicit_selection
    replica_maintenance_document_is_explicit_and_closed
    signed_maintenance_sequence_and_root_mismatches_are_refused
    maintained_previous_root_and_cross_batch_leaf_are_refused
    historical_document_cannot_select_maintained_outcome
    lni_readiness_tests::actual_node_info_refuses_incompatible_receipt_admission
    human::session_membership::tests::summary_binds_canonical_activity_fee_and_authentication_grants
    human::session_membership::tests::mismatched_identity_key_scope_grant_or_time_is_refused
    human::session_membership::tests::original_native_receipts_verify_and_supply_canonical_session_membership
    human::session_membership::tests::native_revocation_and_replacement_preserve_only_current_membership
    human::dynamic::tests::genuine_sponsored_activity_binds_owner_target_network_and_every_original_byte
    human::dynamic::tests::scoped_queries_require_every_coordinate_and_reject_unknown_or_unbound_artifacts
    human::budget_state::record::tests::actual_native_versions_bind_source_revocation_and_remaining
    human::budget_state::record::tests::native_record_decoder_refuses_noncanonical_encodings
    human::budget_state::record::tests::exhausted_closed_and_revoked_budgets_never_report_spendable_value
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
((tests >= 38)) || { echo "executed $tests tests, expected at least 38" >&2; status=1; }
((skipped == 0)) || { echo "$skipped tests were ignored" >&2; status=1; }
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$status"
