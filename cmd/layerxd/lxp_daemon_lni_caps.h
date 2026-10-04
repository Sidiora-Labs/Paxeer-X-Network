#ifndef LXP_DAEMON_LNI_CAPS_H
#define LXP_DAEMON_LNI_CAPS_H

#include "layerx/lxp_state_proof.h"
#include "layerx/lx_budget.h"
#include <openssl/rand.h>

enum {
    LNI_CAPS_REQUEST_BYTES = 177,
    LNI_CAPS_RESPONSE_BYTES = 119,
    LNI_CAPS_SELECTION_BYTES = 77,
    LNI_CAPS_MAX_OBJECTS = 4,
    LNI_CAPS_MAX_ITEMS = LXP_KERNEL_MAX_MODULE_KV + LXP_KERNEL_MAX_BLOBS,
    LNI_CAPS_MAX_UNIVERSAL = LXP_STATE_MAX_CELLS + LXP_STATE_MAX_IDEMPOTENCY +
        LXP_KERNEL_MAX_MODULE_REGISTRATIONS + 2,
    LNI_CAPS_MAX_ACCOUNTS = 8 * LXP_KERNEL_MAX_MODULE_KV,
    LNI_CAPS_MAX_BYTES = LXP_KERNEL_MAX_BLOB_TOTAL_BYTES
};

typedef struct lni_caps_snapshot {
    uint8_t selection[115];
    uint16_t profile;
    uint8_t identity[32];
    uint8_t root[32];
    uint8_t cursor[32];
    uint8_t *bytes;
    size_t length;
    size_t capacity;
    size_t next;
    int64_t expires;
} lni_caps_snapshot;

typedef struct lni_caps_key {
    uint8_t bytes[LXP_STATE_WITNESS_MAX_KEY];
    size_t length;
} lni_caps_key;

static pthread_mutex_t lni_caps_mutex = PTHREAD_MUTEX_INITIALIZER;
static size_t lni_caps_active;
static size_t lni_caps_retained;

static void lni_caps_drop(lni_caps_snapshot **snapshot)
{
    lni_caps_snapshot *value = *snapshot;
    if (value == NULL) return;
    (void)pthread_mutex_lock(&lni_caps_mutex);
    lni_caps_retained -= value->capacity;
    --lni_caps_active;
    (void)pthread_mutex_unlock(&lni_caps_mutex);
    free(value->bytes);
    lxp_secure_zero(value, sizeof(*value));
    free(value);
    *snapshot = NULL;
}

static lxp_result lni_caps_append(lni_caps_snapshot *snapshot,
                                   const void *bytes, size_t length)
{
    size_t required;
    size_t capacity;
    uint8_t *allocation;
    int64_t now;
    lxp_result status = monotonic_milliseconds(&now);
    if (status != LXP_OK) return status;
    if (now >= snapshot->expires) return LXP_ERR_EXPIRED;
    if (length > LNI_CAPS_MAX_BYTES - snapshot->length)
        return LXP_ERR_LENGTH_LIMIT;
    required = snapshot->length + length;
    if (required > snapshot->capacity) {
        capacity = snapshot->capacity == 0U ? 16384U : snapshot->capacity;
        while (capacity < required) {
            if (capacity > LNI_CAPS_MAX_BYTES / 2U) {
                capacity = LNI_CAPS_MAX_BYTES;
                break;
            }
            capacity *= 2U;
        }
        if (pthread_mutex_lock(&lni_caps_mutex) != 0) return LXP_ERR_IO;
        if (capacity - snapshot->capacity >
            LNI_CAPS_MAX_BYTES - lni_caps_retained) {
            (void)pthread_mutex_unlock(&lni_caps_mutex);
            return LXP_ERR_LENGTH_LIMIT;
        }
        allocation = realloc(snapshot->bytes, capacity);
        if (allocation == NULL) {
            (void)pthread_mutex_unlock(&lni_caps_mutex);
            return LXP_ERR_ARENA_EXHAUSTED;
        }
        lni_caps_retained += capacity - snapshot->capacity;
        snapshot->bytes = allocation;
        snapshot->capacity = capacity;
        (void)pthread_mutex_unlock(&lni_caps_mutex);
    }
    if (length != 0U) memcpy(snapshot->bytes + snapshot->length, bytes, length);
    snapshot->length = required;
    return LXP_OK;
}

static lxp_result lni_caps_integer(lni_caps_snapshot *snapshot,
                                    uint64_t value, size_t width)
{
    uint8_t bytes[8];
    size_t index;
    for (index = 0U; index < width; ++index)
        bytes[index] = (uint8_t)(value >> ((width - index - 1U) * 8U));
    return lni_caps_append(snapshot, bytes, width);
}

static lxp_result lni_caps_blob(lni_caps_snapshot *snapshot,
                                 const uint8_t *bytes, size_t length)
{
    lxp_result status;
    if (length > UINT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    status = lni_caps_integer(snapshot, length, 4U);
    return status == LXP_OK ? lni_caps_append(snapshot, bytes, length) : status;
}

static int lni_caps_key_compare(const void *left, const void *right)
{
    const lni_caps_key *a = left;
    const lni_caps_key *b = right;
    size_t length = a->length < b->length ? a->length : b->length;
    int order = memcmp(a->bytes, b->bytes, length);
    if (order != 0) return order;
    return a->length < b->length ? -1 : a->length > b->length ? 1 : 0;
}

static int lni_caps_account_compare(const void *left, const void *right)
{
    return memcmp(left, right, 32U);
}

static lxp_result lni_caps_account_add(uint8_t (*accounts)[32], size_t *count,
                                        const uint8_t id[32])
{
    size_t index;
    if (lxp_ct_is_zero(id, 32U)) return LXP_ERR_NON_CANONICAL;
    for (index = 0U; index < *count; ++index)
        if (memcmp(accounts[index], id, 32U) == 0) return LXP_OK;
    if (*count >= LNI_CAPS_MAX_ACCOUNTS) return LXP_ERR_LENGTH_LIMIT;
    memcpy(accounts[(*count)++], id, 32U);
    return LXP_OK;
}

static lxp_result lni_caps_record_accounts(const lxp_module_kv_entry *entry,
                                           uint8_t (*accounts)[32], size_t *count)
{
    lxp_result status = LXP_OK;
    if (entry->module_id == LXP_MODULE_BUDGET && entry->key_length >= 7U &&
        memcmp(entry->key, "budget:", 7U) == 0) {
        lx_budget_record record;
        if (entry->key_length != LX_BUDGET_STATE_KEY_BYTES)
            return LXP_ERR_NON_CANONICAL;
        status = lx_budget_record_decode(entry->value, entry->value_length, &record);
        if (status == LXP_OK && memcmp(record.budget_id, entry->key + 7U, 32U) != 0)
            status = LXP_ERR_NON_CANONICAL;
        if (status == LXP_OK) status = lni_caps_account_add(accounts, count, record.owner);
        if (status == LXP_OK) status = lni_caps_account_add(accounts, count, record.budget_account);
        if (status == LXP_OK && record.native_source)
            status = lni_caps_account_add(accounts, count, record.source_account);
    } else if (entry->module_id == LXP_MODULE_ASSET && entry->key_length >= 6U &&
               memcmp(entry->key, "grant:", 6U) == 0) {
        lxp_payer_grant grant;
        size_t length = entry->value_length;
        if (entry->key_length != 38U || length < 50U ||
            entry->value[length - 2U] > 1U || entry->value[length - 1U] > 1U)
            return LXP_ERR_NON_CANONICAL;
        status = lxp_payer_grant_decode(entry->value, length - 50U, &grant);
        if (status == LXP_OK && memcmp(grant.grant_id, entry->key + 6U, 32U) != 0)
            status = LXP_ERR_NON_CANONICAL;
        if (status == LXP_OK) status = lni_caps_account_add(accounts, count, grant.from);
        if (status == LXP_OK) status = lni_caps_account_add(accounts, count, grant.recipient);
    }
    return status;
}

static lxp_result lni_caps_witness(lni_caps_snapshot *snapshot,
                                    const lxp_kernel *kernel, uint16_t module,
                                    const lni_caps_key *key, uint32_t position,
                                    uint32_t count, bool module_leaf,
                                    lxp_state_witness *witness, uint8_t *wire)
{
    size_t length = 0U;
    lxp_result status = lxp_state_proof_build(kernel, module,
        (lxp_byte_span){key->bytes, key->length}, witness);
    if (status == LXP_OK) status = lxp_state_proof_verify(witness, snapshot->root);
    if (status == LXP_OK && module_leaf &&
        (witness->layer_a.leaf_index != position || witness->layer_a.leaf_count != count))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK && !module_leaf &&
        (witness->account_path.leaf_index != position || witness->account_path.leaf_count != count))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK)
        status = lxp_state_proof_encode(witness, wire, LXP_STATE_WITNESS_MAX_BYTES, &length);
    return status == LXP_OK ? lni_caps_blob(snapshot, wire, length) : status;
}

static lxp_result lni_caps_module(lni_caps_snapshot *snapshot,
                                   const lxp_kernel *kernel, uint16_t module,
                                   uint8_t (*accounts)[32], size_t *account_count,
                                   lxp_state_witness *witness, uint8_t *wire)
{
    lni_caps_key *keys;
    lxp_state_proof composite;
    uint8_t root[32];
    uint8_t subtree[32];
    size_t count = 0U;
    size_t index;
    lxp_result status = LXP_OK;
    keys = calloc(LNI_CAPS_MAX_UNIVERSAL, sizeof(*keys));
    if (keys == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    if (kernel->module_kv_count > LXP_KERNEL_MAX_MODULE_KV ||
        kernel->blob_count > LXP_KERNEL_MAX_BLOBS ||
        kernel->module_count > LXP_KERNEL_MAX_MODULE_REGISTRATIONS ||
        kernel->state->count > LXP_STATE_MAX_CELLS ||
        kernel->state->idempotency_count > LXP_STATE_MAX_IDEMPOTENCY)
        status = LXP_ERR_LENGTH_LIMIT;
    if (status == LXP_OK && module == 0U) {
        for (index = 0U; index < kernel->state->count; ++index) {
            keys[count].length = 33U;
            keys[count].bytes[0] = 1U;
            memcpy(keys[count++].bytes + 1U, kernel->state->cells[index].key, 32U);
        }
        for (index = 0U; index < kernel->state->idempotency_count; ++index) {
            keys[count].length = 33U;
            keys[count].bytes[0] = 2U;
            memcpy(keys[count++].bytes + 1U, kernel->state->idempotency[index].key_hash, 32U);
        }
        for (index = 0U; index < kernel->module_count; ++index) {
            keys[count].length = 7U;
            keys[count].bytes[0] = 3U;
            store_u16(keys[count].bytes + 1U, kernel->modules[index].module_id);
            store_u32(keys[count++].bytes + 3U, kernel->modules[index].abi_version);
        }
        if (kernel->state->account_root_required) {
            keys[count].length = 12U;
            memcpy(keys[count++].bytes, "account-tree", 12U);
        }
        keys[count].length = 8U;
        memcpy(keys[count++].bytes, "sequence", 8U);
    }
    for (index = 0U; module != 0U && status == LXP_OK && index < kernel->module_kv_count; ++index) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[index];
        if (entry->module_id != module) continue;
        if (entry->key_length == 0U || entry->key_length > LXP_MODULE_MAX_KEY_BYTES ||
            entry->value_length > LXP_MODULE_MAX_VALUE_BYTES || count == LNI_CAPS_MAX_ITEMS) {
            status = LXP_ERR_NON_CANONICAL;
            break;
        }
        keys[count].length = entry->key_length;
        memcpy(keys[count++].bytes, entry->key, entry->key_length);
        status = lni_caps_record_accounts(entry, accounts, account_count);
    }
    for (index = 0U; module != 0U && status == LXP_OK && index < kernel->blob_count; ++index) {
        const lxp_module_blob *blob = &kernel->blobs[index];
        if (blob->module_id != module) continue;
        if (count == LNI_CAPS_MAX_ITEMS) { status = LXP_ERR_LENGTH_LIMIT; break; }
        keys[count].length = LXP_STATE_WITNESS_MAX_KEY;
        keys[count].bytes[0] = 0xffU;
        memcpy(keys[count].bytes + LXP_STATE_WITNESS_MAX_KEY - 32U, blob->key, 32U);
        ++count;
    }
    if (status == LXP_OK) {
        qsort(keys, count, sizeof(*keys), lni_caps_key_compare);
        for (index = 1U; index < count; ++index)
            if (lni_caps_key_compare(&keys[index - 1U], &keys[index]) >= 0)
                status = LXP_ERR_NON_CANONICAL;
    }
    if (status == LXP_OK) status = lxp_state_subtree_root(kernel, module, subtree);
    if (status == LXP_OK) status = lxp_state_root_proof(kernel, module, root, &composite);
    if (status == LXP_OK && (memcmp(root, snapshot->root, 32U) != 0 ||
        composite.leaf_index != module || composite.depth > LXP_STATE_PROOF_MAX_DEPTH))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK) status = lni_caps_integer(snapshot, module, 2U);
    if (status == LXP_OK) status = lni_caps_append(snapshot, subtree, sizeof(subtree));
    if (status == LXP_OK) status = lni_caps_integer(snapshot, composite.leaf_index, 4U);
    if (status == LXP_OK) status = lni_caps_integer(snapshot, composite.leaf_count, 4U);
    if (status == LXP_OK) status = lni_caps_integer(snapshot, composite.depth, 1U);
    if (status == LXP_OK) status = lni_caps_append(snapshot, composite.siblings, 32U * composite.depth);
    if (status == LXP_OK) status = lni_caps_integer(snapshot, count, 4U);
    for (index = 0U; status == LXP_OK && index < count; ++index)
        status = lni_caps_witness(snapshot, kernel, module, &keys[index],
            (uint32_t)index, (uint32_t)count, true, witness, wire);
    free(keys);
    return status;
}

static lxp_result lni_caps_capture(lxp_daemon_lni_server *server,
                                    const lni_envelope *request,
                                    lni_caps_snapshot *snapshot)
{
    lxp_daemon_protocol_owner *owner = server->owner;
    lxp_daemon_receipt_evidence head;
    lxp_daemon_signed_header_evidence signed_header;
    lxp_batch_header header;
    lxp_state_proof composite;
    uint8_t subtree[32];
    uint8_t composite_root[32];
    lxp_state_witness *witness = NULL;
    uint8_t *wire = NULL;
    uint8_t (*accounts)[32] = NULL;
    lxp_byte_span value;
    lxp_byte_span proof;
    uint64_t epoch;
    uint8_t selector = request->payload[39U];
    uint64_t batch = selector == 2U ? load_u64(request->payload + 40U) : 0U;
    const uint8_t *checkpoint = selector == 3U ? request->payload + 40U : NULL;
    size_t account_count = 0U;
    size_t index;
    size_t mark;
    lxp_result status = lni_read_lock(owner);
    if (status != LXP_OK) return status;
    mark = lxp_arena_mark(owner->scratch);
    if (owner->kernel->state == NULL || owner->kernel->state->accounts == NULL ||
        !owner->kernel->state->account_root_required)
        status = LXP_ERR_MODULE_DISABLED;
    else if (owner->kernel->state->accounts->count > LNI_CAPS_MAX_ACCOUNTS)
        status = LXP_ERR_LENGTH_LIMIT;
    if (status == LXP_OK) status = latest_receipt_evidence(owner, owner->scratch, &head);
    if (status == LXP_OK) {
        signed_header.authorization = owner->evidence_store->authorization;
        signed_header.canonical_header = head.canonical_header;
        memcpy(signed_header.signature, head.header_signature, 64U);
        status = lxp_batch_header_decode(head.canonical_header.bytes, head.canonical_header.length, &header);
        if (status == LXP_OK && owner->evidence_store->handover_chain != NULL) {
            status = lxp_handover_trust_authorization(owner->evidence_store->handover_chain,
                header.batch_number, &signed_header.authorization, &epoch);
            if (status == LXP_OK && header.epoch != epoch) status = LXP_ERR_AUTH_SCOPE;
        }
    }
    if (status == LXP_OK)
        status = lxp_daemon_module_evidence_wire_encode(owner->evidence_store,
            owner->kernel, &signed_header, 0U,
            (lxp_byte_span){(const uint8_t *)"sequence", 8U}, selector, batch, checkpoint,
            request->payload[72U], owner->scratch, &value, &proof);
    if (status == LXP_OK) {
        memcpy(snapshot->root, header.resulting_state_root, 32U);
        status = lni_caps_integer(snapshot, 1U, 2U);
    }
    if (status == LXP_OK) status = lni_caps_blob(snapshot, value.bytes, value.length);
    if (status == LXP_OK) status = lni_caps_blob(snapshot, proof.bytes, proof.length);
    if (status == LXP_OK)
        status = lxp_state_root_proof(owner->kernel, 0U, composite_root, &composite);
    if (status == LXP_OK && (memcmp(composite_root, snapshot->root, 32U) != 0 ||
        composite.leaf_count > LXP_MODULE_RESERVED_COUNT + 1U || composite.leaf_count < 9U))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK) status = lni_caps_integer(snapshot, composite.leaf_count, 2U);
    for (index = 0U; status == LXP_OK && index < composite.leaf_count; ++index) {
        status = lxp_state_subtree_root(owner->kernel, (uint16_t)index, subtree);
        if (status == LXP_OK) status = lni_caps_append(snapshot, subtree, 32U);
    }
    if (status == LXP_OK && (owner->kernel->state->accounts == NULL ||
        !owner->kernel->state->account_root_required ||
        owner->kernel->state->accounts->count > LNI_CAPS_MAX_ACCOUNTS))
        status = LXP_ERR_LENGTH_LIMIT;
    if (status == LXP_OK) {
        witness = malloc(sizeof(*witness));
        wire = malloc(LXP_STATE_WITNESS_MAX_BYTES);
        accounts = calloc(LNI_CAPS_MAX_ACCOUNTS, 32U);
        if (witness == NULL || wire == NULL || accounts == NULL)
            status = LXP_ERR_ARENA_EXHAUSTED;
    }
    if (status == LXP_OK) status = lni_caps_module(snapshot, owner->kernel,
        0U, accounts, &account_count, witness, wire);
    if (status == LXP_OK) status = lni_caps_integer(snapshot, 2U, 1U);
    if (status == LXP_OK) status = lni_caps_module(snapshot, owner->kernel,
        LXP_MODULE_BUDGET, accounts, &account_count, witness, wire);
    if (status == LXP_OK) status = lni_caps_module(snapshot, owner->kernel,
        LXP_MODULE_ASSET, accounts, &account_count, witness, wire);
    for (index = 0U; status == LXP_OK && index < account_count; ++index) {
        size_t slot;
        status = lx_account_registry_index_lookup(owner->kernel->state->accounts, accounts[index], &slot);
    }
    if (status == LXP_OK) {
        account_count = owner->kernel->state->accounts->count;
        for (index = 0U; index < account_count; ++index)
            memcpy(accounts[index], owner->kernel->state->accounts->accounts[index].id, 32U);
        qsort(accounts, account_count, 32U, lni_caps_account_compare);
        for (index = 1U; index < account_count; ++index)
            if (memcmp(accounts[index - 1U], accounts[index], 32U) >= 0)
                status = LXP_ERR_NON_CANONICAL;
        if (status == LXP_OK) status = lni_caps_integer(snapshot, account_count, 4U);
    }
    for (index = 0U; status == LXP_OK && index < account_count; ++index) {
        lni_caps_key key;
        key.length = 33U;
        key.bytes[0] = 4U;
        memcpy(key.bytes + 1U, accounts[index], 32U);
        status = lni_caps_witness(snapshot, owner->kernel, 0U, &key,
            (uint32_t)index, (uint32_t)account_count, false, witness, wire);
    }
    free(accounts);
    free(wire);
    free(witness);
    if (lxp_arena_reset(owner->scratch, mark) != LXP_OK && status == LXP_OK)
        status = LXP_FATAL_INVARIANT;
    return lni_read_unlock(owner, status);
}

static lxp_result send_caps_discovery(lxp_daemon_lni_server *server, int descriptor,
                                       const lni_envelope *request,
                                       lni_caps_snapshot **retained, int64_t deadline)
{
    lni_caps_snapshot *snapshot;
    uint8_t *response = NULL;
    uint8_t next_cursor[32] = {0};
    uint32_t maximum = 0U;
    size_t length;
    size_t next;
    int64_t now;
    bool done;
    lxp_result status = LXP_OK;
    if (request->minor < 8U) status = LXP_ERR_VERSION_UNSUPPORTED;
    else if (request->proof_length != 0U || request->correlation_id == 0U ||
        request->payload_length != LNI_CAPS_REQUEST_BYTES ||
        load_u16(request->payload) != 1U || request->payload[2U] > 1U)
        status = LXP_ERR_MALFORMED_ENVELOPE;
    if (status == LXP_OK && (load_u32(request->payload + 3U) != server->owner->network_id ||
        lxp_ct_is_zero(request->payload + 7U, 32U) || request->payload[72U] > 4U))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK) {
        uint8_t selector = request->payload[39U];
        if ((selector == 1U && !lxp_ct_is_zero(request->payload + 40U, 32U)) ||
            (selector == 2U && (load_u64(request->payload + 40U) == 0U ||
                !lxp_ct_is_zero(request->payload + 48U, 24U))) ||
            (selector == 3U && lxp_ct_is_zero(request->payload + 40U, 32U)) ||
            selector < 1U || selector > 3U)
            status = LXP_ERR_MALFORMED_ENVELOPE;
        maximum = load_u32(request->payload + 73U);
        if (maximum == 0U || maximum > LXP_KERNEL_MAX_BLOB_BYTES ||
            server->frame_bytes <= LNI_ENVELOPE_FIXED_BYTES + LNI_CAPS_RESPONSE_BYTES ||
            maximum > server->frame_bytes - LNI_ENVELOPE_FIXED_BYTES - LNI_CAPS_RESPONSE_BYTES)
            status = LXP_ERR_LENGTH_LIMIT;
    }
    if (status == LXP_OK &&
        (server->owner->protocol_version != LXP_PROTOCOL_VERSION_STATE_COMMITMENT ||
         server->owner->evidence_store == NULL || server->owner->receipt_authority == NULL ||
         server->owner->kernel == NULL || server->owner->scratch == NULL))
        status = LXP_ERR_MODULE_DISABLED;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && request->payload[2U] == 0U) {
        lni_caps_drop(retained);
        if (!lxp_ct_is_zero(request->payload + 77U, 100U))
            status = LXP_ERR_MALFORMED_ENVELOPE;
        if (status == LXP_OK) {
            if (pthread_mutex_lock(&lni_caps_mutex) != 0) status = LXP_ERR_IO;
            else {
                if (lni_caps_active >= LNI_CAPS_MAX_OBJECTS) status = LXP_ERR_LENGTH_LIMIT;
                else {
                    *retained = calloc(1U, sizeof(**retained));
                    if (*retained == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
                    else ++lni_caps_active;
                }
                (void)pthread_mutex_unlock(&lni_caps_mutex);
            }
        }
        if (status == LXP_OK) {
            snapshot = *retained;
            memcpy(snapshot->selection, request->payload, LNI_CAPS_SELECTION_BYTES);
            snapshot->expires = deadline;
            if (RAND_bytes(snapshot->identity, 32) != 1 ||
                lxp_ct_is_zero(snapshot->identity, 32U)) status = LXP_ERR_IO;
            if (status == LXP_OK) status = lni_caps_capture(server, request, snapshot);
        }
    } else if (status == LXP_OK) {
        snapshot = *retained;
        if (snapshot == NULL) status = LXP_ERR_CONTEXT_MISMATCH;
        else if (now >= snapshot->expires) status = LXP_ERR_EXPIRED;
        else if (memcmp(request->payload + 3U, snapshot->selection + 3U,
                    LNI_CAPS_SELECTION_BYTES - 3U) != 0 ||
            memcmp(request->payload + 77U, snapshot->identity, 32U) != 0 ||
            memcmp(request->payload + 109U, snapshot->root, 32U) != 0 ||
            load_u32(request->payload + 141U) != snapshot->next ||
            lxp_ct_memcmp(request->payload + 145U, snapshot->cursor, 32U) != 0)
            status = LXP_ERR_CONTEXT_MISMATCH;
    }
    snapshot = *retained;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && now >= snapshot->expires) status = LXP_ERR_EXPIRED;
    if (status != LXP_OK) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    length = snapshot->length - snapshot->next;
    if (length > maximum) length = maximum;
    if (length == 0U) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id,
            LXP_ERR_CONTEXT_MISMATCH, deadline);
    }
    next = snapshot->next + length;
    done = next == snapshot->length;
    if (!done && (RAND_bytes(next_cursor, 32) != 1 || lxp_ct_is_zero(next_cursor, 32U)))
        status = LXP_ERR_IO;
    if (status == LXP_OK) {
        response = malloc(LNI_CAPS_RESPONSE_BYTES + length);
        if (response == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    }
    if (status != LXP_OK) {
        free(response);
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    if (status == LXP_OK) {
        store_u16(response, 1U);
        memcpy(response + 2U, snapshot->identity, 32U);
        store_u32(response + 34U, load_u32(snapshot->selection + 3U));
        memcpy(response + 38U, snapshot->root, 32U);
        store_u32(response + 70U, (uint32_t)snapshot->next);
        store_u32(response + 74U, (uint32_t)snapshot->length);
        store_u32(response + 78U, (uint32_t)next);
        response[82U] = done ? 1U : 0U;
        memcpy(response + 83U, next_cursor, 32U);
        store_u32(response + 115U, (uint32_t)length);
        memcpy(response + LNI_CAPS_RESPONSE_BYTES, snapshot->bytes + snapshot->next, length);
        if (deadline > snapshot->expires) deadline = snapshot->expires;
        status = send_envelope(descriptor, server->frame_bytes, LNI_CAPS_DISCOVERY_RESPONSE,
            request->correlation_id, response, LNI_CAPS_RESPONSE_BYTES + length,
            NULL, 0U, deadline);
    }
    free(response);
    if (status != LXP_OK || done) lni_caps_drop(retained);
    else {
        snapshot->next = next;
        memcpy(snapshot->cursor, next_cursor, 32U);
    }
    return status;
}

typedef struct lni_execution_prestate_sink {
    const lni_envelope *request;
    lni_caps_snapshot *snapshot;
} lni_execution_prestate_sink;

static lxp_result lni_execution_prestate_consume(void *context, lxp_byte_span payload)
{
    lni_execution_prestate_sink *sink = context;
    const lni_envelope *request = sink->request;
    lni_caps_snapshot *snapshot = sink->snapshot;
    if (payload.bytes == NULL || payload.length < 80U ||
        payload.length > LNI_CAPS_MAX_BYTES)
        return LXP_ERR_LENGTH_LIMIT;
    if (snapshot->length != 0U || load_u16(payload.bytes) != 1U ||
        load_u32(payload.bytes + 2U) != load_u32(request->payload + 3U) ||
        memcmp(payload.bytes + 6U, request->payload + 7U, 32U) != 0 ||
        load_u64(payload.bytes + 38U) == 0U ||
        lxp_ct_is_zero(payload.bytes + 46U, 32U))
        return LXP_ERR_CONTEXT_MISMATCH;
    memcpy(snapshot->root, payload.bytes + 46U, 32U);
    return lni_caps_append(snapshot, payload.bytes, payload.length);
}

static lxp_result lni_execution_prestate_capture(lxp_daemon_lni_server *server,
                                                 const lni_envelope *request,
                                                 lni_caps_snapshot *snapshot)
{
    lxp_daemon_protocol_owner *owner = server->owner;
    lni_execution_prestate_sink sink = {request, snapshot};
    size_t mark;
    lxp_result status = lni_read_lock(owner);
    if (status != LXP_OK) return status;
    mark = lxp_arena_mark(owner->scratch);
    status = lxp_daemon_evidence_get_execution_prestate(owner->evidence_store,
        load_u32(request->payload + 3U), request->payload + 7U,
        request->payload + 39U, owner->scratch,
        lni_execution_prestate_consume, &sink);
    if (status == LXP_OK && snapshot->length == 0U)
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (lxp_arena_reset(owner->scratch, mark) != LXP_OK && status == LXP_OK)
        status = LXP_FATAL_INVARIANT;
    return lni_read_unlock(owner, status);
}

static lxp_result send_execution_prestate_discovery(lxp_daemon_lni_server *server, int descriptor,
                                       const lni_envelope *request,
                                       lni_caps_snapshot **retained, int64_t deadline)
{
    lni_caps_snapshot *snapshot;
    uint8_t *response = NULL;
    uint8_t next_cursor[32] = {0};
    uint32_t maximum = 0U;
    size_t length;
    size_t next;
    int64_t now;
    bool done;
    lxp_result status = LXP_OK;
    if (request->minor < LNI_EXECUTION_PRESTATE_MINOR ||
        lni_reply_minor < LNI_EXECUTION_PRESTATE_MINOR) status = LXP_ERR_VERSION_UNSUPPORTED;
    else if (request->proof_length != 0U || request->correlation_id == 0U ||
        request->payload_length != LNI_CAPS_REQUEST_BYTES ||
        load_u16(request->payload) != 2U || request->payload[2U] > 1U ||
        !lxp_ct_is_zero(request->payload + 175U, 2U))
        status = LXP_ERR_MALFORMED_ENVELOPE;
    if (status == LXP_OK && (load_u32(request->payload + 3U) != server->owner->network_id ||
        lxp_ct_is_zero(request->payload + 7U, 32U) ||
        lxp_ct_is_zero(request->payload + 39U, 32U)))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK) {
        maximum = load_u32(request->payload + 71U);
        if (maximum == 0U || maximum > LXP_KERNEL_MAX_BLOB_BYTES ||
            server->frame_bytes <= LNI_ENVELOPE_FIXED_BYTES + LNI_CAPS_RESPONSE_BYTES ||
            maximum > server->frame_bytes - LNI_ENVELOPE_FIXED_BYTES - LNI_CAPS_RESPONSE_BYTES)
            status = LXP_ERR_LENGTH_LIMIT;
    }
    if (status == LXP_OK && !execution_prestate_available(server))
        status = LXP_ERR_MODULE_DISABLED;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && request->payload[2U] == 0U) {
        lni_caps_drop(retained);
        if (!lxp_ct_is_zero(request->payload + 75U, 100U))
            status = LXP_ERR_MALFORMED_ENVELOPE;
        if (status == LXP_OK) {
            if (pthread_mutex_lock(&lni_caps_mutex) != 0) status = LXP_ERR_IO;
            else {
                if (lni_caps_active >= LNI_CAPS_MAX_OBJECTS) status = LXP_ERR_LENGTH_LIMIT;
                else {
                    *retained = calloc(1U, sizeof(**retained));
                    if (*retained == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
                    else ++lni_caps_active;
                }
                (void)pthread_mutex_unlock(&lni_caps_mutex);
            }
        }
        if (status == LXP_OK) {
            snapshot = *retained;
            snapshot->profile = LNI_EXECUTION_PRESTATE_REQUEST;
            memcpy(snapshot->selection, request->payload, 75U);
            snapshot->expires = deadline;
            if (RAND_bytes(snapshot->identity, 32) != 1 ||
                lxp_ct_is_zero(snapshot->identity, 32U)) status = LXP_ERR_IO;
            if (status == LXP_OK) status = lni_execution_prestate_capture(server, request, snapshot);
        }
    } else if (status == LXP_OK) {
        snapshot = *retained;
        if (snapshot == NULL) status = LXP_ERR_CONTEXT_MISMATCH;
        else if (now >= snapshot->expires) status = LXP_ERR_EXPIRED;
        else if (snapshot->profile != LNI_EXECUTION_PRESTATE_REQUEST ||
            load_u16(snapshot->selection) != 2U ||
            memcmp(request->payload + 3U, snapshot->selection + 3U, 72U) != 0 ||
            memcmp(request->payload + 75U, snapshot->identity, 32U) != 0 ||
            memcmp(request->payload + 107U, snapshot->root, 32U) != 0 ||
            load_u32(request->payload + 139U) != snapshot->next ||
            lxp_ct_memcmp(request->payload + 143U, snapshot->cursor, 32U) != 0)
            status = LXP_ERR_CONTEXT_MISMATCH;
    }
    snapshot = *retained;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && now >= snapshot->expires) status = LXP_ERR_EXPIRED;
    if (status != LXP_OK) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    length = snapshot->length - snapshot->next;
    if (length > maximum) length = maximum;
    if (length == 0U) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id,
            LXP_ERR_CONTEXT_MISMATCH, deadline);
    }
    next = snapshot->next + length;
    done = next == snapshot->length;
    if (!done && (RAND_bytes(next_cursor, 32) != 1 || lxp_ct_is_zero(next_cursor, 32U)))
        status = LXP_ERR_IO;
    if (status == LXP_OK) {
        response = malloc(LNI_CAPS_RESPONSE_BYTES + length);
        if (response == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    }
    if (status != LXP_OK) {
        free(response);
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    if (status == LXP_OK) {
        store_u16(response, 2U);
        memcpy(response + 2U, snapshot->identity, 32U);
        store_u32(response + 34U, load_u32(snapshot->selection + 3U));
        memcpy(response + 38U, snapshot->root, 32U);
        store_u32(response + 70U, (uint32_t)snapshot->next);
        store_u32(response + 74U, (uint32_t)snapshot->length);
        store_u32(response + 78U, (uint32_t)next);
        response[82U] = done ? 1U : 0U;
        memcpy(response + 83U, next_cursor, 32U);
        store_u32(response + 115U, (uint32_t)length);
        memcpy(response + LNI_CAPS_RESPONSE_BYTES, snapshot->bytes + snapshot->next, length);
        if (deadline > snapshot->expires) deadline = snapshot->expires;
        status = send_envelope(descriptor, server->frame_bytes, LNI_EXECUTION_PRESTATE_RESPONSE,
            request->correlation_id, response, LNI_CAPS_RESPONSE_BYTES + length,
            NULL, 0U, deadline);
    }
    free(response);
    if (status != LXP_OK || done) lni_caps_drop(retained);
    else {
        snapshot->next = next;
        memcpy(snapshot->cursor, next_cursor, 32U);
    }
    return status;
}

static lxp_result lni_arbiter_prestate_consume(void *context, lxp_byte_span payload)
{
    lni_execution_prestate_sink *sink = context;
    const lni_envelope *request = sink->request;
    lni_caps_snapshot *snapshot = sink->snapshot;
    uint32_t legacy_length;
    if (payload.bytes == NULL || payload.length < 86U || payload.length > LNI_CAPS_MAX_BYTES ||
        load_u16(payload.bytes) != 2U) return LXP_ERR_LENGTH_LIMIT;
    legacy_length = load_u32(payload.bytes + 2U);
    if (legacy_length < 80U || legacy_length > payload.length - 6U || snapshot->length != 0U ||
        load_u16(payload.bytes + 6U) != 1U ||
        load_u32(payload.bytes + 8U) != load_u32(request->payload + 3U) ||
        memcmp(payload.bytes + 12U, request->payload + 7U, 32U) != 0 ||
        load_u64(payload.bytes + 44U) != load_u64(request->payload + 71U) ||
        memcmp(payload.bytes + 52U, request->payload + 79U, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    memcpy(snapshot->root, payload.bytes + 52U, 32U);
    return lni_caps_append(snapshot, payload.bytes, payload.length);
}

static lxp_result lni_arbiter_prestate_capture(lxp_daemon_lni_server *server,
                                               const lni_envelope *request,
                                               lni_caps_snapshot *snapshot)
{
    lxp_daemon_protocol_owner *owner = server->owner;
    lni_execution_prestate_sink sink = {request, snapshot};
    size_t mark;
    lxp_result status = lni_read_lock(owner);
    if (status != LXP_OK) return status;
    mark = lxp_arena_mark(owner->scratch);
    status = lxp_daemon_evidence_get_arbiter_prestate(owner->evidence_store,
        load_u32(request->payload + 3U), request->payload + 7U, request->payload + 39U,
        load_u64(request->payload + 71U), request->payload + 79U, owner->scratch,
        lni_arbiter_prestate_consume, &sink);
    if (status == LXP_OK && snapshot->length == 0U) status = LXP_ERR_CONTEXT_MISMATCH;
    if (lxp_arena_reset(owner->scratch, mark) != LXP_OK && status == LXP_OK)
        status = LXP_FATAL_INVARIANT;
    return lni_read_unlock(owner, status);
}

static lxp_result send_arbiter_prestate_discovery(lxp_daemon_lni_server *server, int descriptor,
                                       const lni_envelope *request,
                                       lni_caps_snapshot **retained, int64_t deadline)
{
    lni_caps_snapshot *snapshot;
    uint8_t *response = NULL;
    uint8_t next_cursor[32] = {0};
    uint32_t maximum = 0U;
    size_t length;
    size_t next;
    int64_t now;
    bool done;
    lxp_result status = LXP_OK;
    if (request->minor < LNI_ARBITER_PRESTATE_MINOR ||
        lni_reply_minor < LNI_ARBITER_PRESTATE_MINOR) status = LXP_ERR_VERSION_UNSUPPORTED;
    else if (request->proof_length != 0U || request->correlation_id == 0U ||
        request->payload_length != 187U ||
        load_u16(request->payload) != 2U || request->payload[2U] > 1U)
        status = LXP_ERR_MALFORMED_ENVELOPE;
    if (status == LXP_OK && (load_u32(request->payload + 3U) != server->owner->network_id ||
        lxp_ct_is_zero(request->payload + 7U, 32U) ||
        lxp_ct_is_zero(request->payload + 39U, 32U) ||
        load_u64(request->payload + 71U) == 0U ||
        lxp_ct_is_zero(request->payload + 79U, 32U)))
        status = LXP_ERR_CONTEXT_MISMATCH;
    if (status == LXP_OK) {
        maximum = load_u32(request->payload + 111U);
        if (maximum == 0U || maximum > LXP_KERNEL_MAX_BLOB_BYTES ||
            server->frame_bytes <= LNI_ENVELOPE_FIXED_BYTES + 191U ||
            maximum > server->frame_bytes - LNI_ENVELOPE_FIXED_BYTES - 191U)
            status = LXP_ERR_LENGTH_LIMIT;
    }
    if (status == LXP_OK && !arbiter_prestate_available(server))
        status = LXP_ERR_MODULE_DISABLED;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && request->payload[2U] == 0U) {
        lni_caps_drop(retained);
        if (!lxp_ct_is_zero(request->payload + 115U, 72U))
            status = LXP_ERR_MALFORMED_ENVELOPE;
        if (status == LXP_OK) {
            if (pthread_mutex_lock(&lni_caps_mutex) != 0) status = LXP_ERR_IO;
            else {
                if (lni_caps_active >= LNI_CAPS_MAX_OBJECTS) status = LXP_ERR_LENGTH_LIMIT;
                else {
                    *retained = calloc(1U, sizeof(**retained));
                    if (*retained == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
                    else ++lni_caps_active;
                }
                (void)pthread_mutex_unlock(&lni_caps_mutex);
            }
        }
        if (status == LXP_OK) {
            snapshot = *retained;
            snapshot->profile = LNI_ARBITER_PRESTATE_REQUEST;
            memcpy(snapshot->selection, request->payload, 115U);
            snapshot->expires = deadline;
            if (RAND_bytes(snapshot->identity, 32) != 1 ||
                lxp_ct_is_zero(snapshot->identity, 32U)) status = LXP_ERR_IO;
            if (status == LXP_OK) status = lni_arbiter_prestate_capture(server, request, snapshot);
        }
    } else if (status == LXP_OK) {
        snapshot = *retained;
        if (snapshot == NULL) status = LXP_ERR_CONTEXT_MISMATCH;
        else if (now >= snapshot->expires) status = LXP_ERR_EXPIRED;
        else if (snapshot->profile != LNI_ARBITER_PRESTATE_REQUEST ||
            load_u16(snapshot->selection) != 2U ||
            memcmp(request->payload + 3U, snapshot->selection + 3U, 112U) != 0 ||
            memcmp(request->payload + 115U, snapshot->identity, 32U) != 0 ||
            load_u32(request->payload + 147U) != snapshot->next ||
            load_u32(request->payload + 183U) != snapshot->length ||
            lxp_ct_memcmp(request->payload + 151U, snapshot->cursor, 32U) != 0)
            status = LXP_ERR_CONTEXT_MISMATCH;
    }
    snapshot = *retained;
    if (status == LXP_OK) status = monotonic_milliseconds(&now);
    if (status == LXP_OK && now >= snapshot->expires) status = LXP_ERR_EXPIRED;
    if (status != LXP_OK) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    length = snapshot->length - snapshot->next;
    if (length > maximum) length = maximum;
    if (length == 0U) {
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id,
            LXP_ERR_CONTEXT_MISMATCH, deadline);
    }
    next = snapshot->next + length;
    done = next == snapshot->length;
    if (!done && (RAND_bytes(next_cursor, 32) != 1 || lxp_ct_is_zero(next_cursor, 32U)))
        status = LXP_ERR_IO;
    if (status == LXP_OK) {
        response = malloc(191U + length);
        if (response == NULL) status = LXP_ERR_ARENA_EXHAUSTED;
    }
    if (status != LXP_OK) {
        free(response);
        lni_caps_drop(retained);
        return evidence_refusal(server, descriptor, request->correlation_id, status, deadline);
    }
    if (status == LXP_OK) {
        store_u16(response, 2U);
        memcpy(response + 2U, snapshot->identity, 32U);
        store_u32(response + 34U, load_u32(snapshot->selection + 3U));
        memcpy(response + 38U, snapshot->root, 32U);
        store_u32(response + 70U, (uint32_t)snapshot->next);
        store_u32(response + 74U, (uint32_t)snapshot->length);
        store_u32(response + 78U, (uint32_t)next);
        response[82U] = done ? 1U : 0U;
        memcpy(response + 83U, next_cursor, 32U);
        store_u32(response + 115U, (uint32_t)length);
        memcpy(response + 119U, snapshot->selection + 7U, 32U);
        memcpy(response + 151U, snapshot->selection + 39U, 32U);
        store_u64(response + 183U, load_u64(snapshot->selection + 71U));
        memcpy(response + 191U, snapshot->bytes + snapshot->next, length);
        if (deadline > snapshot->expires) deadline = snapshot->expires;
        status = send_envelope(descriptor, server->frame_bytes, LNI_ARBITER_PRESTATE_RESPONSE,
            request->correlation_id, response, 191U + length,
            NULL, 0U, deadline);
    }
    free(response);
    if (status != LXP_OK || done) lni_caps_drop(retained);
    else {
        snapshot->next = next;
        memcpy(snapshot->cursor, next_cursor, 32U);
    }
    return status;
}

#endif
