#include "layerx/lxp_state.h"

#include "layerx/lxp_hash.h"

#include <stdlib.h>
#include <string.h>

static lxp_result key_hash(const uint8_t *actor_did, size_t actor_did_length,
                           const uint8_t idempotency_key[32], uint8_t hash[32])
{
    uint8_t input[4U + LXP_MAX_DID_LENGTH + 32U];
    if ((actor_did == NULL && actor_did_length != 0U) ||
        actor_did_length > LXP_MAX_DID_LENGTH || idempotency_key == NULL)
        return LXP_ERR_NON_CANONICAL;
    input[0] = (uint8_t)(actor_did_length >> 24U);
    input[1] = (uint8_t)(actor_did_length >> 16U);
    input[2] = (uint8_t)(actor_did_length >> 8U);
    input[3] = (uint8_t)actor_did_length;
    if (actor_did_length != 0U)
        (void)memcpy(input + 4U, actor_did, actor_did_length);
    (void)memcpy(input + 4U + actor_did_length, idempotency_key, 32U);
    return lxp_hash_context_value(input, 4U + actor_did_length + 32U, hash);
}

static size_t find_entry(const lxp_state_store *store, const uint8_t hash[32])
{
    size_t i;
    for (i = 0U; i < store->idempotency_count; ++i)
        if (memcmp(store->idempotency[i].key_hash, hash, 32U) == 0) return i;
    return store->idempotency_count;
}

lxp_result lxp_idempotency_lookup(lxp_state_store *store,
                                  const uint8_t *actor_did,
                                  size_t actor_did_length,
                                  const uint8_t idempotency_key[32],
                                  const uint8_t **receipt,
                                  size_t *receipt_length)
{
    uint8_t hash[32];
    size_t location;
    lxp_result status;
    if (store == NULL || receipt == NULL || receipt_length == NULL)
        return LXP_ERR_NON_CANONICAL;
    status = key_hash(actor_did, actor_did_length, idempotency_key, hash);
    if (status != LXP_OK) return status;
    if (pthread_mutex_lock(&store->lock) != 0) return LXP_ERR_IO;
    location = find_entry(store, hash);
    if (location == store->idempotency_count) {
        *receipt = NULL;
        *receipt_length = 0U;
        (void)pthread_mutex_unlock(&store->lock);
        return LXP_OK;
    }
    *receipt = store->idempotency[location].receipt;
    *receipt_length = store->idempotency[location].receipt_length;
    if (pthread_mutex_unlock(&store->lock) != 0) return LXP_FATAL_INVARIANT;
    return LXP_ERR_IDEMPOTENT_REPLAY;
}

lxp_result lxp_idempotency_record(lxp_state_journal *journal,
                                  const uint8_t *actor_did,
                                  size_t actor_did_length,
                                  const uint8_t idempotency_key[32],
                                  const uint8_t *receipt,
                                  size_t receipt_length)
{
    lxp_result status;
    if (journal == NULL || !journal->open || journal->has_idempotency ||
        (receipt == NULL && receipt_length != 0U) ||
        receipt_length > LXP_STATE_MAX_RECEIPT_BYTES)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_state_writer_assert_owner(journal->store);
    if (status != LXP_OK) return status;
    status = key_hash(actor_did, actor_did_length, idempotency_key,
                      journal->staged_idempotency.key_hash);
    if (status != LXP_OK) return status;
    journal->staged_idempotency.canonical_length = 0U;
    journal->staged_idempotency.receipt_length = (uint32_t)receipt_length;
    if (receipt_length != 0U)
        (void)memcpy(journal->staged_idempotency.receipt, receipt,
                     receipt_length);
    journal->has_idempotency = true;
    return LXP_OK;
}

lxp_result lxp_idempotency_reserve(lxp_state_store *store)
{
    if (store == NULL) return LXP_ERR_NON_CANONICAL;
    if (store->idempotency != NULL) return LXP_OK;
    store->idempotency = (lxp_idempotency_key_state *)calloc(
        LXP_STATE_MAX_IDEMPOTENCY, sizeof(*store->idempotency));
    return store->idempotency == NULL ? LXP_ERR_ARENA_EXHAUSTED : LXP_OK;
}

lxp_result lxp_idempotency_can_commit(const lxp_state_journal *journal)
{
    if (journal == NULL || !journal->has_idempotency) return LXP_OK;
    if (journal->store == NULL ||
        journal->store->idempotency_count > LXP_STATE_MAX_IDEMPOTENCY ||
        (journal->store->idempotency_count != 0U && journal->store->idempotency == NULL) ||
        journal->staged_idempotency.receipt_length > LXP_STATE_MAX_RECEIPT_BYTES ||
        journal->staged_idempotency.canonical_length > LXP_STATE_MAX_RECEIPT_BYTES)
        return LXP_FATAL_INVARIANT;
    if (find_entry(journal->store, journal->staged_idempotency.key_hash) !=
        journal->store->idempotency_count) return LXP_FATAL_INVARIANT;
    return journal->store->idempotency_count == LXP_STATE_MAX_IDEMPOTENCY ?
           LXP_ERR_ARENA_EXHAUSTED : LXP_OK;
}

void lxp_idempotency_commit_staged(lxp_state_journal *journal)
{
    if (journal != NULL && journal->has_idempotency) {
        journal->store->idempotency[journal->store->idempotency_count] =
            journal->staged_idempotency;
        ++journal->store->idempotency_count;
        journal->has_idempotency = false;
    }
}

static const uint8_t replay_entry_magic[5] = {'L', 'X', 'R', 'F', '1'};

lxp_result lxp_idempotency_canonical_lookup(lxp_state_store *store,
    const uint8_t *actor_did, size_t actor_did_length,
    const uint8_t idempotency_key[32], const uint8_t **receipt,
    size_t *receipt_length)
{
    uint8_t hash[32];
    size_t location;
    lxp_result status;
    if (store == NULL || receipt == NULL || receipt_length == NULL)
        return LXP_ERR_NON_CANONICAL;
    *receipt = NULL;
    *receipt_length = 0U;
    status = key_hash(actor_did, actor_did_length, idempotency_key, hash);
    if (status != LXP_OK) return status;
    if (pthread_mutex_lock(&store->lock) != 0) return LXP_ERR_IO;
    location = find_entry(store, hash);
    status = LXP_OK;
    if (location != store->idempotency_count) {
        const lxp_idempotency_key_state *entry = &store->idempotency[location];
        if (entry->canonical_length == 0U)
            status = LXP_ERR_VERSION_UNSUPPORTED;
        else if (entry->canonical_length > LXP_STATE_MAX_RECEIPT_BYTES)
            status = LXP_FATAL_INVARIANT;
        else {
            *receipt = entry->canonical_receipt;
            *receipt_length = entry->canonical_length;
            status = LXP_ERR_IDEMPOTENT_REPLAY;
        }
    }
    if (pthread_mutex_unlock(&store->lock) != 0) return LXP_FATAL_INVARIANT;
    return status;
}

static void entry_u32(uint8_t *bytes, uint32_t value)
{
    for (size_t i = 0U; i < 4U; ++i)
        bytes[i] = (uint8_t)(value >> (24U - 8U * i));
}

static uint32_t entry_read_u32(const uint8_t *bytes)
{
    uint32_t value = 0U;
    for (size_t i = 0U; i < 4U; ++i) value = (value << 8U) | bytes[i];
    return value;
}

lxp_result lxp_idempotency_snapshot_encode(const lxp_idempotency_key_state *entry,
    uint8_t *bytes, size_t capacity, size_t *length)
{
    size_t size;
    if (entry == NULL || bytes == NULL || length == NULL ||
        entry->receipt_length > LXP_STATE_MAX_RECEIPT_BYTES ||
        entry->canonical_length > LXP_STATE_MAX_RECEIPT_BYTES)
        return LXP_ERR_NON_CANONICAL;
    size = entry->receipt_length;
    if (entry->canonical_length != 0U) size += 13U + entry->canonical_length;
    if (size > capacity) return LXP_ERR_LENGTH_LIMIT;
    if (entry->canonical_length == 0U) {
        (void)memcpy(bytes, entry->receipt, size);
    } else {
        (void)memcpy(bytes, replay_entry_magic, 5U);
        entry_u32(bytes + 5U, entry->receipt_length);
        entry_u32(bytes + 9U, entry->canonical_length);
        (void)memcpy(bytes + 13U, entry->receipt, entry->receipt_length);
        (void)memcpy(bytes + 13U + entry->receipt_length,
                     entry->canonical_receipt, entry->canonical_length);
    }
    *length = size;
    return LXP_OK;
}

lxp_result lxp_idempotency_snapshot_decode(lxp_idempotency_key_state *entry,
    const uint8_t *bytes, size_t length)
{
    uint32_t compact_length, canonical_length;
    if (entry == NULL || bytes == NULL || length > LXP_STATE_MAX_REPLAY_ENTRY_BYTES)
        return LXP_ERR_NON_CANONICAL;
    if (length < 5U || memcmp(bytes, replay_entry_magic, 5U) != 0) {
        if (length >= 4U && memcmp(bytes, replay_entry_magic, 4U) == 0)
            return LXP_ERR_VERSION_UNSUPPORTED;
        if (length > LXP_STATE_MAX_RECEIPT_BYTES) return LXP_ERR_LENGTH_LIMIT;
        entry->canonical_length = 0U;
        entry->receipt_length = (uint32_t)length;
        (void)memcpy(entry->receipt, bytes, length);
        return LXP_OK;
    }
    if (length < 13U) return LXP_ERR_TRUNCATED;
    compact_length = entry_read_u32(bytes + 5U);
    canonical_length = entry_read_u32(bytes + 9U);
    if (compact_length == 0U || canonical_length == 0U ||
        compact_length > LXP_STATE_MAX_RECEIPT_BYTES ||
        canonical_length > LXP_STATE_MAX_RECEIPT_BYTES ||
        length != 13U + (size_t)compact_length + canonical_length)
        return LXP_ERR_NON_CANONICAL;
    entry->receipt_length = compact_length;
    entry->canonical_length = canonical_length;
    (void)memcpy(entry->receipt, bytes + 13U, compact_length);
    (void)memcpy(entry->canonical_receipt, bytes + 13U + compact_length, canonical_length);
    return LXP_OK;
}
