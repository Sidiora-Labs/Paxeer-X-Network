#include "layerx/lxp_fee.h"

#include <string.h>

lxp_result lxp_fee_admission_check(
    lxp_admission_result admission, lxp_u128 fee_limit,
    lxp_u128 actor_spendable_fee_balance,
    lxp_fee_policy_decision *decision)
{
    if (decision == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memset(decision, 0, sizeof(*decision));
    decision->result_code = admission.result_code;
    if (admission.result_code != LXP_OK) {
        if (admission.assign_global_sequence ||
            admission.consume_account_sequence || admission.charge_fee)
            return LXP_FATAL_INVARIANT;
        return LXP_OK;
    }
    if (!admission.assign_global_sequence ||
        !admission.consume_account_sequence || !admission.charge_fee)
        return LXP_FATAL_INVARIANT;
    if (lxp_u128_cmp(actor_spendable_fee_balance, fee_limit) < 0) {
        decision->result_code = LXP_ERR_FEE_UNPAYABLE;
        return LXP_OK;
    }
    decision->assign_global_sequence = true;
    decision->consume_account_sequence = true;
    return LXP_OK;
}

lxp_result lxp_fee_rejection_policy(
    const lxp_fee_policy_decision *admission, lxp_result execution_result,
    lxp_u128 computed_fee, lxp_u128 fee_limit,
    lxp_fee_policy_decision *decision)
{
    if (admission == NULL || decision == NULL)
        return LXP_ERR_NON_CANONICAL;
    if (!admission->assign_global_sequence) {
        if (admission->consume_account_sequence || admission->charge_fee ||
            admission->apply_module_effects ||
            admission->result_code == LXP_OK)
            return LXP_FATAL_INVARIANT;
        *decision = *admission;
        return LXP_OK;
    }
    if (!admission->consume_account_sequence ||
        admission->result_code != LXP_OK || lxp_result_is_fatal(execution_result))
        return LXP_FATAL_INVARIANT;
    (void)memset(decision, 0, sizeof(*decision));
    decision->assign_global_sequence = true;
    decision->consume_account_sequence = true;
    if (lxp_u128_cmp(computed_fee, fee_limit) > 0) {
        decision->result_code = LXP_ERR_FEE_LIMIT;
        decision->fee_charged = fee_limit;
    } else {
        decision->result_code = execution_result;
        decision->fee_charged = computed_fee;
    }
    decision->charge_fee = !lxp_u128_is_zero(decision->fee_charged);
    decision->apply_module_effects = decision->result_code == LXP_OK;
    return LXP_OK;
}

static bool entry_equal(const lxp_fee_replay_entry *left,
                        const lxp_fee_replay_entry *right)
{
    return lxp_u128_cmp(left->fee_charged, right->fee_charged) == 0 &&
           lxp_u128_cmp(left->actor_fee_debit,
                        right->actor_fee_debit) == 0 &&
           lxp_u128_cmp(left->treasury_fee_credit,
                        right->treasury_fee_credit) == 0 &&
           lxp_u128_cmp(left->treasury_balance,
                        right->treasury_balance) == 0 &&
           left->parameter_version == right->parameter_version &&
           memcmp(left->resulting_state_root,
                  right->resulting_state_root, 32U) == 0;
}

lxp_result lxp_fee_replay_check(
    const lxp_fee_replay_entry *committed,
    const lxp_fee_replay_entry *replayed, size_t count,
    lxp_u128 initial_treasury_balance)
{
    lxp_u128 committed_debits = {0U, 0U};
    lxp_u128 committed_credits = {0U, 0U};
    lxp_u128 replayed_debits = {0U, 0U};
    lxp_u128 replayed_credits = {0U, 0U};
    lxp_u128 treasury = initial_treasury_balance;
    size_t i;
    if ((committed == NULL || replayed == NULL) && count != 0U)
        return LXP_ERR_NON_CANONICAL;
    for (i = 0U; i < count; ++i) {
        lxp_u128 updated;
        if (committed[i].parameter_version == 0U ||
            lxp_u128_cmp(committed[i].fee_charged,
                         committed[i].actor_fee_debit) != 0 ||
            lxp_u128_cmp(committed[i].fee_charged,
                         committed[i].treasury_fee_credit) != 0 ||
            lxp_u128_cmp(replayed[i].fee_charged,
                         replayed[i].actor_fee_debit) != 0 ||
            lxp_u128_cmp(replayed[i].fee_charged,
                         replayed[i].treasury_fee_credit) != 0)
            return LXP_FATAL_SUPPLY_MISMATCH;
        if (!entry_equal(&committed[i], &replayed[i]))
            return LXP_FATAL_REPLAY_DIVERGENCE;
        if (lxp_u128_add(treasury, committed[i].treasury_fee_credit,
                         &updated) != LXP_OK)
            return LXP_ERR_OVERFLOW;
        treasury = updated;
        if (lxp_u128_cmp(treasury, committed[i].treasury_balance) != 0)
            return LXP_FATAL_REPLAY_DIVERGENCE;
        if (lxp_u128_add(committed_debits, committed[i].actor_fee_debit,
                         &updated) != LXP_OK)
            return LXP_ERR_OVERFLOW;
        committed_debits = updated;
        if (lxp_u128_add(committed_credits,
                         committed[i].treasury_fee_credit, &updated) != LXP_OK)
            return LXP_ERR_OVERFLOW;
        committed_credits = updated;
        if (lxp_u128_add(replayed_debits, replayed[i].actor_fee_debit,
                         &updated) != LXP_OK)
            return LXP_ERR_OVERFLOW;
        replayed_debits = updated;
        if (lxp_u128_add(replayed_credits,
                         replayed[i].treasury_fee_credit, &updated) != LXP_OK)
            return LXP_ERR_OVERFLOW;
        replayed_credits = updated;
    }
    if (lxp_u128_cmp(committed_debits, committed_credits) != 0 ||
        lxp_u128_cmp(replayed_debits, replayed_credits) != 0 ||
        lxp_u128_cmp(committed_debits, replayed_debits) != 0)
        return LXP_FATAL_SUPPLY_MISMATCH;
    return LXP_OK;
}

bool lxp_fee_two_class_active(uint32_t parameter_version)
{
    return parameter_version >= (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION;
}

bool lxp_fee_ordering_refusal(lxp_result refusal)
{
    if (refusal == LXP_OK || refusal == LXP_ERR_IDEMPOTENT_REPLAY ||
        lxp_result_is_fatal(refusal))
        return false;
    switch (lxp_result_domain(refusal)) {
    case LXP_RESULT_DOMAIN_CODEC:
    case LXP_RESULT_DOMAIN_ENVELOPE:
    case LXP_RESULT_DOMAIN_AUTHORITY:
    case LXP_RESULT_DOMAIN_SEQUENCING:
    case LXP_RESULT_DOMAIN_LEDGER:
    case LXP_RESULT_DOMAIN_ARITHMETIC:
    case LXP_RESULT_DOMAIN_METERING:
    case LXP_RESULT_DOMAIN_MODULE:
        return true;
    default:
        return false;
    }
}

static lxp_fee_transition fee_transition(uint8_t actor_sequence,
    uint8_t global_sequence, bool charge_fee, bool module_effects,
    lxp_fee_receipt_kind receipt, lxp_fee_retry retry)
{
    lxp_fee_transition transition;
    transition.actor_sequence = actor_sequence;
    transition.global_sequence = global_sequence;
    transition.charge_fee = charge_fee;
    transition.module_effects = module_effects;
    transition.receipt = receipt;
    transition.retry = retry;
    return transition;
}

lxp_result lxp_fee_transition_lookup(uint32_t parameter_version,
    lxp_fee_stage stage, lxp_result result, lxp_fee_transition *transition)
{
    bool two_class = lxp_fee_two_class_active(parameter_version);
    if (transition == NULL || parameter_version == 0U ||
        lxp_result_is_fatal(result))
        return LXP_ERR_NON_CANONICAL;
    switch (stage) {
    case LXP_FEE_STAGE_SUBMISSION:
        if (result == LXP_OK)
            *transition = fee_transition(0U, 0U, false, false,
                LXP_FEE_RECEIPT_NONE, LXP_FEE_RETRY_REPLAY);
        else if (result == LXP_ERR_IDEMPOTENT_REPLAY)
            *transition = fee_transition(0U, 0U, false, false,
                LXP_FEE_RECEIPT_NONE, LXP_FEE_RETRY_NONE);
        else
            *transition = fee_transition(0U, 0U, false, false,
                LXP_FEE_RECEIPT_NONE, LXP_FEE_RETRY_RESUBMIT);
        return LXP_OK;
    case LXP_FEE_STAGE_ORDERING:
        if (two_class && result == LXP_ERR_IDEMPOTENT_REPLAY) {
            *transition = fee_transition(0U, 0U, false, false,
                LXP_FEE_RECEIPT_QUEUE_DISPOSITION, LXP_FEE_RETRY_NONE);
            return LXP_OK;
        }
        if (!lxp_fee_ordering_refusal(result)) return LXP_ERR_NON_CANONICAL;
        if (two_class)
            *transition = fee_transition(0U, 0U, false, false,
                LXP_FEE_RECEIPT_QUEUE_DISPOSITION, LXP_FEE_RETRY_RESUBMIT);
        else
            *transition = fee_transition(0U, 1U, false, false,
                LXP_FEE_RECEIPT_REFUSAL, LXP_FEE_RETRY_REPLAY);
        return LXP_OK;
    case LXP_FEE_STAGE_EXECUTION:
        if (result == LXP_ERR_IDEMPOTENT_REPLAY) return LXP_ERR_NON_CANONICAL;
        if (result == LXP_OK)
            *transition = fee_transition(1U, 1U, true, true,
                LXP_FEE_RECEIPT_SUCCESS, LXP_FEE_RETRY_REPLAY);
        else
            *transition = fee_transition(1U, 1U, true, false,
                LXP_FEE_RECEIPT_FAILURE, LXP_FEE_RETRY_REPLAY);
        return LXP_OK;
    default:
        return LXP_ERR_NON_CANONICAL;
    }
}
