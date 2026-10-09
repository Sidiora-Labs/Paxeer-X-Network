#define _POSIX_C_SOURCE 200809L
#include "layerx/programs.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_genesis.h"
#include "../../src/modules/programs/storage.h"
#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void write_u16(uint8_t *out, uint16_t value)
{
    out[0] = (uint8_t)(value >> 8U);
    out[1] = (uint8_t)value;
}

static void write_u32(uint8_t *out, uint32_t value)
{
    out[0] = (uint8_t)(value >> 24U);
    out[1] = (uint8_t)(value >> 16U);
    out[2] = (uint8_t)(value >> 8U);
    out[3] = (uint8_t)value;
}

static void write_u64(uint8_t *out, uint64_t value)
{
    size_t index;
    for (index = 0U; index < 8U; ++index)
        out[index] = (uint8_t)(value >> ((7U - index) * 8U));
}

static lxp_result install_metering_v1(lxp_kernel *kernel)
{
    static const uint8_t active_key[] = "progmet/active/v1";
    static const uint8_t history_key[] = {
        'p', 'r', 'o', 'g', 'm', 'e', 't', '/', 'h', 'i', 's', 't', 'o',
        'r', 'y', '/', 'v', '1', '/', 0U, 0U, 0U, 1U
    };
    static const uint64_t coefficients[9] = {1U, 1U, 1U, 1U, 1U,
                                              8U, 8U, 64U, 8U};
    uint8_t record[LX_PROGRAMS_METERING_RECORD_BYTES] = {0U};
    size_t offset = 0U;
    size_t index;
    if (kernel == NULL || kernel->module_kv_count != 0U)
        return LXP_ERR_NON_CANONICAL;
    (void)memcpy(record + offset, "LXMR1", 5U);
    offset += 5U;
    write_u32(record + offset, 1U);
    offset += 4U;
    for (index = 0U; index < 9U; ++index) {
        write_u64(record + offset, coefficients[index]);
        offset += 8U;
    }
    write_u64(record + offset, 1U);
    offset += 8U;
    record[offset++] = LX_PROGRAMS_METERING_AUTHORITY_GENESIS;
    (void)memset(record + offset, 0xa5, 32U);
    if (offset + 32U != sizeof(record)) return LXP_FATAL_INVARIANT;
    kernel->module_kv[0].module_id = LXP_MODULE_PROGRAMS;
    kernel->module_kv[0].key_length = sizeof(active_key) - 1U;
    kernel->module_kv[0].value_length = sizeof(record);
    (void)memcpy(kernel->module_kv[0].key, active_key,
                 sizeof(active_key) - 1U);
    (void)memcpy(kernel->module_kv[0].value, record, sizeof(record));
    kernel->module_kv[1].module_id = LXP_MODULE_PROGRAMS;
    kernel->module_kv[1].key_length = sizeof(history_key);
    kernel->module_kv[1].value_length = sizeof(record);
    (void)memcpy(kernel->module_kv[1].key, history_key, sizeof(history_key));
    (void)memcpy(kernel->module_kv[1].value, record, sizeof(record));
    kernel->module_kv_count = 2U;
    return LXP_OK;
}

static int executed_public_key(const uint8_t seed[32], uint8_t public_key[32])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    size_t length = 32U;
    int ok = key != NULL && EVP_PKEY_get_raw_public_key(key, public_key, &length) == 1 &&
             length == 32U;
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

typedef struct fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    uint64_t parameters, height;
    uint8_t program[32], keys[3][32], principals[3][32], account_ids[3][32];
    uint8_t rewards[32];
    uint8_t sequencer_key[32];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_receipt receipt;
    lxp_byte_span witness;
} fixture;

static const char *const dids[3] = {
    "did:lxp:paxai-owner", "did:lxp:paxai-treasury", "did:lxp:paxai-outsider"
};
static const uint8_t seeds[3][32] = {{0x63U}, {0x64U}, {0x65U}};
static const uint8_t sequencer_seed[32] = {0x66U};
static const uint8_t asset_id[32] = {9U};
static const uint8_t state_key[] = "paxai/state/v1";
static const uint8_t rewards_seed[] = "paxai/rewards/v1";

static int sign_bytes(const uint8_t seed[32], const uint8_t *bytes, size_t size,
                      uint8_t signature[64])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    size_t length = 64U;
    int ok = key != NULL && ctx != NULL &&
        EVP_DigestSignInit(ctx, NULL, NULL, NULL, key) == 1 &&
        EVP_DigestSign(ctx, signature, &length, bytes, size) == 1 && length == 64U;
    EVP_MD_CTX_free(ctx);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static uint32_t read_be32(const uint8_t *in)
{
    return ((uint32_t)in[0] << 24U) | ((uint32_t)in[1] << 16U) |
           ((uint32_t)in[2] << 8U) | (uint32_t)in[3];
}

static void bytes(uint8_t *out, size_t *n, const void *value, size_t length)
{
    (void)memcpy(out + *n, value, length);
    *n += length;
}

static void integer(uint8_t *out, size_t *n, uint64_t value)
{
    write_u64(out + *n, value);
    *n += 8U;
}

static int initialize(fixture *f)
{
    uint8_t treasury_id[32];
    static const uint8_t treasury_name[] = "system:fees";
    lx_account *account;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters genesis = {0};
    size_t i;
    f->parameters = 1U;
    f->height = 10U;
    (void)memset(f->program, 0x71, 32U);
    if (lx_account_registry_init(&f->accounts) != LXP_OK) return 1;
    for (i = 0U; i < 3U; ++i) {
        char account_name[128];
        lxp_identity *identity;
        int length = snprintf(account_name, sizeof(account_name), "agent:%s:main", dids[i]);
        if (length <= 0 || (size_t)length >= sizeof(account_name) ||
            executed_public_key(seeds[i], f->keys[i]) != 0 ||
            lxp_identity_register(&f->identities, (const uint8_t *)dids[i], strlen(dids[i]),
                                  f->keys[i], &identity) != LXP_OK ||
            lxp_did_id_derive((const uint8_t *)dids[i], strlen(dids[i]), f->principals[i]) != LXP_OK ||
            lx_account_id_from_string((const uint8_t *)account_name, (size_t)length,
                                      f->account_ids[i]) != LXP_OK ||
            lx_account_open(&f->accounts, (const uint8_t *)account_name, (size_t)length,
                            f->account_ids[i], 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
            lxp_ledger_bootstrap_balance(account, asset_id, (lxp_u128){0U, UINT64_C(100000000000000)}, 1U) != LXP_OK)
            return 1;
    }
    if (lx_account_id_from_string(treasury_name, sizeof(treasury_name)-1U, treasury_id) != LXP_OK ||
        lx_account_open(&f->accounts, treasury_name, sizeof(treasury_name)-1U, treasury_id,
                        2U, LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
        lxp_ledger_bootstrap_balance(account, asset_id, (lxp_u128){0U, 0U}, 0U) != LXP_OK ||
        executed_public_key(sequencer_seed, f->sequencer_key) != 0 ||
        lxp_state_store_init(&f->state, 1U) != LXP_OK ||
        lxp_state_store_bind_accounts(&f->state, &f->accounts) != LXP_OK ||
        lxp_kernel_create(&f->kernel, &f->state, &f->journal, &f->parameters, 0U) != LXP_OK ||
        install_metering_v1(&f->kernel) != LXP_OK ||
        lxp_kernel_register_module(&f->kernel, programs_module_registration_v4()) != LXP_OK)
        return 1;
    (void)memcpy(f->asset.asset_id, asset_id, 32U);
    f->asset.registered = true;
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){1U,1U,1U,2U,4U,1U,1U,1U};
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    (void)memcpy(f->runtime.occupancy_asset_id, asset_id, 32U);
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    (void)memcpy(manifest.signer_public_key, f->keys[0], 32U);
    genesis.schedule = f->runtime.fee_schedule;
    (void)memcpy(genesis.occupancy_asset_id, asset_id, 32U);
    genesis.target_occupancy_byte_batches = 3U;
    genesis.response_denominator = 1U;
    genesis.maximum_change_numerator = 1U;
    genesis.maximum_change_denominator = 1U;
    genesis.minimum_fee_units_per_occupancy_byte_batch = 1U;
    genesis.maximum_fee_units_per_occupancy_byte_batch = 10U;
    if (lxp_programs_fee_genesis_append(&manifest, &genesis) != LXP_OK ||
        lxp_programs_fee_genesis_materialize(&manifest, &f->kernel) != LXP_OK) return 1;
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    return lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS, &f->runtime) != LXP_OK ||
        lxp_programs_bind_fee_transaction(&f->kernel) != LXP_OK ||
        lxp_kernel_set_capabilities(&f->kernel, NULL, lxp_kernel_canonical_ledger_apply) != LXP_OK ||
        lxp_state_root(&f->kernel, f->kernel.current_state_root) != LXP_OK ||
        lxp_arena_init(&f->arena, f->arena_bytes, sizeof(f->arena_bytes)) != LXP_OK;
}

static lxp_result execute(fixture *f, unsigned actor, uint32_t type,
                          const uint8_t *payload, size_t length)
{
    lxp_activity activity = {0};
    lxp_authority_resolved authority = {0};
    lxp_authority_grant grant = {0};
    lxp_transfer_allowance allowance = {0};
    lxp_kernel_execution execution = {0};
    lxp_byte_span encoded;
    lxp_batch_roots roots;
    uint8_t signature[64], preimage[88], digest[32];
    lxp_result result;
    f->witness = (lxp_byte_span){NULL, 0U};
    if (lxp_arena_reset(&f->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = 7U;
    activity.activity_type = type;
    activity.actor_did = (lxp_byte_span){(const uint8_t *)dids[actor], strlen(dids[actor])};
    activity.authority = (lxp_byte_span){f->keys[actor], 32U};
    activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity.account_sequence = f->identities.identities[actor].next_sequence;
    write_u64(activity.idempotency_key + 24U, f->state.next_sequence);
    activity.payload = (lxp_byte_span){payload, length};
    activity.fee_limit = (lxp_u128){0U, UINT64_C(10000000000)};
    if (lxp_hash_payload(payload, length, activity.payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(&activity, digest) != LXP_OK ||
        sign_bytes(seeds[actor], digest, 32U, signature) != 0) return LXP_ERR_BAD_SIGNATURE;
    activity.signature = (lxp_byte_span){signature, 64U};
    if (lxp_activity_verify_signature(&activity) != LXP_OK) return LXP_ERR_BAD_SIGNATURE;
    result = lxp_authority_resolve_activity(&f->kernel, &f->identities.identities[actor],
        &activity, true, true, 10U, 100U, f->state.next_sequence, &grant, &authority);
    if (result != LXP_OK) return result;
    lxp_authority_allowance_bind(&grant, &authority, &allowance);
    execution.network_id = 7U;
    execution.epoch = f->kernel.epoch;
    execution.batch_number = f->height;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.global_sequence = f->state.next_sequence;
    execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    execution.recorded_metering_schedule_version = 1U;
    execution.recorded_fee_schedule_version = 1U;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &f->identities;
    execution.authority = &authority;
    execution.allowance = &allowance;
    execution.fee_parameters = &f->fees;
    for (size_t index = 0U; index < f->accounts.count; ++index)
        if (memcmp(f->accounts.accounts[index].id, f->account_ids[actor], 32U) == 0)
            execution.fee_balance = f->accounts.accounts[index].balance;
    execution.gas_limit = UINT64_C(1000000000);
    execution.arena = &f->arena;
    execution.sequencer_private_key = sequencer_seed;
    execution.replay_witness_out = type == LX_PROGRAMS_CALL ? &f->witness : NULL;
    if (lxp_activity_encode(&activity, &f->arena, &encoded) != LXP_OK ||
        lxp_batch_roots_compute(&(lxp_batch_root_inputs){&encoded,1U,NULL,0U,NULL,0U,NULL,0U,NULL,0U},
                                &f->arena, &roots) != LXP_OK) return LXP_FATAL_INVARIANT;
    (void)memcpy(preimage, f->kernel.current_state_root, 32U);
    (void)memcpy(preimage + 32U, roots.activity_merkle_root, 32U);
    write_u64(preimage + 64U, execution.global_sequence);
    write_u64(preimage + 72U, execution.global_sequence);
    write_u64(preimage + 80U, execution.batch_number);
    if (lxp_hash_context_value(preimage, sizeof(preimage), execution.batch_id) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(execution.activity_root, roots.activity_merkle_root, 32U);
    if (lxp_arena_reset(&f->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    (void)memset(&f->receipt, 0, sizeof(f->receipt));
    result = lxp_kernel_execute_activity(&f->kernel, &activity, &execution, &f->receipt);
    if (result == LXP_OK) {
        uint8_t root[32];
        if (lxp_receipt_verify(&f->receipt, f->sequencer_key, &f->arena) != LXP_OK)
            return LXP_ERR_BAD_SIGNATURE;
        if (lxp_state_root(&f->kernel, root) != LXP_OK ||
            memcmp(root, f->receipt.resulting_state_root, 32U) != 0 ||
            memcmp(root, f->kernel.current_state_root, 32U) != 0) return LXP_FATAL_INVARIANT;
    }
    return result;
}

static size_t call_payload(const fixture *f, uint8_t *out, const uint8_t *data, size_t length)
{
    static const uint8_t domain[] = "LXP/program-replay-profile/v1";
    static const uint8_t access[] = "LayerX/programs/access-declaration/v1\0";
    static const uint64_t budget[7] = {100000000U,16777216U,1048576U,1048576U,64U,1048576U,4096U};
    static const uint8_t capabilities[] = {0U, 3U, 3U, 7U, 8U};
    size_t n = 34U, i, original;
    (void)memset(out, 0, 34U);
    bytes(out, &n, domain, sizeof(domain));
    write_u16(out+n, 1U); n += 2U;
    write_u32(out+n, 128U); n += 4U;
    write_u32(out+n, 1048576U); n += 4U;
    original = n + 4U;
    n = original;
    bytes(out, &n, f->program, 32U);
    write_u16(out+n, LX_PROGRAMS_GUEST_ABI_V5_VERSION); n += 2U;
    write_u16(out+n, sizeof("layerx_call")-1U); n += 2U;
    write_u32(out+n, (uint32_t)length); n += 4U;
    write_u16(out+n, (uint16_t)sizeof(capabilities)); n += 2U;
    write_u32(out+n, sizeof(access)); n += 4U;
    write_u32(out+n, 16U); n += 4U;
    for (i = 0U; i < 7U; ++i) integer(out, &n, budget[i]);
    bytes(out, &n, "layerx_call", sizeof("layerx_call")-1U);
    bytes(out, &n, data, length);
    bytes(out, &n, capabilities, sizeof(capabilities));
    bytes(out, &n, access, sizeof(access));
    write_u32(out + original - 4U, (uint32_t)(n - original));
    return n;
}

static const lxp_module_kv_entry *state_head(const fixture *f)
{
    size_t i;
    for (i = 0U; i < f->kernel.module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &f->kernel.module_kv[i];
        if (entry->module_id == LXP_MODULE_PROGRAMS && entry->key_length == 41U &&
            memcmp(entry->key, "progstor", 8U) == 0 &&
            memcmp(entry->key+8U, f->program, 32U) == 0 && entry->key[40U] == 1U &&
            entry->value_length == 38U)
            return entry;
    }
    return NULL;
}

static const lxp_module_blob *find_blob(const fixture *f, const uint8_t key[32], size_t *index)
{
    size_t i;
    for (i = 0U; i < f->kernel.blob_count; ++i) {
        const lxp_module_blob *blob = &f->kernel.blobs[i];
        if (!blob->deleted && blob->module_id == LXP_MODULE_PROGRAMS &&
            memcmp(blob->key, key, 32U) == 0) {
            if (index != NULL) *index = i;
            return blob;
        }
    }
    return NULL;
}

static int shared_state(const fixture *f, const lxp_module_blob **manifest,
                        const lxp_module_blob **value)
{
    const lxp_module_kv_entry *head = state_head(f);
    size_t cursor = 6U, count, i;
    *manifest = NULL;
    *value = NULL;
    if (head == NULL) return 0;
    *manifest = find_blob(f, head->value + 6U, NULL);
    if (*manifest == NULL || (*manifest)->length < 6U) return -1;
    count = read_be32((*manifest)->bytes + 2U);
    for (i = 0U; i < count; ++i) {
        size_t key_length;
        if (cursor + 2U > (*manifest)->length) return -1;
        key_length = ((size_t)(*manifest)->bytes[cursor] << 8U) | (*manifest)->bytes[cursor + 1U];
        cursor += 2U;
        if (cursor + key_length + 36U > (*manifest)->length) return -1;
        if (key_length == sizeof(state_key) - 1U &&
            memcmp((*manifest)->bytes + cursor, state_key, key_length) == 0) {
            *value = find_blob(f, (*manifest)->bytes + cursor + key_length, NULL);
            if (*value == NULL ||
                (*value)->length != read_be32((*manifest)->bytes + cursor + key_length + 32U))
                return -1;
            return 1;
        }
        cursor += key_length + 36U;
    }
    return 0;
}

static uint64_t balance(const fixture *f, const uint8_t id[32])
{
    size_t i;
    for (i = 0U; i < f->accounts.count; ++i)
        if (memcmp(f->accounts.accounts[i].id, id, 32U) == 0 &&
            f->accounts.accounts[i].balance.hi == 0U)
            return f->accounts.accounts[i].balance.lo;
    return UINT64_MAX;
}

static void hex(const uint8_t *data, size_t length)
{
    size_t i;
    if (data == NULL || length == 0U) {
        (void)fputs(" -", stdout);
        return;
    }
    (void)fputc(' ', stdout);
    for (i = 0U; i < length; ++i) (void)printf("%02x", data[i]);
}

static int unhex(const char *text, size_t length, uint8_t *out, size_t capacity)
{
    size_t i;
    if (length % 2U != 0U || length / 2U > capacity) return -1;
    for (i = 0U; i < length; ++i) {
        char c = text[i];
        int v = c >= '0' && c <= '9' ? c - '0' : c >= 'a' && c <= 'f' ? c - 'a' + 10 : -1;
        if (v < 0) return -1;
        if (i % 2U == 0U) out[i / 2U] = (uint8_t)(v << 4); else out[i / 2U] |= (uint8_t)v;
    }
    return (int)(length / 2U);
}

static int call(fixture *f, unsigned actor, const uint8_t *envelope, size_t length, uint8_t *payload)
{
    const lxp_module_blob *manifest, *value;
    size_t blobs_before = f->kernel.blob_count, kv_before = f->kernel.module_kv_count;
    size_t application = 0U, index = 0U;
    uint64_t before = balance(f, f->account_ids[actor]);
    size_t size = call_payload(f, payload, envelope, length);
    lxp_result status = execute(f, actor, LX_PROGRAMS_CALL, payload, size);
    const lxp_program_outcome *outcome = &f->receipt.program_outcome;
    int found = shared_state(f, &manifest, &value);
    if (found < 0) return 1;
    if (found > 0) {
        if (find_blob(f, manifest->key, &index) != NULL && index >= blobs_before) ++application;
        if (find_blob(f, value->key, &index) != NULL && index >= blobs_before) ++application;
    }
    (void)printf("call %d %d %u %u", (int)status, (int)f->receipt.result_code,
                 outcome->present ? (unsigned)outcome->terminal_kind : 0U,
                 outcome->present ? (unsigned)outcome->abi_version : 0U);
    hex(outcome->present ? outcome->terminal_payload.bytes : NULL,
        outcome->present ? outcome->terminal_payload.length : 0U);
    hex(found > 0 ? value->bytes : NULL, found > 0 ? value->length : 0U);
    (void)printf(" %zu %zu %zu %zu %zu %zu %llu %llu %llu %llu",
                 blobs_before, f->kernel.blob_count, application, kv_before,
                 f->kernel.module_kv_count, f->witness.length,
                 (unsigned long long)before,
                 (unsigned long long)balance(f, f->account_ids[actor]),
                 (unsigned long long)(f->receipt.fee_charged.hi == 0U ? f->receipt.fee_charged.lo : UINT64_MAX),
                 (unsigned long long)balance(f, f->rewards));
    hex(outcome->present ? outcome->event_envelope_payload.bytes : NULL,
        outcome->present ? outcome->event_envelope_payload.length : 0U);
    (void)fputc('\n', stdout);
    return fflush(stdout) == 0 ? 0 : 1;
}

static int register_account(fixture *f, const uint8_t *seed, size_t length, uint8_t id[32])
{
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lx_account *account;
    bool created;
    if (lxp_programs_account_derive(f->program, seed, length, id) != LXP_OK ||
        lxp_arena_reset(&f->arena, 0U) != LXP_OK ||
        lxp_state_journal_open(&f->state, f->state.next_sequence, &f->journal) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &f->kernel, LXP_MODULE_PROGRAMS, 10U, 0U,
                            f->state.next_sequence, 100000U, &f->arena, true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    return lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK ||
        lxp_programs_account_register(&ctx, f->program, seed, length, asset_id,
                                      &account, &created) != LXP_OK || !created ||
        lxp_module_ctx_prepare_commit(&ctx) != LXP_OK ||
        lxp_state_journal_commit(&f->journal) != LXP_OK ||
        lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lxp_state_root(&f->kernel, f->kernel.current_state_root) != LXP_OK;
}
static int deploy(fixture *f, const char *path)
{
    FILE *artifact = fopen(path, "rb");
    uint8_t *payload, *upgrade;
    long file_length;
    size_t length;
    lxp_result status;
    int failed;
    if (artifact == NULL || fseek(artifact, 0L, SEEK_END) != 0) return 1;
    file_length = ftell(artifact);
    if (file_length <= 0L || (unsigned long)file_length > LXP_MAX_ACTIVITY_BYTES-106U ||
        fseek(artifact, 0L, SEEK_SET) != 0) return fclose(artifact), 1;
    length = (size_t)file_length;
    payload = calloc(1U, length+104U);
    upgrade = calloc(1U, length+106U);
    if (payload == NULL || upgrade == NULL ||
        fread(payload+104U, 1U, length, artifact) != length) {
        free(payload); free(upgrade);
        return fclose(artifact), 1;
    }
    if (fclose(artifact) != 0) return free(payload), free(upgrade), 1;
    (void)memcpy(payload, f->program, 32U);
    write_u16(payload+32U, LX_PROGRAMS_ACCOUNT_ABI_VERSION);
    payload[34U] = 1U;
    (void)memcpy(payload+36U, f->principals[0], 32U);
    status = lxp_hash_sha256(payload+104U, length, payload+68U);
    write_u32(payload+100U, (uint32_t)length);
    (void)memcpy(upgrade, f->program, 32U);
    write_u16(upgrade+32U, LX_PROGRAMS_GUEST_ABI_V5_VERSION);
    (void)memcpy(upgrade+36U, payload+68U, 32U);
    (void)memcpy(upgrade+68U, payload+68U, 32U);
    write_u32(upgrade+102U, (uint32_t)length);
    (void)memcpy(upgrade+106U, payload+104U, length);
    failed = status != LXP_OK ||
        execute(f, 0U, LX_PROGRAMS_DEPLOY, payload, length+104U) != LXP_OK ||
        f->receipt.result_code != LXP_OK ||
        register_account(f, rewards_seed, sizeof(rewards_seed) - 1U, f->rewards) != 0 ||
        execute(f, 0U, LX_PROGRAMS_UPGRADE, upgrade, length+106U) != LXP_OK ||
        f->receipt.result_code != LXP_OK;
    free(payload); free(upgrade);
    return failed;
}

int main(int argc, char **argv)
{
    fixture *f;
    uint8_t *envelope, *payload;
    char *line = NULL;
    size_t capacity = 0U, i;
    ssize_t read;
    int failed = 0;
    if (argc != 2) return fprintf(stderr, "usage: %s ai_market.wasm\n", argv[0]), 2;
    f = calloc(1U, sizeof(*f));
    envelope = malloc(LXP_MAX_ACTIVITY_BYTES);
    payload = malloc(LXP_MAX_ACTIVITY_BYTES);
    if (f == NULL || envelope == NULL || payload == NULL || initialize(f) != 0 ||
        deploy(f, argv[1]) != 0) {
        (void)fprintf(stderr, "native bring-up failed\n");
        return 2;
    }
    (void)fputs("ready", stdout);
    hex(f->program, 32U);
    for (i = 0U; i < 3U; ++i) hex(f->principals[i], 32U);
    hex(f->rewards, 32U);
    (void)printf(" %u %u %u\n", (unsigned)LXP_KERNEL_MAX_BLOBS,
                 (unsigned)LXP_KERNEL_MAX_STAGED_BLOBS, (unsigned)LXP_KERNEL_MAX_MODULE_KV);
    (void)fflush(stdout);
    while ((read = getline(&line, &capacity, stdin)) > 0) {
        unsigned actor;
        unsigned long long height;
        int offset = 0, length;
        size_t text;
        if (sscanf(line, "%u %llu %n", &actor, &height, &offset) != 2 || actor > 2U ||
            height == 0U || offset <= 0) { failed = 1; break; }
        text = strcspn(line + offset, "\n");
        length = unhex(line + offset, text, envelope, LXP_MAX_ACTIVITY_BYTES / 2U);
        if (length <= 0) { failed = 1; break; }
        f->height = (uint64_t)height;
        if (call(f, actor, envelope, (size_t)length, payload) != 0) { failed = 1; break; }
    }
    free(line);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    if (lxp_state_store_destroy(&f->state) != LXP_OK) failed = 1;
    free(f); free(envelope); free(payload);
    return failed ? 1 : 0;
}
