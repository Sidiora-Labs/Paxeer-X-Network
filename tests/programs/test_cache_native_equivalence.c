#define main metered_fixture_reference_main
int metered_fixture_reference_main(int argc, char **argv);
#include "test_call_activity.c"
#undef main
#include "layerx/lxp_batch_identity.h"
#include "../../src/modules/programs/storage.h"

#define METERED_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "metered call check failed at line %d\n", __LINE__); \
    return 1; } } while (0)

typedef struct metered_fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    lxp_arena arena;
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_identity *identity;
    lx_account *actor;
    lx_account *payee;
    lx_account *source;
    uint64_t parameters;
    uint8_t owner_key[32];
    uint8_t delegate_key[32];
    uint8_t program[32];
    uint8_t source_id[32];
    uint8_t grant_id[32];
    uint8_t call[1024];
    size_t call_length;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_transfer_allowance allowance;
    lxp_kernel_execution execution;
    lxp_activity activity;
    lxp_receipt receipt;
    uint8_t signature[64];
    lxp_u128 signed_fee_limit;
    unsigned recipient_mode;
} metered_fixture;

static const uint8_t metered_owner_seed[32] = {0x33U};
static const uint8_t metered_delegate_seed[32] = {0x34U};
static const uint8_t metered_did[] = "did:lxp:metered-call";
static int metered_account(metered_fixture *f, const char *name,
                            uint64_t balance, lx_account **account)
{
    uint8_t id[32];
    METERED_CHECK(lx_account_id_from_string((const uint8_t *)name,
                                            strlen(name), id) == LXP_OK);
    METERED_CHECK(lx_account_open(&f->accounts, (const uint8_t *)name,
                                   strlen(name), id, 1U,
                                   LX_ACCOUNT_OPEN_GENESIS, NULL, account) ==
                  LXP_OK);
    METERED_CHECK(lxp_ledger_bootstrap_balance(*account, f->asset.asset_id,
                                              (lxp_u128){0U, balance}, 0U) ==
                  LXP_OK);
    return 0;
}

static int metered_activity(metered_fixture *f, uint32_t type,
                             const uint8_t *payload, size_t length,
                             uint8_t marker, bool delegated)
{
    const uint8_t *key = delegated ? f->delegate_key : f->owner_key;
    const uint8_t *seed = delegated ? metered_delegate_seed : metered_owner_seed;
    uint8_t digest[32];
    lxp_byte_span encoded;
    EVP_PKEY *signer_key;
    EVP_MD_CTX *signer_context;
    size_t signature_length = sizeof(f->signature);
    METERED_CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    fill_activity(&f->activity, type, payload, length, metered_did,
                   sizeof(metered_did) - 1U, key);
    f->activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    f->activity.account_sequence = f->identity->next_sequence;
    write_u64(f->activity.idempotency_key + 23U, f->state.next_sequence);
    f->activity.idempotency_key[31] = marker;
    f->activity.fee_limit = lxp_u128_is_zero(f->signed_fee_limit) ?
        (lxp_u128){0U, 67108864U} : f->signed_fee_limit;
    f->activity.signature = (lxp_byte_span){f->signature, sizeof(f->signature)};
    METERED_CHECK(lxp_hash_payload(payload, length, f->activity.payload_hash) ==
                  LXP_OK);
    METERED_CHECK(lxp_activity_signing_preimage(&f->activity, digest) == LXP_OK);
    signer_key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    signer_context = EVP_MD_CTX_new();
    METERED_CHECK(signer_key != NULL && signer_context != NULL);
    METERED_CHECK(EVP_DigestSignInit(signer_context, NULL, NULL, NULL,
                                     signer_key) == 1);
    METERED_CHECK(EVP_DigestSign(signer_context, f->signature, &signature_length,
                                digest, sizeof(digest)) == 1);
    EVP_MD_CTX_free(signer_context);
    EVP_PKEY_free(signer_key);
    METERED_CHECK(signature_length == sizeof(f->signature));
    METERED_CHECK(lxp_activity_verify_signature(&f->activity) == LXP_OK);
    METERED_CHECK(lxp_authority_resolve_activity(&f->kernel, f->identity,
        &f->activity, !delegated, true, 10U, 100U, f->state.next_sequence,
        &f->grant, &f->authority) == LXP_OK);
    lxp_authority_allowance_bind(&f->grant, &f->authority, &f->allowance);
    (void)memset(&f->execution, 0, sizeof(f->execution));
    f->execution.network_id = 7U;
    f->execution.batch_number = 1U;
    f->execution.batch_timestamp_ms = 10U;
    f->execution.maximum_timestamp_window = 100U;
    f->execution.global_sequence = f->state.next_sequence;
    f->execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    f->execution.recorded_metering_schedule_version = 1U;
    f->execution.recorded_fee_schedule_version = 1U;
    f->execution.parameter_version = 1U;
    f->execution.signature_valid = true;
    f->execution.sequencer_private_key = executed_sequencer_seed;
    f->execution.identities = &f->identities;
    f->execution.authority = &f->authority;
    f->execution.allowance = &f->allowance;
    f->execution.fee_parameters = &f->fees;
    f->execution.fee_balance = f->actor->balance;
    f->execution.gas_limit = 1000000U;
    f->execution.arena = &f->arena;
    METERED_CHECK(lxp_activity_encode(&f->activity, &f->arena, &encoded) == LXP_OK);
    METERED_CHECK(lxp_activity_id(encoded.bytes, encoded.length, digest) == LXP_OK);
    METERED_CHECK(lxp_batch_identity_activity(f->kernel.current_state_root,
        digest, f->state.next_sequence, 1U, f->execution.batch_id) == LXP_OK);
    return 0;
}

static int cache_fixture_init(metered_fixture *f)
{
    lx_account *treasury;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters fees = {0};
    METERED_CHECK(executed_public_key(metered_owner_seed, f->owner_key) == 0);
    METERED_CHECK(executed_public_key(metered_delegate_seed, f->delegate_key) == 0);
    f->asset.asset_id[0] = 9U;
    f->asset.registered = true;
    f->program[0] = 0x47U;
    METERED_CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    METERED_CHECK(metered_account(f, "agent:did:lxp:metered-call:main",
                                   UINT64_C(1000000000), &f->actor) == 0);
    METERED_CHECK(metered_account(f, "agent:did:lxp:metered-payee:main", 0U,
                                   &f->payee) == 0);
    METERED_CHECK(metered_account(f, "system:fees", 0U, &treasury) == 0);
    if (f->recipient_mode == 1U) f->payee = f->actor;
    if (f->recipient_mode == 2U) f->payee = treasury;
    f->actor->has_authority_key = true;
    (void)memcpy(f->actor->authority_key, f->owner_key, 32U);
    METERED_CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    METERED_CHECK(lxp_state_store_bind_accounts(&f->state, &f->accounts) == LXP_OK);
    f->parameters = 1U;
    METERED_CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal,
                                     &f->parameters, 0U) == LXP_OK);
    METERED_CHECK(install_metering_v1(&f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_register_module(&f->kernel,
                    programs_module_registration_v4()) == LXP_OK);
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){
        1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U
    };
    (void)memcpy(f->runtime.occupancy_asset_id, f->asset.asset_id, 32U);
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    (void)memcpy(manifest.signer_public_key, f->owner_key, 32U);
    fees.schedule = f->runtime.fee_schedule;
    (void)memcpy(fees.occupancy_asset_id, f->asset.asset_id, 32U);
    fees.target_occupancy_byte_batches = 3U;
    fees.response_denominator = 1U;
    fees.maximum_change_numerator = 1U;
    fees.maximum_change_denominator = 1U;
    fees.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fees.maximum_fee_units_per_occupancy_byte_batch = 10U;
    METERED_CHECK(lxp_programs_fee_genesis_append(&manifest, &fees) == LXP_OK);
    METERED_CHECK(lxp_programs_fee_genesis_materialize(&manifest, &f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS,
                                                  &f->runtime) == LXP_OK);
    METERED_CHECK(lxp_programs_bind_fee_transaction(&f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL,
                    lxp_kernel_canonical_ledger_apply) == LXP_OK);
    METERED_CHECK(lxp_identity_register(&f->identities, metered_did,
        sizeof(metered_did) - 1U, f->owner_key, &f->identity) == LXP_OK);
    f->identity->revocation_sequence = 1U;
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    METERED_CHECK(lxp_arena_init(&f->arena, f->arena_bytes,
                                 sizeof(f->arena_bytes)) == LXP_OK);
    METERED_CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    return 0;
}

extern int32_t layerx_programs_cache_configure(uint64_t max_entries, uint64_t max_bytes);
extern uint64_t layerx_programs_cache_observe(uint32_t field);
extern int32_t layerx_programs_cache_select_observation(
    uint64_t h0, uint64_t h1, uint64_t h2, uint64_t h3);

enum { CACHE_GUESTS = 12, CACHE_HISTORIES = 4, CACHE_CALLS = 256,
       CACHE_RECORDS = CACHE_HISTORIES * (CACHE_GUESTS + CACHE_CALLS),
       CACHE_BYTE_LIMIT = 6000 };

typedef struct cache_guest {
    uint8_t bytes[65536];
    size_t length;
    uint8_t hash[32];
    uint8_t program[32];
    uint16_t abi;
} cache_guest;

typedef struct cache_observation {
    uint8_t *receipt;
    size_t length;
    uint8_t root[32];
    uint8_t activity[32];
    uint64_t usage[6];
} cache_observation;

static size_t cache_host_guest(uint8_t *out, bool refused)
{
    static const uint8_t header[] = {0, 0x61, 0x73, 0x6d, 1, 0, 0, 0};
    static const uint8_t functions[] = {2, 1, 2};
    static const uint8_t memory[] = {1, 1, 1, 1};
    static const uint8_t storage_body[] = {
        0, 0x41, 1, 0x20, 0, 0x2d, 0, 0, 0x3a, 0, 0,
        0x41, 0, 0x41, 1, 0x41, 1, 0x41, 1, 0x10, 0, 0x0b
    };
    static const uint8_t refusal_body[] = {
        0, 0x41, 1, 0x41, 0, 0x41, 1, 0x10, 0, 0x1a,
        0x41, 0x40, 0x0b
    };
    uint8_t section[256];
    size_t cursor = 0, length = 0;
    append_bytes(out, &cursor, header, sizeof(header));
    section[length++] = 3; section[length++] = 0x60;
    section[length++] = refused ? 3 : 4;
    for (unsigned i = 0; i < (refused ? 3U : 4U); ++i) section[length++] = 0x7f;
    section[length++] = 1; section[length++] = 0x7f;
    section[length++] = 0x60; section[length++] = 1; section[length++] = 0x7f;
    section[length++] = 1; section[length++] = 0x7f;
    section[length++] = 0x60; section[length++] = 2;
    section[length++] = 0x7f; section[length++] = 0x7f;
    section[length++] = 1; section[length++] = 0x7f;
    append_section(out, &cursor, 1, section, length);
    length = 0; section[length++] = 1;
    append_name(section, &length, refused ? "layerx_v2" : "layerx_v1");
    append_name(section, &length, refused ? "refusal_write" : "storage_write");
    section[length++] = 0; section[length++] = 0;
    append_section(out, &cursor, 2, section, length);
    append_section(out, &cursor, 3, functions, sizeof(functions));
    append_section(out, &cursor, 5, memory, sizeof(memory));
    length = 0; section[length++] = 3;
    append_name(section, &length, "layerx_reserve");
    section[length++] = 0; section[length++] = 1;
    append_name(section, &length, "layerx_call");
    section[length++] = 0; section[length++] = 2;
    append_name(section, &length, "memory");
    section[length++] = 2; section[length++] = 0;
    append_section(out, &cursor, 7, section, length);
    length = 0; section[length++] = 2;
    section[length++] = 5; section[length++] = 0;
    section[length++] = 0x41; section[length++] = 0x80;
    section[length++] = 8; section[length++] = 0x0b;
    append_u32_leb(section, &length, (uint32_t)(refused ? sizeof(refusal_body) : sizeof(storage_body)));
    append_bytes(section, &length, refused ? refusal_body : storage_body,
                 refused ? sizeof(refusal_body) : sizeof(storage_body));
    append_section(out, &cursor, 10, section, length);
    { static const uint8_t data[] = {1, 0, 0x41, 0, 0x0b, 2, 'k', 'v'};
      append_section(out, &cursor, 11, data, sizeof(data)); }
    return cursor;
}

static size_t cache_guest_generate(uint8_t *out, unsigned index)
{
    static const uint8_t trap[] = {0, 0x0b};
    static const uint8_t loop[] = {0x03, 0x40, 0x0c, 0, 0x0b, 0x41, 0, 0x0b};
    uint8_t success[] = {0x41, 0, 0x0b};
    size_t length;
    if (index < 2U) return cache_host_guest(out, index == 1U);
    if (index == 2U) return candidate_module(out, trap, sizeof(trap));
    if (index == 3U) return candidate_module(out, loop, sizeof(loop));
    success[1] = (uint8_t)index;
    length = candidate_module(out, success, sizeof(success));
    if (index == 11U) {
        out[length++] = 0;
        append_u32_leb(out, &length, 2050U);
        out[length++] = 1; out[length++] = 'p';
        (void)memset(out + length, 0xa5, 2048U);
        length += 2048U;
    }
    return length;
}

static int cache_guests_io(const char *directory, cache_guest *guests, bool emit)
{
    for (unsigned i = 0; i < CACHE_GUESTS; ++i) {
        char path[4096];
        FILE *file;
        int count = snprintf(path, sizeof(path), "%s/guest-%02u.wasm", directory, i);
        METERED_CHECK(count > 0 && (size_t)count < sizeof(path));
        if (emit) {
            guests[i].length = cache_guest_generate(guests[i].bytes, i);
            file = fopen(path, "wb");
            METERED_CHECK(file != NULL);
            METERED_CHECK(fwrite(guests[i].bytes, 1, guests[i].length, file) == guests[i].length);
        } else {
            uint8_t expected[65536];
            size_t expected_length = cache_guest_generate(expected, i);
            file = fopen(path, "rb");
            METERED_CHECK(file != NULL);
            guests[i].length = fread(guests[i].bytes, 1, sizeof(guests[i].bytes), file);
            METERED_CHECK(feof(file) && !ferror(file));
            METERED_CHECK(guests[i].length == expected_length &&
                memcmp(guests[i].bytes, expected, expected_length) == 0);
        }
        METERED_CHECK(fclose(file) == 0);
        METERED_CHECK(lxp_hash_sha256(guests[i].bytes, guests[i].length, guests[i].hash) == LXP_OK);
        guests[i].program[0] = 0x71;
        guests[i].program[31] = (uint8_t)(i + 1U);
        guests[i].abi = i == 9U ? 3U : i == 10U ? 4U : 2U;
    }
    return 0;
}

static int cache_observe(metered_fixture *f, cache_observation *record, bool enabled)
{
    static uint8_t storage[2U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t public_key[32], root[32], activity_id[32];
    lxp_arena arena;
    lxp_byte_span encoded;
    const lxp_program_outcome *outcome = &f->receipt.program_outcome;
    uint64_t usage[6] = {outcome->cpu_fuel, outcome->memory_bytes,
        outcome->storage_read_bytes, outcome->storage_write_bytes,
        outcome->output_values, outcome->output_bytes};
    METERED_CHECK(executed_public_key(executed_sequencer_seed, public_key) == 0);
    METERED_CHECK(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
    METERED_CHECK(lxp_receipt_verify(&f->receipt, public_key, &arena) == LXP_OK);
    METERED_CHECK(lxp_activity_encode(&f->activity, &arena, &encoded) == LXP_OK);
    METERED_CHECK(lxp_activity_id(encoded.bytes, encoded.length, activity_id) == LXP_OK);
    METERED_CHECK(memcmp(activity_id, f->receipt.activity_id, 32) == 0);
    METERED_CHECK(lxp_state_root(&f->kernel, root) == LXP_OK);
    METERED_CHECK(memcmp(root, f->kernel.current_state_root, 32) == 0);
    METERED_CHECK(memcmp(root, f->receipt.resulting_state_root, 32) == 0);
    METERED_CHECK(lxp_arena_reset(&arena, 0) == LXP_OK);
    METERED_CHECK(lxp_receipt_encode(&f->receipt, true, &arena, &encoded) == LXP_OK);
    if (enabled) {
        METERED_CHECK(record->length == encoded.length);
        METERED_CHECK(memcmp(record->receipt, encoded.bytes, encoded.length) == 0);
        METERED_CHECK(memcmp(record->root, root, 32) == 0);
        METERED_CHECK(memcmp(record->activity, activity_id, 32) == 0);
        METERED_CHECK(memcmp(record->usage, usage, sizeof(usage)) == 0);
    } else {
        record->receipt = malloc(encoded.length);
        METERED_CHECK(record->receipt != NULL);
        record->length = encoded.length;
        (void)memcpy(record->receipt, encoded.bytes, encoded.length);
        (void)memcpy(record->root, root, 32);
        (void)memcpy(record->activity, activity_id, 32);
        (void)memcpy(record->usage, usage, sizeof(usage));
    }
    return 0;
}

static int cache_storage_value(metered_fixture *f, const uint8_t program[32], uint8_t expected)
{
    uint8_t namespace_bytes[65] = {0};
    uint8_t storage[65536];
    lxp_arena arena;
    lxp_module_ctx context;
    const uint8_t *key, *value;
    uint16_t key_length;
    uint32_t value_length, cell_count;
    (void)memcpy(namespace_bytes, program, 32);
    (void)memcpy(namespace_bytes + 33, f->identity->did_id, 32);
    METERED_CHECK(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
    METERED_CHECK(lxp_module_ctx_init(&context, &f->kernel, LXP_MODULE_PROGRAMS,
        10, 0, f->state.next_sequence, 1000000, &arena, false) == LXP_OK);
    context.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    lxp_result status = lxp_programs_storage_cell_at(&context, namespace_bytes,
        sizeof(namespace_bytes), 0, &key, &key_length, &value, &value_length, &cell_count);
    bool exact = status == LXP_OK && cell_count == 1U && key_length == 1U &&
        key[0] == 'k' && value_length == 1U && value[0] == expected;
    lxp_module_ctx_rollback(&context);
    METERED_CHECK(exact);
    return 0;
}

static uint64_t cache_hash_word(const uint8_t *bytes)
{
    uint64_t value = 0;
    for (unsigned i = 0; i < 8; ++i) value = (value << 8U) | bytes[i];
    return value;
}

static int cache_run(cache_guest *guests, cache_observation *records, bool enabled)
{
    unsigned order[CACHE_GUESTS];
    unsigned successes = 0, refusals = 0, traps = 0, resources = 0, writes = 0;
    METERED_CHECK(layerx_programs_cache_configure(enabled ? 2U : 0U,
        enabled ? CACHE_BYTE_LIMIT : 0U) == 0);
    for (unsigned i = 0; i < CACHE_GUESTS; ++i) order[i] = i;
    for (unsigned i = 0; i < CACHE_GUESTS; ++i)
        for (unsigned j = i + 1; j < CACHE_GUESTS; ++j)
            if (guests[order[i]].abi < guests[order[j]].abi ||
                (guests[order[i]].abi == guests[order[j]].abi &&
                 memcmp(guests[order[i]].hash, guests[order[j]].hash, 32) < 0)) {
                unsigned temp = order[i]; order[i] = order[j]; order[j] = temp;
            }
    for (unsigned history = 0; history < CACHE_HISTORIES; ++history) {
        metered_fixture *f = calloc(1, sizeof(*f));
        size_t slot = history * (CACHE_GUESTS + CACHE_CALLS);
        uint8_t payload[66000];
        METERED_CHECK(f != NULL && cache_fixture_init(f) == 0);
        for (unsigned i = 0; i < CACHE_GUESTS; ++i) {
            uint8_t hash[32];
            size_t length = program_spend_deploy_payload(payload, guests[i].program,
                f->identity->did_id, guests[i].bytes, guests[i].length, hash);
            write_u16(payload + 32, guests[i].abi);
            METERED_CHECK(metered_activity(f, LX_PROGRAMS_DEPLOY, payload, length,
                (uint8_t)(i + 1), false) == 0);
            METERED_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity,
                &f->execution, &f->receipt) == LXP_OK);
            METERED_CHECK(f->receipt.result_code == LXP_OK);
            METERED_CHECK(cache_observe(f, &records[slot++], enabled) == 0);
        }
        for (unsigned i = 0; i < CACHE_CALLS; ++i) {
            unsigned which = order[(i / 2U) % CACHE_GUESTS];
            cache_guest *guest = &guests[which];
            uint8_t capabilities[] = {0, 0, 2};
            uint8_t calldata = (uint8_t)(i + history);
            static const uint8_t access[] = "LayerX/programs/access-declaration/v1\0";
            METERED_CHECK(layerx_programs_cache_select_observation(
                cache_hash_word(guest->hash), cache_hash_word(guest->hash + 8),
                cache_hash_word(guest->hash + 16), cache_hash_word(guest->hash + 24)) == 0);
            uint64_t before_hits = layerx_programs_cache_observe(8);
            uint64_t before_compiles = layerx_programs_cache_observe(10);
            uint64_t before_entries = layerx_programs_cache_observe(11);
            size_t length;
            if (which == 0U) capabilities[1] = 1;
            length = call_payload_with_data(payload, guest->program, capabilities,
                which == 0U ? 3U : 2U, access, sizeof(access), &calldata, 1U);
            write_u16(payload + 32, guest->abi);
            if (which == 3U) write_u64(payload + 50, 1000U);
            METERED_CHECK(metered_activity(f, LX_PROGRAMS_CALL, payload, length,
                (uint8_t)(i + 32U), false) == 0);
            METERED_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity,
                &f->execution, &f->receipt) == LXP_OK);
            METERED_CHECK(f->receipt.program_outcome.present);
            METERED_CHECK(f->receipt.protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT);
            METERED_CHECK(f->receipt.module_version == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION);
            METERED_CHECK(f->receipt.program_outcome.abi_version == guest->abi);
            METERED_CHECK(f->receipt.program_outcome.metering_schedule_version == 1U);
            if (layerx_programs_cache_observe(8) > before_hits)
                METERED_CHECK(layerx_programs_cache_observe(10) == before_compiles);
            if (which == 1U || which == 2U) {
                METERED_CHECK(f->receipt.result_code == LXP_ERR_PROGRAM_REFUSED);
                METERED_CHECK(f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_FAILURE);
                if (which == 1U) ++refusals; else ++traps;
            } else if (which == 3U) {
                METERED_CHECK(f->receipt.result_code == LXP_ERR_GAS_EXHAUSTED);
                METERED_CHECK(f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_RESOURCE);
                METERED_CHECK(f->receipt.program_outcome.cpu_fuel > 0U);
                ++resources;
            } else {
                METERED_CHECK(f->receipt.result_code == LXP_OK);
                METERED_CHECK(f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS);
                ++successes;
                if (which == 0U) {
                    METERED_CHECK(f->receipt.program_outcome.storage_write_bytes > 0U);
                    ++writes;
                }
            }
            METERED_CHECK(cache_observe(f, &records[slot++], enabled) == 0);
            if (which == 0U) METERED_CHECK(cache_storage_value(f, guest->program, calldata) == 0);
            METERED_CHECK(layerx_programs_cache_observe(5) <= (enabled ? 2U : 0U));
            METERED_CHECK(layerx_programs_cache_observe(6) <= (enabled ? CACHE_BYTE_LIMIT : 0U));
            if (which == 11U) {
                METERED_CHECK(layerx_programs_cache_observe(10) == before_compiles + 1U);
                METERED_CHECK(before_entries == 0U);
                METERED_CHECK(layerx_programs_cache_observe(11) == before_entries);
            }
            if (i == 63U)
                METERED_CHECK(layerx_programs_module_cache_invalidate_abi(2U) == LXP_OK);
            if (i == 127U)
                METERED_CHECK(layerx_programs_module_cache_invalidate_runtime(
                    f->receipt.program_outcome.runtime_version) == LXP_OK);
            if (i == 191U) {
                const uint8_t *hash = guest->hash;
                METERED_CHECK(layerx_programs_module_cache_invalidate_upgrade(
                    cache_hash_word(hash), cache_hash_word(hash + 8),
                    cache_hash_word(hash + 16), cache_hash_word(hash + 24)) == LXP_OK);
            }
        }
        METERED_CHECK(f->identity->next_sequence == CACHE_GUESTS + CACHE_CALLS);
        METERED_CHECK(f->state.next_sequence == 1U + CACHE_GUESTS + CACHE_CALLS);
        while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
        METERED_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
        free(f);
    }
    METERED_CHECK(successes > 0 && refusals > 0 && traps > 0 && resources > 0 && writes > 0);
    if (enabled) {
        METERED_CHECK(layerx_programs_cache_observe(0) > 0U);
        METERED_CHECK(layerx_programs_cache_observe(1) > 0U);
        METERED_CHECK(layerx_programs_cache_observe(2) > 0U);
        METERED_CHECK(layerx_programs_cache_observe(3) > 0U);
        METERED_CHECK(layerx_programs_cache_observe(4) > 0U);
    } else METERED_CHECK(layerx_programs_cache_observe(0) == 0U);
    return 0;
}

int main(int argc, char **argv)
{
    cache_guest *guests = calloc(CACHE_GUESTS, sizeof(*guests));
    cache_observation *records;
    uint64_t uncached_compilations;
    METERED_CHECK(guests != NULL);
    if (argc == 3 && strcmp(argv[1], "--emit-guests") == 0) {
        int result = cache_guests_io(argv[2], guests, true);
        free(guests);
        return result;
    }
    METERED_CHECK(argc == 3 && strcmp(argv[1], "--guest-dir") == 0);
    METERED_CHECK(cache_guests_io(argv[2], guests, false) == 0);
    records = calloc(CACHE_RECORDS, sizeof(*records));
    METERED_CHECK(records != NULL);
    METERED_CHECK(layerx_programs_cache_configure(0, 1) == -1);
    METERED_CHECK(layerx_programs_cache_configure(1, 0) == -1);
    METERED_CHECK(layerx_programs_cache_configure(65, 1) == -1);
    METERED_CHECK(layerx_programs_cache_configure(1, UINT64_C(67108865)) == -1);
    METERED_CHECK(layerx_programs_cache_observe(UINT32_MAX) == UINT64_MAX);
    METERED_CHECK(cache_run(guests, records, false) == 0);
    uncached_compilations = layerx_programs_cache_observe(2);
    METERED_CHECK(cache_run(guests, records, true) == 0);
    METERED_CHECK(layerx_programs_cache_observe(2) < uncached_compilations);
    (void)printf("CACHE_NATIVE_EQUIVALENCE calls=1024 receipts=1072 hits=%llu compilations=%llu evictions=%llu invalidations=%llu\n",
        (unsigned long long)layerx_programs_cache_observe(0),
        (unsigned long long)layerx_programs_cache_observe(2),
        (unsigned long long)layerx_programs_cache_observe(3),
        (unsigned long long)layerx_programs_cache_observe(4));
    for (unsigned i = 0; i < CACHE_RECORDS; ++i) free(records[i].receipt);
    free(records); free(guests);
    return 0;
}
