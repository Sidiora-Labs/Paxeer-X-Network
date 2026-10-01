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
