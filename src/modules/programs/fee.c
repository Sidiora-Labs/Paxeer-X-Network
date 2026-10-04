#include "layerx/programs.h"

#include "layerx/lxp_fee.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_kernel.h"

#include <stdlib.h>
#include <string.h>

typedef struct programs_fee_token {
    lx_account *actor;
    lx_account *treasury;
    lxp_u128 actor_balance;
    lxp_u128 treasury_balance;
    uint8_t actor_asset[32];
    uint8_t treasury_asset[32];
    uint64_t actor_sequence;
    uint64_t treasury_sequence;
    bool actor_has_asset;
    bool treasury_has_asset;
} programs_fee_token;

lxp_result lxp_programs_migration_admit(
    lxp_module_ctx *ctx, const lxp_activity *activity,
    const lxp_authority_resolved *authority, lxp_u128 validation_fee,
    uint32_t module_version, uint32_t parameter_version,
    uint32_t recorded_fee_version, uint32_t recorded_metering_version)
{
    static const uint64_t limits[7] = {
        1000000U, 16777216U, 1048576U, 1048576U, 64U, 1048576U, 4096U
    };
    lxp_migration_admission_facts facts;
    lxp_migration_profile profile;
    lx_programs_fee_schedule schedule;
    lx_programs_metering_schedule metering;
    const lx_programs_transfer_runtime *runtime;
    lx_account *payer;
    lxp_u128 maximum = {0U, 0U};
    lxp_u128 combined;
    lxp_result status;
    size_t index;
    if (ctx == NULL || ctx->kernel == NULL || activity == NULL ||
        authority == NULL || activity->activity_type != LX_PROGRAMS_UPGRADE ||
        ctx->migration_admission.present ||
        lxp_ct_is_zero(ctx->activity_id, 32U))
        return LXP_ERR_NON_CANONICAL;
    (void)memset(&profile, 0, sizeof(profile));
    status = lxp_programs_migration_profile_at(
        ctx, module_version, parameter_version, ctx->epoch, &profile);
    if (status != LXP_OK) {
        bool preactivation = false;
        const lxp_migration_admission_facts *sealed = &ctx->migration_admission;
        if (status != LXP_ERR_VERSION_UNSUPPORTED ||
            !sealed->replay_prestate_authenticated ||
            sealed->module_version != module_version ||
            sealed->parameter_version != parameter_version ||
            lxp_ct_is_zero(sealed->replay_receipt_digest, 32U) ||
            lxp_ct_is_zero(sealed->replay_prestate_root, 32U) ||
            lxp_ct_memcmp(sealed->replay_prestate_root,
                          ctx->kernel->current_state_root, 32U) != 0)
            return status;
        status = lxp_programs_migration_preactivation(ctx, &preactivation);
        if (status != LXP_OK) return status;
        if (!preactivation ||
            (sealed->replay_result_code == LXP_OK ?
                lxp_u128_cmp(sealed->replay_fee, validation_fee) != 0 :
                !lxp_u128_is_zero(sealed->replay_fee)))
            return LXP_ERR_CONTEXT_MISMATCH;
        facts = *sealed;
        facts.profile_version = 0U;
        facts.validation_fee = validation_fee;
        facts.combined_fee = validation_fee;
        facts.runtime_fee = (lxp_u128){0U, 0U};
        (void)memcpy(facts.activity_binding, ctx->activity_id, 32U);
        (void)memcpy(facts.limits, limits, sizeof(limits));
        facts.legacy_replay_authenticated = true;
        facts.present = true;
        ctx->migration_admission = facts;
        return LXP_OK;
    }
    if (profile.version != 1U || profile.module_version != module_version ||
        profile.parameter_version != parameter_version ||
        profile.activation_epoch > ctx->epoch ||
        lxp_ct_is_zero(profile.authority_digest, 32U) ||
        memcmp(profile.limits, limits, sizeof(limits)) != 0)
        return LXP_ERR_VERSION_UNSUPPORTED;
    (void)memset(&facts, 0, sizeof(facts));
    status = lxp_programs_fee_schedule_at(
        ctx, recorded_fee_version, &schedule, facts.fee_asset);
    if (status != LXP_OK) return status;
    status = lxp_programs_metering_schedule_at(
        ctx->kernel, recorded_metering_version, ctx->batch_number, &metering);
    if (status != LXP_OK) return status;
    runtime = lxp_ctx_module_runtime(ctx);
    if (runtime == NULL || runtime->accounts == NULL ||
        lxp_ct_memcmp(runtime->occupancy_asset_id, facts.fee_asset, 32U) != 0)
        return LXP_ERR_ASSET_MISMATCH;
    status = lxp_kernel_program_payment_account(
        runtime->accounts, authority->principal, facts.fee_asset,
        activity->protocol_version, &payer);
    if (status != LXP_OK) return status;
    if (payer == NULL || !payer->has_asset || payer->frozen ||
        lxp_ct_memcmp(payer->asset_id, facts.fee_asset, 32U) != 0)
        return LXP_ERR_FEE_UNPAYABLE;
    facts.prices[0] = schedule.cpu;
    facts.prices[1] = schedule.memory_byte;
    facts.prices[2] = schedule.storage_read_byte;
    facts.prices[3] = schedule.storage_write_byte;
    facts.prices[4] = schedule.output_value;
    facts.prices[5] = schedule.output_byte;
    facts.prices[6] = schedule.occupancy_byte_batch;
    for (index = 0U; index < 6U; ++index) {
        lxp_u256 product;
        lxp_u128 amount;
        status = lxp_u128_mul((lxp_u128){0U, limits[index]},
                             (lxp_u128){0U, facts.prices[index]}, &product);
        if (status != LXP_OK) return status;
        if (product.words[2] != 0U || product.words[3] != 0U)
            return LXP_ERR_OVERFLOW;
        amount = (lxp_u128){product.words[1], product.words[0]};
        status = lxp_u128_add(maximum, amount, &maximum);
        if (status != LXP_OK) return status;
    }
    status = lxp_u128_add(validation_fee, maximum, &combined);
    if (status != LXP_OK) return status;
    if (lxp_u128_cmp(combined, activity->fee_limit) > 0 ||
        lxp_u128_cmp(combined, payer->balance) > 0)
        return LXP_ERR_FEE_UNPAYABLE;
    facts.available_fee_units = payer->balance;
    facts.signed_fee_limit = activity->fee_limit;
    facts.coverage = lxp_u128_cmp(payer->balance, activity->fee_limit) < 0 ?
        payer->balance : activity->fee_limit;
    status = lxp_u128_sub(facts.coverage, validation_fee, &facts.coverage);
    if (status != LXP_OK) return status;
    facts.validation_fee = validation_fee;
    facts.maximum_fee = maximum;
    facts.module_version = module_version;
    facts.parameter_version = parameter_version;
    facts.profile_version = profile.version;
    facts.activation_epoch = profile.activation_epoch;
    facts.fee_schedule_version = schedule.version;
    facts.metering_schedule_version = metering.version;
    (void)memcpy(facts.activity_binding, ctx->activity_id, 32U);
    (void)memcpy(facts.payer, payer->id, 32U);
    (void)memcpy(facts.profile_authority, profile.authority_digest, 32U);
    (void)memcpy(facts.limits, limits, sizeof(limits));
    (void)memcpy(facts.metering_coefficients, metering.coefficients,
                 sizeof(facts.metering_coefficients));
    facts.present = true;
    ctx->migration_admission = facts;
    return LXP_OK;
}

static lxp_result prepare_fee(lxp_kernel *kernel, const lxp_activity *activity,
                              const lxp_authority_resolved *authority,
                              lxp_u128 fee, void **transaction)
{
    lx_programs_transfer_runtime *runtime;
    programs_fee_token *token;
    lx_account *actor;
    lx_account *treasury;
    lxp_transfer_context context;
    lxp_transfer_result transfer;
    lxp_receipt receipt;
    lxp_result status;
    if (kernel == NULL || activity == NULL || authority == NULL ||
        transaction == NULL || lxp_u128_is_zero(fee))
        return LXP_ERR_NON_CANONICAL;
    *transaction = NULL;
    runtime = (lx_programs_transfer_runtime *)
        kernel->module_runtime[LXP_MODULE_PROGRAMS];
    if (runtime == NULL || runtime->accounts == NULL ||
        runtime->assets == NULL || runtime->asset_count == 0U)
        return LXP_FATAL_INVARIANT;
    if (activity->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT &&
        lxp_activity_module_id(activity->activity_type) != LXP_MODULE_PROGRAMS) {
        size_t slot = 0U;
        status = lx_account_registry_index_lookup(runtime->accounts, authority->principal, &slot);
        if (status != LXP_OK) return status;
        actor = &runtime->accounts->accounts[slot];
    } else {
        status = lxp_kernel_program_payment_account(runtime->accounts,
            authority->principal, runtime->occupancy_asset_id,
            activity->protocol_version, &actor);
        if (status != LXP_OK) return status;
    }
    if (!actor->has_asset ||
        memcmp(actor->asset_id, runtime->occupancy_asset_id, 32U) != 0)
        return LXP_ERR_ASSET_MISMATCH;
    status = lxp_fee_treasury_account(runtime->accounts, &treasury);
    if (status != LXP_OK) return status;
    token = (programs_fee_token *)malloc(sizeof(*token));
    if (token == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    token->actor = actor;
    token->treasury = treasury;
    token->actor_balance = actor->balance;
    token->treasury_balance = treasury->balance;
    (void)memcpy(token->actor_asset, actor->asset_id, 32U);
    (void)memcpy(token->treasury_asset, treasury->asset_id, 32U);
    token->actor_sequence = actor->next_sequence;
    token->treasury_sequence = treasury->next_sequence;
    token->actor_has_asset = actor->has_asset;
    token->treasury_has_asset = treasury->has_asset;
    (void)memset(&context, 0, sizeof(context));
    context.assets = runtime->assets;
    context.asset_count = runtime->asset_count;
    (void)memcpy(context.authorized_from, actor->id, 32U);
    context.actor_sequence = actor->next_sequence;
    context.protocol_system_capability = true;
    context.sequence_account = treasury;
    context.origin_module_id = LXP_MODULE_PROGRAMS;
    context.debit_authority_kind = LXP_AUTH_OWNER;
    (void)memset(&receipt, 0, sizeof(receipt));
    status = lxp_fee_charge(actor, treasury, actor->asset_id, fee,
                            activity->fee_limit, &context, &receipt,
                            &transfer);
    if (status != LXP_OK) {
        free(token);
        return status;
    }
    *transaction = token;
    return LXP_OK;
}

static void commit_fee(lxp_kernel *kernel, void *transaction)
{
    (void)kernel;
    free(transaction);
}

static void rollback_fee(lxp_kernel *kernel, void *transaction)
{
    programs_fee_token *token = (programs_fee_token *)transaction;
    (void)kernel;
    if (token == NULL) return;
    (void)lxp_ledger_restore_account_snapshot(
        token->actor, token->actor_balance, token->actor_asset,
        token->actor_has_asset, token->actor_sequence);
    (void)lxp_ledger_restore_account_snapshot(
        token->treasury, token->treasury_balance, token->treasury_asset,
        token->treasury_has_asset, token->treasury_sequence);
    free(token);
}

lxp_result lxp_programs_bind_fee_transaction(lxp_kernel *kernel)
{
    static const lxp_kernel_fee_transaction transaction = {
        prepare_fee, commit_fee, rollback_fee
    };
    return lxp_kernel_set_fee_transaction(kernel, &transaction);
}
