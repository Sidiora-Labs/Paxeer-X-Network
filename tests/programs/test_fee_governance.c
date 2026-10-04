#include "layerx/lxp_kernel.h"
#include "layerx/programs.h"

#include <stdio.h>
#include <string.h>

enum {
    FEE_RECORD_BYTES = 217,
    FEE_PENDING_BYTES = 197,
    FEE_HISTORY_KEY_BYTES = 23
};

static const uint8_t active_key[] = "progfee/active/v1";
static const uint8_t pending_key[] = "progfee/pending/v1";
static const uint8_t history_prefix[] = "progfee/history/v1/";

static void write_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U);
    bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U);
    bytes[3] = (uint8_t)value;
}

static void write_u64(uint8_t *bytes, uint64_t value)
{
    size_t index;
    for (index = 0U; index < 8U; ++index)
        bytes[index] = (uint8_t)(value >> (56U - index * 8U));
}

static void prices(uint8_t *bytes, const lx_programs_fee_schedule *schedule)
{
    const uint64_t values[LX_PROGRAMS_FEE_PRICE_FIELDS] = {
        schedule->cpu, schedule->memory_byte, schedule->storage_read_byte,
        schedule->storage_write_byte, schedule->output_value,
        schedule->output_byte, schedule->occupancy_byte_batch
    };
    size_t index;
    for (index = 0U; index < LX_PROGRAMS_FEE_PRICE_FIELDS; ++index)
        write_u64(bytes + index * 8U, values[index]);
}

static void policy(uint8_t *bytes)
{
    static const uint64_t values[6] = {100U, 1U, 1U, 10U, 10U, 1000U};
    size_t index;
    for (index = 0U; index < 6U; ++index)
        write_u64(bytes + index * 8U, values[index]);
}

static void fee_record(uint8_t encoded[FEE_RECORD_BYTES],
                       const lx_programs_fee_schedule *schedule,
                       uint8_t asset_marker, uint64_t activation_batch,
                       uint64_t last_batch, uint64_t sequence)
{
    size_t offset = 0U;
    (void)memset(encoded, 0, FEE_RECORD_BYTES);
    (void)memcpy(encoded + offset, "LXFR1", 5U); offset += 5U;
    write_u32(encoded + offset, schedule->version); offset += 4U;
    prices(encoded + offset, schedule); offset += 56U;
    (void)memset(encoded + offset, asset_marker, 32U); offset += 32U;
    policy(encoded + offset); offset += 48U;
    write_u64(encoded + offset, activation_batch); offset += 8U;
    write_u64(encoded + offset, last_batch); offset += 8U;
    write_u64(encoded + offset, sequence); offset += 8U;
    (void)memset(encoded + offset, (int)(0xa0U + schedule->version), 32U);
}

static void fee_pending(uint8_t encoded[FEE_PENDING_BYTES],
                        const lx_programs_fee_schedule *schedule,
                        uint8_t asset_marker, uint64_t activation_batch,
                        uint64_t staged_batch, uint64_t sequence)
{
    size_t offset = 0U;
    (void)memset(encoded, 0, FEE_PENDING_BYTES);
    (void)memcpy(encoded + offset, "LXFP1", 5U); offset += 5U;
    prices(encoded + offset, schedule); offset += 56U;
    (void)memset(encoded + offset, asset_marker, 32U); offset += 32U;
    policy(encoded + offset); offset += 48U;
    write_u64(encoded + offset, activation_batch); offset += 8U;
    write_u64(encoded + offset, staged_batch); offset += 8U;
    write_u64(encoded + offset, sequence); offset += 8U;
    (void)memset(encoded + offset, 0xd2, 32U);
}

static void put(lxp_kernel *kernel, const uint8_t *key, size_t key_length,
                const uint8_t *value, size_t value_length)
{
    lxp_module_kv_entry *entry = &kernel->module_kv[kernel->module_kv_count++];
    (void)memset(entry, 0, sizeof(*entry));
    entry->module_id = LXP_MODULE_PROGRAMS;
    entry->key_length = (uint16_t)key_length;
    entry->value_length = (uint32_t)value_length;
    (void)memcpy(entry->key, key, key_length);
    (void)memcpy(entry->value, value, value_length);
}

static void put_history(lxp_kernel *kernel, uint32_t version,
                        const uint8_t record[FEE_RECORD_BYTES])
{
    uint8_t key[FEE_HISTORY_KEY_BYTES];
    (void)memcpy(key, history_prefix, sizeof(history_prefix) - 1U);
    write_u32(key + sizeof(history_prefix) - 1U, version);
    put(kernel, key, sizeof(key), record, FEE_RECORD_BYTES);
}

static int pending_and_history_vectors(void)
{
    static uint8_t arena_bytes[16384];
    const lx_programs_fee_schedule first = {1U, 2U, 3U, 5U, 7U, 11U, 13U, 100U};
    const lx_programs_fee_schedule proposed = {0U, 17U, 19U, 23U, 29U, 31U, 37U, 105U};
    const lx_programs_fee_schedule second = {2U, 17U, 19U, 23U, 29U, 31U, 37U, 105U};
    uint8_t first_record[FEE_RECORD_BYTES];
    uint8_t second_record[FEE_RECORD_BYTES];
    uint8_t pending[FEE_PENDING_BYTES];
    uint8_t digest[32];
    uint8_t asset[32];
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lx_programs_fee_schedule selected;
    uint32_t parameter_version = 77U;
    uint64_t activation;
    if (lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal,
                          &parameter_version, 77U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) !=
            LXP_OK)
        return 1;
    fee_record(first_record, &first, 0x41U, 1U, 7U, 11U);
    fee_record(second_record, &second, 0x42U, 9U, 8U, 12U);
    fee_pending(pending, &proposed, 0x42U, 9U, 8U, 12U);
    put(&kernel, active_key, sizeof(active_key) - 1U,
        first_record, sizeof(first_record));
    put_history(&kernel, 1U, first_record);
    put_history(&kernel, 2U, second_record);
    put(&kernel, pending_key, sizeof(pending_key) - 1U,
        pending, sizeof(pending));
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
                            20U, 77U, 8U, UINT64_MAX, &arena, true) != LXP_OK)
        return 1;
    if (lxp_programs_fee_governance_pending(
            &ctx, &selected, &activation, digest) != LXP_OK ||
        selected.version != 0U || selected.cpu != proposed.cpu ||
        selected.occupancy_byte_batch != 105U || activation != 9U ||
        digest[0] != 0xd2U)
        return 2;
    if (lxp_programs_fee_schedule_current(&ctx, &selected, asset) != LXP_OK ||
        selected.version != 1U || selected.cpu != first.cpu ||
        selected.occupancy_byte_batch != 100U || asset[0] != 0x41U)
        return 3;
    if (lxp_programs_fee_schedule_at(&ctx, 2U, &selected, asset) != LXP_OK ||
        selected.version != 2U || selected.cpu != second.cpu ||
        asset[0] != 0x42U ||
        lxp_programs_fee_schedule_at(&ctx, 3U, &selected, asset) !=
            LXP_ERR_VERSION_UNSUPPORTED)
        return 4;
    if (lxp_programs_fee_governance_resolve_runtime(
            &kernel, 1U, &selected, asset) != LXP_OK ||
        selected.cpu * 9U != 18U ||
        lxp_programs_fee_governance_resolve_runtime(
            &kernel, 2U, &selected, asset) != LXP_OK ||
        selected.cpu * 9U != 153U || kernel.epoch != 77U)
        return 5;
    return 0;
}

static int occupancy_vector(lxp_u128 observed, uint64_t expected,
                            uint32_t expected_version)
{
    static uint8_t arena_bytes[16384];
    const lx_programs_fee_schedule initial = {1U, 2U, 3U, 5U, 7U, 11U, 13U, 100U};
    uint8_t record[FEE_RECORD_BYTES];
    uint8_t asset[32];
    lxp_programs_occupancy_receipt receipt;
    lx_programs_fee_schedule current;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    uint32_t parameter_version = 901U;
    size_t index;
    if (lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal,
                          &parameter_version, 41U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) !=
            LXP_OK)
        return 1;
    fee_record(record, &initial, 0x51U, 1U, 1U, 1U);
    put(&kernel, active_key, sizeof(active_key) - 1U, record, sizeof(record));
    put_history(&kernel, 1U, record);
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
                            10U, 41U, 2U, UINT64_MAX, &arena, true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    ctx.batch_number = 2U;
    (void)memset(&receipt, 0, sizeof(receipt));
    receipt.batch_number = 2U;
    receipt.parameter_version = parameter_version;
    receipt.schedule_version = 1U;
    receipt.schedule_prices[0] = initial.cpu;
    receipt.schedule_prices[1] = initial.memory_byte;
    receipt.schedule_prices[2] = initial.storage_read_byte;
    receipt.schedule_prices[3] = initial.storage_write_byte;
    receipt.schedule_prices[4] = initial.output_value;
    receipt.schedule_prices[5] = initial.output_byte;
    receipt.schedule_prices[6] = initial.occupancy_byte_batch;
    (void)memset(receipt.occupancy_asset_id, 0x51, 32U);
    receipt.byte_batches = observed;
    {
        lxp_programs_occupancy_receipt invalid = receipt;
        invalid.schedule_prices[0] += 1U;
        if (lxp_programs_fee_governance_observe_batch(&ctx, &invalid) !=
                LXP_ERR_CONTEXT_MISMATCH ||
            lxp_programs_fee_schedule_current(&ctx, &current, asset) != LXP_OK ||
            current.version != 1U || current.occupancy_byte_batch != 100U)
            return 4;
        invalid = receipt;
        invalid.schedule_version = 2U;
        if (lxp_programs_fee_governance_observe_batch(&ctx, &invalid) !=
                LXP_ERR_CONTEXT_MISMATCH)
            return 5;
        invalid = receipt;
        invalid.occupancy_asset_id[0] ^= 1U;
        if (lxp_programs_fee_governance_observe_batch(&ctx, &invalid) !=
                LXP_ERR_CONTEXT_MISMATCH)
            return 6;
    }
    if (lxp_programs_fee_governance_observe_batch(&ctx, &receipt) != LXP_OK ||
        lxp_programs_fee_schedule_current(&ctx, &current, asset) != LXP_OK ||
        current.version != expected_version ||
        current.occupancy_byte_batch != expected ||
        asset[0] != 0x51U)
        return 2;
    for (index = 0U; index < 6U; ++index)
        if (((const uint64_t *)&current.cpu)[index] !=
            ((const uint64_t *)&initial.cpu)[index])
            return 3;
    if (lxp_programs_fee_governance_observe_batch(&ctx, &receipt) !=
            LXP_ERR_CONTEXT_MISMATCH)
        return 7;
    if (lxp_programs_fee_schedule_at(&ctx, 1U, &current, asset) != LXP_OK ||
        current.version != 1U || current.occupancy_byte_batch != 100U)
        return 8;
    return 0;
}

static int activation_and_authority_refusals(void)
{
    static uint8_t arena_bytes[16384];
    const lx_programs_fee_schedule first = {1U, 2U, 3U, 5U, 7U, 11U, 13U, 100U};
    const lx_programs_fee_schedule proposed = {0U, 17U, 19U, 23U, 29U, 31U, 37U, 105U};
    uint8_t record[FEE_RECORD_BYTES], pending[FEE_PENDING_BYTES], asset[32];
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lx_programs_fee_schedule selected;
    uint32_t parameter_version = 77U;
    size_t before;
    if (lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal, &parameter_version, 77U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
                            20U, 77U, 8U, UINT64_MAX, &arena, true) != LXP_OK)
        return 1;
    ctx.batch_number = 8U;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    fee_record(record, &first, 0x41U, 1U, 7U, 11U);
    put(&kernel, active_key, sizeof(active_key) - 1U, record, sizeof(record));
    put_history(&kernel, 1U, record);
    (void)memset(asset, 0x42, sizeof(asset));
    before = kernel.module_kv_count;
    if (lxp_programs_fee_governance_stage(&ctx, &proposed, asset,
            100U, 1U, 1U, 10U, 10U, 1000U, 9U, NULL) != LXP_ERR_AUTH_SCOPE ||
        kernel.module_kv_count != before)
        return 2;
    fee_pending(pending, &proposed, 0x42U, 9U, 8U, 12U);
    put(&kernel, pending_key, sizeof(pending_key) - 1U, pending, sizeof(pending));
    if (lxp_programs_fee_governance_activate(&ctx, 8U) != LXP_ERR_NOT_YET_VALID ||
        lxp_programs_fee_schedule_current(&ctx, &selected, asset) != LXP_OK ||
        selected.version != 1U || selected.cpu != first.cpu)
        return 3;
    ctx.batch_number = 10U;
    if (lxp_programs_fee_governance_activate(&ctx, 10U) != LXP_FATAL_REPLAY_DIVERGENCE)
        return 4;
    ctx.batch_number = 9U;
    if (lxp_programs_fee_governance_activate(&ctx, 9U) != LXP_OK ||
        lxp_programs_fee_schedule_current(&ctx, &selected, asset) != LXP_OK ||
        selected.version != 2U || selected.cpu != proposed.cpu || asset[0] != 0x42U ||
        lxp_programs_fee_schedule_at(&ctx, 1U, &selected, asset) != LXP_OK ||
        selected.version != 1U || selected.cpu != first.cpu || asset[0] != 0x41U ||
        lxp_programs_fee_governance_activate(&ctx, 9U) != LXP_ERR_UNKNOWN_FIELD)
        return 5;
    return 0;
}

static int occupancy_up(void) { return occupancy_vector((lxp_u128){0U, 200U}, 110U, 2U); }
static int occupancy_down(void) { return occupancy_vector((lxp_u128){0U, 0U}, 90U, 2U); }
static int occupancy_target(void) { return occupancy_vector((lxp_u128){0U, 100U}, 100U, 1U); }
static int occupancy_full_width(void) { return occupancy_vector((lxp_u128){UINT64_MAX, UINT64_MAX}, 110U, 2U); }

static const struct fee_case { const char *name; int (*run)(void); } cases[] = {
    {"native_pending_and_history", pending_and_history_vectors},
    {"native_occupancy_up", occupancy_up},
    {"native_occupancy_down", occupancy_down},
    {"native_occupancy_target", occupancy_target},
    {"native_occupancy_full_width", occupancy_full_width},
    {"native_activation_and_authority_refusals", activation_and_authority_refusals}
};

int main(int argc, char **argv)
{
    size_t index;
    if (argc == 2 && strcmp(argv[1], "--list-cases") == 0) {
        for (index = 0U; index < sizeof(cases) / sizeof(cases[0]); ++index)
            (void)puts(cases[index].name);
        return 0;
    }
    if (argc != 1 && (argc != 3 || strcmp(argv[1], "--case") != 0))
        return 2;
    for (index = 0U; index < sizeof(cases) / sizeof(cases[0]); ++index) {
        int status;
        if (argc == 3 && strcmp(argv[2], cases[index].name) != 0)
            continue;
        status = cases[index].run();
        if (status != 0) {
            (void)fprintf(stderr, "FAIL %s code=%d\n", cases[index].name, status);
            return 1;
        }
        (void)printf("PASS %s\n", cases[index].name);
        if (argc == 3) return 0;
    }
    return argc == 1 ? 0 : 2;
}
