#define main context_reference_main
int context_reference_main(int argc, char **argv);
#include "test_call_activity.c"
#undef main
#include "layerx/lxp_batch_identity.h"
#include <sys/stat.h>
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

static int context_fixture_init(metered_fixture *f)
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
    return 0;
}

#define CTX_NODES 18U
#define CTX_WASM_MAX 32768U
#define CTX_RECORD 333U
#define CTX_CHECK METERED_CHECK

typedef struct context_plan {
    unsigned count;
    unsigned children[CTX_NODES][17];
    unsigned child_count[CTX_NODES];
    unsigned mode;
    bool edge_observations;
} context_plan;

static const char *const context_cases[] = {
    "native_context_fields", "native_context_refusals",
    "native_context_frame_restore", "native_context_reentry",
    "native_context_depth", "native_context_edges", "native_context_fanout",
    "native_context_visits", "native_context_metering",
    "native_context_exhaustion", "native_context_replay"
};

static void context_id(unsigned node, uint8_t id[32])
{
    (void)memset(id, 0, 32U);
    id[0] = 0xc1U;
    id[31] = (uint8_t)(node + 1U);
}

static void context_i32(uint8_t *out, size_t *length, int32_t value)
{
    bool done;
    out[(*length)++] = 0x41U;
    do {
        uint8_t byte = (uint8_t)((uint32_t)value & 127U);
        value >>= 7;
        done = (value == 0 && (byte & 64U) == 0U) ||
               (value == -1 && (byte & 64U) != 0U);
        out[(*length)++] = (uint8_t)(byte | (done ? 0U : 128U));
    } while (!done);
}

static void context_call(uint8_t *body, size_t *length, unsigned function)
{
    body[(*length)++] = 0x10U;
    append_u32_leb(body, length, function);
}

static void context_assert(uint8_t *body, size_t *length, int32_t expected)
{
    context_i32(body, length, expected);
    body[(*length)++] = 0x47U;
    body[(*length)++] = 0x04U;
    body[(*length)++] = 0x40U;
    body[(*length)++] = 0x00U;
    body[(*length)++] = 0x0bU;
}

static void context_read(uint8_t *body, size_t *length, int32_t field,
                         int32_t pointer, int32_t capacity)
{
    context_i32(body, length, field);
    context_i32(body, length, pointer);
    context_i32(body, length, capacity);
    context_call(body, length, 0U);
}

static size_t context_capabilities(uint8_t *out, unsigned count)
{
    size_t length = 3U;
    write_u16(out, (uint16_t)(count + 1U));
    out[2] = 3U;
    for (unsigned node = 0U; node < count; ++node) {
        out[length++] = 4U;
        context_id(node, out + length);
        length += 32U;
    }
    return length;
}

static void context_observe(uint8_t *body, size_t *length, unsigned mode)
{
    if (mode >= 2U && mode <= 5U) {
        context_i32(body, length, 8192);
        context_read(body, length, mode == 3U ? 6 : 1, 8196,
                     mode == 4U ? 31 : 32);
        body[(*length)++] = 0x36U;
        body[(*length)++] = 2U;
        body[(*length)++] = 0U;
    } else {
        for (unsigned field = 1U; field <= 9U; ++field) {
            context_i32(body, length, (int32_t)(8192U + (field - 1U) * 37U));
            context_read(body, length, (int32_t)field,
                         (int32_t)(8196U + (field - 1U) * 37U), 33);
            body[(*length)++] = 0x36U;
            body[(*length)++] = 2U;
            body[(*length)++] = 0U;
        }
    }
    context_i32(body, length, 0);
    context_i32(body, length, 3);
    context_i32(body, length, 8192);
    context_i32(body, length, CTX_RECORD);
    context_call(body, length, 1U);
    context_assert(body, length, 0);
}

static size_t context_module(uint8_t *out, const context_plan *plan, unsigned node)
{
    static const uint8_t header[] = {0U,97U,115U,109U,1U,0U,0U,0U};
    static const uint8_t types[] = {
        5U, 0x60U,3U,0x7fU,0x7fU,0x7fU,1U,0x7fU,
        0x60U,4U,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7fU,
        0x60U,6U,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7fU,
        0x60U,1U,0x7fU,1U,0x7fU,
        0x60U,2U,0x7fU,0x7fU,1U,0x7fU
    };
    static const uint8_t functions[] = {2U,3U,4U};
    static const uint8_t memory[] = {1U,1U,1U,1U};
    uint8_t section[CTX_WASM_MAX], body[CTX_WASM_MAX], data[1024];
    size_t cursor = 0U, length = 0U, body_length = 0U;
    size_t capabilities_length = context_capabilities(data + 64U, plan->count);
    (void)memset(data, 0, 64U);
    (void)memcpy(data, "ctx", 3U);
    append_bytes(out, &cursor, header, sizeof(header));
    append_section(out, &cursor, 1U, types, sizeof(types));
    section[length++] = 3U;
    const char *modules[] = {"layerx_v2","layerx_v1","layerx_v1"};
    const char *imports[] = {"context_read","event_emit","program_call"};
    for (unsigned index = 0U; index < 3U; ++index) {
        append_name(section, &length, modules[index]);
        append_name(section, &length, imports[index]);
        section[length++] = 0U;
        section[length++] = (uint8_t)index;
    }
    append_section(out, &cursor, 2U, section, length);
    append_section(out, &cursor, 3U, functions, sizeof(functions));
    append_section(out, &cursor, 5U, memory, sizeof(memory));
    length = 0U;
    section[length++] = 3U;
    append_name(section, &length, "layerx_reserve");
    section[length++] = 0U; section[length++] = 3U;
    append_name(section, &length, "layerx_call");
    section[length++] = 0U; section[length++] = 4U;
    append_name(section, &length, "memory");
    section[length++] = 2U; section[length++] = 0U;
    append_section(out, &cursor, 7U, section, length);
    body[body_length++] = 0U;
    if (plan->mode == 1U) {
        const int32_t fields[] = {0,10,-1};
        for (unsigned index = 0U; index < 3U; ++index) {
            context_read(body, &body_length, fields[index], 8196, 33);
            context_assert(body, &body_length, -2);
        }
        const int32_t pointers[] = {-1,65535,INT32_MAX,8196,8196,8196};
        const int32_t capacities[] = {32,32,INT32_MAX,0,31,-1};
        for (unsigned index = 0U; index < 6U; ++index) {
            context_read(body, &body_length, 1, pointers[index], capacities[index]);
            context_assert(body, &body_length, index == 0U || index == 5U ? -2 : -3);
        }
        for (unsigned index = 0U; index < CTX_RECORD; ++index) {
            context_i32(body, &body_length, (int32_t)(8192U + index));
            body[body_length++] = 0x2dU; body[body_length++] = 0U;
            body[body_length++] = 0U;
            context_assert(body, &body_length, 0);
        }
    }
    if (!plan->edge_observations || node != 0U)
        context_observe(body, &body_length, plan->mode);
    for (unsigned child = 0U; child < plan->child_count[node]; ++child) {
        unsigned target = plan->children[node][child];
        context_i32(body, &body_length, (int32_t)(68U + target * 33U));
        context_i32(body, &body_length, 32);
        body[body_length++] = 0x20U; body[body_length++] = 0U;
        body[body_length++] = 0x20U; body[body_length++] = 1U;
        context_i32(body, &body_length, 64);
        context_i32(body, &body_length, (int32_t)capabilities_length);
        context_call(body, &body_length, 2U);
        context_assert(body, &body_length, 0);
        if (!plan->edge_observations)
            context_observe(body, &body_length, plan->mode);
    }
    context_i32(body, &body_length, 0);
    body[body_length++] = 0x0bU;
    length = 0U;
    section[length++] = 2U;
    uint8_t reserve[16]; size_t reserve_length = 0U;
    reserve[reserve_length++] = 0U;
    context_i32(reserve, &reserve_length, 4096);
    reserve[reserve_length++] = 0x0bU;
    append_u32_leb(section, &length, (uint32_t)reserve_length);
    append_bytes(section, &length, reserve, reserve_length);
    append_u32_leb(section, &length, (uint32_t)body_length);
    append_bytes(section, &length, body, body_length);
    append_section(out, &cursor, 10U, section, length);
    length = 0U; section[length++] = 1U; section[length++] = 0U;
    context_i32(section, &length, 0); section[length++] = 0x0bU;
    append_u32_leb(section, &length, (uint32_t)(64U + capabilities_length));
    append_bytes(section, &length, data, 64U + capabilities_length);
    append_section(out, &cursor, 11U, section, length);
    return cursor;
}

static void context_topology(unsigned kind, unsigned variant, context_plan *plan)
{
    (void)memset(plan, 0, sizeof(*plan));
    plan->count = 1U;
    if (kind == 1U) plan->mode = 1U;
    if (kind == 8U) plan->mode = variant + 2U;
    if (kind == 2U) {
        plan->count = 4U;
        plan->child_count[0] = 2U;
        plan->children[0][0] = 1U; plan->children[0][1] = 3U;
        plan->child_count[1] = 1U; plan->children[1][0] = 2U;
    } else if (kind == 3U) {
        plan->count = variant == 0U ? 1U : 2U;
        plan->child_count[0] = 1U;
        plan->children[0][0] = variant == 0U ? 0U : 1U;
        if (variant != 0U) { plan->child_count[1] = 1U; plan->children[1][0] = 0U; }
    } else if (kind == 4U) {
        plan->count = 9U + variant;
        for (unsigned node = 0U; node + 1U < plan->count; ++node) {
            plan->child_count[node] = 1U; plan->children[node][0] = node + 1U;
        }
    } else if (kind == 5U) {
        plan->edge_observations = true;
        plan->count = 17U;
        plan->child_count[0] = 8U;
        for (unsigned node = 1U; node <= 8U; ++node) {
            plan->children[0][node - 1U] = node;
            plan->child_count[node] = variant == 0U ? (node == 8U ? 2U : 3U) : 7U;
            for (unsigned child = 0U; child < plan->child_count[node]; ++child)
                plan->children[node][child] = 9U + child;
        }
        if (variant == 2U) {
            ++plan->child_count[8];
            plan->children[8][7] = 16U;
        }
    } else if (kind == 6U) {
        plan->count = 17U + variant;
        plan->child_count[0] = 16U + variant;
        for (unsigned child = 0U; child < plan->child_count[0]; ++child)
            plan->children[0][child] = child + 1U;
    } else if (kind == 7U) {
        plan->count = 2U;
        plan->child_count[0] = 8U + variant;
        for (unsigned child = 0U; child < plan->child_count[0]; ++child)
            plan->children[0][child] = 1U;
    }
}

static unsigned context_variants(unsigned kind)
{
    if (kind == 8U) return 4U;
    if (kind == 5U) return 3U;
    if (kind >= 3U && kind <= 7U) return 2U;
    return 1U;
}

static int context_guest_file(const char *directory, unsigned kind, unsigned variant,
                               unsigned node, uint8_t *wasm, size_t *length,
                               bool emit, const context_plan *plan)
{
    char path[4096];
    int path_length = snprintf(path, sizeof(path), "%s/context-%u-%u-%u.wasm", directory,
                               kind, variant, node);
    CTX_CHECK(path_length > 0 && (size_t)path_length < sizeof(path));
    if (emit) *length = context_module(wasm, plan, node);
    FILE *file = fopen(path, emit ? "wb" : "rb");
    CTX_CHECK(file != NULL);
    if (emit) CTX_CHECK(fwrite(wasm, 1U, *length, file) == *length);
    else {
        *length = fread(wasm, 1U, CTX_WASM_MAX, file);
        CTX_CHECK(*length > 8U && *length < CTX_WASM_MAX && !ferror(file));
        CTX_CHECK(fgetc(file) == EOF);
    }
    CTX_CHECK(fclose(file) == 0);
    return 0;
}

static uint64_t context_be(const uint8_t *bytes, size_t length)
{
    uint64_t value = 0U;
    for (size_t index = 0U; index < length; ++index) value = (value << 8U) | bytes[index];
    return value;
}

static uint32_t context_le32(const uint8_t *bytes)
{
    return (uint32_t)bytes[0] | ((uint32_t)bytes[1] << 8U) |
           ((uint32_t)bytes[2] << 16U) | ((uint32_t)bytes[3] << 24U);
}

static int context_observations(metered_fixture *f, const context_plan *plan,
                                 unsigned expected_events, uint64_t budget)
{
    static const uint8_t domain[] = "LayerX/programs/events/v1";
    lxp_byte_span envelope = f->receipt.program_outcome.event_envelope_payload;
    size_t cursor = sizeof(domain);
    CTX_CHECK(envelope.length >= cursor + 4U && memcmp(envelope.bytes, domain, cursor) == 0);
    CTX_CHECK(context_be(envelope.bytes + cursor, 4U) == expected_events);
    cursor += 4U;
    for (unsigned event = 0U; event < expected_events; ++event) {
        CTX_CHECK(envelope.length - cursor >= 77U);
        const uint8_t *producer = envelope.bytes + cursor;
        unsigned node = (unsigned)producer[31] - 1U;
        CTX_CHECK(node < plan->count);
        uint8_t expected_producer[32]; context_id(node, expected_producer);
        CTX_CHECK(memcmp(producer, expected_producer, 32U) == 0);
        CTX_CHECK(memcmp(producer + 32U, f->authority.principal, 32U) == 0);
        cursor += 73U;
        CTX_CHECK(context_be(envelope.bytes + cursor, 4U) == 3U);
        cursor += 4U;
        CTX_CHECK(envelope.length - cursor >= 3U + 4U + CTX_RECORD);
        CTX_CHECK(memcmp(envelope.bytes + cursor, "ctx", 3U) == 0);
        cursor += 3U;
        CTX_CHECK(context_be(envelope.bytes + cursor, 4U) == CTX_RECORD);
        cursor += 4U;
        const uint8_t *record = envelope.bytes + cursor;
        if (plan->mode >= 2U && plan->mode <= 5U) {
            CTX_CHECK((int32_t)context_le32(record) ==
                (plan->mode == 4U ? -3 : plan->mode == 3U ? 2 : 32));
        } else {
            static const unsigned sizes[] = {32U,33U,32U,8U,8U,2U,2U,8U,4U};
            for (unsigned field = 0U; field < 9U; ++field) {
                const uint8_t *slot = record + field * 37U;
                unsigned expected = field == 1U && node == 0U ? 1U : sizes[field];
                CTX_CHECK(context_le32(slot) == expected);
                for (unsigned pad = expected; pad < 33U; ++pad) CTX_CHECK(slot[4U + pad] == 0U);
            }
            CTX_CHECK(memcmp(record + 4U, producer, 32U) == 0);
            const uint8_t *caller = record + 41U;
            CTX_CHECK(caller[0] == (node == 0U ? 0U : 1U));
            if (node == 0U) CTX_CHECK(lxp_ct_is_zero(producer + 64U, 9U));
            if (node != 0U) {
                unsigned parent = 0U;
                unsigned depth = producer[72U];
                CTX_CHECK(depth > 0U && depth <= 8U);
                for (unsigned level = 0U; level + 1U < depth; ++level) {
                    unsigned ordinal = producer[64U + level];
                    CTX_CHECK(ordinal > 0U && ordinal <= plan->child_count[parent]);
                    parent = plan->children[parent][ordinal - 1U];
                }
                unsigned last = producer[64U + depth - 1U];
                CTX_CHECK(last > 0U && last <= plan->child_count[parent]);
                CTX_CHECK(plan->children[parent][last - 1U] == node);
                uint8_t expected[32]; context_id(parent, expected);
                CTX_CHECK(memcmp(caller + 1U, expected, 32U) == 0);
            }
            CTX_CHECK(memcmp(record + 78U, f->authority.principal, 32U) == 0);
            CTX_CHECK(context_be(record + 115U, 8U) == f->execution.global_sequence);
            CTX_CHECK(context_be(record + 152U, 8U) == f->execution.batch_number);
            CTX_CHECK(context_be(record + 189U, 2U) == f->receipt.program_outcome.runtime_version);
            CTX_CHECK(context_be(record + 226U, 2U) == 2U);
            uint64_t remaining = context_be(record + 263U, 8U);
            CTX_CHECK(f->receipt.program_outcome.cpu_fuel <= budget);
            CTX_CHECK(remaining < budget && remaining >= budget - f->receipt.program_outcome.cpu_fuel);
            CTX_CHECK(context_be(record + 300U, 4U) == f->execution.recorded_fee_schedule_version);
        }
        cursor += CTX_RECORD;
    }
    CTX_CHECK(cursor == envelope.length);
    return 0;
}

static int context_resource_detail(const lxp_program_outcome *outcome, unsigned resource)
{
    static const uint8_t wrapper[] = "LXP/programs/terminal-applied-legs/v1";
    static const uint8_t domain[] = "LXP/program-execution/v4";
    lxp_byte_span terminal = outcome->terminal_payload;
    size_t cursor = sizeof(wrapper);
    CTX_CHECK(terminal.length >= cursor + 4U);
    CTX_CHECK(memcmp(terminal.bytes, wrapper, cursor) == 0);
    size_t length = (size_t)context_be(terminal.bytes + cursor, 4U);
    cursor += 4U;
    CTX_CHECK(length <= terminal.length - cursor);
    const uint8_t *detail = terminal.bytes + cursor;
    cursor = sizeof(domain);
    CTX_CHECK(length >= cursor + 18U && memcmp(detail, domain, cursor) == 0);
    cursor += 10U;
    uint64_t outputs = context_be(detail + cursor, 8U);
    cursor += 8U;
    for (uint64_t index = 0U; index < outputs; ++index) {
        CTX_CHECK(cursor < length);
        unsigned tag = detail[cursor++];
        CTX_CHECK(tag == 1U || tag == 2U);
        size_t size = tag == 1U ? 4U : 8U;
        CTX_CHECK(size <= length - cursor);
        cursor += size;
    }
    CTX_CHECK(length - cursor >= 61U);
    cursor += 60U;
    unsigned trace = detail[cursor++];
    CTX_CHECK(trace <= 1U);
    if (trace != 0U) {
        CTX_CHECK(length - cursor >= 8U);
        size_t trace_length = (size_t)context_be(detail + cursor, 8U);
        cursor += 8U;
        CTX_CHECK(trace_length <= length - cursor);
        cursor += trace_length;
    }
    CTX_CHECK(length - cursor >= 53U);
    uint8_t root[32]; context_id(0U, root);
    CTX_CHECK(memcmp(detail + cursor, root, 32U) == 0);
    cursor += 32U;
    CTX_CHECK(context_be(detail + cursor, 2U) == 2U);
    cursor += 2U;
    CTX_CHECK(detail[cursor++] == 2U);
    CTX_CHECK(detail[cursor++] == 0U);
    CTX_CHECK(detail[cursor++] == resource);
    CTX_CHECK(context_be(detail + cursor, 8U) == 64U);
    CTX_CHECK(context_be(detail + cursor + 8U, 8U) > 64U);
    if (resource == 4U) {
        CTX_CHECK(context_be(detail + cursor + 8U, 8U) == 65U);
        CTX_CHECK(outcome->output_values <= 64U);
    }
    return 0;
}

static int context_run(unsigned kind, unsigned variant, const char *directory,
                        uint64_t *fuel, uint8_t root[32])
{
    context_plan plan;
    context_topology(kind, variant, &plan);
    metered_fixture *f = calloc(1U, sizeof(*f));
    uint8_t wasm[CTX_WASM_MAX], payload[CTX_WASM_MAX + 2048U];
    uint8_t id[32], hash[32], capabilities[1024], attacker[96];
    size_t length;
    uint64_t budget = kind == 9U ? 64U : 1000000U;
    CTX_CHECK(f != NULL && context_fixture_init(f) == 0);
    for (unsigned node = 0U; node < plan.count; ++node) {
        CTX_CHECK(context_guest_file(directory, kind, variant, node, wasm, &length, false, &plan) == 0);
        context_id(node, id);
        length = program_spend_deploy_payload(payload, id, f->identity->did_id, wasm, length, hash);
        CTX_CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
        CTX_CHECK(metered_activity(f, LX_PROGRAMS_DEPLOY, payload, length, (uint8_t)(node + 1U), false) == 0);
        CTX_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity, &f->execution, &f->receipt) == LXP_OK);
        if (f->receipt.result_code != LXP_OK) (void)fprintf(stderr, "context deploy %u result %d\n", node, f->receipt.result_code);
        CTX_CHECK(f->receipt.result_code == LXP_OK);
    }
    context_id(0U, id);
    size_t capability_length = context_capabilities(capabilities, plan.count);
    (void)memset(attacker, 0xa5, sizeof(attacker));
    static const uint8_t absent_access[] = "LayerX/programs/access-declaration/v1";
    length = call_payload_with_data(payload, id, capabilities, capability_length,
        absent_access, sizeof(absent_access), attacker, sizeof(attacker));
    write_u16(payload + 32U, 2U);
    write_u64(payload + 50U, budget);
    CTX_CHECK(metered_activity(f, LX_PROGRAMS_CALL, payload, length, 0xf0U, false) == 0);
    CTX_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity, &f->execution, &f->receipt) == LXP_OK);
    bool resource = kind == 9U || (kind == 5U && variant != 0U);
    bool refused = kind == 3U || (kind >= 4U && kind <= 7U && variant != 0U) || kind == 9U;
    lxp_result expected_result = resource ? LXP_ERR_GAS_EXHAUSTED :
        refused ? LXP_ERR_PROGRAM_REFUSED : LXP_OK;
    if (f->receipt.result_code != expected_result)
        (void)fprintf(stderr, "context case %u variant %u result %d terminal %u\n", kind, variant,
                      f->receipt.result_code, f->receipt.program_outcome.terminal_kind);
    CTX_CHECK(f->receipt.result_code == expected_result);
    CTX_CHECK(f->receipt.program_outcome.present && f->receipt.program_outcome.abi_version == 2U);
    CTX_CHECK(f->receipt.program_outcome.cpu_fuel > 0U);
    if (refused) {
        CTX_CHECK(f->receipt.program_outcome.terminal_kind ==
            (resource ? LXP_PROGRAM_TERMINAL_RESOURCE : LXP_PROGRAM_TERMINAL_FAILURE));
        CTX_CHECK(f->receipt.effects.count == 0U);
        CTX_CHECK(lxp_ct_is_zero(f->receipt.transfer_set_root, 32U));
        if (resource) CTX_CHECK(context_resource_detail(&f->receipt.program_outcome, kind == 5U ? 4U : 0U) == 0);
    } else {
        unsigned edges = 0U;
        for (unsigned node = 0U; node < plan.count; ++node) edges += plan.child_count[node];
        CTX_CHECK(context_observations(f, &plan, plan.edge_observations ? edges : edges * 2U + 1U, budget) == 0);
        CTX_CHECK(f->receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS);
    }
    *fuel = f->receipt.program_outcome.cpu_fuel;
    (void)memcpy(root, f->receipt.resulting_state_root, 32U);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CTX_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f);
    return 0;
}

int main(int argc, char **argv)
{
    if (argc == 2 && strcmp(argv[1], "--list-cases") == 0) {
        for (unsigned kind = 0U; kind < sizeof(context_cases) / sizeof(context_cases[0]); ++kind)
            (void)puts(context_cases[kind]);
        return 0;
    }
    if (argc == 3 && strcmp(argv[1], "--emit-wasm") == 0) {
        for (unsigned kind = 0U; kind < sizeof(context_cases) / sizeof(context_cases[0]); ++kind)
            for (unsigned variant = 0U; variant < context_variants(kind); ++variant) {
                context_plan plan; context_topology(kind, variant, &plan);
                for (unsigned node = 0U; node < plan.count; ++node) {
                    uint8_t wasm[CTX_WASM_MAX]; size_t length = 0U;
                    CTX_CHECK(context_guest_file(argv[2], kind, variant, node, wasm, &length, true, &plan) == 0);
                }
            }
        return 0;
    }
    if (argc == 5 && strcmp(argv[1], "--case") == 0 && strcmp(argv[3], "--wasm-dir") == 0) {
        for (unsigned kind = 0U; kind < sizeof(context_cases) / sizeof(context_cases[0]); ++kind) {
            if (strcmp(argv[2], context_cases[kind]) != 0) continue;
            uint64_t fuel[4] = {0U}; uint8_t roots[4][32];
            for (unsigned variant = 0U; variant < context_variants(kind); ++variant)
                CTX_CHECK(context_run(kind, variant, argv[4], &fuel[variant], roots[variant]) == 0);
            if (kind == 8U) {
                CTX_CHECK(fuel[0] == fuel[1] + 30U);
                CTX_CHECK(fuel[3] == fuel[2] + 32U);
            }
            if (kind == 10U) {
                CTX_CHECK(context_run(kind, 0U, argv[4], &fuel[1], roots[1]) == 0);
                CTX_CHECK(fuel[0] == fuel[1] && memcmp(roots[0], roots[1], 32U) == 0);
            }
            (void)printf("PASS %s\n", context_cases[kind]);
            return 0;
        }
    }
    (void)fprintf(stderr, "invalid context fixture selector\n");
    return 2;
}
