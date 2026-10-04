#define _POSIX_C_SOURCE 200809L
#include "layerx/lxp_daemon.h"
#include "lxp_daemon_batch_wal.h"
#include "layerx/lxp_merkle.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>

static lxp_daemon_evidence_store *transport_store;
static lxp_arena *transport_arena;
static lxp_kernel_prepared_batch *transport_prepared;
static const lxp_daemon_batch_wal_input *transport_publication;

static lxp_result transport_serial(lxp_kernel *kernel, const lxp_activity *activity,
    const lxp_kernel_execution *execution, size_t bound, lxp_kernel_prepared_batch **out)
{
    lxp_result status = lxp_kernel_prepare_serial_activity_batch_with_admission_prestate(
        kernel, activity, execution, bound, out);
    if (status == LXP_OK) transport_prepared = *out;
    return status;
}

static lxp_result transport_scheduled(lxp_kernel *kernel, const lxp_activity *activities,
    const lxp_kernel_execution *executions, size_t count, uint32_t workers,
    size_t bound, lxp_kernel_prepared_batch **out, size_t *retry)
{
    lxp_result status = lxp_kernel_prepare_activity_batch_with_admission_prestate(
        kernel, activities, executions, count, workers, bound, out, retry);
    if (status == LXP_OK) transport_prepared = *out;
    return status;
}

static lxp_result transport_terminal(lxp_kernel *kernel, const lxp_activity *activity,
    const lxp_kernel_execution *execution, lxp_result refusal, size_t bound,
    lxp_kernel_prepared_batch **out)
{
    lxp_result status = lxp_kernel_prepare_terminal_rejection_with_admission_prestate(
        kernel, activity, execution, refusal, bound, out);
    if (status == LXP_OK) transport_prepared = *out;
    return status;
}

static lxp_result transport_wal(const char *directory,
    const lxp_daemon_batch_wal_input *input, uint8_t durable[32])
{
    lxp_result status = lxp_daemon_evidence_retain_arbiter_admission_prestates(
        transport_store, transport_prepared);
    if (status != LXP_OK) return status;
    transport_publication = input;
    return lxp_daemon_batch_wal_write_prepared(directory, input, durable);
}

static lxp_result transport_publish(lxp_kernel *kernel, const lxp_activity *activities,
    const lxp_kernel_prepared_batch *prepared, const uint8_t durable[32])
{
    uint8_t leaves[64][32], root[32];
    lxp_batch_header header;
    lxp_result status = lxp_kernel_finalize_prepared_batch_publication(
        kernel, activities, prepared, durable);
    const lxp_daemon_batch_wal_input *input = transport_publication;
    if (status != LXP_OK || input == NULL || input->count > 64U) return LXP_ERR_NON_CANONICAL;
    status = lxp_batch_header_decode(input->canonical_header.bytes,
        input->canonical_header.length, &header);
    for (size_t index = 0U; status == LXP_OK && index < input->count; ++index)
        status = lxp_merkle_leaf_hash(input->activities[index].bytes,
            input->activities[index].length, leaves[index]);
    for (size_t index = 0U; status == LXP_OK && index < input->count; ++index) {
        lxp_merkle_proof proof;
        size_t mark = lxp_arena_mark(transport_arena);
        status = lxp_merkle_proof_generate((const uint8_t (*)[32])leaves,
            input->count, index, transport_arena, &proof, root);
        if (status == LXP_OK && memcmp(root, header.activity_merkle_root, 32U) != 0)
            status = LXP_ERR_CONTEXT_MISMATCH;
        if (status == LXP_OK)
            status = lxp_daemon_activity_evidence_publish(transport_store,
                input->activities[index], &proof, input->receipts[index],
                &input->receipt_proofs[index], &input->authorization,
                input->canonical_header, input->header_signature, transport_arena, NULL);
        if (lxp_arena_reset(transport_arena, mark) != LXP_OK) return LXP_FATAL_INVARIANT;
    }
    return status;
}

#define lxp_kernel_prepare_serial_activity_batch_with_arbiter_prestate transport_serial
#define lxp_kernel_prepare_activity_batch_with_arbiter_prestate transport_scheduled
#define lxp_kernel_prepare_terminal_rejection_with_arbiter_prestate transport_terminal
#define lxp_daemon_batch_wal_write_prepared transport_wal
#define lxp_kernel_finalize_prepared_batch_publication transport_publish
#define _POSIX_C_SOURCE 200809L
#include "lxp_daemon_batch_wal.h"
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_genesis.h"
#include "layerx/lxp_maintenance.h"
#include "layerx/lxp_fault.h"
#include "layerx/lx_asset.h"
#include "layerx/lx_perps.h"
#include "layerx/lxp_bridge_credit.h"
#include "layerx/lxp_module_ctx.h"
#include "../bridge/files.h"
#include <unistd.h>
#include <sys/stat.h>
#define main maintenance_activity_fixture_main
#include "../programs/test_call_activity.c"
#undef main

#define CHECK(expression) do { if (!(expression)) { \
    fprintf(stderr, "maintenance publication failure at %d: %s\n", __LINE__, #expression); \
    return 1; } } while (0)

static const uint8_t maintenance_actor_seed[32] = {0x33U};
static const uint8_t maintenance_did[] = "did:lxp:maintenance-publication";

typedef struct maintenance_fixture {
    const lxp_activity *input_activity;
    const char *evidence_directory;
    bool execution_prestate_checks;
    bool arbiter_terminal;
    FILE *arbiter_manifest;
    bool arbiter_first_capture;
    bool arbiter_export;
    unsigned execution_prestate_crashes;
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lxp_identity *identity;
    lx_account_registry accounts;
    lx_account *actor;
    lx_account *treasury;
    lx_account *recipient;
    lxp_transfer_asset_state asset;
    lx_asset_record asset_record;
    lx_asset_runtime asset_runtime;
    lx_programs_transfer_runtime runtime;
    lxp_authority_scope scope;
    lxp_authority_resolved authority;
    lxp_fee_params fees;
    lxp_sequencer_authorization authorization;
    lxp_daemon_receipt_authority_store receipt_authority;
    lxp_log authority_log;
    lxp_log feed_log;
    lxp_log canonical_log;
    lxp_log evidence_log;
    lxp_history history;
    lx_programs_state_feed_store feed;
    lxp_daemon_evidence_store evidence;
    pthread_mutex_t feed_mutex;
    lxp_arena arena;
    uint8_t *storage;
    uint8_t actor_public_key[32];
    char directory[128];
    char authority_path[160];
} maintenance_fixture;

static bool arbiter_populated_genesis;
static int arbiter_insert(maintenance_fixture *f, uint16_t module,
    const uint8_t *key, size_t key_length, const uint8_t *value, size_t value_length);

static int write_evidence_file(const char *directory, const char *name,
    const uint8_t *bytes, size_t length)
{
    char path[4096];
    int written = snprintf(path, sizeof(path), "%s/%s", directory, name);
    FILE *file;
    if (written <= 0 || (size_t)written >= sizeof(path)) return 1;
    file = fopen(path, "wbx");
    if (file == NULL) return 1;
    if (fwrite(bytes, 1U, length, file) != length) {
        (void)fclose(file);
        return 1;
    }
    return fclose(file) == 0 ? 0 : 1;
}

static int write_evidence_proof(const char *directory, const char *name,
    const lxp_merkle_proof *proof)
{
    uint8_t encoded[10U + LXP_MERKLE_MAX_DEPTH * 32U];
    encoded[0] = 1U;
    for (size_t i = 0U; i < 4U; ++i) {
        encoded[1U + i] = (uint8_t)(proof->leaf_index >> (24U - 8U * i));
        encoded[5U + i] = (uint8_t)(proof->leaf_count >> (24U - 8U * i));
    }
    encoded[9] = proof->depth;
    if (proof->depth > LXP_MERKLE_MAX_DEPTH) return 1;
    for (size_t i = 0U; i < proof->depth; ++i)
        memcpy(encoded + 10U + i * 32U, proof->siblings[i], 32U);
    return write_evidence_file(directory, name, encoded, 10U + (size_t)proof->depth * 32U);
}

static int maintenance_fixture_logs(maintenance_fixture *f, uint32_t network_id)
{
    CHECK(executed_public_key(executed_sequencer_seed, f->authorization.public_key) == 0);
    memcpy(f->authorization.sequencer_id, f->authorization.public_key, 32U);
    f->authorization.authorized = 1U;
    f->authorization.first_batch_number = 1U;
    f->authorization.last_batch_number = 100U;
    strcpy(f->directory, "/tmp/lxp-maintenance-publication-XXXXXX");
    CHECK(mkdtemp(f->directory) != NULL);
    CHECK(snprintf(f->authority_path, sizeof(f->authority_path), "%s/authority.log", f->directory) > 0);
    CHECK(lxp_log_open_or_create(&f->authority_log, f->authority_path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
    CHECK(lxp_daemon_receipt_authority_open(&f->receipt_authority, &f->authority_log,
        &f->authorization) == LXP_OK);
    {
        char path[160], database[160];
        uint8_t anchor[32] = {1U};
        CHECK(pthread_mutex_init(&f->feed_mutex, NULL) == 0);
        CHECK(snprintf(path, sizeof(path), "%s/feed.log", f->directory) > 0);
        CHECK(lxp_log_open_or_create(&f->feed_log, path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
        CHECK(snprintf(path, sizeof(path), "%s/canonical.log", f->directory) > 0);
        CHECK(lxp_log_open_or_create(&f->canonical_log, path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
        CHECK(snprintf(database, sizeof(database), "%s/history.db", f->directory) > 0);
        CHECK(lxp_history_open(&f->history, &f->canonical_log, database, "migrations/0007_history_index.sql") == LXP_OK);
        CHECK(lxp_programs_state_feed_store_open(&f->feed, &f->feed_log, &f->canonical_log,
            &f->history, &f->arena, &f->feed_mutex) == LXP_OK);
        CHECK(lxp_programs_state_feed_store_anchor(&f->feed, f->state.next_sequence, f->kernel.current_state_root) == LXP_OK);
        f->runtime.state_feed = &f->feed.feed;
        CHECK(lxp_programs_bind_state_feed(&f->kernel, f->runtime.state_feed) == LXP_OK);
        CHECK(lxp_programs_state_feed_store_recover(&f->feed, &f->kernel) == LXP_OK);
        CHECK(snprintf(path, sizeof(path), "%s/evidence.log", f->directory) > 0);
        CHECK(lxp_log_open_or_create(&f->evidence_log, path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
        CHECK(lxp_daemon_evidence_open(&f->evidence, &f->evidence_log, network_id, &f->authorization,
            anchor, true, NULL, NULL, &f->arena) == LXP_OK);
    }
    return 0;
}
static int maintenance_fixture_open(maintenance_fixture *f)
{
    static const uint8_t actor_name[] = "agent:did:lxp:maintenance-publication:main";
    static const uint8_t treasury_name[] = "system:fees";
    static const uint8_t recipient_name[] = "agent:did:lxp:recipient:main";
    uint8_t actor_id[32], treasury_id[32], recipient_id[32], grant[32] = {0};
    static const uint64_t parameters = 1U;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters fees = {0};
    memset(f, 0, sizeof(*f));
    f->authority_log.descriptor = -1;
    f->storage = malloc(4U * LXP_MAX_BATCH_BODY_BYTES);
    CHECK(f->storage != NULL);
    CHECK(lxp_arena_init(&f->arena, f->storage, 4U * LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
    CHECK(executed_public_key(maintenance_actor_seed, f->actor_public_key) == 0);
    CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    CHECK(lx_account_id_from_string(actor_name, sizeof(actor_name) - 1U, actor_id) == LXP_OK);
    CHECK(lx_account_id_from_string(treasury_name, sizeof(treasury_name) - 1U, treasury_id) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, actor_name, sizeof(actor_name) - 1U,
        actor_id, 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->actor) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, treasury_name, sizeof(treasury_name) - 1U,
        treasury_id, 2U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->treasury) == LXP_OK);
    CHECK(lx_account_id_from_string(recipient_name, sizeof(recipient_name) - 1U, recipient_id) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, recipient_name, sizeof(recipient_name) - 1U,
        recipient_id, 3U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->recipient) == LXP_OK);
    f->asset.asset_id[0] = 9U;
    f->asset.registered = true;
    CHECK(lxp_ledger_bootstrap_balance(f->actor, f->asset.asset_id,
        (lxp_u128){0U, UINT64_MAX}, 1U) == LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(f->treasury, f->asset.asset_id,
        (lxp_u128){0U, 0U}, 0U) == LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(f->recipient, f->asset.asset_id,
        (lxp_u128){0U, 0U}, 0U) == LXP_OK);
    CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    CHECK(lxp_identity_register(&f->identities, maintenance_did, sizeof(maintenance_did) - 1U,
        f->actor_public_key, &f->identity) == LXP_OK);
    CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal, &parameters, 1U) == LXP_OK);
    CHECK(install_metering_v1(&f->kernel) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, programs_module_registration_v4()) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lx_asset_module_iface()) == LXP_OK);
    memcpy(f->asset_record.asset_id, f->asset.asset_id, 32U);
    f->asset_runtime = (lx_asset_runtime){&f->accounts, &f->asset_record, 1U,
        &f->asset, 1U, 7U, 3U};
    f->actor->has_authority_key = true;
    memcpy(f->actor->authority_key, f->actor_public_key, 32U);
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_ASSET, &f->asset_runtime) == LXP_OK);
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U};
    memcpy(f->runtime.occupancy_asset_id, f->asset.asset_id, 32U);
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS, &f->runtime) == LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    memcpy(manifest.signer_public_key, f->actor_public_key, 32U);
    fees.schedule = f->runtime.fee_schedule;
    memcpy(fees.occupancy_asset_id, f->asset.asset_id, 32U);
    fees.target_occupancy_byte_batches = 3U;
    fees.response_denominator = 1U;
    fees.maximum_change_numerator = 1U;
    fees.maximum_change_denominator = 1U;
    fees.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fees.maximum_fee_units_per_occupancy_byte_batch = 10U;
    CHECK(lxp_programs_fee_genesis_append(&manifest, &fees) == LXP_OK);
    CHECK(lxp_programs_fee_genesis_materialize(&manifest, &f->kernel) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lx_web_module_iface()) == LXP_OK);
    if (arbiter_populated_genesis) {
        uint8_t oracle_key[39] = {0}, oracle[LX_PERPS_ORACLE_BYTES] = {0};
        uint8_t web_key[51] = {0}, web_header[40] = {0};
        CHECK(lxp_kernel_register_module(&f->kernel, lx_perps_module_iface()) == LXP_OK);
        memcpy(oracle_key, "oracle:", 7U);
        oracle_key[7] = 0x31U;
        oracle[7] = 1U; oracle[23] = 1U; oracle[31] = 1U;
        CHECK(arbiter_insert(f, LXP_MODULE_PERPS, oracle_key, sizeof(oracle_key), oracle, sizeof(oracle)) == 0);
        memcpy(web_key, "web/answer", 10U);
        web_key[10] = 0x31U;
        CHECK(arbiter_insert(f, LXP_MODULE_WEB, web_key, sizeof(web_key), web_header, sizeof(web_header)) == 0);
    }
    CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    f->scope.module_mask = UINT64_C(1) << LXP_MODULE_PROGRAMS;
    f->scope.activity_ordinal_min = 1U;
    f->scope.activity_ordinal_max = 10U;
    f->scope.maximum_per_activity = (lxp_u128){UINT64_MAX, UINT64_MAX};
    f->scope.maximum_total = f->scope.maximum_per_activity;
    f->scope.maximum_per_period = f->scope.maximum_per_activity;
    f->authority.scope = &f->scope;
    f->authority.kind = LXP_AUTHORITY_OWNER;
    memcpy(f->authority.actor, f->identity->did_id, 32U);
    CHECK(lxp_did_id_derive(maintenance_did, sizeof(maintenance_did) - 1U,
        f->authority.principal) == LXP_OK);
    memcpy(f->authority.verified_key, f->actor_public_key, 32U);
    CHECK(lxp_authority_hash(f->authority.kind, grant, f->actor_public_key,
        f->authority.authority_hash) == LXP_OK);
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    return maintenance_fixture_logs(f, 7U);
}

typedef struct prestate_crash_workload {
    maintenance_fixture *fixture;
    const lxp_kernel_prepared_batch *prepared;
    const lxp_byte_span *activities;
    const lxp_byte_span *receipts;
    const lxp_merkle_proof *activity_proofs;
    const lxp_merkle_proof *receipt_proofs;
    lxp_byte_span header;
    const uint8_t *signature;
    size_t count;
} prestate_crash_workload;

typedef struct prestate_file_sink {
    FILE *file;
    size_t calls;
    size_t bytes;
} prestate_file_sink;

static lxp_result prestate_file_consume(void *context, lxp_byte_span payload)
{
    prestate_file_sink *sink = context;
    if (payload.bytes == NULL || payload.length == 0U ||
        payload.length > LXP_KERNEL_MAX_BLOB_TOTAL_BYTES || sink->calls != 0U)
        return LXP_ERR_NON_CANONICAL;
    if (fwrite(payload.bytes, 1U, payload.length, sink->file) != payload.length)
        return LXP_ERR_IO;
    ++sink->calls;
    sink->bytes = payload.length;
    return LXP_OK;
}

static int prestate_readback(maintenance_fixture *f,
    const lxp_receipt *receipt, lxp_byte_span expected, lxp_result result)
{
    uint8_t digest[32], bytes[4096];
    size_t mark = lxp_arena_mark(&f->arena);
    prestate_file_sink sink = {tmpfile(), 0U, 0U};
    CHECK(sink.file != NULL);
    CHECK(lxp_receipt_digest(receipt, &f->arena, digest) == LXP_OK);
    CHECK(lxp_daemon_evidence_get_execution_prestate(&f->evidence, 7U,
        receipt->activity_id, digest, &f->arena, prestate_file_consume, &sink) == result);
    if (result != LXP_OK) {
        CHECK(sink.calls == 0U && sink.bytes == 0U && ftell(sink.file) == 0L);
    } else {
        CHECK(sink.calls == 1U && sink.bytes == expected.length && expected.length >= 80U);
        CHECK(fflush(sink.file) == 0 && fseek(sink.file, 0L, SEEK_SET) == 0);
        for (size_t offset = 0U; offset < expected.length;) {
            size_t length = expected.length - offset;
            if (length > sizeof(bytes)) length = sizeof(bytes);
            CHECK(fread(bytes, 1U, length, sink.file) == length &&
                memcmp(bytes, expected.bytes + offset, length) == 0);
            offset += length;
        }
        CHECK(fgetc(sink.file) == EOF && !ferror(sink.file));
    }
    CHECK(fclose(sink.file) == 0 && lxp_arena_reset(&f->arena, mark) == LXP_OK);
    return 0;
}

static int prestate_reopen(maintenance_fixture *f)
{
    char path[160];
    uint8_t anchor[32];
    size_t mark = lxp_arena_mark(&f->arena);
    memcpy(anchor, f->evidence.registry.finalisation.settlement_anchor, 32U);
    CHECK(snprintf(path, sizeof(path), "%s/evidence.log", f->directory) > 0);
    CHECK(lxp_log_close(&f->evidence_log) == LXP_OK);
    CHECK(lxp_log_open(&f->evidence_log, path) == LXP_OK);
    CHECK(lxp_daemon_evidence_open(&f->evidence, &f->evidence_log, 7U,
        &f->authorization, anchor, true, NULL, NULL, &f->arena) == LXP_OK);
    f->evidence.execution_prestate_enabled = true;
    CHECK(lxp_daemon_evidence_execution_prestate_ready(&f->evidence));
    CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
    return 0;
}

static lxp_result prestate_retain_workload(void *context)
{
    prestate_crash_workload *workload = context;
    return lxp_daemon_evidence_retain_execution_prestates(
        &workload->fixture->evidence, workload->prepared);
}

static lxp_result prestate_publish_workload(void *context)
{
    prestate_crash_workload *workload = context;
    maintenance_fixture *f = workload->fixture;
    lxp_result status = LXP_OK;
    for (size_t index = 0U; status == LXP_OK && index < workload->count; ++index)
        status = lxp_daemon_activity_evidence_publish(&f->evidence,
            workload->activities[index], &workload->activity_proofs[index],
            workload->receipts[index], &workload->receipt_proofs[index],
            &f->authorization, workload->header, workload->signature, &f->arena, NULL);
    return status;
}

static int prestate_crash_retain(maintenance_fixture *f,
    const lxp_kernel_prepared_batch *prepared, const lxp_receipt *receipts, size_t count)
{
    prestate_crash_workload workload = {.fixture = f, .prepared = prepared};
    uint64_t before_records = f->evidence.record_count;
    size_t before_bytes = f->evidence.execution_prestate_retained_bytes;
    size_t captured_bytes = 0U;
    uint8_t live_root[32];
    int child_exit_status;
    memcpy(live_root, f->kernel.current_state_root, 32U);
    CHECK(count != 0U && count <= UINT32_MAX);
    for (size_t index = 0U; index < count; ++index) {
        lxp_byte_span capture = lxp_kernel_prepared_batch_execution_prestate(prepared, index);
        uint8_t sequence[8];
        write_u64(sequence, receipts[index].global_sequence);
        CHECK(capture.bytes != NULL && capture.length >= 80U &&
            memcmp(capture.bytes + 6U, receipts[index].activity_id, 32U) == 0 &&
            memcmp(capture.bytes + 38U, sequence, sizeof(sequence)) == 0 &&
            memcmp(capture.bytes + 46U, receipts[index].previous_state_root, 32U) == 0);
        CHECK(capture.length <= LXP_KERNEL_MAX_BLOB_TOTAL_BYTES - captured_bytes);
        captured_bytes += capture.length;
        if (index == 0U) CHECK(memcmp(receipts[index].previous_state_root, live_root, 32U) == 0);
        else {
            CHECK(memcmp(receipts[index].previous_state_root, receipts[index - 1U].resulting_state_root, 32U) == 0);
            CHECK(memcmp(receipts[index].previous_state_root, live_root, 32U) != 0);
        }
        CHECK(prestate_readback(f, &receipts[index], capture, LXP_ERR_UNKNOWN_ACTIVITY) == 0);
    }
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_BODY_WRITTEN, 1U,
        prestate_retain_workload, &workload, &child_exit_status) == LXP_OK);
    ++f->execution_prestate_crashes;
    CHECK(prestate_reopen(f) == 0 && f->evidence.record_count == before_records &&
        f->evidence.execution_prestate_retained_bytes == before_bytes);
    for (size_t index = 0U; index < count; ++index)
        CHECK(prestate_readback(f, &receipts[index],
            lxp_kernel_prepared_batch_execution_prestate(prepared, index), LXP_ERR_UNKNOWN_ACTIVITY) == 0);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_SYNCED, (uint32_t)count,
        prestate_retain_workload, &workload, &child_exit_status) == LXP_OK);
    ++f->execution_prestate_crashes;
    CHECK(prestate_reopen(f) == 0 && f->evidence.record_count == before_records + count &&
        f->evidence.execution_prestate_retained_bytes == before_bytes + captured_bytes);
    CHECK(memcmp(f->kernel.current_state_root, live_root, 32U) == 0 &&
        f->state.next_sequence == receipts[0].global_sequence);
    for (size_t index = 0U; index < count; ++index)
        CHECK(prestate_readback(f, &receipts[index],
            lxp_kernel_prepared_batch_execution_prestate(prepared, index), LXP_ERR_UNKNOWN_ACTIVITY) == 0);
    CHECK(lxp_daemon_evidence_retain_execution_prestates(&f->evidence, prepared) == LXP_OK &&
        f->evidence.record_count == before_records + count &&
        f->evidence.execution_prestate_retained_bytes == before_bytes + captured_bytes);
    return 0;
}

static int prestate_crash_publish(maintenance_fixture *f,
    const lxp_kernel_prepared_batch *prepared, const lxp_daemon_batch_wal_input *input,
    const lxp_receipt *receipts, const lxp_batch_header *header)
{
    uint8_t leaves[64][32], root[32];
    lxp_merkle_proof activity_proofs[64];
    uint64_t before_records = f->evidence.record_count;
    size_t before_bytes = f->evidence.execution_prestate_retained_bytes;
    size_t mark = lxp_arena_mark(&f->arena);
    int child_exit_status;
    prestate_crash_workload workload = {f, prepared, input->activities, input->receipts,
        activity_proofs, input->receipt_proofs, input->canonical_header,
        input->header_signature, input->count};
    CHECK(input->count != 0U && input->count <= 64U);
    CHECK(f->state.next_sequence == header->last_sequence + 1U &&
        memcmp(f->kernel.current_state_root, header->resulting_state_root, 32U) == 0);
    for (size_t index = 0U; index < input->count; ++index) {
        CHECK(lxp_merkle_leaf_hash(input->activities[index].bytes,
            input->activities[index].length, leaves[index]) == LXP_OK);
        CHECK(prestate_readback(f, &receipts[index],
            lxp_kernel_prepared_batch_execution_prestate(prepared, index), LXP_ERR_UNKNOWN_ACTIVITY) == 0);
    }
    for (size_t index = 0U; index < input->count; ++index) {
        CHECK(lxp_merkle_proof_generate((const uint8_t (*)[32])leaves, input->count,
            index, &f->arena, &activity_proofs[index], root) == LXP_OK);
        CHECK(memcmp(root, header->activity_merkle_root, 32U) == 0);
    }
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_BODY_WRITTEN, 1U,
        prestate_publish_workload, &workload, &child_exit_status) == LXP_OK);
    ++f->execution_prestate_crashes;
    CHECK(prestate_reopen(f) == 0 && f->evidence.record_count == before_records &&
        f->evidence.execution_prestate_retained_bytes == before_bytes);
    for (size_t index = 0U; index < input->count; ++index)
        CHECK(prestate_readback(f, &receipts[index],
            lxp_kernel_prepared_batch_execution_prestate(prepared, index), LXP_ERR_UNKNOWN_ACTIVITY) == 0);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_SYNCED, (uint32_t)input->count,
        prestate_publish_workload, &workload, &child_exit_status) == LXP_OK);
    ++f->execution_prestate_crashes;
    CHECK(prestate_reopen(f) == 0 && f->evidence.record_count == before_records + input->count &&
        f->evidence.execution_prestate_retained_bytes == before_bytes);
    for (size_t index = 0U; index < input->count; ++index)
        CHECK(prestate_readback(f, &receipts[index],
            lxp_kernel_prepared_batch_execution_prestate(prepared, index), LXP_OK) == 0);
    CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
    return 0;
}

static int maintenance_publish(maintenance_fixture *f, uint32_t type,
    const uint8_t *payload, size_t payload_length, size_t count, uint64_t batch_number)
{
    lxp_activity *activities = calloc(count, sizeof(*activities));
    lxp_kernel_execution *executions = calloc(count, sizeof(*executions));
    lxp_byte_span canonical[64], receipts[65], events[65], artifacts[64], graphs[64];
    uint8_t *canonical_storage[64] = {NULL}, *receipt_storage[64] = {NULL};
    lxp_merkle_proof proofs[64];
    uint8_t signatures[64][64], leaves[65][32], root[32], batch_id[32], durable[32];
    lxp_kernel_prepared_batch *prepared = NULL;
    lxp_daemon_batch_wal_record *loaded = NULL;
    lxp_daemon_batch_wal_input input = {0};
    lxp_batch_roots roots;
    lxp_batch_header header = {0};
    lxp_batch_body *body = calloc(1U, sizeof(*body));
    lxp_kernel_batch_boundary live;
    lxp_daemon_batch_wal_recovery recovery;
    const lxp_receipt *decoded;
    lxp_programs_occupancy_receipt maintenance;
    lxp_batch_maintenance envelope;
    size_t retry = 0U;
    bool present = false;
    uint64_t first_sequence = f->state.next_sequence;
    CHECK(count > 0U && count <= 64U && activities != NULL && executions != NULL && body != NULL);
    CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    f->scope.module_mask = UINT64_C(1) << lxp_activity_module_id(type);
    if (lxp_activity_module_id(type) == LXP_MODULE_PROGRAMS) {
        CHECK(lxp_did_id_derive(maintenance_did, sizeof(maintenance_did) - 1U,
            f->authority.principal) == LXP_OK);
    } else if (f->actor != NULL) {
        memcpy(f->authority.principal, f->actor->id, 32U);
    }
    for (size_t i = 0U; i < count; ++i) {
        EVP_PKEY *key;
        EVP_MD_CTX *ctx;
        size_t signature_length = 64U;
        uint8_t preimage[32];
        if (f->input_activity != NULL) {
            CHECK(count == 1U);
            activities[i] = *f->input_activity;
        } else {
        fill_activity(&activities[i], type, payload, payload_length,
            maintenance_did, sizeof(maintenance_did) - 1U, f->actor_public_key);
        activities[i].protocol_version = 3U;
        activities[i].network_id = 7U;
        activities[i].account_sequence = f->identity->next_sequence + i;
        write_u64(activities[i].idempotency_key, activities[i].account_sequence + 1U);
        activities[i].fee_limit = type == LX_PROGRAMS_CALL ? (lxp_u128){0U, 1000000000U} : (lxp_u128){0U, 0U};
        CHECK(lxp_activity_signing_preimage(&activities[i], preimage) == LXP_OK);
        key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, maintenance_actor_seed, 32U);
        ctx = EVP_MD_CTX_new();
        CHECK(key != NULL && ctx != NULL);
        CHECK(EVP_DigestSignInit(ctx, NULL, NULL, NULL, key) == 1 &&
            EVP_DigestSign(ctx, signatures[i], &signature_length, preimage, 32U) == 1 && signature_length == 64U);
        EVP_MD_CTX_free(ctx);
        EVP_PKEY_free(key);
        activities[i].signature = (lxp_byte_span){signatures[i], 64U};
        }
        {
            size_t mark = lxp_arena_mark(&f->arena);
            CHECK(lxp_activity_encode(&activities[i], &f->arena, &canonical[i]) == LXP_OK);
            canonical_storage[i] = malloc(canonical[i].length);
            CHECK(canonical_storage[i] != NULL);
            memcpy(canonical_storage[i], canonical[i].bytes, canonical[i].length);
            canonical[i].bytes = canonical_storage[i];
            CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
        }
        executions[i].network_id = activities[i].network_id;
        executions[i].batch_number = batch_number;
        executions[i].batch_timestamp_ms = f->input_activity != NULL ?
            activities[i].timestamp_bound.not_before : 10U;
        executions[i].maximum_timestamp_window = f->input_activity != NULL ?
            activities[i].timestamp_bound.not_after - activities[i].timestamp_bound.not_before : 100U;
        executions[i].epoch = 1U;
        executions[i].global_sequence = first_sequence + i;
        executions[i].recorded_module_version = type == LXP_BRIDGE_CREDIT ? 1U : type == LX_ASSET_SEND ?
            lx_asset_module_iface()->abi_version : LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
        executions[i].parameter_version = 1U;
        executions[i].signature_valid = true;
        executions[i].identities = &f->identities;
        executions[i].authority = &f->authority;
        executions[i].fee_parameters = &f->fees;
        executions[i].fee_balance = f->actor != NULL ? f->actor->balance : (lxp_u128){0U, 0U};
        executions[i].gas_limit = UINT64_MAX;
        executions[i].arena = &f->arena;
        executions[i].sequencer_private_key = executed_sequencer_seed;
    }
    CHECK(lxp_daemon_batch_bind_prefix(canonical, count, f->kernel.current_state_root,
        first_sequence, batch_number, &f->arena, executions, &roots, batch_id) == LXP_OK);
    if (f->arbiter_manifest != NULL) {
        lxp_kernel_prepared_batch *legacy = NULL;
        lxp_kernel_batch_snapshot *snapshot = NULL;
        lxp_byte_span fresh = {NULL, 0U}, refused = {(const uint8_t *)"x", 1U};
        lxp_result prepared_status;
        if (f->arbiter_terminal) {
            CHECK(count == 1U);
            prepared_status = lxp_kernel_prepare_terminal_rejection_with_arbiter_prestate(
                &f->kernel, activities, executions, LXP_ERR_IDENTITY_FROZEN,
                64U * 1024U * 1024U, &prepared);
            CHECK(lxp_kernel_prepare_terminal_rejection(&f->kernel, activities, executions,
                LXP_ERR_IDENTITY_FROZEN, &legacy) == LXP_OK);
        } else if (type == LX_PROGRAMS_CALL) {
            prepared_status = lxp_kernel_prepare_activity_batch_with_arbiter_prestate(
                &f->kernel, activities, executions, count, 4U,
                64U * 1024U * 1024U, &prepared, &retry);
            CHECK(lxp_kernel_prepare_activity_batch(&f->kernel, activities, executions,
                count, 4U, &legacy, &retry) == LXP_OK);
        } else {
            CHECK(count == 1U);
            prepared_status = lxp_kernel_prepare_serial_activity_batch_with_arbiter_prestate(
                &f->kernel, activities, executions, 64U * 1024U * 1024U, &prepared);
            CHECK(lxp_kernel_prepare_serial_activity_batch(&f->kernel, activities,
                executions, &legacy) == LXP_OK);
        }
        CHECK(prepared_status == LXP_OK && prepared != NULL && retry == 0U);
        CHECK(lxp_kernel_batch_snapshot_create(&f->kernel, &f->identities,
            executions[0].verified_receipts, executions, &snapshot) == LXP_OK);
        CHECK(lxp_kernel_encode_arbiter_prestate(snapshot, activities, executions,
            1U, &refused) != LXP_OK && refused.bytes == NULL && refused.length == 0U);
        CHECK(lxp_kernel_encode_arbiter_prestate(snapshot, activities, executions,
            64U * 1024U * 1024U, &fresh) == LXP_OK);
        {
            lxp_byte_span sidecar = lxp_kernel_prepared_batch_arbiter_prestate(prepared, 0U);
            CHECK(fresh.length == sidecar.length && memcmp(fresh.bytes, sidecar.bytes, fresh.length) == 0);
        }
        lxp_kernel_batch_snapshot_destroy(snapshot);
        CHECK(fresh.bytes != NULL && fresh.bytes[1] == 2U);
        lxp_kernel_arbiter_prestate_destroy(&fresh);
        CHECK(fresh.bytes == NULL && fresh.length == 0U);
        lxp_kernel_arbiter_prestate_destroy(&fresh);
        for (size_t i = 0U; i < count; ++i) {
            lxp_byte_span old = lxp_kernel_prepared_batch_execution_prestate(legacy, i);
            lxp_byte_span unchanged = lxp_kernel_prepared_batch_execution_prestate(prepared, i);
            lxp_byte_span v2 = lxp_kernel_prepared_batch_arbiter_prestate(prepared, i);
            lxp_byte_span v3 = lxp_kernel_prepared_batch_admission_prestate(prepared, i);
            CHECK(v3.bytes != NULL && v3.length > v2.length + 12U &&
                v3.bytes[0] == 0U && v3.bytes[1] == 3U &&
                memcmp(v3.bytes + 6U, v2.bytes, v2.length) == 0);
            CHECK(lxp_kernel_prepared_batch_arbiter_prestate(legacy, i).bytes == NULL);
            CHECK(old.bytes != NULL && old.length == unchanged.length &&
                memcmp(old.bytes, unchanged.bytes, old.length) == 0);
            CHECK(v2.length > old.length + 8U && memcmp(v2.bytes + 6U, old.bytes, old.length) == 0);
            CHECK(memcmp(v2.bytes + 52U,
                lxp_kernel_prepared_batch_receipts(prepared)[i].previous_state_root, 32U) == 0);
        }
        CHECK(lxp_kernel_prepared_batch_arbiter_prestate(prepared, count).bytes == NULL);
        CHECK(lxp_kernel_prepared_batch_arbiter_prestate(NULL, 0U).bytes == NULL);
        if (count > 1U) CHECK(memcmp(
            lxp_kernel_prepared_batch_receipts(prepared)[0].previous_state_root,
            lxp_kernel_prepared_batch_receipts(prepared)[1].previous_state_root, 32U) != 0);
        lxp_kernel_prepared_batch_destroy(legacy);
        puts("ARBITER_CASE native-actual-boundaries-legacy-bytes-ownership-bounds");
    } else if (type == LX_PROGRAMS_CALL) {
        CHECK(lxp_kernel_prepare_activity_batch(&f->kernel, activities, executions,
            count, 4U, &prepared, &retry) == LXP_OK && retry == 0U);
    } else {
        CHECK(count == 1U);
        CHECK(lxp_kernel_prepare_serial_activity_batch(&f->kernel, activities, executions, &prepared) == LXP_OK);
    }
    {
        lxp_result maintenance_status = lxp_kernel_prepare_batch_maintenance(prepared, activities, executions);
        if (maintenance_status != LXP_OK) fprintf(stderr, "maintenance preparation result %d\n", (int)maintenance_status);
        CHECK(maintenance_status == LXP_OK);
    }
    CHECK(f->state.next_sequence == first_sequence);
    decoded = lxp_kernel_prepared_batch_receipts(prepared);
    for (size_t i = 0U; i < count; ++i) {
        if (decoded[i].result_code != LXP_OK)
            fprintf(stderr, "maintained activity type %u result %d\n", type, (int)decoded[i].result_code);
        CHECK(decoded[i].result_code == (f->arbiter_terminal ? LXP_ERR_IDENTITY_FROZEN : LXP_OK));
        size_t mark = lxp_arena_mark(&f->arena);
        CHECK(lxp_receipt_verify(&decoded[i], f->authorization.public_key, &f->arena) == LXP_OK);
        CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
        CHECK(lxp_receipt_encode(&decoded[i], true, &f->arena, &receipts[i]) == LXP_OK);
        receipt_storage[i] = malloc(receipts[i].length);
        CHECK(receipt_storage[i] != NULL);
        memcpy(receipt_storage[i], receipts[i].bytes, receipts[i].length);
        receipts[i].bytes = receipt_storage[i];
        CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
        artifacts[i] = decoded[i].program_outcome.terminal_payload;
        graphs[i] = decoded[i].program_outcome.call_graph_payload;
    }
    receipts[count] = lxp_kernel_prepared_batch_maintenance(prepared);
    CHECK(lxp_batch_maintenance_decode(receipts[count].bytes, receipts[count].length, &envelope) == LXP_OK);
    CHECK(envelope.epoch == executions[0].epoch && envelope.timestamp_ms == executions[0].batch_timestamp_ms);
    CHECK(lxp_programs_occupancy_receipt_decode(envelope.occupancy.bytes, envelope.occupancy.length, &maintenance) == LXP_OK);
    CHECK(maintenance.global_sequence == first_sequence + count && maintenance.batch_number == batch_number);
    for (size_t i = 0U; i < count; ++i)
        events[i] = lxp_kernel_prepared_batch_events(prepared)[i];
    events[count] = envelope.effects;
    CHECK(lxp_batch_roots_compute(&(lxp_batch_root_inputs){canonical, count, receipts, count + 1U,
        events, count + 1U, NULL, 0U, NULL, 0U}, &f->arena, &roots) == LXP_OK);
    header.protocol_version = 3U;
    header.network_id = activities[0].network_id;
    header.epoch = 1U;
    header.batch_number = batch_number;
    header.first_sequence = first_sequence;
    header.last_sequence = first_sequence + count;
    header.timestamp_ms = executions[0].batch_timestamp_ms;
    memcpy(header.previous_state_root, f->kernel.current_state_root, 32U);
    memcpy(header.resulting_state_root, maintenance.resulting_state_root, 32U);
    memcpy(header.activity_merkle_root, roots.activity_merkle_root, 32U);
    memcpy(header.receipt_merkle_root, roots.receipt_merkle_root, 32U);
    memcpy(header.event_merkle_root, roots.event_merkle_root, 32U);
    memcpy(header.oracle_root, roots.oracle_root, 32U);
    memcpy(header.data_availability_root, roots.data_availability_root, 32U);
    memcpy(header.sequencer_id, f->authorization.sequencer_id, 32U);
    input.protocol_version = 3U;
    input.network_id = header.network_id;
    input.epoch = 1U;
    input.batch_number = batch_number;
    input.timestamp_ms = header.timestamp_ms;
    input.parameter_version = 1U;
    input.fee_schedule_version = lxp_kernel_prepared_batch_fee_schedule_version(prepared);
    input.metering_schedule_version = lxp_kernel_prepared_batch_metering_schedule_version(prepared);
    input.first_sequence = first_sequence;
    input.last_sequence = header.last_sequence;
    input.count = count;
    input.base = *lxp_kernel_prepared_batch_base_boundary(prepared);
    input.settled = *lxp_kernel_prepared_batch_final_boundary(prepared);
    memcpy(input.publication_digest, lxp_kernel_prepared_batch_publication_digest(prepared), 32U);
    input.authorization = f->authorization;
    input.activities = canonical;
    input.receipts = receipts;
    input.events = lxp_kernel_prepared_batch_events(prepared);
    input.terminal_payloads = artifacts;
    input.call_graphs = graphs;
    input.receipt_proofs = proofs;
    input.maintenance = receipts[count];
    CHECK(lxp_da_body_from_kernels(&header,
        lxp_kernel_prepared_batch_base_kernel(prepared),
        lxp_kernel_prepared_batch_settled_kernel(prepared), canonical, count,
        receipts, count + 1U, events, count + 1U, NULL, 0U, &f->arena, body) == LXP_OK);
    header = body->header;
    input.state_diff = body->state_diff;
    input.recovery_metadata = body->recovery_metadata;
    CHECK(lxp_batch_sign(&header, executed_sequencer_seed, &f->authorization, input.header_signature, &f->arena) == LXP_OK);
    CHECK(lxp_batch_header_encode(&header, &f->arena, &input.canonical_header) == LXP_OK);
    for (size_t i = 0U; i <= count; ++i)
        CHECK(lxp_merkle_leaf_hash(receipts[i].bytes, receipts[i].length, leaves[i]) == LXP_OK);
    for (size_t i = 0U; i <= count; ++i) {
        lxp_merkle_proof *proof = i == count ? &input.maintenance_proof : &proofs[i];
        CHECK(lxp_merkle_proof_generate((const uint8_t (*)[32])leaves, count + 1U, i,
            &f->arena, proof, root) == LXP_OK);
        CHECK(memcmp(root, roots.receipt_merkle_root, 32U) == 0);
    }
    if (f->arbiter_manifest != NULL && f->arbiter_export) {
        char name[128], absolute[4096];
        const char *kind = f->arbiter_terminal ? "terminal" : type == LX_PROGRAMS_CALL ? "scheduled" : "serial-empty";
        CHECK(snprintf(name, sizeof(name), "batch-%llu.header", (unsigned long long)batch_number) > 0);
        CHECK(write_evidence_file(f->evidence_directory, name, input.canonical_header.bytes, input.canonical_header.length) == 0);
        CHECK(snprintf(name, sizeof(name), "batch-%llu.signature", (unsigned long long)batch_number) > 0);
        CHECK(write_evidence_file(f->evidence_directory, name, input.header_signature, 64U) == 0);
        CHECK(snprintf(name, sizeof(name), "batch-%llu.maintenance", (unsigned long long)batch_number) > 0);
        CHECK(write_evidence_file(f->evidence_directory, name, input.maintenance.bytes, input.maintenance.length) == 0);
        CHECK(snprintf(name, sizeof(name), "batch-%llu.maintenance-proof", (unsigned long long)batch_number) > 0);
        CHECK(write_evidence_proof(f->evidence_directory, name, &input.maintenance_proof) == 0);
        for (size_t i = 0U; i < count; ++i) {
            lxp_byte_span v2 = lxp_kernel_prepared_batch_arbiter_prestate(prepared, i);
            lxp_byte_span v1 = lxp_kernel_prepared_batch_execution_prestate(prepared, i);
            lxp_byte_span v3 = lxp_kernel_prepared_batch_admission_prestate(prepared, i);
            CHECK(v3.length > v2.length + 12U && v3.bytes[0] == 0U && v3.bytes[1] == 3U);
            CHECK(memcmp(v3.bytes + 6U, v2.bytes, v2.length) == 0);
            CHECK(snprintf(name, sizeof(name), "batch-%llu-%zu.v3", (unsigned long long)batch_number, i) > 0);
            CHECK(write_evidence_file(f->evidence_directory, name, v3.bytes, v3.length) == 0);
            CHECK(snprintf(name, sizeof(name), "batch-%llu-%zu.v2", (unsigned long long)batch_number, i) > 0);
            CHECK(write_evidence_file(f->evidence_directory, name, v2.bytes, v2.length) == 0);
            CHECK(snprintf(name, sizeof(name), "batch-%llu-%zu.v1", (unsigned long long)batch_number, i) > 0);
            CHECK(write_evidence_file(f->evidence_directory, name, v1.bytes, v1.length) == 0);
            CHECK(snprintf(name, sizeof(name), "batch-%llu-%zu.receipt", (unsigned long long)batch_number, i) > 0);
            CHECK(write_evidence_file(f->evidence_directory, name, receipts[i].bytes, receipts[i].length) == 0);
            CHECK(snprintf(name, sizeof(name), "batch-%llu-%zu.proof", (unsigned long long)batch_number, i) > 0);
            CHECK(write_evidence_proof(f->evidence_directory, name, &proofs[i]) == 0);
            CHECK(snprintf(absolute, sizeof(absolute), "%s/batch-%llu", f->evidence_directory, (unsigned long long)batch_number) > 0);
            CHECK(fprintf(f->arbiter_manifest, "%s{\"name\":\"%s-%zu\",\"v3_path\":\"%s-%zu.v3\",\"v2_path\":\"%s-%zu.v2\",\"v1_path\":\"%s-%zu.v1\",\"receipt_path\":\"%s-%zu.receipt\",\"proof_path\":\"%s-%zu.proof\",\"header_path\":\"%s.header\",\"header_signature_path\":\"%s.signature\",\"maintenance_path\":\"%s.maintenance\",\"maintenance_proof_path\":\"%s.maintenance-proof\",\"receipts\":[", f->arbiter_first_capture ? "" : ",", kind, i,
                absolute, i, absolute, i, absolute, i, absolute, i, absolute, i, absolute, absolute, absolute, absolute) > 0);
            for (size_t j = 0U; j < count; ++j)
                CHECK(fprintf(f->arbiter_manifest, "%s\"%s-%zu.receipt\"", j == 0U ? "" : ",", absolute, j) > 0);
            CHECK(fprintf(f->arbiter_manifest, "]}") > 0);
            f->arbiter_first_capture = false;
        }
    }
    if (f->evidence_directory != NULL && f->arbiter_manifest == NULL) {
        CHECK(count == 1U);
        CHECK(write_evidence_file(f->evidence_directory, "header",
            input.canonical_header.bytes, input.canonical_header.length) == 0);
        CHECK(write_evidence_file(f->evidence_directory, "header.signature",
            input.header_signature, 64U) == 0);
        CHECK(write_evidence_file(f->evidence_directory, "sequencer.public",
            f->authorization.public_key, 32U) == 0);
        CHECK(write_evidence_file(f->evidence_directory, "credit.receipt",
            receipts[0].bytes, receipts[0].length) == 0);
        CHECK(write_evidence_proof(f->evidence_directory, "receipt.proof", &proofs[0]) == 0);
        CHECK(write_evidence_file(f->evidence_directory, "maintenance.receipt",
            input.maintenance.bytes, input.maintenance.length) == 0);
        CHECK(write_evidence_proof(f->evidence_directory, "maintenance.proof",
            &input.maintenance_proof) == 0);
    }
    if (f->execution_prestate_checks)
        CHECK(prestate_crash_retain(f, prepared, decoded, count) == 0);
    CHECK(lxp_daemon_batch_wal_write_prepared(f->directory, &input, durable) == LXP_OK);
    CHECK(lxp_daemon_batch_wal_load(f->directory, &f->authorization, &loaded, &present) == LXP_OK && present);
    CHECK(lxp_daemon_batch_wal_classify(loaded, &input.base, &recovery) == LXP_OK &&
        recovery == LXP_DAEMON_BATCH_WAL_DISCARD_BASE);
    CHECK(lxp_daemon_batch_wal_view(loaded)->count == count &&
        lxp_daemon_batch_wal_view(loaded)->maintenance.length == input.maintenance.length);
    CHECK(lxp_kernel_commit_prepared_batch(&f->kernel, &f->identities, prepared, durable) == LXP_OK);
    CHECK(lxp_kernel_batch_boundary_read(&f->kernel, &live) == LXP_OK);
    CHECK(lxp_daemon_batch_wal_classify(loaded, &live, &recovery) == LXP_OK &&
        recovery == LXP_DAEMON_BATCH_WAL_FINALIZE_SETTLED);
    CHECK(lxp_kernel_finalize_prepared_batch_publication(&f->kernel, activities, prepared, durable) == LXP_OK);
    for (size_t i = 0U; i < count; ++i)
        CHECK(lxp_daemon_receipt_authority_append_artifacts(&f->receipt_authority,
            receipts[i].bytes, receipts[i].length, input.canonical_header.bytes,
            input.canonical_header.length, input.header_signature, &proofs[i], &f->arena,
            artifacts[i], graphs[i]) == LXP_OK);
    {
        uint8_t bad_signature[64];
        lxp_merkle_proof bad_proof = input.maintenance_proof;
        memcpy(bad_signature, input.header_signature, 64U);
        bad_signature[0] ^= 1U;
        CHECK(lxp_daemon_receipt_authority_append_maintenance(&f->receipt_authority,
            input.maintenance.bytes, input.maintenance.length, input.canonical_header.bytes,
            input.canonical_header.length, bad_signature, &input.maintenance_proof, &f->arena) != LXP_OK);
        bad_proof.leaf_index = 0U;
        CHECK(lxp_daemon_receipt_authority_append_maintenance(&f->receipt_authority,
            input.maintenance.bytes, input.maintenance.length, input.canonical_header.bytes,
            input.canonical_header.length, input.header_signature, &bad_proof, &f->arena) != LXP_OK);
        CHECK(f->receipt_authority.last_global_sequence == first_sequence + count - 1U);
    }
    CHECK(lxp_daemon_receipt_authority_append_maintenance(&f->receipt_authority,
        input.maintenance.bytes, input.maintenance.length, input.canonical_header.bytes,
        input.canonical_header.length, input.header_signature, &input.maintenance_proof, &f->arena) == LXP_OK);
    {
        lxp_daemon_receipt_evidence activity_evidence, selected;
        bool selected_present;
        uint8_t digest[32];
        size_t mark = lxp_arena_mark(&f->arena);
        CHECK(lxp_receipt_digest(&decoded[0], &f->arena, digest) == LXP_OK);
        CHECK(lxp_daemon_receipt_authority_lookup(&f->receipt_authority,
            digest, &f->arena, &activity_evidence) == LXP_OK);
        CHECK(lxp_daemon_receipt_authority_batch_maintenance(&f->receipt_authority,
            &activity_evidence, &f->arena, &selected, &selected_present) == LXP_OK && selected_present);
        CHECK(selected.format_version == 3U && selected.receipt_proof.leaf_index == count &&
            selected.receipt_proof.leaf_count == count + 1U &&
            selected.canonical_receipt.length == input.maintenance.length &&
            memcmp(selected.canonical_receipt.bytes, input.maintenance.bytes, input.maintenance.length) == 0);
        activity_evidence.header_signature[0] ^= 1U;
        CHECK(lxp_daemon_receipt_authority_batch_maintenance(&f->receipt_authority,
            &activity_evidence, &f->arena, &selected, &selected_present) == LXP_ERR_CONTEXT_MISMATCH);
        CHECK(lxp_arena_reset(&f->arena, mark) == LXP_OK);
    }
    CHECK(f->feed.scanned_through_sequence == maintenance.global_sequence);
    CHECK(memcmp(f->feed.head_state_root, maintenance.resulting_state_root, 32U) == 0);
    CHECK(lxp_daemon_account_evidence_publish_batch_maintenance(&f->evidence, &f->kernel,
        input.maintenance, &input.maintenance_proof, &f->authorization, input.canonical_header,
        input.header_signature, &f->arena) == LXP_OK);
    {
        lxp_daemon_account_evidence account;
        const uint8_t *account_id = f->actor != NULL ? f->actor->id : f->authority.principal;
        CHECK(lxp_daemon_account_evidence_lookup(&f->evidence, account_id,
            maintenance.resulting_state_root, &f->arena, &account) == LXP_OK);
        CHECK(account.format_version == 3U && account.observed_sequence == maintenance.global_sequence);
        CHECK(account.canonical_receipt.length == input.maintenance.length &&
            memcmp(account.canonical_receipt.bytes, input.maintenance.bytes, input.maintenance.length) == 0);
    }
    if (f->execution_prestate_checks)
        CHECK(prestate_crash_publish(f, prepared, &input, decoded, &header) == 0);
    CHECK(lxp_daemon_batch_wal_transition(f->directory, loaded, &live, LXP_DAEMON_BATCH_WAL_COMMITTED) == LXP_OK);
    CHECK(lxp_daemon_batch_wal_retire(f->directory, loaded, &live) == LXP_OK);
    CHECK(f->state.next_sequence == first_sequence + count + 1U);
    CHECK(f->receipt_authority.last_global_sequence == header.last_sequence);
    lxp_daemon_batch_wal_destroy(loaded);
    lxp_kernel_prepared_batch_destroy(prepared);
    for (size_t i = 0U; i < count; ++i) {
        free(canonical_storage[i]);
        free(receipt_storage[i]);
    }
    free(executions);
    free(activities);
    free(body);
    return 0;
}

static int maintenance_noncall(maintenance_fixture *f)
{
    static const uint8_t entry[] = {0x41U, 0U, 0x0bU};
    uint8_t program[32] = {0x71U}, code_hash[32], account[32], wasm[512], payload[2048];
    size_t wasm_length = candidate_module(wasm, entry, sizeof(entry));
    size_t length = deploy_payload(payload, program, f->authority.principal, wasm, wasm_length,
        code_hash, LX_PROGRAMS_ACCOUNT_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f, LX_PROGRAMS_DEPLOY, payload, length, 1U, 6U) == 0);
    memset(payload, 0, sizeof(payload));
    memcpy(payload, program, 32U);
    memcpy(payload + 32U, "LXPA1", 5U);
    memcpy(payload + 37U, f->asset.asset_id, 32U);
    payload[72U] = 5U;
    memcpy(payload + 73U, "vault", 5U);
    CHECK(maintenance_publish(f, LX_PROGRAMS_ACCOUNT, payload, 78U, 1U, 7U) == 0);
    CHECK(lxp_programs_account_derive(program, (const uint8_t *)"vault", 5U, account) == LXP_OK);
    memcpy(payload, program, 32U);
    payload[32U] = 1U;
    memcpy(payload + 33U, account, 32U);
    memcpy(payload + 65U, f->asset.asset_id, 32U);
    memcpy(payload + 97U, f->actor->id, 32U);
    write_u16(payload + 129U, 5U);
    memcpy(payload + 131U, "vault", 5U);
    CHECK(maintenance_publish(f, LX_PROGRAMS_WIND_DOWN, payload, 136U, 1U, 8U) == 0);
    memcpy(payload, program, 32U);
    payload[32U] = 2U;
    memcpy(payload + 33U, program, 32U);
    write_u64(payload + 65U, f->state.next_sequence + 1U);
    CHECK(maintenance_publish(f, LX_PROGRAMS_WIND_DOWN, payload, 73U, 1U, 9U) == 0);
    payload[32U] = 3U;
    CHECK(maintenance_publish(f, LX_PROGRAMS_WIND_DOWN, payload, 33U, 1U, 10U) == 0);
    {
        lxp_send send = {0};
        uint8_t material[144], message[512], digest[32];
        size_t message_length, signature_length = 64U;
        EVP_PKEY *key;
        EVP_MD_CTX *ctx;
        lxp_u128 before = f->recipient->balance;
        memcpy(send.from, f->actor->id, 32U);
        memcpy(send.to, f->recipient->id, 32U);
        memcpy(send.asset, f->asset.asset_id, 32U);
        send.amount.lo = 1U;
        send.sequence = f->actor->next_sequence;
        send.expires_at = 100U;
        write_u64(send.idempotency_key, f->identity->next_sequence + 1U);
        send.idempotency_key[31U] = 1U;
        send.authorization.kind = LXP_AUTH_OWNER;
        send.authorization.network_id = 7U;
        send.authorization.protocol_version = 3U;
        memcpy(send.authorization.controller, send.from, 32U);
        memcpy(material, send.from, 32U);
        memcpy(material + 32U, send.to, 32U);
        memcpy(material + 64U, send.asset, 32U);
        CHECK(lxp_u128_to_be(send.amount, material + 96U) == LXP_OK);
        memcpy(material + 112U, send.idempotency_key, 32U);
        CHECK(lxp_hash_context_value(material, sizeof(material), send.context_hash) == LXP_OK);
        memcpy(send.authorization.signed_context_hash, send.context_hash, 32U);
        memcpy(send.authorization.public_key, f->actor_public_key, 32U);
        CHECK(lxp_send_authorization_message(&send, message, sizeof(message), &message_length) == LXP_OK);
        CHECK(lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message, message_length, digest) == LXP_OK);
        key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, maintenance_actor_seed, 32U);
        ctx = EVP_MD_CTX_new();
        CHECK(key != NULL && ctx != NULL && EVP_DigestSignInit(ctx, NULL, NULL, NULL, key) == 1);
        CHECK(EVP_DigestSign(ctx, send.authorization.signature, &signature_length, digest, 32U) == 1 &&
            signature_length == 64U);
        EVP_MD_CTX_free(ctx);
        EVP_PKEY_free(key);
        CHECK(lxp_send_encode(&send, payload, sizeof(payload), &length) == LXP_OK);
        CHECK(maintenance_publish(f, LX_ASSET_SEND, payload, length, 1U, 11U) == 0);
        CHECK(lxp_u128_add(before, send.amount, &before) == LXP_OK);
        CHECK(lxp_u128_cmp(f->recipient->balance, before) == 0);
    }
    CHECK(f->state.next_sequence == 86U && f->receipt_authority.last_global_sequence == 85U);
    return 0;
}

static uint64_t credit_now_ms(const lxp_bridge_credit *credit)
{
    uint64_t seconds = 0U;
    for (size_t index = 0U; index < 8U; ++index)
        seconds = (seconds << 8U) | credit->proof[29U + index];
    return seconds * 1000U;
}

static int maintenance_bridge(const char *manifest_path, const char *activity_path,
    const char *evidence_directory)
{
    maintenance_fixture *f = calloc(1U, sizeof(*f));
    lxp_genesis_manifest *manifest = calloc(1U, sizeof(*manifest));
    lxp_activity activity;
    lxp_bridge_profile profile;
    lxp_bridge_credit credit;
    lx_account *recipient;
    uint8_t *manifest_bytes, *activity_bytes;
    size_t manifest_length, activity_length;
    uint8_t name[LX_ACCOUNT_NAME_MAX], grant[32] = {0}, nullifier[32];
    size_t name_length;
    lxp_u128 amount;
    bool present;
    CHECK(f != NULL && manifest != NULL);
    CHECK(read_file(manifest_path, LXP_GENESIS_MAX_ENCODED_BYTES, false,
        &manifest_bytes, &manifest_length) == 0);
    CHECK(read_file(activity_path, LXP_MAX_ACTIVITY_BYTES, false,
        &activity_bytes, &activity_length) == 0);
    f->storage = malloc(4U * LXP_MAX_BATCH_BODY_BYTES);
    CHECK(f->storage != NULL && lxp_arena_init(&f->arena, f->storage,
        4U * LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
    CHECK(lxp_genesis_parse(manifest_bytes, manifest_length, LXP_GENESIS_INPUT_MANIFEST, manifest) == LXP_OK);
    CHECK(lxp_genesis_verify_signature(manifest, &f->arena) == LXP_OK);
    CHECK(lxp_bridge_genesis_profile(manifest, &profile, &present) == LXP_OK && present);
    CHECK(lxp_activity_decode(activity_bytes, activity_length, &activity) == LXP_OK);
    CHECK(lxp_activity_verify_signature(&activity) == LXP_OK && activity.activity_type == LXP_BRIDGE_CREDIT);
    CHECK(lxp_bridge_credit_parse(activity.payload.bytes, activity.payload.length, &credit) == LXP_OK);
    CHECK(lxp_bridge_credit_verify(&profile, &credit, manifest->network_id, 3U, NULL, credit_now_ms(&credit), nullifier, NULL) == LXP_OK);
    CHECK(lxp_u128_from_be(credit.bytes + 191U, &amount) == LXP_OK);
    CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    CHECK(lxp_state_store_bind_accounts(&f->state, &f->accounts) == LXP_OK);
    CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal, manifest, 1U) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, programs_module_registration_v4()) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lx_asset_module_iface()) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lxp_governance_module_iface()) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lxp_bridge_module_iface()) == LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    CHECK(lxp_genesis_materialize(manifest, &f->arena, &f->kernel) == LXP_OK);
    for (size_t i = 0U; i < f->accounts.count; ++i)
        CHECK(lxp_u128_is_zero(f->accounts.accounts[i].balance));
    memcpy(f->asset.asset_id, profile.bytes + 97U, 32U);
    f->asset.registered = true;
    memcpy(f->asset_record.asset_id, f->asset.asset_id, 32U);
    f->asset_runtime = (lx_asset_runtime){&f->accounts, &f->asset_record, 1U,
        &f->asset, 1U, manifest->network_id, 3U};
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_ASSET, &f->asset_runtime) == LXP_OK);
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    CHECK(lxp_programs_fee_governance_resolve_runtime(&f->kernel, 0U,
        &f->runtime.fee_schedule, f->runtime.occupancy_asset_id) == LXP_OK);
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS, &f->runtime) == LXP_OK);
    CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    CHECK(memcmp(f->kernel.current_state_root, manifest->genesis_state_root, 32U) == 0);
    CHECK(activity.authority.length == 32U);
    CHECK(lxp_identity_register(&f->identities, activity.actor_did.bytes, activity.actor_did.length,
        activity.authority.bytes, &f->identity) == LXP_OK);
    CHECK(activity.actor_did.length <= sizeof(name) - 11U);
    memcpy(name, "agent:", 6U);
    memcpy(name + 6U, activity.actor_did.bytes, activity.actor_did.length);
    name_length = 6U + activity.actor_did.length;
    memcpy(name + name_length, ":main", 5U);
    name_length += 5U;
    CHECK(lx_account_id_from_string(name, name_length, f->authority.principal) == LXP_OK);
    f->scope.activity_ordinal_min = 1U;
    f->scope.activity_ordinal_max = 1U;
    f->scope.maximum_per_activity = (lxp_u128){UINT64_MAX, UINT64_MAX};
    f->scope.maximum_total = f->scope.maximum_per_activity;
    f->scope.maximum_per_period = f->scope.maximum_per_activity;
    f->authority.scope = &f->scope;
    f->authority.kind = LXP_AUTHORITY_OWNER;
    memcpy(f->authority.actor, f->identity->did_id, 32U);
    memcpy(f->authority.verified_key, activity.authority.bytes, 32U);
    CHECK(lxp_authority_hash(f->authority.kind, grant, f->authority.verified_key,
        f->authority.authority_hash) == LXP_OK);
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    CHECK(maintenance_fixture_logs(f, manifest->network_id) == 0);
    f->input_activity = &activity;
    f->evidence_directory = evidence_directory;
    CHECK(maintenance_publish(f, LXP_BRIDGE_CREDIT, activity.payload.bytes,
        activity.payload.length, 1U, 1U) == 0);
    CHECK(lx_account_lookup(&f->accounts, name, name_length, credit.bytes + 107U, &recipient) == LXP_OK);
    CHECK(lxp_u128_cmp(recipient->balance, amount) == 0);
    CHECK(f->state.next_sequence == 3U && f->receipt_authority.last_global_sequence == 2U);
    CHECK(lxp_history_close(&f->history) == LXP_OK);
    CHECK(lxp_log_close(&f->feed_log) == LXP_OK && lxp_log_close(&f->canonical_log) == LXP_OK &&
        lxp_log_close(&f->evidence_log) == LXP_OK && lxp_log_close(&f->authority_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&f->feed_mutex) == 0);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f->storage);
    free(f);
    free(manifest);
    free(manifest_bytes);
    free(activity_bytes);
    puts("real custody credit privately prepared and published with authenticated maintenance");
    return 0;
}

static int maintenance_execution_prestate(void)
{
    maintenance_fixture *f = calloc(1U, sizeof(*f));
    static const uint8_t entry[] = {0x41U, 0U, 0x0bU};
    uint8_t wasm[512], payload[2048], call[STAGED_CALL_FIXTURE_BYTES];
    uint8_t program[32] = {0x31U}, code_hash[32];
    size_t wasm_length = candidate_module(wasm, entry, sizeof(entry));
    size_t length;
    CHECK(f != NULL && maintenance_fixture_open(f) == 0);
    f->execution_prestate_checks = true;
    f->kernel.execution_prestate_capture_enabled = true;
    f->evidence.execution_prestate_enabled = true;
    length = deploy_payload(payload, program, f->authority.principal, wasm, wasm_length,
        code_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f, LX_PROGRAMS_DEPLOY, payload, length, 1U, 1U) == 0);
    length = staged_call_payload(call, program);
    CHECK(length == sizeof(call));
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 2U, 2U) == 0);
    CHECK(f->execution_prestate_crashes == 8U && f->state.next_sequence == 6U &&
        f->receipt_authority.last_global_sequence == 5U);
    CHECK(lxp_history_close(&f->history) == LXP_OK);
    CHECK(lxp_log_close(&f->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&f->canonical_log) == LXP_OK);
    CHECK(lxp_log_close(&f->evidence_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&f->feed_mutex) == 0);
    CHECK(lxp_log_close(&f->authority_log) == LXP_OK);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f->storage);
    free(f);
    puts("real maintained deploy and intervening ProgramCall prestates survive sidecar and receipt crash boundaries");
    return 0;
}

int arbiter_legacy_fixture_main(int argc, char **argv)
{
    if (argc == 2 && strcmp(argv[1], "--execution-prestate") == 0)
        return maintenance_execution_prestate();
    if (argc == 3 || argc == 4) return maintenance_bridge(argv[1], argv[2], argc == 4 ? argv[3] : NULL);
    CHECK(argc == 1);
    maintenance_fixture *f = calloc(1U, sizeof(*f));
    static const uint8_t entry[] = {0x41U, 0U, 0x0bU};
    static const uint8_t upgraded_entry[] = {0x41U, 7U, 0x0bU};
    uint8_t wasm[512], upgraded[128], payload[2048], call[STAGED_CALL_FIXTURE_BYTES];
    uint8_t program[32] = {0x31U}, code_hash[32], upgraded_hash[32];
    size_t wasm_length = candidate_module(wasm, entry, sizeof(entry));
    size_t upgraded_length = candidate_module(upgraded, upgraded_entry, sizeof(upgraded_entry));
    size_t length;
    lxp_daemon_receipt_authority_store reopened;
    CHECK(f != NULL && maintenance_fixture_open(f) == 0);
    length = deploy_payload(payload, program, f->authority.principal, wasm, wasm_length,
        code_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f, LX_PROGRAMS_DEPLOY, payload, length, 1U, 1U) == 0);
    length = staged_call_payload(call, program);
    CHECK(length == sizeof(call));
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 1U, 2U) == 0);
    length = upgrade_payload(payload, program, code_hash, upgraded, upgraded_length,
        upgraded_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE, false);
    CHECK(maintenance_publish(f, LX_PROGRAMS_UPGRADE, payload, length, 1U, 3U) == 0);
    length = staged_call_payload(call, program);
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 1U, 4U) == 0);
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 64U, 5U) == 0);
    CHECK(f->state.next_sequence == 74U);
    CHECK(lxp_log_close(&f->authority_log) == LXP_OK);
    CHECK(lxp_log_open_or_create(&f->authority_log, f->authority_path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
    CHECK(lxp_daemon_receipt_authority_open(&reopened, &f->authority_log, &f->authorization) == LXP_OK);
    CHECK(reopened.last_global_sequence == 73U);
    CHECK(lxp_programs_state_feed_store_open(&f->feed, &f->feed_log, &f->canonical_log,
        &f->history, &f->arena, &f->feed_mutex) == LXP_OK);
    CHECK(lxp_programs_state_feed_store_recover(&f->feed, &f->kernel) == LXP_OK);
    CHECK(f->feed.scanned_through_sequence == 73U &&
        memcmp(f->feed.head_state_root, f->kernel.current_state_root, 32U) == 0);
    CHECK(maintenance_noncall(f) == 0);
    CHECK(lxp_history_close(&f->history) == LXP_OK);
    CHECK(lxp_log_close(&f->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&f->canonical_log) == LXP_OK);
    CHECK(lxp_log_close(&f->evidence_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&f->feed_mutex) == 0);
    CHECK(lxp_log_close(&f->authority_log) == LXP_OK);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f->storage);
    free(f);
    puts("real lifecycle maintenance, mixed WAL recovery, combined authority and 64-activity batch passed");
    return 0;
}

static int arbiter_insert(maintenance_fixture *f, uint16_t module,
    const uint8_t *key, size_t key_length, const uint8_t *value, size_t value_length)
{
    lxp_module_ctx ctx;
    CHECK(lxp_module_ctx_init(&ctx, &f->kernel, module, 10U, 1U,
        f->state.next_sequence, UINT64_MAX, &f->arena, true) == LXP_OK);
    CHECK(lxp_ctx_kv_put(&ctx, key, key_length, value, value_length) == LXP_OK);
    CHECK(lxp_module_ctx_prepare_commit(&ctx) == LXP_OK);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    return 0;
}

static int arbiter_close_fixture(maintenance_fixture *f)
{
    CHECK(lxp_history_close(&f->history) == LXP_OK);
    CHECK(lxp_log_close(&f->feed_log) == LXP_OK && lxp_log_close(&f->canonical_log) == LXP_OK &&
        lxp_log_close(&f->evidence_log) == LXP_OK && lxp_log_close(&f->authority_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&f->feed_mutex) == 0);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    lx_account_registry_release(&f->accounts);
    free(f->storage);
    free(f);
    return 0;
}

int arbiter_transport_fixture_main(int argc, char **argv)
{
    maintenance_fixture *f = calloc(1U, sizeof(*f));
    static const uint8_t entry[] = {0x41U, 0U, 0x0bU};
    uint8_t wasm[512], payload[2048], call[STAGED_CALL_FIXTURE_BYTES];
    uint8_t program[32] = {0x31U}, code_hash[32];
    char manifest[4096], populated[4096];
    FILE *manifest_file;
    size_t wasm_length = candidate_module(wasm, entry, sizeof(entry));
    size_t length;
    CHECK(argc == 2 && argv[1][0] == '/' && f != NULL);
    CHECK(maintenance_fixture_open(f) == 0);
    f->kernel.execution_prestate_capture_enabled = true;
    f->evidence_directory = argv[1];
    f->arbiter_first_capture = true;
    f->arbiter_export = true;
    CHECK(snprintf(manifest, sizeof(manifest), "%s/inputs.json", argv[1]) > 0);
    f->arbiter_manifest = fopen(manifest, "wbx");
    CHECK(f->arbiter_manifest != NULL);
    CHECK(fprintf(f->arbiter_manifest, "{\"network_id\":7,\"sequencer_public_key\":\"") > 0);
    for (size_t i = 0U; i < 32U; ++i) CHECK(fprintf(f->arbiter_manifest, "%02x", f->authorization.public_key[i]) > 0);
    CHECK(fprintf(f->arbiter_manifest, "\",\"sequencer_id\":\"") > 0);
    for (size_t i = 0U; i < 32U; ++i) CHECK(fprintf(f->arbiter_manifest, "%02x", f->authorization.sequencer_id[i]) > 0);
    CHECK(fprintf(f->arbiter_manifest, "\",\"first_batch_number\":1,\"last_batch_number\":100,\"captures\":[") > 0);
    length = deploy_payload(payload, program, f->authority.principal, wasm, wasm_length,
        code_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f, LX_PROGRAMS_DEPLOY, payload, length, 1U, 1U) == 0);
    manifest_file = f->arbiter_manifest;
    CHECK(arbiter_close_fixture(f) == 0);
    f = calloc(1U, sizeof(*f));
    arbiter_populated_genesis = true;
    CHECK(f != NULL && maintenance_fixture_open(f) == 0);
    CHECK(snprintf(populated, sizeof(populated), "%s/populated", argv[1]) > 0);
    CHECK(mkdir(populated, 0700) == 0);
    f->kernel.execution_prestate_capture_enabled = true;
    f->evidence_directory = populated;
    f->arbiter_manifest = manifest_file;
    f->arbiter_first_capture = false;
    f->arbiter_export = false;
    length = deploy_payload(payload, program, f->authority.principal, wasm, wasm_length,
        code_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f, LX_PROGRAMS_DEPLOY, payload, length, 1U, 1U) == 0);
    length = staged_call_payload(call, program);
    CHECK(length == sizeof(call));
    f->arbiter_export = true;
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 2U, 2U) == 0);
    f->arbiter_terminal = true;
    CHECK(maintenance_publish(f, LX_PROGRAMS_CALL, call, length, 1U, 3U) == 0);
    CHECK(fprintf(f->arbiter_manifest, "]}\n") > 0);
    CHECK(fclose(f->arbiter_manifest) == 0);
    CHECK(arbiter_close_fixture(f) == 0);
    puts("ARBITER_CASE native-serial-scheduled-terminal-maintained-signatures-wal-recovery");
    return 0;
}

#undef lxp_kernel_prepare_serial_activity_batch_with_arbiter_prestate
#undef lxp_kernel_prepare_activity_batch_with_arbiter_prestate
#undef lxp_kernel_prepare_terminal_rejection_with_arbiter_prestate
#undef lxp_daemon_batch_wal_write_prepared
#undef lxp_kernel_finalize_prepared_batch_publication

static int transport_owner_open(lxp_daemon_protocol_owner *owner,
    lxp_daemon *daemon, maintenance_fixture *fixture)
{
    memset(owner, 0, sizeof(*owner));
    memset(daemon, 0, sizeof(*daemon));
    CHECK(pthread_mutex_init(&owner->publication_mutex, NULL) == 0);
    CHECK(pthread_mutex_init(&owner->mutex, NULL) == 0);
    CHECK(pthread_mutex_init(&owner->receipt_mutex, NULL) == 0);
    CHECK(pthread_mutex_init(&owner->receipt_authority_mutex, NULL) == 0);
    CHECK(pthread_mutex_init(&daemon->mutex, NULL) == 0);
    owner->kernel = &fixture->kernel;
    owner->identities = &fixture->identities;
    owner->programs_runtime = &fixture->runtime;
    owner->history = &fixture->history;
    owner->receipt_authority = &fixture->receipt_authority;
    owner->evidence_store = &fixture->evidence;
    owner->scratch = &fixture->arena;
    owner->feed_store = fixture->feed;
    owner->network_id = 7U;
    owner->protocol_version = 3U;
    owner->latest_sealed_timestamp = 10U;
    owner->attached = true;
    daemon->config.role = LXP_DAEMON_REPLICA;
    daemon->config.network_id = 7U;
    daemon->config.start_sequence = fixture->state.next_sequence;
    daemon->next_sequence = fixture->state.next_sequence;
    daemon->primitives_initialized = true;
    return 0;
}

static int transport_reopen(maintenance_fixture *fixture)
{
    char path[160];
    uint8_t anchor[32];
    memcpy(anchor, fixture->evidence.registry.finalisation.settlement_anchor, 32U);
    CHECK(snprintf(path, sizeof(path), "%s/evidence.log", fixture->directory) > 0);
    CHECK(lxp_log_close(&fixture->evidence_log) == LXP_OK);
    CHECK(lxp_log_open(&fixture->evidence_log, path) == LXP_OK);
    CHECK(lxp_arena_reset(&fixture->arena, 0U) == LXP_OK);
    CHECK(lxp_daemon_evidence_open(&fixture->evidence, &fixture->evidence_log,
        7U, &fixture->authorization, anchor, true, NULL, NULL, &fixture->arena) == LXP_OK);
    fixture->evidence.arbiter_admission_prestate_enabled = true;
    CHECK(lxp_daemon_evidence_arbiter_admission_prestate_ready(&fixture->evidence));
    return 0;
}

int main(int argc, char **argv)
{
    static const uint8_t entry[] = {0x41U, 0U, 0x0bU};
    maintenance_fixture *fixture = calloc(1U, sizeof(*fixture));
    lxp_daemon_protocol_owner *owner = calloc(1U, sizeof(*owner));
    lxp_daemon *daemon = calloc(1U, sizeof(*daemon));
    lxp_daemon_lni_server *server = calloc(1U, sizeof(*server));
    lxp_daemon_lni_configuration configuration = {0};
    uint8_t wasm[512], payload[2048], call[STAGED_CALL_FIXTURE_BYTES];
    uint8_t program[32] = {0x31U}, code_hash[32];
    char manifest[4096], socket_directory[4096], admission_directory[4096], socket_path[4096];
    char command[32];
    size_t wasm_length = candidate_module(wasm, entry, sizeof(entry));
    size_t length;
    CHECK(argc == 2 && argv[1][0] == '/' && geteuid() == 0U);
    CHECK(fixture != NULL && owner != NULL && daemon != NULL && server != NULL);
    CHECK(maintenance_fixture_open(fixture) == 0);
    fixture->kernel.execution_prestate_capture_enabled = true;
    fixture->evidence.arbiter_admission_prestate_enabled = true;
    transport_store = &fixture->evidence;
    transport_arena = &fixture->arena;
    fixture->evidence_directory = argv[1];
    fixture->arbiter_first_capture = true;
    fixture->arbiter_export = true;
    CHECK(snprintf(manifest, sizeof(manifest), "%s/inputs.json", argv[1]) > 0);
    CHECK(snprintf(socket_directory, sizeof(socket_directory), "%s/socket", argv[1]) > 0);
    CHECK(snprintf(admission_directory, sizeof(admission_directory), "%s/admission", argv[1]) > 0);
    CHECK(snprintf(socket_path, sizeof(socket_path), "%s/lni.sock", socket_directory) > 0);
    CHECK(mkdir(socket_directory, 0750) == 0 && chown(socket_directory, 0U, 65534U) == 0);
    CHECK(chmod(socket_directory, 0750) == 0 && mkdir(admission_directory, 0700) == 0);
    fixture->arbiter_manifest = fopen(manifest, "wbx");
    CHECK(fixture->arbiter_manifest != NULL);
    CHECK(fprintf(fixture->arbiter_manifest, "{\"network_id\":7,\"endpoint\":\"%s\",\"sequencer_public_key\":\"", socket_path) > 0);
    for (size_t index = 0U; index < 32U; ++index)
        CHECK(fprintf(fixture->arbiter_manifest, "%02x", fixture->authorization.public_key[index]) > 0);
    CHECK(fprintf(fixture->arbiter_manifest, "\",\"sequencer_id\":\"") > 0);
    for (size_t index = 0U; index < 32U; ++index)
        CHECK(fprintf(fixture->arbiter_manifest, "%02x", fixture->authorization.sequencer_id[index]) > 0);
    CHECK(fprintf(fixture->arbiter_manifest, "\",\"first_batch_number\":1,\"last_batch_number\":100,\"captures\":[") > 0);
    length = deploy_payload(payload, program, fixture->authority.principal, wasm,
        wasm_length, code_hash, LX_PROGRAMS_ABI_VERSION, INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(fixture, LX_PROGRAMS_DEPLOY, payload, length, 1U, 1U) == 0);
    length = staged_call_payload(call, program);
    CHECK(length == sizeof(call));
    CHECK(maintenance_publish(fixture, LX_PROGRAMS_CALL, call, length, 2U, 2U) == 0);
    fixture->arbiter_terminal = true;
    CHECK(maintenance_publish(fixture, LX_PROGRAMS_CALL, call, length, 1U, 3U) == 0);
    CHECK(fprintf(fixture->arbiter_manifest, "]}\n") > 0);
    CHECK(fclose(fixture->arbiter_manifest) == 0);
    CHECK(transport_owner_open(owner, daemon, fixture) == 0);
    configuration.socket_path = socket_path;
    configuration.admission_directory = admission_directory;
    configuration.allowed_peer_uid = 65534U;
    configuration.allowed_peer_gid = 65534U;
    configuration.frame_bytes = LXP_DAEMON_LNI_MAX_FRAME_BYTES;
    configuration.deadline_milliseconds = 10000U;
    configuration.socket_mode = 0660U;
    CHECK(lxp_daemon_lni_serve(server, daemon, owner, &configuration) == LXP_OK);
    puts("ADMISSION_TRANSPORT_READY before");
    CHECK(fflush(stdout) == 0);
    while (fgets(command, sizeof(command), stdin) != NULL) {
        CHECK(lxp_daemon_lni_stop(server) == LXP_OK);
        if (strcmp(command, "stop\n") == 0) break;
        CHECK(strcmp(command, "reopen\n") == 0);
        CHECK(transport_reopen(fixture) == 0);
        CHECK(lxp_daemon_lni_serve(server, daemon, owner, &configuration) == LXP_OK);
        puts("ADMISSION_TRANSPORT_READY after");
        CHECK(fflush(stdout) == 0);
    }
    CHECK(pthread_mutex_destroy(&owner->publication_mutex) == 0);
    CHECK(pthread_mutex_destroy(&owner->mutex) == 0);
    CHECK(pthread_mutex_destroy(&owner->receipt_mutex) == 0);
    CHECK(pthread_mutex_destroy(&owner->receipt_authority_mutex) == 0);
    CHECK(pthread_mutex_destroy(&daemon->mutex) == 0);
    CHECK(arbiter_close_fixture(fixture) == 0);
    free(owner); free(daemon); free(server);
    puts("ADMISSION_TRANSPORT native-retained-reopened-authenticated-boundary");
    return 0;
}
