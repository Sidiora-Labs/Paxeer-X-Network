#include "layerx/lxp_kernel.h"
#include "layerx/programs.h"

#include <openssl/evp.h>
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

static int sequencer_public_key(const uint8_t seed[32], uint8_t public_key[32])
{
    size_t length = 32U;
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                                 seed, 32U);
    int ok = key != NULL &&
             EVP_PKEY_get_raw_public_key(key, public_key, &length) == 1 &&
             length == 32U;
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static void proposal_body(uint8_t body[149],
                          const lx_programs_fee_schedule *schedule,
                          uint8_t asset_marker, uint64_t activation_batch)
{
    (void)memcpy(body, "LXFG1", 5U);
    prices(body + 5U, schedule);
    (void)memset(body + 61U, asset_marker, 32U);
    policy(body + 93U);
    write_u64(body + 141U, activation_batch);
}

static int activation_and_receipt_refusal_vectors(void)
{
    static uint8_t arena_bytes[16384];
    static lxp_receipt receipt;
    const lx_programs_fee_schedule first = {1U, 2U, 3U, 5U, 7U, 11U, 13U, 100U};
    const lx_programs_fee_schedule proposed = {0U, 17U, 19U, 23U, 29U, 31U, 37U, 105U};
    uint8_t first_record[FEE_RECORD_BYTES];
    uint8_t pending[FEE_PENDING_BYTES];
    uint8_t digest[32];
    uint8_t asset[32];
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lx_programs_fee_schedule selected;
    uint32_t parameter_version = 78U;
    uint64_t activation;
    if (lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal,
                          &parameter_version, 78U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) !=
            LXP_OK)
        return 1;
    fee_record(first_record, &first, 0x41U, 1U, 7U, 11U);
    fee_pending(pending, &proposed, 0x42U, 9U, 8U, 12U);
    put(&kernel, active_key, sizeof(active_key) - 1U,
        first_record, sizeof(first_record));
    put_history(&kernel, 1U, first_record);
    put(&kernel, pending_key, sizeof(pending_key) - 1U,
        pending, sizeof(pending));
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
                            20U, 78U, 30U, UINT64_MAX, &arena, true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    /* Pending state is visible and the active schedule is untouched before
     * the activation boundary. */
    ctx.batch_number = 8U;
    if (lxp_programs_fee_governance_pending(
            &ctx, &selected, &activation, digest) != LXP_OK ||
        activation != 9U || selected.version != 0U ||
        lxp_programs_fee_governance_activate(&ctx, 8U) !=
            LXP_ERR_NOT_YET_VALID ||
        lxp_programs_fee_schedule_current(&ctx, &selected, asset) != LXP_OK ||
        selected.version != 1U || selected.occupancy_byte_batch != 100U)
        return 2;
    ctx.batch_number = 10U;
    if (lxp_programs_fee_governance_activate(&ctx, 10U) !=
        LXP_FATAL_REPLAY_DIVERGENCE)
        return 3;
    ctx.batch_number = 9U;
    if (lxp_programs_fee_governance_activate(&ctx, 9U) != LXP_OK ||
        lxp_programs_fee_governance_pending(
            &ctx, &selected, &activation, digest) != LXP_ERR_UNKNOWN_FIELD ||
        lxp_programs_fee_schedule_current(&ctx, &selected, asset) != LXP_OK ||
        selected.version != 2U || selected.cpu != proposed.cpu ||
        selected.occupancy_byte_batch != 105U || asset[0] != 0x42U ||
        selected.memory_byte != proposed.memory_byte ||
        selected.storage_read_byte != proposed.storage_read_byte ||
        selected.storage_write_byte != proposed.storage_write_byte ||
        selected.output_value != proposed.output_value ||
        selected.output_byte != proposed.output_byte)
        return 4;
    /* History version 1 is not mutated retroactively. */
    if (lxp_programs_fee_schedule_at(&ctx, 1U, &selected, asset) != LXP_OK ||
        selected.version != 1U || selected.cpu != first.cpu ||
        selected.occupancy_byte_batch != 100U || asset[0] != 0x41U ||
        lxp_programs_fee_schedule_at(&ctx, 2U, &selected, asset) != LXP_OK ||
        selected.cpu != proposed.cpu ||
        lxp_programs_fee_schedule_at(&ctx, 3U, &selected, asset) !=
            LXP_ERR_VERSION_UNSUPPORTED)
        return 5;
    /* Governance receipts that are not verified successful Governance
     * receipts never stage a proposal. */
    (void)memset(&receipt, 0, sizeof(receipt));
    receipt.module_id = LXP_MODULE_PROGRAMS;
    receipt.result_code = LXP_OK;
    receipt.global_sequence = 13U;
    receipt.timestamp = 1U;
    (void)memset(receipt.resulting_state_root, 0x33, 32U);
    (void)memset(asset, 0x42, 32U);
    if (lxp_programs_fee_governance_stage(
            &ctx, &proposed, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
            &receipt) != LXP_ERR_AUTH_SCOPE)
        return 6;
    receipt.module_id = LXP_MODULE_GOVERNANCE;
    receipt.result_code = LXP_ERR_AUTH_SCOPE;
    if (lxp_programs_fee_governance_stage(
            &ctx, &proposed, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
            &receipt) != LXP_ERR_AUTH_SCOPE)
        return 7;
    receipt.result_code = LXP_OK;
    if (lxp_programs_fee_governance_stage(
            &ctx, &proposed, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
            &receipt) == LXP_OK ||
        lxp_programs_fee_governance_stage(
            &ctx, &proposed, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
            NULL) == LXP_OK ||
        lxp_programs_fee_governance_pending(
            &ctx, &selected, &activation, digest) != LXP_ERR_UNKNOWN_FIELD)
        return 8;
    if (lxp_programs_fee_governance_resolve_runtime(
            &kernel, 3U, &selected, asset) == LXP_OK)
        return 9;
    {
        static lxp_verified_receipt_index index;
        static const uint8_t seed[32] = {
            0x11U, 0x22U, 0x33U, 0x44U, 0x55U, 0x66U, 0x77U, 0x08U,
            0x19U, 0x2aU, 0x3bU, 0x4cU, 0x5dU, 0x6eU, 0x7fU, 0x10U,
            0x21U, 0x32U, 0x43U, 0x54U, 0x65U, 0x76U, 0x07U, 0x18U,
            0x29U, 0x3aU, 0x4bU, 0x5cU, 0x6dU, 0x7eU, 0x0fU, 0x20U};
        const lx_programs_fee_schedule third = {0U, 41U, 43U, 47U, 53U, 59U, 61U, 110U};
        uint8_t public_key[32];
        uint8_t receipt_digest[32];
        lxp_effect *effect;
        (void)memset(&receipt, 0, sizeof(receipt));
        receipt.module_id = LXP_MODULE_GOVERNANCE;
        receipt.result_code = LXP_OK;
        receipt.global_sequence = 13U;
        receipt.timestamp = 1U;
        (void)memset(receipt.resulting_state_root, 0x33, 32U);
        receipt.effects.count = 1U;
        effect = &receipt.effects.effects[0];
        effect->module_id = LXP_MODULE_GOVERNANCE;
        effect->kind = LXP_EFFECT_STATE;
        effect->monetary = false;
        effect->body_length = 149U;
        proposal_body(effect->body, &third, 0x43U, 12U);
        (void)memset(asset, 0x43, 32U);
        /* Missing proof: a well-formed receipt with no verified facts. */
        ctx.verified_receipts = NULL;
        if (lxp_programs_fee_governance_stage(
                &ctx, &third, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
                &receipt) != LXP_ERR_UNKNOWN_FIELD)
            return 10;
        if (sequencer_public_key(seed, public_key) != 0 ||
            lxp_verified_receipt_index_init(&index) != LXP_OK ||
            lxp_receipt_sign(&receipt, seed, &arena) != LXP_OK ||
            lxp_verified_receipt_index_add(&index, &receipt, public_key,
                                           &arena) != LXP_OK ||
            lxp_receipt_digest(&receipt, &arena, receipt_digest) != LXP_OK)
            return 11;
        ctx.verified_receipts = &index;
        /* Wrong proposal binding: the verified receipt commits activation
         * batch 12, not 13. */
        if (lxp_programs_fee_governance_stage(
                &ctx, &third, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 13U,
                &receipt) != LXP_ERR_AUTH_SCOPE ||
            lxp_programs_fee_governance_pending(
                &ctx, &selected, &activation, digest) != LXP_ERR_UNKNOWN_FIELD)
            return 12;
        if (lxp_programs_fee_governance_stage(
                &ctx, &third, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 12U,
                &receipt) != LXP_OK ||
            lxp_programs_fee_governance_pending(
                &ctx, &selected, &activation, digest) != LXP_OK ||
            activation != 12U || selected.version != 0U ||
            selected.cpu != third.cpu ||
            memcmp(digest, receipt_digest, 32U) != 0 ||
            lxp_programs_fee_schedule_current(&ctx, &selected, asset) !=
                LXP_OK || selected.version != 2U ||
            selected.cpu != proposed.cpu)
            return 13;
        ctx.batch_number = 12U;
        (void)memset(asset, 0x43, 32U);
        if (lxp_programs_fee_governance_activate(&ctx, 12U) != LXP_OK ||
            lxp_programs_fee_schedule_current(&ctx, &selected, asset) !=
                LXP_OK || selected.version != 3U ||
            selected.cpu != third.cpu || asset[0] != 0x43U)
            return 14;
        /* Reused receipt: the same verified Governance receipt cannot stage
         * a second schedule change. */
        (void)memset(asset, 0x43, 32U);
        if (lxp_programs_fee_governance_stage(
                &ctx, &third, asset, 100U, 1U, 1U, 10U, 10U, 1000U, 14U,
                &receipt) == LXP_OK ||
            lxp_programs_fee_governance_pending(
                &ctx, &selected, &activation, digest) != LXP_ERR_UNKNOWN_FIELD)
            return 15;
        /* Multi-schedule history: every recorded version still resolves to
         * the prices it was charged under. */
        if (lxp_programs_fee_schedule_at(&ctx, 1U, &selected, asset) !=
                LXP_OK || selected.cpu != first.cpu ||
            lxp_programs_fee_schedule_at(&ctx, 2U, &selected, asset) !=
                LXP_OK || selected.cpu != proposed.cpu ||
            lxp_programs_fee_schedule_at(&ctx, 3U, &selected, asset) !=
                LXP_OK || selected.cpu != third.cpu ||
            lxp_programs_fee_schedule_at(&ctx, 4U, &selected, asset) !=
                LXP_ERR_VERSION_UNSUPPORTED)
            return 16;
    }
    return 0;
}

static int occupancy_case(uint32_t initial_version, uint64_t initial_price,
                          uint64_t observed_hi, uint64_t observed,
                          lxp_result expected_status,
                          uint64_t expected, uint32_t expected_version)
{
    static uint8_t arena_bytes[16384];
    const lx_programs_fee_schedule initial = {
        initial_version, 2U, 3U, 5U, 7U, 11U, 13U, initial_price};
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
    put_history(&kernel, initial_version, record);
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
                            10U, 41U, 2U, UINT64_MAX, &arena, true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    ctx.batch_number = 2U;
    (void)memset(&receipt, 0, sizeof(receipt));
    receipt.batch_number = 2U;
    receipt.parameter_version = parameter_version;
    receipt.schedule_version = initial_version;
    receipt.schedule_prices[0] = initial.cpu;
    receipt.schedule_prices[1] = initial.memory_byte;
    receipt.schedule_prices[2] = initial.storage_read_byte;
    receipt.schedule_prices[3] = initial.storage_write_byte;
    receipt.schedule_prices[4] = initial.output_value;
    receipt.schedule_prices[5] = initial.output_byte;
    receipt.schedule_prices[6] = initial.occupancy_byte_batch;
    (void)memset(receipt.occupancy_asset_id, 0x51, 32U);
    receipt.byte_batches = (lxp_u128){observed_hi, observed};
    if (expected_status != LXP_OK) {
        if (lxp_programs_fee_governance_observe_batch(&ctx, &receipt) !=
                expected_status ||
            lxp_programs_fee_schedule_current(&ctx, &current, asset) !=
                LXP_OK ||
            current.version != initial_version ||
            current.occupancy_byte_batch != initial_price)
            return 2;
        return 0;
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
    return 0;
}

static int occupancy_vector(uint64_t observed, uint64_t expected,
                            uint32_t expected_version)
{
    return occupancy_case(1U, 100U, 0U, observed, LXP_OK, expected,
                          expected_version);
}

static int case_count;
static int case_failures;

static void report(const char *name, int passed)
{
    ++case_count;
    if (!passed) ++case_failures;
    (void)printf("FEE_CASE {\"name\":\"%s\",\"result\":\"%s\"}\n", name,
                 passed ? "pass" : "fail");
}

static int reached(int status, int last_code)
{
    return status == 0 || status > last_code;
}

int main(void)
{
    int status = pending_and_history_vectors();
    int high = occupancy_vector(200U, 110U, 2U);
    int zero = occupancy_vector(0U, 90U, 2U);
    int target = occupancy_vector(100U, 100U, 1U);
    report("seeded-pending-history", status == 0);
    report("occupancy-100-110-90-100", high == 0 && zero == 0 && target == 0);
    report("occupancy-high", high == 0);
    report("occupancy-zero", zero == 0);
    report("occupancy-target", target == 0);
    report("occupancy-bounded-up", occupancy_vector(105U, 105U, 2U) == 0);
    report("occupancy-bounded-down", occupancy_vector(95U, 95U, 2U) == 0);
    report("occupancy-full",
           occupancy_case(1U, 100U, UINT64_MAX, UINT64_MAX, LXP_OK,
                          110U, 2U) == 0);
    report("occupancy-cap-floor",
           occupancy_case(1U, 1000U, 0U, 200U, LXP_OK, 1000U, 1U) == 0 &&
           occupancy_case(1U, 10U, 0U, 0U, LXP_OK, 10U, 1U) == 0 &&
           occupancy_case(1U, 995U, 0U, 200U, LXP_OK, 1000U, 2U) == 0);
    report("occupancy-overflow-refused",
           occupancy_case(UINT32_MAX, 100U, 0U, 200U, LXP_ERR_OVERFLOW,
                          100U, UINT32_MAX) == 0);
    status = activation_and_receipt_refusal_vectors();
    (void)printf("FEE_STATUS activation_and_receipt_refusal_vectors step=%d\n", status);
    report("pending-visible-before-activation", reached(status, 2));
    report("schedule-state-named-coefficients", reached(status, 4));
    report("activation-exact-boundary", reached(status, 4));
    report("no-retroactive-mutation", reached(status, 5));
    report("history-replay-recorded-version", reached(status, 5));
    report("governance-refuses-wrong-module", reached(status, 6));
    report("governance-refuses-unsuccessful-receipt", reached(status, 7));
    report("history-refuses-unknown-version", reached(status, 9));
    report("governance-refuses-missing-proof", reached(status, 10));
    report("governance-refuses-wrong-proposal", reached(status, 12));
    report("governance-accepts-verified-receipt", reached(status, 14));
    report("governance-refuses-reused-receipt", reached(status, 15));
    report("multi-schedule-replay-preserves-charges", status == 0);
    (void)printf("FEE_GOVERNANCE cases=%d skipped=0\n", case_count);
    return case_failures == 0 ? 0 : 1;
}
