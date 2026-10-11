#include "layerx/lxp_authority.h"
#include "layerx/lxp_governance.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_module_ctx.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define FEE_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "fee grant check failed at line %d\n", __LINE__); return 1; \
} } while (0)

static int fee_grants(void)
{
    uint8_t storage[2048], encoded_v1[1024], encoded_v2[1024], first_id[32], second_id[32];
    uint8_t owner[32] = {1U}, key[32] = {2U};
    lxp_authority_grant grant, decoded, narrower;
    lxp_authority_fee_budget original;
    lxp_arena arena;
    lxp_byte_span encoded;
    size_t legacy_length, length;
    FEE_CHECK(lxp_session_key_bind(&grant, owner, key, 2U, 1U, 9U, 10U, 100U, 1U) == LXP_OK);
    grant.kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    grant.scope.asset_id[0] = 3U;
    grant.scope.purpose_hash[0] = 4U;
    grant.scope.maximum_per_activity.lo = 2U;
    grant.scope.maximum_total.lo = 10U;
    FEE_CHECK(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
    FEE_CHECK(lxp_grant_encode(&grant, &arena, &encoded) == LXP_OK);
    legacy_length = encoded.length;
    FEE_CHECK(legacy_length <= sizeof(encoded_v1) && encoded.bytes[4] == 1U);
    (void)memcpy(encoded_v1, encoded.bytes, legacy_length);
    FEE_CHECK(lxp_grant_id_compute(&grant, first_id) == LXP_OK);
    FEE_CHECK(lxp_grant_decode(encoded_v1, legacy_length, &decoded) == LXP_OK && !decoded.fee_budget.present);
    FEE_CHECK(lxp_grant_id_compute(&decoded, second_id) == LXP_OK && memcmp(first_id, second_id, 32U) == 0);
    grant.fee_budget.present = true;
    grant.fee_budget.asset_id[0] = 5U;
    grant.fee_budget.maximum_per_activity.lo = 4U;
    grant.fee_budget.maximum_total.lo = 12U;
    grant.fee_budget.period_length = 10U;
    grant.fee_budget.maximum_per_period.lo = 8U;
    grant.fee_budget.period_start = 10U;
    FEE_CHECK(lxp_arena_reset(&arena, 0U) == LXP_OK);
    FEE_CHECK(lxp_grant_encode(&grant, &arena, &encoded) == LXP_OK);
    length = encoded.length;
    FEE_CHECK(length == legacy_length + 132U && length <= sizeof(encoded_v2));
    (void)memcpy(encoded_v2, encoded.bytes, length);
    FEE_CHECK(encoded_v2[4] == 2U && memcmp(encoded_v1, encoded_v2, 4U) == 0 &&
        memcmp(encoded_v1 + 5U, encoded_v2 + 5U, legacy_length - 5U) == 0);
    FEE_CHECK(lxp_grant_decode(encoded_v2, length, &decoded) == LXP_OK && decoded.fee_budget.present);
    FEE_CHECK(lxp_grant_id_compute(&decoded, second_id) == LXP_OK && memcmp(first_id, second_id, 32U) != 0);
    for (size_t truncated = 0U; truncated < length; ++truncated)
        FEE_CHECK(lxp_grant_decode(encoded_v2, truncated, &decoded) != LXP_OK);
    encoded_v2[4] = 1U;
    FEE_CHECK(lxp_grant_decode(encoded_v2, length, &decoded) != LXP_OK);
    encoded_v2[4] = 3U;
    FEE_CHECK(lxp_grant_decode(encoded_v2, length, &decoded) == LXP_ERR_VERSION_UNSUPPORTED);
    encoded_v2[4] = 2U;
    narrower = grant;
    narrower.grantor_revocation_sequence++;
    narrower.fee_budget.maximum_total.lo++;
    FEE_CHECK(lxp_authority_amend(&grant, &narrower) == LXP_ERR_AUTH_SCOPE);
    original = grant.fee_budget;
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 5U}, 10U) == LXP_ERR_GRANT_EXHAUSTED);
    FEE_CHECK(memcmp(&grant.fee_budget, &original, sizeof(original)) == 0);
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 4U}, 10U) == LXP_OK);
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 4U}, 19U) == LXP_OK);
    original = grant.fee_budget;
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 1U}, 19U) == LXP_ERR_GRANT_EXHAUSTED);
    FEE_CHECK(memcmp(&grant.fee_budget, &original, sizeof(original)) == 0);
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 4U}, 20U) == LXP_OK);
    FEE_CHECK(grant.fee_budget.spent_total.lo == 12U && grant.fee_budget.spent_this_period.lo == 4U &&
        grant.fee_budget.period_start == 20U);
    original = grant.fee_budget;
    FEE_CHECK(lxp_authority_fee_charge(&grant.fee_budget, (lxp_u128){0U, 1U}, 21U) == LXP_ERR_GRANT_EXHAUSTED);
    FEE_CHECK(memcmp(&grant.fee_budget, &original, sizeof(original)) == 0);
    return 0;
}

static int authentication_sessions(void)
{
    uint8_t storage[2048], bytes[1024], owner[32] = {1U}, key[32] = {2U};
    lxp_authority_grant grant, decoded;
    lxp_arena arena;
    lxp_byte_span encoded;
    FEE_CHECK(lxp_authentication_key_bind(&grant, owner, key, 10U, 100U, 1U) == LXP_OK);
    FEE_CHECK(grant.authentication_only && grant.scope.module_mask == 0U && !grant.fee_budget.present);
    FEE_CHECK(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
    FEE_CHECK(lxp_grant_encode(&grant, &arena, &encoded) == LXP_OK && encoded.bytes[4] == 3U);
    FEE_CHECK(encoded.length < sizeof(bytes));
    (void)memcpy(bytes, encoded.bytes, encoded.length);
    FEE_CHECK(lxp_grant_decode(bytes, encoded.length, &decoded) == LXP_OK && decoded.authentication_only);
    for (size_t i = 0U; i < encoded.length; ++i)
        FEE_CHECK(lxp_grant_decode(bytes, i, &decoded) != LXP_OK);
    for (unsigned purpose = 0U; purpose <= 255U; ++purpose) {
        if (purpose == 1U) continue;
        bytes[encoded.length - 1U] = (uint8_t)purpose;
        FEE_CHECK(lxp_grant_decode(bytes, encoded.length, &decoded) != LXP_OK);
    }
    bytes[encoded.length - 1U] = 1U;
    bytes[encoded.length] = 0U;
    FEE_CHECK(lxp_grant_decode(bytes, encoded.length + 1U, &decoded) != LXP_OK);
    grant.scope.module_mask = 2U;
    FEE_CHECK(lxp_grant_id_compute(&grant, grant.grant_id) != LXP_OK);
    grant.scope.module_mask = 0U;
    grant.fee_budget.present = true;
    FEE_CHECK(lxp_grant_id_compute(&grant, grant.grant_id) != LXP_OK);
    grant.fee_budget.present = false;
    (void)memset(grant.key, 0, 32U);
    FEE_CHECK(lxp_grant_id_compute(&grant, grant.grant_id) != LXP_OK);
    return 0;
}

typedef struct session_fixture {
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_fee_params parameters;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_arena arena;
    uint8_t arena_bytes[2U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t did[32];
    uint8_t grant_id[32];
    uint8_t event[209];
} session_fixture;

static const uint8_t session_owner_did[] = "did:layerx:session-owner";

static void put_be(uint8_t *out, uint64_t value, size_t width)
{
    for (size_t i = 0U; i < width; ++i) out[width - 1U - i] = (uint8_t)(value >> (8U * i));
}

static int session_key_read(session_fixture *f)
{
    uint8_t owner_key[32], session_key[32], action_key[32], state[223] = {0};
    uint8_t grant_storage[1024], payload[1024] = {0x71U, 5U, 1U, 3U}, summary[209];
    lxp_authority_grant grant;
    lxp_arena grant_arena;
    lxp_byte_span body;
    lxp_activity activity = {0};
    lxp_authority_resolved authority = {0};
    void *decoded = NULL;
    size_t length, events = 0U;
    for (size_t i = 0U; i < 32U; ++i) {
        owner_key[i] = (uint8_t)(0x21U + i);
        session_key[i] = (uint8_t)(0x41U + i);
        action_key[i] = (uint8_t)(0x61U + i);
    }
    f->parameters.version = 1U;
    f->parameters.multiplier_basis_points = 10000U;
    FEE_CHECK(lxp_state_store_init(&f->store, 2U) == LXP_OK);
    FEE_CHECK(lxp_kernel_create(&f->kernel, &f->store, &f->journal, &f->parameters, 1U) == LXP_OK);
    FEE_CHECK(lxp_kernel_register_module(&f->kernel, lxp_governance_module_iface()) == LXP_OK);
    FEE_CHECK(lxp_did_id_derive(session_owner_did, sizeof(session_owner_did) - 1U, f->did) == LXP_OK);
    (void)memcpy(state, "LXGI1", 5U);
    (void)memcpy(state + 5U, f->did, 32U);
    (void)memcpy(state + 37U, owner_key, 32U);
    put_be(state + 69U, 1U, 8U);
    put_be(state + 215U, 1U, 8U);
    lxp_module_kv_entry *identity = &f->kernel.module_kv[f->kernel.module_kv_count++];
    identity->module_id = LXP_MODULE_GOVERNANCE;
    identity->key_length = 32U;
    identity->value_length = sizeof(state);
    (void)memcpy(identity->key, f->did, 32U);
    (void)memcpy(identity->value, state, sizeof(state));
    FEE_CHECK(lxp_session_key_bind(&grant, f->did, session_key, 2U, 1U, 9U, 10U, 5000U, 1U) == LXP_OK);
    (void)memcpy(f->grant_id, grant.grant_id, 32U);
    FEE_CHECK(lxp_arena_init(&grant_arena, grant_storage, sizeof(grant_storage)) == LXP_OK);
    FEE_CHECK(lxp_grant_encode(&grant, &grant_arena, &body) == LXP_OK);
    length = 4U + 4U + body.length + 8U + 4U + 32U;
    FEE_CHECK(length <= sizeof(payload));
    put_be(payload + 4U, body.length, 4U);
    (void)memcpy(payload + 8U, body.bytes, body.length);
    put_be(payload + 8U + body.length, 100U, 8U);
    put_be(payload + 16U + body.length, 32U, 4U);
    (void)memcpy(payload + 20U + body.length, action_key, 32U);
    activity.protocol_version = 3U;
    activity.network_id = 77U;
    activity.activity_type = 0x00070005U;
    activity.actor_did = (lxp_byte_span){session_owner_did, sizeof(session_owner_did) - 1U};
    activity.authority = (lxp_byte_span){owner_key, 32U};
    activity.payload = (lxp_byte_span){payload, length};
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.actor, f->did, 32U);
    (void)memcpy(authority.verified_key, owner_key, 32U);
    FEE_CHECK(lxp_arena_init(&f->arena, f->arena_bytes, sizeof(f->arena_bytes)) == LXP_OK);
    FEE_CHECK(lxp_module_ctx_init(&f->ctx, &f->kernel, LXP_MODULE_GOVERNANCE,
        1000U, 1U, 2U, 100000U, &f->arena, true) == LXP_OK);
    FEE_CHECK(lxp_effect_buffer_init(&f->effects) == LXP_OK &&
        lxp_module_ctx_bind_effects(&f->ctx, &f->effects) == LXP_OK);
    const lxp_module_iface *module = lxp_governance_module_iface();
    FEE_CHECK(module->decode(&f->ctx, 5U, payload, length, &decoded) == LXP_OK);
    FEE_CHECK(module->validate(&f->ctx, &activity, &authority, decoded) == LXP_OK);
    FEE_CHECK(module->execute(&f->ctx, &activity, &authority, decoded, &f->effects) == LXP_OK);
    for (size_t i = 0U; i < f->effects.count; ++i) {
        if (f->effects.effects[i].event_type != 0x7145U) continue;
        FEE_CHECK(f->effects.effects[i].body_length == sizeof(f->event));
        (void)memcpy(f->event, f->effects.effects[i].body, sizeof(f->event));
        ++events;
    }
    FEE_CHECK(events == 1U);
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, f->grant_id, summary) == LXP_ERR_UNKNOWN_FIELD);
    FEE_CHECK(lxp_module_ctx_prepare_commit(&f->ctx) == LXP_OK && lxp_module_ctx_commit(&f->ctx) == LXP_OK);
    (void)memset(summary, 0, sizeof(summary));
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, f->grant_id, summary) == LXP_OK);
    FEE_CHECK(memcmp(summary, f->event, sizeof(summary)) == 0);
    FEE_CHECK(memcmp(summary, "LXGS2", 5U) == 0 && memcmp(summary + 5U, f->grant_id, 32U) == 0 &&
        memcmp(summary + 37U, f->did, 32U) == 0 && memcmp(summary + 69U, owner_key, 32U) == 0 &&
        memcmp(summary + 101U, action_key, 32U) == 0 && memcmp(summary + 133U, session_key, 32U) == 0);
    uint8_t expected[8];
    put_be(expected, 100U, 8U);
    FEE_CHECK(memcmp(summary + 165U, expected, 8U) == 0);
    put_be(expected, 2U, 8U);
    FEE_CHECK(memcmp(summary + 173U, expected, 8U) == 0);
    FEE_CHECK(summary[181] == 0U && summary[182] == 1U && summary[183] == 0U && summary[184] == 9U);
    put_be(expected, 10U, 8U);
    FEE_CHECK(memcmp(summary + 185U, expected, 8U) == 0);
    put_be(expected, 5000U, 8U);
    FEE_CHECK(memcmp(summary + 193U, expected, 8U) == 0);
    put_be(expected, 1U, 8U);
    FEE_CHECK(memcmp(summary + 201U, expected, 8U) == 0);
    lxp_module_kv_entry *stored = NULL;
    for (size_t i = 0U; i < f->kernel.module_kv_count; ++i)
        if (f->kernel.module_kv[i].key_length == 33U && f->kernel.module_kv[i].key[0] == 0x15U)
            stored = &f->kernel.module_kv[i];
    FEE_CHECK(stored != NULL && stored->module_id == LXP_MODULE_GOVERNANCE);
    stored->value_length = 208U;
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, f->grant_id, summary) == LXP_ERR_NON_CANONICAL);
    stored->value_length = 209U;
    stored->value[4] = (uint8_t)'1';
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, f->grant_id, summary) == LXP_ERR_NON_CANONICAL);
    stored->value[4] = (uint8_t)'2';
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, f->grant_id, summary) == LXP_OK &&
        memcmp(summary, f->event, sizeof(summary)) == 0);
    return 0;
}

static int session_key_read_absent(session_fixture *f)
{
    uint8_t unknown[32], summary[209], untouched[209];
    (void)memset(unknown, 0x05, sizeof(unknown));
    (void)memset(summary, 0xa5, sizeof(summary));
    (void)memcpy(untouched, summary, sizeof(summary));
    FEE_CHECK(lxp_governance_session_key_read(&f->kernel, unknown, summary) == LXP_ERR_UNKNOWN_FIELD);
    FEE_CHECK(memcmp(summary, untouched, sizeof(summary)) == 0);
    FEE_CHECK(lxp_governance_session_key_read(NULL, unknown, summary) == LXP_ERR_NON_CANONICAL);
    return 0;
}

static int session_summaries(void)
{
    session_fixture *fixture = calloc(1U, sizeof(*fixture));
    int failed;
    if (fixture == NULL) return 1;
    failed = session_key_read(fixture);
    if (failed == 0) failed = session_key_read_absent(fixture);
    if (lxp_state_store_destroy(&fixture->store) != LXP_OK) failed = 1;
    free(fixture);
    return failed;
}

int main(void)
{
    if (fee_grants() != 0 || authentication_sessions() != 0 || session_summaries() != 0) return 1;
    uint8_t storage[1024];
    uint8_t second_storage[1024];
    uint8_t grantor[32] = { 1U };
    uint8_t session_key[32] = { 2U };
    lxp_authority_grant grant;
    lxp_arena arena;
    lxp_arena second_arena;
    lxp_byte_span first;
    lxp_byte_span second;
    uint8_t first_id[32];
    uint8_t amended_id[32];
    if (lxp_session_key_bind(&grant, grantor, session_key, UINT64_C(3), 1U,
                             9U, 10U, 20U, 0U) != LXP_OK ||
        lxp_arena_init(&arena, storage, sizeof(storage)) != LXP_OK ||
        lxp_grant_encode(&grant, &arena, &first) != LXP_OK ||
        lxp_grant_id_compute(&grant, first_id) != LXP_OK ||
        memcmp(first_id, grant.grant_id, 32U) != 0) return 1;
    if (lxp_arena_init(&second_arena, second_storage, sizeof(second_storage)) !=
        LXP_OK || lxp_grant_encode(&grant, &second_arena, &second) != LXP_OK ||
        first.length != second.length ||
        memcmp(first.bytes, second.bytes, first.length) != 0) return 1;
    grant.not_after = 19U;
    if (lxp_grant_id_compute(&grant, amended_id) != LXP_OK ||
        memcmp(first_id, amended_id, 32U) == 0) return 1;
    if (lxp_session_key_bind(&grant, grantor, session_key, 0U, 1U, 9U,
                             10U, 20U, 0U) != LXP_ERR_MALFORMED_GRANT ||
        lxp_session_key_bind(&grant, grantor, session_key, 1U, 9U, 1U,
                             10U, 20U, 0U) != LXP_ERR_MALFORMED_GRANT ||
        lxp_session_key_bind(&grant, grantor, session_key, 1U, 1U, 9U,
                             10U, 0U, 0U) != LXP_ERR_MALFORMED_GRANT)
        return 1;
    (void)memset(&grant, 0, sizeof(grant));
    grant.kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    grant.not_after = 20U;
    if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
        lxp_grant_encode(&grant, &arena, &first) != LXP_ERR_MALFORMED_GRANT)
        return 1;
    return 0;
}
