#!/bin/sh
set -eu

process=cmd/layerxd/lxp_daemon_process.c
authority=cmd/layerxd/lxp_daemon_receipt_authority.c
evidence=cmd/layerxd/lxp_daemon_evidence.c
feed=src/modules/programs/feed_store.c

line_of() {
    pattern=$1
    file=$2
    line=$(awk -v pattern="$pattern" 'index($0, pattern) { print NR; exit }' "$file")
    test -n "$line" || {
        echo "missing recovery-order anchor: $pattern" >&2
        exit 1
    }
    printf '%s\n' "$line"
}

count_of() {
    awk -v pattern="$1" 'index($0, pattern) { count++ } END { print count + 0 }' "$2"
}

canonical_open=$(line_of '&process->canonical_log, "LAYERX_NODE_CANONICAL_LOG"' "$process")
canonical_occupancy_recover=$(line_of 'status = lxp_log_recover_complete_records(&process->canonical_log, NULL, NULL);' "$process")
canonical_recover=$(line_of 'status = lxp_log_recover(&process->canonical_log, NULL, NULL);' "$process")
authority_open=$(line_of '&process->authority_log, "LAYERX_NODE_RECEIPT_AUTHORITY_LOG"' "$process")
batch_open=$(line_of '&process->batch_log, "LAYERX_NODE_BATCH_LOG"' "$process")
batch_recover=$(line_of '&process->batch_log, NULL, NULL);' "$process")
batch_wal=$(line_of 'status = lxp_daemon_batch_wal_initialize(' "$process")
evidence_open=$(line_of '&process->evidence_log, "LAYERX_NODE_EVIDENCE_LOG"' "$process")
distinct_logs=$(line_of 'status = require_distinct_logs(logs, 5U);' "$process")
evidence_component=$(line_of 'status = lxp_daemon_evidence_open_history(' "$process")
history_open=$(line_of 'status = lxp_history_open(' "$process")
authority_component=$(line_of 'status = lxp_daemon_receipt_authority_open_history(' "$process")
reconcile=$(line_of 'status = replicate_authority_history(process);' "$process")
owner_attach=$(line_of 'status = lxp_daemon_protocol_owner_attach(' "$process")

test "$(awk -v line="$batch_recover" 'NR == line - 1 { print }' "$process" | grep -c 'status = lxp_log_recover_complete_records(')" -eq 1

test "$canonical_open" -lt "$canonical_occupancy_recover"
test "$canonical_occupancy_recover" -lt "$canonical_recover"
test "$canonical_recover" -lt "$authority_open"
test "$authority_open" -lt "$batch_open"
test "$batch_open" -lt "$batch_recover"
test "$batch_recover" -lt "$batch_wal"
test "$batch_wal" -lt "$authority_component"
test "$batch_recover" -lt "$authority_component"
test "$authority_component" -lt "$evidence_open"
test "$evidence_open" -lt "$distinct_logs"
test "$distinct_logs" -lt "$evidence_component"
test "$evidence_component" -lt "$history_open"
test "$history_open" -lt "$reconcile"
test "$reconcile" -lt "$owner_attach"
test "$authority_component" -lt "$owner_attach"

test "$(count_of 'lxp_log_recover_complete_records(&process->canonical_log' "$process")" -eq 1
test "$(count_of 'lxp_log_recover(&process->canonical_log' "$process")" -eq 1
test "$(count_of '&process->batch_log, NULL, NULL);' "$process")" -eq 1
test "$(count_of 'lxp_daemon_receipt_authority_open_history(' "$process")" -eq 1
test "$(count_of 'lxp_daemon_evidence_open_history(' "$process")" -eq 1
test "$(count_of 'lxp_daemon_receipt_authority_open(' "$process")" -eq 0
test "$(count_of 'lxp_daemon_evidence_open(' "$process")" -eq 0
test "$(count_of 'lxp_log_recover_complete_records(&process->authority_log' "$process")" -eq 0
test "$(count_of 'lxp_log_recover_complete_records(&process->evidence_log' "$process")" -eq 0
test "$(count_of 'lxp_log_recover_complete_records(&process->feed_log' "$process")" -eq 0

authority_owner=$(line_of 'lxp_result lxp_daemon_receipt_authority_open_history(' "$authority")
authority_recover=$(line_of 'lxp_result status = lxp_log_recover_complete_records(' "$authority")
authority_wrapper=$(line_of 'lxp_result lxp_daemon_receipt_authority_open(' "$authority")
evidence_owner=$(line_of 'lxp_result lxp_daemon_evidence_open_history(' "$evidence")
evidence_recover=$(line_of 'status = lxp_log_recover_complete_records(log, NULL, NULL);' "$evidence")
evidence_wrapper=$(line_of 'lxp_result lxp_daemon_evidence_open(' "$evidence")
feed_owner=$(line_of 'lxp_result lxp_programs_state_feed_store_open(' "$feed")
feed_recover=$(line_of 'status = lxp_log_recover_complete_records(log, replay_feed, store);' "$feed")

test "$authority_owner" -lt "$authority_recover"
test "$authority_recover" -lt "$authority_wrapper"
test "$evidence_owner" -lt "$evidence_recover"
test "$evidence_recover" -lt "$evidence_wrapper"
test "$feed_owner" -lt "$feed_recover"

test "$(awk '/lxp_log_recover_complete_records\(/ { count++ } END { print count + 0 }' "$authority")" -eq 1
test "$(awk '/lxp_log_recover_complete_records\(log, NULL, NULL\)/ { count++ } END { print count + 0 }' "$evidence")" -eq 1
test "$(awk '/lxp_log_recover_complete_records\(log, replay_feed, store\)/ { count++ } END { print count + 0 }' "$feed")" -eq 1
