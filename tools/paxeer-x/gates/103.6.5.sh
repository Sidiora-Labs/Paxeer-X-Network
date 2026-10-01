#!/usr/bin/env bash
# Full layerx-platform-identity qualification: every test target, then strict
# clippy over all targets. Counts are summed over every test binary.
# paxeer-x-services: identity
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
logs=${PAXEER_X_EVIDENCE_DIR:-$(mktemp -d)}
manifest=(--locked --manifest-path platform/Cargo.toml -p layerx-platform-identity --all-targets)

timeout 25m cargo test "${manifest[@]}" >"$logs/103.6.5-test.log" 2>&1
test_code=$?
cat "$logs/103.6.5-test.log"
timeout 10m cargo clippy "${manifest[@]}" -- -D warnings >"$logs/103.6.5-clippy.log" 2>&1
clippy_code=$?
cat "$logs/103.6.5-clippy.log"

output=$(<"$logs/103.6.5-test.log")
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')

required=(
    health_routes_answer_without_a_service_token
    readiness_fails_when_the_store_is_not_writable
    every_introspection_shape_matches_its_consumer
    wrong_service_tokens_are_refused
    the_registrar_creates_principals_and_only_provisioning_mints_their_sessions
    boot_requires_a_distinct_registrar_token
    revoked_sessions_introspect_inactive
    expired_sessions_introspect_inactive
    state_survives_a_restart
    principal_tenant_is_required_bounded_and_echoed
    a_second_tenant_cannot_claim_a_bound_subject
    sessions_belong_to_the_tenant_of_their_principal
    registry_resolver_authenticates_and_retains_authority_across_restart
    session_capacity_excludes_expired_after_restart
    tests::service_resolution_is_exact
    tests::the_registrar_creates_principals_without_session_authority
    tests::session_authority_belongs_to_provisioning_alone
    tests::service_names_are_distinct_and_file_name_safe
    tests::session_token_form_is_strict
    tests::subject_and_key_validation_matches_the_gateway_rules
    tests::introspection_shapes_serialize_exactly
    tests::tenant_names_are_bounded_and_colon_free
    tests::session_response_names_the_tenant_it_was_minted_in
    tests::a_cross_tenant_subject_claim_is_a_conflict
    tests::refusal_bodies_follow_the_hosted_contract
    tests::request_parser_rejects_unbounded_and_ambiguous_messages
    resolver_tests::route_requires_registry_authority_and_rejects_principal_substitution
    seal::tests::seal_round_trips_and_binds_the_key
    seal::tests::seal_uses_a_fresh_nonce_and_rejects_tampering
    seal::tests::hmac_matches_rfc_4231_case_two
    seal::tests::hex_round_trips
    store::tests::state_survives_reopen_and_compaction
    store::tests::principals_are_keyed_by_tenant_and_subject
    store::tests::sessions_are_scoped_to_the_tenant_of_their_principal
    store::tests::cross_tenant_state_on_disk_is_refused
    store::tests::torn_trailing_record_is_discarded_and_malformed_records_refuse
    store::tests::replaced_journal_refuses_readiness_and_writes
    store::tests::failed_journal_write_requires_restart
    store::tests::session_requires_a_known_principal_and_unique_identifier
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
((tests >= 39)) || { echo "executed $tests tests, expected at least 39" >&2; status=1; }
((skipped == 0)) || { echo "$skipped tests were ignored" >&2; status=1; }
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$status"
