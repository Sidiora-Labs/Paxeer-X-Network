#define _POSIX_C_SOURCE 200809L
#include "layerx/programs.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_maintenance.h"
#include "layerx/lxp_hash.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static lxp_result module_registration_execute(lxp_kernel *, const lxp_activity *,
    lxp_kernel_execution *, lxp_receipt *);
static lxp_result module_registration_identity(lxp_identity_store *, const uint8_t *, size_t, const uint8_t *, lxp_identity **);
static const lxp_byte_span *module_registration_events(const lxp_kernel_prepared_batch *);
static int module_registration_state(lxp_kernel *, const uint8_t *,
    const lxp_sequencer_authorization *, const lxp_batch_header *,
    const lxp_byte_span *, lxp_byte_span, const uint8_t *);
static lxp_byte_span observed_events;
static size_t observed_batches;
static size_t observed_failures;
static size_t observed_resources;

#define main retained_registration_main
#include "test_registration.c"
#undef main
#define main retained_call_activity_main
#define lxp_identity_register module_registration_identity
#define lxp_kernel_execute_activity module_registration_execute
#define lxp_kernel_prepared_batch_events module_registration_events
#define LXP_TEST_PROGRAM_STATE_OBSERVER module_registration_state
#include "test_call_activity.c"
#undef LXP_TEST_PROGRAM_STATE_OBSERVER
#undef lxp_kernel_prepared_batch_events
#undef lxp_kernel_execute_activity
#undef lxp_identity_register
#undef main

static lxp_result module_registration_identity(lxp_identity_store *store,
    const uint8_t *did, size_t length, const uint8_t *key, lxp_identity **identity)
{
    static const uint8_t seed[32] = {0x33U};
    uint8_t public_key[32];
    (void)key;
    if (executed_public_key(seed, public_key) != 0) return LXP_ERR_BAD_SIGNATURE;
    return lxp_identity_register(store, did, length, public_key, identity);
}

static const lxp_byte_span *module_registration_events(const lxp_kernel_prepared_batch *prepared)
{
    const lxp_byte_span *events = lxp_kernel_prepared_batch_events(prepared);
    if (events != NULL) observed_events = *events;
    return events;
}

static lxp_result module_registration_execute(lxp_kernel *kernel,
    const lxp_activity *activity, lxp_kernel_execution *execution, lxp_receipt *receipt)
{
    lxp_activity signed_activity = *activity;
    uint8_t signature[64], public_key[32];
    const size_t kv_count = kernel->module_kv_count;
    const size_t blob_count = kernel->blob_count;
    const size_t blob_bytes = kernel->blob_total_bytes;
    lxp_module_kv_entry *kv = malloc((kv_count == 0U ? 1U : kv_count) * sizeof(*kv));
    uint8_t (*blob_hashes)[32] = malloc((blob_count == 0U ? 1U : blob_count) * 32U);
    const uint64_t sequence = kernel->state->next_sequence;
    lxp_result status;
    if (kv == NULL || blob_hashes == NULL) { free(kv); free(blob_hashes); return LXP_FATAL_INVARIANT; }
    (void)memcpy(kv, kernel->module_kv, kv_count * sizeof(*kv));
    for (size_t i = 0U; i < blob_count; ++i)
        if (lxp_hash_sha256(kernel->blobs[i].bytes, kernel->blobs[i].length, blob_hashes[i]) != LXP_OK) {
            free(kv); free(blob_hashes); return LXP_FATAL_INVARIANT;
        }
    signed_activity.signature = (lxp_byte_span){signature, sizeof(signature)};
    {
        static const uint8_t seed[32] = {0x33U};
        if (executed_public_key(seed, public_key) != 0) {
            free(kv); free(blob_hashes); return LXP_ERR_BAD_SIGNATURE;
        }
    }
    signed_activity.authority = (lxp_byte_span){public_key, sizeof(public_key)};
    if (lifecycle_vector_signature(&signed_activity, public_key, signature) != 0 ||
        lxp_activity_verify_signature(&signed_activity) != LXP_OK) {
        free(kv); free(blob_hashes); return LXP_ERR_BAD_SIGNATURE;
    }
    {
        lxp_byte_span canonical;
        lxp_batch_roots roots;
        uint8_t preimage[88];
        size_t mark = lxp_arena_mark(execution->arena);
        status = lxp_activity_encode(&signed_activity, execution->arena, &canonical);
        if (status == LXP_OK)
            status = lxp_batch_roots_compute(&(lxp_batch_root_inputs){&canonical, 1U,
                NULL, 0U, NULL, 0U, NULL, 0U, NULL, 0U}, execution->arena, &roots);
        if (status == LXP_OK) {
            (void)memcpy(preimage, kernel->current_state_root, 32U);
            (void)memcpy(preimage + 32U, roots.activity_merkle_root, 32U);
            write_u64(preimage + 64U, execution->global_sequence);
            write_u64(preimage + 72U, execution->global_sequence);
            write_u64(preimage + 80U, execution->batch_number);
            status = lxp_hash_context_value(preimage, sizeof(preimage), execution->batch_id);
            (void)memcpy(execution->activity_root, roots.activity_merkle_root, 32U);
        }
        if (lxp_arena_reset(execution->arena, mark) != LXP_OK) status = LXP_FATAL_INVARIANT;
    }
    if (status == LXP_OK)
        status = lxp_kernel_execute_activity(kernel, &signed_activity, execution, receipt);
    if (status == LXP_OK && receipt->program_outcome.present && receipt->result_code != LXP_OK) {
        if (kernel->module_kv_count != kv_count || kernel->blob_count != blob_count ||
            kernel->blob_total_bytes != blob_bytes || receipt->effects.count != 0U ||
            kernel->state->next_sequence != sequence + 1U || lxp_u128_is_zero(receipt->fee_charged))
            status = LXP_FATAL_INVARIANT;
        for (size_t i = 0U; status == LXP_OK && i < kv_count; ++i)
            if (kv[i].module_id != kernel->module_kv[i].module_id ||
                kv[i].key_length != kernel->module_kv[i].key_length ||
                kv[i].value_length != kernel->module_kv[i].value_length ||
                memcmp(kv[i].key, kernel->module_kv[i].key, kv[i].key_length) != 0 ||
                memcmp(kv[i].value, kernel->module_kv[i].value, kv[i].value_length) != 0)
                status = LXP_FATAL_INVARIANT;
        for (size_t i = 0U; status == LXP_OK && i < blob_count; ++i) {
            uint8_t hash[32];
            if (lxp_hash_sha256(kernel->blobs[i].bytes, kernel->blobs[i].length, hash) != LXP_OK ||
                memcmp(hash, blob_hashes[i], 32U) != 0) status = LXP_FATAL_INVARIANT;
        }
        if (status == LXP_OK && receipt->program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_FAILURE)
            ++observed_failures;
        if (status == LXP_OK && receipt->program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_RESOURCE)
            ++observed_resources;
    }
    free(kv); free(blob_hashes);
    return status;
}

static int module_registration_state(lxp_kernel *kernel, const uint8_t *payload,
    const lxp_sequencer_authorization *authorization, const lxp_batch_header *header,
    const lxp_byte_span *receipts, lxp_byte_span header_bytes, const uint8_t *signature)
{
    lxp_receipt receipt;
    lxp_batch_roots roots;
    lxp_byte_span events[2] = {observed_events, {NULL, 0U}};
    lxp_batch_maintenance maintenance;
    uint8_t arena_bytes[262144];
    lxp_arena arena;
    size_t event_count = 1U;
    (void)payload;
    (void)header_bytes;
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_receipt_decode(receipts[0].bytes, receipts[0].length, true, &receipt) != LXP_OK ||
        receipt.module_id != LXP_MODULE_PROGRAMS || receipt.effects.count == 0U ||
        lxp_receipt_verify(&receipt, authorization->public_key, &arena) != LXP_OK ||
        lxp_batch_verify_signature(header, signature, 64U, authorization, &arena) != LXP_OK ||
        memcmp(kernel->current_state_root, header->resulting_state_root, 32U) != 0)
        return 1;
    for (size_t i = 0U; i < receipt.effects.count; ++i)
        if (receipt.effects.effects[i].module_id != LXP_MODULE_PROGRAMS) return 1;
    if (header->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT) {
        if (lxp_batch_maintenance_decode(receipts[1].bytes, receipts[1].length, &maintenance) != LXP_OK)
            return 1;
        events[1] = maintenance.effects;
        event_count = 2U;
    }
    if (lxp_batch_roots_compute(&(lxp_batch_root_inputs){NULL, 0U, NULL, 0U,
            events, event_count, NULL, 0U, NULL, 0U}, &arena, &roots) != LXP_OK ||
        memcmp(roots.event_merkle_root, header->event_merkle_root, 32U) != 0)
        return 1;
    ++observed_batches;
    return 0;
}

static int module_registration_protocol(uint16_t protocol)
{
    observed_failures = 0U;
    observed_resources = 0U;
    observed_batches = 0U;
    post_upgrade_batch_regression = false;
    if (deploy_and_upgrade_artifacts_case(protocol, false) != 0 ||
        observed_failures != 1U || observed_resources != 1U) return 1;
    post_upgrade_batch_regression = true;
    if (deploy_and_upgrade_artifacts_case(protocol, false) != 0 || observed_batches != 4U)
        return 1;
    post_upgrade_batch_regression = false;
    return 0;
}

int main(int argc, char **argv)
{
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    const lxp_module_registration *resolved = NULL;
    uint64_t parameters = 1U;
    if (argc != 1 || retained_registration_main(argc, argv) != 0 ||
        lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) != LXP_OK ||
        lxp_kernel_module_for_activity(&kernel,
            ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | UINT16_MAX, 0U, &resolved) != LXP_ERR_UNKNOWN_ACTIVITY ||
        lxp_module_version_for_epoch(&kernel, LXP_MODULE_PROGRAMS, 0U, UINT16_MAX, &resolved) != LXP_ERR_VERSION_UNSUPPORTED ||
        lxp_state_store_destroy(&state) != LXP_OK) return 1;
    (void)puts("PROGRAM_MODULE_REGISTRATION_CASE name=versioned_registration");
    if (module_registration_protocol(LXP_PROTOCOL_VERSION) != 0) return 1;
    (void)puts("PROGRAM_MODULE_REGISTRATION_CASE name=protocol2_execution");
    if (module_registration_protocol(LXP_PROTOCOL_VERSION_STATE_COMMITMENT) != 0) return 1;
    (void)puts("PROGRAM_MODULE_REGISTRATION_CASE name=protocol3_execution");
    return 0;
}
