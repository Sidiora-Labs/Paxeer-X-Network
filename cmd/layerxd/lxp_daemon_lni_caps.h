#ifndef LXP_DAEMON_LNI_CAPS_H
#define LXP_DAEMON_LNI_CAPS_H

#include "layerx/lxp_hash.h"
#include "layerx/lxp_module.h"
#include "layerx/lxp_state.h"
#include "layerx/lxp_state_proof.h"

#include <openssl/rand.h>

#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

enum {
    LNI_CAPS_VERSION = 1,
    LNI_CAPS_OP_OPEN = 1,
    LNI_CAPS_OP_PAGE = 2,
    LNI_CAPS_OP_RELEASE = 3,
    LNI_CAPS_OPEN_REQUEST_BYTES = 3 + 4 + 32 + 1 + 2 + 4,
    LNI_CAPS_CURSOR_BYTES = 2 + 32 + 4 + 32 + 32 + 1 + 4,
    LNI_CAPS_PAGE_REQUEST_BYTES = 3 + LNI_CAPS_CURSOR_BYTES + 2 + 4,
    LNI_CAPS_RELEASE_REQUEST_BYTES = 3 + 32,
    LNI_CAPS_MAX_ACTIVE_SNAPSHOTS = 4,
    LNI_CAPS_MAX_ITEMS = 4096,
    LNI_CAPS_MAX_SNAPSHOT_BYTES = 32 * 1024 * 1024,
    LNI_CAPS_LIFETIME_MS = 60000,
    LNI_CAPS_PAGE_MAX_ITEMS = 64,
    LNI_CAPS_PAGE_MIN_BYTES = 4096,
    LNI_CAPS_PREFIXES = 2,
    LNI_CAPS_BOUND_NONE = 0,
    LNI_CAPS_BOUND_EDGE = 1,
    LNI_CAPS_BOUND_WITNESS = 2,
    LNI_CAPS_BOUND_EMPTY_MODULE = 3
};

static const uint8_t LNI_CAPS_SELECTION_DOMAIN[] = "LayerX/caps-selection/v1";
static const uint8_t LNI_CAPS_SNAPSHOT_DOMAIN[] = "LayerX/caps-snapshot/v1";

typedef struct lni_caps_bytes {
    uint8_t *bytes;
    size_t length;
} lni_caps_bytes;

typedef struct lni_caps_prefix {
    uint16_t module_id;
    uint32_t module_leaf_count;
    uint32_t first;
    uint32_t count;
    lni_caps_bytes *items;
    uint8_t lower_flag;
    uint8_t upper_flag;
    lni_caps_bytes lower;
    lni_caps_bytes upper;
} lni_caps_prefix;

typedef struct lni_caps_snapshot {
    bool active;
    uint8_t snapshot_id[32];
    uint32_t network_id;
    uint8_t state_root[32];
    uint8_t selection_digest[32];
    uint64_t expires_at_ms;
    uint8_t next_prefix;
    uint32_t next_position;
    size_t retained_bytes;
    lni_caps_bytes opened;
    lni_caps_prefix prefixes[LNI_CAPS_PREFIXES];
} lni_caps_snapshot;

typedef struct lni_caps_connection {
    bool initialized;
    uint8_t secret[32];
    uint64_t counter;
    lni_caps_snapshot snapshots[LNI_CAPS_MAX_ACTIVE_SNAPSHOTS];
} lni_caps_connection;

typedef struct lni_caps_writer {
    uint8_t *bytes;
    size_t capacity;
    size_t cursor;
} lni_caps_writer;

static void caps_store_u16(uint8_t *bytes, uint16_t value)
{
    bytes[0] = (uint8_t)(value >> 8U);
    bytes[1] = (uint8_t)value;
}

static void caps_store_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U);
    bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U);
    bytes[3] = (uint8_t)value;
}

static void caps_store_u64(uint8_t *bytes, uint64_t value)
{
    size_t index;
    for (index = 0U; index < 8U; ++index)
        bytes[index] = (uint8_t)(value >> ((7U - index) * 8U));
}

static lxp_result caps_put(lni_caps_writer *writer, const void *bytes,
                           size_t length)
{
    if (writer->capacity - writer->cursor < length) return LXP_ERR_LENGTH_LIMIT;
    if (length != 0U) (void)memcpy(writer->bytes + writer->cursor, bytes, length);
    writer->cursor += length;
    return LXP_OK;
}

static lxp_result caps_put_u8(lni_caps_writer *writer, uint8_t value)
{
    return caps_put(writer, &value, 1U);
}

static lxp_result caps_put_u16(lni_caps_writer *writer, uint16_t value)
{
    uint8_t bytes[2];
    caps_store_u16(bytes, value);
    return caps_put(writer, bytes, sizeof(bytes));
}

static lxp_result caps_put_u32(lni_caps_writer *writer, uint32_t value)
{
    uint8_t bytes[4];
    caps_store_u32(bytes, value);
    return caps_put(writer, bytes, sizeof(bytes));
}

static lxp_result caps_put_u64(lni_caps_writer *writer, uint64_t value)
{
    uint8_t bytes[8];
    caps_store_u64(bytes, value);
    return caps_put(writer, bytes, sizeof(bytes));
}

static lxp_result caps_now_ms(uint64_t *milliseconds)
{
    struct timespec now;
    if (clock_gettime(CLOCK_REALTIME, &now) != 0 || now.tv_sec < 0)
        return LXP_ERR_IO;
    *milliseconds = (uint64_t)now.tv_sec * 1000U + (uint64_t)now.tv_nsec / 1000000U;
    return LXP_OK;
}

static void caps_bytes_free(lni_caps_bytes *bytes)
{
    if (bytes->bytes != NULL) {
        lxp_secure_zero(bytes->bytes, bytes->length);
        free(bytes->bytes);
    }
    bytes->bytes = NULL;
    bytes->length = 0U;
}

static void caps_snapshot_release(lni_caps_snapshot *snapshot)
{
    size_t prefix;
    size_t item;
    for (prefix = 0U; prefix < LNI_CAPS_PREFIXES; ++prefix) {
        lni_caps_prefix *range = &snapshot->prefixes[prefix];
        if (range->items != NULL) {
            for (item = 0U; item < range->count; ++item)
                caps_bytes_free(&range->items[item]);
            free(range->items);
        }
        caps_bytes_free(&range->lower);
        caps_bytes_free(&range->upper);
    }
    caps_bytes_free(&snapshot->opened);
    lxp_secure_zero(snapshot, sizeof(*snapshot));
}

static lxp_result lni_caps_connection_init(lni_caps_connection *connection)
{
    (void)memset(connection, 0, sizeof(*connection));
    if (RAND_bytes(connection->secret, (int)sizeof(connection->secret)) != 1)
        return LXP_ERR_IO;
    connection->initialized = true;
    return LXP_OK;
}

static void lni_caps_connection_release(lni_caps_connection *connection)
{
    size_t index;
    for (index = 0U; index < LNI_CAPS_MAX_ACTIVE_SNAPSHOTS; ++index)
        if (connection->snapshots[index].active)
            caps_snapshot_release(&connection->snapshots[index]);
    lxp_secure_zero(connection, sizeof(*connection));
}

static void caps_expire(lni_caps_connection *connection, uint64_t now)
{
    size_t index;
    for (index = 0U; index < LNI_CAPS_MAX_ACTIVE_SNAPSHOTS; ++index)
        if (connection->snapshots[index].active &&
            connection->snapshots[index].expires_at_ms <= now)
            caps_snapshot_release(&connection->snapshots[index]);
}

static lxp_result caps_selection_digest(uint32_t network_id,
                                        const uint8_t account[32],
                                        uint8_t digest[32])
{
    uint8_t preimage[sizeof(LNI_CAPS_SELECTION_DOMAIN) + 4U + 32U];
    (void)memcpy(preimage, LNI_CAPS_SELECTION_DOMAIN, sizeof(LNI_CAPS_SELECTION_DOMAIN));
    caps_store_u32(preimage + sizeof(LNI_CAPS_SELECTION_DOMAIN), network_id);
    (void)memcpy(preimage + sizeof(LNI_CAPS_SELECTION_DOMAIN) + 4U, account, 32U);
    return lxp_hash_sha256(preimage, sizeof(preimage), digest);
}

static lxp_result caps_snapshot_id(const lni_caps_connection *connection,
                                   const uint8_t state_root[32],
                                   uint8_t id[32])
{
    uint8_t preimage[sizeof(LNI_CAPS_SNAPSHOT_DOMAIN) + 32U + 8U + 32U];
    lxp_result status;
    size_t cursor = sizeof(LNI_CAPS_SNAPSHOT_DOMAIN);
    (void)memcpy(preimage, LNI_CAPS_SNAPSHOT_DOMAIN, cursor);
    (void)memcpy(preimage + cursor, connection->secret, 32U); cursor += 32U;
    caps_store_u64(preimage + cursor, connection->counter); cursor += 8U;
    (void)memcpy(preimage + cursor, state_root, 32U);
    status = lxp_hash_sha256(preimage, sizeof(preimage), id);
    lxp_secure_zero(preimage, sizeof(preimage));
    return status;
}

static void caps_encode_cursor(const lni_caps_snapshot *snapshot,
                               uint8_t prefix, uint32_t position,
                               uint8_t cursor[LNI_CAPS_CURSOR_BYTES])
{
    caps_store_u16(cursor, LNI_CAPS_VERSION);
    (void)memcpy(cursor + 2U, snapshot->snapshot_id, 32U);
    caps_store_u32(cursor + 34U, snapshot->network_id);
    (void)memcpy(cursor + 38U, snapshot->state_root, 32U);
    (void)memcpy(cursor + 70U, snapshot->selection_digest, 32U);
    cursor[102] = prefix;
    caps_store_u32(cursor + 103U, position);
}

typedef struct caps_leaf_ref {
    const uint8_t *key;
    size_t key_length;
} caps_leaf_ref;

static int caps_key_compare(const uint8_t *left, size_t left_length,
                            const uint8_t *right, size_t right_length)
{
    size_t shared = left_length < right_length ? left_length : right_length;
    int order = shared == 0U ? 0 : memcmp(left, right, shared);
    if (order != 0) return order;
    return left_length < right_length ? -1 : left_length > right_length ? 1 : 0;
}

static int caps_leaf_order(const void *left, const void *right)
{
    const caps_leaf_ref *a = left;
    const caps_leaf_ref *b = right;
    return caps_key_compare(a->key, a->key_length, b->key, b->key_length);
}

/* Builds and encodes one witness at the captured root and checks its
 * module-tree position. Everything copied out is owned by the snapshot. */
static lxp_result caps_capture_witness(
    const lxp_kernel *kernel, uint16_t module_id, lxp_byte_span key,
    const uint8_t state_root[32], lxp_state_witness *witness, uint8_t *wire,
    uint32_t expected_index, uint32_t *leaf_count, lni_caps_bytes *out,
    size_t *retained)
{
    size_t wire_length = 0U;
    lxp_result status = lxp_state_proof_build(kernel, module_id, key, witness);
    if (status == LXP_OK) status = lxp_state_proof_verify(witness, state_root);
    if (status == LXP_OK && witness->layer_a.leaf_index != expected_index)
        status = LXP_FATAL_INVARIANT;
    if (status == LXP_OK && *leaf_count != 0U &&
        witness->layer_a.leaf_count != *leaf_count)
        status = LXP_FATAL_INVARIANT;
    if (status == LXP_OK)
        status = lxp_state_proof_encode(witness, wire, LXP_STATE_WITNESS_MAX_BYTES,
                                        &wire_length);
    if (status == LXP_OK && wire_length > LNI_CAPS_MAX_SNAPSHOT_BYTES - *retained)
        status = LXP_ERR_LENGTH_LIMIT;
    if (status == LXP_OK) {
        out->bytes = malloc(wire_length);
        if (out->bytes == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    }
    if (status == LXP_OK) {
        (void)memcpy(out->bytes, wire, wire_length);
        out->length = wire_length;
        *retained += wire_length;
        *leaf_count = witness->layer_a.leaf_count;
    }
    return status;
}

static lxp_result caps_capture_empty_module(
    const lxp_kernel *kernel, uint16_t module_id, lni_caps_bytes *out,
    size_t *retained)
{
    lxp_state_proof proof;
    uint8_t root[32];
    size_t length;
    lni_caps_writer writer;
    lxp_result status = lxp_state_root_proof(kernel, module_id, root, &proof);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(root, kernel->current_state_root, 32U) != 0)
        return LXP_ERR_PROJECTION_STALE;
    length = 2U + 4U + 1U + 32U * (size_t)proof.depth;
    if (proof.leaf_index != module_id || proof.depth > LXP_STATE_PROOF_MAX_DEPTH)
        return LXP_FATAL_INVARIANT;
    out->bytes = malloc(length);
    if (out->bytes == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    writer = (lni_caps_writer){out->bytes, length, 0U};
    status = caps_put_u16(&writer, module_id);
    if (status == LXP_OK) status = caps_put_u32(&writer, proof.leaf_count);
    if (status == LXP_OK) status = caps_put_u8(&writer, proof.depth);
    if (status == LXP_OK)
        status = caps_put(&writer, proof.siblings, 32U * (size_t)proof.depth);
    if (status == LXP_OK && writer.cursor != length) status = LXP_FATAL_INVARIANT;
    out->length = length;
    *retained += length;
    return status;
}

/* Captures the complete authenticated range of one key prefix inside one
 * module at the committed root held by the caller's read lock. */
static lxp_result caps_capture_prefix(
    const lxp_kernel *kernel, uint16_t module_id, const uint8_t *prefix,
    size_t prefix_length, const uint8_t state_root[32],
    lxp_state_witness *witness, uint8_t *wire, lni_caps_prefix *range,
    size_t *retained)
{
    static const uint8_t blob_marker = 0xffU;
    uint8_t (*blob_keys)[LXP_STATE_WITNESS_MAX_KEY] = NULL;
    caps_leaf_ref *leaves = NULL;
    size_t leaf_count = 0U;
    size_t blob_count = 0U;
    size_t index;
    size_t first = SIZE_MAX;
    size_t end = 0U;
    uint32_t module_leaf_count = 0U;
    lxp_result status = LXP_OK;
    range->module_id = module_id;
    leaves = calloc(kernel->module_kv_count + kernel->blob_count + 1U, sizeof(*leaves));
    blob_keys = calloc(kernel->blob_count + 1U, sizeof(*blob_keys));
    if (leaves == NULL || blob_keys == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    for (index = 0U; status == LXP_OK && index < kernel->module_kv_count; ++index) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[index];
        if (entry->module_id != module_id) continue;
        leaves[leaf_count++] = (caps_leaf_ref){entry->key, entry->key_length};
    }
    for (index = 0U; status == LXP_OK && index < kernel->blob_count; ++index) {
        const lxp_module_blob *blob = &kernel->blobs[index];
        if (blob->module_id != module_id) continue;
        blob_keys[blob_count][0] = blob_marker;
        (void)memcpy(blob_keys[blob_count] + LXP_STATE_WITNESS_MAX_KEY - 32U, blob->key, 32U);
        leaves[leaf_count++] = (caps_leaf_ref){blob_keys[blob_count],
                                               LXP_STATE_WITNESS_MAX_KEY};
        ++blob_count;
    }
    if (status == LXP_OK)
        qsort(leaves, leaf_count, sizeof(*leaves), caps_leaf_order);
    for (index = 1U; status == LXP_OK && index < leaf_count; ++index)
        if (caps_leaf_order(&leaves[index - 1U], &leaves[index]) >= 0)
            status = LXP_FATAL_INVARIANT;
    for (index = 0U; status == LXP_OK && index < leaf_count; ++index) {
        bool member = leaves[index].key_length > prefix_length &&
            memcmp(leaves[index].key, prefix, prefix_length) == 0;
        if (member && first == SIZE_MAX) first = index;
        if (member) end = index + 1U;
        if (!member && first == SIZE_MAX &&
            caps_key_compare(leaves[index].key, leaves[index].key_length,
                             prefix, prefix_length) > 0) {
            first = index;
            end = index;
        }
    }
    if (status == LXP_OK && first == SIZE_MAX) {
        first = leaf_count;
        end = leaf_count;
    }
    if (status == LXP_OK && end - first > LNI_CAPS_MAX_ITEMS)
        status = LXP_ERR_LENGTH_LIMIT;
    if (status == LXP_OK && end > first) {
        range->items = calloc(end - first, sizeof(*range->items));
        if (range->items == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    }
    range->first = (uint32_t)(first == SIZE_MAX ? 0U : first);
    for (index = first; status == LXP_OK && index < end; ++index) {
        status = caps_capture_witness(
            kernel, module_id,
            (lxp_byte_span){leaves[index].key, leaves[index].key_length},
            state_root, witness, wire, (uint32_t)index, &module_leaf_count,
            &range->items[index - first], retained);
        if (status == LXP_OK) range->count = (uint32_t)(index - first + 1U);
    }
    if (status == LXP_OK && leaf_count == 0U) {
        range->lower_flag = LNI_CAPS_BOUND_EMPTY_MODULE;
        range->upper_flag = LNI_CAPS_BOUND_EMPTY_MODULE;
        status = caps_capture_empty_module(kernel, module_id, &range->lower, retained);
        if (status == LXP_OK)
            status = caps_capture_empty_module(kernel, module_id, &range->upper, retained);
    } else if (status == LXP_OK) {
        if (first == 0U) {
            range->lower_flag = LNI_CAPS_BOUND_EDGE;
        } else {
            range->lower_flag = LNI_CAPS_BOUND_WITNESS;
            status = caps_capture_witness(
                kernel, module_id,
                (lxp_byte_span){leaves[first - 1U].key, leaves[first - 1U].key_length},
                state_root, witness, wire, (uint32_t)(first - 1U),
                &module_leaf_count, &range->lower, retained);
        }
        if (status == LXP_OK && end == leaf_count) {
            range->upper_flag = LNI_CAPS_BOUND_EDGE;
        } else if (status == LXP_OK) {
            range->upper_flag = LNI_CAPS_BOUND_WITNESS;
            status = caps_capture_witness(
                kernel, module_id,
                (lxp_byte_span){leaves[end].key, leaves[end].key_length},
                state_root, witness, wire, (uint32_t)end, &module_leaf_count,
                &range->upper, retained);
        }
        if (status == LXP_OK && module_leaf_count != leaf_count)
            status = LXP_FATAL_INVARIANT;
    }
    range->module_leaf_count = (uint32_t)leaf_count;
    free(blob_keys);
    free(leaves);
    return status;
}

static lxp_result caps_encode_opened(lni_caps_snapshot *snapshot,
                                     uint64_t observed_sequence,
                                     uint8_t requested_rank,
                                     lxp_byte_span account_value,
                                     lxp_byte_span account_evidence)
{
    uint8_t cursor[LNI_CAPS_CURSOR_BYTES];
    size_t length = 3U + 32U + 4U + 32U + 8U + 8U + 32U + 1U + 4U +
        account_value.length + 4U + account_evidence.length +
        LNI_CAPS_PREFIXES * 14U + LNI_CAPS_CURSOR_BYTES;
    lni_caps_writer writer;
    size_t prefix;
    lxp_result status;
    if (account_value.length > UINT32_MAX || account_evidence.length > UINT32_MAX)
        return LXP_ERR_LENGTH_LIMIT;
    snapshot->opened.bytes = malloc(length);
    if (snapshot->opened.bytes == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    snapshot->opened.length = length;
    snapshot->retained_bytes += length;
    writer = (lni_caps_writer){snapshot->opened.bytes, length, 0U};
    status = caps_put_u16(&writer, LNI_CAPS_VERSION);
    if (status == LXP_OK) status = caps_put_u8(&writer, LNI_CAPS_OP_OPEN);
    if (status == LXP_OK) status = caps_put(&writer, snapshot->snapshot_id, 32U);
    if (status == LXP_OK) status = caps_put_u32(&writer, snapshot->network_id);
    if (status == LXP_OK) status = caps_put(&writer, snapshot->state_root, 32U);
    if (status == LXP_OK) status = caps_put_u64(&writer, observed_sequence);
    if (status == LXP_OK) status = caps_put_u64(&writer, snapshot->expires_at_ms);
    if (status == LXP_OK) status = caps_put(&writer, snapshot->selection_digest, 32U);
    if (status == LXP_OK) status = caps_put_u8(&writer, requested_rank);
    if (status == LXP_OK) status = caps_put_u32(&writer, (uint32_t)account_value.length);
    if (status == LXP_OK) status = caps_put(&writer, account_value.bytes, account_value.length);
    if (status == LXP_OK) status = caps_put_u32(&writer, (uint32_t)account_evidence.length);
    if (status == LXP_OK)
        status = caps_put(&writer, account_evidence.bytes, account_evidence.length);
    for (prefix = 0U; status == LXP_OK && prefix < LNI_CAPS_PREFIXES; ++prefix) {
        const lni_caps_prefix *range = &snapshot->prefixes[prefix];
        status = caps_put_u16(&writer, range->module_id);
        if (status == LXP_OK) status = caps_put_u32(&writer, range->module_leaf_count);
        if (status == LXP_OK) status = caps_put_u32(&writer, range->first);
        if (status == LXP_OK) status = caps_put_u32(&writer, range->count);
    }
    caps_encode_cursor(snapshot, 1U, snapshot->prefixes[0].first, cursor);
    if (status == LXP_OK) status = caps_put(&writer, cursor, sizeof(cursor));
    if (status == LXP_OK && writer.cursor != length) status = LXP_FATAL_INVARIANT;
    return status;
}

/* Captures one immutable snapshot under the existing read lock. The lock is
 * released before any page is served; nothing retained points into the
 * kernel. */
static lxp_result caps_open_snapshot(
    lxp_daemon_lni_server *server, lni_caps_connection *connection,
    uint32_t network_id, const uint8_t account[32], uint8_t requested_rank,
    lni_caps_snapshot *snapshot)
{
    static const uint8_t budget_prefix[] = { 'b', 'u', 'd', 'g', 'e', 't', ':' };
    static const uint8_t grant_prefix[] = { 'g', 'r', 'a', 'n', 't', ':' };
    lxp_daemon_protocol_owner *owner = server->owner;
    lxp_daemon_receipt_evidence head;
    lxp_daemon_signed_header_evidence signed_header;
    lxp_batch_header header;
    lxp_byte_span value = {0};
    lxp_byte_span proof = {0};
    lxp_state_witness *witness;
    uint8_t *wire;
    uint8_t account_key[33];
    uint64_t epoch;
    uint64_t now;
    uint64_t observed_sequence = 0U;
    size_t mark;
    lxp_result status = caps_now_ms(&now);
    if (status != LXP_OK) return status;
    if (now > UINT64_MAX - LNI_CAPS_LIFETIME_MS) return LXP_ERR_LENGTH_LIMIT;
    witness = malloc(sizeof(*witness));
    wire = malloc(LXP_STATE_WITNESS_MAX_BYTES);
    if (witness == NULL || wire == NULL) {
        free(witness);
        free(wire);
        return LXP_ERR_ARENA_EXHAUSTED;
    }
    (void)memset(snapshot, 0, sizeof(*snapshot));
    account_key[0] = 4U;
    (void)memcpy(account_key + 1U, account, 32U);
    status = lni_read_lock(owner);
    if (status != LXP_OK) {
        free(witness);
        free(wire);
        return status;
    }
    mark = lxp_arena_mark(owner->scratch);
    if (owner->evidence_store == NULL || owner->kernel == NULL ||
        owner->protocol_version != LXP_PROTOCOL_VERSION_STATE_COMMITMENT)
        status = LXP_ERR_MODULE_DISABLED;
    else if (owner->evidence_store->network_id != network_id)
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK)
        status = latest_receipt_evidence(owner, owner->scratch, &head);
    if (status == LXP_OK) {
        signed_header.authorization = owner->evidence_store->authorization;
        signed_header.canonical_header = head.canonical_header;
        (void)memcpy(signed_header.signature, head.header_signature,
                     sizeof(signed_header.signature));
        if (owner->evidence_store->handover_chain != NULL) {
            status = lxp_batch_header_decode(head.canonical_header.bytes,
                head.canonical_header.length, &header);
            if (status == LXP_OK)
                status = lxp_handover_trust_authorization(
                    owner->evidence_store->handover_chain, header.batch_number,
                    &signed_header.authorization, &epoch);
            if (status == LXP_OK && header.epoch != epoch)
                status = LXP_ERR_AUTH_SCOPE;
        }
    }
    if (status == LXP_OK)
        status = lxp_daemon_module_evidence_wire_encode(
            owner->evidence_store, owner->kernel, &signed_header, 0U,
            (lxp_byte_span){account_key, sizeof(account_key)}, 1U, 0U, NULL,
            requested_rank, owner->scratch, &value, &proof);
    if (status == LXP_OK) {
        snapshot->network_id = network_id;
        (void)memcpy(snapshot->state_root, owner->kernel->current_state_root, 32U);
        observed_sequence = owner->kernel->state->next_sequence - 1U;
        ++connection->counter;
        status = caps_snapshot_id(connection, snapshot->state_root,
                                  snapshot->snapshot_id);
    }
    if (status == LXP_OK)
        status = caps_selection_digest(network_id, account, snapshot->selection_digest);
    if (status == LXP_OK)
        status = caps_capture_prefix(owner->kernel, LXP_MODULE_BUDGET, budget_prefix,
            sizeof(budget_prefix), snapshot->state_root, witness, wire,
            &snapshot->prefixes[0], &snapshot->retained_bytes);
    if (status == LXP_OK)
        status = caps_capture_prefix(owner->kernel, LXP_MODULE_ASSET, grant_prefix,
            sizeof(grant_prefix), snapshot->state_root, witness, wire,
            &snapshot->prefixes[1], &snapshot->retained_bytes);
    if (status == LXP_OK) {
        snapshot->expires_at_ms = now + LNI_CAPS_LIFETIME_MS;
        status = caps_encode_opened(snapshot, observed_sequence, requested_rank,
                                    value, proof);
    }
    if (lxp_arena_reset(owner->scratch, mark) != LXP_OK && status == LXP_OK)
        status = LXP_FATAL_INVARIANT;
    status = lni_read_unlock(owner, status);
    lxp_secure_zero(witness, sizeof(*witness));
    free(witness);
    free(wire);
    if (status == LXP_OK && snapshot->retained_bytes > LNI_CAPS_MAX_SNAPSHOT_BYTES)
        status = LXP_ERR_LENGTH_LIMIT;
    if (status != LXP_OK) {
        caps_snapshot_release(snapshot);
        return status;
    }
    snapshot->next_prefix = 1U;
    snapshot->next_position = snapshot->prefixes[0].first;
    snapshot->active = true;
    return LXP_OK;
}

static size_t caps_bound_length(uint8_t flag, const lni_caps_bytes *bound)
{
    return flag == LNI_CAPS_BOUND_WITNESS ? 1U + 4U + bound->length :
           flag == LNI_CAPS_BOUND_EMPTY_MODULE ? 1U + bound->length : 1U;
}

static lxp_result caps_put_bound(lni_caps_writer *writer, uint8_t flag,
                                 const lni_caps_bytes *bound)
{
    lxp_result status = caps_put_u8(writer, flag);
    if (status == LXP_OK && flag == LNI_CAPS_BOUND_WITNESS)
        status = caps_put_u32(writer, (uint32_t)bound->length);
    if (status == LXP_OK && (flag == LNI_CAPS_BOUND_WITNESS ||
                             flag == LNI_CAPS_BOUND_EMPTY_MODULE))
        status = caps_put(writer, bound->bytes, bound->length);
    return status;
}

/* Encodes the next page strictly from the retained immutable snapshot. */
static lxp_result caps_encode_page(lni_caps_snapshot *snapshot,
                                   const uint8_t cursor[LNI_CAPS_CURSOR_BYTES],
                                   uint16_t max_items, uint32_t max_bytes,
                                   uint8_t **payload, size_t *payload_length)
{
    lni_caps_prefix *range = &snapshot->prefixes[snapshot->next_prefix - 1U];
    uint32_t offset = snapshot->next_position - range->first;
    uint32_t take = 0U;
    uint8_t next_cursor[LNI_CAPS_CURSOR_BYTES];
    uint8_t lower_flag;
    uint8_t upper_flag;
    bool exhausted;
    bool last_prefix = snapshot->next_prefix == LNI_CAPS_PREFIXES;
    size_t length = 3U + LNI_CAPS_CURSOR_BYTES + 2U;
    size_t bounds;
    lni_caps_writer writer;
    uint32_t index;
    lxp_result status = LXP_OK;
    if (snapshot->next_position < range->first || offset > range->count)
        return LXP_FATAL_INVARIANT;
    lower_flag = offset == 0U ? range->lower_flag : LNI_CAPS_BOUND_NONE;
    while (offset + take < range->count && take < max_items) {
        size_t item = 4U + range->items[offset + take].length;
        size_t tail = caps_bound_length(lower_flag, &range->lower) +
            caps_bound_length(offset + take + 1U == range->count ?
                range->upper_flag : LNI_CAPS_BOUND_NONE, &range->upper) +
            1U + LNI_CAPS_CURSOR_BYTES;
        if (length + item + tail > max_bytes) break;
        length += item;
        ++take;
    }
    exhausted = offset + take == range->count;
    if (take == 0U && !exhausted) return LXP_ERR_LENGTH_LIMIT;
    upper_flag = exhausted ? range->upper_flag : LNI_CAPS_BOUND_NONE;
    bounds = caps_bound_length(lower_flag, &range->lower) +
        caps_bound_length(upper_flag, &range->upper);
    length += bounds + 1U + (exhausted && last_prefix ? 0U : LNI_CAPS_CURSOR_BYTES);
    if (length > max_bytes) return LXP_ERR_LENGTH_LIMIT;
    *payload = malloc(length);
    if (*payload == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    writer = (lni_caps_writer){*payload, length, 0U};
    status = caps_put_u16(&writer, LNI_CAPS_VERSION);
    if (status == LXP_OK) status = caps_put_u8(&writer, LNI_CAPS_OP_PAGE);
    if (status == LXP_OK) status = caps_put(&writer, cursor, LNI_CAPS_CURSOR_BYTES);
    if (status == LXP_OK) status = caps_put_u16(&writer, (uint16_t)take);
    for (index = 0U; status == LXP_OK && index < take; ++index) {
        const lni_caps_bytes *item = &range->items[offset + index];
        status = caps_put_u32(&writer, (uint32_t)item->length);
        if (status == LXP_OK) status = caps_put(&writer, item->bytes, item->length);
    }
    if (status == LXP_OK) status = caps_put_bound(&writer, lower_flag, &range->lower);
    if (status == LXP_OK) status = caps_put_bound(&writer, upper_flag, &range->upper);
    if (status == LXP_OK) status = caps_put_u8(&writer, exhausted ? 1U : 0U);
    if (status == LXP_OK && !(exhausted && last_prefix)) {
        uint8_t next_prefix = exhausted ? (uint8_t)(snapshot->next_prefix + 1U) :
            snapshot->next_prefix;
        uint32_t next_position = exhausted ?
            snapshot->prefixes[next_prefix - 1U].first :
            snapshot->next_position + take;
        caps_encode_cursor(snapshot, next_prefix, next_position, next_cursor);
        status = caps_put(&writer, next_cursor, sizeof(next_cursor));
        if (status == LXP_OK) {
            snapshot->next_prefix = next_prefix;
            snapshot->next_position = next_position;
        }
    } else if (status == LXP_OK) {
        snapshot->next_prefix = 0U;
    }
    if (status == LXP_OK && writer.cursor != length) status = LXP_FATAL_INVARIANT;
    if (status != LXP_OK) {
        free(*payload);
        *payload = NULL;
        return status;
    }
    *payload_length = length;
    return LXP_OK;
}

static lni_caps_snapshot *caps_find(lni_caps_connection *connection,
                                    const uint8_t snapshot_id[32])
{
    size_t index;
    for (index = 0U; index < LNI_CAPS_MAX_ACTIVE_SNAPSHOTS; ++index)
        if (connection->snapshots[index].active &&
            lxp_ct_memcmp(connection->snapshots[index].snapshot_id, snapshot_id, 32U) == 0)
            return &connection->snapshots[index];
    return NULL;
}

static lxp_result send_caps_discovery(
    lxp_daemon_lni_server *server, lni_caps_connection *connection,
    int descriptor, const lni_envelope *request, int64_t deadline)
{
    const uint8_t *payload = request->payload;
    uint8_t *page = NULL;
    size_t page_length = 0U;
    uint8_t released[3U + 32U];
    lni_caps_snapshot *snapshot = NULL;
    uint64_t now;
    size_t index;
    uint8_t op;
    lxp_result status;
    if (request->minor < 8U)
        return send_refusal(descriptor, server->frame_bytes,
                            request->correlation_id, 3U,
                            LXP_ERR_VERSION_UNSUPPORTED, deadline);
    if (request->correlation_id == 0U || request->proof_length != 0U ||
        request->payload_length < 3U || load_u16(payload) != LNI_CAPS_VERSION ||
        !connection->initialized)
        return send_refusal(descriptor, server->frame_bytes,
                            request->correlation_id, 1U,
                            LXP_ERR_MALFORMED_ENVELOPE, deadline);
    if (server->owner->protocol_version != LXP_PROTOCOL_VERSION_STATE_COMMITMENT ||
        server->owner->evidence_store == NULL)
        return evidence_refusal(server, descriptor, request->correlation_id,
                                LXP_ERR_MODULE_DISABLED, deadline);
    status = caps_now_ms(&now);
    if (status != LXP_OK)
        return evidence_refusal(server, descriptor, request->correlation_id,
                                status, deadline);
    caps_expire(connection, now);
    op = payload[2];
    if (op == LNI_CAPS_OP_OPEN) {
        uint16_t max_items;
        uint32_t max_bytes;
        if (request->payload_length != LNI_CAPS_OPEN_REQUEST_BYTES ||
            lxp_ct_is_zero(payload + 7U, 32U) || payload[39] > 4U)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_MALFORMED_ENVELOPE, deadline);
        max_items = load_u16(payload + 40U);
        max_bytes = load_u32(payload + 42U);
        if (max_items == 0U || max_items > LNI_CAPS_PAGE_MAX_ITEMS ||
            max_bytes < LNI_CAPS_PAGE_MIN_BYTES ||
            max_bytes > server->frame_bytes - LNI_ENVELOPE_FIXED_BYTES)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_LENGTH_LIMIT, deadline);
        for (index = 0U; index < LNI_CAPS_MAX_ACTIVE_SNAPSHOTS; ++index)
            if (!connection->snapshots[index].active) {
                snapshot = &connection->snapshots[index];
                break;
            }
        if (snapshot == NULL)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_LENGTH_LIMIT, deadline);
        status = caps_open_snapshot(server, connection, load_u32(payload + 3U),
                                    payload + 7U, payload[39], snapshot);
        if (status == LXP_ERR_LENGTH_LIMIT || status == LXP_ERR_CONTEXT_MISMATCH)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U, status, deadline);
        if (status != LXP_OK)
            return evidence_refusal(server, descriptor, request->correlation_id,
                                    status, deadline);
        if (snapshot->opened.length > max_bytes) {
            caps_snapshot_release(snapshot);
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_LENGTH_LIMIT, deadline);
        }
        return send_envelope(descriptor, server->frame_bytes,
                             LNI_CAPS_DISCOVERY_RESPONSE, request->correlation_id,
                             snapshot->opened.bytes, snapshot->opened.length,
                             NULL, 0U, deadline);
    }
    if (op == LNI_CAPS_OP_PAGE) {
        const uint8_t *cursor = payload + 3U;
        uint8_t expected[LNI_CAPS_CURSOR_BYTES];
        uint16_t max_items;
        uint32_t max_bytes;
        if (request->payload_length != LNI_CAPS_PAGE_REQUEST_BYTES ||
            load_u16(cursor) != LNI_CAPS_VERSION)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_MALFORMED_ENVELOPE, deadline);
        snapshot = caps_find(connection, cursor + 2U);
        if (snapshot == NULL)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 4U,
                                LXP_ERR_EXPIRED, deadline);
        max_items = load_u16(cursor + LNI_CAPS_CURSOR_BYTES);
        max_bytes = load_u32(cursor + LNI_CAPS_CURSOR_BYTES + 2U);
        if (max_items == 0U || max_items > LNI_CAPS_PAGE_MAX_ITEMS ||
            max_bytes < LNI_CAPS_PAGE_MIN_BYTES ||
            max_bytes > server->frame_bytes - LNI_ENVELOPE_FIXED_BYTES)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_LENGTH_LIMIT, deadline);
        if (snapshot->next_prefix == 0U)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_MALFORMED_ENVELOPE, deadline);
        caps_encode_cursor(snapshot, snapshot->next_prefix,
                           snapshot->next_position, expected);
        if (lxp_ct_memcmp(cursor, expected, sizeof(expected)) != 0)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 1U,
                                LXP_ERR_MALFORMED_ENVELOPE, deadline);
        status = caps_encode_page(snapshot, cursor, max_items, max_bytes,
                                  &page, &page_length);
        if (status != LXP_OK)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id,
                                status == LXP_ERR_LENGTH_LIMIT ? 1U : 4U,
                                status == LXP_FATAL_INVARIANT ?
                                    LXP_ERR_MODULE_DISABLED : status,
                                deadline);
        if (snapshot->next_prefix == 0U) caps_snapshot_release(snapshot);
        status = send_envelope(descriptor, server->frame_bytes,
                               LNI_CAPS_DISCOVERY_RESPONSE, request->correlation_id,
                               page, page_length, NULL, 0U, deadline);
        lxp_secure_zero(page, page_length);
        free(page);
        return status;
    }
    if (op == LNI_CAPS_OP_RELEASE &&
        request->payload_length == LNI_CAPS_RELEASE_REQUEST_BYTES) {
        snapshot = caps_find(connection, payload + 3U);
        if (snapshot == NULL)
            return send_refusal(descriptor, server->frame_bytes,
                                request->correlation_id, 4U,
                                LXP_ERR_EXPIRED, deadline);
        caps_snapshot_release(snapshot);
        caps_store_u16(released, LNI_CAPS_VERSION);
        released[2] = LNI_CAPS_OP_RELEASE;
        (void)memcpy(released + 3U, payload + 3U, 32U);
        return send_envelope(descriptor, server->frame_bytes,
                             LNI_CAPS_DISCOVERY_RESPONSE, request->correlation_id,
                             released, sizeof(released), NULL, 0U, deadline);
    }
    return send_refusal(descriptor, server->frame_bytes, request->correlation_id,
                        1U, LXP_ERR_MALFORMED_ENVELOPE, deadline);
}

#endif
