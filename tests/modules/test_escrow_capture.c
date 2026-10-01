#include "layerx/lx_escrow.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_receipt.h"
#include "layerx/lxp_state.h"

#include <string.h>

static size_t transfer_calls;
static uint16_t last_reason;
static lxp_authorization_kind last_authority;

static lxp_result apply_capability(lxp_kernel *kernel,
                                   const lxp_transfer_set *set,
                                   lxp_receipt *receipt)
{
    lxp_transfer_set_result result;
    lxp_transfer_context context = set->context;
    lxp_result status;
    (void)kernel;
    if (set->context.source_authorities == NULL ||
        set->context.source_authority_count != 1U)
        return LXP_ERR_NON_CANONICAL;
    ++transfer_calls;
    last_reason = set->legs[0].reason;
    last_authority = set->context.source_authorities[0].debit_authority_kind;
    status = lxp_apply_transfer_set((lxp_transfer_leg *)set->legs,
                                    set->leg_count, &context, &result);
    if (status == LXP_OK)
        (void)memcpy(receipt->transfer_set_root, result.transfer_set_root, 32U);
    return status;
}

static lxp_result build_capture_payload(const uint8_t escrow_id[32],
                                        lxp_u128 amount,
                                        const uint8_t idempotency_key[32],
                                        uint8_t payload[LX_ESCROW_CAPTURE_PAYLOAD_BYTES])
{
    (void)memset(payload, 0, (size_t)LX_ESCROW_CAPTURE_PAYLOAD_BYTES);
    (void)memcpy(payload, escrow_id, 32U);
    (void)memcpy(payload + 48U, idempotency_key, 32U);
    return lxp_u128_to_be(amount, payload + 32U);
}

static lxp_result dispatch(lxp_module_ctx *ctx, lxp_effect_buffer *effects,
                           uint32_t activity_type, uint16_t ordinal,
                           const uint8_t *payload, size_t payload_length,
                           const lxp_authority_resolved *authority)
{
    const lxp_module_iface *iface = lx_escrow_module_iface();
    lxp_activity activity;
    void *decoded = NULL;
    lxp_result status;
    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = activity_type;
    activity.payload.bytes = payload;
    activity.payload.length = payload_length;
    status = iface->decode(ctx, ordinal, payload, payload_length, &decoded);
    if (status == LXP_OK)
        status = iface->validate(ctx, &activity, authority, decoded);
    if (status != LXP_OK) return status;
    return iface->execute(ctx, &activity, authority, decoded, effects);
}

int main(void)
{
    static lxp_effect_buffer effects;
    lx_asset_registry assets;
    lx_asset_record asset;
    lx_account_registry accounts;
    lx_account *owner;
    lx_account *escrow_account;
    lx_account *beneficiary;
    lx_escrow_runtime runtime;
    lx_escrow_record record;
    lx_escrow_record stored;
    lx_escrow_capture_request request;
    lxp_authority_resolved authority;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lxp_receipt first_receipt;
    lxp_receipt replay_receipt;
    lxp_receipt scratch;
    uint8_t arena_bytes[16384];
    uint8_t payload[LX_ESCROW_CAPTURE_PAYLOAD_BYTES];
    uint8_t partial_key[32];
    uint8_t full_key[32];
    uint8_t expired_key[32];
    uint64_t parameters = 1U;
    lxp_u128 remaining;
    bool found;
    const char *owner_name = "agent:did:key:owner:main";
    const char *escrow_name = "agent:did:key:owner:escrow:hold-1";
    const char *beneficiary_name = "agent:did:key:provider:main";

    (void)memset(&asset, 0, sizeof(asset));
    asset.asset_id[0] = 1U;
    asset.symbol_length = 3U;
    (void)memcpy(asset.symbol, "USD", 4U);
    asset.name[0] = (uint8_t)'A';
    asset.name_length = 1U;
    asset.issuer_kind = 2U;
    asset.issuer_did32[0] = 1U;
    asset.custody_kind = LX_ASSET_CUSTODY_PAXEER;
    asset.custody_reference[0] = 1U;
    asset.custody_reference_length = 1U;
    (void)memset(&authority, 0, sizeof(authority));
    (void)memset(&first_receipt, 0, sizeof(first_receipt));
    (void)memset(&replay_receipt, 0xff, sizeof(replay_receipt));
    (void)memset(partial_key, 0, sizeof(partial_key));
    (void)memset(full_key, 0, sizeof(full_key));
    (void)memset(expired_key, 0, sizeof(expired_key));
    partial_key[0] = 1U;
    full_key[0] = 2U;
    expired_key[0] = 3U;
    if (lx_asset_registry_init(&assets, 0U) != LXP_OK ||
        lx_asset_register(&assets, &asset, 0U,
                          (lxp_u128){ 0U, 0U }) != LXP_OK ||
        lx_account_registry_init(&accounts) != LXP_OK ||
        lx_asset_account_open(&assets, &accounts, asset.asset_id,
                              (const uint8_t *)owner_name, strlen(owner_name),
                              1U, LX_ACCOUNT_OPEN_CREDIT, NULL,
                              &owner) != LXP_OK ||
        lx_asset_account_open(&assets, &accounts, asset.asset_id,
                              (const uint8_t *)escrow_name,
                              strlen(escrow_name), 2U, LX_ACCOUNT_OPEN_CREDIT,
                              NULL, &escrow_account) != LXP_OK ||
        lx_asset_account_open(&assets, &accounts, asset.asset_id,
                              (const uint8_t *)beneficiary_name,
                              strlen(beneficiary_name), 3U,
                              LX_ACCOUNT_OPEN_CREDIT, NULL,
                              &beneficiary) != LXP_OK ||
        lxp_ledger_bootstrap_balance(escrow_account, asset.asset_id,
                                     (lxp_u128){ 0U, 100U }, 0U) != LXP_OK ||
        lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, lx_escrow_module_iface()) != LXP_OK ||
        lxp_kernel_set_capabilities(&kernel, NULL, apply_capability) != LXP_OK ||
        lxp_kernel_bind_module_runtime(&kernel, LXP_MODULE_ESCROW,
                                       &runtime) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ESCROW, 500U, 0U, 1U,
                            100000U, &arena, true) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    runtime.accounts = &accounts;
    runtime.assets = &assets;

    (void)memset(&record, 0, sizeof(record));
    record.escrow_id[0] = 3U;
    (void)memcpy(record.owner, owner->id, 32U);
    (void)memcpy(record.escrow_account, escrow_account->id, 32U);
    (void)memcpy(record.beneficiary, beneficiary->id, 32U);
    record.arbiter[0] = 8U;
    (void)memcpy(record.asset_id, asset.asset_id, 32U);
    record.locked_amount = (lxp_u128){ 0U, 100U };
    record.state = LX_ESCROW_STATE_OPEN;
    record.expiry = 1000U;
    record.dispute_window = 900U;
    if (lx_escrow_state_put(&ctx, &record) != LXP_OK ||
        lx_escrow_state_put(&ctx, &record) != LXP_ERR_ESCROW_STATE ||
        lxp_module_ctx_commit(&ctx) != LXP_OK)
        return 1;

    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.principal, beneficiary->id, 32U);
    (void)memcpy(authority.actor, beneficiary->id, 32U);
    if (build_capture_payload(record.escrow_id, (lxp_u128){ 0U, 30U },
                              partial_key, payload) != LXP_OK ||
        dispatch(&ctx, &effects, LX_ESCROW_PARTIAL_CAPTURE, 3U, payload,
                 sizeof(payload), &authority) != LXP_OK ||
        transfer_calls != 1U || last_reason != LXP_REASON_ESCROW_CAPTURE ||
        last_authority != LXP_AUTH_ESCROW ||
        escrow_account->balance.lo != 70U || beneficiary->balance.lo != 30U ||
        lx_escrow_lookup(&ctx, record.escrow_id, &stored) != LXP_OK ||
        stored.captured_amount.lo != 30U || stored.locked_amount.lo != 70U ||
        stored.state != LX_ESCROW_STATE_PARTIALLY_CAPTURED ||
        lx_escrow_invariant_check(&stored, escrow_account) != LXP_OK ||
        lx_escrow_remaining(&stored, escrow_account, &remaining) != LXP_OK ||
        remaining.lo != 70U || effects.count != 1U ||
        effects.effects[0].event_type != 3U ||
        effects.effects[0].body[34] !=
            (uint8_t)LX_ESCROW_STATE_PARTIALLY_CAPTURED)
        return 1;
    if (lx_escrow_receipt_replay(&ctx, partial_key, &first_receipt,
                                 &found) != LXP_OK || !found ||
        first_receipt.module_id != LXP_MODULE_ESCROW ||
        first_receipt.operation != 3U || first_receipt.amount.lo != 30U ||
        memcmp(first_receipt.from, escrow_account->id, 32U) != 0 ||
        memcmp(first_receipt.to, beneficiary->id, 32U) != 0)
        return 1;

    (void)memset(&request, 0, sizeof(request));
    request.escrow_id = record.escrow_id;
    request.escrow_account = escrow_account;
    request.beneficiary_account = beneficiary;
    request.owner_account = owner;
    request.asset = &asset;
    request.authority = &authority;
    request.amount = (lxp_u128){ 0U, 5U };
    (void)memcpy(request.idempotency_key, partial_key, 32U);
    /* Reused inner key with a changed amount is a different request. */
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_CONTEXT_MISMATCH ||
        transfer_calls != 1U ||
        escrow_account->balance.lo != 70U || beneficiary->balance.lo != 30U)
        return 1;
    request.amount = (lxp_u128){ 0U, 30U };
    if (lx_escrow_partial_capture_execute(&ctx, &request, &replay_receipt) !=
            LXP_OK ||
        transfer_calls != 1U ||
        memcmp(&first_receipt, &replay_receipt, sizeof(first_receipt)) != 0 ||
        escrow_account->balance.lo != 70U || beneficiary->balance.lo != 30U)
        return 1;
    /* Changed actor: the owner's delegate may capture, but not replay the
     * beneficiary's result. */
    authority.kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    (void)memcpy(authority.principal, owner->id, 32U);
    (void)memcpy(authority.actor, owner->id, 32U);
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_CONTEXT_MISMATCH || transfer_calls != 1U)
        return 1;
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.principal, beneficiary->id, 32U);
    (void)memcpy(authority.actor, beneficiary->id, 32U);
    /* Cross-operation: a full capture or a release cannot claim it. */
    if (lx_escrow_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_CONTEXT_MISMATCH || transfer_calls != 1U)
        return 1;
    {
        lx_escrow_release_request release;
        lxp_authority_resolved owner_authority = authority;
        (void)memset(&release, 0, sizeof(release));
        (void)memcpy(owner_authority.principal, owner->id, 32U);
        (void)memcpy(owner_authority.actor, owner->id, 32U);
        release.escrow_id = record.escrow_id;
        release.escrow_account = escrow_account;
        release.owner_account = owner;
        release.asset = &asset;
        release.authority = &owner_authority;
        (void)memcpy(release.idempotency_key, partial_key, 32U);
        if (lx_escrow_release_execute(&ctx, &release, &scratch) !=
                LXP_ERR_CONTEXT_MISMATCH || transfer_calls != 1U)
            return 1;
    }
    /* Cross-hold: the same inner key on a second real hold. */
    {
        lx_escrow_record second = record;
        lx_escrow_record second_after;
        second.escrow_id[0] = 4U;
        if (lx_escrow_state_put(&ctx, &second) != LXP_OK)
            return 1;
        request.escrow_id = second.escrow_id;
        if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
                LXP_ERR_CONTEXT_MISMATCH || transfer_calls != 1U ||
            lx_escrow_lookup(&ctx, second.escrow_id, &second_after) !=
                LXP_OK ||
            second_after.state != LX_ESCROW_STATE_OPEN ||
            !lxp_u128_is_zero(second_after.captured_amount))
            return 1;
        request.escrow_id = record.escrow_id;
    }
    /* Routed: a fresh Activity reusing the inner key with a changed amount
     * fails and emits nothing; the exact retry emits the stored transition. */
    if (lxp_effect_buffer_init(&effects) != LXP_OK ||
        build_capture_payload(record.escrow_id, (lxp_u128){ 0U, 31U },
                              partial_key, payload) != LXP_OK ||
        dispatch(&ctx, &effects, LX_ESCROW_PARTIAL_CAPTURE, 3U, payload,
                 sizeof(payload), &authority) != LXP_ERR_CONTEXT_MISMATCH ||
        effects.count != 0U || transfer_calls != 1U ||
        build_capture_payload(record.escrow_id, (lxp_u128){ 0U, 30U },
                              partial_key, payload) != LXP_OK ||
        dispatch(&ctx, &effects, LX_ESCROW_CAPTURE, 2U, payload,
                 sizeof(payload), &authority) != LXP_ERR_CONTEXT_MISMATCH ||
        effects.count != 0U || transfer_calls != 1U ||
        dispatch(&ctx, &effects, LX_ESCROW_PARTIAL_CAPTURE, 3U, payload,
                 sizeof(payload), &authority) != LXP_OK ||
        transfer_calls != 1U || effects.count != 1U ||
        effects.effects[0].event_type != 3U ||
        memcmp(effects.effects[0].body, record.escrow_id, 32U) != 0 ||
        effects.effects[0].body[34] !=
            (uint8_t)LX_ESCROW_STATE_PARTIALLY_CAPTURED ||
        escrow_account->balance.lo != 70U || beneficiary->balance.lo != 30U)
        return 1;
    if (lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ESCROW, 500U, 0U, 1U,
                            100000U, &arena, true) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    /* Restart: the committed state store and the committed module state
     * table are restored into a fresh kernel; the context commitment and
     * original receipt survive. */
    {
        static lxp_module_kv_entry saved_kv[LXP_KERNEL_MAX_MODULE_KV];
        size_t saved_kv_count = kernel.module_kv_count;
        lxp_state_snapshot *snapshot = NULL;
        if (saved_kv_count == 0U || saved_kv_count > LXP_KERNEL_MAX_MODULE_KV)
            return 1;
        (void)memcpy(saved_kv, kernel.module_kv,
                     saved_kv_count * sizeof(saved_kv[0]));
        if (lxp_state_snapshot_create(&state, &snapshot) != LXP_OK ||
            lxp_state_store_destroy(&state) != LXP_OK ||
            lxp_state_store_init(&state, 0U) != LXP_OK ||
            lxp_state_snapshot_restore(snapshot, &state) != LXP_OK)
            return 1;
        lxp_state_snapshot_destroy(snapshot);
        if (lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) !=
                LXP_OK ||
            lxp_kernel_register_module(&kernel, lx_escrow_module_iface()) !=
                LXP_OK ||
            lxp_kernel_set_capabilities(&kernel, NULL, apply_capability) !=
                LXP_OK ||
            lxp_kernel_bind_module_runtime(&kernel, LXP_MODULE_ESCROW,
                                           &runtime) != LXP_OK ||
            kernel.module_kv_count != 0U)
            return 1;
        (void)memcpy(kernel.module_kv, saved_kv,
                     saved_kv_count * sizeof(saved_kv[0]));
        kernel.module_kv_count = saved_kv_count;
        if (lxp_effect_buffer_init(&effects) != LXP_OK ||
            lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ESCROW, 500U, 0U,
                                1U, 100000U, &arena, true) != LXP_OK ||
            lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
            return 1;
        request.amount = (lxp_u128){ 0U, 29U };
        if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
                LXP_ERR_CONTEXT_MISMATCH ||
            lx_escrow_capture_execute(&ctx, &request, &scratch) !=
                LXP_ERR_CONTEXT_MISMATCH)
            return 1;
        request.amount = (lxp_u128){ 0U, 30U };
        (void)memset(&replay_receipt, 0xff, sizeof(replay_receipt));
        if (lx_escrow_partial_capture_execute(&ctx, &request,
                                              &replay_receipt) != LXP_OK ||
            memcmp(&first_receipt, &replay_receipt,
                   sizeof(first_receipt)) != 0 ||
            transfer_calls != 1U || escrow_account->balance.lo != 70U ||
            beneficiary->balance.lo != 30U ||
            lx_escrow_lookup(&ctx, record.escrow_id, &stored) != LXP_OK ||
            stored.captured_amount.lo != 30U ||
            stored.state != LX_ESCROW_STATE_PARTIALLY_CAPTURED ||
            lxp_module_ctx_commit(&ctx) != LXP_OK)
            return 1;
    }

    (void)memcpy(request.idempotency_key, full_key, 32U);
    request.amount = (lxp_u128){ 0U, 71U };
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_CAPTURE_EXCEEDS_HOLD || transfer_calls != 1U)
        return 1;
    request.amount = (lxp_u128){ 0U, 70U };
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_CAPTURE_EXCEEDS_HOLD || transfer_calls != 1U)
        return 1;
    request.amount = (lxp_u128){ 0U, 1U };
    (void)memset(authority.principal, 0xa5, 32U);
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_UNAUTHORIZED_CAPTURE || transfer_calls != 1U)
        return 1;
    (void)memcpy(authority.principal, beneficiary->id, 32U);
    {
        uint8_t missing[32];
        (void)memset(missing, 0, sizeof(missing));
        missing[0] = 0x5aU;
        request.escrow_id = missing;
        if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_ESCROW_STATE)
            return 1;
        request.escrow_id = record.escrow_id;
    }
    if (lx_escrow_lookup(&ctx, record.escrow_id, &stored) != LXP_OK ||
        stored.captured_amount.lo != 30U || stored.locked_amount.lo != 70U ||
        stored.state != LX_ESCROW_STATE_PARTIALLY_CAPTURED)
        return 1;

    if (lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ESCROW, 1000U, 0U, 2U,
                            100000U, &arena, true) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    (void)memcpy(request.idempotency_key, expired_key, 32U);
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_HOLD_EXPIRED || transfer_calls != 1U ||
        escrow_account->balance.lo != 70U || beneficiary->balance.lo != 30U ||
        lx_escrow_lookup(&ctx, record.escrow_id, &stored) != LXP_OK ||
        stored.captured_amount.lo != 30U || stored.locked_amount.lo != 70U ||
        stored.state != LX_ESCROW_STATE_PARTIALLY_CAPTURED ||
        lx_escrow_receipt_replay(&ctx, expired_key, &scratch, &found) !=
            LXP_OK || found || effects.count != 0U)
        return 1;

    if (lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ESCROW, 900U, 0U, 3U,
                            100000U, &arena, true) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    authority.kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    (void)memcpy(authority.principal, owner->id, 32U);
    if (build_capture_payload(record.escrow_id, (lxp_u128){ 0U, 0U }, full_key,
                              payload) != LXP_OK ||
        dispatch(&ctx, &effects, LX_ESCROW_CAPTURE, 2U, payload,
                 sizeof(payload), &authority) != LXP_OK ||
        transfer_calls != 2U || last_authority != LXP_AUTH_ESCROW ||
        escrow_account->balance.lo != 0U || beneficiary->balance.lo != 100U ||
        lx_escrow_lookup(&ctx, record.escrow_id, &stored) != LXP_OK ||
        stored.captured_amount.lo != 100U ||
        !lxp_u128_is_zero(stored.locked_amount) ||
        stored.state != LX_ESCROW_STATE_CAPTURED ||
        lx_escrow_invariant_check(&stored, escrow_account) != LXP_OK ||
        effects.count != 1U || effects.effects[0].event_type != 2U ||
        effects.effects[0].body[34] != (uint8_t)LX_ESCROW_STATE_CAPTURED)
        return 1;

    (void)memcpy(request.idempotency_key, expired_key, 32U);
    request.amount = (lxp_u128){ 0U, 1U };
    if (lx_escrow_partial_capture_execute(&ctx, &request, &scratch) !=
            LXP_ERR_ESCROW_STATE || transfer_calls != 2U ||
        lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lxp_state_store_destroy(&state) != LXP_OK)
        return 1;
    return 0;
}
