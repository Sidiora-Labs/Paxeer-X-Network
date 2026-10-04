#include "layerx/programs.h"

#include "artifact.h"
#include "event.h"
#include "storage.h"
#include "occupancy.h"

#include "layerx/lx_oracle.h"
#include "layerx/lx_web.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_merkle.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_receipt.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

enum {
    PROGRAM_CALL_FIXED_BYTES = 32 + 2 + 2 + 4 + 2 + 4 + 4 +
                               LX_PROGRAMS_CALL_BUDGET_FIELDS * 8,
    PROGRAM_RECORD_BYTES = 71,
    PROGRAM_KEY_BYTES = 40,
    ARBITER_AUTHORITY_BYTES = 247
};

enum {
    WEB_FEE_DOMAIN_BYTES = 19,
    WEB_PENDING_PREFIX_BYTES = 11,
    WEB_PENDING_KEY_BYTES = WEB_PENDING_PREFIX_BYTES + 32 + 8,
    WEB_PENDING_RECORD_VERSION = 1,
    WEB_PENDING_RECORD_BYTES = 123
};

enum {
    PROGRAM_TRANSFER_SOURCE_PRINCIPAL = 1,
    PROGRAM_TRANSFER_SOURCE_PROGRAM = 2,
    PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING = 3
};

typedef struct lxp_programs_call_transfer_source {
    uint8_t kind;
    uint8_t owner_program[32];
    uint8_t staging_program[32];
    uint8_t frame_path[8];
    uint8_t frame_depth;
    uint8_t *seed;
    uint16_t seed_length;
    uint16_t seed_written;
} lxp_programs_call_transfer_source;

typedef struct lxp_programs_call_catalog_entry {
    uint8_t program_id[32];
    uint8_t owner[32];
    uint8_t code_hash[32];
    uint32_t wasm_length;
    uint16_t abi_version;
    struct {
        lxp_programs_storage_cell *cells;
        uint32_t count;
        bool begun;
        bool applied;
    } storage_final[2];
} lxp_programs_call_catalog_entry;

struct lxp_programs_call_activity {
    lxp_module_ctx *ctx;
    uint32_t network_id;
    uint32_t replay_prefix_length;
    lxp_programs_replay_capture replay;
    uint8_t *replay_runtime, *replay_authority, *replay_hosts;
    uint32_t replay_runtime_length, replay_runtime_written, replay_authority_length, replay_hosts_length;
    bool replay_finished;
    uint8_t program_id[32];
    uint8_t code_hash[32];
    uint32_t wasm_length;
    uint16_t abi_version;
    uint16_t entrypoint_length;
    const uint8_t *entrypoint;
    uint32_t calldata_length;
    const uint8_t *calldata;
    uint16_t capabilities_length;
    const uint8_t *capabilities;
    uint32_t access_declaration_length;
    const uint8_t *access_declaration;
    uint32_t response_capacity;
    uint64_t budget[LX_PROGRAMS_CALL_BUDGET_FIELDS];
    const lxp_authority_resolved *authority;
    lxp_effect_buffer *effects;
    lxp_transfer_set *transfer_set;
    lxp_programs_call_transfer_source *transfer_sources;
    lxp_transfer_source_authority *transfer_source_authorities;
    uint64_t transfer_program_spend_token;
    lxp_receipt transfer_receipt;
    lxp_programs_occupancy_bridge *occupancy;
    uint8_t transfer_leg_written[LXP_MAX_TRANSFER_SET_LEGS];
    uint16_t transfer_leg_count;
    bool transfer_applied;
    bool storage_settlement_authorized;
    lxp_programs_call_catalog_entry *catalog;
    uint32_t catalog_count;
    uint32_t catalog_cursor;
    bool receipt_view_active;
    lxp_verified_receipt_facts receipt_view;
    bool arbiter_authority_active;
    uint8_t arbiter_authority[ARBITER_AUTHORITY_BYTES];
    struct {
        bool active;
        uint8_t account[32];
        uint8_t asset[32];
        lxp_u128 balance;
        uint8_t receipt_digest[32];
        uint8_t state_root[32];
        uint64_t observed_sequence;
    } balance_view;
    struct {
        bool active;
        uint8_t market_id[32];
        uint8_t record[LX_ORACLE_COMMITTED_BYTES];
    } oracle_view;
    struct {
        bool active;
        uint8_t program_id[32];
        uint8_t header[LX_WEB_ANSWER_HEADER_BYTES];
        uint32_t response_length;
        uint8_t response[LX_WEB_MAX_RESPONSE_BYTES];
    } web_view;
    struct {
        lxp_programs_storage_cell *cells;
        uint32_t count;
        bool begun;
        bool applied;
    } storage_final[2];
    struct {
        bool active;
        uint8_t terminal_kind;
        lxp_result result_code;
        uint16_t runtime_version;
        uint16_t abi_version;
        uint32_t fee_schedule_version;
        uint32_t metering_schedule_version;
        uint64_t cpu_fuel;
        uint64_t memory_bytes;
        uint64_t storage_read_bytes;
        uint64_t storage_write_bytes;
        uint32_t output_values;
        uint64_t output_bytes;
        lxp_u128 fee_units;
        uint8_t transfer_root[32];
        uint8_t applied_legs[LXP_MAX_TRANSFER_SET_LEGS * 115U];
        uint32_t applied_length;
        uint32_t applied_written;
        uint8_t *graph;
        uint32_t graph_length;
        uint8_t *terminal;
        uint32_t terminal_length;
        uint8_t *events;
        uint32_t events_length;
        uint32_t written[3];
        uint32_t graph_capacity;
        uint32_t terminal_capacity;
        uint32_t events_capacity;
        bool reserved;
    } terminal;
    struct {
        bool active;
        uint8_t program_id[32];
        uint8_t principal[32];
        uint8_t frame_path[8];
        uint8_t frame_depth;
        uint32_t written[2];
        uint32_t event_index;
        uint8_t *topic;
        uint16_t topic_length;
        uint8_t *data;
        uint32_t data_length;
    } event;
    uint32_t emitted_event_count;
    struct {
        bool active;
        uint8_t program_id[32];
        uint64_t request_id;
        uint8_t kind;
        uint8_t payload_hash[32];
        uint8_t fee_asset[32];
        uint8_t fee_account[32];
        lxp_u128 fee_amount;
    } web_request;
};

static lxp_result replay_host_fact(lxp_programs_call_activity *value, uint8_t tag,
    const uint8_t *first, size_t first_length, const uint8_t *second, size_t second_length);
static lxp_result replay_host_failure(lxp_programs_call_activity *value, uint8_t tag,
    lxp_result checked_status, const uint8_t *input, size_t input_length);
static lxp_result replay_capture_authority(lxp_programs_call_activity *value,
    const lxp_activity *activity, const lxp_authority_resolved *authority);

static void call_activity_release(void *state)
{
    lxp_programs_call_activity *value = state;
    if (value == NULL) return;
    free(value->replay.bytes);
    free(value->replay_runtime);
    free(value->replay_authority);
    free(value->replay_hosts);
    value->replay.bytes = NULL;
    value->replay_runtime = NULL;
    value->replay_authority = NULL;
    value->replay_hosts = NULL;
    free(value->terminal.graph);
    free(value->terminal.terminal);
    free(value->terminal.events);
    value->terminal.graph = NULL;
    value->terminal.terminal = NULL;
    value->terminal.events = NULL;
}

static lxp_result call_namespace_for_program(const lxp_programs_call_activity *value,
                                             const uint8_t program_id[32],
                                 uint16_t selector, uint8_t bytes[65],
                                 uint16_t *length)
{
    const lxp_call_admission_facts *admission;
    if (value == NULL || length == NULL || selector > 1U)
        return LXP_ERR_NON_CANONICAL;
    admission = lxp_ctx_call_admission(value->ctx);
    if (admission == NULL) return LXP_FATAL_INVARIANT;
    (void)memcpy(bytes, program_id, 32U);
    bytes[32] = (uint8_t)(selector == 0U ? 0U : 1U);
    if (selector == 0U) {
        (void)memcpy(bytes + 33U, value->authority->principal, 32U);
        *length = 65U;
    } else *length = 33U;
    return LXP_OK;
}

static lxp_result call_namespace(const lxp_programs_call_activity *value,
                                 uint16_t selector, uint8_t bytes[65],
                                 uint16_t *length)
{
    return call_namespace_for_program(value, value->program_id, selector,
                                      bytes, length);
}

static lxp_result storage_cell(const lxp_programs_call_activity *value,
                               uint16_t selector, uint32_t index,
                               const uint8_t **key, uint16_t *key_length,
                               const uint8_t **cell_value,
                               uint32_t *value_length, uint32_t *count)
{
    uint8_t ns[65]; uint16_t ns_length;
    lxp_result status = call_namespace(value, selector, ns, &ns_length);
    if (status != LXP_OK) return status;
    return lxp_programs_storage_cell_at(value->ctx, ns, ns_length, index,
        key, key_length, cell_value, value_length, count);
}

lxp_result layerx_programs_call_storage_cell_count(uint64_t token,
                                                    uint16_t selector)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value; uint16_t key_length;
    uint32_t value_length, count = 0U;
    lxp_result status = storage_cell(value, selector, 0U, &key, &key_length,
                                     &cell_value, &value_length, &count);
    if (status == LXP_ERR_UNKNOWN_FIELD && count == 0U) return 0;
    return status == LXP_OK ? (lxp_result)count : status;
}

lxp_result layerx_programs_call_storage_cell_length(
    uint64_t token, uint16_t selector, uint32_t index, uint16_t section)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value; uint16_t key_length;
    uint32_t value_length, count;
    lxp_result status = storage_cell(value, selector, index, &key, &key_length,
                                     &cell_value, &value_length, &count);
    if (status != LXP_OK) return status;
    if (section == 0U) return (lxp_result)key_length;
    if (section == 1U && value_length <= INT32_MAX) return (lxp_result)value_length;
    return section == 1U ? LXP_ERR_LENGTH_LIMIT : LXP_ERR_UNKNOWN_FIELD;
}

lxp_result layerx_programs_call_storage_cell_byte(
    uint64_t token, uint16_t selector, uint32_t index, uint16_t section,
    uint32_t offset)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value, *bytes; uint16_t key_length;
    uint32_t value_length, count, length;
    lxp_result status = storage_cell(value, selector, index, &key, &key_length,
                                     &cell_value, &value_length, &count);
    if (status != LXP_OK) return status;
    if (section == 0U) { bytes = key; length = key_length; }
    else if (section == 1U) { bytes = cell_value; length = value_length; }
    else return LXP_ERR_UNKNOWN_FIELD;
    return offset < length ? (lxp_result)bytes[offset] : LXP_ERR_TRUNCATED;
}

lxp_result layerx_programs_call_storage_final_begin(
    uint64_t token, uint16_t selector, uint32_t count)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    void *allocation; lxp_result status;
    if (value == NULL || value->ctx == NULL || selector > 1U ||
        value->storage_final[selector].begun) return LXP_ERR_NON_CANONICAL;
    if (count > INT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    if (count == 0U) { value->storage_final[selector].begun = true; return LXP_OK; }
    if (sizeof(lxp_programs_storage_cell) > SIZE_MAX / count) return LXP_ERR_LENGTH_LIMIT;
    status = lxp_ctx_arena_alloc(value->ctx,
        (size_t)count * sizeof(lxp_programs_storage_cell),
        _Alignof(lxp_programs_storage_cell), &allocation);
    if (status != LXP_OK) return status;
    value->storage_final[selector].begun = true;
    value->storage_final[selector].cells = allocation;
    value->storage_final[selector].count = count;
    (void)memset(allocation, 0, (size_t)count * sizeof(lxp_programs_storage_cell));
    return LXP_OK;
}

lxp_result layerx_programs_call_storage_final_cell(
    uint64_t token, uint16_t selector, uint32_t index,
    uint16_t key_length, uint32_t value_length)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_storage_cell *cell; void *key, *cell_value; lxp_result status;
    if (value == NULL || selector > 1U || index >= value->storage_final[selector].count ||
        key_length == 0U || key_length > LX_PROGRAMS_STORAGE_MAX_KEY_BYTES ||
        value_length > LX_PROGRAMS_STORAGE_MAX_VALUE_BYTES) return LXP_ERR_NON_CANONICAL;
    cell = &value->storage_final[selector].cells[index];
    if (cell->key != NULL) return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(value->ctx, key_length, 1U, &key);
    if (status != LXP_OK) return status;
    status = lxp_ctx_arena_alloc(value->ctx, value_length == 0U ? 1U : value_length,
                                 1U, &cell_value);
    if (status != LXP_OK) return status;
    cell->key = key; cell->key_length = key_length;
    cell->value = cell_value; cell->value_length = value_length;
    return LXP_OK;
}

lxp_result layerx_programs_call_storage_final_byte(
    uint64_t token, uint16_t selector, uint32_t index, uint16_t section,
    uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_storage_cell *cell; uint8_t *bytes; uint32_t length;
    if (value == NULL || selector > 1U || index >= value->storage_final[selector].count)
        return LXP_ERR_NON_CANONICAL;
    cell = &value->storage_final[selector].cells[index];
    if (section == 0U) { bytes = (uint8_t *)cell->key; length = cell->key_length; }
    else if (section == 1U) { bytes = (uint8_t *)cell->value; length = cell->value_length; }
    else return LXP_ERR_UNKNOWN_FIELD;
    if (bytes == NULL || offset >= length) return LXP_ERR_TRUNCATED;
    bytes[offset] = byte; return LXP_OK;
}

lxp_result layerx_programs_call_storage_final_apply(uint64_t token,
                                                     uint16_t selector)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t ns[65]; uint16_t ns_length; lxp_result status;
    if (value == NULL || selector > 1U || !value->storage_settlement_authorized ||
        !value->storage_final[selector].begun)
        return LXP_ERR_NON_CANONICAL;
    if (value->storage_final[selector].applied) return LXP_ERR_NON_CANONICAL;
    status = call_namespace(value, selector, ns, &ns_length);
    if (status != LXP_OK) return status;
    status = lxp_programs_storage_stage_final(value->ctx, ns, ns_length,
        value->storage_final[selector].cells,
        value->storage_final[selector].count);
    if (status == LXP_OK) value->storage_final[selector].applied = true;
    return status;
}

static uint16_t read_u16(const uint8_t *bytes)
{
    return (uint16_t)(((uint16_t)bytes[0] << 8U) | bytes[1]);
}

static uint32_t read_u32(const uint8_t *bytes)
{
    return ((uint32_t)bytes[0] << 24U) | ((uint32_t)bytes[1] << 16U) |
           ((uint32_t)bytes[2] << 8U) | bytes[3];
}

static uint64_t read_u64(const uint8_t *bytes)
{
    uint64_t value = 0U;
    size_t index;
    for (index = 0U; index < 8U; ++index)
        value = (value << 8U) | bytes[index];
    return value;
}

static bool checked_size_add(size_t left, size_t right, size_t *sum)
{
    if (sum == NULL || right > SIZE_MAX - left) return false;
    *sum = left + right;
    return true;
}

static void write_u64(uint8_t *bytes, uint64_t value)
{
    size_t index;
    for (index = 0U; index < 8U; ++index)
        bytes[index] = (uint8_t)(value >> ((7U - index) * 8U));
}

/* The account every program web request is paid into, one per asset: the
 * Programs module value account whose id is sha256 of the fee domain and the
 * asset id. Intake pays the attestors out of it. */
static lxp_result web_fee_account_id(const uint8_t asset[32], uint8_t id[32])
{
    static const uint8_t domain[WEB_FEE_DOMAIN_BYTES] = {
        'P', 'A', 'X', 'E', 'E', 'R', 'X', '_', 'W', 'E', 'B', '_',
        'F', 'E', 'E', 'S', '_', 'V', '1'
    };
    uint8_t preimage[WEB_FEE_DOMAIN_BYTES + 32];
    (void)memcpy(preimage, domain, sizeof(domain));
    (void)memcpy(preimage + sizeof(domain), asset, 32U);
    return lxp_hash_sha256(preimage, sizeof(preimage), id);
}

static void web_pending_key(const uint8_t program_id[32], uint64_t request_id,
                            uint8_t key[WEB_PENDING_KEY_BYTES])
{
    static const uint8_t prefix[WEB_PENDING_PREFIX_BYTES] = {
        'w', 'e', 'b', '/', 'p', 'e', 'n', 'd', 'i', 'n', 'g'
    };
    (void)memcpy(key, prefix, sizeof(prefix));
    (void)memcpy(key + sizeof(prefix), program_id, 32U);
    write_u64(key + sizeof(prefix) + 32U, request_id);
}

static lx_account *account_by_id(lx_account_registry *accounts,
                                 const uint8_t id[32])
{
    size_t index;
    if (accounts == NULL) return NULL;
    for (index = 0U; index < accounts->count; ++index)
        if (lxp_ct_memcmp(accounts->accounts[index].id, id, 32U) == 0)
            return &accounts->accounts[index];
    return NULL;
}

static void program_key(const uint8_t program_id[32], uint8_t key[PROGRAM_KEY_BYTES])
{
    static const uint8_t prefix[8] = {'p', 'r', 'o', 'g', 'r', 'a', 'm', 0};
    (void)memcpy(key, prefix, sizeof(prefix));
    (void)memcpy(key + sizeof(prefix), program_id, 32U);
}

static lxp_result catalog_count_visit(const uint8_t *key, size_t key_length,
                                      const uint8_t *record, size_t record_length,
                                      void *user)
{
    lxp_programs_call_activity *value = user;
    lxp_result lifecycle;
    if (value == NULL || key == NULL || record == NULL ||
        key_length != PROGRAM_KEY_BYTES || record_length != PROGRAM_RECORD_BYTES ||
        memcmp(key, "program\0", 8U) != 0 || lxp_ct_is_zero(key + 8U, 32U) ||
        read_u16(record + 65U) == 0U ||
        read_u16(record + 65U) > LX_PROGRAMS_GUEST_ABI_V4_VERSION ||
        (read_u16(record + 65U) >= LX_PROGRAMS_GUEST_ABI_V2_VERSION &&
         !lxp_protocol_version_uses_occupancy(value->ctx->protocol_version)) ||
        lxp_ct_is_zero(record + 33U, 32U) || value->catalog_count == UINT32_MAX)
        return LXP_FATAL_INVARIANT;
    lifecycle = lxp_programs_program_active(value->ctx, key + 8U);
    if (lifecycle == LXP_ERR_PROGRAM_REFUSED) return LXP_OK;
    if (lifecycle != LXP_OK) return lifecycle;
    ++value->catalog_count;
    return LXP_OK;
}

static lxp_result catalog_fill_visit(const uint8_t *key, size_t key_length,
                                     const uint8_t *record, size_t record_length,
                                     void *user)
{
    lxp_programs_call_activity *value = user;
    lxp_programs_call_catalog_entry *entry;
    const uint8_t *wasm;
    size_t wasm_length;
    lxp_result status;
    if (value == NULL || key == NULL || record == NULL ||
        key_length != PROGRAM_KEY_BYTES || record_length != PROGRAM_RECORD_BYTES ||
        value->catalog == NULL || value->catalog_count == 0U)
        return LXP_FATAL_INVARIANT;
    status = lxp_programs_program_active(value->ctx, key + 8U);
    if (status == LXP_ERR_PROGRAM_REFUSED) return LXP_OK;
    if (status != LXP_OK) return status;
    if (value->catalog_cursor >= value->catalog_count)
        return LXP_FATAL_INVARIANT;
    entry = &value->catalog[value->catalog_cursor];
    (void)memcpy(entry->program_id, key + 8U, 32U);
    if (lxp_protocol_version_uses_occupancy(value->ctx->protocol_version)) {
        status = lxp_programs_account_owner_read(
            value->ctx, entry->program_id, entry->owner);
        if (status != LXP_OK) return status;
    } else {
        (void)memcpy(entry->owner, record + 1U, 32U);
    }
    (void)memcpy(entry->code_hash, record + 33U, 32U);
    entry->abi_version = read_u16(record + 65U);
    status = lxp_programs_artifact_open(value->ctx, entry->program_id,
                                        entry->code_hash, &wasm, &wasm_length);
    if (status != LXP_OK) return status;
    if (wasm_length == 0U || wasm_length > UINT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    entry->wasm_length = (uint32_t)wasm_length;
    ++value->catalog_cursor;
    return LXP_OK;
}

static lxp_result call_catalog_build(lxp_programs_call_activity *value)
{
    static const uint8_t prefix[8] = {'p','r','o','g','r','a','m',0};
    uint32_t count;
    void *allocation;
    lxp_result status;
    if (value == NULL || value->ctx == NULL || value->catalog != NULL ||
        value->catalog_count != 0U) return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_kv_iter(value->ctx, prefix, sizeof(prefix),
                             catalog_count_visit, value);
    if (status != LXP_OK) return status;
    count = value->catalog_count;
    if (count == 0U || count > INT32_MAX ||
        sizeof(*value->catalog) > SIZE_MAX / count)
        return LXP_ERR_LENGTH_LIMIT;
    status = lxp_ctx_arena_alloc(value->ctx,
                                 (size_t)count * sizeof(*value->catalog),
                                 _Alignof(lxp_programs_call_catalog_entry),
                                 &allocation);
    if (status != LXP_OK) return status;
    value->catalog = allocation;
    (void)memset(value->catalog, 0, (size_t)count * sizeof(*value->catalog));
    status = lxp_ctx_kv_iter(value->ctx, prefix, sizeof(prefix),
                             catalog_fill_visit, value);
    if (status != LXP_OK || value->catalog_cursor != count)
        return LXP_FATAL_INVARIANT;
    return LXP_OK;
}

static lxp_programs_call_catalog_entry *catalog_entry(
    lxp_programs_call_activity *value, uint32_t index)
{
    if (value == NULL || value->catalog == NULL || index >= value->catalog_count)
        return NULL;
    return &value->catalog[index];
}

lxp_result layerx_programs_call_catalog_count(uint64_t token)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->catalog == NULL || value->catalog_count == 0U ||
        value->catalog_count > INT32_MAX) return LXP_ERR_NON_CANONICAL;
    return (lxp_result)value->catalog_count;
}

static lxp_result catalog_interface(lxp_programs_call_activity *value,
                                     uint32_t index, const uint8_t **encoding,
                                     size_t *length)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(value, index);
    uint8_t key[42] = "interface";
    uint8_t digest[32];
    const uint8_t *stored;
    size_t stored_length;
    lxp_result status;
    if (entry == NULL || encoding == NULL || length == NULL)
        return LXP_ERR_NON_CANONICAL;
    (void)memcpy(key + 10U, entry->program_id, 32U);
    status = lxp_ctx_kv_get(value->ctx, key, sizeof(key), &stored, &stored_length);
    if (status == LXP_ERR_UNKNOWN_FIELD) {
        *encoding = NULL;
        *length = 0U;
        return LXP_OK;
    }
    if (status != LXP_OK) return status;
    if (stored_length <= 72U || stored_length > 1024U ||
        lxp_ct_memcmp(stored, entry->program_id, 32U) != 0 ||
        read_u32(stored + 32U) == 0U ||
        read_u32(stored + 68U) != stored_length - 72U)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_hash_sha256(stored + 72U, stored_length - 72U, digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, stored + 36U, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    *encoding = stored + 72U;
    *length = stored_length - 72U;
    return LXP_OK;
}

lxp_result layerx_programs_call_catalog_interface_length(uint64_t token, uint32_t index)
{
    const uint8_t *encoding;
    size_t length;
    lxp_result status = catalog_interface((lxp_programs_call_activity *)(uintptr_t)token,
                                         index, &encoding, &length);
    return status == LXP_OK ? (lxp_result)length : status;
}

lxp_result layerx_programs_call_catalog_interface_byte(uint64_t token, uint32_t index,
                                                       uint32_t offset)
{
    const uint8_t *encoding;
    size_t length;
    lxp_result status = catalog_interface((lxp_programs_call_activity *)(uintptr_t)token,
                                         index, &encoding, &length);
    if (status != LXP_OK) return status;
    return offset < length ? (lxp_result)encoding[offset] : LXP_ERR_TRUNCATED;
}

lxp_result layerx_programs_call_catalog_wasm_length(uint64_t token,
                                                     uint32_t index)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(
        (lxp_programs_call_activity *)(uintptr_t)token, index);
    if (entry == NULL || entry->wasm_length == 0U ||
        entry->wasm_length > INT32_MAX) return LXP_ERR_NON_CANONICAL;
    return (lxp_result)entry->wasm_length;
}

lxp_result layerx_programs_call_catalog_abi_version(uint64_t token,
                                                     uint32_t index)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(
        (lxp_programs_call_activity *)(uintptr_t)token, index);
    return entry == NULL || entry->abi_version == 0U ?
           LXP_ERR_NON_CANONICAL : (lxp_result)entry->abi_version;
}

lxp_result layerx_programs_call_catalog_identity_byte(
    uint64_t token, uint32_t index, uint16_t section, uint32_t offset)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(
        (lxp_programs_call_activity *)(uintptr_t)token, index);
    const uint8_t *bytes;
    if (entry == NULL || offset >= 32U) return LXP_ERR_NON_CANONICAL;
    if (section == 0U) bytes = entry->program_id;
    else if (section == 1U) bytes = entry->code_hash;
    else if (section == 2U) bytes = entry->owner;
    else return LXP_ERR_UNKNOWN_FIELD;
    return (lxp_result)bytes[offset];
}

lxp_result layerx_programs_call_catalog_wasm_byte(uint64_t token,
                                                   uint32_t index,
                                                   uint32_t offset)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_call_catalog_entry *entry = catalog_entry(value, index);
    const uint8_t *wasm;
    size_t wasm_length;
    lxp_result status;
    if (entry == NULL) return LXP_ERR_NON_CANONICAL;
    status = lxp_programs_artifact_open(value->ctx, entry->program_id,
                                        entry->code_hash, &wasm, &wasm_length);
    if (status != LXP_OK) return status;
    if (wasm_length != entry->wasm_length || offset >= wasm_length)
        return LXP_ERR_TRUNCATED;
    return (lxp_result)wasm[offset];
}

lxp_result layerx_programs_call_receipt_view_begin(
    uint64_t token, uint64_t d0, uint64_t d1, uint64_t d2, uint64_t d3)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t digest[32];
    lxp_result status;
    if (value == NULL || value->ctx == NULL) return LXP_ERR_NON_CANONICAL;
    write_u64(digest, d0);
    write_u64(digest + 8U, d1);
    write_u64(digest + 16U, d2);
    write_u64(digest + 24U, d3);
    status = lxp_ctx_verified_receipt_facts(value->ctx, digest,
                                             &value->receipt_view);
    if (status != LXP_OK) {
        value->receipt_view_active = false;
        (void)memset(&value->receipt_view, 0, sizeof(value->receipt_view));
        return replay_host_failure(value, 1U, status, digest, sizeof(digest));
    }
    if (lxp_ct_memcmp(value->receipt_view.receipt_digest, digest, 32U) != 0)
        return replay_host_failure(value, 1U, LXP_FATAL_INVARIANT, digest, sizeof(digest));
    value->receipt_view_active = true;
    if (value->replay.max_bytes != 0U) {
        uint8_t facts[116];
        (void)memcpy(facts, value->receipt_view.receipt_digest, 32U);
        facts[32] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 24U);
        facts[33] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 16U);
        facts[34] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 8U);
        facts[35] = (uint8_t)(uint32_t)value->receipt_view.result_code;
        (void)memcpy(facts + 36U, value->receipt_view.asset, 32U);
        (void)lxp_u128_to_be(value->receipt_view.amount, facts + 68U);
        (void)memcpy(facts + 84U, value->receipt_view.resulting_state_root, 32U);
        return replay_host_fact(value, 1U, facts, sizeof(facts), NULL, 0U);
    }
    return LXP_OK;
}

lxp_result layerx_programs_call_receipt_view_byte(
    uint64_t token, uint16_t section, uint32_t offset)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t result[4];
    uint8_t amount[16];
    const uint8_t *bytes;
    size_t length;
    if (value == NULL || !value->receipt_view_active)
        return LXP_ERR_UNKNOWN_FIELD;
    if (section == 0U) {
        bytes = value->receipt_view.receipt_digest;
        length = 32U;
    } else if (section == 1U) {
        result[0] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 24U);
        result[1] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 16U);
        result[2] = (uint8_t)((uint32_t)value->receipt_view.result_code >> 8U);
        result[3] = (uint8_t)(uint32_t)value->receipt_view.result_code;
        bytes = result;
        length = sizeof(result);
    } else if (section == 2U) {
        bytes = value->receipt_view.asset;
        length = 32U;
    } else if (section == 3U) {
        lxp_u128_to_be(value->receipt_view.amount, amount);
        bytes = amount;
        length = sizeof(amount);
    } else if (section == 4U) {
        bytes = value->receipt_view.resulting_state_root;
        length = 32U;
    } else {
        return LXP_ERR_UNKNOWN_FIELD;
    }
    if (offset >= length) return LXP_ERR_TRUNCATED;
    return (lxp_result)bytes[offset];
}

lxp_result layerx_programs_call_arbiter_authority_begin(
    uint64_t token, uint64_t d0, uint64_t d1, uint64_t d2, uint64_t d3,
    uint64_t expected_batch)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_verified_receipt_authority_facts facts;
    uint8_t digest[32];
    uint8_t *bytes;
    size_t offset = 0U;
    lxp_result status;
    if (value == NULL) return LXP_ERR_NON_CANONICAL;
    value->arbiter_authority_active = false;
    (void)memset(value->arbiter_authority, 0,
                 sizeof(value->arbiter_authority));
    if (value->ctx == NULL || value->authority == NULL ||
        value->network_id == 0U || expected_batch == 0U)
        return LXP_ERR_NON_CANONICAL;
    write_u64(digest, d0);
    write_u64(digest + 8U, d1);
    write_u64(digest + 16U, d2);
    write_u64(digest + 24U, d3);
    if (lxp_ct_is_zero(digest, sizeof(digest)))
        return LXP_ERR_NON_CANONICAL;
    (void)memset(&facts, 0, sizeof(facts));
    status = lxp_ctx_verified_receipt_authority_facts(
        value->ctx, digest, &facts);
    if (status != LXP_OK) return status;
    if (facts.version != 1U)
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (facts.network_id != value->network_id)
        return LXP_ERR_WRONG_NETWORK;
    if (lxp_ct_memcmp(facts.receipt_digest, digest, sizeof(digest)) != 0)
        return LXP_ERR_ROOT_MISMATCH;
    if (facts.batch_number != expected_batch)
        return LXP_ERR_ROOT_MISMATCH;
    if (facts.batch_number >= value->ctx->batch_number ||
        facts.global_sequence == 0U ||
        facts.global_sequence >= value->ctx->global_sequence)
        return LXP_ERR_NOT_YET_VALID;
    if (facts.authorization_first_batch_number > facts.batch_number ||
        facts.authorization_last_batch_number < facts.batch_number)
        return LXP_ERR_AUTH_SCOPE;
    bytes = value->arbiter_authority;
#define AUTHORITY_U16(field) do { \
    bytes[offset++] = (uint8_t)((field) >> 8U); \
    bytes[offset++] = (uint8_t)(field); \
} while (0)
#define AUTHORITY_U32(field) do { \
    const uint32_t scalar = (uint32_t)(field); \
    bytes[offset++] = (uint8_t)(scalar >> 24U); \
    bytes[offset++] = (uint8_t)(scalar >> 16U); \
    bytes[offset++] = (uint8_t)(scalar >> 8U); \
    bytes[offset++] = (uint8_t)scalar; \
} while (0)
#define AUTHORITY_U64(field) do { \
    write_u64(bytes + offset, (field)); \
    offset += 8U; \
} while (0)
#define AUTHORITY_HASH(field) do { \
    (void)memcpy(bytes + offset, (field), 32U); \
    offset += 32U; \
} while (0)
    AUTHORITY_U16(facts.version);
    AUTHORITY_U32(facts.network_id);
    AUTHORITY_U16(facts.protocol_version);
    AUTHORITY_U16(facts.module_id);
    bytes[offset++] = facts.operation;
    AUTHORITY_U32(facts.result_code);
    AUTHORITY_U64(facts.batch_number);
    AUTHORITY_U64(facts.epoch);
    AUTHORITY_U64(facts.global_sequence);
    AUTHORITY_U64(facts.authorization_first_batch_number);
    AUTHORITY_U64(facts.authorization_last_batch_number);
    AUTHORITY_HASH(facts.receipt_digest);
    AUTHORITY_HASH(facts.activity_id);
    AUTHORITY_HASH(facts.previous_state_root);
    AUTHORITY_HASH(facts.resulting_state_root);
    AUTHORITY_HASH(facts.sequencer_id);
    AUTHORITY_HASH(facts.sequencer_public_key);
#undef AUTHORITY_HASH
#undef AUTHORITY_U64
#undef AUTHORITY_U32
#undef AUTHORITY_U16
    if (offset != sizeof(value->arbiter_authority)) {
        (void)memset(value->arbiter_authority, 0,
                     sizeof(value->arbiter_authority));
        return LXP_FATAL_INVARIANT;
    }
    value->arbiter_authority_active = true;
    return LXP_OK;
}

lxp_result layerx_programs_call_arbiter_authority_byte(
    uint64_t token, uint32_t offset)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || !value->arbiter_authority_active)
        return LXP_ERR_UNKNOWN_FIELD;
    if (offset >= sizeof(value->arbiter_authority))
        return LXP_ERR_TRUNCATED;
    return (lxp_result)value->arbiter_authority[offset];
}

lxp_result layerx_programs_call_balance_view_begin(
    uint64_t token,
    uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3,
    uint64_t s0, uint64_t s1, uint64_t s2, uint64_t s3,
    uint64_t d0, uint64_t d1, uint64_t d2, uint64_t d3)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t account[32];
    uint8_t asset[32];
    uint8_t digest[32];
    uint8_t input[96];
    lx_programs_balance_view verified;
    lxp_result status;
    if (value == NULL || value->ctx == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memset(&value->balance_view, 0, sizeof(value->balance_view));
    write_u64(account, a0);
    write_u64(account + 8U, a1);
    write_u64(account + 16U, a2);
    write_u64(account + 24U, a3);
    write_u64(asset, s0);
    write_u64(asset + 8U, s1);
    write_u64(asset + 16U, s2);
    write_u64(asset + 24U, s3);
    write_u64(digest, d0);
    write_u64(digest + 8U, d1);
    write_u64(digest + 16U, d2);
    write_u64(digest + 24U, d3);
    (void)memcpy(input, account, 32U);
    (void)memcpy(input + 32U, asset, 32U);
    (void)memcpy(input + 64U, digest, 32U);
    status = lxp_programs_balance_read(
        value->ctx, account, asset, digest, &verified);
    if (status != LXP_OK) return replay_host_failure(value, 2U, status, input, sizeof(input));
    if (lxp_ct_memcmp(verified.account.id, account, 32U) != 0 ||
        lxp_ct_memcmp(verified.account.asset_id, asset, 32U) != 0 ||
        lxp_ct_memcmp(verified.receipt_digest, digest, 32U) != 0 ||
        lxp_ct_is_zero(verified.state_root, 32U) ||
        verified.observed_sequence == 0U)
        return replay_host_failure(value, 2U, LXP_ERR_ROOT_MISMATCH, input, sizeof(input));
    (void)memcpy(value->balance_view.account, verified.account.id, 32U);
    (void)memcpy(value->balance_view.asset, verified.account.asset_id, 32U);
    value->balance_view.balance = verified.balance;
    (void)memcpy(value->balance_view.receipt_digest,
                 verified.receipt_digest, 32U);
    (void)memcpy(value->balance_view.state_root, verified.state_root, 32U);
    value->balance_view.observed_sequence = verified.observed_sequence;
    value->balance_view.active = true;
    if (value->replay.max_bytes != 0U) {
        uint8_t facts[152];
        (void)memcpy(facts, value->balance_view.account, 32U);
        (void)memcpy(facts + 32U, value->balance_view.asset, 32U);
        (void)lxp_u128_to_be(value->balance_view.balance, facts + 64U);
        (void)memcpy(facts + 80U, value->balance_view.receipt_digest, 32U);
        (void)memcpy(facts + 112U, value->balance_view.state_root, 32U);
        write_u64(facts + 144U, value->balance_view.observed_sequence);
        return replay_host_fact(value, 2U, facts, sizeof(facts), NULL, 0U);
    }
    return LXP_OK;
}

lxp_result layerx_programs_call_balance_view_byte(
    uint64_t token, uint16_t section, uint32_t offset)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t balance[16];
    uint8_t sequence[8];
    const uint8_t *bytes;
    size_t length;
    if (value == NULL || !value->balance_view.active)
        return LXP_ERR_UNKNOWN_FIELD;
    if (section == 0U) {
        bytes = value->balance_view.account;
        length = 32U;
    } else if (section == 1U) {
        bytes = value->balance_view.asset;
        length = 32U;
    } else if (section == 2U) {
        lxp_u128_to_be(value->balance_view.balance, balance);
        bytes = balance;
        length = sizeof(balance);
    } else if (section == 3U) {
        bytes = value->balance_view.receipt_digest;
        length = 32U;
    } else if (section == 4U) {
        bytes = value->balance_view.state_root;
        length = 32U;
    } else if (section == 5U) {
        write_u64(sequence, value->balance_view.observed_sequence);
        bytes = sequence;
        length = sizeof(sequence);
    } else {
        return LXP_ERR_UNKNOWN_FIELD;
    }
    if ((size_t)offset >= length) return LXP_ERR_TRUNCATED;
    return (lxp_result)bytes[offset];
}

lxp_result layerx_programs_call_oracle_view_begin(
    uint64_t token, uint64_t m0, uint64_t m1, uint64_t m2, uint64_t m3)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t market_id[32];
    lx_oracle_committed committed;
    lxp_result status;
    if (value == NULL || value->ctx == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memset(&value->oracle_view, 0, sizeof(value->oracle_view));
    write_u64(market_id, m0);
    write_u64(market_id + 8U, m1);
    write_u64(market_id + 16U, m2);
    write_u64(market_id + 24U, m3);
    status = lx_oracle_committed_read(value->ctx, market_id, &committed);
    if (status != LXP_OK) return replay_host_failure(value, 3U, status, market_id, sizeof(market_id));
    if (lxp_ct_memcmp(committed.market_id, market_id, 32U) != 0)
        return replay_host_failure(value, 3U, LXP_ERR_ROOT_MISMATCH, market_id, sizeof(market_id));
    status = lx_oracle_committed_encode(&committed, value->oracle_view.record);
    if (status != LXP_OK) return replay_host_failure(value, 3U, status, market_id, sizeof(market_id));
    (void)memcpy(value->oracle_view.market_id, market_id, 32U);
    value->oracle_view.active = true;
    return replay_host_fact(value, 3U, value->oracle_view.market_id, 32U,
        value->oracle_view.record, LX_ORACLE_COMMITTED_BYTES);
}

lxp_result layerx_programs_call_oracle_view_byte(
    uint64_t token, uint16_t section, uint32_t offset)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *bytes;
    size_t length;
    if (value == NULL || !value->oracle_view.active)
        return LXP_ERR_UNKNOWN_FIELD;
    if (section == 0U) {
        bytes = value->oracle_view.market_id;
        length = 32U;
    } else if (section == 1U) {
        bytes = value->oracle_view.record;
        length = LX_ORACLE_COMMITTED_BYTES;
    } else {
        return LXP_ERR_UNKNOWN_FIELD;
    }
    if ((size_t)offset >= length) return LXP_ERR_TRUNCATED;
    return (lxp_result)bytes[offset];
}

lxp_result layerx_programs_call_web_view_begin(
    uint64_t token, uint64_t p0, uint64_t p1, uint64_t p2, uint64_t p3,
    uint64_t request_id)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t program_id[32];
    uint8_t input[40];
    lx_web_answer answer;
    size_t index;
    lxp_result status;
    if (value == NULL || value->ctx == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memset(&value->web_view, 0, sizeof(value->web_view));
    write_u64(program_id, p0);
    write_u64(program_id + 8U, p1);
    write_u64(program_id + 16U, p2);
    write_u64(program_id + 24U, p3);
    (void)memcpy(input, program_id, 32U);
    write_u64(input + 32U, request_id);
    status = lx_web_committed_read(value->ctx, program_id, request_id,
                                   &answer);
    if (status != LXP_OK) return replay_host_failure(value, 4U, status, input, sizeof(input));
    if (lxp_ct_memcmp(answer.program_id, program_id, 32U) != 0 ||
        answer.request_id != request_id ||
        answer.response_length > LX_WEB_MAX_RESPONSE_BYTES ||
        answer.response_length > answer.full_length)
        return replay_host_failure(value, 4U, LXP_ERR_ROOT_MISMATCH, input, sizeof(input));
    (void)memcpy(value->web_view.program_id, program_id, 32U);
    (void)memcpy(value->web_view.header, answer.content_digest, 32U);
    for (index = 0U; index < 4U; ++index) {
        value->web_view.header[32U + index] =
            (uint8_t)(answer.full_length >> ((3U - index) * 8U));
        value->web_view.header[36U + index] =
            (uint8_t)(answer.response_length >> ((3U - index) * 8U));
    }
    value->web_view.response_length = answer.response_length;
    (void)memcpy(value->web_view.response, answer.response,
                 answer.response_length);
    value->web_view.active = true;
    if (value->replay.max_bytes != 0U) {
        uint8_t facts[32U + 8U + LX_WEB_ANSWER_HEADER_BYTES];
        (void)memcpy(facts, program_id, 32U);
        write_u64(facts + 32U, request_id);
        (void)memcpy(facts + 40U, value->web_view.header, LX_WEB_ANSWER_HEADER_BYTES);
        return replay_host_fact(value, 4U, facts, sizeof(facts),
            value->web_view.response, value->web_view.response_length);
    }
    return LXP_OK;
}

lxp_result layerx_programs_call_web_view_byte(
    uint64_t token, uint16_t section, uint32_t offset)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *bytes;
    size_t length;
    if (value == NULL || !value->web_view.active)
        return LXP_ERR_UNKNOWN_FIELD;
    if (section == 0U) {
        bytes = value->web_view.program_id;
        length = 32U;
    } else if (section == 1U) {
        bytes = value->web_view.header;
        length = LX_WEB_ANSWER_HEADER_BYTES;
    } else if (section == 2U) {
        bytes = value->web_view.response;
        length = value->web_view.response_length;
    } else {
        return LXP_ERR_UNKNOWN_FIELD;
    }
    if ((size_t)offset >= length) return LXP_ERR_TRUNCATED;
    return (lxp_result)bytes[offset];
}

static lxp_result catalog_storage_cell(const lxp_programs_call_activity *value,
                                       uint32_t program_index,
                                       uint16_t selector, uint32_t index,
                                       const uint8_t **key, uint16_t *key_length,
                                       const uint8_t **cell_value,
                                       uint32_t *value_length, uint32_t *count)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(
        (lxp_programs_call_activity *)value, program_index);
    uint8_t ns[65];
    uint16_t ns_length;
    lxp_result status;
    if (entry == NULL) return LXP_ERR_NON_CANONICAL;
    status = call_namespace_for_program(value, entry->program_id, selector,
                                        ns, &ns_length);
    if (status != LXP_OK) return status;
    return lxp_programs_storage_cell_at(value->ctx, ns, ns_length, index,
                                        key, key_length, cell_value,
                                        value_length, count);
}

lxp_result layerx_programs_call_catalog_storage_cell_count(
    uint64_t token, uint32_t program_index, uint16_t selector)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value;
    uint16_t key_length;
    uint32_t value_length, count = 0U;
    lxp_result status = catalog_storage_cell(value, program_index, selector, 0U,
                                             &key, &key_length, &cell_value,
                                             &value_length, &count);
    if (status == LXP_ERR_UNKNOWN_FIELD && count == 0U) return 0;
    return status == LXP_OK ? (lxp_result)count : status;
}

lxp_result layerx_programs_call_catalog_storage_cell_length(
    uint64_t token, uint32_t program_index, uint16_t selector,
    uint32_t index, uint16_t section)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value;
    uint16_t key_length;
    uint32_t value_length, count;
    lxp_result status = catalog_storage_cell(value, program_index, selector, index,
                                             &key, &key_length, &cell_value,
                                             &value_length, &count);
    if (status != LXP_OK) return status;
    if (section == 0U) return (lxp_result)key_length;
    if (section == 1U && value_length <= INT32_MAX) return (lxp_result)value_length;
    return section == 1U ? LXP_ERR_LENGTH_LIMIT : LXP_ERR_UNKNOWN_FIELD;
}

lxp_result layerx_programs_call_catalog_storage_cell_byte(
    uint64_t token, uint32_t program_index, uint16_t selector,
    uint32_t index, uint16_t section, uint32_t offset)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *key, *cell_value, *bytes;
    uint16_t key_length;
    uint32_t value_length, count, length;
    lxp_result status = catalog_storage_cell(value, program_index, selector, index,
                                             &key, &key_length, &cell_value,
                                             &value_length, &count);
    if (status != LXP_OK) return status;
    if (section == 0U) { bytes = key; length = key_length; }
    else if (section == 1U) { bytes = cell_value; length = value_length; }
    else return LXP_ERR_UNKNOWN_FIELD;
    return offset < length ? (lxp_result)bytes[offset] : LXP_ERR_TRUNCATED;
}

static lxp_result catalog_storage_final_slot(lxp_programs_call_activity *value,
                                             uint32_t program_index,
                                             uint16_t selector,
                                             lxp_programs_storage_cell **cells,
                                             uint32_t **count)
{
    lxp_programs_call_catalog_entry *entry = catalog_entry(value, program_index);
    if (entry == NULL || selector > 1U || cells == NULL || count == NULL)
        return LXP_ERR_NON_CANONICAL;
    *cells = entry->storage_final[selector].cells;
    *count = &entry->storage_final[selector].count;
    return LXP_OK;
}

lxp_result layerx_programs_call_catalog_storage_final_begin(
    uint64_t token, uint32_t program_index, uint16_t selector, uint32_t count)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_storage_cell *cells;
    uint32_t *stored_count;
    void *allocation;
    lxp_result status = catalog_storage_final_slot(value, program_index, selector,
                                                    &cells, &stored_count);
    lxp_programs_call_catalog_entry *entry = catalog_entry(value, program_index);
    if (status != LXP_OK || entry == NULL || entry->storage_final[selector].begun)
        return status == LXP_OK ? LXP_ERR_NON_CANONICAL : status;
    if (count > INT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    if (count == 0U) { entry->storage_final[selector].begun = true; return LXP_OK; }
    if (sizeof(*cells) > SIZE_MAX / count) return LXP_ERR_LENGTH_LIMIT;
    status = lxp_ctx_arena_alloc(value->ctx, (size_t)count * sizeof(*cells),
                                 _Alignof(lxp_programs_storage_cell), &allocation);
    if (status != LXP_OK) return status;
    entry->storage_final[selector].begun = true;
    entry->storage_final[selector].cells =
        (lxp_programs_storage_cell *)allocation;
    cells = entry->storage_final[selector].cells;
    *stored_count = count;
    (void)memset(cells, 0, (size_t)count * sizeof(*cells));
    return LXP_OK;
}

lxp_result layerx_programs_call_catalog_storage_final_cell(
    uint64_t token, uint32_t program_index, uint16_t selector, uint32_t index,
    uint16_t key_length, uint32_t value_length)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_storage_cell *cells;
    uint32_t *count;
    void *key, *cell_value;
    lxp_result status = catalog_storage_final_slot(value, program_index, selector,
                                                    &cells, &count);
    if (status != LXP_OK || cells == NULL || index >= *count || key_length == 0U ||
        key_length > LX_PROGRAMS_STORAGE_MAX_KEY_BYTES ||
        value_length > LX_PROGRAMS_STORAGE_MAX_VALUE_BYTES)
        return status == LXP_OK ? LXP_ERR_NON_CANONICAL : status;
    if (cells[index].key != NULL) return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(value->ctx, key_length, 1U, &key);
    if (status != LXP_OK) return status;
    status = lxp_ctx_arena_alloc(value->ctx, value_length == 0U ? 1U : value_length,
                                 1U, &cell_value);
    if (status != LXP_OK) return status;
    cells[index].key = key; cells[index].key_length = key_length;
    cells[index].value = cell_value; cells[index].value_length = value_length;
    return LXP_OK;
}

lxp_result layerx_programs_call_catalog_storage_final_byte(
    uint64_t token, uint32_t program_index, uint16_t selector, uint32_t index,
    uint16_t section, uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_storage_cell *cells;
    uint32_t *count, length;
    uint8_t *bytes;
    lxp_result status = catalog_storage_final_slot(value, program_index, selector,
                                                    &cells, &count);
    if (status != LXP_OK || cells == NULL || index >= *count)
        return status == LXP_OK ? LXP_ERR_NON_CANONICAL : status;
    if (section == 0U) { bytes = (uint8_t *)cells[index].key; length = cells[index].key_length; }
    else if (section == 1U) { bytes = (uint8_t *)cells[index].value; length = cells[index].value_length; }
    else return LXP_ERR_UNKNOWN_FIELD;
    if (bytes == NULL || offset >= length) return LXP_ERR_TRUNCATED;
    bytes[offset] = byte;
    return LXP_OK;
}

lxp_result layerx_programs_call_catalog_storage_final_apply(
    uint64_t token, uint32_t program_index, uint16_t selector)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_call_catalog_entry *entry = catalog_entry(value, program_index);
    uint8_t ns[65];
    uint16_t ns_length;
    lxp_result status;
    if (entry == NULL || selector > 1U) return LXP_ERR_NON_CANONICAL;
    if (!value->storage_settlement_authorized ||
        !entry->storage_final[selector].begun) return LXP_ERR_NON_CANONICAL;
    if (entry->storage_final[selector].applied) return LXP_ERR_NON_CANONICAL;
    status = call_namespace_for_program(value, entry->program_id, selector,
                                        ns, &ns_length);
    if (status != LXP_OK) return status;
    status = lxp_programs_storage_stage_final(value->ctx, ns, ns_length,
        entry->storage_final[selector].cells, entry->storage_final[selector].count);
    if (status == LXP_OK) entry->storage_final[selector].applied = true;
    return status;
}

lxp_result layerx_programs_call_storage_final_authorize(uint64_t token)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->ctx == NULL || value->storage_settlement_authorized)
        return LXP_ERR_NON_CANONICAL;
    value->storage_settlement_authorized = true;
    return LXP_OK;
}

static lxp_result terminal_buffer(lxp_programs_call_activity *value,
                                  uint16_t section, uint8_t **bytes,
                                  uint32_t *length)
{
    if (value == NULL || !value->terminal.active || bytes == NULL || length == NULL)
        return LXP_ERR_NON_CANONICAL;
    if (section == LX_PROGRAMS_TERMINAL_BYTES_GRAPH) {
        *bytes = value->terminal.graph;
        *length = value->terminal.graph_length;
    } else if (section == LX_PROGRAMS_TERMINAL_BYTES_PAYLOAD) {
        *bytes = value->terminal.terminal;
        *length = value->terminal.terminal_length;
    } else if (section == LX_PROGRAMS_TERMINAL_BYTES_EVENTS) {
        *bytes = value->terminal.events;
        *length = value->terminal.events_length;
    } else return LXP_ERR_UNKNOWN_FIELD;
    return *bytes == NULL || *length == 0U ? LXP_ERR_NON_CANONICAL : LXP_OK;
}

lxp_result layerx_programs_call_terminal_begin(
    uint64_t token, uint8_t terminal_kind, lxp_result result_code,
    uint16_t runtime_version,
    uint16_t abi_version, uint32_t fee_schedule_version,
    uint32_t metering_schedule_version,
    uint64_t cpu_fuel, uint64_t memory_bytes, uint64_t storage_read_bytes,
    uint64_t storage_write_bytes, uint32_t output_values, uint64_t output_bytes,
    uint64_t fee_hi, uint64_t fee_lo,
    uint64_t transfer0, uint64_t transfer1, uint64_t transfer2, uint64_t transfer3,
    uint32_t graph_length, uint32_t terminal_length, uint32_t events_length)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    const lxp_call_admission_facts *admission;
    uint8_t transfer_root[32];
    write_u64(transfer_root, transfer0);
    write_u64(transfer_root + 8U, transfer1);
    write_u64(transfer_root + 16U, transfer2);
    write_u64(transfer_root + 24U, transfer3);
    admission = value == NULL || value->ctx == NULL ? NULL :
        lxp_ctx_call_admission(value->ctx);
    if (value == NULL || value->ctx == NULL || value->terminal.active ||
        admission == NULL ||
        (terminal_kind != LXP_PROGRAM_TERMINAL_SUCCESS &&
         terminal_kind != LXP_PROGRAM_TERMINAL_FAILURE &&
         terminal_kind != LXP_PROGRAM_TERMINAL_RESOURCE) ||
        (terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS && result_code != LXP_OK) ||
        (terminal_kind != LXP_PROGRAM_TERMINAL_SUCCESS &&
         (result_code == LXP_OK || lxp_result_is_fatal(result_code))) ||
        runtime_version == 0U || abi_version == 0U ||
        abi_version != value->abi_version || fee_schedule_version == 0U ||
        fee_schedule_version != admission->fee_schedule_version ||
        !lxp_program_metering_schedule_available(
            metering_schedule_version) ||
        metering_schedule_version != admission->metering_schedule_version ||
        !value->terminal.reserved || graph_length == 0U || terminal_length == 0U ||
        events_length == 0U || graph_length > value->terminal.graph_capacity ||
        terminal_length > value->terminal.terminal_capacity ||
        events_length > value->terminal.events_capacity)
        return LXP_ERR_NON_CANONICAL;
    if (lxp_protocol_version_uses_occupancy(value->ctx->protocol_version) &&
        terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS &&
        (value->occupancy == NULL || !value->occupancy->applied))
        return LXP_FATAL_INVARIANT;
    if (terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS) {
        if (value->transfer_set == NULL) {
            if (!lxp_ct_is_zero(transfer_root, sizeof(transfer_root)))
                return LXP_ERR_NON_CANONICAL;
        } else if (!value->transfer_applied || lxp_ct_memcmp(
                       transfer_root, value->transfer_receipt.transfer_set_root,
                       sizeof(transfer_root)) != 0) return LXP_ERR_NON_CANONICAL;
    } else if (!lxp_ct_is_zero(transfer_root, sizeof(transfer_root))) {
        return LXP_ERR_NON_CANONICAL;
    }
    value->terminal.active = true;
    value->terminal.terminal_kind = terminal_kind;
    value->terminal.result_code = result_code;
    value->terminal.runtime_version = runtime_version;
    value->terminal.abi_version = abi_version;
    value->terminal.fee_schedule_version = fee_schedule_version;
    value->terminal.metering_schedule_version = metering_schedule_version;
    value->terminal.cpu_fuel = cpu_fuel;
    value->terminal.memory_bytes = memory_bytes;
    value->terminal.storage_read_bytes = storage_read_bytes;
    value->terminal.storage_write_bytes = storage_write_bytes;
    value->terminal.output_values = output_values;
    value->terminal.output_bytes = output_bytes;
    value->terminal.fee_units = (lxp_u128){fee_hi, fee_lo};
    (void)memcpy(value->terminal.transfer_root, transfer_root,
                 sizeof(transfer_root));
    value->terminal.graph_length = graph_length;
    value->terminal.terminal_length = terminal_length;
    value->terminal.events_length = events_length;
    return LXP_OK;
}

lxp_result layerx_programs_call_terminal_reserve(
    uint64_t token, uint32_t graph_capacity,
    uint32_t terminal_capacity, uint32_t events_capacity)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->ctx == NULL || value->terminal.reserved ||
        graph_capacity == 0U || terminal_capacity == 0U || events_capacity == 0U)
        return LXP_ERR_NON_CANONICAL;
    value->terminal.graph = malloc(graph_capacity);
    value->terminal.terminal = malloc(terminal_capacity);
    value->terminal.events = malloc(events_capacity);
    if (value->terminal.graph == NULL || value->terminal.terminal == NULL ||
        value->terminal.events == NULL) {
        free(value->terminal.graph); free(value->terminal.terminal);
        free(value->terminal.events);
        value->terminal.graph = NULL; value->terminal.terminal = NULL;
        value->terminal.events = NULL;
        return LXP_ERR_ARENA_EXHAUSTED;
    }
    value->terminal.graph_capacity = graph_capacity;
    value->terminal.terminal_capacity = terminal_capacity;
    value->terminal.events_capacity = events_capacity;
    value->terminal.reserved = true;
    return LXP_OK;
}

lxp_result layerx_programs_call_terminal_byte(uint64_t token, uint16_t section,
                                               uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t *bytes;
    uint32_t length;
    lxp_result status = terminal_buffer(value, section, &bytes, &length);
    if (status != LXP_OK) return status;
    if (offset >= length) return LXP_ERR_TRUNCATED;
    if (section >= 3U || offset != value->terminal.written[section])
        return LXP_ERR_NON_CANONICAL;
    bytes[offset] = byte;
    ++value->terminal.written[section];
    return LXP_OK;
}

lxp_result layerx_programs_call_terminal_applied_begin(uint64_t token, uint32_t length)
{
    lxp_programs_call_activity *value = (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->ctx == NULL || value->terminal.active ||
        !value->terminal.reserved || !value->transfer_applied ||
        value->terminal.applied_length != 0U || length == 0U ||
        length > sizeof(value->terminal.applied_legs) ||
        length != (uint32_t)value->transfer_leg_count * 115U)
        return LXP_ERR_NON_CANONICAL;
    value->terminal.applied_length = length;
    return LXP_OK;
}

lxp_result layerx_programs_call_terminal_applied_byte(uint64_t token, uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value = (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->ctx == NULL || value->terminal.active ||
        offset != value->terminal.applied_written || offset >= value->terminal.applied_length)
        return LXP_ERR_NON_CANONICAL;
    value->terminal.applied_legs[offset] = byte;
    ++value->terminal.applied_written;
    return LXP_OK;
}

static void terminal_write_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U);
    bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U);
    bytes[3] = (uint8_t)value;
}

static lxp_result terminal_wrap_applied(lxp_programs_call_activity *value)
{
    static const uint8_t domain[] = "LXP/programs/terminal-applied-legs/v1";
    uint8_t hashes[LXP_MAX_TRANSFER_SET_LEGS][32];
    uint32_t length = value->terminal.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS ?
        value->terminal.applied_length : 0U;
    uint32_t detail_length = value->terminal.terminal_length;
    size_t prefix = sizeof(domain) + 4U;
    size_t count = length / 115U, i;
    lxp_result status;
    if (value->terminal.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS) {
        if (value->terminal.applied_written != length ||
            ((length == 0U) != lxp_ct_is_zero(value->terminal.transfer_root, 32U)))
            return LXP_ERR_NON_CANONICAL;
        for (i = 0U; i < count; ++i) {
            status = lxp_merkle_leaf_hash(value->terminal.applied_legs + i * 115U, 115U, hashes[i]);
            if (status != LXP_OK) return status;
        }
        while (count > 1U) {
            size_t next = (count + 1U) / 2U;
            for (i = 0U; i < next; ++i) {
                size_t right = i * 2U + 1U < count ? i * 2U + 1U : i * 2U;
                status = lxp_merkle_node_hash(hashes[i * 2U], hashes[right], hashes[i]);
                if (status != LXP_OK) return status;
            }
            count = next;
        }
        if (length != 0U && lxp_ct_memcmp(hashes[0], value->terminal.transfer_root, 32U) != 0)
            return LXP_ERR_NON_CANONICAL;
    }
    if (prefix + detail_length + 4U + length > value->terminal.terminal_capacity ||
        prefix + detail_length + 4U + length > LXP_MAX_ACTIVITY_BYTES)
        return LXP_ERR_LENGTH_LIMIT;
    (void)memmove(value->terminal.terminal + prefix, value->terminal.terminal, detail_length);
    (void)memcpy(value->terminal.terminal, domain, sizeof(domain));
    terminal_write_u32(value->terminal.terminal + sizeof(domain), detail_length);
    terminal_write_u32(value->terminal.terminal + prefix + detail_length, length);
    (void)memcpy(value->terminal.terminal + prefix + detail_length + 4U,
                 value->terminal.applied_legs, length);
    value->terminal.terminal_length = (uint32_t)(prefix + detail_length + 4U + length);
    return LXP_OK;
}

static lxp_result web_request_record(lxp_programs_call_activity *value);

lxp_result layerx_programs_call_terminal_publish(uint64_t token)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_program_outcome outcome;
    lxp_programs_call_outcome event;
    const lxp_call_admission_facts *admission;
    const uint8_t *activity_id;
    uint8_t graph_root[32], terminal_root[32], events_root[32], frame[8] = {0};
    lxp_result status;
    if (value == NULL || value->ctx == NULL || !value->terminal.active ||
        value->terminal.written[0] != value->terminal.graph_length ||
        value->terminal.written[1] != value->terminal.terminal_length ||
        value->terminal.written[2] != value->terminal.events_length)
        return LXP_ERR_NON_CANONICAL;
    admission = lxp_ctx_call_admission(value->ctx);
    activity_id = lxp_ctx_activity_id(value->ctx);
    if (admission == NULL || activity_id == NULL) return LXP_FATAL_INVARIANT;
    if (value->ctx->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT) {
        status = terminal_wrap_applied(value);
        if (status != LXP_OK) return status;
    }
    status = lxp_hash_sha256(value->terminal.graph, value->terminal.graph_length, graph_root);
    if (status == LXP_OK) status = lxp_hash_sha256(value->terminal.terminal,
                                                    value->terminal.terminal_length,
                                                    terminal_root);
    if (status == LXP_OK) status = lxp_hash_sha256(value->terminal.events,
                                                    value->terminal.events_length,
                                                    events_root);
    if (status != LXP_OK) return status;
    (void)memset(&outcome, 0, sizeof(outcome));
    outcome.present = true;
    outcome.encoding_version = value->ctx->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT ? 4U : 3U;
    if (outcome.encoding_version == 4U) {
        status = lxp_hash_sha256(value->terminal.applied_legs,
            value->terminal.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS ? value->terminal.applied_length : 0U,
            outcome.applied_legs_digest);
        if (status != LXP_OK) return status;
    }
    outcome.terminal_kind = value->terminal.terminal_kind;
    outcome.result_code = value->terminal.result_code;
    outcome.runtime_version = value->terminal.runtime_version;
    outcome.abi_version = value->terminal.abi_version;
    outcome.fee_schedule_version = value->terminal.fee_schedule_version;
    outcome.metering_schedule_version =
        value->terminal.metering_schedule_version;
    outcome.cpu_fuel = value->terminal.cpu_fuel;
    outcome.memory_bytes = value->terminal.memory_bytes;
    outcome.storage_read_bytes = value->terminal.storage_read_bytes;
    outcome.storage_write_bytes = value->terminal.storage_write_bytes;
    outcome.output_values = value->terminal.output_values;
    outcome.output_bytes = value->terminal.output_bytes;
    outcome.fee_units = value->terminal.fee_units;
    if (outcome.encoding_version >= 2U)
        (void)memcpy(outcome.fee_schedule_prices,
                     admission->fee_schedule_prices,
                     sizeof(outcome.fee_schedule_prices));
    if (outcome.encoding_version >= 2U &&
        outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS) {
        outcome.occupancy_byte_batches =
            value->occupancy->receipt.byte_batches;
        outcome.occupancy_fee_units =
            value->occupancy->receipt.fee_units;
        (void)memcpy(outcome.occupancy_asset_id,
                     value->occupancy->receipt.occupancy_asset_id, 32U);
        (void)memcpy(outcome.occupancy_evidence_digest,
                     value->occupancy->receipt.settlement_evidence_digest,
                     sizeof(outcome.occupancy_evidence_digest));
        (void)memcpy(outcome.occupancy_transfer_root,
                     value->occupancy->receipt.transfer_set_root,
                     sizeof(outcome.occupancy_transfer_root));
    }
    (void)memcpy(outcome.call_graph_root, graph_root, sizeof(graph_root));
    (void)memcpy(outcome.terminal_payload_root, terminal_root, sizeof(terminal_root));
    {
        outcome.call_graph_payload = (lxp_byte_span){
            value->terminal.graph, value->terminal.graph_length
        };
        outcome.terminal_payload = (lxp_byte_span){
            value->terminal.terminal, value->terminal.terminal_length
        };
    }
    if (outcome.terminal_kind != LXP_PROGRAM_TERMINAL_SUCCESS)
        return lxp_ctx_bind_program_outcome(value->ctx, &outcome);
    (void)memcpy(outcome.transfer_root, value->terminal.transfer_root, 32U);
    outcome.event_envelope_payload = (lxp_byte_span){
        value->terminal.events, value->terminal.events_length
    };
    status = web_request_record(value);
    if (status != LXP_OK) return status;
    event.program_id = value->program_id;
    event.principal = value->authority->principal;
    event.activity_id = activity_id;
    event.frame_path = frame;
    event.frame_depth = 0U;
    event.runtime_version = outcome.runtime_version;
    event.abi_version = outcome.abi_version;
    event.fee_schedule_version = outcome.fee_schedule_version;
    event.metering_schedule_version = outcome.metering_schedule_version;
    event.terminal_result = outcome.result_code;
    event.transfer_set_root = outcome.transfer_root;
    event.call_graph_digest = outcome.call_graph_root;
    event.terminal_detail_digest = outcome.terminal_payload_root;
    event.event_envelope_digest = events_root;
    status = lxp_programs_emit_call_outcome(value->ctx, &event);
    if (status != LXP_OK) return status;
    return lxp_ctx_bind_program_outcome(value->ctx, &outcome);
}

static bool catalog_contains(const lxp_programs_call_activity *value,
                             const uint8_t program_id[32])
{
    uint32_t index;
    if (value == NULL || value->catalog == NULL) return false;
    for (index = 0U; index < value->catalog_count; ++index)
        if (lxp_ct_memcmp(value->catalog[index].program_id, program_id, 32U) == 0)
            return true;
    return false;
}

static bool event_frame_valid(const uint8_t path[8], uint8_t depth)
{
    size_t index;
    if (depth > 8U) return false;
    for (index = 0U; index < 8U; ++index)
        if ((index < depth && path[index] == 0U) ||
            (index >= depth && path[index] != 0U)) return false;
    return true;
}

lxp_result layerx_programs_call_event_begin(
    uint64_t token, uint32_t event_index,
    uint64_t p0, uint64_t p1, uint64_t p2, uint64_t p3,
    uint64_t r0, uint64_t r1, uint64_t r2, uint64_t r3,
    uint64_t frame_path, uint8_t frame_depth,
    uint16_t topic_length, uint32_t data_length)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    const lxp_call_admission_facts *admission;
    void *topic, *data;
    lxp_result status;
    if (value == NULL || value->ctx == NULL || value->event.active ||
        event_index != value->emitted_event_count ||
        value->emitted_event_count >= LXP_PROGRAMS_EVENT_MAX_COUNT ||
        topic_length > LXP_PROGRAMS_EVENT_MAX_TOPIC_BYTES ||
        data_length > LXP_PROGRAMS_EVENT_MAX_DATA_BYTES)
        return LXP_ERR_NON_CANONICAL;
    admission = lxp_ctx_call_admission(value->ctx);
    if (admission == NULL) return LXP_FATAL_INVARIANT;
    write_u64(value->event.program_id, p0);
    write_u64(value->event.program_id + 8U, p1);
    write_u64(value->event.program_id + 16U, p2);
    write_u64(value->event.program_id + 24U, p3);
    write_u64(value->event.principal, r0);
    write_u64(value->event.principal + 8U, r1);
    write_u64(value->event.principal + 16U, r2);
    write_u64(value->event.principal + 24U, r3);
    write_u64(value->event.frame_path, frame_path);
    if (!catalog_contains(value, value->event.program_id) ||
        lxp_ct_memcmp(value->event.principal, value->authority->principal, 32U) != 0 ||
        !event_frame_valid(value->event.frame_path, frame_depth))
        return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(value->ctx, topic_length == 0U ? 1U : topic_length,
                                 1U, &topic);
    if (status != LXP_OK) return status;
    status = lxp_ctx_arena_alloc(value->ctx, data_length == 0U ? 1U : data_length,
                                 1U, &data);
    if (status != LXP_OK) return status;
    value->event.written[0] = 0U;
    value->event.written[1] = 0U;
    value->event.active = true;
    value->event.event_index = event_index;
    value->event.frame_depth = frame_depth;
    value->event.topic = topic;
    value->event.topic_length = topic_length;
    value->event.data = data;
    value->event.data_length = data_length;
    return LXP_OK;
}

lxp_result layerx_programs_call_event_byte(uint64_t token, uint16_t section,
                                            uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    uint8_t *bytes;
    uint32_t length;
    if (value == NULL || !value->event.active) return LXP_ERR_NON_CANONICAL;
    if (section == 0U) {
        bytes = value->event.topic;
        length = value->event.topic_length;
    } else if (section == 1U) {
        bytes = value->event.data;
        length = value->event.data_length;
    } else return LXP_ERR_UNKNOWN_FIELD;
    if (bytes == NULL || offset >= length) return LXP_ERR_TRUNCATED;
    if (offset != value->event.written[section]) return LXP_ERR_NON_CANONICAL;
    bytes[offset] = byte;
    ++value->event.written[section];
    return LXP_OK;
}

/* A web request record emitted under the request topic is admitted only
 * with this call's applied transfer into the web fee account, and at most
 * once per call; a request id the program already used, pending or
 * answered, is refused. */
static lxp_result web_request_capture(lxp_programs_call_activity *value)
{
    static const uint8_t answer_prefix[LX_WEB_ANSWER_PREFIX_BYTES] = {
        'w', 'e', 'b', '/', 'a', 'n', 's', 'w', 'e', 'r'
    };
    uint8_t key[WEB_PENDING_KEY_BYTES];
    uint8_t answer_key[LX_WEB_ANSWER_KEY_BYTES];
    const uint8_t *stored;
    size_t stored_length;
    lxp_byte_span payload;
    lxp_u128 total = {0U, 0U};
    uint64_t request_id;
    uint8_t kind;
    uint16_t index;
    bool paid = false;
    lxp_result status;
    if (value->web_request.active) return LXP_ERR_DUPLICATE_ENTRY;
    status = lx_web_request_record_decode(value->event.data,
                                          value->event.data_length,
                                          &request_id, &kind, &payload);
    if (status != LXP_OK) return status;
    web_pending_key(value->event.program_id, request_id, key);
    status = lxp_ctx_kv_get(value->ctx, key, sizeof(key), &stored,
                            &stored_length);
    if (status == LXP_OK) return LXP_ERR_SEQUENCE_REUSED;
    if (status != LXP_ERR_UNKNOWN_FIELD) return status;
    (void)memcpy(answer_key, answer_prefix, sizeof(answer_prefix));
    (void)memcpy(answer_key + LX_WEB_ANSWER_PREFIX_BYTES,
                 value->event.program_id, 32U);
    write_u64(answer_key + LX_WEB_ANSWER_PREFIX_BYTES + 32U, request_id);
    answer_key[LX_WEB_ANSWER_KEY_BYTES - 1U] = 0U;
    status = lxp_ctx_kv_get(value->ctx, answer_key, sizeof(answer_key),
                            &stored, &stored_length);
    if (status == LXP_OK) return LXP_ERR_SEQUENCE_REUSED;
    if (status != LXP_ERR_UNKNOWN_FIELD) return status;
    if (!value->transfer_applied || value->transfer_set == NULL)
        return LXP_ERR_FEE_UNPAYABLE;
    for (index = 0U; index < value->transfer_set->leg_count; ++index) {
        const lxp_transfer_leg *leg = &value->transfer_set->legs[index];
        uint8_t fee_account[32];
        if (leg->to == NULL) return LXP_FATAL_INVARIANT;
        status = web_fee_account_id(leg->asset_id, fee_account);
        if (status != LXP_OK) return status;
        if (lxp_ct_memcmp(leg->to->id, fee_account, 32U) != 0) continue;
        if (paid && lxp_ct_memcmp(value->web_request.fee_asset,
                                  leg->asset_id, 32U) != 0)
            return LXP_ERR_ASSET_MISMATCH;
        status = lxp_u128_add(total, leg->amount, &total);
        if (status != LXP_OK) return status;
        (void)memcpy(value->web_request.fee_asset, leg->asset_id, 32U);
        (void)memcpy(value->web_request.fee_account, fee_account, 32U);
        paid = true;
    }
    if (!paid || lxp_u128_is_zero(total)) return LXP_ERR_FEE_UNPAYABLE;
    status = lxp_keccak256(payload.bytes, payload.length,
                           value->web_request.payload_hash);
    if (status != LXP_OK) return status;
    (void)memcpy(value->web_request.program_id, value->event.program_id, 32U);
    value->web_request.request_id = request_id;
    value->web_request.kind = kind;
    value->web_request.fee_amount = total;
    value->web_request.active = true;
    return LXP_OK;
}

/* Stages the pending record intake reads: kind, payload hash, the fee asset,
 * account and amount it splits, the recording sequence and the fulfilled
 * flag, under the requesting program and request id. */
static lxp_result web_request_record(lxp_programs_call_activity *value)
{
    uint8_t key[WEB_PENDING_KEY_BYTES];
    uint8_t record[WEB_PENDING_RECORD_BYTES];
    lxp_result status;
    if (!value->web_request.active) return LXP_OK;
    web_pending_key(value->web_request.program_id,
                    value->web_request.request_id, key);
    record[0] = WEB_PENDING_RECORD_VERSION;
    record[1] = value->web_request.kind;
    (void)memcpy(record + 2U, value->web_request.payload_hash, 32U);
    (void)memcpy(record + 34U, value->web_request.fee_asset, 32U);
    (void)memcpy(record + 66U, value->web_request.fee_account, 32U);
    status = lxp_u128_to_be(value->web_request.fee_amount, record + 98U);
    if (status != LXP_OK) return status;
    write_u64(record + 114U, lxp_ctx_global_sequence(value->ctx));
    record[122] = 0U;
    return lxp_ctx_kv_put(value->ctx, key, sizeof(key), record,
                          sizeof(record));
}

lxp_result layerx_programs_call_event_emit(uint64_t token)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *activity_id;
    lxp_programs_guest_event event;
    lxp_result status;
    if (value == NULL || value->ctx == NULL || !value->event.active ||
        value->event.written[0] != value->event.topic_length ||
        value->event.written[1] != value->event.data_length)
        return LXP_ERR_NON_CANONICAL;
    activity_id = lxp_ctx_activity_id(value->ctx);
    if (activity_id == NULL) return LXP_FATAL_INVARIANT;
    event.program_id = value->event.program_id;
    event.principal = value->event.principal;
    event.activity_id = activity_id;
    event.frame_path = value->event.frame_path;
    event.frame_depth = value->event.frame_depth;
    event.event_index = value->event.event_index;
    event.topic = value->event.topic;
    event.topic_length = value->event.topic_length;
    event.data = value->event.data;
    event.data_length = value->event.data_length;
    if (value->event.topic_length == LX_WEB_REQUEST_TOPIC_BYTES &&
        memcmp(value->event.topic, LX_WEB_REQUEST_TOPIC,
               LX_WEB_REQUEST_TOPIC_BYTES) == 0) {
        status = web_request_capture(value);
        if (status != LXP_OK) return status;
    }
    status = lxp_programs_emit_guest_event(value->ctx, &event);
    if (status == LXP_OK) {
        value->event.active = false;
        ++value->emitted_event_count;
    }
    return status;
}

static bool valid_entrypoint(const uint8_t *entrypoint, uint16_t length)
{
    size_t index;
    if (length == 0U || length > LX_PROGRAMS_MAX_ENTRYPOINT_BYTES)
        return false;
    for (index = 0U; index < length; ++index) {
        const uint8_t byte = entrypoint[index];
        if (!((byte >= (uint8_t)'a' && byte <= (uint8_t)'z') ||
              (byte >= (uint8_t)'A' && byte <= (uint8_t)'Z') ||
              (byte >= (uint8_t)'0' && byte <= (uint8_t)'9') ||
              byte == (uint8_t)'_' || byte == (uint8_t)'.'))
            return false;
    }
    return true;
}

static lxp_result call_scalar_begin(const lxp_programs_call_activity *value,
                                    const lxp_authority_resolved *authority)
{
    const lxp_call_admission_facts *admission = lxp_ctx_call_admission(value->ctx);
    uint64_t program[4];
    uint64_t principal[4];
    uint64_t payment_account[4];
    uint64_t authority_hash[4];
    uint64_t binding[4];
    size_t index;
    if (admission == NULL || admission->fee_schedule_version == 0U ||
        !lxp_program_metering_schedule_available(
            admission->metering_schedule_version) ||
        admission->parameter_version == 0U)
        return LXP_FATAL_INVARIANT;
    for (index = 0U; index < 4U; ++index) {
        program[index] = read_u64(value->program_id + index * 8U);
        principal[index] = read_u64(value->authority->principal + index * 8U);
        payment_account[index] = read_u64(admission->payer + index * 8U);
        authority_hash[index] = read_u64(authority->authority_hash + index * 8U);
        binding[index] = read_u64(admission->activity_binding + index * 8U);
    }
    return layerx_programs_call_begin(
        (uint64_t)(uintptr_t)value,
        (uint64_t)(uintptr_t)value->occupancy,
        program[0], program[1], program[2], program[3],
        principal[0], principal[1], principal[2], principal[3],
        payment_account[0], payment_account[1], payment_account[2],
        payment_account[3],
        authority_hash[0], authority_hash[1], authority_hash[2], authority_hash[3],
        binding[0], binding[1], binding[2], binding[3],
        admission->signed_fee_limit.hi, admission->signed_fee_limit.lo,
        admission->available_fee_units.hi, admission->available_fee_units.lo,
        admission->fee_schedule_version, admission->metering_schedule_version,
        admission->parameter_version,
        admission->metering_schedule_coefficients[0],
        admission->metering_schedule_coefficients[1],
        admission->metering_schedule_coefficients[2],
        admission->metering_schedule_coefficients[3],
        admission->metering_schedule_coefficients[4],
        admission->metering_schedule_coefficients[5],
        admission->metering_schedule_coefficients[6],
        admission->metering_schedule_coefficients[7],
        admission->metering_schedule_coefficients[8],
        admission->fee_schedule_prices[0], admission->fee_schedule_prices[1],
        admission->fee_schedule_prices[2], admission->fee_schedule_prices[3],
        admission->fee_schedule_prices[4], admission->fee_schedule_prices[5],
        admission->fee_schedule_prices[6], lxp_ctx_batch_number(value->ctx),
        lxp_ctx_global_sequence(value->ctx),
        value->ctx->protocol_version,
        value->abi_version, value->entrypoint_length, value->wasm_length,
        value->calldata_length,
        value->capabilities_length, value->access_declaration_length,
        value->response_capacity,
        value->budget[0], value->budget[1], value->budget[2], value->budget[3],
        value->budget[4], value->budget[5], value->budget[6]);
}

lxp_result layerx_programs_call_activity_byte(uint64_t token, uint16_t section,
                                              uint32_t offset)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *bytes;
    uint32_t length;
    size_t blob_length;
    if (value == NULL || token == 0U) return LXP_ERR_NON_CANONICAL;
    switch (section) {
    case LX_PROGRAMS_ACTIVITY_BYTES_WASM:
        {
            lxp_result status = lxp_programs_artifact_open(
                value->ctx, value->program_id, value->code_hash, &bytes,
                &blob_length);
            if (status != LXP_OK) return status;
            if (blob_length > UINT32_MAX) return LXP_ERR_LENGTH_LIMIT;
            length = (uint32_t)blob_length;
        }
        break;
    case LX_PROGRAMS_ACTIVITY_BYTES_ENTRYPOINT:
        bytes = value->entrypoint;
        length = value->entrypoint_length;
        break;
    case LX_PROGRAMS_ACTIVITY_BYTES_CALLDATA:
        bytes = value->calldata;
        length = value->calldata_length;
        break;
    case LX_PROGRAMS_ACTIVITY_BYTES_CAPABILITIES:
        bytes = value->capabilities;
        length = value->capabilities_length;
        break;
    case LX_PROGRAMS_ACTIVITY_BYTES_ACCESS_DECLARATION:
        bytes = value->access_declaration;
        length = value->access_declaration_length;
        break;
    default:
        return LXP_ERR_UNKNOWN_FIELD;
    }
    if (offset >= length) return LXP_ERR_TRUNCATED;
    return (lxp_result)bytes[offset];
}

lxp_result layerx_programs_call_transfer_begin(uint64_t token,
                                               uint64_t program_spend_token,
                                               uint16_t leg_count)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    void *allocation;
    lxp_result status;
    if (value == NULL || value->ctx == NULL || value->transfer_set != NULL ||
        leg_count == 0U ||
        leg_count > LXP_MAX_TRANSFER_SET_LEGS)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(value->ctx, sizeof(*value->transfer_set),
                                 _Alignof(lxp_transfer_set), &allocation);
    if (status != LXP_OK) return status;
    value->transfer_set = (lxp_transfer_set *)allocation;
    (void)memset(value->transfer_set, 0, sizeof(*value->transfer_set));
    status = lxp_ctx_arena_alloc(
        value->ctx, (size_t)leg_count * sizeof(*value->transfer_sources),
        _Alignof(lxp_programs_call_transfer_source), &allocation);
    if (status != LXP_OK) return status;
    value->transfer_sources = (lxp_programs_call_transfer_source *)allocation;
    (void)memset(value->transfer_sources, 0,
                 (size_t)leg_count * sizeof(*value->transfer_sources));
    status = lxp_ctx_arena_alloc(
        value->ctx,
        (size_t)leg_count * sizeof(*value->transfer_source_authorities),
        _Alignof(lxp_transfer_source_authority), &allocation);
    if (status != LXP_OK) return status;
    value->transfer_source_authorities =
        (lxp_transfer_source_authority *)allocation;
    (void)memset(value->transfer_source_authorities, 0,
                 (size_t)leg_count *
                     sizeof(*value->transfer_source_authorities));
    (void)memset(value->transfer_leg_written, 0,
                 sizeof(value->transfer_leg_written));
    value->transfer_leg_count = 0U;
    value->transfer_applied = false;
    value->transfer_program_spend_token = program_spend_token;
    value->transfer_set->leg_count = leg_count;
    (void)memset(&value->transfer_receipt, 0, sizeof(value->transfer_receipt));
    return LXP_OK;
}

lxp_result layerx_programs_call_payment_name_byte(
    uint64_t token, uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3,
    uint32_t offset)
{
    const lxp_programs_call_activity *value = (const void *)(uintptr_t)token;
    lx_programs_transfer_runtime *runtime;
    lx_account *account;
    uint8_t asset[32];
    lxp_result status;
    if (value == NULL || value->ctx == NULL || value->authority == NULL)
        return LXP_ERR_NON_CANONICAL;
    runtime = lxp_ctx_module_runtime(value->ctx);
    if (runtime == NULL) return LXP_ERR_MODULE_DISABLED;
    write_u64(asset, a0); write_u64(asset + 8U, a1);
    write_u64(asset + 16U, a2); write_u64(asset + 24U, a3);
    status = lxp_kernel_program_payment_account(runtime->accounts,
        value->authority->principal, asset, value->ctx->protocol_version, &account);
    if (status != LXP_OK) return status;
    if (offset == UINT32_MAX) return (lxp_result)account->name_length;
    if (offset >= account->name_length) return LXP_ERR_TRUNCATED;
    return (lxp_result)account->name[offset];
}

lxp_result layerx_programs_call_transfer_leg(
    uint64_t token, uint16_t index, uint8_t source_kind,
    uint64_t f0, uint64_t f1, uint64_t f2, uint64_t f3,
    uint64_t o0, uint64_t o1, uint64_t o2, uint64_t o3,
    uint64_t p0, uint64_t p1, uint64_t p2, uint64_t p3,
    uint64_t frame_path, uint8_t frame_depth, uint16_t seed_length,
    uint64_t t0, uint64_t t1, uint64_t t2, uint64_t t3,
    uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3,
    uint64_t amount_hi, uint64_t amount_lo)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lx_programs_transfer_runtime *runtime;
    lxp_transfer_leg *leg;
    lxp_programs_call_transfer_source *source;
    void *allocation;
    lxp_result status;
    uint8_t from[32];
    uint8_t to[32];
    if (value == NULL || value->ctx == NULL || value->authority == NULL ||
        value->transfer_set == NULL || value->transfer_sources == NULL ||
        value->transfer_source_authorities == NULL || value->transfer_applied ||
        index >= value->transfer_set->leg_count || value->transfer_leg_written[index])
        return LXP_ERR_NON_CANONICAL;
    if ((source_kind != PROGRAM_TRANSFER_SOURCE_PRINCIPAL &&
         source_kind != PROGRAM_TRANSFER_SOURCE_PROGRAM &&
         source_kind != PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING) ||
        frame_depth > 8U || seed_length > 128U)
        return LXP_ERR_NON_CANONICAL;
    runtime = (lx_programs_transfer_runtime *)lxp_ctx_module_runtime(value->ctx);
    if (runtime == NULL || runtime->accounts == NULL || runtime->assets == NULL)
        return LXP_ERR_MODULE_DISABLED;
    write_u64(from, f0);
    write_u64(from + 8U, f1);
    write_u64(from + 16U, f2);
    write_u64(from + 24U, f3);
    write_u64(to, t0);
    write_u64(to + 8U, t1);
    write_u64(to + 16U, t2);
    write_u64(to + 24U, t3);
    leg = &value->transfer_set->legs[index];
    source = &value->transfer_sources[index];
    leg->from = account_by_id(runtime->accounts, from);
    leg->to = account_by_id(runtime->accounts, to);
    write_u64(leg->asset_id, a0);
    write_u64(leg->asset_id + 8U, a1);
    write_u64(leg->asset_id + 16U, a2);
    write_u64(leg->asset_id + 24U, a3);
    if (leg->to == NULL) {
        uint8_t fee_account[32];
        bool created;
        status = web_fee_account_id(leg->asset_id, fee_account);
        if (status != LXP_OK) return status;
        if (lxp_ct_memcmp(fee_account, to, 32U) == 0) {
            status = lxp_ctx_account_stage_module_value(
                value->ctx, fee_account, leg->asset_id, &leg->to, &created);
            if (status != LXP_OK) return status;
        }
    }
    if (source_kind == PROGRAM_TRANSFER_SOURCE_PRINCIPAL ||
        source_kind == PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING) {
        if (memcmp(from, value->authority->principal, 32U) != 0)
            return LXP_ERR_AUTH_SCOPE;
        status = lxp_kernel_program_payment_account(runtime->accounts, from,
            leg->asset_id, value->ctx->protocol_version, &leg->from);
        if (status != LXP_OK) return status;
    }
    if (leg->from == NULL || leg->to == NULL) return LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE;
    leg->amount = (lxp_u128){amount_hi, amount_lo};
    leg->reason = LXP_REASON_PAYMENT;
    leg->supply_mode = LXP_TRANSFER_CONSERVED;
    source->kind = source_kind;
    write_u64(source->owner_program, o0);
    write_u64(source->owner_program + 8U, o1);
    write_u64(source->owner_program + 16U, o2);
    write_u64(source->owner_program + 24U, o3);
    write_u64(source->staging_program, p0);
    write_u64(source->staging_program + 8U, p1);
    write_u64(source->staging_program + 16U, p2);
    write_u64(source->staging_program + 24U, p3);
    write_u64(source->frame_path, frame_path);
    source->frame_depth = frame_depth;
    source->seed_length = seed_length;
    if (seed_length != 0U) {
        status = lxp_ctx_arena_alloc(value->ctx, seed_length, 1U, &allocation);
        if (status != LXP_OK) return status;
        source->seed = (uint8_t *)allocation;
        (void)memset(source->seed, 0, seed_length);
    }
    value->transfer_leg_written[index] = 1U;
    value->transfer_leg_count += 1U;
    return LXP_OK;
}

lxp_result layerx_programs_call_transfer_seed_byte(
    uint64_t token, uint16_t index, uint16_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lxp_programs_call_transfer_source *source;
    if (value == NULL || value->transfer_set == NULL ||
        value->transfer_sources == NULL || value->transfer_applied ||
        index >= value->transfer_set->leg_count ||
        !value->transfer_leg_written[index])
        return LXP_ERR_NON_CANONICAL;
    source = &value->transfer_sources[index];
    if ((source->kind != PROGRAM_TRANSFER_SOURCE_PROGRAM &&
         source->kind != PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING) ||
        source->seed == NULL || offset != source->seed_written ||
        offset >= source->seed_length)
        return LXP_ERR_NON_CANONICAL;
    source->seed[offset] = byte;
    source->seed_written += 1U;
    return LXP_OK;
}

static bool transfer_frame_canonical(
    const lxp_programs_call_transfer_source *source)
{
    size_t index;
    if (source == NULL || source->frame_depth > sizeof(source->frame_path))
        return false;
    for (index = 0U; index < sizeof(source->frame_path); ++index) {
        if (index < source->frame_depth && source->frame_path[index] == 0U)
            return false;
        if (index >= source->frame_depth && source->frame_path[index] != 0U)
            return false;
    }
    return true;
}

static bool transfer_catalog_contains(
    const lxp_programs_call_activity *value, const uint8_t program_id[32])
{
    uint32_t index;
    if (value == NULL || value->catalog == NULL || program_id == NULL)
        return false;
    for (index = 0U; index < value->catalog_count; ++index)
        if (lxp_ct_memcmp(value->catalog[index].program_id,
                          program_id, 32U) == 0)
            return true;
    return false;
}

static lxp_result transfer_source_validate(
    const lxp_programs_call_activity *value, uint16_t index)
{
    const lxp_programs_call_transfer_source *source;
    const lxp_transfer_leg *leg;
    lx_programs_account_binding binding;
    lx_account *bound_account;
    lxp_result status;
    if (value == NULL || value->authority == NULL ||
        value->transfer_set == NULL || value->transfer_sources == NULL ||
        index >= value->transfer_set->leg_count)
        return LXP_ERR_NON_CANONICAL;
    source = &value->transfer_sources[index];
    leg = &value->transfer_set->legs[index];
    if (leg->from == NULL || !transfer_frame_canonical(source) ||
        lxp_ct_is_zero(source->staging_program, 32U) ||
        !transfer_catalog_contains(value, source->staging_program))
        return LXP_ERR_AUTH_SCOPE;
    if (source->kind == PROGRAM_TRANSFER_SOURCE_PRINCIPAL ||
        source->kind == PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING) {
        lx_programs_transfer_runtime *runtime = lxp_ctx_module_runtime(value->ctx);
        lx_account *expected;
        if (runtime == NULL) return LXP_ERR_MODULE_DISABLED;
        status = lxp_kernel_program_payment_account(runtime->accounts,
            value->authority->principal, leg->asset_id,
            value->ctx->protocol_version, &expected);
        if (status != LXP_OK) return status;
        if (expected != leg->from) return LXP_ERR_AUTH_SCOPE;
    }
    if (source->kind == PROGRAM_TRANSFER_SOURCE_PRINCIPAL) {
        if (!lxp_ct_is_zero(source->owner_program, 32U) ||
            source->seed != NULL || source->seed_length != 0U ||
            source->seed_written != 0U)
            return LXP_ERR_AUTH_SCOPE;
        return LXP_OK;
    }
    if (source->kind == PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING ||
        source->kind == PROGRAM_TRANSFER_SOURCE_PROGRAM) {
        uint16_t account_abi;
        status = lxp_programs_program_abi(
            value->ctx, source->owner_program, &account_abi);
        if (status == LXP_OK)
            status = lxp_programs_account_guest_validate(
                value->ctx, source->owner_program, account_abi);
        if (status != LXP_OK) return status;
    }
    if (source->kind == PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING) {
        if ((value->abi_version != LX_PROGRAMS_GUEST_ABI_V2_VERSION &&
             value->abi_version != LX_PROGRAMS_GUEST_ABI_V3_VERSION &&
             value->abi_version != LX_PROGRAMS_GUEST_ABI_V4_VERSION) ||
            lxp_ct_is_zero(source->owner_program, 32U) ||
            lxp_ct_memcmp(source->owner_program, source->staging_program, 32U) != 0 ||
            source->seed_written != source->seed_length ||
            (source->seed_length != 0U && source->seed == NULL))
            return LXP_ERR_AUTH_SCOPE;
        status = lxp_programs_account_lookup(value->ctx, source->owner_program,
                                             source->seed, source->seed_length,
                                             &binding, &bound_account);
        if (status != LXP_OK || bound_account == NULL || bound_account != leg->to ||
            lxp_ct_memcmp(binding.program_id, source->owner_program, 32U) != 0 ||
            binding.seed_length != source->seed_length ||
            (source->seed_length != 0U &&
             memcmp(binding.seed, source->seed, source->seed_length) != 0) ||
            lxp_ct_memcmp(binding.account_id, leg->to->id, 32U) != 0 ||
            lxp_ct_memcmp(binding.asset_id, leg->asset_id, 32U) != 0 ||
            leg->to->kind != LX_ACCOUNT_MODULE_VALUE || !leg->to->has_asset ||
            leg->to->has_authority_key ||
            lxp_ct_memcmp(leg->to->asset_id, binding.asset_id, 32U) != 0)
            return LXP_ERR_AUTH_SCOPE;
        return LXP_OK;
    }
    if (source->kind != PROGRAM_TRANSFER_SOURCE_PROGRAM ||
        (value->abi_version != LX_PROGRAMS_GUEST_ABI_V2_VERSION &&
         value->abi_version != LX_PROGRAMS_GUEST_ABI_V3_VERSION &&
         value->abi_version != LX_PROGRAMS_GUEST_ABI_V4_VERSION) ||
        lxp_ct_is_zero(source->owner_program, 32U) ||
        lxp_ct_memcmp(source->owner_program, source->staging_program, 32U) != 0 ||
        source->seed_written != source->seed_length ||
        (source->seed_length != 0U && source->seed == NULL))
        return LXP_ERR_AUTH_SCOPE;
    status = lxp_programs_account_lookup(
        value->ctx, source->owner_program, source->seed,
        source->seed_length, &binding, &bound_account);
    if (status != LXP_OK || bound_account == NULL || bound_account != leg->from ||
        lxp_ct_memcmp(binding.program_id, source->owner_program, 32U) != 0 ||
        binding.seed_length != source->seed_length ||
        (source->seed_length != 0U &&
         memcmp(binding.seed, source->seed, source->seed_length) != 0) ||
        lxp_ct_memcmp(binding.account_id, leg->from->id, 32U) != 0 ||
        lxp_ct_memcmp(binding.asset_id, leg->asset_id, 32U) != 0 ||
        leg->from->kind != LX_ACCOUNT_MODULE_VALUE ||
        !leg->from->has_asset || leg->from->has_authority_key ||
        lxp_ct_memcmp(leg->from->asset_id, binding.asset_id, 32U) != 0)
        return LXP_ERR_AUTH_SCOPE;
    return LXP_OK;
}

static lxp_result transfer_source_authority_add(
    lxp_programs_call_activity *value, const lxp_transfer_leg *leg,
    uint8_t source_kind, size_t *authority_count)
{
    size_t index;
    lxp_transfer_source_authority *authority;
    lxp_authorization_kind kind =
        source_kind == PROGRAM_TRANSFER_SOURCE_PROGRAM ?
            LXP_AUTH_PROGRAM_SPEND : LXP_AUTH_OWNER;
    if (value == NULL || leg == NULL || leg->from == NULL ||
        authority_count == NULL || value->transfer_source_authorities == NULL)
        return LXP_ERR_NON_CANONICAL;
    for (index = 0U; index < *authority_count; ++index)
        if (lxp_ct_memcmp(
                value->transfer_source_authorities[index].authorized_from,
                leg->from->id, 32U) == 0)
            return value->transfer_source_authorities[index]
                               .debit_authority_kind == kind ?
                       LXP_OK : LXP_ERR_AUTH_SCOPE;
    if (*authority_count >= value->transfer_set->leg_count)
        return LXP_ERR_LENGTH_LIMIT;
    authority = &value->transfer_source_authorities[*authority_count];
    (void)memcpy(authority->authorized_from, leg->from->id, 32U);
    authority->debit_authority_kind = kind;
    authority->protocol_system_capability = false;
    *authority_count += 1U;
    return LXP_OK;
}

lxp_result layerx_programs_call_transfer_apply(uint64_t token)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)(uintptr_t)token;
    lx_programs_transfer_runtime *runtime;
    lxp_transfer_set *set;
    lx_account *sequence_account;
    lx_account *payment_account = NULL;
    size_t authority_count = 0U;
    size_t program_spend_legs = 0U;
    size_t index;
    if (value == NULL || value->ctx == NULL || value->authority == NULL ||
        value->transfer_set == NULL || value->transfer_sources == NULL ||
        value->transfer_source_authorities == NULL || value->transfer_applied ||
        value->transfer_set->leg_count == 0U ||
        value->transfer_leg_count != value->transfer_set->leg_count)
        return LXP_ERR_NON_CANONICAL;
    runtime = (lx_programs_transfer_runtime *)lxp_ctx_module_runtime(value->ctx);
    if (runtime == NULL || runtime->accounts == NULL || runtime->assets == NULL)
        return LXP_ERR_MODULE_DISABLED;
    set = value->transfer_set;
    sequence_account = account_by_id(runtime->accounts,
                                     value->ctx->ledger_admission.bound ?
                                         value->ctx->ledger_admission.account_id :
                                         value->authority->principal);
    if (sequence_account == NULL) return LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE;
    for (index = 0U; index < set->leg_count; ++index) {
        lxp_result status = transfer_source_validate(value, (uint16_t)index);
        if (status == LXP_OK)
            status = transfer_source_authority_add(
                value, &set->legs[index], value->transfer_sources[index].kind,
                &authority_count);
        if (status != LXP_OK) return status;
        if (value->transfer_sources[index].kind ==
            PROGRAM_TRANSFER_SOURCE_PROGRAM)
            ++program_spend_legs;
    }
    /* A program-owned debit is authorized only by the Programs transfer law,
     * which issues the one-shot token the kernel redeems leg by leg. A token
     * without such a leg, or such a leg without a token, is refused here
     * rather than reaching the ledger. */
    if ((program_spend_legs == 0U) !=
        (value->transfer_program_spend_token == 0U))
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    set->context.assets = runtime->assets;
    set->context.asset_count = runtime->asset_count;
    (void)memcpy(set->context.authorized_from, sequence_account->id, 32U);
    set->context.actor_sequence = lxp_ctx_global_sequence(value->ctx);
    {
        lxp_result status = lxp_ctx_ledger_execution_sequence(
            value->ctx, sequence_account->id,
            set->context.actor_sequence, &set->context.actor_sequence);
        if (status != LXP_OK) return status;
    }
    if (value->ctx->ledger_admission.bound) {
        for (index = 0U; index < set->leg_count; ++index) {
            if (value->transfer_sources[index].kind != PROGRAM_TRANSFER_SOURCE_PRINCIPAL &&
                value->transfer_sources[index].kind != PROGRAM_TRANSFER_SOURCE_PROGRAM_FUNDING)
                continue;
            if (payment_account != NULL && payment_account != set->legs[index].from)
                return LXP_ERR_ASSET_MISMATCH;
            payment_account = set->legs[index].from;
        }
        if (payment_account != NULL) {
            sequence_account = payment_account;
            set->context.actor_sequence = payment_account->next_sequence;
            (void)memcpy(set->context.authorized_from, payment_account->id, 32U);
        }
    }
    set->context.batch_timestamp = lxp_ctx_batch_timestamp_ms(value->ctx);
    set->context.sequence_account = sequence_account;
    set->context.debit_authority_kind = LXP_AUTH_OWNER;
    set->context.program_spend_token = value->transfer_program_spend_token;
    set->context.source_authorities = value->transfer_source_authorities;
    set->context.source_authority_count = authority_count;
    {
        lxp_result status = lxp_ctx_emit_transfer_set(value->ctx, set,
                                                       &value->transfer_receipt);
        if (status == LXP_OK) value->transfer_applied = true;
        return status;
    }
}

lxp_result layerx_programs_call_transfer_root_byte(uint64_t token,
                                                    uint32_t offset)
{
    const lxp_programs_call_activity *value =
        (const lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || offset >= 32U ||
        lxp_ct_is_zero(value->transfer_receipt.transfer_set_root, 32U))
        return LXP_ERR_NON_CANONICAL;
    return (lxp_result)value->transfer_receipt.transfer_set_root[offset];
}

lxp_result lxp_programs_call_decode(lxp_module_ctx *ctx,
                                    const uint8_t *payload,
                                    size_t payload_length, void **decoded)
{
    lxp_programs_call_activity *value;
    size_t cursor;
    size_t expected;
    size_t index;
    const lxp_module_registration *registration;
    void *allocation;
    lxp_result status;
    if (ctx == NULL || payload == NULL || decoded == NULL ||
        payload_length < PROGRAM_CALL_FIXED_BYTES)
        return LXP_ERR_TRUNCATED;
    status = lxp_ctx_arena_alloc(ctx, sizeof(*value),
                                 _Alignof(lxp_programs_call_activity),
                                 &allocation);
    if (status != LXP_OK) return status;
    value = (lxp_programs_call_activity *)allocation;
    (void)memset(value, 0, sizeof(*value));
    value->ctx = ctx;
    (void)memcpy(value->program_id, payload, sizeof(value->program_id));
    cursor = 32U;
    value->abi_version = read_u16(payload + cursor);
    cursor += 2U;
    value->entrypoint_length = read_u16(payload + cursor);
    cursor += 2U;
    value->calldata_length = read_u32(payload + cursor);
    cursor += 4U;
    value->capabilities_length = read_u16(payload + cursor);
    cursor += 2U;
    value->access_declaration_length = read_u32(payload + cursor);
    cursor += 4U;
    value->response_capacity = read_u32(payload + cursor);
    cursor += 4U;
    for (index = 0U; index < LX_PROGRAMS_CALL_BUDGET_FIELDS; ++index) {
        value->budget[index] = read_u64(payload + cursor);
        cursor += 8U;
    }
    status = lxp_kernel_module_by_id(ctx->kernel, LXP_MODULE_PROGRAMS,
                                     ctx->epoch, &registration);
    if (status != LXP_OK) return status;
    if (lxp_ct_is_zero(value->program_id, sizeof(value->program_id)) ||
        value->abi_version == 0U ||
        value->calldata_length > LX_PROGRAMS_MAX_CALLDATA_BYTES ||
        value->access_declaration_length > LX_PROGRAMS_MAX_ACCESS_DECLARATION_BYTES ||
        value->response_capacity > LX_PROGRAMS_MAX_RESPONSE_BYTES)
        return LXP_ERR_NON_CANONICAL;
    if (value->abi_version > registration->abi_version ||
        value->abi_version > LX_PROGRAMS_GUEST_ABI_V4_VERSION ||
        (value->abi_version >= LX_PROGRAMS_GUEST_ABI_V2_VERSION &&
         !lxp_protocol_version_uses_occupancy(ctx->protocol_version)))
        return LXP_ERR_VERSION_UNSUPPORTED;
    if ((size_t)value->entrypoint_length > SIZE_MAX - cursor)
        return LXP_ERR_LENGTH_LIMIT;
    expected = cursor + (size_t)value->entrypoint_length;
    if ((size_t)value->calldata_length > SIZE_MAX - expected)
        return LXP_ERR_LENGTH_LIMIT;
    expected += (size_t)value->calldata_length;
    if ((size_t)value->capabilities_length > SIZE_MAX - expected)
        return LXP_ERR_LENGTH_LIMIT;
    expected += (size_t)value->capabilities_length;
    if ((size_t)value->access_declaration_length > SIZE_MAX - expected)
        return LXP_ERR_LENGTH_LIMIT;
    expected += (size_t)value->access_declaration_length;
    if (expected != payload_length ||
        !valid_entrypoint(payload + cursor, value->entrypoint_length))
        return LXP_ERR_NON_CANONICAL;
    value->entrypoint = payload + cursor;
    cursor += value->entrypoint_length;
    value->calldata = payload + cursor;
    cursor += value->calldata_length;
    value->capabilities = payload + cursor;
    cursor += value->capabilities_length;
    value->access_declaration = payload + cursor;
    *decoded = value;
    return LXP_OK;
}

lxp_result lxp_programs_call_validate(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lxp_authority_resolved *authority, const void *decoded)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)decoded;
    uint8_t key[PROGRAM_KEY_BYTES];
    const uint8_t *record;
    const uint8_t *wasm;
    size_t record_length;
    size_t wasm_length;
    lxp_result status;
    if (ctx == NULL || activity == NULL || authority == NULL || value == NULL ||
        activity->activity_type != LX_PROGRAMS_CALL ||
        lxp_ct_is_zero(authority->principal, sizeof(authority->principal)) ||
        lxp_ct_is_zero(authority->authority_hash, sizeof(authority->authority_hash)))
        return LXP_ERR_NON_CANONICAL;
    value->authority = authority;
    value->network_id = activity->network_id;
    program_key(value->program_id, key);
    status = lxp_ctx_kv_get(ctx, key, sizeof(key), &record, &record_length);
    if (status != LXP_OK) return status;
    if (record_length != PROGRAM_RECORD_BYTES) return LXP_FATAL_INVARIANT;
    if (read_u16(record + 65U) != value->abi_version) return LXP_ERR_VERSION_UNSUPPORTED;
    status = lxp_programs_program_active(ctx, value->program_id);
    if (status != LXP_OK) return status;
    (void)memcpy(value->code_hash, record + 33U, sizeof(value->code_hash));
    status = lxp_programs_artifact_open(ctx, value->program_id, value->code_hash,
                                        &wasm, &wasm_length);
    if (status != LXP_OK) return status;
    if (wasm_length > UINT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    value->wasm_length = (uint32_t)wasm_length;
    return lxp_ctx_charge_gas(ctx, (uint64_t)PROGRAM_CALL_FIXED_BYTES +
                              value->entrypoint_length + value->calldata_length +
                              value->capabilities_length +
                              value->access_declaration_length);
}

lxp_result lxp_programs_call_schedule_decode(
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    const lxp_call_admission_facts *admission, const void *decoded,
    lxp_programs_call_schedule_descriptor *descriptor)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)decoded;
    uintptr_t payload_begin;
    uintptr_t payload_end;
    uintptr_t capabilities_begin;
    uintptr_t declaration_begin;
    size_t capabilities_offset;
    const lx_programs_transfer_runtime *runtime;
    lx_account *treasury = NULL;
    size_t owner_index;
    lxp_result status;
    if (activity == NULL || authority == NULL || admission == NULL ||
        value == NULL || descriptor == NULL || !admission->present ||
        activity->activity_type != LX_PROGRAMS_CALL ||
        activity->payload.bytes == NULL ||
        lxp_ct_is_zero(admission->activity_binding, 32U) ||
        lxp_ct_is_zero(authority->principal, 32U) ||
        lxp_ct_is_zero(admission->payer, 32U))
        return LXP_ERR_NON_CANONICAL;
    status = lxp_activity_verify_payload_hash(activity);
    if (status != LXP_OK) return status;
    if (value->catalog == NULL) {
        status = call_catalog_build(value);
        if (status != LXP_OK) return status;
    }
    payload_begin = (uintptr_t)activity->payload.bytes;
    capabilities_begin = (uintptr_t)value->capabilities;
    declaration_begin = (uintptr_t)value->access_declaration;
    if (activity->payload.length > UINTPTR_MAX - payload_begin)
        return LXP_ERR_LENGTH_LIMIT;
    payload_end = payload_begin + activity->payload.length;
    capabilities_offset = PROGRAM_CALL_FIXED_BYTES + value->replay_prefix_length;
    if (!checked_size_add(capabilities_offset, value->entrypoint_length,
                          &capabilities_offset) ||
        !checked_size_add(capabilities_offset, value->calldata_length,
                          &capabilities_offset))
        return LXP_ERR_LENGTH_LIMIT;
    if (capabilities_begin < payload_begin ||
        declaration_begin < capabilities_begin ||
        capabilities_begin > payload_end || declaration_begin > payload_end ||
        value->capabilities_length > (size_t)(payload_end - capabilities_begin) ||
        value->access_declaration_length >
            (size_t)(payload_end - declaration_begin) ||
        value->capabilities_length > declaration_begin - capabilities_begin ||
        capabilities_begin - payload_begin != capabilities_offset ||
        declaration_begin - capabilities_begin != value->capabilities_length ||
        payload_end - declaration_begin != value->access_declaration_length)
        return LXP_FATAL_INVARIANT;
    (void)memset(descriptor, 0, sizeof(*descriptor));
    descriptor->canonical_payload = activity->payload;
    descriptor->capabilities.bytes = value->capabilities;
    descriptor->capabilities.length = value->capabilities_length;
    descriptor->access_declaration.bytes = value->access_declaration;
    descriptor->access_declaration.length = value->access_declaration_length;
    if (value->catalog_count <= LXP_PROGRAMS_SCHEDULE_MAX_OWNERS) {
        descriptor->owner_catalog_complete = 1U;
        descriptor->owner_count = (uint16_t)value->catalog_count;
        for (owner_index = 0U; owner_index < value->catalog_count;
             ++owner_index) {
            (void)memcpy(descriptor->owners[owner_index].program_id,
                         value->catalog[owner_index].program_id, 32U);
            (void)memcpy(descriptor->owners[owner_index].owner,
                         value->catalog[owner_index].owner, 32U);
        }
    }
    runtime = (const lx_programs_transfer_runtime *)
        lxp_ctx_module_runtime(value->ctx);
    if (runtime != NULL && runtime->accounts != NULL &&
        lxp_fee_treasury_account(runtime->accounts, &treasury) == LXP_OK &&
        treasury != NULL)
        (void)memcpy(descriptor->fee_treasury, treasury->id, 32U);
    (void)memcpy(descriptor->activity_binding, admission->activity_binding, 32U);
    (void)memcpy(descriptor->program_id, value->program_id, 32U);
    (void)memcpy(descriptor->principal, authority->principal, 32U);
    (void)memcpy(descriptor->payer, admission->payer, 32U);
    return LXP_OK;
}

lxp_result lxp_programs_call_schedule_item_prepare(
    const lxp_programs_call_schedule_descriptor *descriptor,
    const uint8_t identity_actor[32],
    const uint8_t fee_asset[32], const uint8_t occupancy_asset[32],
    bool occupancy_active, bool effects_complete,
    lxp_programs_schedule_item *item)
{
    if (descriptor == NULL || identity_actor == NULL || fee_asset == NULL ||
        occupancy_asset == NULL ||
        item == NULL || lxp_ct_is_zero(descriptor->principal, 32U) ||
        lxp_ct_is_zero(identity_actor, 32U) ||
        lxp_ct_is_zero(descriptor->payer, 32U))
        return LXP_ERR_NON_CANONICAL;
    (void)memset(item, 0, sizeof(*item));
    item->version = LXP_PROGRAMS_SCHEDULE_ITEM_VERSION;
    item->call = *descriptor;
    (void)memcpy(item->identity_actor, identity_actor, 32U);
    (void)memcpy(item->identity_principal, descriptor->principal, 32U);
    if (!effects_complete) return LXP_OK;
    if (lxp_ct_is_zero(fee_asset, 32U)) return LXP_ERR_NON_CANONICAL;
    if (lxp_ct_is_zero(descriptor->fee_treasury, 32U)) return LXP_OK;
    item->protocol_effects_complete = 1U;
    item->account_effect_count = 1U;
    (void)memcpy(item->account_effects[0].account, descriptor->payer, 32U);
    (void)memcpy(item->account_effects[0].asset, fee_asset, 32U);
    item->account_effects[0].mode = 1U;
    if (occupancy_active) {
        if (lxp_ct_is_zero(occupancy_asset, 32U))
            return LXP_ERR_NON_CANONICAL;
        (void)memcpy(item->occupancy_asset, occupancy_asset, 32U);
        (void)memcpy(item->occupancy_treasury,
                     descriptor->fee_treasury, 32U);
        item->account_effect_count = 2U;
        (void)memcpy(item->account_effects[1].account,
                     descriptor->fee_treasury, 32U);
        (void)memcpy(item->account_effects[1].asset,
                     occupancy_asset, 32U);
        item->account_effects[1].mode = 1U;
    }
    return LXP_OK;
}

lxp_result lxp_programs_call_execute(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lxp_authority_resolved *authority, const void *decoded,
    lxp_effect_buffer *effects)
{
    lxp_programs_call_activity *value =
        (lxp_programs_call_activity *)decoded;
    lxp_result status;
    void *allocation;
    if (ctx == NULL || activity == NULL || authority == NULL || value == NULL ||
        activity->activity_type != LX_PROGRAMS_CALL)
        return LXP_ERR_NON_CANONICAL;
    value->authority = authority;
    value->effects = effects;
    value->network_id = activity->network_id;
    status = lxp_ctx_bind_activity_state(ctx, value, call_activity_release);
    if (status != LXP_OK) return status;
    if (value->catalog == NULL) {
        status = call_catalog_build(value);
        if (status != LXP_OK) return status;
    }
    status = lxp_ctx_arena_alloc(ctx, sizeof(*value->occupancy),
                                 _Alignof(lxp_programs_occupancy_bridge),
                                 &allocation);
    if (status != LXP_OK) return status;
    value->occupancy = (lxp_programs_occupancy_bridge *)allocation;
    status = lxp_programs_occupancy_bridge_init(value->occupancy, ctx);
    if (status == LXP_OK &&
        lxp_protocol_version_uses_occupancy(ctx->protocol_version))
        status = lxp_programs_occupancy_bind_call(
            value->occupancy, value->program_id, authority->principal, value->budget);
    if (status != LXP_OK) return status;
    /* The Rust boundary consumes this exact arena-owned activity once. It must
     * publish into the existing C journal before reporting success. */
    status = replay_capture_authority(value, activity, authority);
    if (status != LXP_OK) return status;
    status = call_scalar_begin(value, authority);
    return status;
}


static lxp_result replay_append(uint8_t **bytes, uint32_t *length,
    uint32_t maximum, const void *data, size_t count)
{
    uint8_t *next;
    if (count > maximum || *length > maximum - count)
        return LXP_ERR_LENGTH_LIMIT;
    if (count == 0U) return LXP_OK;
    if (data == NULL) return LXP_ERR_NON_CANONICAL;
    next = realloc(*bytes, (size_t)*length + count);
    if (next == NULL) return LXP_ERR_LENGTH_LIMIT;
    (void)memcpy(next + *length, data, count);
    *bytes = next;
    *length += (uint32_t)count;
    return LXP_OK;
}

static void replay_write_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U); bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U); bytes[3] = (uint8_t)value;
}

static lxp_result replay_host_fact(lxp_programs_call_activity *value, uint8_t tag,
    const uint8_t *first, size_t first_length, const uint8_t *second, size_t second_length)
{
    uint8_t header[9];
    lxp_result status;
    if (value->replay.max_bytes == 0U) return LXP_OK;
    if (first_length > UINT32_MAX || second_length > UINT32_MAX - first_length)
        return LXP_ERR_LENGTH_LIMIT;
    header[0] = tag;
    replay_write_u32(header + 1U, 0U);
    replay_write_u32(header + 5U, (uint32_t)(first_length + second_length));
    status = replay_append(&value->replay_hosts, &value->replay_hosts_length,
        value->replay.max_bytes, header, sizeof(header));
    if (status == LXP_OK) status = replay_append(&value->replay_hosts,
        &value->replay_hosts_length, value->replay.max_bytes, first, first_length);
    if (status == LXP_OK) status = replay_append(&value->replay_hosts,
        &value->replay_hosts_length, value->replay.max_bytes, second, second_length);
    return status;
}

static lxp_result replay_host_failure(lxp_programs_call_activity *value, uint8_t tag,
    lxp_result checked_status, const uint8_t *input, size_t input_length)
{
    uint8_t header[9];
    lxp_result status;
    if (value->replay.max_bytes == 0U) return checked_status;
    if (checked_status == LXP_OK || input_length > UINT32_MAX)
        return LXP_ERR_NON_CANONICAL;
    header[0] = tag;
    replay_write_u32(header + 1U, (uint32_t)checked_status);
    replay_write_u32(header + 5U, (uint32_t)input_length);
    status = replay_append(&value->replay_hosts, &value->replay_hosts_length,
        value->replay.max_bytes, header, sizeof(header));
    if (status == LXP_OK) status = replay_append(&value->replay_hosts,
        &value->replay_hosts_length, value->replay.max_bytes, input, input_length);
    return status == LXP_OK ? checked_status : status;
}

static lxp_result replay_capture_authority(lxp_programs_call_activity *value,
    const lxp_activity *activity, const lxp_authority_resolved *authority)
{
    static const uint8_t domain[] = "LXP/program-replay-authority/v1";
    static const uint8_t hosts_domain[] = "LXP/program-replay-hosts/v1";
    const lxp_call_admission_facts *admission;
    const lxp_authority_scope *scope = authority->scope;
    lxp_byte_span encoded;
    uint8_t scalar[8];
    lxp_result status;
    size_t index;
    if (value->replay.max_bytes == 0U) return LXP_OK;
    admission = lxp_ctx_call_admission(value->ctx);
    if (admission == NULL || !admission->present) return LXP_FATAL_INVARIANT;
    if (value->replay_authority != NULL) return LXP_ERR_DUPLICATE_ENTRY;
    value->replay.network_id = activity->network_id;
    value->replay.sequence = value->ctx->global_sequence;
    value->replay.abi_version = value->abi_version;
    value->replay.fee_version = admission->fee_schedule_version;
    value->replay.metering_version = admission->metering_schedule_version;
    (void)memcpy(value->replay.activity_id, value->ctx->activity_id, 32U);
    (void)memcpy(value->replay.previous_root, value->ctx->kernel->current_state_root, 32U);
    (void)memcpy(value->replay.program_id, value->program_id, 32U);
    (void)memcpy(value->replay.code_hash, value->code_hash, 32U);
    status = lxp_activity_encode(activity, value->ctx->arena, &encoded);
    if (status != LXP_OK || encoded.length > UINT32_MAX)
        return status == LXP_OK ? LXP_ERR_LENGTH_LIMIT : status;
#define REPLAY_AUTH_BYTES(data, size) do { \
    status = replay_append(&value->replay_authority, &value->replay_authority_length, \
        value->replay.max_bytes, (data), (size)); \
    if (status != LXP_OK) return status; \
} while (0)
#define REPLAY_AUTH_U64(number) do { write_u64(scalar, (uint64_t)(number)); REPLAY_AUTH_BYTES(scalar, 8U); } while (0)
#define REPLAY_AUTH_U128(number) do { REPLAY_AUTH_U64((number).hi); REPLAY_AUTH_U64((number).lo); } while (0)
    REPLAY_AUTH_BYTES(domain, sizeof(domain));
    replay_write_u32(scalar, (uint32_t)encoded.length);
    REPLAY_AUTH_BYTES(scalar, 4U); REPLAY_AUTH_BYTES(encoded.bytes, encoded.length);
    REPLAY_AUTH_BYTES(authority->actor, 32U); REPLAY_AUTH_BYTES(authority->principal, 32U);
    REPLAY_AUTH_U64(authority->kind); REPLAY_AUTH_BYTES(authority->verified_key, 32U);
    REPLAY_AUTH_BYTES(authority->authority_hash, 32U); REPLAY_AUTH_BYTES(authority->grant_id, 32U);
    REPLAY_AUTH_U64(scope != NULL);
    if (scope != NULL) {
        if (scope->signer_count > LXP_AUTHORITY_MULTISIG_MAX_SIGNERS ||
            scope->approval_count > LXP_AUTHORITY_MULTISIG_MAX_SIGNERS)
            return LXP_ERR_NON_CANONICAL;
        REPLAY_AUTH_U64(scope->module_mask); REPLAY_AUTH_U64(scope->activity_ordinal_min);
        REPLAY_AUTH_U64(scope->activity_ordinal_max); REPLAY_AUTH_BYTES(scope->asset_id, 32U);
        REPLAY_AUTH_U128(scope->maximum_per_activity); REPLAY_AUTH_U128(scope->maximum_total);
        REPLAY_AUTH_U128(scope->spent_total); REPLAY_AUTH_U64(scope->period_length);
        REPLAY_AUTH_U128(scope->maximum_per_period); REPLAY_AUTH_U128(scope->spent_this_period);
        REPLAY_AUTH_U64(scope->period_start); REPLAY_AUTH_BYTES(scope->purpose_hash, 32U);
        REPLAY_AUTH_U64(scope->earliest_sequence); REPLAY_AUTH_U64(scope->earliest_timestamp);
        REPLAY_AUTH_U64(scope->signer_threshold); REPLAY_AUTH_U64(scope->signer_count);
        REPLAY_AUTH_BYTES(scope->signers, (size_t)scope->signer_count * 32U);
        REPLAY_AUTH_U64(scope->approval_count);
        REPLAY_AUTH_BYTES(scope->approvals, (size_t)scope->approval_count * 32U);
    }
    REPLAY_AUTH_BYTES(admission->activity_binding, 32U); REPLAY_AUTH_BYTES(admission->payer, 32U);
    REPLAY_AUTH_U128(admission->available_fee_units); REPLAY_AUTH_U128(admission->signed_fee_limit);
    REPLAY_AUTH_U64(admission->fee_schedule_version); REPLAY_AUTH_U64(admission->metering_schedule_version);
    REPLAY_AUTH_U64(admission->parameter_version);
    for (index = 0U; index < 9U; ++index) REPLAY_AUTH_U64(admission->metering_schedule_coefficients[index]);
    for (index = 0U; index < 7U; ++index) REPLAY_AUTH_U64(admission->fee_schedule_prices[index]);
    REPLAY_AUTH_U64(value->ctx->protocol_version); REPLAY_AUTH_U64(value->ctx->batch_number);
    REPLAY_AUTH_U64(value->ctx->epoch);
#undef REPLAY_AUTH_U128
#undef REPLAY_AUTH_U64
#undef REPLAY_AUTH_BYTES
    status = replay_append(&value->replay_hosts, &value->replay_hosts_length,
        value->replay.max_bytes, hosts_domain, sizeof(hosts_domain));
    return status;
}

lxp_result lxp_programs_call_profile_decode(lxp_module_ctx *ctx,
    const uint8_t *payload, size_t payload_length, void **decoded)
{
    static const uint8_t domain[] = "LXP/program-replay-profile/v1";
    const size_t prefix = 34U + sizeof(domain) + 2U + 12U;
    size_t cursor;
    uint32_t maximum_boundaries, maximum_bytes, original_length;
    lxp_programs_call_activity *value;
    lxp_result status;
    if (payload == NULL || payload_length < 34U || !lxp_ct_is_zero(payload, 34U))
        return lxp_programs_call_decode(ctx, payload, payload_length, decoded);
    if (payload_length < prefix) return LXP_ERR_TRUNCATED;
    if (lxp_ct_memcmp(payload + 34U, domain, sizeof(domain)) != 0)
        return LXP_ERR_NON_CANONICAL;
    cursor = 34U + sizeof(domain);
    if (read_u16(payload + cursor) != 1U) return LXP_ERR_VERSION_UNSUPPORTED;
    cursor += 2U;
    maximum_boundaries = read_u32(payload + cursor); cursor += 4U;
    maximum_bytes = read_u32(payload + cursor); cursor += 4U;
    original_length = read_u32(payload + cursor); cursor += 4U;
    if (maximum_boundaries == 0U || maximum_boundaries > LXP_PROGRAMS_REPLAY_MAX_BOUNDARIES ||
        maximum_bytes < 512U || maximum_bytes > LXP_PROGRAMS_REPLAY_MAX_BYTES ||
        original_length != payload_length - cursor)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_programs_call_decode(ctx, payload + cursor, original_length, decoded);
    if (status != LXP_OK) return status;
    value = *decoded;
    value->replay_prefix_length = (uint32_t)cursor;
    value->replay.max_boundaries = maximum_boundaries;
    value->replay.max_bytes = maximum_bytes;
    return LXP_OK;
}

lxp_result layerx_programs_call_replay_profile(uint64_t token, uint16_t section)
{
    const lxp_programs_call_activity *value = (const lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL) return LXP_ERR_NON_CANONICAL;
    if (section == 0U) return value->replay.max_bytes != 0U ? 1 : 0;
    if (section == 1U) return (lxp_result)value->replay.max_boundaries;
    if (section == 2U) return (lxp_result)value->replay.max_bytes;
    return LXP_ERR_UNKNOWN_FIELD;
}

lxp_result layerx_programs_call_replay_record_begin(uint64_t token, uint32_t length)
{
    lxp_programs_call_activity *value = (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->replay.max_bytes == 0U || length == 0U ||
        length > value->replay.max_bytes || value->replay_runtime != NULL || value->replay_finished)
        return LXP_ERR_NON_CANONICAL;
    value->replay_runtime = malloc(length);
    if (value->replay_runtime == NULL) return LXP_ERR_LENGTH_LIMIT;
    value->replay_runtime_length = length;
    return LXP_OK;
}

lxp_result layerx_programs_call_replay_record_byte(uint64_t token, uint32_t offset, uint8_t byte)
{
    lxp_programs_call_activity *value = (lxp_programs_call_activity *)(uintptr_t)token;
    if (value == NULL || value->replay_runtime == NULL || value->replay_finished ||
        offset >= value->replay_runtime_length || offset != value->replay_runtime_written)
        return LXP_ERR_NON_CANONICAL;
    value->replay_runtime[offset] = byte;
    ++value->replay_runtime_written;
    return LXP_OK;
}

static lxp_result replay_boundary_root(const uint8_t *bytes, size_t length,
    uint32_t expected_count, uint8_t root[32])
{
    static const uint8_t witness_domain[] = "LXP/program-replay-witness/v1";
    static const uint8_t leaf_domain[] = "LXP/program-replay-leaf/v1";
    static const uint8_t node_domain[] = "LXP/program-replay-node/v1";
    uint8_t *hashes;
    size_t cursor, index, count;
    lxp_result status = LXP_OK;
    if (length < sizeof(witness_domain) + 4U || expected_count == 0U ||
        expected_count > LXP_PROGRAMS_REPLAY_MAX_BOUNDARIES ||
        lxp_ct_memcmp(bytes, witness_domain, sizeof(witness_domain)) != 0)
        return LXP_ERR_NON_CANONICAL;
    cursor = sizeof(witness_domain);
    count = read_u32(bytes + cursor); cursor += 4U;
    if (count != expected_count) return LXP_ERR_NON_CANONICAL;
    hashes = malloc(count * 32U);
    if (hashes == NULL) return LXP_ERR_LENGTH_LIMIT;
    for (index = 0U; index < count; ++index) {
        lxp_hash_context hash;
        uint8_t fields[8];
        uint32_t leaf_length;
        if (length - cursor < 4U) { status = LXP_ERR_TRUNCATED; break; }
        leaf_length = read_u32(bytes + cursor); cursor += 4U;
        if (leaf_length == 0U || leaf_length > length - cursor) { status = LXP_ERR_TRUNCATED; break; }
        replay_write_u32(fields, (uint32_t)index); replay_write_u32(fields + 4U, leaf_length);
        lxp_hash_init(&hash);
        status = lxp_hash_update(&hash, leaf_domain, sizeof(leaf_domain));
        if (status == LXP_OK) status = lxp_hash_update(&hash, fields, sizeof(fields));
        if (status == LXP_OK) status = lxp_hash_update(&hash, bytes + cursor, leaf_length);
        if (status == LXP_OK) status = lxp_hash_final(&hash, hashes + index * 32U);
        if (status != LXP_OK) break;
        cursor += leaf_length;
    }
    if (status == LXP_OK && cursor != length) status = LXP_ERR_NON_CANONICAL;
    while (status == LXP_OK && count > 1U) {
        size_t next_count = (count + 1U) / 2U;
        for (index = 0U; index < next_count; ++index) {
            lxp_hash_context hash;
            size_t right = index * 2U + 1U < count ? index * 2U + 1U : index * 2U;
            uint8_t children[64];
            (void)memcpy(children, hashes + index * 2U * 32U, 32U);
            (void)memcpy(children + 32U, hashes + right * 32U, 32U);
            lxp_hash_init(&hash);
            status = lxp_hash_update(&hash, node_domain, sizeof(node_domain));
            if (status == LXP_OK) status = lxp_hash_update(&hash, children, sizeof(children));
            if (status == LXP_OK) status = lxp_hash_final(&hash, hashes + index * 32U);
            if (status != LXP_OK) break;
        }
        count = next_count;
    }
    if (status == LXP_OK) (void)memcpy(root, hashes, 32U);
    free(hashes);
    return status;
}

lxp_result layerx_programs_call_replay_record_finish(uint64_t token)
{
    static const uint8_t domain[] = "LXP/program-replay-record/v1";
    static const uint8_t native_domain[] = "LXP/program-replay-native-blob/v1";
    lxp_programs_call_activity *value = (lxp_programs_call_activity *)(uintptr_t)token;
    const uint8_t *bytes;
    uint8_t digest[32], root[32], length_bytes[4];
    size_t cursor, fixed = sizeof(domain) + 2U + 64U + 4U + 8U + 8U + 1U + 4U + 64U + 4U;
    uint32_t witness_length;
    lxp_result status;
    if (value == NULL || value->replay_runtime == NULL || value->replay_finished ||
        value->replay_runtime_written != value->replay_runtime_length ||
        value->replay_runtime_length < fixed || value->replay_authority == NULL || value->replay_hosts == NULL)
        return LXP_ERR_NON_CANONICAL;
    bytes = value->replay_runtime;
    if (lxp_ct_memcmp(bytes, domain, sizeof(domain)) != 0) return LXP_ERR_NON_CANONICAL;
    cursor = sizeof(domain);
    if (read_u16(bytes + cursor) != 1U) return LXP_ERR_VERSION_UNSUPPORTED;
    cursor += 2U;
    if (lxp_ct_memcmp(bytes + cursor, value->code_hash, 32U) != 0) return LXP_ERR_ROOT_MISMATCH;
    (void)memcpy(value->replay.input_digest, bytes + cursor + 32U, 32U);
    cursor += 64U;
    value->replay.runtime_version = read_u16(bytes + cursor); cursor += 2U;
    if (value->replay.runtime_version == 0U || read_u16(bytes + cursor) != value->replay.abi_version)
        return LXP_ERR_VERSION_UNSUPPORTED;
    cursor += 2U;
    if (read_u32(bytes + cursor) != value->replay.fee_version ||
        read_u32(bytes + cursor + 4U) != value->replay.metering_version)
        return LXP_ERR_VERSION_UNSUPPORTED;
    cursor += 8U;
    if (read_u32(bytes + cursor) != value->replay.max_boundaries ||
        read_u32(bytes + cursor + 4U) != value->replay.max_bytes)
        return LXP_ERR_NON_CANONICAL;
    cursor += 8U;
    value->replay.terminal_status = bytes[cursor++];
    if (value->replay.terminal_status > 2U) return LXP_ERR_NON_CANONICAL;
    value->replay.boundary_count = read_u32(bytes + cursor); cursor += 4U;
    if (value->replay.boundary_count == 0U || value->replay.boundary_count > value->replay.max_boundaries)
        return LXP_ERR_LENGTH_LIMIT;
    (void)memcpy(value->replay.boundary_root, bytes + cursor, 32U); cursor += 32U;
    (void)memcpy(value->replay.witness_digest, bytes + cursor, 32U); cursor += 32U;
    witness_length = read_u32(bytes + cursor); cursor += 4U;
    if (witness_length != value->replay_runtime_length - cursor) return LXP_ERR_NON_CANONICAL;
    status = lxp_hash_sha256(bytes + cursor, witness_length, digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, value->replay.witness_digest, 32U) != 0) return LXP_ERR_ROOT_MISMATCH;
    status = replay_boundary_root(bytes + cursor, witness_length, value->replay.boundary_count, root);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(root, value->replay.boundary_root, 32U) != 0) return LXP_ERR_ROOT_MISMATCH;
    status = lxp_hash_sha256(value->replay_authority, value->replay_authority_length, value->replay.authority_root);
    if (status == LXP_OK) status = lxp_hash_sha256(value->replay_hosts, value->replay_hosts_length, value->replay.host_root);
#define REPLAY_BLOB(data, count) do { if (status == LXP_OK) status = replay_append(&value->replay.bytes, &value->replay.length, value->replay.max_bytes, (data), (count)); } while (0)
    REPLAY_BLOB(native_domain, sizeof(native_domain));
    replay_write_u32(length_bytes, value->replay_runtime_length); REPLAY_BLOB(length_bytes, 4U);
    REPLAY_BLOB(value->replay_runtime, value->replay_runtime_length);
    replay_write_u32(length_bytes, value->replay_authority_length); REPLAY_BLOB(length_bytes, 4U);
    REPLAY_BLOB(value->replay_authority, value->replay_authority_length);
    replay_write_u32(length_bytes, value->replay_hosts_length); REPLAY_BLOB(length_bytes, 4U);
    REPLAY_BLOB(value->replay_hosts, value->replay_hosts_length);
#undef REPLAY_BLOB
    if (status == LXP_OK) value->replay_finished = true;
    return status;
}

lxp_result lxp_programs_call_replay_take(lxp_module_ctx *ctx, lxp_programs_replay_capture **capture)
{
    lxp_programs_call_activity *value;
    lxp_programs_replay_capture *owned;
    if (ctx == NULL || capture == NULL) return LXP_ERR_NON_CANONICAL;
    *capture = NULL;
    if (ctx->module_id != LXP_MODULE_PROGRAMS || ctx->activity_state == NULL ||
        ctx->activity_state_release != call_activity_release) return LXP_OK;
    value = ctx->activity_state;
    if (value->replay.max_bytes == 0U) return LXP_OK;
    if (!value->replay_finished || value->replay.bytes == NULL) return LXP_ERR_NON_CANONICAL;
    owned = malloc(sizeof(*owned));
    if (owned == NULL) return LXP_ERR_LENGTH_LIMIT;
    *owned = value->replay;
    value->replay.bytes = NULL;
    value->replay.length = 0U;
    *capture = owned;
    return LXP_OK;
}

void lxp_programs_replay_capture_release(lxp_programs_replay_capture *capture)
{
    if (capture == NULL) return;
    free(capture->bytes);
    free(capture->metadata_proof);
    free(capture);
}

lxp_result lxp_programs_replay_capture_bytes(const lxp_programs_replay_capture *capture, lxp_byte_span *bytes)
{
    if (capture == NULL || bytes == NULL || capture->bytes == NULL || capture->length == 0U)
        return LXP_ERR_NON_CANONICAL;
    bytes->bytes = capture->bytes;
    bytes->length = capture->length;
    return LXP_OK;
}
