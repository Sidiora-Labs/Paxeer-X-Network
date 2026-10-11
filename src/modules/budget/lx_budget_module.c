#include "layerx/lx_budget.h"
#include "../asset/committed.h"

#include "layerx/lxp_crypto.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_protocol.h"

#include <stdbool.h>
#include <string.h>

static const uint32_t activity_types[] = {
    LX_BUDGET_CREATE, LX_BUDGET_FUND, LX_BUDGET_AMEND,
    LX_BUDGET_DELEGATE_ADD, LX_BUDGET_DELEGATE_REMOVE,
    LX_BUDGET_SPEND, LX_BUDGET_CLOSE, LX_BUDGET_DEFUND, LX_BUDGET_REVOKE,
    LX_BUDGET_DELEGATE_SPEND, LX_BUDGET_PULL
};

enum {
    BUDGET_CAPABILITY_KEY_BYTES = 80,
    BUDGET_CAPABILITY_BYTES = 195,
    BUDGET_GRANT_DRAW_KEY_BYTES = 45,
    BUDGET_GRANT_DRAW_BYTES = 42
};

typedef union budget_typed_payload {
    lx_budget_create_payload create;
    lx_budget_amount_payload amount;
    lx_budget_amend_payload amend;
    lx_budget_delegate_payload delegate;
    lx_budget_spend_payload spend;
    lx_budget_delegate_spend_payload delegate_spend;
    lx_budget_pull_payload pull;
    lx_budget_close_payload close;
    lx_budget_defund_payload defund;
    lx_budget_revoke_payload revoke;
} budget_typed_payload;

typedef struct budget_decoded {
    uint16_t ordinal;
    const uint8_t *payload;
    size_t payload_length;
    budget_typed_payload *typed;
} budget_decoded;

static void budget_event_u64(uint8_t *bytes, uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bytes[i] = (uint8_t)(value >> (56U - i * 8U));
}

static lxp_result budget_asset_state(lxp_module_ctx *ctx,
                                     const uint8_t asset_id[32],
                                     lxp_transfer_asset_state *state)
{
    lx_asset_runtime *runtime;
    size_t i;
    if (ctx == NULL || ctx->kernel == NULL || asset_id == NULL ||
        state == NULL || ctx->kernel->module_kv_count >
                             LXP_KERNEL_MAX_MODULE_KV)
        return LXP_ERR_NON_CANONICAL;
    if (ctx->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT) {
        const lx_asset_record *record;
        lxp_result status = lxp_module_committed_asset(ctx, asset_id, &record);
        return status == LXP_OK ? lx_asset_transfer_state(record, state) : status;
    }
    for (i = 0U; i < ctx->kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &ctx->kernel->module_kv[i];
        lx_asset_record record;
        lxp_result status;
        if (entry->module_id != LXP_MODULE_ASSET || entry->key_length != 38U ||
            memcmp(entry->key, "asset:", 6U) != 0 ||
            memcmp(entry->key + 6U, asset_id, 32U) != 0)
            continue;
        status = lx_asset_record_decode(entry->value, entry->value_length,
                                        &record);
        return status == LXP_OK ? lx_asset_transfer_state(&record, state) :
                                  status;
    }
    runtime = (lx_asset_runtime *)ctx->kernel->module_runtime[LXP_MODULE_ASSET];
    if (runtime == NULL) return LXP_ERR_ASSET_MISMATCH;
    if (runtime->transfer_assets != NULL &&
        runtime->transfer_asset_count <= LX_ASSET_REGISTRY_CAPACITY)
        for (i = 0U; i < runtime->transfer_asset_count; ++i)
            if (memcmp(runtime->transfer_assets[i].asset_id, asset_id,
                       32U) == 0) {
                *state = runtime->transfer_assets[i];
                return LXP_OK;
            }
    if (runtime->assets != NULL &&
        runtime->asset_count <= LX_ASSET_REGISTRY_CAPACITY)
        for (i = 0U; i < runtime->asset_count; ++i)
            if (memcmp(runtime->assets[i].asset_id, asset_id, 32U) == 0)
                return lx_asset_transfer_state(&runtime->assets[i], state);
    return LXP_ERR_ASSET_MISMATCH;
}

static lxp_result budget_load(lxp_module_ctx *ctx, const uint8_t id[32],
                              lx_budget_record *record)
{
    uint8_t key[LX_BUDGET_STATE_KEY_BYTES];
    const uint8_t *bytes;
    size_t length;
    lx_budget_runtime *runtime;
    lx_budget_record *base;
    lxp_result status = lx_budget_state_key(id, key);
    if (status != LXP_OK) return status;
    status = lxp_ctx_kv_get(ctx, key, sizeof(key), &bytes, &length);
    if (status == LXP_OK) return lx_budget_record_decode(bytes, length, record);
    if (status != LXP_ERR_UNKNOWN_FIELD) return status;
    runtime = (lx_budget_runtime *)lxp_ctx_module_runtime(ctx);
    if (runtime == NULL || runtime->store == NULL) return LXP_ERR_UNKNOWN_FIELD;
    status = lx_budget_lookup(runtime->store, id, &base);
    if (status != LXP_OK) return status;
    *record = *base;
    return LXP_OK;
}

static lxp_result budget_save(lxp_module_ctx *ctx,
                              const lx_budget_record *record)
{
    uint8_t key[LX_BUDGET_STATE_KEY_BYTES];
    uint8_t bytes[LX_BUDGET_RECORD_MAX_BYTES];
    size_t length;
    lxp_result status = lx_budget_record_encode(record, bytes, sizeof(bytes),
                                                &length);
    if (status == LXP_OK) status = lx_budget_state_key(record->budget_id, key);
    return status == LXP_OK ?
        lxp_ctx_kv_put(ctx, key, sizeof(key), bytes, length) : status;
}

static lxp_result budget_capacity(lxp_module_ctx *ctx)
{
    lx_budget_runtime *runtime =
        (lx_budget_runtime *)lxp_ctx_module_runtime(ctx);
    lx_budget_store *store = runtime == NULL ? NULL : runtime->store;
    size_t count = store == NULL ? 0U : store->count;
    size_t i;
    if (count > (size_t)LX_BUDGET_STORE_CAPACITY) return LXP_FATAL_INVARIANT;
    for (i = 0U; i < ctx->kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &ctx->kernel->module_kv[i];
        lx_budget_record *existing;
        if (entry->module_id != LXP_MODULE_BUDGET ||
            entry->key_length != (uint16_t)LX_BUDGET_STATE_KEY_BYTES ||
            memcmp(entry->key, "budget:", 7U) != 0)
            continue;
        if (store != NULL &&
            lx_budget_lookup(store, entry->key + 7U, &existing) == LXP_OK)
            continue;
        ++count;
    }
    return count >= (size_t)LX_BUDGET_STORE_CAPACITY ?
        LXP_ERR_ARENA_EXHAUSTED : LXP_OK;
}

static lxp_result budget_execution_sequence(lxp_module_ctx *ctx,
    const lxp_activity *activity, const lx_account *account, uint64_t *sequence)
{
    const lxp_ledger_admission_facts *facts = &ctx->ledger_admission;
    uint8_t actor[32];
    lxp_result status;
    *sequence = activity->account_sequence;
    if (!facts->bound) return LXP_OK;
    if (ctx->protocol_version != LXP_PROTOCOL_VERSION_STATE_COMMITMENT ||
        ctx->module_id != LXP_MODULE_BUDGET || !facts->account_present ||
        facts->activity_type != activity->activity_type ||
        memcmp(facts->activity_binding, ctx->activity_id, 32U) != 0 ||
        memcmp(facts->account_id, account->id, 32U) != 0 ||
        activity->authority.length != 32U || activity->authority.bytes == NULL ||
        memcmp(facts->verified_key, activity->authority.bytes, 32U) != 0 ||
        facts->next_sequence != account->next_sequence)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_did_id_derive(activity->actor_did.bytes, activity->actor_did.length, actor);
    if (status != LXP_OK) return status;
    if (memcmp(actor, facts->actor, 32U) != 0) return LXP_ERR_CONTEXT_MISMATCH;
    *sequence = facts->next_sequence;
    return LXP_OK;
}

static lxp_result budget_transfer(lxp_module_ctx *ctx,
                                  const lxp_activity *activity,
                                  const uint8_t asset_id[32],
                                  lx_account *from, lx_account *to,
                                  lx_account *sequence_account,
                                  lxp_u128 amount, uint16_t reason,
                                  lxp_authorization_kind authority_kind)
{
    lxp_transfer_set set;
    lxp_transfer_source_authority source;
    lxp_transfer_asset_state asset_state;
    lxp_receipt receipt;
    uint64_t sequence = activity->account_sequence;
    bool native_source = activity->protocol_version == 3U &&
        ((activity->activity_type == LX_BUDGET_CREATE && activity->payload.length == LX_BUDGET_CREATE_V2_PAYLOAD_BYTES) ||
         (activity->activity_type == LX_BUDGET_FUND && activity->payload.length == LX_BUDGET_FUND_V2_PAYLOAD_BYTES));
    lxp_result status;
    if (from == NULL || to == NULL || sequence_account == NULL)
        return LXP_ERR_NON_CANONICAL;
    if (sequence_account->next_sequence == UINT64_MAX) return LXP_ERR_OVERFLOW;
    status = native_source ? LXP_OK : budget_execution_sequence(ctx, activity, sequence_account, &sequence);
    if (status == LXP_OK) status = budget_asset_state(ctx, asset_id, &asset_state);
    if (status != LXP_OK) return status;
    (void)memset(&set, 0, sizeof(set));
    (void)memset(&source, 0, sizeof(source));
    (void)memset(&receipt, 0, sizeof(receipt));
    set.leg_count = 1U;
    set.legs[0].from = from;
    set.legs[0].to = to;
    (void)memcpy(set.legs[0].asset_id, asset_id, 32U);
    set.legs[0].amount = amount;
    set.legs[0].reason = reason;
    set.context.assets = &asset_state;
    set.context.asset_count = 1U;
    set.context.sequence_account = sequence_account;
    set.context.actor_sequence = sequence;
    if (activity->protocol_version == 3U && activity->activity_type == LX_BUDGET_CREATE &&
        activity->payload.length == LX_BUDGET_CREATE_V2_PAYLOAD_BYTES) {
        lx_budget_create_payload payload;
        status = lx_budget_create_decode(activity->payload.bytes, activity->payload.length, &payload);
        if (status != LXP_OK) return status;
        if (payload.encoding_version != 2U || memcmp(payload.source_account, sequence_account->id, 32U) != 0 ||
            payload.source_sequence != sequence_account->next_sequence)
            return LXP_ERR_CONTEXT_MISMATCH;
        set.context.actor_sequence = payload.source_sequence;
    }
    if (activity->protocol_version == 3U && activity->activity_type == LX_BUDGET_FUND &&
        activity->payload.length == LX_BUDGET_FUND_V2_PAYLOAD_BYTES) {
        lx_budget_amount_payload payload;
        status = lx_budget_amount_decode(activity->payload.bytes, activity->payload.length, &payload);
        if (status != LXP_OK) return status;
        if (payload.encoding_version != 2U || payload.source_sequence != sequence_account->next_sequence)
            return LXP_ERR_CONTEXT_MISMATCH;
        set.context.actor_sequence = payload.source_sequence;
    }
    set.context.batch_timestamp = lxp_ctx_batch_timestamp_ms(ctx);
    set.context.debit_authority_kind = authority_kind;
    (void)memcpy(set.context.authorized_from, from->id, 32U);
    lx_budget_bind_source_authority(&set, &source);
    return lxp_ctx_emit_transfer_set(ctx, &set, &receipt);
}

static bool budget_name_binds_actor(const lx_account *account,
                                    const lxp_activity *activity,
                                    const char *suffix, size_t suffix_length,
                                    bool exact)
{
    size_t prefix_length;
    size_t bound;
    if (account == NULL || activity == NULL ||
        activity->actor_did.bytes == NULL ||
        activity->actor_did.length == 0U ||
        activity->actor_did.length > (size_t)LXP_MAX_DID_LENGTH)
        return false;
    prefix_length = 6U + activity->actor_did.length;
    bound = prefix_length + suffix_length;
    if ((size_t)account->name_length < bound ||
        memcmp(account->name, "agent:", 6U) != 0 ||
        memcmp(account->name + 6U, activity->actor_did.bytes,
               activity->actor_did.length) != 0 ||
        memcmp(account->name + prefix_length, suffix, suffix_length) != 0)
        return false;
    return exact ? (size_t)account->name_length == bound :
                   (size_t)account->name_length > bound;
}

static lxp_result budget_authorized_signer(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lxp_authority_resolved *authority, const budget_decoded *value,
    lx_account **signer)
{
    uint8_t actor[32];
    uint64_t sequence;
    lxp_result status;
    if (authority == NULL || activity == NULL || value == NULL ||
        value->typed == NULL || authority->kind != LXP_AUTHORITY_OWNER ||
        activity->authority.length != 32U ||
        activity->authority.bytes == NULL ||
        memcmp(authority->verified_key, activity->authority.bytes, 32U) != 0 ||
        activity->activity_type !=
            (((uint32_t)LXP_MODULE_BUDGET << 16U) | value->ordinal))
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    status = lxp_did_id_derive(activity->actor_did.bytes,
                               activity->actor_did.length, actor);
    if (status == LXP_OK && memcmp(actor, authority->actor, 32U) != 0)
        status = LXP_ERR_UNAUTHORIZED_DEBIT;
    if (status == LXP_OK) status = lxp_activity_verify_payload_hash(activity);
    if (status == LXP_OK) status = lxp_activity_verify_signature(activity);
    if (status == LXP_OK)
        status = lxp_ctx_account_find(ctx, authority->principal, signer);
    if (status != LXP_OK) return status;
    if ((*signer)->kind != LX_ACCOUNT_AGENT_MAIN ||
        !budget_name_binds_actor(*signer, activity, ":main", 5U, true) ||
        !(*signer)->has_authority_key ||
        memcmp((*signer)->authority_key, authority->verified_key, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    status = budget_execution_sequence(ctx, activity, *signer, &sequence);
    if (status != LXP_OK) return status;
    if (sequence < (*signer)->next_sequence)
        return LXP_ERR_SEQUENCE_REUSED;
    return sequence > (*signer)->next_sequence ?
        LXP_ERR_SEQUENCE_GAP : LXP_OK;
}

static lxp_result budget_business_source(lxp_module_ctx *ctx, const lxp_activity *activity,
    const lx_account *owner, const uint8_t asset[32], const uint8_t source_id[32], lx_account **source)
{
    uint8_t actor[32];
    lxp_result status;
    if (activity->protocol_version != 3U) return LXP_ERR_VERSION_UNSUPPORTED;
    status = lxp_did_id_derive(activity->actor_did.bytes, activity->actor_did.length, actor);
    if (status == LXP_OK) status = lxp_kernel_program_payment_account(ctx->kernel->state->accounts,
        actor, asset, activity->protocol_version, source);
    if (status != LXP_OK) return status;
    if (memcmp((*source)->id, source_id, 32U) != 0 || !(*source)->has_authority_key ||
        lx_account_validate_canonical(*source) != LXP_OK || !owner->has_authority_key ||
        memcmp((*source)->authority_key, owner->authority_key, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    return LXP_OK;
}

static lxp_result budget_execute_create(lxp_module_ctx *ctx,
                                        const lxp_activity *activity,
                                        const lx_budget_create_payload *payload,
                                        lx_account *owner)
{
    lx_budget_record record;
    lx_account *budget_account = NULL;
    lx_account *source = owner;
    uint8_t event[80];
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status == LXP_OK) return LXP_ERR_SEQUENCE_REUSED;
    if (status != LXP_ERR_UNKNOWN_FIELD) return status;
    status = budget_capacity(ctx);
    if (status == LXP_OK && ctx->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT)
        status = lxp_ctx_account_stage_module_custody(ctx, activity, payload->budget_id,
            payload->asset_id, payload->budget_account, &budget_account);
    else if (status == LXP_OK)
        status = lxp_ctx_account_find(ctx, payload->budget_account, &budget_account);
    if (status != LXP_OK) return status;
    if (budget_account->kind != LX_ACCOUNT_AGENT_BUDGET ||
        !budget_name_binds_actor(budget_account, activity, ":budget:", 8U,
                                 false))
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    (void)memset(&record, 0, sizeof(record));
    (void)memcpy(record.budget_id, payload->budget_id, 32U);
    (void)memcpy(record.owner, owner->id, 32U);
    (void)memcpy(record.budget_account, budget_account->id, 32U);
    (void)memcpy(record.asset_id, payload->asset_id, 32U);
    (void)memcpy(record.purpose_hash, payload->purpose_hash, 32U);
    record.per_period_limit = payload->per_period_limit;
    record.configured_period_limit = payload->per_period_limit;
    record.carry_cap = payload->carry_cap;
    record.period_length = payload->period_length;
    record.period_start = payload->period_start;
    record.expiry = payload->expiry;
    record.revocation_sequence = payload->revocation_sequence;
    record.rollover_policy =
        (lx_budget_rollover_policy)payload->rollover_policy;
    if (payload->encoding_version == 2U) {
        status = budget_business_source(ctx, activity, owner, record.asset_id, payload->source_account, &source);
        if (status != LXP_OK) return status;
        record.native_source = true;
        (void)memcpy(record.source_account, source->id, 32U);
    }
    status = lx_budget_record_validate(&record);
    if (status == LXP_OK)
        status = budget_transfer(ctx, activity, record.asset_id, source,
                                 budget_account, source, payload->amount,
                                 LXP_REASON_BUDGET_FUND, LXP_AUTH_OWNER);
    if (status == LXP_OK) status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    (void)memcpy(event + 32U, record.asset_id, 32U);
    status = lxp_u128_to_be(payload->amount, event + 64U);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 1U, event, sizeof(event)) : status;
}

static lxp_result budget_execute_fund(lxp_module_ctx *ctx,
                                      const lxp_activity *activity,
                                      const lx_budget_amount_payload *payload,
                                      lx_account *owner)
{
    lx_budget_record record;
    lx_account *budget_account;
    lx_account *source = owner;
    uint8_t event[48];
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status != LXP_OK) return status;
    if (record.closed) return LXP_ERR_UNKNOWN_FIELD;
    if (record.revoked) return LXP_ERR_BUDGET_REVOKED;
    if (memcmp(record.owner, owner->id, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    status = lxp_ctx_account_find(ctx, record.budget_account, &budget_account);
    if (status != LXP_OK) return status;
    if (record.native_source != (payload->encoding_version == 2U)) return LXP_ERR_VERSION_UNSUPPORTED;
    if (record.native_source) {
        status = budget_business_source(ctx, activity, owner, record.asset_id, record.source_account, &source);
        if (status != LXP_OK) return status;
    }
    status = budget_transfer(ctx, activity, record.asset_id, source,
                             budget_account, source, payload->amount,
                             LXP_REASON_BUDGET_FUND, LXP_AUTH_OWNER);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    status = lxp_u128_to_be(payload->amount, event + 32U);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 2U, event, sizeof(event)) : status;
}

static lxp_result budget_execute_amend(lxp_module_ctx *ctx,
                                       const lx_budget_amend_payload *payload,
                                       lx_account *owner)
{
    lx_budget_record record;
    lxp_u128 limit;
    uint8_t event[56];
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status != LXP_OK) return status;
    if (record.closed) return LXP_ERR_UNKNOWN_FIELD;
    if (record.revoked) return LXP_ERR_BUDGET_REVOKED;
    if (memcmp(record.owner, owner->id, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    status = lxp_u128_add(payload->per_period_limit, record.carried, &limit);
    if (status != LXP_OK) return status;
    if (lxp_u128_cmp(limit, record.spent_this_period) < 0)
        return LXP_ERR_BUDGET_PERIOD_CAP;
    record.configured_period_limit = payload->per_period_limit;
    record.per_period_limit = limit;
    record.carry_cap = payload->carry_cap;
    record.expiry = payload->expiry;
    record.rollover_policy =
        (lx_budget_rollover_policy)payload->rollover_policy;
    status = lx_budget_record_validate(&record);
    if (status == LXP_OK) status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    status = lxp_u128_to_be(record.per_period_limit, event + 32U);
    budget_event_u64(event + 48U, record.expiry);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 3U, event, sizeof(event)) : status;
}

static lxp_result budget_execute_delegate(
    lxp_module_ctx *ctx, uint16_t ordinal,
    const lx_budget_delegate_payload *payload, lx_account *owner)
{
    lx_budget_record record;
    uint8_t event[64];
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status != LXP_OK) return status;
    if (record.closed) return LXP_ERR_UNKNOWN_FIELD;
    if (record.revoked) return LXP_ERR_BUDGET_REVOKED;
    if (memcmp(record.owner, owner->id, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    status = ordinal == 4U ?
        lx_budget_delegate_add_execute(&record, payload->delegate) :
        lx_budget_delegate_remove_execute(&record, payload->delegate);
    if (status == LXP_OK) status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    (void)memcpy(event + 32U, payload->delegate, 32U);
    return lxp_ctx_emit_event(ctx, ordinal, event, sizeof(event));
}

static lxp_result budget_spent_event(lxp_module_ctx *ctx,
                                     const uint8_t budget_id[32],
                                     const uint8_t recipient_id[32],
                                     lxp_u128 amount)
{
    uint8_t event[80];
    lxp_result status;
    (void)memcpy(event, budget_id, 32U);
    (void)memcpy(event + 32U, recipient_id, 32U);
    status = lxp_u128_to_be(amount, event + 64U);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 6U, event, sizeof(event)) : status;
}

static lxp_result budget_savepoint_end(lxp_module_ctx *ctx,
                                       lxp_module_savepoint *savepoint,
                                       lxp_result status)
{
    if (status == LXP_OK) return lxp_module_savepoint_accept(ctx, savepoint);
    if (savepoint->active &&
        ctx->transfer_snapshot_count == savepoint->transfer_snapshot_count &&
        ctx->transfer_applied == savepoint->transfer_applied) {
        lxp_result discarded = lxp_module_savepoint_discard(ctx, savepoint);
        if (discarded != LXP_OK) return discarded;
    }
    return status;
}

static lxp_result budget_spend_apply(lxp_module_ctx *ctx,
                                     const lxp_activity *activity,
                                     lx_budget_record *record,
                                     const uint8_t recipient_id[32],
                                     lxp_u128 amount, lx_account *signer)
{
    lx_account *budget_account = NULL;
    lx_account *recipient = NULL;
    lxp_u128 balance;
    lxp_result status =
        lxp_ctx_account_find(ctx, record->budget_account, &budget_account);
    if (status == LXP_OK)
        status = lxp_ctx_account_find(ctx, recipient_id, &recipient);
    if (status != LXP_OK) return status;
    if (budget_account->kind != LX_ACCOUNT_AGENT_BUDGET ||
        (recipient->kind != LX_ACCOUNT_AGENT_MAIN &&
         !(record->native_source && recipient->kind == LX_ACCOUNT_AGENT_ASSET)))
        return LXP_ERR_NON_CANONICAL;
    status = lxp_state_balance_get(budget_account, record->asset_id, &balance);
    if (status != LXP_OK) return status;
    status = lx_budget_spend_prepare(record, lxp_ctx_batch_timestamp_ms(ctx),
                                     balance, amount);
    if (status == LXP_OK)
        status = budget_transfer(ctx, activity, record->asset_id,
                                 budget_account, recipient, signer,
                                 amount, LXP_REASON_BUDGET_SPEND,
                                 LXP_AUTH_BUDGET_ALLOWANCE);
    if (status == LXP_OK) status = budget_save(ctx, record);
    return status == LXP_OK ?
        budget_spent_event(ctx, record->budget_id, recipient->id, amount) :
        status;
}

static lxp_result budget_execute_spend(lxp_module_ctx *ctx,
                                       const lxp_activity *activity,
                                       const lxp_authority_resolved *authority,
                                       const lx_budget_spend_payload *payload,
                                       lx_account *signer)
{
    lx_budget_record record;
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status != LXP_OK) return status;
    if (memcmp(record.owner, signer->id, 32U) != 0 &&
        !lx_budget_delegate_present(&record, authority->actor))
        return LXP_ERR_UNAUTHORIZED_DELEGATE;
    return budget_spend_apply(ctx, activity, &record, payload->recipient,
                              payload->amount, signer);
}

static void budget_capability_key(const uint8_t budget_id[32],
                                  const uint8_t holder[32],
                                  uint8_t key[BUDGET_CAPABILITY_KEY_BYTES])
{
    (void)memcpy(key, "budget-delegate:", 16U);
    (void)memcpy(key + 16U, budget_id, 32U);
    (void)memcpy(key + 48U, holder, 32U);
}

static uint64_t budget_capability_u64(const uint8_t *bytes)
{
    uint64_t value = 0U;
    size_t i;
    for (i = 0U; i < 8U; ++i) value = (value << 8U) | bytes[i];
    return value;
}

static lxp_result budget_capability_load(
    lxp_module_ctx *ctx, const uint8_t budget_id[32],
    const uint8_t holder[32], lx_budget_delegate_capability *capability)
{
    uint8_t key[BUDGET_CAPABILITY_KEY_BYTES];
    const uint8_t *bytes;
    size_t length;
    lx_budget_delegate_capability value;
    lxp_result status;
    budget_capability_key(budget_id, holder, key);
    status = lxp_ctx_kv_get(ctx, key, sizeof(key), &bytes, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) return LXP_ERR_UNAUTHORIZED_DELEGATE;
    if (status != LXP_OK) return status;
    if (length != (size_t)BUDGET_CAPABILITY_BYTES || bytes[0] != 0U ||
        bytes[1] != 1U || bytes[194] > 1U)
        return LXP_ERR_NON_CANONICAL;
    (void)memset(&value, 0, sizeof(value));
    (void)memcpy(value.holder, bytes + 2U, 32U);
    (void)memcpy(value.asset_id, bytes + 34U, 32U);
    (void)memcpy(value.recipient, bytes + 66U, 32U);
    (void)memcpy(value.purpose_hash, bytes + 98U, 32U);
    status = lxp_u128_from_be(bytes + 130U, &value.maximum_per_spend);
    if (status == LXP_OK)
        status = lxp_u128_from_be(bytes + 146U, &value.maximum_total);
    if (status == LXP_OK)
        status = lxp_u128_from_be(bytes + 162U, &value.consumed);
    if (status != LXP_OK) return status;
    value.expiry = budget_capability_u64(bytes + 178U);
    value.revocation_sequence = budget_capability_u64(bytes + 186U);
    value.revoked = bytes[194] != 0U;
    if (memcmp(value.holder, holder, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (lxp_u128_cmp(value.consumed, value.maximum_total) > 0)
        return LXP_ERR_NON_CANONICAL;
    *capability = value;
    return LXP_OK;
}

static lxp_result budget_capability_save(
    lxp_module_ctx *ctx, const uint8_t budget_id[32],
    const lx_budget_delegate_capability *capability)
{
    uint8_t key[BUDGET_CAPABILITY_KEY_BYTES];
    uint8_t bytes[BUDGET_CAPABILITY_BYTES];
    lxp_result status;
    budget_capability_key(budget_id, capability->holder, key);
    bytes[0] = 0U;
    bytes[1] = 1U;
    (void)memcpy(bytes + 2U, capability->holder, 32U);
    (void)memcpy(bytes + 34U, capability->asset_id, 32U);
    (void)memcpy(bytes + 66U, capability->recipient, 32U);
    (void)memcpy(bytes + 98U, capability->purpose_hash, 32U);
    status = lxp_u128_to_be(capability->maximum_per_spend, bytes + 130U);
    if (status == LXP_OK)
        status = lxp_u128_to_be(capability->maximum_total, bytes + 146U);
    if (status == LXP_OK)
        status = lxp_u128_to_be(capability->consumed, bytes + 162U);
    if (status != LXP_OK) return status;
    budget_event_u64(bytes + 178U, capability->expiry);
    budget_event_u64(bytes + 186U, capability->revocation_sequence);
    bytes[194] = capability->revoked ? 1U : 0U;
    return lxp_ctx_kv_put(ctx, key, sizeof(key), bytes, sizeof(bytes));
}

static lxp_result budget_execute_delegate_spend(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lxp_authority_resolved *authority,
    const lx_budget_delegate_spend_payload *payload, lx_account *signer)
{
    lx_budget_record record;
    lx_budget_delegate_capability capability;
    lxp_module_savepoint savepoint = {0};
    lxp_result status = lxp_module_savepoint_begin(ctx, &savepoint);
    if (status == LXP_OK) status = budget_load(ctx, payload->budget_id, &record);
    if (status == LXP_OK)
        status = budget_capability_load(ctx, record.budget_id,
                                        authority->actor, &capability);
    if (status == LXP_OK)
        status = lx_budget_authorize_delegate(
            &record, authority->actor, &capability, payload->recipient,
            payload->amount, lxp_ctx_batch_timestamp_ms(ctx));
    if (status == LXP_OK)
        status = budget_spend_apply(ctx, activity, &record, payload->recipient,
                                    payload->amount, signer);
    if (status == LXP_OK)
        status = budget_capability_save(ctx, record.budget_id, &capability);
    return budget_savepoint_end(ctx, &savepoint, status);
}

static void budget_grant_draw_key(const uint8_t grant_id[32],
                                  uint8_t key[BUDGET_GRANT_DRAW_KEY_BYTES])
{
    (void)memcpy(key, "budget-grant:", 13U);
    (void)memcpy(key + 13U, grant_id, 32U);
}

static lxp_result budget_grant_draw_load(lxp_module_ctx *ctx,
                                         lxp_grant_state *draws)
{
    uint8_t key[BUDGET_GRANT_DRAW_KEY_BYTES];
    const uint8_t *bytes;
    size_t length;
    lxp_result status;
    draws->drawn_total = (lxp_u128){ 0U, 0U };
    draws->drawn_this_period = (lxp_u128){ 0U, 0U };
    draws->window_start = 0U;
    budget_grant_draw_key(draws->grant.grant_id, key);
    status = lxp_ctx_kv_get(ctx, key, sizeof(key), &bytes, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) return LXP_OK;
    if (status != LXP_OK) return status;
    if (length != (size_t)BUDGET_GRANT_DRAW_BYTES || bytes[0] != 0U ||
        bytes[1] != 1U)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_u128_from_be(bytes + 2U, &draws->drawn_total);
    if (status == LXP_OK)
        status = lxp_u128_from_be(bytes + 18U, &draws->drawn_this_period);
    if (status != LXP_OK) return status;
    draws->window_start = budget_capability_u64(bytes + 34U);
    return lxp_u128_cmp(draws->drawn_this_period, draws->drawn_total) > 0 ?
        LXP_ERR_NON_CANONICAL : LXP_OK;
}

static lxp_result budget_grant_draw_save(lxp_module_ctx *ctx,
                                         const lxp_grant_state *draws)
{
    uint8_t key[BUDGET_GRANT_DRAW_KEY_BYTES];
    uint8_t bytes[BUDGET_GRANT_DRAW_BYTES];
    lxp_result status;
    budget_grant_draw_key(draws->grant.grant_id, key);
    bytes[0] = 0U;
    bytes[1] = 1U;
    status = lxp_u128_to_be(draws->drawn_total, bytes + 2U);
    if (status == LXP_OK)
        status = lxp_u128_to_be(draws->drawn_this_period, bytes + 18U);
    if (status != LXP_OK) return status;
    budget_event_u64(bytes + 34U, draws->window_start);
    return lxp_ctx_kv_put(ctx, key, sizeof(key), bytes, sizeof(bytes));
}

static lxp_result budget_grant_combine(lxp_grant_state *state,
                                       const lxp_grant_state *draws)
{
    lxp_u128 sum;
    lxp_result status = lxp_u128_add(state->drawn_total, draws->drawn_total,
                                     &sum);
    if (status != LXP_OK) return status;
    state->drawn_total = sum;
    if (draws->window_start > state->window_start) {
        state->drawn_this_period = draws->drawn_this_period;
        state->window_start = draws->window_start;
    } else if (draws->window_start == state->window_start) {
        status = lxp_u128_add(state->drawn_this_period,
                              draws->drawn_this_period, &sum);
        if (status != LXP_OK) return status;
        state->drawn_this_period = sum;
    }
    return LXP_OK;
}

static lxp_result budget_pull_request_bind(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lx_budget_pull_payload *payload, lx_account *signer,
    lx_budget_pull_request *pull, lx_asset_record *asset,
    lxp_transfer_asset_state *asset_state, const lx_account **grantor)
{
    const lx_budget_record *record = &pull->spend.store->records[0];
    lx_account *owner = NULL;
    uint64_t sequence;
    lxp_result status;
    if (signer->next_sequence == UINT64_MAX) return LXP_ERR_OVERFLOW;
    status = lxp_ctx_account_find(ctx, record->budget_account,
                                  &pull->spend.budget_account);
    if (status == LXP_OK)
        status = lxp_ctx_account_find(ctx, payload->recipient,
                                      &pull->spend.recipient);
    if (status == LXP_OK)
        status = lxp_ctx_account_find(ctx, record->owner, &owner);
    if (status == LXP_OK)
        status = budget_asset_state(ctx, record->asset_id, asset_state);
    if (status == LXP_OK)
        status = budget_execution_sequence(ctx, activity, signer, &sequence);
    if (status != LXP_OK) return status;
    (void)memset(asset, 0, sizeof(*asset));
    (void)memcpy(asset->asset_id, record->asset_id, 32U);
    pull->spend.budget_id = record->budget_id;
    pull->spend.asset = asset;
    pull->spend.amount = payload->amount;
    pull->spend.context.assets = asset_state;
    pull->spend.context.asset_count = 1U;
    pull->spend.context.sequence_account = signer;
    pull->spend.context.actor_sequence = sequence;
    pull->spend.context.batch_timestamp = lxp_ctx_batch_timestamp_ms(ctx);
    *grantor = owner;
    return LXP_OK;
}

static lxp_result budget_execute_pull(lxp_module_ctx *ctx,
                                      const lxp_activity *activity,
                                      const lx_budget_pull_payload *payload,
                                      lx_account *signer)
{
    lx_budget_pull_request pull;
    lx_asset_record asset;
    lxp_transfer_asset_state asset_state;
    lxp_grant_state state;
    lxp_grant_state draws;
    lxp_receipt receipt;
    lxp_module_savepoint savepoint = {0};
    void *memory = NULL;
    lxp_result status = lxp_module_savepoint_begin(ctx, &savepoint);
    (void)memset(&pull, 0, sizeof(pull));
    (void)memset(&receipt, 0, sizeof(receipt));
    (void)memset(&state, 0, sizeof(state));
    (void)memset(&draws, 0, sizeof(draws));
    if (status == LXP_OK)
        status = lxp_ctx_arena_alloc(ctx, sizeof(lx_budget_store),
                                     _Alignof(lx_budget_store), &memory);
    if (status == LXP_OK) {
        pull.spend.store = (lx_budget_store *)memory;
        (void)memset(pull.spend.store, 0, sizeof(*pull.spend.store));
        pull.spend.store->count = 1U;
        status = budget_load(ctx, payload->budget_id,
                             &pull.spend.store->records[0]);
    }
    if (status == LXP_OK)
        status = budget_pull_request_bind(ctx, activity, payload, signer,
                                          &pull, &asset, &asset_state,
                                          &pull.grantor);
    if (status == LXP_OK)
        status = lx_asset_payer_grant_read(ctx->kernel,
                                           payload->grant.grant_id, &state);
    if (status == LXP_OK) {
        draws = state;
        status = budget_grant_draw_load(ctx, &draws);
    }
    if (status == LXP_OK) status = budget_grant_combine(&state, &draws);
    if (status == LXP_OK) {
        pull.grant_state = &state;
        status = lx_budget_pull_execute(ctx, &pull, &receipt);
    }
    if (status == LXP_OK)
        status = lxp_grant_draw_record(&draws, payload->amount,
                                       lxp_ctx_batch_timestamp_ms(ctx));
    if (status == LXP_OK) status = budget_grant_draw_save(ctx, &draws);
    if (status == LXP_OK)
        status = budget_save(ctx, &pull.spend.store->records[0]);
    if (status == LXP_OK)
        status = budget_spent_event(ctx, payload->budget_id,
                                    payload->recipient, payload->amount);
    return budget_savepoint_end(ctx, &savepoint, status);
}

static lxp_result budget_execute_close(lxp_module_ctx *ctx,
                                       const lxp_activity *activity,
                                       const lx_budget_close_payload *payload,
                                       lx_account *owner)
{
    lx_budget_record record;
    lx_account *budget_account;
    lx_account *refund = owner;
    lxp_u128 balance;
    uint8_t event[56];
    lxp_result status = budget_load(ctx, payload->budget_id, &record);
    if (status != LXP_OK) return status;
    if (record.closed) return LXP_ERR_UNKNOWN_FIELD;
    if (memcmp(record.owner, owner->id, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    if (payload->revocation_sequence <= record.revocation_sequence)
        return LXP_ERR_STALE_REVOCATION;
    status = lxp_ctx_account_find(ctx, record.budget_account, &budget_account);
    if (status != LXP_OK) return status;
    status = lxp_state_balance_get(budget_account, record.asset_id, &balance);
    if (status != LXP_OK) return status;
    if (record.native_source) {
        status = budget_business_source(ctx, activity, owner, record.asset_id, record.source_account, &refund);
        if (status != LXP_OK) return status;
    }
    if (!lxp_u128_is_zero(balance))
        status = budget_transfer(ctx, activity, record.asset_id,
                                 budget_account, refund, owner, balance,
                                 LXP_REASON_BUDGET_DEFUND,
                                 LXP_AUTH_BUDGET_ALLOWANCE);
    if (status != LXP_OK) return status;
    record.revocation_sequence = payload->revocation_sequence;
    record.closed = true;
    status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    status = lxp_u128_to_be(balance, event + 32U);
    budget_event_u64(event + 48U, record.revocation_sequence);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 7U, event, sizeof(event)) : status;
}

static lxp_result budget_owned_live(lxp_module_ctx *ctx,
                                    const lxp_activity *activity,
                                    const uint8_t budget_id[32],
                                    lx_account *owner,
                                    lx_budget_record *record,
                                    lx_account **budget_account,
                                    lx_account **refund,
                                    lxp_u128 *balance)
{
    lxp_result status = budget_load(ctx, budget_id, record);
    if (status != LXP_OK) return status;
    if (record->closed) return LXP_ERR_UNKNOWN_FIELD;
    if (memcmp(record->owner, owner->id, 32U) != 0)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    if (record->revoked) return LXP_ERR_BUDGET_REVOKED;
    status = lxp_ctx_account_find(ctx, record->budget_account, budget_account);
    if (status != LXP_OK) return status;
    status = lxp_state_balance_get(*budget_account, record->asset_id, balance);
    if (status != LXP_OK) return status;
    *refund = owner;
    return record->native_source ?
        budget_business_source(ctx, activity, owner, record->asset_id,
                               record->source_account, refund) :
        LXP_OK;
}

static lxp_result budget_execute_defund(lxp_module_ctx *ctx,
                                        const lxp_activity *activity,
                                        const lx_budget_defund_payload *payload,
                                        lx_account *owner)
{
    lx_budget_record record;
    lx_account *budget_account;
    lx_account *refund;
    lxp_u128 balance;
    lxp_u128 limit;
    uint8_t event[64];
    lxp_result status = budget_owned_live(ctx, activity, payload->budget_id,
                                          owner, &record, &budget_account,
                                          &refund, &balance);
    if (status == LXP_OK)
        status = lx_budget_defund_limit(&record, balance, payload->amount,
                                        &limit);
    if (status == LXP_OK)
        status = budget_transfer(ctx, activity, record.asset_id,
                                 budget_account, refund, owner,
                                 payload->amount, LXP_REASON_BUDGET_DEFUND,
                                 LXP_AUTH_BUDGET_ALLOWANCE);
    if (status != LXP_OK) return status;
    record.per_period_limit = limit;
    status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    status = lxp_u128_to_be(payload->amount, event + 32U);
    if (status == LXP_OK)
        status = lxp_u128_to_be(record.per_period_limit, event + 48U);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 8U, event, sizeof(event)) : status;
}

static lxp_result budget_execute_revoke(lxp_module_ctx *ctx,
                                        const lxp_activity *activity,
                                        const lx_budget_revoke_payload *payload,
                                        lx_account *owner)
{
    lx_budget_record record;
    lx_account *budget_account;
    lx_account *refund;
    lxp_u128 balance;
    uint8_t event[56];
    lxp_result status = budget_owned_live(ctx, activity, payload->budget_id,
                                          owner, &record, &budget_account,
                                          &refund, &balance);
    if (status != LXP_OK) return status;
    if (payload->revocation_sequence <= record.revocation_sequence)
        return LXP_ERR_STALE_REVOCATION;
    if (!lxp_u128_is_zero(balance))
        status = budget_transfer(ctx, activity, record.asset_id,
                                 budget_account, refund, owner, balance,
                                 LXP_REASON_BUDGET_DEFUND,
                                 LXP_AUTH_BUDGET_ALLOWANCE);
    if (status != LXP_OK) return status;
    record.revocation_sequence = payload->revocation_sequence;
    record.revoked = true;
    record.per_period_limit = record.spent_this_period;
    status = budget_save(ctx, &record);
    if (status != LXP_OK) return status;
    (void)memcpy(event, record.budget_id, 32U);
    status = lxp_u128_to_be(balance, event + 32U);
    budget_event_u64(event + 48U, record.revocation_sequence);
    return status == LXP_OK ?
        lxp_ctx_emit_event(ctx, 9U, event, sizeof(event)) : status;
}

static lxp_result module_genesis(lxp_module_ctx *ctx, const uint8_t *manifest,
                                 size_t length)
{
    if (ctx == NULL || (manifest == NULL && length != 0U))
        return LXP_ERR_NON_CANONICAL;
    return lxp_ctx_charge_gas(ctx, length);
}

static lxp_result module_decode(lxp_module_ctx *ctx, uint16_t ordinal,
                                const uint8_t *payload, size_t length,
                                void **decoded)
{
    budget_decoded *value;
    void *memory;
    lxp_result status;
    if (ctx == NULL || decoded == NULL || ordinal == 0U || ordinal > 11U ||
        payload == NULL || length == 0U) return LXP_ERR_UNKNOWN_ACTIVITY;
    status = lxp_ctx_arena_alloc(ctx, sizeof(*value), _Alignof(budget_decoded),
                                 &memory);
    if (status != LXP_OK) return status;
    value = (budget_decoded *)memory;
    (void)memset(value, 0, sizeof(*value));
    value->ordinal = ordinal;
    value->payload = payload;
    value->payload_length = length;
    status = lxp_ctx_arena_alloc(ctx, sizeof(*value->typed),
                                 _Alignof(budget_typed_payload), &memory);
    if (status != LXP_OK) return status;
    value->typed = (budget_typed_payload *)memory;
    (void)memset(value->typed, 0, sizeof(*value->typed));
    switch (ordinal) {
    case 1U:
        status = lx_budget_create_decode(payload, length,
                                         &value->typed->create);
        break;
    case 2U:
        status = lx_budget_amount_decode(payload, length,
                                         &value->typed->amount);
        break;
    case 3U:
        status = lx_budget_amend_decode(payload, length, &value->typed->amend);
        break;
    case 4U:
    case 5U:
        status = lx_budget_delegate_decode(payload, length,
                                           &value->typed->delegate);
        break;
    case 6U:
        status = lx_budget_spend_decode(payload, length,
                                        &value->typed->spend);
        break;
    case 7U:
        status = lx_budget_close_decode(payload, length,
                                        &value->typed->close);
        break;
    case 8U:
        status = lx_budget_defund_decode(payload, length,
                                         &value->typed->defund);
        break;
    case 9U:
        status = lx_budget_revoke_decode(payload, length,
                                         &value->typed->revoke);
        break;
    case 10U:
        status = lx_budget_delegate_spend_decode(
            payload, length, &value->typed->delegate_spend);
        break;
    case 11U:
        status = lx_budget_pull_decode(payload, length, &value->typed->pull);
        break;
    default:
        return LXP_ERR_UNKNOWN_ACTIVITY;
    }
    if (status != LXP_OK) return status;
    *decoded = value;
    return LXP_OK;
}

static lxp_result module_validate(lxp_module_ctx *ctx,
                                  const lxp_activity *activity,
                                  const lxp_authority_resolved *authority,
                                  const void *decoded)
{
    const budget_decoded *value = (const budget_decoded *)decoded;
    if (ctx == NULL || activity == NULL || authority == NULL || value == NULL ||
        value->typed == NULL || value->ordinal == 0U || value->ordinal > 11U)
        return LXP_ERR_UNKNOWN_ACTIVITY;
    if (activity->activity_type !=
        (((uint32_t)LXP_MODULE_BUDGET << 16U) | value->ordinal))
        return LXP_ERR_UNKNOWN_ACTIVITY;
    return lxp_ctx_charge_gas(ctx, value->payload_length + 1U);
}

static lxp_result module_execute(lxp_module_ctx *ctx,
                                 const lxp_activity *activity,
                                 const lxp_authority_resolved *authority,
                                 const void *decoded,
                                 lxp_effect_buffer *effects)
{
    const budget_decoded *value = (const budget_decoded *)decoded;
    lx_account *signer = NULL;
    lxp_result status;
    (void)effects;
    if (ctx == NULL || value == NULL) return LXP_ERR_UNKNOWN_ACTIVITY;
    status = budget_authorized_signer(ctx, activity, authority, value, &signer);
    if (status != LXP_OK) return status;
    switch (value->ordinal) {
    case 1U:
        return budget_execute_create(ctx, activity, &value->typed->create,
                                     signer);
    case 2U:
        return budget_execute_fund(ctx, activity, &value->typed->amount,
                                   signer);
    case 3U:
        return budget_execute_amend(ctx, &value->typed->amend, signer);
    case 4U:
    case 5U:
        return budget_execute_delegate(ctx, value->ordinal,
                                       &value->typed->delegate, signer);
    case 6U:
        return budget_execute_spend(ctx, activity, authority,
                                    &value->typed->spend, signer);
    case 7U:
        return budget_execute_close(ctx, activity, &value->typed->close,
                                    signer);
    case 8U:
        return budget_execute_defund(ctx, activity, &value->typed->defund,
                                     signer);
    case 9U:
        return budget_execute_revoke(ctx, activity, &value->typed->revoke,
                                     signer);
    case 10U:
        return budget_execute_delegate_spend(
            ctx, activity, authority, &value->typed->delegate_spend, signer);
    case 11U:
        return budget_execute_pull(ctx, activity, &value->typed->pull, signer);
    default:
        break;
    }
    return LXP_ERR_UNKNOWN_ACTIVITY;
}

static lxp_result module_epoch_end(lxp_module_ctx *ctx, uint64_t epoch,
                                   uint64_t timestamp)
{
    (void)epoch;
    (void)timestamp;
    return ctx == NULL ? LXP_ERR_NON_CANONICAL : lxp_ctx_charge_gas(ctx, 1U);
}

static lxp_result module_state_root(lxp_module_ctx *ctx, uint8_t root[32])
{
    if (ctx == NULL || root == NULL) return LXP_ERR_NON_CANONICAL;
    return lxp_state_subtree_root(ctx->kernel, LXP_MODULE_BUDGET, root);
}

const lxp_module_iface *lx_budget_module_iface(void)
{
    static const lxp_module_iface iface = {
        LXP_MODULE_BUDGET, 1U, "budget", activity_types,
        sizeof(activity_types) / sizeof(activity_types[0]),
        module_genesis, module_decode, module_validate, module_execute,
        lx_budget_epoch_begin, module_epoch_end, module_state_root, NULL
    };
    return &iface;
}
