#include "layerx/program.h"
#include "host.h"

static inline lxp_program_status lxp_program_v_span(const void *pointer, size_t length)
{
    uintptr_t address = (uintptr_t)pointer;
    if (length != 0U && pointer == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    if (length > (size_t)INT32_MAX || address > (uintptr_t)INT32_MAX ||
        length > (size_t)((uintptr_t)INT32_MAX - address)) return LXP_PROGRAM_ERR_BOUNDS;
    return LXP_PROGRAM_OK;
}
static inline int32_t lxp_program_v_pointer(const void *pointer)
{
    return (int32_t)(uintptr_t)pointer;
}
static inline lxp_program_status lxp_program_v_exact(int32_t status, int32_t expected)
{
    return status < 0 ? status : status == expected ? LXP_PROGRAM_OK : LXP_PROGRAM_ERR_INVALID;
}
lxp_program_status lxp_program_response_write(int32_t code, const uint8_t *bytes, size_t length)
{
    lxp_program_status status;
    if (code < 0) return LXP_PROGRAM_ERR_INVALID;
    if (length > LXP_PROGRAM_MAX_CALL_RESPONSE_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    status = lxp_program_v_span(bytes, length);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_response_write(code, lxp_program_v_pointer(bytes), (int32_t)length), 0);
}
lxp_program_status lxp_program_refusal_write(lxp_program_refusal_class class_code, const uint8_t *reason, size_t length)
{
    lxp_program_status status;
    if (class_code < LXP_PROGRAM_REFUSAL_REJECTED || class_code > LXP_PROGRAM_REFUSAL_NOT_FOUND)
        return LXP_PROGRAM_ERR_INVALID;
    if (length > LXP_PROGRAM_MAX_REFUSAL_REASON_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    status = lxp_program_v_span(reason, length);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_refusal_write((int32_t)class_code,
        length == 0U ? 0 : lxp_program_v_pointer(reason), (int32_t)length), 0);
}
lxp_program_status lxp_program_call_response(lxp_program_id callee,
    const uint8_t *input, size_t input_length, const uint8_t *capabilities,
    size_t capabilities_length, uint8_t *output, size_t capacity, int32_t *code, size_t *length)
{
    lxp_program_status status;
    int64_t result;
    uint64_t packed;
    if (code == NULL || length == NULL || capabilities == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *code = 0; *length = 0U;
    if (lxp_program_bytes32_is_zero(callee.bytes)) return LXP_PROGRAM_ERR_RESERVED_IDENTIFIER;
    if (input_length > LXP_PROGRAM_MAX_CALL_INPUT_BYTES || capacity > LXP_PROGRAM_MAX_CALL_RESPONSE_BYTES ||
        capabilities_length > LXP_PROGRAM_MAX_CAPABILITY_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    if (capabilities_length < 2U) return LXP_PROGRAM_ERR_INVALID;
    status = lxp_program_v_span(input, input_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(capabilities, capabilities_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(output, capacity);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(callee.bytes, 32U);
    if (status != LXP_PROGRAM_OK) return status;
    result = lxp_program_host_program_call_response(lxp_program_v_pointer(callee.bytes), 32,
        lxp_program_v_pointer(input), (int32_t)input_length, lxp_program_v_pointer(capabilities),
        (int32_t)capabilities_length, capacity == 0U ? 0 : lxp_program_v_pointer(output), (int32_t)capacity);
    if (result < 0) return result < INT32_MIN ? LXP_PROGRAM_ERR_INVALID : (int32_t)result;
    packed = (uint64_t)result;
    if ((packed >> 32U) > (uint64_t)INT32_MAX || (uint32_t)packed > capacity)
        return LXP_PROGRAM_ERR_INVALID;
    *code = (int32_t)(packed >> 32U); *length = (size_t)(uint32_t)packed;
    return LXP_PROGRAM_OK;
}
static inline lxp_program_status lxp_program_v_scope(lxp_program_storage_scope scope)
{
    return scope == LXP_PROGRAM_STORAGE_PRINCIPAL || scope == LXP_PROGRAM_STORAGE_SHARED ?
        LXP_PROGRAM_OK : LXP_PROGRAM_ERR_INVALID;
}
static inline lxp_program_status lxp_program_v_key(const uint8_t *key, size_t length)
{
    if (length == 0U) return LXP_PROGRAM_ERR_EMPTY_KEY;
    if (length > LXP_PROGRAM_MAX_STORAGE_KEY_BYTES) return LXP_PROGRAM_ERR_KEY_TOO_LARGE;
    return lxp_program_v_span(key, length);
}
lxp_program_status lxp_program_storage_read_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length, uint8_t *out, size_t capacity, size_t *length, bool *found)
{
    lxp_program_status status;
    int32_t result;
    if (length == NULL || found == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *length = 0U; *found = false;
    if (capacity > LXP_PROGRAM_MAX_STORAGE_VALUE_BYTES) return LXP_PROGRAM_ERR_VALUE_TOO_LARGE;
    status = lxp_program_v_scope(scope);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_key(key, key_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(out, capacity);
    if (status != LXP_PROGRAM_OK) return status;
    result = lxp_program_host_storage_read_scoped((int32_t)scope, lxp_program_v_pointer(key),
        (int32_t)key_length, lxp_program_v_pointer(out), (int32_t)capacity);
    if (result < 0) return result;
    if (result == 0) return LXP_PROGRAM_OK;
    if ((size_t)(uint32_t)(result - 1) > capacity) return LXP_PROGRAM_ERR_INVALID;
    *length = (size_t)(uint32_t)(result - 1); *found = true;
    return LXP_PROGRAM_OK;
}
lxp_program_status lxp_program_storage_write_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length, const uint8_t *value, size_t value_length)
{
    lxp_program_status status = lxp_program_v_scope(scope);
    if (value_length > LXP_PROGRAM_MAX_STORAGE_VALUE_BYTES) return LXP_PROGRAM_ERR_VALUE_TOO_LARGE;
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_key(key, key_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(value, value_length);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_storage_write_scoped((int32_t)scope,
        lxp_program_v_pointer(key), (int32_t)key_length, lxp_program_v_pointer(value), (int32_t)value_length), 0);
}
lxp_program_status lxp_program_storage_delete_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length)
{
    lxp_program_status status = lxp_program_v_scope(scope);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_key(key, key_length);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_storage_delete_scoped((int32_t)scope,
        lxp_program_v_pointer(key), (int32_t)key_length), 0);
}
lxp_program_status lxp_program_storage_drop_scoped(lxp_program_storage_scope scope)
{
    lxp_program_status status = lxp_program_v_scope(scope);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_storage_drop_scoped((int32_t)scope), 0);
}
static inline bool lxp_program_v_take(size_t *offset, size_t length, size_t count)
{
    if (*offset > length || count > length - *offset) return false;
    *offset += count;
    return true;
}
lxp_program_status lxp_program_storage_scan_scoped(lxp_program_storage_scope scope,
    const uint8_t *prefix, size_t prefix_length, const uint8_t *cursor, size_t cursor_length,
    uint32_t max_entries, uint32_t max_bytes, uint8_t *output, size_t capacity, lxp_program_scan_page *page)
{
    lxp_program_status status = lxp_program_v_scope(scope);
    int32_t result;
    size_t offset = 0U, previous_length = 0U, index;
    const uint8_t *previous = NULL;
    uint16_t count, continuation;
    uint8_t present;
    if (page == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *page = (lxp_program_scan_page){0};
    if (prefix_length > LXP_PROGRAM_MAX_STORAGE_KEY_BYTES || cursor_length > LXP_PROGRAM_MAX_SCAN_CURSOR_BYTES ||
        max_entries == 0U || max_entries > LXP_PROGRAM_MAX_SCAN_ENTRIES || max_bytes < 5U ||
        max_bytes > LXP_PROGRAM_MAX_SCAN_BYTES || capacity > LXP_PROGRAM_MAX_SCAN_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(prefix, prefix_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(cursor, cursor_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(output, capacity);
    if (status != LXP_PROGRAM_OK) return status;
    result = lxp_program_host_storage_scan_scoped((int32_t)scope, lxp_program_v_pointer(prefix),
        (int32_t)prefix_length, lxp_program_v_pointer(cursor), (int32_t)cursor_length, (int32_t)max_entries,
        (int32_t)max_bytes, lxp_program_v_pointer(output), (int32_t)capacity);
    if (result < 0) return result;
    if ((size_t)(uint32_t)result > capacity || (uint32_t)result > max_bytes || result < 5) return LXP_PROGRAM_ERR_INVALID;
    capacity = (size_t)(uint32_t)result;
    if (!lxp_program_v_take(&offset, capacity, 2U)) return LXP_PROGRAM_ERR_INVALID;
    count = lxp_program_read_u16_be(output);
    if (count > max_entries) return LXP_PROGRAM_ERR_INVALID;
    for (index = 0U; index < count; ++index) {
        uint16_t key_length;
        uint32_t value_length;
        const uint8_t *key;
        if (!lxp_program_v_take(&offset, capacity, 2U)) return LXP_PROGRAM_ERR_INVALID;
        key_length = lxp_program_read_u16_be(output + offset - 2U);
        key = output + offset;
        if (key_length == 0U || key_length > LXP_PROGRAM_MAX_STORAGE_KEY_BYTES ||
            key_length < prefix_length || !lxp_program_v_take(&offset, capacity, key_length)) return LXP_PROGRAM_ERR_INVALID;
        if (prefix_length != 0U && !lxp_program_bytes_equal(key, prefix, prefix_length)) return LXP_PROGRAM_ERR_INVALID;
        if (previous != NULL) {
            size_t common = previous_length < key_length ? previous_length : key_length;
            int order = lxp_program_bytes_compare(previous, key, common);
            if (order > 0 || (order == 0 && previous_length >= key_length)) return LXP_PROGRAM_ERR_INVALID;
        }
        previous = key; previous_length = key_length;
        if (!lxp_program_v_take(&offset, capacity, 4U)) return LXP_PROGRAM_ERR_INVALID;
        value_length = lxp_program_read_u32_be(output + offset - 4U);
        if (value_length > LXP_PROGRAM_MAX_STORAGE_VALUE_BYTES ||
            !lxp_program_v_take(&offset, capacity, value_length)) return LXP_PROGRAM_ERR_INVALID;
    }
    if (!lxp_program_v_take(&offset, capacity, 1U)) return LXP_PROGRAM_ERR_INVALID;
    present = output[offset - 1U];
    if (!lxp_program_v_take(&offset, capacity, 2U)) return LXP_PROGRAM_ERR_INVALID;
    continuation = lxp_program_read_u16_be(output + offset - 2U);
    if (present > 1U || continuation > LXP_PROGRAM_MAX_SCAN_CURSOR_BYTES ||
        (present == 0U && continuation != 0U) || (present == 1U && continuation == 0U)) return LXP_PROGRAM_ERR_INVALID;
    page->cursor = output + offset;
    if (!lxp_program_v_take(&offset, capacity, continuation) || offset != capacity) {
        *page = (lxp_program_scan_page){0}; return LXP_PROGRAM_ERR_INVALID;
    }
    page->encoded = output; page->length = capacity; page->count = count; page->cursor_length = continuation;
    return LXP_PROGRAM_OK;
}
static inline lxp_program_status lxp_program_v_account_payment(lxp_program_amount amount,
    const uint8_t *seed, size_t seed_length, lxp_program_account account, lxp_program_asset asset)
{
    lxp_program_status status;
    if (lxp_program_amount_is_zero(amount)) return LXP_PROGRAM_ERR_ZERO_AMOUNT;
    if (lxp_program_bytes32_is_zero(account.bytes) || lxp_program_bytes32_is_zero(asset.bytes))
        return LXP_PROGRAM_ERR_RESERVED_IDENTIFIER;
    if (seed_length > LXP_PROGRAM_MAX_ACCOUNT_SEED_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    status = lxp_program_v_span(seed, seed_length);
    return status;
}
lxp_program_status lxp_program_transfer_program_402(lxp_program_amount amount,
    const uint8_t *seed, size_t seed_length, lxp_program_account source, lxp_program_asset asset, lxp_program_account recipient)
{
    lxp_program_status status = lxp_program_v_account_payment(amount, seed, seed_length, source, asset);
    if (lxp_program_bytes32_is_zero(recipient.bytes)) return LXP_PROGRAM_ERR_RESERVED_IDENTIFIER;
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(source.bytes, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(asset.bytes, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(recipient.bytes, 32U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_transfer_program_402((int64_t)amount.hi, (int64_t)amount.lo,
        lxp_program_v_pointer(seed), (int32_t)seed_length, lxp_program_v_pointer(source.bytes), 32,
        lxp_program_v_pointer(asset.bytes), 32, lxp_program_v_pointer(recipient.bytes), 32), 0);
}
lxp_program_status lxp_program_fund_program_402(lxp_program_amount amount,
    const uint8_t *seed, size_t seed_length, lxp_program_account destination, lxp_program_asset asset)
{
    lxp_program_status status = lxp_program_v_account_payment(amount, seed, seed_length, destination, asset);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(destination.bytes, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(asset.bytes, 32U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_fund_program_402((int64_t)amount.hi, (int64_t)amount.lo,
        lxp_program_v_pointer(seed), (int32_t)seed_length, lxp_program_v_pointer(destination.bytes), 32,
        lxp_program_v_pointer(asset.bytes), 32), 0);
}
lxp_program_status lxp_program_context_read(lxp_program_context_field field,
    uint8_t *output, size_t capacity, size_t *length)
{
    size_t expected;
    int32_t result;
    lxp_program_status status;
    if (length == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *length = 0U;
    switch (field) {
    case LXP_PROGRAM_CONTEXT_EXECUTING_PROGRAM: case LXP_PROGRAM_CONTEXT_INVOKING_PRINCIPAL: expected = 32U; break;
    case LXP_PROGRAM_CONTEXT_IMMEDIATE_CALLER: expected = 33U; break;
    case LXP_PROGRAM_CONTEXT_ACTIVITY_SEQUENCE: case LXP_PROGRAM_CONTEXT_BATCH_HEIGHT:
    case LXP_PROGRAM_CONTEXT_REMAINING_FUEL: expected = 8U; break;
    case LXP_PROGRAM_CONTEXT_RUNTIME_VERSION: case LXP_PROGRAM_CONTEXT_ABI_VERSION: expected = 2U; break;
    case LXP_PROGRAM_CONTEXT_FEE_SCHEDULE_VERSION: expected = 4U; break;
    default: return LXP_PROGRAM_ERR_INVALID;
    }
    if (capacity < expected || capacity > (size_t)INT32_MAX) return LXP_PROGRAM_ERR_BUFFER_TOO_SMALL;
    status = lxp_program_v_span(output, capacity);
    if (status != LXP_PROGRAM_OK) return status;
    result = lxp_program_host_context_read((int32_t)field, lxp_program_v_pointer(output), (int32_t)capacity);
    if (result < 0) return result;
    if (field == LXP_PROGRAM_CONTEXT_IMMEDIATE_CALLER) {
        if (!((result == 1 && output[0] == 0U) ||
            (result == 33 && output[0] == 1U && !lxp_program_bytes32_is_zero(output + 1U))))
            return LXP_PROGRAM_ERR_INVALID;
    } else if ((size_t)(uint32_t)result != expected) return LXP_PROGRAM_ERR_INVALID;
    if ((field == LXP_PROGRAM_CONTEXT_EXECUTING_PROGRAM || field == LXP_PROGRAM_CONTEXT_INVOKING_PRINCIPAL) &&
        lxp_program_bytes32_is_zero(output)) return LXP_PROGRAM_ERR_INVALID;
    *length = (size_t)(uint32_t)result;
    return LXP_PROGRAM_OK;
}
lxp_program_status lxp_program_balance_read(lxp_program_account account,
    lxp_program_asset asset, lxp_program_amount *amount)
{
    uint8_t encoded[16];
    lxp_program_status status;
    if (amount == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *amount = (lxp_program_amount){0U, 0U};
    if (lxp_program_bytes32_is_zero(account.bytes) || lxp_program_bytes32_is_zero(asset.bytes)) return LXP_PROGRAM_ERR_RESERVED_IDENTIFIER;
    status = lxp_program_v_span(account.bytes, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(asset.bytes, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(encoded, sizeof(encoded));
    if (status != LXP_PROGRAM_OK) return status;
    status = lxp_program_v_exact(lxp_program_host_balance_read(lxp_program_v_pointer(account.bytes), 32,
        lxp_program_v_pointer(asset.bytes), 32, lxp_program_v_pointer(encoded), 16), 16);
    if (status != LXP_PROGRAM_OK) return status;
    *amount = lxp_program_amount_from_be(encoded);
    return LXP_PROGRAM_OK;
}
lxp_program_status lxp_program_hash(lxp_program_hash_algorithm algorithm,
    const uint8_t *input, size_t length, lxp_program_digest *digest)
{
    lxp_program_status status;
    if (algorithm < LXP_PROGRAM_HASH_SHA256 || algorithm > LXP_PROGRAM_HASH_BLAKE3) return LXP_PROGRAM_ERR_INVALID;
    if (length > LXP_PROGRAM_MAX_HASH_INPUT_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    if (digest == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    status = lxp_program_v_span(input, length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(digest->bytes, 32U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_hash((int32_t)algorithm, lxp_program_v_pointer(input),
        (int32_t)length, lxp_program_v_pointer(digest->bytes)), 0);
}
lxp_program_status lxp_program_signature_verify(lxp_program_signature_algorithm algorithm,
    const uint8_t *message, size_t message_length, const uint8_t *key, size_t key_length, const uint8_t signature[64])
{
    lxp_program_status status;
    if (algorithm == LXP_PROGRAM_SIGNATURE_ED25519) {
        if (message_length > 64U || key_length != 32U) return LXP_PROGRAM_ERR_BOUNDS;
    } else if (algorithm == LXP_PROGRAM_SIGNATURE_SECP256K1) {
        if (message_length != 32U || (key_length != 33U && key_length != 65U)) return LXP_PROGRAM_ERR_BOUNDS;
    } else return LXP_PROGRAM_ERR_INVALID;
    status = lxp_program_v_span(message, message_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(key, key_length);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(signature, 64U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_signature_verify((int32_t)algorithm,
        lxp_program_v_pointer(message), (int32_t)message_length, lxp_program_v_pointer(key),
        (int32_t)key_length, lxp_program_v_pointer(signature), 64), 0);
}
lxp_program_status lxp_program_signature_recover(const uint8_t digest[32],
    const uint8_t signature[64], uint8_t recovery_id, uint8_t output[65])
{
    lxp_program_status status;
    if (recovery_id > 3U) return LXP_PROGRAM_ERR_INVALID;
    status = lxp_program_v_span(digest, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(signature, 64U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(output, 65U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_signature_recover(lxp_program_v_pointer(digest), 32,
        lxp_program_v_pointer(signature), 64, (int32_t)recovery_id, lxp_program_v_pointer(output), 65), 65);
}
static inline lxp_program_status lxp_program_v_bigint(const uint8_t left[32], const uint8_t right[32], uint8_t *output,
    size_t width, int32_t (*operation)(int32_t, int32_t, int32_t, int32_t, int32_t, int32_t))
{
    lxp_program_status status = lxp_program_v_span(left, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(right, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(output, width);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(operation(lxp_program_v_pointer(left), 32, lxp_program_v_pointer(right),
        32, lxp_program_v_pointer(output), (int32_t)width), (int32_t)width);
}
lxp_program_status lxp_program_bigint_mul_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[64])
{
    return lxp_program_v_bigint(left, right, output, 64U, lxp_program_host_bigint_mul_256);
}
lxp_program_status lxp_program_bigint_div_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[32])
{
    return lxp_program_v_bigint(left, right, output, 32U, lxp_program_host_bigint_div_256);
}
lxp_program_status lxp_program_bigint_rem_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[32])
{
    return lxp_program_v_bigint(left, right, output, 32U, lxp_program_host_bigint_rem_256);
}
lxp_program_status lxp_program_bigint_modexp_256(const uint8_t base[32], const uint8_t exponent[32],
    const uint8_t modulus[32], uint8_t output[32])
{
    lxp_program_status status = lxp_program_v_span(base, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(exponent, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(modulus, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(output, 32U);
    if (status != LXP_PROGRAM_OK) return status;
    return lxp_program_v_exact(lxp_program_host_bigint_modexp_256(lxp_program_v_pointer(base), 32,
        lxp_program_v_pointer(exponent), 32, lxp_program_v_pointer(modulus), 32, lxp_program_v_pointer(output), 32), 32);
}
static inline uint64_t lxp_program_v_read_u64_le(const uint8_t *bytes)
{
    uint64_t value = 0U;
    size_t index;
    for (index = 0U; index < 8U; ++index) value |= (uint64_t)bytes[index] << (index * 8U);
    return value;
}
static inline uint32_t lxp_program_v_read_u32_le(const uint8_t *bytes)
{
    return (uint32_t)bytes[0] | (uint32_t)bytes[1] << 8U | (uint32_t)bytes[2] << 16U | (uint32_t)bytes[3] << 24U;
}
lxp_program_status lxp_program_oracle_read(const uint8_t market[32], lxp_program_oracle_observation *observation)
{
    uint8_t record[LXP_PROGRAM_ORACLE_OBSERVATION_BYTES];
    lxp_program_status status;
    if (observation == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *observation = (lxp_program_oracle_observation){0};
    status = lxp_program_v_span(market, 32U);
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(record, sizeof(record));
    if (status != LXP_PROGRAM_OK) return status;
    status = lxp_program_v_exact(lxp_program_host_oracle_read(lxp_program_v_pointer(market), 32,
        lxp_program_v_pointer(record), LXP_PROGRAM_ORACLE_OBSERVATION_BYTES), LXP_PROGRAM_ORACLE_OBSERVATION_BYTES);
    if (status != LXP_PROGRAM_OK) return status;
    observation->price.lo = lxp_program_v_read_u64_le(record);
    observation->price.hi = lxp_program_v_read_u64_le(record + 8U);
    observation->observed_at = lxp_program_v_read_u64_le(record + 16U);
    observation->sequence = lxp_program_v_read_u64_le(record + 24U);
    lxp_program_copy(observation->source_set_digest.bytes, record + 32U, 32U);
    return LXP_PROGRAM_OK;
}
lxp_program_status lxp_program_web_read(uint64_t request_id, uint8_t *record, size_t capacity,
    lxp_program_web_answer *answer, bool *found)
{
    uint8_t request[8];
    size_t index;
    int32_t result;
    uint32_t full, returned;
    lxp_program_status status;
    if (answer == NULL || found == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *answer = (lxp_program_web_answer){0}; *found = false;
    if (capacity < LXP_PROGRAM_WEB_RECORD_BYTES) return LXP_PROGRAM_ERR_BUFFER_TOO_SMALL;
    if (capacity > (size_t)INT32_MAX) return LXP_PROGRAM_ERR_BOUNDS;
    for (index = 0U; index < 8U; ++index) request[index] = (uint8_t)(request_id >> (index * 8U));
    status = lxp_program_v_span(request, sizeof(request));
    if (status == LXP_PROGRAM_OK) status = lxp_program_v_span(record, capacity);
    if (status != LXP_PROGRAM_OK) return status;
    result = lxp_program_host_web_read(lxp_program_v_pointer(request), 8, lxp_program_v_pointer(record), (int32_t)capacity);
    if (result == LXP_PROGRAM_WEB_STATUS_ABSENT) return LXP_PROGRAM_OK;
    if (result < 0) return result;
    if (result < LXP_PROGRAM_WEB_HEADER_BYTES || (size_t)(uint32_t)result > capacity) return LXP_PROGRAM_ERR_INVALID;
    full = lxp_program_v_read_u32_le(record + 32U); returned = lxp_program_v_read_u32_le(record + 36U);
    if (returned > LXP_PROGRAM_WEB_MAX_RESPONSE_BYTES || returned > full ||
        returned != (uint32_t)result - LXP_PROGRAM_WEB_HEADER_BYTES) return LXP_PROGRAM_ERR_INVALID;
    lxp_program_copy(answer->content_digest.bytes, record, 32U);
    answer->full_length = full; answer->response = record + LXP_PROGRAM_WEB_HEADER_BYTES;
    answer->response_length = returned; *found = true;
    return LXP_PROGRAM_OK;
}

const uint8_t *lxp_program_abi_manifest_version(uint16_t version, size_t *length)
{
    static const char v2[] = "layerx_v1\0storage_read(i32,i32,i32,i32)->i32\0storage_write(i32,i32,i32,i32)->i32\0storage_delete(i32,i32)->i32\0event_emit(i32,i32,i32,i32)->i32\0program_call(i32,i32,i32,i32,i32,i32)->i32\0transfer_402(i64,i64,i32,i32,i32,i32)->i32\0receipt_read(i32,i32,i32,i32)->i32\0layerx_v2\0response_write(i32,i32,i32)->i32\0program_call_response(i32,i32,i32,i32,i32,i32,i32,i32)->i64\0refusal_write(i32,i32,i32)->i32\0storage_read_scoped(i32,i32,i32,i32,i32)->i32\0storage_write_scoped(i32,i32,i32,i32,i32)->i32\0storage_delete_scoped(i32,i32,i32)->i32\0storage_drop_scoped(i32)->i32\0storage_scan_scoped(i32,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0transfer_program_402(i64,i64,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0fund_program_402(i64,i64,i32,i32,i32,i32,i32,i32)->i32\0context_read(i32,i32,i32)->i32\0balance_read(i32,i32,i32,i32,i32,i32)->i32\0hash(i32,i32,i32,i32)->i32\0signature_verify(i32,i32,i32,i32,i32,i32,i32)->i32\0signature_recover(i32,i32,i32,i32,i32,i32,i32)->i32\0bigint_mul_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_div_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_rem_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_modexp_256(i32,i32,i32,i32,i32,i32,i32,i32)->i32\0";
    static const char v3[] = "layerx_v1\0storage_read(i32,i32,i32,i32)->i32\0storage_write(i32,i32,i32,i32)->i32\0storage_delete(i32,i32)->i32\0event_emit(i32,i32,i32,i32)->i32\0program_call(i32,i32,i32,i32,i32,i32)->i32\0transfer_402(i64,i64,i32,i32,i32,i32)->i32\0receipt_read(i32,i32,i32,i32)->i32\0layerx_v2\0response_write(i32,i32,i32)->i32\0program_call_response(i32,i32,i32,i32,i32,i32,i32,i32)->i64\0refusal_write(i32,i32,i32)->i32\0storage_read_scoped(i32,i32,i32,i32,i32)->i32\0storage_write_scoped(i32,i32,i32,i32,i32)->i32\0storage_delete_scoped(i32,i32,i32)->i32\0storage_drop_scoped(i32)->i32\0storage_scan_scoped(i32,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0transfer_program_402(i64,i64,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0fund_program_402(i64,i64,i32,i32,i32,i32,i32,i32)->i32\0context_read(i32,i32,i32)->i32\0balance_read(i32,i32,i32,i32,i32,i32)->i32\0hash(i32,i32,i32,i32)->i32\0signature_verify(i32,i32,i32,i32,i32,i32,i32)->i32\0signature_recover(i32,i32,i32,i32,i32,i32,i32)->i32\0bigint_mul_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_div_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_rem_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_modexp_256(i32,i32,i32,i32,i32,i32,i32,i32)->i32\0layerx_v3\0oracle_read(i32,i32,i32,i32)->i32\0";
    static const char v4[] = "layerx_v1\0storage_read(i32,i32,i32,i32)->i32\0storage_write(i32,i32,i32,i32)->i32\0storage_delete(i32,i32)->i32\0event_emit(i32,i32,i32,i32)->i32\0program_call(i32,i32,i32,i32,i32,i32)->i32\0transfer_402(i64,i64,i32,i32,i32,i32)->i32\0receipt_read(i32,i32,i32,i32)->i32\0layerx_v2\0response_write(i32,i32,i32)->i32\0program_call_response(i32,i32,i32,i32,i32,i32,i32,i32)->i64\0refusal_write(i32,i32,i32)->i32\0storage_read_scoped(i32,i32,i32,i32,i32)->i32\0storage_write_scoped(i32,i32,i32,i32,i32)->i32\0storage_delete_scoped(i32,i32,i32)->i32\0storage_drop_scoped(i32)->i32\0storage_scan_scoped(i32,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0transfer_program_402(i64,i64,i32,i32,i32,i32,i32,i32,i32,i32)->i32\0fund_program_402(i64,i64,i32,i32,i32,i32,i32,i32)->i32\0context_read(i32,i32,i32)->i32\0balance_read(i32,i32,i32,i32,i32,i32)->i32\0hash(i32,i32,i32,i32)->i32\0signature_verify(i32,i32,i32,i32,i32,i32,i32)->i32\0signature_recover(i32,i32,i32,i32,i32,i32,i32)->i32\0bigint_mul_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_div_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_rem_256(i32,i32,i32,i32,i32,i32)->i32\0bigint_modexp_256(i32,i32,i32,i32,i32,i32,i32,i32)->i32\0layerx_v3\0oracle_read(i32,i32,i32,i32)->i32\0layerx_v4\0web_read(i32,i32,i32,i32)->i32\0";
    if (length != NULL) *length = 0U;
    switch (version) {
    case 1U: return lxp_program_abi_manifest(length);
    case 2U: if (length != NULL) *length = sizeof(v2) - 1U; return (const uint8_t *)v2;
    case 3U: if (length != NULL) *length = sizeof(v3) - 1U; return (const uint8_t *)v3;
    case 4U: if (length != NULL) *length = sizeof(v4) - 1U; return (const uint8_t *)v4;
    default: return NULL;
    }
}

static uint8_t lxp_program_versioned_call_input[LXP_PROGRAM_MAX_CALL_INPUT_BYTES];
int32_t lxp_program_reserve_call_input_versioned(int32_t length)
{
    if (length < 0 || (uint32_t)length > LXP_PROGRAM_MAX_CALL_INPUT_BYTES ||
        lxp_program_v_span(lxp_program_versioned_call_input, (size_t)(uint32_t)length) != LXP_PROGRAM_OK)
        return LXP_PROGRAM_RESERVATION_REFUSED;
    return lxp_program_v_pointer(lxp_program_versioned_call_input);
}
lxp_program_status lxp_program_call_input_versioned(int32_t pointer, int32_t length,
    const uint8_t **out, size_t *out_length)
{
    if (out == NULL || out_length == NULL) return LXP_PROGRAM_ERR_NULL_ARGUMENT;
    *out = NULL; *out_length = 0U;
    if (length < 0 || (uint32_t)length > LXP_PROGRAM_MAX_CALL_INPUT_BYTES) return LXP_PROGRAM_ERR_BOUNDS;
    if (length != 0 && pointer != lxp_program_v_pointer(lxp_program_versioned_call_input)) return LXP_PROGRAM_ERR_INVALID;
    if (lxp_program_v_span(lxp_program_versioned_call_input, (size_t)(uint32_t)length) != LXP_PROGRAM_OK)
        return LXP_PROGRAM_ERR_BOUNDS;
    *out = lxp_program_versioned_call_input; *out_length = (size_t)(uint32_t)length;
    return LXP_PROGRAM_OK;
}
