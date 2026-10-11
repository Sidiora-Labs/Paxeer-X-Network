#include "layerx/lx_stream.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_kernel.h"

#include <stdio.h>
#include <string.h>

#define CHECK(x) do { \
    if (!(x)) { (void)fprintf(stderr, "line %d\n", __LINE__); return 1; } \
} while (0)

static const char payer_name[] = "agent:did:key:payer:main";
static const char provider_name[] = "agent:did:key:provider:main";
static const char stream_name[] = "agent:did:key:payer:stream:s1";

static struct {
    size_t calls;
    size_t leg_count;
    uint16_t reason;
    size_t authority_count;
    lxp_authorization_kind authority_kind;
    uint8_t authorized_from[32];
    uint16_t origin_module_id;
    uint64_t actor_sequence;
} observed;

static lxp_result apply_capability(lxp_kernel *kernel,
                                   const lxp_transfer_set *set,
                                   lxp_receipt *receipt)
{
    lxp_transfer_set_result result;
    lxp_transfer_context context = set->context;
    lxp_result status;
    (void)kernel;
    ++observed.calls;
    observed.leg_count = set->leg_count;
    observed.reason = set->legs[0].reason;
    observed.authority_count = set->context.source_authority_count;
    observed.origin_module_id = set->context.origin_module_id;
    observed.actor_sequence = set->context.actor_sequence;
    if (set->context.source_authorities != NULL &&
        set->context.source_authority_count != 0U) {
        observed.authority_kind =
            set->context.source_authorities[0].debit_authority_kind;
        (void)memcpy(observed.authorized_from,
                     set->context.source_authorities[0].authorized_from, 32U);
    }
    status = lxp_apply_transfer_set((lxp_transfer_leg *)set->legs,
                                    set->leg_count, &context, &result);
    if (status == LXP_OK)
        (void)memcpy(receipt->transfer_set_root, result.transfer_set_root,
                     32U);
    return status;
}

static lx_account_registry accounts;
static lx_account *payer;
static lx_account *provider;
static lx_account *stream_account;
static lx_asset_record asset;
static lxp_transfer_asset_state asset_state;
static lx_stream_runtime runtime;
static lxp_state_store state;
static lxp_state_journal journal;
static lxp_kernel kernel;
static lxp_module_ctx ctx;
static lxp_effect_buffer effects;
static lxp_arena arena;
static uint8_t arena_bytes[131072];
static uint64_t parameters = 1U;

static int open_account(const char *name, lx_account **account)
{
    uint8_t id[32];
    size_t length = strlen(name);
    CHECK(lx_account_id_from_string((const uint8_t *)name, length, id) ==
          LXP_OK);
    CHECK(lx_account_open(&accounts, (const uint8_t *)name, length, id, 1U,
                          LX_ACCOUNT_OPEN_CREDIT, NULL, account) == LXP_OK);
    return 0;
}

static int fixture_init(void)
{
    (void)memset(&asset, 0, sizeof(asset));
    asset.asset_id[0] = 6U;
    CHECK(lx_asset_transfer_state(&asset, &asset_state) == LXP_OK);
    runtime.assets = &asset_state;
    runtime.asset_count = 1U;
    CHECK(lx_account_registry_init(&accounts) == LXP_OK);
    if (open_account(payer_name, &payer) != 0) return 1;
    if (open_account(provider_name, &provider) != 0) return 1;
    if (open_account(stream_name, &stream_account) != 0) return 1;
    CHECK(payer->kind == LX_ACCOUNT_AGENT_MAIN);
    CHECK(provider->kind == LX_ACCOUNT_AGENT_MAIN);
    CHECK(stream_account->kind == LX_ACCOUNT_AGENT_STREAM);
    CHECK(lxp_ledger_bootstrap_balance(payer, asset.asset_id,
                                       (lxp_u128){ 0U, 100U }, 0U) == LXP_OK);
    CHECK(lxp_state_store_init(&state, 0U) == LXP_OK);
    CHECK(lxp_state_store_bind_accounts(&state, &accounts) == LXP_OK);
    CHECK(lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) ==
          LXP_OK);
    CHECK(lxp_kernel_register_module(&kernel, lx_stream_module_iface()) ==
          LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&kernel, NULL, apply_capability) ==
          LXP_OK);
    return 0;
}

static lxp_result run(uint32_t activity_type, const uint8_t *payload,
                      size_t length, const uint8_t principal[32],
                      uint64_t timestamp, uint64_t sequence,
                      uint64_t gas_limit, lxp_result *module_result)
{
    const lxp_module_registration *registration;
    lxp_activity activity;
    lxp_authority_resolved authority;
    lxp_result status;
    status = lxp_kernel_module_for_activity(&kernel, LX_STREAM_OPEN, 0U,
                                            &registration);
    if (status != LXP_OK) return status;
    (void)memset(&activity, 0, sizeof(activity));
    (void)memset(&authority, 0, sizeof(authority));
    activity.activity_type = activity_type;
    activity.payload.bytes = payload;
    activity.payload.length = length;
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.principal, principal, 32U);
    status = lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes));
    if (status == LXP_OK) status = lxp_effect_buffer_init(&effects);
    if (status == LXP_OK)
        status = lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_STREAM,
                                     timestamp, 0U, sequence, gas_limit,
                                     &arena, true);
    if (status == LXP_OK) status = lxp_module_ctx_bind_effects(&ctx, &effects);
    if (status != LXP_OK) return status;
    *module_result = LXP_OK;
    (void)memset(&observed, 0, sizeof(observed));
    return lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
                               &effects, module_result);
}

static void open_payload_init(lx_stream_open_payload *payload,
                              uint8_t marker, uint64_t start)
{
    (void)memset(payload, 0, sizeof(*payload));
    payload->record.stream_id[0] = marker;
    (void)memcpy(payload->record.stream_account, stream_account->id, 32U);
    (void)memcpy(payload->record.recipient, provider->id, 32U);
    (void)memcpy(payload->record.asset_id, asset.asset_id, 32U);
    payload->record.mode = LX_STREAM_MODE_TIME;
    payload->record.rate = (lxp_u128){ 0U, 10U };
    payload->record.rate_unit = 1000U;
    payload->record.start_timestamp = start;
    payload->record.total_cap = (lxp_u128){ 0U, 500U };
    payload->initial_funding = (lxp_u128){ 0U, 40U };
}

static int open_payload_codec(void)
{
    lx_stream_open_payload payload;
    lx_stream_open_payload decoded;
    uint8_t bytes[LX_STREAM_OPEN_PAYLOAD_MAX];
    size_t length = 0U;
    size_t i;

    /* The payer is never carried on the wire: it is the resolved authority
     * principal, so a caller can not open a stream that debits another
     * account. */
    open_payload_init(&payload, 4U, 1000U);
    payload.record.end_timestamp = 9000U;
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(length == (size_t)LX_STREAM_OPEN_PAYLOAD_FIXED);
    CHECK(bytes[0] == 0U && bytes[1] == (uint8_t)LX_STREAM_PAYLOAD_VERSION);
    CHECK(lxp_ct_is_zero(payload.record.payer, 32U));
    CHECK(lx_stream_open_decode(bytes, length, &decoded) == LXP_OK);
    payload.record.last_accrual_timestamp = payload.record.start_timestamp;
    CHECK(memcmp(&payload, &decoded, sizeof(payload)) == 0);

    {
        size_t written = 0U;
        CHECK(lx_stream_open_encode(&payload, bytes, length - 1U,
                                    &written) == LXP_ERR_LENGTH_LIMIT);
        CHECK(written == 0U);
    }
    CHECK(lx_stream_open_decode(bytes, length - 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_decode(bytes, length + 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_decode(NULL, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_decode(bytes, length, NULL) ==
          LXP_ERR_NON_CANONICAL);
    bytes[1] = (uint8_t)(LX_STREAM_PAYLOAD_VERSION + 1U);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_VERSION_UNSUPPORTED);
    bytes[1] = (uint8_t)LX_STREAM_PAYLOAD_VERSION;
    bytes[0] = 1U;
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_VERSION_UNSUPPORTED);
    bytes[0] = 0U;
    bytes[130] = 3U;
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    bytes[130] = 0U;
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    bytes[130] = (uint8_t)LX_STREAM_MODE_TIME;
    bytes[LX_STREAM_OPEN_PAYLOAD_FIXED - 1] =
        (uint8_t)(LX_STREAM_MAX_METER_AUTHORITIES + 1U);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);

    /* A time stream may carry no meter authority and a metered stream may
     * not go without one. */
    open_payload_init(&payload, 4U, 1000U);
    payload.record.meter_authorities[0][0] = 1U;
    payload.record.meter_authority_count = 1U;
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(length == (size_t)LX_STREAM_OPEN_PAYLOAD_FIXED + 32U);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);

    open_payload_init(&payload, 4U, 1000U);
    payload.record.mode = LX_STREAM_MODE_METERED;
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);

    open_payload_init(&payload, 4U, 1000U);
    payload.record.mode = LX_STREAM_MODE_METERED;
    for (i = 0U; i < (size_t)LX_STREAM_MAX_METER_AUTHORITIES; ++i)
        payload.record.meter_authorities[i][0] = (uint8_t)(i + 1U);
    payload.record.meter_authority_count =
        (size_t)LX_STREAM_MAX_METER_AUTHORITIES;
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(length == (size_t)LX_STREAM_OPEN_PAYLOAD_MAX);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) == LXP_OK);
    payload.record.last_accrual_timestamp = payload.record.start_timestamp;
    CHECK(memcmp(&payload, &decoded, sizeof(payload)) == 0);
    /* Authorities are strictly ascending and never zero. */
    (void)memcpy(bytes + LX_STREAM_OPEN_PAYLOAD_FIXED + 32U,
                 bytes + LX_STREAM_OPEN_PAYLOAD_FIXED, 32U);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    (void)memset(bytes + LX_STREAM_OPEN_PAYLOAD_FIXED, 0, 32U);
    CHECK(lx_stream_open_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);

    payload.record.meter_authority_count =
        (size_t)LX_STREAM_MAX_METER_AUTHORITIES + 1U;
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_encode(NULL, bytes, sizeof(bytes), &length) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), NULL) ==
          LXP_ERR_NON_CANONICAL);
    return 0;
}

static int amount_payload_codec(void)
{
    lx_stream_amount_payload payload;
    lx_stream_amount_payload decoded;
    uint8_t bytes[LX_STREAM_TOP_UP_PAYLOAD_BYTES];
    size_t length = 0U;

    (void)memset(&payload, 0, sizeof(payload));
    payload.stream_id[0] = 4U;
    payload.amount = (lxp_u128){ 0U, 10U };
    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(length == (size_t)LX_STREAM_TOP_UP_PAYLOAD_BYTES);
    CHECK(lx_stream_amount_decode(bytes, length, &decoded) == LXP_OK);
    CHECK(memcmp(&payload, &decoded, sizeof(payload)) == 0);
    CHECK(lx_stream_amount_encode(&payload, bytes, length - 1U, &length) ==
          LXP_ERR_LENGTH_LIMIT);

    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(lx_stream_amount_decode(bytes, length - 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_amount_decode(bytes, length + 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_amount_decode(NULL, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_amount_decode(bytes, length, NULL) ==
          LXP_ERR_NON_CANONICAL);
    bytes[1] = 9U;
    CHECK(lx_stream_amount_decode(bytes, length, &decoded) ==
          LXP_ERR_VERSION_UNSUPPORTED);
    bytes[1] = (uint8_t)LX_STREAM_PAYLOAD_VERSION;

    (void)memset(bytes + 34U, 0, 16U);
    CHECK(lx_stream_amount_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    (void)memset(bytes + 2U, 0, 32U);
    CHECK(lx_stream_amount_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    return 0;
}

static int rate_change_payload_codec(void)
{
    static const uint8_t rate_bytes[16] = {
        1U, 2U, 3U, 4U, 5U, 6U, 7U, 8U,
        17U, 18U, 19U, 20U, 21U, 22U, 23U, 24U
    };
    lx_stream_rate_change_payload decoded;
    uint8_t bytes[LX_STREAM_RATE_CHANGE_PAYLOAD_BYTES + 1U] = { 0U };
    uint8_t canonical[LX_STREAM_RATE_CHANGE_PAYLOAD_BYTES];
    size_t i;

    CHECK(LX_STREAM_RATE_CHANGE == 0x00040008U);
    CHECK(LX_STREAM_RATE_CHANGE_PAYLOAD_BYTES == 82U);
    bytes[1] = 1U;
    for (i = 0U; i < 32U; ++i) {
        bytes[2U + i] = (uint8_t)(i + 1U);
        bytes[50U + i] = (uint8_t)(128U + i);
    }
    (void)memcpy(bytes + 34U, rate_bytes, sizeof(rate_bytes));
    (void)memcpy(canonical, bytes, sizeof(canonical));
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_OK);
    CHECK(memcmp(decoded.stream_id, canonical + 2U, 32U) == 0);
    CHECK(decoded.new_rate.hi == UINT64_C(0x0102030405060708));
    CHECK(decoded.new_rate.lo == UINT64_C(0x1112131415161718));
    CHECK(memcmp(decoded.idempotency_key, canonical + 50U, 32U) == 0);

    for (i = 0U; i <= sizeof(bytes); ++i) {
        if (i == sizeof(canonical)) continue;
        CHECK(lx_stream_rate_change_decode(bytes, i, &decoded) ==
              LXP_ERR_NON_CANONICAL);
    }
    CHECK(lx_stream_rate_change_decode(bytes, SIZE_MAX, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_rate_change_decode(NULL, sizeof(canonical), &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_rate_change_decode(NULL, 0U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), NULL) ==
          LXP_ERR_NON_CANONICAL);

    for (i = 1U; i <= UINT8_MAX; ++i) {
        bytes[0] = (uint8_t)i;
        CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical),
                                           &decoded) ==
              LXP_ERR_VERSION_UNSUPPORTED);
    }
    bytes[0] = 0U;
    for (i = 0U; i <= UINT8_MAX; ++i) {
        if (i == 1U) continue;
        bytes[1] = (uint8_t)i;
        CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical),
                                           &decoded) ==
              LXP_ERR_VERSION_UNSUPPORTED);
    }
    (void)memcpy(bytes, canonical, sizeof(canonical));
    (void)memset(bytes + 2U, 0, 32U);
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_ERR_NON_CANONICAL);
    (void)memcpy(bytes, canonical, sizeof(canonical));
    (void)memset(bytes + 34U, 0, 16U);
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_ERR_NON_CANONICAL);
    bytes[41] = 1U;
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_OK);
    CHECK(decoded.new_rate.hi == 1U && decoded.new_rate.lo == 0U);
    bytes[41] = 0U;
    bytes[49] = 1U;
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_OK);
    CHECK(decoded.new_rate.hi == 0U && decoded.new_rate.lo == 1U);
    (void)memset(bytes + 34U, UINT8_MAX, 16U);
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_OK);
    CHECK(decoded.new_rate.hi == UINT64_MAX &&
          decoded.new_rate.lo == UINT64_MAX);
    (void)memcpy(bytes, canonical, sizeof(canonical));
    (void)memset(bytes + 50U, 0, 32U);
    CHECK(lx_stream_rate_change_decode(bytes, sizeof(canonical), &decoded) ==
          LXP_ERR_NON_CANONICAL);
    return 0;
}

static int dispatch_refusals(void)
{
    lx_stream_open_payload payload;
    uint8_t bytes[LX_STREAM_OPEN_PAYLOAD_MAX];
    size_t length = 0U;
    lxp_result module_result = LXP_OK;

    open_payload_init(&payload, 4U, 1000U);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);

    /* Until the host publishes the assets this module may move, the module
     * refuses rather than assuming an asset is live. */
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_ASSET_MISMATCH);
    /* The binding can never be taken away once it is installed. */
    CHECK(lxp_kernel_bind_module_runtime(&kernel, LXP_MODULE_STREAM, NULL) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lxp_kernel_bind_module_runtime(&kernel, LXP_MODULE_STREAM,
                                         &runtime) == LXP_OK);

    /* An ordinal outside the advertised range never decodes. */
    CHECK(run(0x00040009U, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_ACTIVITY);
    CHECK(run(0x00040008U, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_NON_CANONICAL);
    /* A foreign module id carrying a stream ordinal is refused in validate. */
    CHECK(run(0x00050001U, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_ACTIVITY);
    CHECK(observed.calls == 0U);
    CHECK(payer->balance.lo == 100U);

    /* An empty payload is refused before any typed decode runs. */
    CHECK(run(LX_STREAM_OPEN, bytes, 0U, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_ACTIVITY);

    /* Gas is charged from the payload length, so a short limit refuses. */
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_GAS_EXHAUSTED);
    CHECK(observed.calls == 0U && payer->balance.lo == 100U);

    /* A zero principal can not stand in for an account. */
    {
        uint8_t zero[32] = { 0U };
        CHECK(run(LX_STREAM_OPEN, bytes, length, zero, 1000U, 1U, 100000U,
                  &module_result) == LXP_OK);
        CHECK(module_result == LXP_ERR_UNAUTHORIZED_DEBIT);
    }

    /* An asset the host never published is refused; the module never
     * guesses at asset state it can not read. */
    runtime.asset_count = 0U;
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_ASSET_MISMATCH);
    runtime.asset_count = 1U;
    asset_state.asset_id[0] = 7U;
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_ASSET_MISMATCH);
    asset_state.asset_id[0] = 6U;
    asset_state.paused = true;
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_ASSET_PAUSED);
    asset_state.paused = false;
    CHECK(observed.calls == 0U && payer->balance.lo == 100U);

    /* A stream account that is not stream custody is refused. */
    open_payload_init(&payload, 4U, 1000U);
    (void)memcpy(payload.record.stream_account, provider->id, 32U);
    (void)memcpy(payload.record.recipient, stream_account->id, 32U);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_NON_CANONICAL);

    /* The payer may not also be the custody account. */
    open_payload_init(&payload, 4U, 1000U);
    (void)memcpy(payload.record.stream_account, payer->id, 32U);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_NON_CANONICAL);

    /* An account the ledger does not know is refused. */
    open_payload_init(&payload, 4U, 1000U);
    payload.record.stream_account[31] =
        (uint8_t)(payload.record.stream_account[31] ^ 1U);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE);

    /* Funding beyond the payer balance leaves no partial state behind. */
    open_payload_init(&payload, 4U, 1000U);
    payload.initial_funding = (lxp_u128){ 0U, 101U };
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_INSUFFICIENT_BALANCE);
    CHECK(observed.calls == 1U);
    CHECK(payer->balance.lo == 100U);
    CHECK(lxp_u128_is_zero(stream_account->balance));
    CHECK(payer->next_sequence == 0U);
    CHECK(ctx.staged_count == 0U);
    CHECK(effects.count == 0U);
    return 0;
}

static int dispatch_open(void)
{
    lx_stream_open_payload payload;
    lx_stream_record stored;
    uint8_t bytes[LX_STREAM_OPEN_PAYLOAD_MAX];
    uint8_t key[LX_STREAM_KEY_BYTES];
    uint8_t funding[16];
    size_t length = 0U;
    lxp_result module_result = LXP_OK;

    open_payload_init(&payload, 4U, 1000U);
    CHECK(lx_stream_open_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1000U, 1U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_OK);
    CHECK(observed.calls == 1U && observed.leg_count == 1U);
    CHECK(observed.reason == (uint16_t)LXP_REASON_STREAM_FUND);
    CHECK(observed.authority_count == 1U);
    CHECK(observed.authority_kind == LXP_AUTH_OWNER);
    CHECK(memcmp(observed.authorized_from, payer->id, 32U) == 0);
    CHECK(observed.origin_module_id == (uint16_t)LXP_MODULE_STREAM);
    CHECK(observed.actor_sequence == 0U);
    CHECK(payer->balance.lo == 60U && stream_account->balance.lo == 40U);
    CHECK(payer->next_sequence == 1U);
    CHECK(stream_account->has_asset);
    CHECK(memcmp(stream_account->asset_id, asset.asset_id, 32U) == 0);
    CHECK(effects.count == 1U);
    CHECK(effects.effects[0].kind == LXP_EFFECT_EVENT);
    CHECK(effects.effects[0].module_id == (uint16_t)LXP_MODULE_STREAM);
    CHECK(effects.effects[0].event_type == 1U);
    CHECK(effects.effects[0].body_length == 177U);
    CHECK(memcmp(effects.effects[0].body, payload.record.stream_id, 32U) == 0);
    CHECK(memcmp(effects.effects[0].body + 32U, payer->id, 32U) == 0);
    CHECK(memcmp(effects.effects[0].body + 64U, stream_account->id, 32U) == 0);
    CHECK(memcmp(effects.effects[0].body + 96U, provider->id, 32U) == 0);
    CHECK(memcmp(effects.effects[0].body + 128U, asset.asset_id, 32U) == 0);
    CHECK(effects.effects[0].body[160] == (uint8_t)LX_STREAM_MODE_TIME);
    CHECK(lxp_u128_to_be(payload.initial_funding, funding) == LXP_OK);
    CHECK(memcmp(effects.effects[0].body + 161U, funding, 16U) == 0);

    /* The record is staged under the canonical key and survives commit. */
    CHECK(lx_stream_state_key(payload.record.stream_id, key) == LXP_OK);
    CHECK(memcmp(key, "stream:", 7U) == 0);
    CHECK(memcmp(key + 7U, payload.record.stream_id, 32U) == 0);
    CHECK(ctx.staged_count == 1U);
    CHECK(ctx.staged[0].key_length == (uint16_t)LX_STREAM_KEY_BYTES);
    CHECK(memcmp(ctx.staged[0].key, key, sizeof(key)) == 0);
    CHECK(ctx.staged[0].value_length == (uint32_t)LX_STREAM_RECORD_BYTES);
    CHECK(!ctx.staged[0].deleted);
    CHECK(lx_stream_load(&ctx, payload.record.stream_id, &stored) == LXP_OK);
    CHECK(memcmp(stored.payer, payer->id, 32U) == 0);
    CHECK(stored.last_accrual_timestamp == 1000U);
    CHECK(stored.start_timestamp == 1000U);
    CHECK(lxp_u128_is_zero(stored.accrued_total));
    CHECK(lxp_u128_is_zero(stored.settled_total));
    CHECK(!stored.underfunded && !stored.paused && !stored.closed);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    CHECK(kernel.module_kv_count == 1U);

    /* The same stream id can never be opened twice. */
    CHECK(run(LX_STREAM_OPEN, bytes, length, payer->id, 1100U, 2U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_SEQUENCE_REUSED);
    CHECK(observed.calls == 0U);
    CHECK(payer->balance.lo == 60U && stream_account->balance.lo == 40U);
    return 0;
}

static int rate_change_unchanged(const uint8_t stream_id[32],
                                 const uint8_t before[LX_STREAM_RECORD_BYTES],
                                 const uint8_t root_before[32])
{
    lx_stream_record stored;
    uint8_t after[LX_STREAM_RECORD_BYTES];
    uint8_t root_after[32];

    CHECK(payer->balance.hi == 0U && payer->balance.lo == 60U);
    CHECK(provider->balance.hi == 0U && provider->balance.lo == 0U);
    CHECK(stream_account->balance.hi == 0U &&
          stream_account->balance.lo == 40U);
    CHECK(payer->next_sequence == 1U);
    CHECK(provider->next_sequence == 0U);
    CHECK(stream_account->next_sequence == 0U);
    CHECK(observed.calls == 0U);
    CHECK(ctx.staged_count == 0U);
    CHECK(ctx.staged_account_count == 0U);
    CHECK(ctx.transfer_snapshot_count == 0U);
    CHECK(ctx.staged_blob_count == 0U);
    CHECK(!ctx.transfer_applied);
    CHECK(effects.count == 0U);
    CHECK(ctx.next_effect_ordinal == 0U);
    CHECK(kernel.module_kv_count == 1U);
    CHECK(lx_stream_load(&ctx, stream_id, &stored) == LXP_OK);
    CHECK(lx_stream_record_encode(&stored, after) == LXP_OK);
    CHECK(memcmp(before, after, sizeof(after)) == 0);
    CHECK(lx_stream_module_iface()->state_root(&ctx, root_after) == LXP_OK);
    CHECK(memcmp(root_before, root_after, sizeof(root_after)) == 0);
    return 0;
}

static int dispatch_rate_change(void)
{
    const lxp_module_iface *iface = lx_stream_module_iface();
    const lxp_module_registration *registration;
    lx_stream_record stored;
    lxp_activity activity;
    lxp_authority_resolved authority;
    uint8_t bytes[LX_STREAM_RATE_CHANGE_PAYLOAD_BYTES] = { 0U };
    uint8_t before[LX_STREAM_RECORD_BYTES];
    uint8_t root_before[32];
    void *decoded = NULL;
    lxp_result module_result = LXP_OK;

    bytes[1] = 1U;
    bytes[2] = 4U;
    CHECK(lxp_u128_to_be((lxp_u128){ 0U, 20U }, bytes + 34U) == LXP_OK);
    bytes[81] = 1U;
    CHECK(lx_stream_load(&ctx, bytes + 2U, &stored) == LXP_OK);
    CHECK(lx_stream_record_encode(&stored, before) == LXP_OK);
    CHECK(iface->state_root(&ctx, root_before) == LXP_OK);
    CHECK(lxp_kernel_module_for_activity(&kernel, LX_STREAM_RATE_CHANGE, 0U,
                                         &registration) == LXP_OK);
    CHECK(registration->iface == iface);

    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = LX_STREAM_RATE_CHANGE;
    activity.payload.bytes = bytes;
    activity.payload.length = sizeof(bytes);
    (void)memset(&authority, 0, sizeof(authority));
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.principal, payer->id, 32U);
    CHECK(lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) == LXP_OK);
    CHECK(lxp_effect_buffer_init(&effects) == LXP_OK);
    CHECK(lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_STREAM, 1500U, 0U,
                              2U, 83U, &arena, true) == LXP_OK);
    CHECK(lxp_module_ctx_bind_effects(&ctx, &effects) == LXP_OK);
    (void)memset(&observed, 0, sizeof(observed));
    CHECK(iface->decode(&ctx, 8U, bytes, sizeof(bytes), &decoded) == LXP_OK);
    CHECK(decoded != NULL);
    CHECK(ctx.gas_used == 0U);
    CHECK(lxp_module_ctx_set_mutable(&ctx, false) == LXP_OK);
    CHECK(iface->validate(&ctx, &activity, &authority, decoded) == LXP_OK);
    CHECK(ctx.gas_used == sizeof(bytes) + 1U && ctx.gas_used == 83U);
    (void)memcpy(authority.principal, provider->id, 32U);
    CHECK(iface->validate(&ctx, &activity, &authority, decoded) ==
          LXP_ERR_UNAUTHORIZED_DEBIT);
    CHECK(ctx.gas_used == 83U);
    (void)memcpy(authority.principal, payer->id, 32U);
    CHECK(lxp_module_ctx_set_mutable(&ctx, true) == LXP_OK);
    CHECK(iface->execute(&ctx, &activity, &authority, decoded, &effects) ==
          LXP_ERR_UNKNOWN_ACTIVITY);
    CHECK(ctx.gas_used == 83U);
    if (rate_change_unchanged(bytes + 2U, before, root_before) != 0) return 1;

    CHECK(run(LX_STREAM_RATE_CHANGE, bytes, sizeof(bytes), payer->id, 1500U,
              2U, 83U, &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_ACTIVITY);
    CHECK(ctx.gas_used == 83U);
    if (rate_change_unchanged(bytes + 2U, before, root_before) != 0) return 1;
    CHECK(run(LX_STREAM_RATE_CHANGE, bytes, sizeof(bytes), provider->id,
              1500U, 2U, 83U, &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNAUTHORIZED_DEBIT);
    CHECK(ctx.gas_used == 0U);
    if (rate_change_unchanged(bytes + 2U, before, root_before) != 0) return 1;
    CHECK(run(LX_STREAM_RATE_CHANGE, bytes, sizeof(bytes), payer->id, 1500U,
              2U, 82U, &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_GAS_EXHAUSTED);
    CHECK(ctx.gas_used == 0U);
    if (rate_change_unchanged(bytes + 2U, before, root_before) != 0) return 1;
    return 0;
}

static int dispatch_top_up(void)
{
    lx_stream_amount_payload payload;
    lx_stream_record stored;
    uint8_t bytes[LX_STREAM_TOP_UP_PAYLOAD_BYTES];
    size_t length = 0U;
    lxp_result module_result = LXP_OK;

    (void)memset(&payload, 0, sizeof(payload));
    payload.stream_id[0] = 4U;
    payload.amount = (lxp_u128){ 0U, 15U };
    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);

    /* Only the payer of record may add funds. */
    CHECK(run(LX_STREAM_TOP_UP, bytes, length, provider->id, 1500U, 3U,
              100000U, &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNAUTHORIZED_DEBIT);
    CHECK(observed.calls == 0U);

    /* Time never runs backwards inside a stream. */
    CHECK(run(LX_STREAM_TOP_UP, bytes, length, payer->id, 500U, 3U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_NON_MONOTONIC_TIME);
    CHECK(observed.calls == 0U);

    /* An unknown stream has no record to fund. */
    payload.stream_id[0] = 9U;
    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_TOP_UP, bytes, length, payer->id, 1500U, 3U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_UNKNOWN_FIELD);

    payload.stream_id[0] = 4U;
    CHECK(lx_stream_amount_encode(&payload, bytes, sizeof(bytes), &length) ==
          LXP_OK);
    CHECK(run(LX_STREAM_TOP_UP, bytes, length, payer->id, 1500U, 3U, 100000U,
              &module_result) == LXP_OK);
    CHECK(module_result == LXP_OK);
    CHECK(observed.calls == 1U && observed.leg_count == 1U);
    CHECK(observed.reason == (uint16_t)LXP_REASON_STREAM_FUND);
    CHECK(observed.authority_kind == LXP_AUTH_OWNER);
    CHECK(memcmp(observed.authorized_from, payer->id, 32U) == 0);
    CHECK(observed.actor_sequence == 1U);
    CHECK(payer->balance.lo == 45U && stream_account->balance.lo == 55U);
    CHECK(payer->next_sequence == 2U);
    CHECK(effects.count == 1U);
    CHECK(effects.effects[0].event_type == 2U);
    CHECK(effects.effects[0].body_length == 49U);
    CHECK(memcmp(effects.effects[0].body, payload.stream_id, 32U) == 0);
    CHECK(effects.effects[0].body[48] == 0U);
    CHECK(lx_stream_load(&ctx, payload.stream_id, &stored) == LXP_OK);
    CHECK(!stored.underfunded && stored.last_accrual_timestamp == 1000U);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    return 0;
}

static lxp_result count_streams(const lx_stream_record *record, void *user)
{
    size_t *count = (size_t *)user;
    if (record == NULL || lxp_ct_is_zero(record->stream_id, 32U))
        return LXP_ERR_NON_CANONICAL;
    ++(*count);
    return LXP_OK;
}

static int committed_state(void)
{
    lx_stream_record stored;
    lxp_result module_result = LXP_OK;
    size_t count = 0U;
    uint8_t id[32] = { 0U };
    uint8_t bytes[LX_STREAM_TOP_UP_PAYLOAD_BYTES] = { 0U };
    id[0] = 4U;
    /* A fresh context reads the committed record, not staged memory. */
    CHECK(run(LX_STREAM_TOP_UP, bytes, sizeof(bytes), payer->id, 2000U, 4U,
              100000U, &module_result) == LXP_OK);
    CHECK(module_result == LXP_ERR_VERSION_UNSUPPORTED);
    CHECK(ctx.staged_count == 0U);
    CHECK(lx_stream_load(&ctx, id, &stored) == LXP_OK);
    CHECK(memcmp(stored.stream_id, id, 32U) == 0);
    CHECK(memcmp(stored.payer, payer->id, 32U) == 0);
    CHECK(memcmp(stored.stream_account, stream_account->id, 32U) == 0);
    CHECK(memcmp(stored.recipient, provider->id, 32U) == 0);
    CHECK(stored.mode == LX_STREAM_MODE_TIME);
    CHECK(stored.rate.lo == 10U && stored.rate_unit == 1000U);
    CHECK(stored.total_cap.lo == 500U);
    CHECK(lx_stream_iter(&ctx, count_streams, &count) == LXP_OK);
    CHECK(count == 1U);
    id[0] = 9U;
    CHECK(lx_stream_load(&ctx, id, &stored) == LXP_ERR_UNKNOWN_FIELD);
    return 0;
}

int main(void)
{
    if (fixture_init() != 0) return 1;
    if (open_payload_codec() != 0) return 1;
    if (amount_payload_codec() != 0) return 1;
    if (rate_change_payload_codec() != 0) return 1;
    if (dispatch_refusals() != 0) return 1;
    if (dispatch_open() != 0) return 1;
    if (dispatch_rate_change() != 0) return 1;
    if (dispatch_top_up() != 0) return 1;
    if (committed_state() != 0) return 1;
    if (lxp_state_store_destroy(&state) != LXP_OK) return 1;
    return 0;
}
