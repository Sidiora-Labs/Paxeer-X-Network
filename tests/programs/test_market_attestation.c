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
    bool expiry_grant;
    uint8_t program[32], keys[3][32], principals[3][32], account_ids[3][32];
    uint8_t sequencer_key[32];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_receipt receipt;
    lxp_activity last_activity;
    uint8_t last_payload[LXP_MAX_ACTIVITY_BYTES], last_signature[64];
    unsigned failures;
    unsigned verified_receipts, verified_roots, refusal_checks;
    bool refusal_changed_state;
} fixture;

static const char *const dids[3] = {
    "did:lxp:market-provider", "did:lxp:market-tenant", "did:lxp:market-outsider"
};
static const uint8_t seeds[3][32] = {{0x33U}, {0x34U}, {0x35U}};
static const uint8_t sequencer_seed[32] = {0x45U};
static const uint8_t attester_seed[32] = {0x56U};
static const uint8_t asset_id[32] = {9U};
static const uint8_t attester_name[] = "sensor-a";
static uint8_t attester_key[32];

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

static int report(fixture *f, const char *name, int passed, lxp_result code)
{
    if (name != NULL)
        (void)printf("{\"case\":\"%s\",\"passed\":%s,\"result_code\":%d}\n",
                     name, passed ? "true" : "false", (int)code);
    if (!passed) ++f->failures;
    return passed ? 0 : 1;
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

static void amount(uint8_t *out, size_t *n, uint64_t value)
{
    integer(out, n, 0U);
    integer(out, n, value);
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
                          const uint8_t *payload, size_t length, bool replay)
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
    if (replay) activity = f->last_activity;
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
        ++f->verified_receipts;
        if (lxp_state_root(&f->kernel, root) != LXP_OK ||
            memcmp(root, f->receipt.resulting_state_root, 32U) != 0 ||
            memcmp(root, f->kernel.current_state_root, 32U) != 0) return LXP_FATAL_INVARIANT;
        ++f->verified_roots;
    }
    if (!replay) {
        f->last_activity = activity;
        (void)memcpy(f->last_payload, payload, length);
        (void)memcpy(f->last_signature, signature, 64U);
        f->last_activity.payload.bytes = f->last_payload;
        f->last_activity.signature.bytes = f->last_signature;
    }
    return result;
}

static size_t call_payload(fixture *f, uint8_t *out, const uint8_t *data, size_t length,
                           const uint8_t *destination)
{
    static const uint8_t access[] = "LayerX/programs/access-declaration/v1\0";
    static const uint64_t budget[7] = {100000000U,16777216U,1048576U,1048576U,64U,1048576U,4096U};
    uint8_t capabilities[256];
    size_t c = 2U, n = 0U, i;
    write_u16(capabilities, destination == NULL && !f->expiry_grant ? 3U : 4U);
    capabilities[c++] = 3U;
    if (destination != NULL) {
        capabilities[c++] = 5U;
        bytes(capabilities, &c, asset_id, 32U);
        bytes(capabilities, &c, destination, 32U);
        amount(capabilities, &c, 1000U);
    }
    if (f->expiry_grant) {
        uint8_t seed[2] = {'e', 2U}, source[32];
        if (lxp_programs_account_derive(f->program, seed, sizeof(seed), source) != LXP_OK) return 0U;
        capabilities[c++] = 9U;
        bytes(capabilities, &c, f->program, 32U);
        write_u16(capabilities+c, sizeof(seed)); c += 2U;
        bytes(capabilities, &c, seed, sizeof(seed));
        bytes(capabilities, &c, source, 32U);
        bytes(capabilities, &c, asset_id, 32U);
        bytes(capabilities, &c, f->account_ids[1], 32U);
        amount(capabilities, &c, 10U);
    }
    capabilities[c++] = 7U;
    capabilities[c++] = 8U;
    bytes(out, &n, f->program, 32U);
    write_u16(out+n, LX_PROGRAMS_GUEST_ABI_V5_VERSION); n += 2U;
    write_u16(out+n, sizeof("layerx_call")-1U); n += 2U;
    write_u32(out+n, (uint32_t)length); n += 4U;
    write_u16(out+n, (uint16_t)c); n += 2U;
    write_u32(out+n, sizeof(access)); n += 4U;
    write_u32(out+n, 16U); n += 4U;
    for (i = 0U; i < 7U; ++i) integer(out, &n, budget[i]);
    bytes(out, &n, "layerx_call", sizeof("layerx_call")-1U);
    bytes(out, &n, data, length);
    bytes(out, &n, capabilities, c);
    bytes(out, &n, access, sizeof(access));
    return n;
}

static bool business_root(const fixture *f, uint8_t root[32])
{
    size_t i;
    (void)memset(root, 0, 32U);
    for (i = 0U; i < f->kernel.module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &f->kernel.module_kv[i];
        if (entry->module_id == LXP_MODULE_PROGRAMS && entry->key_length == 41U &&
            memcmp(entry->key, "progstor", 8U) == 0 &&
            memcmp(entry->key+8U, f->program, 32U) == 0 && entry->key[40U] == 1U &&
            entry->value_length == 38U) {
            (void)memcpy(root, entry->value+6U, 32U);
            return true;
        }
    }
    return false;
}

static int invoke(fixture *f, const char *name, unsigned actor, const uint8_t *data,
                   size_t length, const uint8_t *destination, bool success)
{
    uint8_t payload[8192], before[32], after[32];
    bool before_found = business_root(f, before);
    size_t size = call_payload(f, payload, data, length, destination);
    lxp_result status = execute(f, actor, LX_PROGRAMS_CALL, payload, size, false);
    bool accepted = status == LXP_OK && f->receipt.result_code == LXP_OK &&
        f->receipt.program_outcome.present &&
        f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS;
    bool refused = status == LXP_OK && f->receipt.result_code != LXP_OK &&
        f->receipt.program_outcome.present &&
        f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_FAILURE;
    if (!success) {
        bool after_found = business_root(f, after);
        ++f->refusal_checks;
        if (!before_found || !after_found || memcmp(before, after, 32U) != 0) {
            f->refusal_changed_state = true;
            refused = false;
        }
    }
    return report(f, name, success ? accepted : refused,
                  status == LXP_OK ? f->receipt.result_code : status);
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
static size_t request_start(uint8_t *out, uint8_t operation)
{
    out[0] = 1U;
    out[1] = operation;
    return 2U;
}

static int provision_lease_instance(fixture *f, uint8_t model, uint8_t instance,
                                    uint8_t lease[32])
{
    uint8_t data[1024], offer[32], stake[32], escrow[32];
    uint8_t stake_seed_bytes[2] = {'s', instance}, escrow_seed[2] = {'e', instance};
    char name[80];
    size_t n;
    (void)memset(offer, 0x80U + instance, 32U);
    (void)memset(lease, 0x90U + instance, 32U);
    if (register_account(f, stake_seed_bytes, sizeof(stake_seed_bytes), stake) != 0 ||
        register_account(f, escrow_seed, sizeof(escrow_seed), escrow) != 0) return 1;
    n = request_start(data, 1U);
    bytes(data, &n, offer, 32U);
    bytes(data, &n, f->principals[0], 32U);
    bytes(data, &n, f->account_ids[0], 32U);
    bytes(data, &n, asset_id, 32U);
    bytes(data, &n, stake, 32U);
    write_u16(data+n, sizeof(stake_seed_bytes)); n += 2U;
    bytes(data, &n, stake_seed_bytes, sizeof(stake_seed_bytes));
    amount(data, &n, 100U);
    amount(data, &n, 1U);
    integer(data, &n, 100U);
    integer(data, &n, 1U);
    integer(data, &n, 100U);
    integer(data, &n, 1000U);
    data[n++] = model;
    (void)snprintf(name, sizeof(name), model == 2U ? "register-offer" : "register-offer-model-%u", model);
    if (invoke(f, instance == model ? name : NULL, 0U, data, n, stake, true) != 0) return 1;
    n = request_start(data, 2U);
    bytes(data, &n, lease, 32U);
    bytes(data, &n, offer, 32U);
    bytes(data, &n, f->principals[1], 32U);
    bytes(data, &n, f->account_ids[1], 32U);
    bytes(data, &n, escrow, 32U);
    write_u16(data+n, sizeof(escrow_seed)); n += 2U;
    bytes(data, &n, escrow_seed, sizeof(escrow_seed));
    integer(data, &n, 10U);
    amount(data, &n, 10U);
    integer(data, &n, 900U);
    (void)snprintf(name, sizeof(name), model == 2U ? "open-lease" : "open-lease-model-%u", model);
    return invoke(f, instance == model ? name : NULL, 1U, data, n, escrow, true);
}

static int provision_lease(fixture *f, uint8_t model, uint8_t lease[32])
{
    return provision_lease_instance(f, model, model, lease);
}

static size_t configure_request(uint8_t *data, const uint8_t lease[32])
{
    size_t n = request_start(data, 6U);
    bytes(data, &n, lease, 32U);
    integer(data, &n, 1U);
    data[n++] = 1U;
    data[n++] = sizeof(attester_name)-1U;
    bytes(data, &n, attester_name, sizeof(attester_name)-1U);
    bytes(data, &n, attester_key, 32U);
    return n;
}

static size_t input_fields(uint8_t *data, size_t n, const uint8_t lease[32], uint32_t ordinal)
{
    uint8_t id[32] = {0}, payload[32], locator[32];
    write_u32(id+28U, ordinal);
    (void)lxp_hash_sha256((const uint8_t *)"temperature=21", 14U, payload);
    (void)lxp_hash_sha256((const uint8_t *)"sensor://real-signed-fixture", 28U, locator);
    bytes(data, &n, lease, 32U);
    bytes(data, &n, id, 32U);
    bytes(data, &n, payload, 32U);
    integer(data, &n, 14U);
    data[n++] = 2U;
    bytes(data, &n, locator, 32U);
    return n;
}

static int policy_digest(fixture *f, const uint8_t lease[32], uint8_t digest[32])
{
    static const uint8_t domain[] = "LXP/market-attesters/v1";
    uint8_t data[256];
    size_t n = 0U;
    bytes(data, &n, domain, sizeof(domain));
    bytes(data, &n, lease, 32U);
    bytes(data, &n, f->principals[1], 32U);
    integer(data, &n, 1U);
    data[n++] = 1U;
    data[n++] = sizeof(attester_name)-1U;
    bytes(data, &n, attester_name, sizeof(attester_name)-1U);
    bytes(data, &n, attester_key, 32U);
    return lxp_hash_sha256(data, n, digest) != LXP_OK;
}

static size_t attestation_request(fixture *f, uint8_t *data, const uint8_t lease[32],
                                  uint8_t ordinal, unsigned mutation)
{
    static const uint8_t domain[] = "LXP/market-attested-input/v1";
    uint8_t statement[512], policy[32], digest[32], signature[64];
    size_t n = request_start(data, 9U), s = 0U;
    if (policy_digest(f, lease, policy) != 0) return 0U;
    n = input_fields(data, n, lease, ordinal);
    integer(data, &n, 10U);
    data[n++] = sizeof(attester_name)-1U;
    bytes(data, &n, attester_name, sizeof(attester_name)-1U);
    bytes(statement, &s, domain, sizeof(domain));
    if (mutation == 1U) policy[0] ^= 1U;
    bytes(statement, &s, policy, 32U);
    integer(statement, &s, mutation == 2U ? 2U : 1U);
    bytes(statement, &s, data+2U, n-2U);
    if (mutation == 3U) statement[0] ^= 1U;
    if (lxp_hash_sha256(statement, s, digest) != LXP_OK ||
        sign_bytes(mutation == 4U ? seeds[2] : attester_seed, digest, 32U, signature) != 0)
        return 0U;
    bytes(data, &n, signature, 64U);
    return n;
}

static size_t usage_request(uint8_t *data, const uint8_t lease[32], const uint8_t root[32])
{
    uint8_t id[32], output[32], state[32];
    size_t n = request_start(data, 10U), i;
    (void)memset(id, 0x61, 32U); id[31] = lease[31];
    (void)memset(output, 0x62, 32U);
    (void)memset(state, 0x63, 32U);
    bytes(data, &n, id, 32U);
    bytes(data, &n, lease, 32U);
    bytes(data, &n, root, 32U);
    bytes(data, &n, output, 32U);
    bytes(data, &n, state, 32U);
    for (i = 0U; i < 6U; ++i) integer(data, &n, 1U);
    amount(data, &n, 1U);
    amount(data, &n, 1U);
    integer(data, &n, 20U);
    return n;
}

typedef struct read_cell {
    const uint8_t *key;
    size_t key_length;
    uint8_t value[2048];
    size_t value_length;
    bool found;
} read_cell;

static lxp_result capture_cell(void *opaque, const uint8_t *key, uint16_t key_length,
                                const uint8_t *value, uint32_t value_length)
{
    read_cell *cell = opaque;
    if (key_length == cell->key_length && memcmp(key, cell->key, key_length) == 0) {
        if (value_length > sizeof(cell->value)) return LXP_FATAL_INVARIANT;
        (void)memcpy(cell->value, value, value_length);
        cell->value_length = value_length;
        cell->found = true;
    }
    return LXP_OK;
}

static int read_shared(fixture *f, const uint8_t *key, size_t length, read_cell *cell)
{
    uint8_t ns[33];
    lxp_module_ctx ctx;
    (void)memcpy(ns, f->program, 32U); ns[32] = 1U;
    (void)memset(cell, 0, sizeof(*cell));
    cell->key = key; cell->key_length = length;
    return lxp_arena_reset(&f->arena, 0U) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &f->kernel, LXP_MODULE_PROGRAMS, 10U, 0U,
                            f->state.next_sequence, 1000000U, &f->arena, false) != LXP_OK ||
        lxp_programs_storage_import(&ctx, ns, sizeof(ns), capture_cell, cell) != LXP_OK ||
        !cell->found;
}

static int require_lease_model(fixture *f, const uint8_t lease[32], uint8_t model)
{
    uint8_t key[48];
    read_cell cell;
    (void)memcpy(key, "lx.market.lease/", 16U);
    (void)memcpy(key+16U, lease, 32U);
    return read_shared(f, key, sizeof(key), &cell) != 0 ||
        cell.value_length < 3U || cell.value[0] != 1U || cell.value[2] != model;
}

static int settlement_root(fixture *f, const uint8_t lease[32], uint8_t root[32])
{
    static const uint8_t committed_domain[] = "LXP/market-committed-set/v1";
    static const uint8_t admitted_domain[] = "LXP/market-admitted-set/v1";
    static const uint8_t accumulator[] = "LXP/market-input-accumulator/v1";
    static const uint8_t settlement[] = "LXP/market-settlement-inputs/v1";
    uint8_t committed[32] = {0}, admitted[32] = {0}, policy[32], leaf[32];
    uint8_t data[512], key[128], id[32] = {0};
    unsigned ordinal;
    size_t n, k;
    read_cell cell;
    if (policy_digest(f, lease, policy) != 0) return 1;
    for (ordinal = 1U; ordinal <= 2U; ++ordinal) {
        n = 0U; bytes(data, &n, committed_domain, sizeof(committed_domain));
        n = input_fields(data, n, lease, (uint8_t)ordinal);
        if (lxp_hash_sha256(data, n, leaf) != LXP_OK) return 1;
        n = 0U; bytes(data, &n, accumulator, sizeof(accumulator));
        bytes(data, &n, committed, 32U); bytes(data, &n, leaf, 32U);
        if (lxp_hash_sha256(data, n, committed) != LXP_OK) return 1;
        k = 0U; bytes(key, &k, "lx.market.input/", 16U);
        bytes(key, &k, lease, 32U); id[31] = (uint8_t)ordinal; bytes(key, &k, id, 32U);
        if (read_shared(f, key, k, &cell) != 0 || cell.value_length < 4U ||
            cell.value[0] != 1U || cell.value[1] != 2U || cell.value[2] != 1U) return 1;
        n = 0U; bytes(data, &n, admitted_domain, sizeof(admitted_domain));
        bytes(data, &n, cell.value, cell.value_length);
        if (lxp_hash_sha256(data, n, leaf) != LXP_OK) return 1;
        n = 0U; bytes(data, &n, accumulator, sizeof(accumulator));
        bytes(data, &n, admitted, 32U); bytes(data, &n, leaf, 32U);
        if (lxp_hash_sha256(data, n, admitted) != LXP_OK) return 1;
    }
    n = 0U; bytes(data, &n, settlement, sizeof(settlement));
    bytes(data, &n, lease, 32U); integer(data, &n, 1U);
    bytes(data, &n, policy, 32U); write_u32(data+n, 2U); n += 4U;
    bytes(data, &n, committed, 32U); bytes(data, &n, admitted, 32U);
    return lxp_hash_sha256(data, n, root) != LXP_OK;
}

static int restore(fixture *source, fixture *restored)
{
    uint8_t *memory = malloc(8U * LXP_MAX_ACTIVITY_BYTES);
    lxp_arena arena;
    lxp_byte_span snapshot;
    lxp_snapshot_manifest_record manifest;
    FILE *persisted;
    uint8_t root[32], loaded[32];
    int failed;
    if (memory == NULL) return 1;
    failed = lxp_arena_init(&arena, memory, 8U * LXP_MAX_ACTIVITY_BYTES) != LXP_OK ||
        lxp_state_root(&source->kernel, root) != LXP_OK ||
        lxp_snapshot_write(&source->kernel, source->state.next_sequence-1U, &arena, &snapshot) != LXP_OK ||
        lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length, source->state.next_sequence-1U,
                                    root, source->kernel.current_state_root, &manifest) != LXP_OK ||
        initialize(restored) != 0;
    if (!failed) {
        persisted = tmpfile();
        failed = persisted == NULL;
        if (persisted != NULL) {
            failed = fwrite(snapshot.bytes, 1U, snapshot.length, persisted) != snapshot.length ||
                fflush(persisted) != 0 || fseek(persisted, 0L, SEEK_SET) != 0 ||
                fread(memory, 1U, snapshot.length, persisted) != snapshot.length;
            if (fclose(persisted) != 0) failed = 1;
        }
    }
    if (!failed) {
        restored->identities = source->identities;
        restored->height = source->height;
        failed = lxp_snapshot_load(memory, snapshot.length, &manifest, &restored->kernel) != LXP_OK ||
            lxp_state_root(&restored->kernel, loaded) != LXP_OK || memcmp(root, loaded, 32U) != 0 ||
            lxp_snapshot_verify_root(&restored->kernel, &manifest) != LXP_OK;
    }
    free(memory);
    return failed;
}
static int distinct_lease_root(fixture *f, uint8_t root[32])
{
    uint8_t lease[32], data[1024];
    size_t n;
    unsigned ordinal;
    if (provision_lease_instance(f, 2U, 4U, lease) != 0) return 1;
    n = configure_request(data, lease);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0) return 1;
    for (ordinal = 1U; ordinal <= 2U; ++ordinal) {
        n = input_fields(data, request_start(data, 7U), lease, ordinal);
        if (invoke(f, NULL, 1U, data, n, NULL, true) != 0) return 1;
    }
    n = request_start(data, 8U); bytes(data, &n, lease, 32U);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0) return 1;
    for (ordinal = 1U; ordinal <= 2U; ++ordinal) {
        n = attestation_request(f, data, lease, (uint8_t)ordinal, 0U);
        if (invoke(f, NULL, 0U, data, n, NULL, true) != 0) return 1;
    }
    return require_lease_model(f, lease, 2U) != 0 || settlement_root(f, lease, root) != 0;
}

static int native_input_bound(fixture *f, const uint8_t lease[32])
{
    fixture *bounded = calloc(1U, sizeof(*bounded));
    uint8_t data[512], payload[8192], before[32], after[32];
    unsigned ordinal;
    lxp_result status = LXP_OK, code = LXP_OK;
    bool passed = false;
    size_t prior_growth = 0U;
    if (bounded == NULL || restore(f, bounded) != 0) return 1;
    for (ordinal = 1U; ordinal <= 1025U; ++ordinal) {
        size_t before_blobs = bounded->kernel.blob_count;
        size_t n = input_fields(data, request_start(data, 7U), lease, ordinal);
        size_t size = call_payload(bounded, payload, data, n, NULL);
        bool found = business_root(bounded, before);
        if (!found) break;
        status = execute(bounded, 1U, LX_PROGRAMS_CALL, payload, size, false);
        code = status == LXP_OK ? bounded->receipt.result_code : status;
        if (code != LXP_OK) {
            passed = code == LXP_ERR_ARENA_EXHAUSTED && ordinal > 1U &&
                prior_growth > 0U && before_blobs + prior_growth > LXP_KERNEL_MAX_BLOBS &&
                bounded->kernel.blob_count == before_blobs &&
                business_root(bounded, after) && memcmp(before, after, 32U) == 0;
            (void)fprintf(stderr, "native input capacity ordinal=%u blobs=%zu limit=%u code=%d\n",
                          ordinal, before_blobs, (unsigned)LXP_KERNEL_MAX_BLOBS, (int)code);
            break;
        }
        if (!bounded->receipt.program_outcome.present ||
            bounded->receipt.program_outcome.terminal_kind != LXP_PROGRAM_TERMINAL_SUCCESS) break;
        prior_growth = bounded->kernel.blob_count - before_blobs;
    }
    while (bounded->kernel.blob_count != 0U) free(bounded->kernel.blobs[--bounded->kernel.blob_count].bytes);
    (void)lxp_state_store_destroy(&bounded->state);
    free(bounded);
    return report(f, "input-bound-refused", passed, code);
}

static int admission_cases(fixture *f, const uint8_t lease[32])
{
    uint8_t data[2048], signed_data[2048], altered[2048], root[32], zero[32] = {0};
    size_t n, signed_length, i;
    lxp_result result;
    fixture *restored;
    static const struct { const char *name; size_t offset; } mutations[] = {
        {"bound-lease-refused", 2U}, {"bound-input-refused", 65U},
        {"bound-payload-refused", 66U}, {"bound-length-refused", 105U},
        {"bound-source-refused", 106U}, {"bound-locator-refused", 107U},
        {"bound-observation-refused", 146U}, {"bound-name-refused", 148U}
    };
    n = configure_request(data, lease);
    (void)memcpy(altered, data, n); altered[42U] = 0U;
    (void)invoke(f, "empty-policy-refused", 1U, altered, 43U, NULL, false);
    (void)memcpy(altered, data, n); altered[42U] = 9U;
    (void)invoke(f, "attester-bound-refused", 1U, altered, n, NULL, false);
    (void)memcpy(altered, data, n); altered[44U] = 'A';
    (void)invoke(f, "invalid-name-refused", 1U, altered, n, NULL, false);
    (void)memcpy(altered, data, n); altered[42U] = 2U;
    (void)memcpy(altered+n, data+43U, n-43U);
    (void)invoke(f, "duplicate-attester-refused", 1U, altered, 2U*n-43U, NULL, false);
    (void)invoke(f, "wrong-tenant-refused", 2U, data, n, NULL, false);
    if (invoke(f, "configure-named-attester", 1U, data, n, NULL, true) != 0) return 1;
    (void)native_input_bound(f, lease);
    signed_length = attestation_request(f, signed_data, lease, 1U, 0U);
    if (signed_length == 0U) return 1;
    n = input_fields(data, request_start(data, 7U), lease, 1U);
    (void)memcpy(altered, data, n); (void)memset(altered+66U, 0, 32U);
    (void)invoke(f, "zero-commitment-refused", 1U, altered, n, NULL, false);
    (void)memcpy(altered, data, n); altered[106U] = 0U;
    (void)invoke(f, "invalid-source-refused", 1U, altered, n, NULL, false);
    if (invoke(f, "commit-input", 1U, data, n, NULL, true) != 0) return 1;
    (void)invoke(f, "duplicate-input-refused", 1U, data, n, NULL, false);
    (void)invoke(f, "unsealed-input-refused", 0U, signed_data, signed_length, NULL, false);
    n = input_fields(data, request_start(data, 7U), lease, 2U);
    if (invoke(f, "commit-second-input", 1U, data, n, NULL, true) != 0) return 1;
    n = request_start(data, 8U); bytes(data, &n, lease, 32U);
    if (invoke(f, "seal-inputs", 1U, data, n, NULL, true) != 0) return 1;
    n = attestation_request(f, data, lease, 3U, 0U);
    (void)invoke(f, "uncommitted-input-refused", 0U, data, n, NULL, false);
    n = input_fields(data, request_start(data, 7U), lease, 3U);
    (void)invoke(f, "late-commit-refused", 1U, data, n, NULL, false);
    n = usage_request(data, lease, attester_key);
    (void)invoke(f, "usage-before-admission-refused", 0U, data, n, NULL, false);
    n = attestation_request(f, data, lease, 2U, 0U);
    (void)invoke(f, "input-order-refused", 0U, data, n, NULL, false);
    (void)invoke(f, "wrong-caller-refused", 2U, signed_data, signed_length, NULL, false);
    (void)invoke(f, "wrong-provider-refused", 1U, signed_data, signed_length, NULL, false);
    (void)memcpy(altered, signed_data, signed_length); altered[148U] = 'x';
    (void)invoke(f, "unnamed-attester-refused", 0U, altered, signed_length, NULL, false);
    (void)memcpy(altered, signed_data, signed_length); altered[signed_length-1U] ^= 1U;
    (void)invoke(f, "bad-signature-refused", 0U, altered, signed_length, NULL, false);
    (void)invoke(f, "malformed-truncated-refused", 0U, signed_data, signed_length-1U, NULL, false);
    (void)memcpy(altered, signed_data, signed_length); altered[signed_length] = 0U;
    (void)invoke(f, "malformed-trailing-refused", 0U, altered, signed_length+1U, NULL, false);
    for (i = 0U; i < sizeof(mutations)/sizeof(mutations[0]); ++i) {
        (void)memcpy(altered, signed_data, signed_length);
        altered[mutations[i].offset] ^= 1U;
        (void)invoke(f, mutations[i].name, 0U, altered, signed_length, NULL, false);
    }
    (void)memcpy(altered, signed_data, signed_length); altered[66U] ^= 2U;
    (void)invoke(f, "mismatched-precommit-refused", 0U, altered, signed_length, NULL, false);
    (void)memcpy(altered, signed_data, signed_length); write_u64(altered+139U, 11U);
    (void)invoke(f, "future-observation-refused", 0U, altered, signed_length, NULL, false);
    (void)memcpy(altered, signed_data, signed_length); write_u64(altered+139U, 9U);
    (void)invoke(f, "preseal-observation-refused", 0U, altered, signed_length, NULL, false);
    n = attestation_request(f, data, lease, 1U, 1U);
    (void)invoke(f, "bound-policy-refused", 0U, data, n, NULL, false);
    n = attestation_request(f, data, lease, 1U, 2U);
    (void)invoke(f, "bound-revision-refused", 0U, data, n, NULL, false);
    n = attestation_request(f, data, lease, 1U, 3U);
    (void)invoke(f, "bound-domain-refused", 0U, data, n, NULL, false);
    n = attestation_request(f, data, lease, 1U, 4U);
    (void)invoke(f, "wrong-named-key-refused", 0U, data, n, NULL, false);
    restored = calloc(1U, sizeof(*restored));
    if (restored == NULL || restore(f, restored) != 0) return 1;
    if (invoke(f, "named-attester-accepted", 0U, signed_data, signed_length, NULL, true) != 0) return 1;
    {
        uint8_t original_digest[32], replay_digest[32], payload[8192];
        size_t payload_length = call_payload(restored, payload, signed_data, signed_length, NULL);
        int matched = lxp_receipt_digest(&f->receipt, &f->arena, original_digest) == LXP_OK;
        result = execute(restored, 0U, LX_PROGRAMS_CALL, payload, payload_length, false);
        matched = matched && result == LXP_OK && restored->receipt.result_code == LXP_OK &&
            lxp_receipt_digest(&restored->receipt, &restored->arena, replay_digest) == LXP_OK &&
            memcmp(original_digest, replay_digest, 32U) == 0 &&
            memcmp(f->kernel.current_state_root, restored->kernel.current_state_root, 32U) == 0;
        (void)report(f, "deterministic-admission", matched, result);
    }
    restored->height = 901U;
    restored->expiry_grant = true;
    n = request_start(data, 4U); bytes(data, &n, lease, 32U);
    if (invoke(restored, "expire-funded-lease", 1U, data, n, NULL, true) == 0) {
        uint8_t key[48];
        read_cell cell;
        (void)memcpy(key, "lx.market.lease/", 16U);
        (void)memcpy(key+16U, lease, 32U);
        if (read_shared(restored, key, sizeof(key), &cell) != 0 ||
            cell.value_length < 2U || cell.value[1] != 3U) return 1;
        restored->expiry_grant = false;
        n = attestation_request(restored, data, lease, 2U, 0U);
        (void)invoke(restored, "unfunded-lease-refused", 0U, data, n, NULL, false);
    }
    f->failures += restored->failures;
    while (restored->kernel.blob_count != 0U) free(restored->kernel.blobs[--restored->kernel.blob_count].bytes);
    (void)lxp_state_store_destroy(&restored->state);
    free(restored);
    result = execute(f, 0U, LX_PROGRAMS_CALL, f->last_payload, f->last_activity.payload.length, true);
    (void)report(f, "same-activity-replay-refused",
                 result == LXP_ERR_SEQUENCE_REUSED || result == LXP_ERR_IDEMPOTENT_REPLAY ||
                 (result == LXP_OK && (f->receipt.result_code == LXP_ERR_SEQUENCE_REUSED ||
                                      f->receipt.result_code == LXP_ERR_IDEMPOTENT_REPLAY)),
                 result == LXP_OK ? f->receipt.result_code : result);
    (void)invoke(f, "new-activity-replay-refused", 0U, signed_data, signed_length, NULL, false);
    n = usage_request(data, lease, attester_key);
    (void)invoke(f, "partial-admission-settlement-refused", 0U, data, n, NULL, false);
    n = attestation_request(f, data, lease, 2U, 0U);
    if (invoke(f, "second-attester-accepted", 0U, data, n, NULL, true) != 0) return 1;
    if (require_lease_model(f, lease, 2U) != 0 || settlement_root(f, lease, root) != 0)
        return report(f, "evidence-attested", false, LXP_FATAL_INVARIANT);
    (void)report(f, "evidence-attested", true, LXP_OK);
    restored = calloc(1U, sizeof(*restored));
    if (restored == NULL) return 1;
    if (report(f, "restored-state-root", restore(f, restored) == 0, LXP_OK) == 0) {
        (void)invoke(restored, "restored-replay-refused", 0U, signed_data, signed_length, NULL, false);
        f->failures += restored->failures;
    }
    while (restored->kernel.blob_count != 0U) free(restored->kernel.blobs[--restored->kernel.blob_count].bytes);
    (void)lxp_state_store_destroy(&restored->state);
    free(restored);
    n = usage_request(data, lease, zero);
    (void)invoke(f, "usage-zero-root-refused", 0U, data, n, NULL, false);
    (void)memmove(data+66U, data+98U, n-98U);
    (void)invoke(f, "omitted-root-settlement-refused", 0U, data, n-32U, NULL, false);
    n = usage_request(data, lease, root); data[66U] ^= 1U;
    (void)invoke(f, "usage-mutated-root-refused", 0U, data, n, NULL, false);
    {
        uint8_t other_root[32];
        if (distinct_lease_root(f, other_root) != 0 || memcmp(root, other_root, 32U) == 0)
            return report(f, "usage-swapped-root-refused", false, LXP_FATAL_INVARIANT);
        n = usage_request(data, lease, other_root);
        (void)invoke(f, "usage-swapped-root-refused", 0U, data, n, NULL, false);
    }
    n = usage_request(data, lease, root);
    return invoke(f, "usage-exact-root-accepted", 0U, data, n, NULL, true);
}

static int model_cases(fixture *f, uint8_t model)
{
    uint8_t lease[32], data[1024], root[32];
    size_t n;
    unsigned ordinal;
    const char *positive = model == 1U ? "bonded-external-input-accepted" : "fraudprovable-external-input-accepted";
    const char *negative = model == 1U ? "bonded-uncommitted-settlement-refused" : "fraud-provable-uncommitted-settlement-refused";
    if (provision_lease(f, model, lease) != 0) return 1;
    n = usage_request(data, lease, attester_key);
    (void)invoke(f, negative, 0U, data, n, NULL, false);
    n = configure_request(data, lease);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0)
        return report(f, positive, false, f->receipt.result_code);
    for (ordinal = 1U; ordinal <= 2U; ++ordinal) {
        n = input_fields(data, request_start(data, 7U), lease, (uint8_t)ordinal);
        if (invoke(f, NULL, 1U, data, n, NULL, true) != 0)
            return report(f, positive, false, f->receipt.result_code);
    }
    n = request_start(data, 8U); bytes(data, &n, lease, 32U);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0)
        return report(f, positive, false, f->receipt.result_code);
    for (ordinal = 1U; ordinal <= 2U; ++ordinal) {
        n = attestation_request(f, data, lease, (uint8_t)ordinal, 0U);
        if (invoke(f, NULL, 0U, data, n, NULL, true) != 0)
            return report(f, positive, false, f->receipt.result_code);
    }
    if (require_lease_model(f, lease, model) != 0 || settlement_root(f, lease, root) != 0)
        return report(f, positive, false, LXP_FATAL_INVARIANT);
    n = usage_request(data, lease, root);
    return invoke(f, positive, 0U, data, n, NULL, true);
}

static int empty_case(fixture *f, uint8_t model, const char *suffix,
                      int passed, lxp_result code)
{
    static const char *const models[] = {"", "bonded", "attested", "fraudprovable"};
    char name[128];
    (void)snprintf(name, sizeof(name), "empty-%s-%s", models[model], suffix);
    return report(f, name, passed, code);
}

static int empty_invoke(fixture *f, uint8_t model, const char *suffix,
                        unsigned actor, const uint8_t *data, size_t length, bool success)
{
    static const char *const models[] = {"", "bonded", "attested", "fraudprovable"};
    char name[128];
    (void)snprintf(name, sizeof(name), "empty-%s-%s", models[model], suffix);
    return invoke(f, name, actor, data, length, NULL, success);
}

static int empty_settlement_root(fixture *f, const uint8_t lease[32], uint8_t root[32])
{
    static const uint8_t domain[] = "LXP/market-settlement-inputs/v1";
    uint8_t policy[32], data[192], zero[32] = {0};
    size_t n = 0U;
    if (policy_digest(f, lease, policy) != 0) return 1;
    bytes(data, &n, domain, sizeof(domain));
    bytes(data, &n, lease, 32U);
    integer(data, &n, 1U);
    bytes(data, &n, policy, 32U);
    write_u32(data+n, 0U); n += 4U;
    bytes(data, &n, zero, 32U);
    bytes(data, &n, zero, 32U);
    return lxp_hash_sha256(data, n, root) != LXP_OK || memcmp(root, zero, 32U) == 0;
}

static int read_empty_plan(fixture *f, const uint8_t lease[32], const uint8_t root[32],
                           read_cell *cell)
{
    static const uint8_t prefix[] = "lx.market.attesters/";
    uint8_t key[sizeof(prefix)-1U+32U], expected[512], zero[32] = {0};
    size_t n = 0U, k = 0U;
    bytes(key, &k, prefix, sizeof(prefix)-1U);
    bytes(key, &k, lease, 32U);
    expected[n++] = 1U;
    bytes(expected, &n, lease, 32U);
    bytes(expected, &n, f->principals[1], 32U);
    integer(expected, &n, 1U);
    expected[n++] = 1U;
    integer(expected, &n, f->height);
    write_u32(expected+n, 0U); n += 4U;
    write_u32(expected+n, 0U); n += 4U;
    for (unsigned index = 0U; index < 4U; ++index) bytes(expected, &n, zero, 32U);
    expected[n++] = 1U;
    bytes(expected, &n, root, 32U);
    expected[n++] = 1U;
    expected[n++] = sizeof(attester_name)-1U;
    bytes(expected, &n, attester_name, sizeof(attester_name)-1U);
    bytes(expected, &n, attester_key, 32U);
    return read_shared(f, key, k, cell) != 0 || cell->value_length != n ||
        memcmp(cell->value, expected, n) != 0;
}

static int empty_plan_cases(fixture *source, uint8_t model)
{
    fixture *f = calloc(1U, sizeof(*f)), *restored = NULL;
    uint8_t lease[32], other_lease[32], data[1024];
    uint8_t root[32], other_root[32], zero[32] = {0};
    read_cell frozen, after;
    size_t n;
    int failed = 1;
    if (f == NULL || restore(source, f) != 0) goto done;
    if (provision_lease_instance(f, model, (uint8_t)(10U+model), lease) != 0) goto done;
    n = usage_request(data, lease, attester_key);
    (void)empty_invoke(f, model, "no-policy-usage-refused", 0U, data, n, false);
    n = configure_request(data, lease);
    if (empty_invoke(f, model, "configure-accepted", 1U, data, n, true) != 0) goto done;
    n = usage_request(data, lease, attester_key);
    (void)empty_invoke(f, model, "unsealed-usage-refused", 0U, data, n, false);
    n = request_start(data, 8U); bytes(data, &n, lease, 32U);
    (void)empty_invoke(f, model, "wrong-tenant-seal-refused", 2U, data, n, false);
    if (empty_invoke(f, model, "seal-accepted", 1U, data, n, true) != 0) goto done;
    if (empty_case(f, model, "frozen-state-valid",
        empty_settlement_root(f, lease, root) == 0 &&
        require_lease_model(f, lease, model) == 0 &&
        read_empty_plan(f, lease, root, &frozen) == 0, LXP_OK) != 0) goto done;
    (void)empty_invoke(f, model, "reseal-refused", 1U, data, n, false);
    n = input_fields(data, request_start(data, 7U), lease, 1U);
    (void)empty_invoke(f, model, "late-input-refused", 1U, data, n, false);
    n = attestation_request(f, data, lease, 1U, 0U);
    if (n == 0U) goto done;
    (void)empty_invoke(f, model, "external-admission-refused", 0U, data, n, false);
    n = usage_request(data, lease, zero);
    (void)empty_invoke(f, model, "zero-root-usage-refused", 0U, data, n, false);
    n = usage_request(data, lease, root); data[66U] ^= 1U;
    (void)empty_invoke(f, model, "mutated-root-usage-refused", 0U, data, n, false);
    if (provision_lease_instance(f, model, (uint8_t)(20U+model), other_lease) != 0) goto done;
    n = configure_request(data, other_lease);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0) goto done;
    n = request_start(data, 8U); bytes(data, &n, other_lease, 32U);
    if (invoke(f, NULL, 1U, data, n, NULL, true) != 0 ||
        empty_settlement_root(f, other_lease, other_root) != 0 ||
        read_empty_plan(f, other_lease, other_root, &after) != 0 ||
        memcmp(root, other_root, 32U) == 0) goto done;
    n = usage_request(data, lease, other_root);
    (void)empty_invoke(f, model, "swapped-root-usage-refused", 0U, data, n, false);
    restored = calloc(1U, sizeof(*restored));
    if (restored == NULL) goto done;
    if (empty_case(f, model, "restored-state-root-equal", restore(f, restored) == 0 &&
        read_empty_plan(restored, lease, root, &after) == 0 &&
        frozen.value_length == after.value_length &&
        memcmp(frozen.value, after.value, frozen.value_length) == 0, LXP_OK) != 0) goto done;
    n = usage_request(data, lease, root);
    (void)empty_invoke(restored, model, "restored-usage-exact-root-accepted", 0U, data, n, true);
    (void)empty_invoke(f, model, "usage-exact-root-accepted", 0U, data, n, true);
    (void)empty_case(f, model, "input-state-unchanged-after-refusals",
        read_empty_plan(f, lease, root, &after) == 0 &&
        frozen.value_length == after.value_length &&
        memcmp(frozen.value, after.value, frozen.value_length) == 0 &&
        read_empty_plan(restored, lease, root, &after) == 0 &&
        frozen.value_length == after.value_length &&
        memcmp(frozen.value, after.value, frozen.value_length) == 0 &&
        memcmp(f->kernel.current_state_root, restored->kernel.current_state_root, 32U) == 0 &&
        !f->refusal_changed_state, LXP_OK);
    failed = 0;
done:
    if (restored != NULL) {
        source->failures += restored->failures;
        source->verified_receipts += restored->verified_receipts;
        source->verified_roots += restored->verified_roots;
        while (restored->kernel.blob_count != 0U)
            free(restored->kernel.blobs[--restored->kernel.blob_count].bytes);
        if (lxp_state_store_destroy(&restored->state) != LXP_OK) failed = 1;
        free(restored);
    }
    if (f != NULL) {
        source->failures += f->failures;
        source->verified_receipts += f->verified_receipts;
        source->verified_roots += f->verified_roots;
        source->refusal_checks += f->refusal_checks;
        source->refusal_changed_state |= f->refusal_changed_state;
        while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
        if (lxp_state_store_destroy(&f->state) != LXP_OK) failed = 1;
        free(f);
    }
    return failed;
}

int main(int argc, char **argv)
{
    fixture *f;
    FILE *artifact;
    uint8_t *wasm, *payload, lease[32];
    size_t length;
    long file_length;
    lxp_result status;
    int failed = 0;
    if (argc != 2) return fprintf(stderr, "usage: %s market.wasm\n", argv[0]), 2;
    artifact = fopen(argv[1], "rb");
    if (artifact == NULL || fseek(artifact, 0L, SEEK_END) != 0) return 2;
    file_length = ftell(artifact);
    if (file_length <= 0L || (unsigned long)file_length > LXP_MAX_ACTIVITY_BYTES-104U ||
        fseek(artifact, 0L, SEEK_SET) != 0) return fclose(artifact), 2;
    length = (size_t)file_length;
    wasm = malloc(length);
    payload = calloc(1U, length+104U);
    f = calloc(1U, sizeof(*f));
    if (wasm == NULL || payload == NULL || f == NULL) return fclose(artifact), 2;
    if (fread(wasm, 1U, length, artifact) != length || fclose(artifact) != 0 ||
        executed_public_key(attester_seed, attester_key) != 0 || initialize(f) != 0) return 2;
    (void)memcpy(payload, f->program, 32U);
    write_u16(payload+32U, LX_PROGRAMS_GUEST_ABI_V5_VERSION);
    payload[34U] = 1U;
    (void)memcpy(payload+36U, f->principals[0], 32U);
    if (lxp_hash_sha256(wasm, length, payload+68U) != LXP_OK) return 2;
    write_u32(payload+100U, (uint32_t)length);
    (void)memcpy(payload+104U, wasm, length);
    status = execute(f, 0U, LX_PROGRAMS_DEPLOY, payload, length+104U, false);
    if (report(f, "native-deploy", status == LXP_OK && f->receipt.result_code == LXP_OK,
               status == LXP_OK ? f->receipt.result_code : status) == 0) {
        for (uint8_t model = 1U; model <= 3U; ++model)
            failed |= empty_plan_cases(f, model);
        failed |= provision_lease(f, 2U, lease);
        if (!failed) failed |= admission_cases(f, lease);
        failed |= model_cases(f, 1U);
        failed |= model_cases(f, 3U);
    }
    (void)report(f, "signed-native-receipts-verified", f->verified_receipts > 0U &&
                 f->verified_receipts == f->verified_roots, LXP_OK);
    (void)report(f, "committed-state-root-verified", f->verified_roots > 0U &&
                 f->verified_receipts == f->verified_roots, LXP_OK);
    (void)report(f, "refusal-preserves-business-state", f->refusal_checks > 0U &&
                 !f->refusal_changed_state, LXP_OK);
    failed |= f->failures != 0U;
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    if (lxp_state_store_destroy(&f->state) != LXP_OK) failed = 1;
    free(f); free(payload); free(wasm);
    return failed ? 1 : 0;
}
