#ifndef LAYERX_PROGRAM_H
#define LAYERX_PROGRAM_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define lxp_program_status int32_t

#define LXP_PROGRAM_ABI_MODULE "layerx_v1"
#define LXP_PROGRAM_ENTRYPOINT "layerx_main"
#define LXP_PROGRAM_CALL_ENTRY_EXPORT "layerx_call"
#define LXP_PROGRAM_CALL_RESERVE_EXPORT "layerx_reserve"
#define LXP_PROGRAM_MEMORY_EXPORT "memory"

#define LXP_PROGRAM_EXPORT(export_name_literal) \
    __attribute__((export_name(export_name_literal)))

enum {
    LXP_PROGRAM_ABI_VERSION = 1,
    LXP_PROGRAM_RUNTIME_VERSION = 1,
    LXP_PROGRAM_ID_BYTES = 32,
    LXP_PROGRAM_AMOUNT_BYTES = 16,
    LXP_PROGRAM_DIGEST_BYTES = 32,
    LXP_PROGRAM_RECEIPT_BYTES = 116,
    LXP_PROGRAM_MAX_STORAGE_KEY_BYTES = 256,
    LXP_PROGRAM_MAX_STORAGE_VALUE_BYTES = 1048576,
    LXP_PROGRAM_MAX_EVENT_TOPIC_BYTES = 64,
    LXP_PROGRAM_MAX_EVENT_DATA_BYTES = 65536,
    LXP_PROGRAM_MAX_EVENTS_PER_ACTIVITY = 64,
    LXP_PROGRAM_MAX_CALL_INPUT_BYTES = 1048576,
    LXP_PROGRAM_MAX_CAPABILITY_BYTES = 65535,
    LXP_PROGRAM_MAX_CAPABILITY_ENCODING_HEADER_BYTES = 2,
    LXP_PROGRAM_MAX_CAPABILITY_ENCODING_GRANT_BYTES = 275,
    LXP_PROGRAM_MAX_CAPABILITIES =
        (LXP_PROGRAM_MAX_CAPABILITY_BYTES -
         LXP_PROGRAM_MAX_CAPABILITY_ENCODING_HEADER_BYTES) /
        LXP_PROGRAM_MAX_CAPABILITY_ENCODING_GRANT_BYTES,
    LXP_PROGRAM_MAX_CANONICAL_CAPABILITY_SET_BYTES =
        LXP_PROGRAM_MAX_CAPABILITY_ENCODING_HEADER_BYTES +
        LXP_PROGRAM_MAX_CAPABILITIES *
        LXP_PROGRAM_MAX_CAPABILITY_ENCODING_GRANT_BYTES,
    LXP_PROGRAM_CALL_INPUT_CAPACITY = 8192,
    LXP_PROGRAM_RESERVATION_REFUSED = -1
};

/*
 * Status numbers cross the guest boundary inside canonical execution evidence
 * and are consensus data. The first band mirrors the host status codes exactly;
 * the second band is guest-side refusal the host never produces. Renumbering
 * any value is a protocol-version change, never a refactor.
 */
#define LXP_PROGRAM_STATUS_LIST(X) \
    X(LXP_PROGRAM_OK, 0) \
    X(LXP_PROGRAM_ERR_DENIED, -1) \
    X(LXP_PROGRAM_ERR_INVALID, -2) \
    X(LXP_PROGRAM_ERR_BOUNDS, -3) \
    X(LXP_PROGRAM_ERR_METER, -4) \
    X(LXP_PROGRAM_ERR_EVIDENCE, -5) \
    X(LXP_PROGRAM_ERR_NULL_ARGUMENT, -16) \
    X(LXP_PROGRAM_ERR_EMPTY_KEY, -17) \
    X(LXP_PROGRAM_ERR_KEY_TOO_LARGE, -18) \
    X(LXP_PROGRAM_ERR_VALUE_TOO_LARGE, -19) \
    X(LXP_PROGRAM_ERR_EMPTY_TOPIC, -20) \
    X(LXP_PROGRAM_ERR_TOPIC_TOO_LARGE, -21) \
    X(LXP_PROGRAM_ERR_DATA_TOO_LARGE, -22) \
    X(LXP_PROGRAM_ERR_INPUT_TOO_LARGE, -23) \
    X(LXP_PROGRAM_ERR_ZERO_AMOUNT, -24) \
    X(LXP_PROGRAM_ERR_RESERVED_IDENTIFIER, -25) \
    X(LXP_PROGRAM_ERR_DUPLICATE_CAPABILITY, -26) \
    X(LXP_PROGRAM_ERR_CAPABILITY_LIMIT, -27) \
    X(LXP_PROGRAM_ERR_CAPABILITY_BYTES, -28) \
    X(LXP_PROGRAM_ERR_BUFFER_TOO_SMALL, -29) \
    X(LXP_PROGRAM_ERR_RECEIPT_ENCODING, -30) \
    X(LXP_PROGRAM_ERR_OVERFLOW, -31) \
    X(LXP_PROGRAM_ERR_UNDERFLOW, -32)

#define LXP_PROGRAM_DECLARE_STATUS(name, value) enum { name = value };
LXP_PROGRAM_STATUS_LIST(LXP_PROGRAM_DECLARE_STATUS)
#undef LXP_PROGRAM_DECLARE_STATUS

typedef struct lxp_program_amount {
    uint64_t hi;
    uint64_t lo;
} lxp_program_amount;

typedef struct lxp_program_id {
    uint8_t bytes[LXP_PROGRAM_ID_BYTES];
} lxp_program_id;

typedef struct lxp_program_asset {
    uint8_t bytes[LXP_PROGRAM_ID_BYTES];
} lxp_program_asset;

typedef struct lxp_program_account {
    uint8_t bytes[LXP_PROGRAM_ID_BYTES];
} lxp_program_account;

typedef struct lxp_program_digest {
    uint8_t bytes[LXP_PROGRAM_DIGEST_BYTES];
} lxp_program_digest;

typedef struct lxp_program_receipt {
    lxp_program_digest digest;
    int32_t result_code;
    lxp_program_asset asset;
    lxp_program_amount amount;
    uint8_t state_root[32];
} lxp_program_receipt;

typedef enum lxp_program_capability_kind {
    LXP_PROGRAM_CAPABILITY_STORAGE_READ = 1,
    LXP_PROGRAM_CAPABILITY_STORAGE_WRITE = 2,
    LXP_PROGRAM_CAPABILITY_EMIT_EVENT = 3,
    LXP_PROGRAM_CAPABILITY_CALL = 4,
    LXP_PROGRAM_CAPABILITY_TRANSFER_402 = 5,
    LXP_PROGRAM_CAPABILITY_RECEIPT_READ = 6,
    LXP_PROGRAM_CAPABILITY_SHARED_STORAGE_READ = 7,
    LXP_PROGRAM_CAPABILITY_SHARED_STORAGE_WRITE = 8
} lxp_program_capability_kind;

/*
 * One explicit authority. Every constructor validates its payload, so an
 * invalid grant cannot be represented and a reserved identifier never reaches
 * the wire.
 */
typedef struct lxp_program_capability {
    uint8_t kind;
    lxp_program_id program;
    lxp_program_asset asset;
    lxp_program_account to;
    lxp_program_amount maximum_amount;
    lxp_program_digest receipt_digest;
} lxp_program_capability;

/*
 * Capability sets never allocate. The caller supplies the backing array, so a
 * program carries no ambient authority and no hidden heap.
 */
typedef struct lxp_program_capability_set {
    lxp_program_capability *grants;
    uint16_t capacity;
    uint16_t count;
} lxp_program_capability_set;

const uint8_t *lxp_program_abi_manifest(size_t *length);
const char *lxp_program_status_name(lxp_program_status status);

/*
 * Collapses a guest-side refusal onto the frozen host status band so the
 * integer an entrypoint returns is the same in every authoring language.
 */
lxp_program_status lxp_program_status_abi(lxp_program_status status);

void lxp_program_write_u16_be(uint8_t *out, uint16_t value);
void lxp_program_write_u32_be(uint8_t *out, uint32_t value);
void lxp_program_write_u64_be(uint8_t *out, uint64_t value);
uint16_t lxp_program_read_u16_be(const uint8_t *bytes);
uint32_t lxp_program_read_u32_be(const uint8_t *bytes);
uint64_t lxp_program_read_u64_be(const uint8_t *bytes);
int32_t lxp_program_read_i32_be(const uint8_t *bytes);
void lxp_program_copy(uint8_t *destination, const uint8_t *source, size_t length);
bool lxp_program_bytes_equal(const uint8_t *left, const uint8_t *right, size_t length);
int lxp_program_bytes_compare(const uint8_t *left, const uint8_t *right, size_t length);
bool lxp_program_bytes32_is_zero(const uint8_t bytes[32]);

/*
 * The guest boundary carries integers only. Fixed thirty-two byte identifiers
 * therefore arrive as four big-endian sixty-four bit words.
 */
void lxp_program_bytes32_from_words(uint64_t word0, uint64_t word1,
                                    uint64_t word2, uint64_t word3,
                                    uint8_t out[32]);
lxp_program_id lxp_program_id_from_words(uint64_t word0, uint64_t word1,
                                         uint64_t word2, uint64_t word3);
lxp_program_asset lxp_program_asset_from_words(uint64_t word0, uint64_t word1,
                                               uint64_t word2, uint64_t word3);
lxp_program_account lxp_program_account_from_words(uint64_t word0,
                                                   uint64_t word1,
                                                   uint64_t word2,
                                                   uint64_t word3);
lxp_program_digest lxp_program_digest_from_words(uint64_t word0, uint64_t word1,
                                                 uint64_t word2,
                                                 uint64_t word3);

lxp_program_amount lxp_program_amount_from_parts(uint64_t hi, uint64_t lo);
lxp_program_amount lxp_program_amount_from_words(uint64_t hi, uint64_t lo);
lxp_program_amount lxp_program_amount_from_be(const uint8_t bytes[16]);
void lxp_program_amount_to_be(lxp_program_amount value, uint8_t bytes[16]);
bool lxp_program_amount_is_zero(lxp_program_amount value);
int lxp_program_amount_cmp(lxp_program_amount left, lxp_program_amount right);
lxp_program_status lxp_program_amount_add(lxp_program_amount left,
                                          lxp_program_amount right,
                                          lxp_program_amount *out);
lxp_program_status lxp_program_amount_sub(lxp_program_amount left,
                                          lxp_program_amount right,
                                          lxp_program_amount *out);

lxp_program_capability lxp_program_capability_storage_read(void);
lxp_program_capability lxp_program_capability_storage_write(void);
lxp_program_capability lxp_program_capability_shared_storage_read(void);
lxp_program_capability lxp_program_capability_shared_storage_write(void);
lxp_program_capability lxp_program_capability_emit_event(void);
lxp_program_status lxp_program_capability_call(lxp_program_id program,
                                               lxp_program_capability *out);
lxp_program_status lxp_program_capability_transfer_402(
    lxp_program_asset asset, lxp_program_account to,
    lxp_program_amount maximum_amount, lxp_program_capability *out);
lxp_program_status lxp_program_capability_receipt_read(
    lxp_program_digest receipt_digest, lxp_program_capability *out);

lxp_program_status lxp_program_capability_set_init(
    lxp_program_capability_set *set, lxp_program_capability *storage,
    uint16_t capacity);
lxp_program_status lxp_program_capability_set_push(
    lxp_program_capability_set *set, lxp_program_capability grant);
lxp_program_status lxp_program_capability_set_encode(
    const lxp_program_capability_set *set, uint8_t *out, size_t capacity,
    size_t *length);
size_t lxp_program_capability_set_encoded_length(
    const lxp_program_capability_set *set);

lxp_program_status lxp_program_storage_read(const uint8_t *key,
                                            size_t key_length, uint8_t *out,
                                            size_t capacity, size_t *length,
                                            bool *found);
lxp_program_status lxp_program_storage_write(const uint8_t *key,
                                             size_t key_length,
                                             const uint8_t *value,
                                             size_t value_length);
lxp_program_status lxp_program_storage_delete(const uint8_t *key,
                                              size_t key_length);

lxp_program_status lxp_program_event_emit(const uint8_t *topic,
                                          size_t topic_length,
                                          const uint8_t *data,
                                          size_t data_length);

lxp_program_status lxp_program_call(lxp_program_id callee,
                                    const uint8_t *input, size_t input_length,
                                    const uint8_t *capabilities,
                                    size_t capabilities_length);

lxp_program_status lxp_program_transfer_402(lxp_program_asset asset,
                                            lxp_program_account to,
                                            lxp_program_amount amount);

lxp_program_status lxp_program_receipt_read(lxp_program_digest receipt_digest,
                                            lxp_program_receipt *out);

/*
 * Composition enters a callee by asking it to reserve a bounded region of its
 * own linear memory, writing the call input there, and then invoking the call
 * entry export. The reservation lives in the SDK so a program declares its
 * entry points without owning a raw pointer of its own.
 */
int32_t lxp_program_reserve_call_input(int32_t length);
lxp_program_status lxp_program_call_input(int32_t pointer, int32_t length,
                                          const uint8_t **out,
                                          size_t *out_length);

#define LXP_PROGRAM_V2_ABI_MODULE "layerx_v2"
#define LXP_PROGRAM_V3_ABI_MODULE "layerx_v3"
#define LXP_PROGRAM_V4_ABI_MODULE "layerx_v4"
enum {
    LXP_PROGRAM_V1_ABI_VERSION = 1,
    LXP_PROGRAM_V2_ABI_VERSION = 2,
    LXP_PROGRAM_V3_ABI_VERSION = 3,
    LXP_PROGRAM_V4_ABI_VERSION = 4,
    LXP_PROGRAM_MAX_CALL_RESPONSE_BYTES = 1048576,
    LXP_PROGRAM_MAX_REFUSAL_REASON_BYTES = 4096,
    LXP_PROGRAM_MAX_ACCOUNT_SEED_BYTES = 128,
    LXP_PROGRAM_V2_REFUSAL_SENTINEL = -64,
    LXP_PROGRAM_MAX_SCAN_CURSOR_BYTES = 591,
    LXP_PROGRAM_MAX_SCAN_ENTRIES = 64,
    LXP_PROGRAM_MAX_SCAN_BYTES = 67126228,
    LXP_PROGRAM_MAX_HASH_INPUT_BYTES = 1048576,
    LXP_PROGRAM_ORACLE_OBSERVATION_BYTES = 64,
    LXP_PROGRAM_WEB_HEADER_BYTES = 40,
    LXP_PROGRAM_WEB_MAX_RESPONSE_BYTES = 4096,
    LXP_PROGRAM_WEB_RECORD_BYTES = 4136,
    LXP_PROGRAM_WEB_STATUS_ABSENT = -7
};
typedef enum lxp_program_storage_scope {
    LXP_PROGRAM_STORAGE_PRINCIPAL = 1,
    LXP_PROGRAM_STORAGE_SHARED = 2
} lxp_program_storage_scope;
typedef enum lxp_program_context_field {
    LXP_PROGRAM_CONTEXT_EXECUTING_PROGRAM = 1,
    LXP_PROGRAM_CONTEXT_IMMEDIATE_CALLER = 2,
    LXP_PROGRAM_CONTEXT_INVOKING_PRINCIPAL = 3,
    LXP_PROGRAM_CONTEXT_ACTIVITY_SEQUENCE = 4,
    LXP_PROGRAM_CONTEXT_BATCH_HEIGHT = 5,
    LXP_PROGRAM_CONTEXT_RUNTIME_VERSION = 6,
    LXP_PROGRAM_CONTEXT_ABI_VERSION = 7,
    LXP_PROGRAM_CONTEXT_REMAINING_FUEL = 8,
    LXP_PROGRAM_CONTEXT_FEE_SCHEDULE_VERSION = 9
} lxp_program_context_field;
typedef enum lxp_program_hash_algorithm {
    LXP_PROGRAM_HASH_SHA256 = 1,
    LXP_PROGRAM_HASH_KECCAK256 = 2,
    LXP_PROGRAM_HASH_BLAKE3 = 3
} lxp_program_hash_algorithm;
typedef enum lxp_program_signature_algorithm {
    LXP_PROGRAM_SIGNATURE_ED25519 = 1,
    LXP_PROGRAM_SIGNATURE_SECP256K1 = 2
} lxp_program_signature_algorithm;
typedef enum lxp_program_refusal_class {
    LXP_PROGRAM_REFUSAL_REJECTED = 1,
    LXP_PROGRAM_REFUSAL_INVALID_INPUT = 2,
    LXP_PROGRAM_REFUSAL_UNAUTHORIZED = 3,
    LXP_PROGRAM_REFUSAL_CONFLICT = 4,
    LXP_PROGRAM_REFUSAL_NOT_FOUND = 5,
    LXP_PROGRAM_REFUSAL_RUNTIME_FAULT = 254,
    LXP_PROGRAM_REFUSAL_LEGACY = 255
} lxp_program_refusal_class;
typedef struct lxp_program_scan_page {
    const uint8_t *encoded;
    size_t length;
    uint16_t count;
    const uint8_t *cursor;
    size_t cursor_length;
} lxp_program_scan_page;
typedef struct lxp_program_oracle_observation {
    lxp_program_amount price;
    uint64_t observed_at;
    uint64_t sequence;
    lxp_program_digest source_set_digest;
} lxp_program_oracle_observation;
typedef struct lxp_program_web_answer {
    lxp_program_digest content_digest;
    uint32_t full_length;
    const uint8_t *response;
    size_t response_length;
} lxp_program_web_answer;


const uint8_t *lxp_program_abi_manifest_version(uint16_t version, size_t *length);
lxp_program_status lxp_program_response_write(int32_t code, const uint8_t *bytes, size_t length);
lxp_program_status lxp_program_refusal_write(lxp_program_refusal_class class_code, const uint8_t *reason, size_t length);
lxp_program_status lxp_program_call_response(lxp_program_id callee,
    const uint8_t *input, size_t input_length, const uint8_t *capabilities,
    size_t capabilities_length, uint8_t *output, size_t capacity, int32_t *code, size_t *length);
lxp_program_status lxp_program_storage_read_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length, uint8_t *out, size_t capacity, size_t *length, bool *found);
lxp_program_status lxp_program_storage_write_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length, const uint8_t *value, size_t value_length);
lxp_program_status lxp_program_storage_delete_scoped(lxp_program_storage_scope scope,
    const uint8_t *key, size_t key_length);
lxp_program_status lxp_program_storage_drop_scoped(lxp_program_storage_scope scope);
lxp_program_status lxp_program_storage_scan_scoped(lxp_program_storage_scope scope,
    const uint8_t *prefix, size_t prefix_length, const uint8_t *cursor, size_t cursor_length,
    uint32_t max_entries, uint32_t max_bytes, uint8_t *output, size_t capacity, lxp_program_scan_page *page);
lxp_program_status lxp_program_transfer_program_402(lxp_program_amount amount,
    const uint8_t *seed, size_t seed_length, lxp_program_account source, lxp_program_asset asset, lxp_program_account recipient);
lxp_program_status lxp_program_fund_program_402(lxp_program_amount amount,
    const uint8_t *seed, size_t seed_length, lxp_program_account destination, lxp_program_asset asset);
lxp_program_status lxp_program_context_read(lxp_program_context_field field,
    uint8_t *output, size_t capacity, size_t *length);
lxp_program_status lxp_program_balance_read(lxp_program_account account,
    lxp_program_asset asset, lxp_program_amount *amount);
lxp_program_status lxp_program_hash(lxp_program_hash_algorithm algorithm,
    const uint8_t *input, size_t length, lxp_program_digest *digest);
lxp_program_status lxp_program_signature_verify(lxp_program_signature_algorithm algorithm,
    const uint8_t *message, size_t message_length, const uint8_t *key, size_t key_length, const uint8_t signature[64]);
lxp_program_status lxp_program_signature_recover(const uint8_t digest[32],
    const uint8_t signature[64], uint8_t recovery_id, uint8_t output[65]);
lxp_program_status lxp_program_bigint_mul_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[64]);
lxp_program_status lxp_program_bigint_div_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[32]);
lxp_program_status lxp_program_bigint_rem_256(const uint8_t left[32], const uint8_t right[32], uint8_t output[32]);
lxp_program_status lxp_program_bigint_modexp_256(const uint8_t base[32], const uint8_t exponent[32],
    const uint8_t modulus[32], uint8_t output[32]);
lxp_program_status lxp_program_oracle_read(const uint8_t market[32], lxp_program_oracle_observation *observation);
lxp_program_status lxp_program_web_read(uint64_t request_id, uint8_t *record, size_t capacity,
    lxp_program_web_answer *answer, bool *found);

int32_t lxp_program_reserve_call_input_versioned(int32_t length);
lxp_program_status lxp_program_call_input_versioned(int32_t pointer, int32_t length, const uint8_t **out, size_t *out_length);

#endif
