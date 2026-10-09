#define _POSIX_C_SOURCE 200809L
#include "layerx/programs.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_genesis.h"
#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define ABI5_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "programs ABI 5 admission check failed at line %d\n", __LINE__); \
    return 1; } } while (0)

enum {
    DEPLOY_HEADER_BYTES = 104,
    UPGRADE_HEADER_BYTES = 106,
    ACCOUNT_HEADER_BYTES = 73
};

typedef struct fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    lxp_effect_buffer effects;
    uint64_t parameters;
    uint32_t module_version;
    uint8_t program[32], second_program[32];
    uint8_t keys[2][32], principals[2][32], account_ids[2][32];
    uint8_t sequencer_key[32];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_receipt receipt;
} fixture;

static const char *const dids[2] = {
    "did:lxp:abi5-owner", "did:lxp:abi5-outsider"
};
static const uint8_t seeds[2][32] = {{0x71U}, {0x72U}};
static const uint8_t sequencer_seed[32] = {0x73U};
static const uint8_t asset_id[32] = {9U};
static const uint8_t profile_seed[] = "abi5/profile";
static const uint8_t guest_seed[] = "abi5/guest";
static const uint8_t legacy_seed[] = "abi5/legacy";
static const uint8_t account_profile2_magic[] = {'L', 'X', 'P', 'A', '2'};

static const uint8_t guest_wasm[] = {
    0x00U, 0x61U, 0x73U, 0x6dU, 0x01U, 0x00U, 0x00U, 0x00U,
    0x01U, 0x0cU, 0x02U, 0x60U, 0x01U, 0x7fU, 0x01U, 0x7fU,
    0x60U, 0x02U, 0x7fU, 0x7fU, 0x01U, 0x7fU,
    0x03U, 0x03U, 0x02U, 0x00U, 0x01U,
    0x05U, 0x04U, 0x01U, 0x01U, 0x01U, 0x01U,
    0x07U, 0x29U, 0x03U,
    0x0eU, 'l', 'a', 'y', 'e', 'r', 'x', '_', 'r', 'e', 's', 'e', 'r', 'v', 'e',
    0x00U, 0x00U,
    0x0bU, 'l', 'a', 'y', 'e', 'r', 'x', '_', 'c', 'a', 'l', 'l', 0x00U, 0x01U,
    0x06U, 'm', 'e', 'm', 'o', 'r', 'y', 0x02U, 0x00U,
    0x0aU, 0x0bU, 0x02U,
    0x04U, 0x00U, 0x41U, 0x00U, 0x0bU,
    0x04U, 0x00U, 0x41U, 0x00U, 0x0bU
};

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

static const lxp_module_iface *programs_registration_abi6(void)
{
    static lxp_module_iface iface;
    iface = *programs_module_registration_v5();
    iface.abi_version = LX_PROGRAMS_GUEST_ABI_V5_VERSION + 1U;
    return &iface;
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

static int initialize(fixture *f, const lxp_module_iface *registration)
{
    uint8_t treasury_id[32];
    static const uint8_t treasury_name[] = "system:fees";
    lx_account *account;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters genesis = {0};
    size_t i;
    f->parameters = 1U;
    f->module_version = registration->abi_version;
    (void)memset(f->program, 0x51, 32U);
    (void)memset(f->second_program, 0x52, 32U);
    if (lx_account_registry_init(&f->accounts) != LXP_OK) return 1;
    for (i = 0U; i < 2U; ++i) {
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
        lxp_kernel_register_module(&f->kernel, registration) != LXP_OK)
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

static int teardown(fixture *f)
{
    int failed = 0;
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    if (lxp_state_store_destroy(&f->state) != LXP_OK) failed = 1;
    lx_account_registry_release(&f->accounts);
    free(f);
    return failed;
}

static lxp_result execute(fixture *f, uint32_t type, const uint8_t *payload, size_t length)
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
    if (lxp_arena_reset(&f->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = 7U;
    activity.activity_type = type;
    activity.actor_did = (lxp_byte_span){(const uint8_t *)dids[0], strlen(dids[0])};
    activity.authority = (lxp_byte_span){f->keys[0], 32U};
    activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity.account_sequence = f->identities.identities[0].next_sequence;
    write_u64(activity.idempotency_key + 24U, f->state.next_sequence);
    activity.payload = (lxp_byte_span){payload, length};
    activity.fee_limit = (lxp_u128){0U, UINT64_C(10000000000)};
    if (lxp_hash_payload(payload, length, activity.payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(&activity, digest) != LXP_OK ||
        sign_bytes(seeds[0], digest, 32U, signature) != 0) return LXP_ERR_BAD_SIGNATURE;
    activity.signature = (lxp_byte_span){signature, 64U};
    if (lxp_activity_verify_signature(&activity) != LXP_OK) return LXP_ERR_BAD_SIGNATURE;
    result = lxp_authority_resolve_activity(&f->kernel, &f->identities.identities[0],
        &activity, true, true, 10U, 100U, f->state.next_sequence, &grant, &authority);
    if (result != LXP_OK) return result;
    lxp_authority_allowance_bind(&grant, &authority, &allowance);
    execution.network_id = 7U;
    execution.epoch = f->kernel.epoch;
    execution.batch_number = 10U;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.global_sequence = f->state.next_sequence;
    execution.recorded_module_version = f->module_version;
    execution.recorded_metering_schedule_version = 1U;
    execution.recorded_fee_schedule_version = 1U;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &f->identities;
    execution.authority = &authority;
    execution.allowance = &allowance;
    execution.fee_parameters = &f->fees;
    for (size_t index = 0U; index < f->accounts.count; ++index)
        if (memcmp(f->accounts.accounts[index].id, f->account_ids[0], 32U) == 0)
            execution.fee_balance = f->accounts.accounts[index].balance;
    execution.gas_limit = UINT64_C(1000000000);
    execution.arena = &f->arena;
    execution.sequencer_private_key = sequencer_seed;
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

static lxp_result deploy(fixture *f, const uint8_t program[32], uint16_t abi_version)
{
    uint8_t payload[DEPLOY_HEADER_BYTES + sizeof(guest_wasm)] = {0U};
    (void)memcpy(payload, program, 32U);
    write_u16(payload + 32U, abi_version);
    payload[34U] = 1U;
    (void)memcpy(payload + 36U, f->principals[0], 32U);
    if (lxp_hash_sha256(guest_wasm, sizeof(guest_wasm), payload + 68U) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    write_u32(payload + 100U, (uint32_t)sizeof(guest_wasm));
    (void)memcpy(payload + DEPLOY_HEADER_BYTES, guest_wasm, sizeof(guest_wasm));
    return execute(f, LX_PROGRAMS_DEPLOY, payload, sizeof(payload));
}

static lxp_result upgrade(fixture *f, const uint8_t program[32], uint16_t abi_version)
{
    uint8_t payload[UPGRADE_HEADER_BYTES + sizeof(guest_wasm)] = {0U};
    (void)memcpy(payload, program, 32U);
    write_u16(payload + 32U, abi_version);
    if (lxp_hash_sha256(guest_wasm, sizeof(guest_wasm), payload + 36U) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(payload + 68U, payload + 36U, 32U);
    write_u32(payload + 102U, (uint32_t)sizeof(guest_wasm));
    (void)memcpy(payload + UPGRADE_HEADER_BYTES, guest_wasm, sizeof(guest_wasm));
    return execute(f, LX_PROGRAMS_UPGRADE, payload, sizeof(payload));
}

static int ctx_open(fixture *f, lxp_module_ctx *ctx, uint16_t protocol_version)
{
    if (lxp_arena_reset(&f->arena, 0U) != LXP_OK ||
        lxp_state_journal_open(&f->state, f->state.next_sequence, &f->journal) != LXP_OK ||
        lxp_module_ctx_init(ctx, &f->kernel, LXP_MODULE_PROGRAMS, 10U, f->kernel.epoch,
                            f->state.next_sequence, 100000U, &f->arena, true) != LXP_OK ||
        lxp_effect_buffer_init(&f->effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(ctx, &f->effects) != LXP_OK)
        return 1;
    ctx->protocol_version = protocol_version;
    return 0;
}

static int ctx_commit(fixture *f, lxp_module_ctx *ctx)
{
    return lxp_module_ctx_prepare_commit(ctx) != LXP_OK ||
        lxp_state_journal_commit(&f->journal) != LXP_OK ||
        lxp_module_ctx_commit(ctx) != LXP_OK ||
        lxp_state_root(&f->kernel, f->kernel.current_state_root) != LXP_OK;
}

static int ctx_discard(fixture *f, lxp_module_ctx *ctx)
{
    lxp_module_ctx_rollback(ctx);
    return lxp_state_journal_rollback(&f->journal) != LXP_OK;
}

static void owner_authority(const fixture *f, lxp_authority_resolved *authority)
{
    (void)memset(authority, 0, sizeof(*authority));
    (void)memcpy(authority->actor, f->principals[0], 32U);
    (void)memcpy(authority->principal, f->principals[0], 32U);
    (void)memcpy(authority->verified_key, f->keys[0], 32U);
    authority->kind = LXP_AUTHORITY_OWNER;
}

static const lx_account *account_by_id(const fixture *f, const uint8_t id[32])
{
    size_t index;
    for (index = 0U; index < f->accounts.count; ++index)
        if (memcmp(f->accounts.accounts[index].id, id, 32U) == 0)
            return &f->accounts.accounts[index];
    return NULL;
}

static int profile_account_activity(fixture *f, const uint8_t program[32],
                                    lxp_result expected)
{
    uint8_t payload[ACCOUNT_HEADER_BYTES + sizeof(profile_seed) - 1U];
    const lxp_module_registration *registration;
    lxp_authority_resolved authority;
    lxp_activity activity = {0};
    lxp_module_ctx ctx;
    lxp_result module_result = LXP_FATAL_INVARIANT;
    (void)memcpy(payload, program, 32U);
    (void)memcpy(payload + 32U, account_profile2_magic, sizeof(account_profile2_magic));
    (void)memcpy(payload + 37U, asset_id, 32U);
    write_u32(payload + 69U, (uint32_t)(sizeof(profile_seed) - 1U));
    (void)memcpy(payload + ACCOUNT_HEADER_BYTES, profile_seed, sizeof(profile_seed) - 1U);
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = 7U;
    activity.activity_type = LX_PROGRAMS_ACCOUNT;
    activity.payload = (lxp_byte_span){payload, sizeof(payload)};
    owner_authority(f, &authority);
    ABI5_CHECK(ctx_open(f, &ctx, LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == 0 &&
        lxp_kernel_module_for_activity(&f->kernel, LX_PROGRAMS_ACCOUNT, f->kernel.epoch,
                                       &registration) == LXP_OK &&
        registration->abi_version == f->module_version &&
        lxp_kernel_dispatch(registration, &ctx, &activity, &authority, &f->effects,
                            &module_result) == LXP_OK &&
        module_result == expected);
    return module_result == LXP_OK ? ctx_commit(f, &ctx) : ctx_discard(f, &ctx);
}

static int register_account(fixture *f, const uint8_t program[32], const uint8_t *seed,
                            size_t seed_length, uint16_t protocol_version,
                            lxp_result expected)
{
    lxp_module_ctx ctx;
    lx_account *account = NULL;
    uint8_t id[32];
    bool created = false;
    ABI5_CHECK(ctx_open(f, &ctx, protocol_version) == 0 &&
        lxp_programs_account_register(&ctx, program, seed, seed_length, asset_id,
                                      &account, &created) == expected);
    if (expected != LXP_OK) return ctx_discard(f, &ctx);
    ABI5_CHECK(created && account != NULL &&
        lxp_programs_account_derive(program, seed, seed_length, id) == LXP_OK &&
        memcmp(account->id, id, 32U) == 0 && ctx_commit(f, &ctx) == 0 &&
        account_by_id(f, id) != NULL);
    return 0;
}

static int deploy_admitted(fixture *f, const uint8_t program[32], uint16_t abi_version)
{
    lxp_module_ctx ctx;
    uint16_t recorded = 0U;
    ABI5_CHECK(deploy(f, program, abi_version) == LXP_OK &&
        f->receipt.result_code == LXP_OK &&
        f->receipt.module_version == f->module_version &&
        ctx_open(f, &ctx, LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == 0 &&
        lxp_programs_program_abi(&ctx, program, &recorded) == LXP_OK &&
        recorded == abi_version && ctx_discard(f, &ctx) == 0);
    return 0;
}

static int guest_validation(fixture *f, const uint8_t program[32], uint16_t protocol_version,
                            uint16_t abi_version, lxp_result expected)
{
    lxp_module_ctx ctx;
    ABI5_CHECK(ctx_open(f, &ctx, protocol_version) == 0 &&
        lxp_programs_account_guest_validate(&ctx, program, abi_version) == expected &&
        ctx_discard(f, &ctx) == 0);
    return 0;
}

static int module_validation_matrix(void)
{
    static const struct {
        const lxp_module_iface *(*registration)(void);
        lxp_result occupancy;
        lxp_result state_commitment;
    } rows[] = {
        {programs_module_registration, LXP_ERR_VERSION_UNSUPPORTED, LXP_ERR_VERSION_UNSUPPORTED},
        {programs_module_registration_v2, LXP_OK, LXP_OK},
        {programs_module_registration_v3, LXP_OK, LXP_OK},
        {programs_module_registration_v4, LXP_ERR_VERSION_UNSUPPORTED, LXP_OK},
        {programs_module_registration_v4_storage_retirement, LXP_ERR_VERSION_UNSUPPORTED, LXP_OK},
        {programs_module_registration_v5, LXP_ERR_VERSION_UNSUPPORTED, LXP_OK},
        {programs_registration_abi6, LXP_ERR_VERSION_UNSUPPORTED, LXP_ERR_VERSION_UNSUPPORTED}
    };
    static const uint16_t protocols[2] = {
        LXP_PROTOCOL_VERSION_OCCUPANCY, LXP_PROTOCOL_VERSION_STATE_COMMITMENT
    };
    size_t row, column;
    for (row = 0U; row < sizeof(rows) / sizeof(rows[0]); ++row) {
        fixture *f = calloc(1U, sizeof(*f));
        int failed = f == NULL || initialize(f, rows[row].registration()) != 0;
        for (column = 0U; !failed && column < 2U; ++column) {
            const lxp_result expected = column == 0U ? rows[row].occupancy :
                                                       rows[row].state_commitment;
            lxp_module_ctx ctx;
            failed = ctx_open(f, &ctx, protocols[column]) != 0 ||
                lxp_programs_account_module_validate(&ctx) != expected ||
                lxp_programs_account_guest_validate(
                    &ctx, f->program, LX_PROGRAMS_ACCOUNT_ABI_VERSION) != expected ||
                ctx_discard(f, &ctx) != 0;
        }
        if (f != NULL && teardown(f) != 0) failed = 1;
        ABI5_CHECK(!failed);
    }
    return 0;
}

static int abi5_registered_before_deploy(fixture *f)
{
    lxp_module_ctx ctx;
    uint8_t profile[33];
    uint16_t recorded = 0U;
    ABI5_CHECK(f->module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION);
    ABI5_CHECK(deploy_admitted(f, f->program, LX_PROGRAMS_ACCOUNT_ABI_VERSION) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_ACCOUNT_ABI_VERSION, LXP_OK) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(register_account(f, f->program, legacy_seed, sizeof(legacy_seed) - 1U,
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, LXP_OK) == 0);
    ABI5_CHECK(profile_account_activity(f, f->program, LXP_OK) == 0);
    ABI5_CHECK(ctx_open(f, &ctx, LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == 0 &&
        lxp_programs_account_profile_read(&ctx, f->program, profile) == LXP_OK &&
        profile[0] == 2U && memcmp(profile + 1U, f->principals[0], 32U) == 0 &&
        ctx_discard(f, &ctx) == 0);
    ABI5_CHECK(upgrade(f, f->program, LX_PROGRAMS_GUEST_ABI_V5_VERSION) == LXP_OK &&
        f->receipt.result_code == LXP_OK &&
        f->receipt.module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION);
    ABI5_CHECK(ctx_open(f, &ctx, LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == 0 &&
        lxp_programs_program_abi(&ctx, f->program, &recorded) == LXP_OK &&
        recorded == LX_PROGRAMS_GUEST_ABI_V5_VERSION && ctx_discard(f, &ctx) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION, LXP_OK) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION + 1U,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_OCCUPANCY,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(register_account(f, f->program, guest_seed, sizeof(guest_seed) - 1U,
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, LXP_OK) == 0);
    ABI5_CHECK(deploy_admitted(f, f->second_program, LX_PROGRAMS_GUEST_ABI_V5_VERSION) == 0);
    ABI5_CHECK(guest_validation(f, f->second_program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    return 0;
}

static int protocol_rule_unchanged(fixture *f)
{
    ABI5_CHECK(deploy_admitted(f, f->program, LX_PROGRAMS_ACCOUNT_ABI_VERSION) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_ACCOUNT_ABI_VERSION, LXP_OK) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_OCCUPANCY,
                                LX_PROGRAMS_ACCOUNT_ABI_VERSION,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(register_account(f, f->program, legacy_seed, sizeof(legacy_seed) - 1U,
                                LXP_PROTOCOL_VERSION_OCCUPANCY,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(register_account(f, f->program, legacy_seed, sizeof(legacy_seed) - 1U,
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, LXP_OK) == 0);
    return 0;
}

static int abi6_refused(fixture *f)
{
    lxp_module_ctx ctx;
    uint16_t recorded = 0U;
    ABI5_CHECK(f->module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION + 1U);
    ABI5_CHECK(deploy(f, f->program, LX_PROGRAMS_ACCOUNT_ABI_VERSION) == LXP_OK &&
        f->receipt.result_code == LXP_ERR_VERSION_UNSUPPORTED);
    ABI5_CHECK(ctx_open(f, &ctx, LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == 0 &&
        lxp_programs_program_abi(&ctx, f->program, &recorded) == LXP_ERR_UNKNOWN_FIELD &&
        ctx_discard(f, &ctx) == 0);
    ABI5_CHECK(register_account(f, f->program, legacy_seed, sizeof(legacy_seed) - 1U,
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    ABI5_CHECK(guest_validation(f, f->program, LXP_PROTOCOL_VERSION_STATE_COMMITMENT,
                                LX_PROGRAMS_GUEST_ABI_V5_VERSION,
                                LXP_ERR_VERSION_UNSUPPORTED) == 0);
    return 0;
}

static int ledger_admission(const lxp_module_iface *registration, uint16_t protocol_version,
                            bool binds)
{
    fixture *f = calloc(1U, sizeof(*f));
    lxp_authority_resolved authority;
    lxp_module_ctx ctx;
    const lx_account *account;
    uint64_t legacy, sequence = 0U;
    int failed = f == NULL || initialize(f, registration) != 0 ||
        ctx_open(f, &ctx, protocol_version) != 0;
    if (!failed) {
        owner_authority(f, &authority);
        (void)memset(ctx.activity_id, 0x5a, 32U);
        account = account_by_id(f, f->account_ids[0]);
        legacy = account == NULL ? 0U : account->next_sequence + 1000U;
        failed = account == NULL ||
            lxp_kernel_bind_ledger_admission(&ctx, &authority, LX_PROGRAMS_CALL) != LXP_OK ||
            ctx.ledger_admission.bound != binds ||
            lxp_ctx_ledger_execution_sequence(&ctx, f->account_ids[0], legacy,
                                              &sequence) != LXP_OK;
        if (!failed && binds)
            failed = ctx.ledger_admission.activity_type != LX_PROGRAMS_CALL ||
                !ctx.ledger_admission.account_present ||
                memcmp(ctx.ledger_admission.account_id, f->account_ids[0], 32U) != 0 ||
                memcmp(ctx.ledger_admission.activity_binding, ctx.activity_id, 32U) != 0 ||
                memcmp(ctx.ledger_admission.actor, f->principals[0], 32U) != 0 ||
                ctx.ledger_admission.next_sequence != account->next_sequence ||
                sequence != account->next_sequence ||
                lxp_ctx_ledger_execution_sequence(&ctx, f->account_ids[1], legacy,
                                                  &sequence) != LXP_ERR_CONTEXT_MISMATCH ||
                lxp_kernel_bind_ledger_admission(&ctx, &authority, LX_PROGRAMS_CALL) !=
                    LXP_ERR_CONTEXT_MISMATCH;
        else if (!failed)
            failed = sequence != legacy;
        if (ctx_discard(f, &ctx) != 0) failed = 1;
    }
    if (f != NULL && teardown(f) != 0) failed = 1;
    ABI5_CHECK(!failed);
    return 0;
}

static int lifecycle_case(const lxp_module_iface *registration, int (*body)(fixture *))
{
    fixture *f = calloc(1U, sizeof(*f));
    int failed = f == NULL || initialize(f, registration) != 0 || body(f) != 0;
    if (f != NULL && teardown(f) != 0) failed = 1;
    ABI5_CHECK(!failed);
    return 0;
}

int main(void)
{
    ABI5_CHECK(module_validation_matrix() == 0);
    ABI5_CHECK(lifecycle_case(programs_module_registration_v5(),
                              abi5_registered_before_deploy) == 0);
    ABI5_CHECK(lifecycle_case(programs_module_registration_v4(), protocol_rule_unchanged) == 0);
    ABI5_CHECK(lifecycle_case(programs_module_registration_v5(), protocol_rule_unchanged) == 0);
    ABI5_CHECK(lifecycle_case(programs_registration_abi6(), abi6_refused) == 0);
    ABI5_CHECK(ledger_admission(programs_module_registration_v5(),
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, true) == 0);
    ABI5_CHECK(ledger_admission(programs_module_registration_v4(),
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, true) == 0);
    ABI5_CHECK(ledger_admission(programs_module_registration_v5(),
                                LXP_PROTOCOL_VERSION_OCCUPANCY, false) == 0);
    ABI5_CHECK(ledger_admission(programs_module_registration_v4(),
                                LXP_PROTOCOL_VERSION_OCCUPANCY, false) == 0);
    ABI5_CHECK(ledger_admission(programs_module_registration_v3(),
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, false) == 0);
    ABI5_CHECK(ledger_admission(programs_registration_abi6(),
                                LXP_PROTOCOL_VERSION_STATE_COMMITMENT, false) == 0);
    (void)puts("programs ABI 5 admission: 11 cases passed");
    return 0;
}
