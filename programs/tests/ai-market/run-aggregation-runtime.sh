#!/bin/sh
# AI.F05-T03 aggregation runtime scenarios: one result line per acceptance case. Exits 1 when a
# scenario fails, 2 when every run scenario passed but a scenario could not run, 0 otherwise.

set -u

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)
programs_cargo=${PROGRAMS_CARGO:-cargo}
log=$(mktemp)
trap 'rm -f "$log"' EXIT

cd "$repository_root/programs" || exit 1
"$programs_cargo" test --locked -p layerx-programs-ai-market \
    --test f05_aggregation_runtime -- --test-threads=1 >"$log" 2>&1
cargo_status=$?

failed=0
while read -r case name; do
    if grep -q "^test $name \.\.\. ok\$" "$log"; then
        echo "$case PASS $name"
    else
        echo "$case FAIL $name"
        failed=1
    fi
done <<'CASES'
AI.F05-A01 f05_a01_three_distinct_scores_select_the_median
AI.F05-A02 f05_a02_four_scores_select_the_lower_median
AI.F05-A03 f05_a03_zero_quorum_and_insufficient_quorum_stay_distinct
AI.F05-A04 f05_a04_absent_reports_add_no_observations
AI.F05-A05 f05_a05_arrival_order_never_changes_bytes_and_seal_height_does
AI.F05-A06 f05_a06_canonical_worker_order_and_display_shares
AI.F05-A07 f05_a07_display_truncation_and_weight_settlement
AI.F05-A08 f05_a08_largest_roster_in_four_chunks
AI.F05-A09 f05_a09_no_reports_settles_no_eligible_score
AI.F05-A10 f05_a10_refused_scores_and_tampered_persisted_records
AI.F05-A11 f05_a11_self_assessment_and_unbacked_persisted_votes
AI.F05-A12 f05_a12_settlement_window_and_delayed_completion
AI.F05-A13 f05_a13_cursor_retries_and_once_only_completion
AI.F05-A14 f05_a14_revocation_orderings
AI.F05-A15 f05_a15_worker_replacement_after_freeze
AI.F05-A16 f05_a16_refused_steps_keep_committed_progress
AI.F05-A17 f05_a17_colluding_low_reports_select_zero
AI.F05-A18 f05_a18_settlement_reads_no_evidence
AI.F04-A06 f04_a06_expired_commitments_supply_no_rows
AI.F04-A07 f05_a03_zero_quorum_and_insufficient_quorum_stay_distinct
AI.F04-A11 f04_a11_stable_set_at_reveal_end
AI.F04-A12 f05_a14_revocation_orderings
AI.F04-A12 f05_a12_settlement_window_and_delayed_completion
CASES

echo "AI.F05-A16 NOT_RUN native fuel refusal during ProcessAggregation: no T-C02 host boundary guest harness"

if [ "$cargo_status" -ne 0 ] || [ "$failed" -ne 0 ]; then
    cat "$log"
    exit 1
fi
exit 2
