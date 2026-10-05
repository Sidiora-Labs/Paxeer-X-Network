#include "layerx/programs.h"

#include "layerx/lxp_kernel.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#if defined(__has_feature)
#if __has_feature(address_sanitizer)
#define SANDBOX_ADDRESS_SANITIZER 1
#endif
#endif
#if defined(__SANITIZE_ADDRESS__)
#define SANDBOX_ADDRESS_SANITIZER 1
#endif


static int registration_contract(void)
{
    static const uint32_t expected_types[] = {
        LX_PROGRAMS_DEPLOY,
        LX_PROGRAMS_UPGRADE,
        LX_PROGRAMS_CALL,
        LX_PROGRAMS_REGISTRY,
        LX_PROGRAMS_TRANSFER
    };
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    const lxp_module_iface *current = programs_module_registration();
    const lxp_module_iface *next = programs_module_registration_v2();
    const lxp_module_iface *sandbox = programs_module_registration_v3();
    const lxp_module_iface *destroy = programs_module_registration_v4();
    const lxp_module_registration *resolved;
    uint64_t parameters = 1U;
    size_t i;
    if (current == NULL || current != lx_programs_module_iface() ||
        current->module_id != LXP_MODULE_PROGRAMS ||
        current->abi_version != LX_PROGRAMS_ABI_VERSION ||
        strcmp(current->name, "programs") != 0 ||
        current->activity_type_count !=
            sizeof(expected_types) / sizeof(expected_types[0]) ||
        current->genesis == NULL || current->decode == NULL ||
        current->validate == NULL || current->execute == NULL ||
        current->epoch_begin == NULL || current->epoch_end == NULL ||
        current->state_root == NULL)
        return 1;
    if (next == NULL || next->module_id != LXP_MODULE_PROGRAMS ||
        next->abi_version != LX_PROGRAMS_ACCOUNT_ABI_VERSION ||
        strcmp(next->name, "programs") != 0 ||
        next->activity_type_count !=
            sizeof(expected_types) / sizeof(expected_types[0]) + 3U)
        return 1;
    if (sandbox == NULL || sandbox->module_id != LXP_MODULE_PROGRAMS ||
        sandbox->abi_version != LX_PROGRAMS_SANDBOX_ABI_VERSION ||
        strcmp(sandbox->name, "programs") != 0 ||
        sandbox->activity_type_count !=
            sizeof(expected_types) / sizeof(expected_types[0]) + 4U ||
        sandbox->activity_types[sandbox->activity_type_count - 1U] !=
            LX_PROGRAMS_SANDBOX)
        return 1;
    if (destroy == NULL || destroy->module_id != LXP_MODULE_PROGRAMS ||
        destroy->abi_version != LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION ||
        destroy->activity_type_count !=
            sizeof(expected_types) / sizeof(expected_types[0]) + 5U ||
        destroy->activity_types[destroy->activity_type_count - 1U] !=
            LX_PROGRAMS_SANDBOX_DESTROY)
        return 1;
    for (i = 0U; i < current->activity_type_count; ++i)
        if (current->activity_types[i] != expected_types[i] ||
            next->activity_types[i] != expected_types[i] ||
            sandbox->activity_types[i] != expected_types[i])
            return 1;
    if (next->activity_types[next->activity_type_count - 3U] !=
            LX_PROGRAMS_ACCOUNT ||
        next->activity_types[next->activity_type_count - 2U] !=
            LX_PROGRAMS_WIND_DOWN ||
        next->activity_types[next->activity_type_count - 1U] !=
            LX_PROGRAMS_FEE_GOVERNANCE)
        return 1;
    if (lxp_state_store_init(&store, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) !=
            LXP_OK ||
        lxp_kernel_register_module(&kernel, current) != LXP_OK)
        return 1;
    for (i = 0U; i < current->activity_type_count; ++i)
        if (lxp_kernel_module_for_activity(&kernel, expected_types[i], 0U,
                                           &resolved) != LXP_OK ||
            resolved->iface != current ||
            resolved->abi_version != LX_PROGRAMS_ABI_VERSION)
            return 1;
    if (lxp_kernel_module_for_activity(&kernel, LX_PROGRAMS_ACCOUNT, 0U,
                                       &resolved) !=
            LXP_ERR_UNKNOWN_ACTIVITY ||
        lxp_kernel_set_epoch(&kernel, 1U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, next) != LXP_OK ||
        lxp_module_version_for_epoch(&kernel, LXP_MODULE_PROGRAMS, 0U,
                                     LX_PROGRAMS_ABI_VERSION, &resolved) !=
            LXP_OK ||
        resolved->iface != current ||
        lxp_module_version_for_epoch(&kernel, LXP_MODULE_PROGRAMS, 1U,
                                     LX_PROGRAMS_ACCOUNT_ABI_VERSION,
                                     &resolved) != LXP_OK ||
        resolved->iface != next ||
        lxp_kernel_module_for_activity(&kernel, LX_PROGRAMS_ACCOUNT, 1U,
                                       &resolved) != LXP_OK ||
        resolved->iface != next ||
        lxp_kernel_module_for_activity(&kernel, LX_PROGRAMS_WIND_DOWN, 1U,
                                       &resolved) != LXP_OK ||
        resolved->iface != next ||
        lxp_module_version_for_epoch(&kernel, LXP_MODULE_PROGRAMS, 1U,
                                     LX_PROGRAMS_ABI_VERSION, &resolved) !=
            LXP_ERR_VERSION_UNSUPPORTED)
        return 1;
    if (lxp_kernel_set_epoch(&kernel, 2U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, sandbox) != LXP_OK ||
        lxp_kernel_module_for_activity(&kernel, LX_PROGRAMS_SANDBOX, 2U,
                                       &resolved) != LXP_OK ||
        resolved->iface != sandbox ||
        resolved->abi_version != LX_PROGRAMS_SANDBOX_ABI_VERSION)
        return 1;
    if (lxp_kernel_set_epoch(&kernel, 3U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, destroy) != LXP_OK ||
        lxp_kernel_module_for_activity(&kernel,
            LX_PROGRAMS_SANDBOX_DESTROY, 3U, &resolved) != LXP_OK ||
        resolved->iface != destroy ||
        resolved->abi_version != LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION)
        return 1;
    return lxp_state_store_destroy(&store) == LXP_OK ? 0 : 1;
}

static int exercise(uint16_t ordinal, size_t payload_length,
                    lxp_result expected, size_t expected_writes)
{
    uint8_t arena_bytes[4096];
    uint8_t payload[40];
    lxp_arena arena;
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity;
    lxp_authority_resolved authority;
    const lxp_module_registration *registration;
    lxp_result module_result = LXP_OK;
    uint64_t parameters = 1U;
    (void)memset(payload, 0x31, sizeof(payload));
    (void)memset(&activity, 0, sizeof(activity));
    (void)memset(&authority, 0, sizeof(authority));
    (void)memset(authority.principal, 0x42, sizeof(authority.principal));
    activity.activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity.payload = (lxp_byte_span){ payload, payload_length };
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_state_store_init(&store, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) !=
            LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) !=
            LXP_OK ||
        lxp_kernel_module_for_activity(&kernel, activity.activity_type, 0U,
                                       &registration) != LXP_OK ||
        registration->abi_version != LX_PROGRAMS_ABI_VERSION ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS, 1U, 0U, 9U,
                            1000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK ||
        lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
                            &effects, &module_result) != LXP_OK)
        return 1;
    if (module_result != expected) return 1;
    if (module_result == LXP_OK) {
        if (lxp_module_ctx_commit(&ctx) != LXP_OK || effects.count != 1U ||
            effects.effects[0].module_id != LXP_MODULE_PROGRAMS ||
            effects.effects[0].event_type != ordinal ||
            kernel.module_kv_count != expected_writes)
            return 1;
    } else {
        lxp_module_ctx_rollback(&ctx);
        if (kernel.module_kv_count != 0U || effects.count != 0U) return 1;
    }
    return lxp_state_store_destroy(&store) == LXP_OK ? 0 : 1;
}

#ifdef SANDBOX_ADDRESS_SANITIZER
static void put_u16(uint8_t *bytes, uint16_t value)
{
    bytes[0] = (uint8_t)(value >> 8U);
    bytes[1] = (uint8_t)value;
}

static void put_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U);
    bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U);
    bytes[3] = (uint8_t)value;
}

static int sandbox_case(const lxp_module_registration *registration,
                        lxp_module_ctx *ctx, lxp_effect_buffer *effects,
                        const char *name, const uint8_t *source, size_t length,
                        lxp_result expected, size_t *count)
{
    uint8_t *payload = malloc(length == 0U ? 1U : length);
    void *decoded = NULL;
    lxp_result result;
    if (payload == NULL || lxp_arena_reset(ctx->arena, 0U) != LXP_OK) {
        free(payload);
        return 1;
    }
    if (length != 0U) (void)memcpy(payload, source, length);
    result = registration->iface->decode(ctx,
        lxp_activity_type_ordinal(LX_PROGRAMS_SANDBOX), payload, length, &decoded);
    free(payload);
    if (result != expected || (result == LXP_OK) != (decoded != NULL) ||
        ctx->staged_count != 0U || ctx->staged_account_count != 0U ||
        ctx->staged_blob_count != 0U || ctx->identity_staged ||
        effects->count != 0U || ctx->kernel->module_kv_count != 0U) {
        (void)fprintf(stderr, "sandbox case %s abi=%u length=%zu: result=%d expected=%d\n",
                      name, (unsigned)registration->abi_version, length,
                      (int)result, (int)expected);
        return 1;
    }
    ++*count;
    (void)printf("SANDBOX_CASE {\"name\":\"%s\",\"abi\":%u,\"length\":%zu,"
                 "\"result\":%d,\"expected\":%d,\"staged\":0,\"effects\":0}\n",
                 name, (unsigned)registration->abi_version, length,
                 (int)result, (int)expected);
    return 0;
}

#endif

static int sandbox_bounds(void)
{
#ifndef SANDBOX_ADDRESS_SANITIZER
    (void)fprintf(stderr, "sandbox bounds requires AddressSanitizer\n");
    return 1;
#else
    static const size_t fixed[] = {236U, 248U, 76U};
    static const size_t protected_fields[] = {4U, 44U, 76U, 108U, 140U};
    const size_t call_fixed = 50U + LX_PROGRAMS_CALL_BUDGET_FIELDS * 8U;
    const size_t largest_call = call_fixed + LX_PROGRAMS_MAX_ENTRYPOINT_BYTES +
        LX_PROGRAMS_MAX_CALLDATA_BYTES + UINT16_MAX +
        LX_PROGRAMS_MAX_ACCESS_DECLARATION_BYTES;
    const size_t capacity = 236U + largest_call + 1U;
    uint8_t *payload = calloc(capacity, 1U);
    uint8_t *arena_bytes = malloc(32U * 1024U * 1024U);
    lxp_module_ctx *ctx = calloc(1U, sizeof(*ctx));
    lxp_kernel *kernel = calloc(1U, sizeof(*kernel));
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_arena arena;
    lxp_effect_buffer effects;
    const lxp_module_registration *registration;
    uint64_t parameters = 1U;
    size_t count = 0U, abi, operation, length, index;
    int failed = 1;
    if (payload == NULL || arena_bytes == NULL || ctx == NULL || kernel == NULL)
        goto cleanup;
    if (lxp_state_store_init(&store, 0U) != LXP_OK) goto cleanup;
    if (lxp_kernel_create(kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, 32U * 1024U * 1024U) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK)
        goto destroy;
    for (abi = 3U; abi <= 4U; ++abi) {
        const lxp_module_iface *iface = abi == 3U ?
            programs_module_registration_v3() : programs_module_registration_v4();
        if (lxp_kernel_set_epoch(kernel, (uint64_t)abi) != LXP_OK ||
            lxp_kernel_register_module(kernel, iface) != LXP_OK ||
            lxp_kernel_module_for_activity(kernel, LX_PROGRAMS_SANDBOX,
                (uint64_t)abi, &registration) != LXP_OK ||
            registration->iface != iface ||
            lxp_module_ctx_init(ctx, kernel, LXP_MODULE_PROGRAMS, 1U,
                (uint64_t)abi, 1U, UINT64_MAX, &arena, false) != LXP_OK ||
            lxp_module_ctx_bind_effects(ctx, &effects) != LXP_OK)
            goto destroy;
#define CASE(name, size, result) do { \
    if (sandbox_case(registration, ctx, &effects, (name), payload, \
                     (size), (result), &count) != 0) goto destroy; \
} while (0)
        for (operation = 1U; operation <= 3U; ++operation) {
            char name[32];
            (void)memset(payload, 0, capacity);
            payload[0] = 1U;
            payload[1] = (uint8_t)operation;
            (void)snprintf(name, sizeof(name), "prefix-%zu", operation);
            for (length = 0U; length < fixed[operation - 1U]; ++length)
                CASE(name, length, LXP_ERR_TRUNCATED);
            CASE("fixed-minimum", fixed[operation - 1U],
                 operation == 1U ? LXP_ERR_NON_CANONICAL :
                 operation == 2U ? LXP_ERR_TRUNCATED : LXP_OK);
            if (operation == 3U) {
                CASE("activate-trailing", 77U, LXP_ERR_NON_CANONICAL);
                (void)memset(payload + 4U, 0x25, 72U);
                CASE("activate-canonical", 76U, LXP_OK);
            }
            for (index = 0U; index < 4U; ++index) {
                const uint8_t saved = payload[index];
                payload[index] = index < 2U ? UINT8_MAX : 1U;
                CASE("invalid-header", fixed[operation - 1U], LXP_ERR_NON_CANONICAL);
                payload[index] = saved;
            }
        }
        (void)memset(payload, 0, capacity);
        payload[0] = 1U; payload[1] = 1U;
        for (index = 0U; index < sizeof(protected_fields) / sizeof(protected_fields[0]); ++index)
            (void)memset(payload + protected_fields[index], 0x31, 32U);
        put_u32(payload + 172U, 1U);
        (void)memset(payload + 236U, 0x41, 32U);
        put_u16(payload + 268U, 1U);
        put_u16(payload + 270U, 1U);
        payload[236U + call_fixed] = (uint8_t)'f';
        put_u32(payload + 232U, (uint32_t)(call_fixed + 1U));
        CASE("execute-canonical", 236U + call_fixed + 1U, LXP_OK);
        CASE("execute-trailing", 236U + call_fixed + 2U, LXP_ERR_NON_CANONICAL);
        for (length = 1U; length < call_fixed; ++length) {
            put_u32(payload + 232U, (uint32_t)length);
            CASE("nested-call-prefix", 236U + length, LXP_ERR_TRUNCATED);
        }
        put_u32(payload + 232U, (uint32_t)call_fixed);
        CASE("nested-call-missing-entrypoint", 236U + call_fixed, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 232U, UINT32_MAX);
        CASE("execute-length-overflow", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 232U, 0U);
        CASE("execute-empty-call", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 232U, (uint32_t)(call_fixed + 1U));
        for (index = 0U; index < sizeof(protected_fields) / sizeof(protected_fields[0]); ++index) {
            (void)memset(payload + protected_fields[index], 0, 32U);
            CASE("execute-zero-identifier", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
            (void)memset(payload + protected_fields[index], 0x31, 32U);
        }
        put_u32(payload + 172U, 0U);
        CASE("execute-zero-fee-version", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 172U, 1U);
        payload[236U + call_fixed] = (uint8_t)'!';
        CASE("nested-call-invalid-entrypoint", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
        payload[236U + call_fixed] = (uint8_t)'f';
        put_u32(payload + 272U, LX_PROGRAMS_MAX_CALLDATA_BYTES + 1U);
        CASE("nested-call-over-limit", 236U + call_fixed + 1U, LXP_ERR_NON_CANONICAL);
        put_u16(payload + 270U, LX_PROGRAMS_MAX_ENTRYPOINT_BYTES);
        put_u32(payload + 272U, LX_PROGRAMS_MAX_CALLDATA_BYTES);
        put_u16(payload + 276U, UINT16_MAX);
        put_u32(payload + 278U, LX_PROGRAMS_MAX_ACCESS_DECLARATION_BYTES);
        put_u32(payload + 282U, LX_PROGRAMS_MAX_RESPONSE_BYTES);
        (void)memset(payload + 236U + call_fixed, 'f', LX_PROGRAMS_MAX_ENTRYPOINT_BYTES);
        put_u32(payload + 232U, (uint32_t)largest_call);
        CASE("execute-maximum-nested-fields", 236U + largest_call, LXP_OK);
        (void)memset(payload, 0, capacity);
        payload[0] = 1U; payload[1] = 2U;
        put_u32(payload + 248U, 1U);
        payload[252U] = 1U;
        put_u32(payload + 253U, 146U);
        (void)memset(payload + 257U, 0x41, 32U);
        put_u16(payload + 289U, 1U);
        CASE("fund-canonical", 403U, LXP_OK);
        CASE("fund-trailing", 404U, LXP_ERR_NON_CANONICAL);
        for (length = 248U; length < 252U; ++length)
            CASE("fund-lifecycle-length-prefix", length, LXP_ERR_TRUNCATED);
        CASE("fund-missing-lifecycle", 252U, LXP_ERR_NON_CANONICAL);
        for (length = 253U; length < 257U; ++length)
            CASE("fund-transfer-length-prefix", length, LXP_ERR_TRUNCATED);
        for (length = 1U; length < 34U; ++length) {
            put_u32(payload + 253U, (uint32_t)length);
            CASE("nested-transfer-prefix", 257U + length, LXP_ERR_TRUNCATED);
        }
        put_u32(payload + 253U, 34U);
        CASE("nested-transfer-missing-leg", 291U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 253U, UINT32_MAX);
        CASE("fund-transfer-overflow", 403U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 248U, UINT32_MAX);
        CASE("fund-lifecycle-overflow", 403U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 248U, 0U);
        CASE("fund-empty-lifecycle", 403U, LXP_ERR_NON_CANONICAL);
        put_u32(payload + 248U, 1U);
        put_u32(payload + 253U, 34U + LXP_MAX_TRANSFER_SET_LEGS * 112U);
        put_u16(payload + 289U, LXP_MAX_TRANSFER_SET_LEGS);
        CASE("fund-maximum-transfer", 257U + 34U + LXP_MAX_TRANSFER_SET_LEGS * 112U, LXP_OK);
        put_u16(payload + 289U, LXP_MAX_TRANSFER_SET_LEGS + 1U);
        CASE("fund-transfer-over-limit", 257U + 34U + LXP_MAX_TRANSFER_SET_LEGS * 112U, LXP_ERR_LENGTH_LIMIT);
#undef CASE
    }
    (void)printf("SANDBOX_BOUNDS detector=address-sanitizer cases=%zu skipped=0\n", count);
    failed = 0;
destroy:
    if (lxp_state_store_destroy(&store) != LXP_OK) failed = 1;
cleanup:
    free(kernel); free(ctx); free(arena_bytes); free(payload);
    return failed;
#endif
}

static int abi_transition_matrix(void)
{
    static const uint16_t versions[] = {
        LX_PROGRAMS_ABI_VERSION, LX_PROGRAMS_ACCOUNT_ABI_VERSION,
        LX_PROGRAMS_SANDBOX_ABI_VERSION, LX_PROGRAMS_GUEST_ABI_V4_VERSION,
        LX_PROGRAMS_GUEST_ABI_V5_VERSION
    };
    size_t current;
    size_t requested;
    size_t count = 0U;
    for (current = 0U; current < sizeof(versions) / sizeof(versions[0]); ++current) {
        if (lxp_programs_abi_transition_validate(0U, versions[current]) != LXP_OK)
            return 1;
        ++count;
        for (requested = 0U; requested < sizeof(versions) / sizeof(versions[0]); ++requested) {
            lxp_result expected = versions[requested] < versions[current]
                ? LXP_ERR_VERSION_UNSUPPORTED : LXP_OK;
            if (lxp_programs_abi_transition_validate(versions[current], versions[requested]) != expected)
                return 1;
            ++count;
        }
        if (lxp_programs_abi_transition_validate(versions[current], 0U) != LXP_ERR_VERSION_UNSUPPORTED ||
            lxp_programs_abi_transition_validate(versions[current], UINT16_MAX) != LXP_ERR_VERSION_UNSUPPORTED ||
            lxp_programs_abi_transition_validate(UINT16_MAX, versions[current]) != LXP_ERR_VERSION_UNSUPPORTED)
            return 1;
        count += 3U;
    }
    (void)printf("ABI_POLICY_NATIVE tests=%zu skipped=0\n", count);
    return 0;
}

int main(int argc, char **argv)
{
    if (argc == 2 && strcmp(argv[1], "--sandbox-bounds") == 0)
        return sandbox_bounds();
    if (argc != 1) return 1;
    if (lxp_programs_abi_transition_validate(0U, LX_PROGRAMS_ABI_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(0U, LX_PROGRAMS_ACCOUNT_ABI_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_ABI_VERSION,
                                             LX_PROGRAMS_ACCOUNT_ABI_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(0U,
                                             LX_PROGRAMS_SANDBOX_ABI_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_ACCOUNT_ABI_VERSION,
                                             LX_PROGRAMS_SANDBOX_ABI_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_ACCOUNT_ABI_VERSION,
                                             LX_PROGRAMS_ABI_VERSION) !=
            LXP_ERR_VERSION_UNSUPPORTED ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_SANDBOX_ABI_VERSION,
                                             LX_PROGRAMS_ACCOUNT_ABI_VERSION) !=
            LXP_ERR_VERSION_UNSUPPORTED ||
        lxp_programs_abi_transition_validate(0U,
                                             LX_PROGRAMS_GUEST_ABI_V4_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_SANDBOX_ABI_VERSION,
                                             LX_PROGRAMS_GUEST_ABI_V4_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_GUEST_ABI_V4_VERSION,
                                             LX_PROGRAMS_SANDBOX_ABI_VERSION) !=
            LXP_ERR_VERSION_UNSUPPORTED ||
        lxp_programs_abi_transition_validate(0U,
                                             LX_PROGRAMS_GUEST_ABI_V5_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_GUEST_ABI_V4_VERSION,
                                             LX_PROGRAMS_GUEST_ABI_V5_VERSION) != LXP_OK ||
        lxp_programs_abi_transition_validate(LX_PROGRAMS_GUEST_ABI_V5_VERSION,
                                             LX_PROGRAMS_GUEST_ABI_V4_VERSION) !=
            LXP_ERR_VERSION_UNSUPPORTED ||
        lxp_programs_abi_transition_validate(0U,
                                             LX_PROGRAMS_GUEST_ABI_V5_VERSION + 1U) !=
            LXP_ERR_VERSION_UNSUPPORTED)
        return 1;
    if (abi_transition_matrix() != 0) return 1;
    if (registration_contract() != 0) return 1;
    if (exercise(lxp_activity_type_ordinal(LX_PROGRAMS_CALL), 40U,
                 LXP_ERR_TRUNCATED, 0U) != 0) return 1;
    if (exercise(lxp_activity_type_ordinal(LX_PROGRAMS_REGISTRY), 32U,
                 LXP_OK, 0U) != 0) return 1;
    if (exercise(lxp_activity_type_ordinal(LX_PROGRAMS_CALL), 31U,
                 LXP_ERR_TRUNCATED, 0U) != 0) return 1;
    return 0;
}
