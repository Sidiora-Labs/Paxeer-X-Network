#include "layerx/lxp_fuzz.h"

#include "layerx/lxp_crypto.h"
#include "layerx/lxp_transfer.h"

#include <string.h>

typedef struct transfer_run {
    lx_account accounts[4];
    lxp_transfer_set_result result;
    lxp_result operation;
} transfer_run;

static uint8_t input_byte(const uint8_t *data, size_t size, size_t offset)
{
    return offset < size ? data[offset] : 0U;
}

static void initialize_accounts(lx_account accounts[4],
                                const uint8_t asset_a[32],
                                const uint8_t asset_b[32])
{
    size_t i;
    (void)memset(accounts, 0, sizeof(lx_account) * 4U);
    for (i = 0U; i < 4U; ++i) {
        accounts[i].id[0] = (uint8_t)(i + 1U);
        accounts[i].kind = (lx_account_kind)(LX_ACCOUNT_SYSTEM_LIQUIDITY + i);
        accounts[i].balance = (lxp_u128){ 0U, UINT64_C(1000000000) };
        (void)memcpy(accounts[i].asset_id, i < 2U ? asset_a : asset_b, 32U);
        accounts[i].has_asset = true;
        accounts[i].next_sequence = 7U;
    }
}

static lxp_u128 asset_total(const lx_account accounts[4],
                            const uint8_t asset[32])
{
    lxp_u128 total = { 0U, 0U };
    size_t i;
    for (i = 0U; i < 4U; ++i) {
        if (accounts[i].has_asset &&
            memcmp(accounts[i].asset_id, asset, 32U) == 0) {
            lxp_u128 next;
            if (lxp_u128_add(total, accounts[i].balance, &next) != LXP_OK)
                return (lxp_u128){ UINT64_MAX, UINT64_MAX };
            total = next;
        }
    }
    return total;
}

static lxp_result execute_case(const uint8_t *data, size_t size,
                               transfer_run *run)
{
    uint8_t asset_a[32] = { 0x41U };
    uint8_t asset_b[32] = { 0x42U };
    uint8_t unknown_asset[32] = { 0x7fU };
    lxp_transfer_asset_state assets[2];
    lxp_transfer_leg legs[LXP_MAX_TRANSFER_SET_LEGS];
    lxp_transfer_context context;
    lx_account before[4];
    lxp_u128 before_a;
    lxp_u128 before_b;
    size_t leg_count;
    size_t i;
    if (run == NULL || (data == NULL && size != 0U))
        return LXP_ERR_NON_CANONICAL;
    (void)memset(run, 0, sizeof(*run));
    initialize_accounts(run->accounts, asset_a, asset_b);
    (void)memcpy(before, run->accounts, sizeof(before));
    before_a = asset_total(before, asset_a);
    before_b = asset_total(before, asset_b);
    (void)memset(assets, 0, sizeof(assets));
    (void)memcpy(assets[0].asset_id, asset_a, 32U);
    (void)memcpy(assets[1].asset_id, asset_b, 32U);
    assets[0].registered = true;
    assets[1].registered = true;
    assets[1].paused = (input_byte(data, size, 2U) & 0x80U) != 0U;
    (void)memset(&context, 0, sizeof(context));
    context.assets = assets;
    context.asset_count = 2U;
    context.protocol_system_capability = true;
    context.actor_sequence = 7U;
    context.batch_timestamp = 100U;
    context.expires_at = 200U;
    context.debit_authority_kind = LXP_AUTH_PROTOCOL_MODULE;
    leg_count = size < 2U ? size :
        ((((size_t)data[0] << 8U) | data[1]) %
         (LXP_MAX_TRANSFER_SET_LEGS + 3U));
    (void)memset(legs, 0, sizeof(legs));
    for (i = 0U; i < leg_count && i < LXP_MAX_TRANSFER_SET_LEGS; ++i) {
        size_t offset = 3U + i * 9U;
        uint8_t from = (uint8_t)(input_byte(data, size, offset) % 5U);
        uint8_t to = (uint8_t)(input_byte(data, size, offset + 1U) % 5U);
        uint8_t asset = (uint8_t)(input_byte(data, size, offset + 2U) % 3U);
        uint64_t amount = ((uint64_t)input_byte(data, size, offset + 3U) << 24U) |
                          ((uint64_t)input_byte(data, size, offset + 4U) << 16U) |
                          ((uint64_t)input_byte(data, size, offset + 5U) << 8U) |
                          input_byte(data, size, offset + 6U);
        legs[i].from = from < 4U ? &run->accounts[from] : NULL;
        legs[i].to = to < 4U ? &run->accounts[to] : NULL;
        (void)memcpy(legs[i].asset_id,
                     asset == 0U ? asset_a : asset == 1U ? asset_b :
                     unknown_asset, 32U);
        legs[i].amount = (lxp_u128){ 0U, amount };
        legs[i].reason = (uint16_t)input_byte(data, size, offset + 7U);
        legs[i].supply_mode =
            (uint8_t)(input_byte(data, size, offset + 8U) % 4U);
    }
    run->operation = lxp_apply_transfer_set(legs, leg_count, &context,
                                             &run->result);
    if (run->operation != LXP_OK) {
        return memcmp(before, run->accounts, sizeof(before)) == 0 ? LXP_OK :
               LXP_FATAL_REPLAY_DIVERGENCE;
    }
    if (!run->result.receipt_emitted || run->result.leg_count == 0U ||
        run->result.leg_count > leg_count ||
        lxp_ct_is_zero(run->result.transfer_set_root, 32U) ||
        lxp_u128_cmp(asset_total(run->accounts, asset_a), before_a) != 0 ||
        lxp_u128_cmp(asset_total(run->accounts, asset_b), before_b) != 0)
        return LXP_FATAL_REPLAY_DIVERGENCE;
    return LXP_OK;
}

lxp_result lxp_fuzz_transfer_set(const uint8_t *data, size_t size)
{
    transfer_run first;
    transfer_run second;
    lxp_result status;
    status = execute_case(data, size, &first);
    if (status == LXP_OK) status = execute_case(data, size, &second);
    if (status != LXP_OK) return status;
    if (first.operation != second.operation ||
        memcmp(first.accounts, second.accounts, sizeof(first.accounts)) != 0 ||
        first.result.leg_count != second.result.leg_count ||
        first.result.failed_leg != second.result.failed_leg ||
        first.result.failure != second.result.failure ||
        first.result.receipt_emitted != second.result.receipt_emitted ||
        memcmp(first.result.transfer_set_root, second.result.transfer_set_root,
               32U) != 0)
        return LXP_FATAL_REPLAY_DIVERGENCE;
    return LXP_OK;
}
