#include "layerx/lxp_authority.h"
#include "layerx/lxp_governance.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_module_ctx.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "revocation check failed at line %d\n", __LINE__); return 1; \
} } while (0)

enum { IDENTITY_BYTES = 223, IDENTITY_REVOCATION = 69, IDENTITY_SEQUENCE = 215,
       REVOKE_BYTES = 45, REVOKED_RECORD_BYTES = 41 };

typedef struct revocation_fixture {
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_fee_params parameters;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_arena arena;
    uint8_t arena_bytes[2U * LXP_MAX_ACTIVITY_BYTES];
} revocation_fixture;

typedef struct governance_actor {
    const uint8_t *did_text;
    size_t did_length;
    uint8_t did[32];
    uint8_t key[32];
} governance_actor;

static const uint8_t grantor_did_text[] = "did:layerx:revocation-grantor";
static const uint8_t other_did_text[] = "did:layerx:revocation-other";

static void put_be(uint8_t *out, uint64_t value, size_t width)
{
    for (size_t i = 0U; i < width; ++i) out[width - 1U - i] = (uint8_t)(value >> (8U * i));
}

static uint64_t get_be64(const uint8_t *in)
{
    uint64_t value = 0U;
    for (size_t i = 0U; i < 8U; ++i) value = (value << 8U) | in[i];
    return value;
}

static int seed_identity(lxp_kernel *kernel, const governance_actor *actor)
{
    uint8_t state[IDENTITY_BYTES] = {0};
    CHECK(kernel->module_kv_count < LXP_KERNEL_MAX_MODULE_KV);
    (void)memcpy(state, "LXGI1", 5U);
    (void)memcpy(state + 5U, actor->did, 32U);
    (void)memcpy(state + 37U, actor->key, 32U);
    put_be(state + IDENTITY_REVOCATION, 1U, 8U);
    put_be(state + IDENTITY_SEQUENCE, 1U, 8U);
    lxp_module_kv_entry *entry = &kernel->module_kv[kernel->module_kv_count++];
    entry->module_id = LXP_MODULE_GOVERNANCE;
    entry->key_length = 32U;
    entry->value_length = sizeof(state);
    (void)memcpy(entry->key, actor->did, 32U);
    (void)memcpy(entry->value, state, sizeof(state));
    return 0;
}

static const lxp_module_kv_entry *find_entry(const lxp_kernel *kernel,
                                             const uint8_t *key, size_t length)
{
    for (size_t i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[i];
        if (entry->module_id == LXP_MODULE_GOVERNANCE && entry->key_length == length &&
            memcmp(entry->key, key, length) == 0) return entry;
    }
    return NULL;
}

static lxp_result dispatch(revocation_fixture *f, const governance_actor *actor,
                           uint32_t activity_type, const uint8_t *payload, size_t length,
                           uint64_t sequence)
{
    lxp_activity activity = {0};
    lxp_authority_resolved authority = {0};
    const lxp_module_iface *module = lxp_governance_module_iface();
    void *decoded = NULL;
    lxp_result status;
    activity.protocol_version = 3U;
    activity.network_id = 77U;
    activity.activity_type = activity_type;
    activity.actor_did = (lxp_byte_span){actor->did_text, actor->did_length};
    activity.authority = (lxp_byte_span){actor->key, 32U};
    activity.payload = (lxp_byte_span){payload, length};
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.actor, actor->did, 32U);
    (void)memcpy(authority.verified_key, actor->key, 32U);
    status = lxp_arena_init(&f->arena, f->arena_bytes, sizeof(f->arena_bytes));
    if (status == LXP_OK)
        status = lxp_module_ctx_init(&f->ctx, &f->kernel, LXP_MODULE_GOVERNANCE,
                                     1000U, 1U, sequence, 100000U, &f->arena, true);
    if (status == LXP_OK) status = lxp_effect_buffer_init(&f->effects);
    if (status == LXP_OK) status = lxp_module_ctx_bind_effects(&f->ctx, &f->effects);
    if (status == LXP_OK)
        status = module->decode(&f->ctx, (uint16_t)activity_type, payload, length, &decoded);
    if (status == LXP_OK) status = module->validate(&f->ctx, &activity, &authority, decoded);
    if (status == LXP_OK)
        status = module->execute(&f->ctx, &activity, &authority, decoded, &f->effects);
    if (status == LXP_OK) status = lxp_module_ctx_prepare_commit(&f->ctx);
    if (status == LXP_OK) status = lxp_module_ctx_commit(&f->ctx);
    return status;
}

static const lxp_effect *find_event(const lxp_effect_buffer *effects, uint16_t event_type,
                                    size_t *count)
{
    const lxp_effect *found = NULL;
    *count = 0U;
    for (size_t i = 0U; i < effects->count; ++i) {
        if (effects->effects[i].event_type != event_type) continue;
        found = &effects->effects[i];
        ++*count;
    }
    return found;
}

static void revoke_payload(uint8_t payload[REVOKE_BYTES], const uint8_t grant_id[32],
                           uint64_t sequence)
{
    payload[0] = 0x71U;
    payload[1] = 6U;
    payload[2] = 0U;
    payload[3] = 3U;
    (void)memcpy(payload + 4U, grant_id, 32U);
    payload[36] = 1U;
    put_be(payload + 37U, sequence, 8U);
}

static int sub_op_6_run(revocation_fixture *f)
{
    governance_actor grantor = {grantor_did_text, sizeof(grantor_did_text) - 1U, {0}, {0}};
    governance_actor other = {other_did_text, sizeof(other_did_text) - 1U, {0}, {0}};
    uint8_t capability_key[32], grant_id[32], grant_storage[1024], issue[1024] = {0x71U, 8U, 1U, 1U};
    uint8_t revoke[REVOKE_BYTES], expected[REVOKED_RECORD_BYTES], key[33];
    lxp_authority_grant grant;
    lxp_arena grant_arena;
    lxp_byte_span body;
    const lxp_module_kv_entry *entry;
    const lxp_effect *event;
    size_t issue_length, count;
    for (size_t i = 0U; i < 32U; ++i) {
        grantor.key[i] = (uint8_t)(0x21U + i);
        other.key[i] = (uint8_t)(0x81U + i);
        capability_key[i] = (uint8_t)(0x41U + i);
    }
    f->parameters.version = 1U;
    f->parameters.multiplier_basis_points = 10000U;
    CHECK(lxp_state_store_init(&f->store, 2U) == LXP_OK);
    CHECK(lxp_kernel_create(&f->kernel, &f->store, &f->journal, &f->parameters, 1U) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lxp_governance_module_iface()) == LXP_OK);
    CHECK(lxp_did_id_derive(grantor.did_text, grantor.did_length, grantor.did) == LXP_OK);
    CHECK(lxp_did_id_derive(other.did_text, other.did_length, other.did) == LXP_OK);
    CHECK(seed_identity(&f->kernel, &grantor) == 0 && seed_identity(&f->kernel, &other) == 0);

    (void)memset(&grant, 0, sizeof(grant));
    grant.kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    (void)memcpy(grant.grantor, grantor.did, 32U);
    (void)memcpy(grant.grantee, grantor.did, 32U);
    (void)memcpy(grant.key, capability_key, 32U);
    grant.scope.module_mask = UINT64_C(1) << LXP_MODULE_GOVERNANCE;
    grant.scope.activity_ordinal_min = 1U;
    grant.scope.activity_ordinal_max = 8U;
    grant.scope.asset_id[0] = 4U;
    grant.scope.maximum_per_activity = (lxp_u128){0U, 100U};
    grant.scope.maximum_total = (lxp_u128){0U, 1000U};
    grant.scope.purpose_hash[0] = 5U;
    grant.not_before = 10U;
    grant.not_after = 5000U;
    grant.grantor_revocation_sequence = 1U;
    CHECK(lxp_grant_id_compute(&grant, grant_id) == LXP_OK);
    CHECK(lxp_arena_init(&grant_arena, grant_storage, sizeof(grant_storage)) == LXP_OK);
    CHECK(lxp_grant_encode(&grant, &grant_arena, &body) == LXP_OK);
    issue_length = 8U + body.length;
    CHECK(issue_length <= sizeof(issue));
    put_be(issue + 4U, body.length, 4U);
    (void)memcpy(issue + 8U, body.bytes, body.length);
    CHECK(dispatch(f, &grantor, 0x00070008U, issue, issue_length, 32U) == LXP_OK);
    key[0] = 5U;
    (void)memcpy(key + 1U, grant_id, 32U);
    entry = find_entry(&f->kernel, key, sizeof(key));
    CHECK(entry != NULL && entry->value_length == body.length &&
          memcmp(entry->value, body.bytes, body.length) == 0);

    revoke_payload(revoke, grant_id, 33U);
    CHECK(dispatch(f, &grantor, 0x00070006U, revoke, sizeof(revoke), 33U) == LXP_OK);
    (void)memcpy(expected, grant_id, 32U);
    expected[32] = 1U;
    put_be(expected + 33U, 33U, 8U);
    key[0] = 6U;
    entry = find_entry(&f->kernel, key, sizeof(key));
    CHECK(entry != NULL && entry->value_length == REVOKED_RECORD_BYTES &&
          memcmp(entry->value, expected, sizeof(expected)) == 0);
    event = find_event(&f->effects, 0x7106U, &count);
    CHECK(count == 1U && event->body_length == REVOKED_RECORD_BYTES &&
          memcmp(event->body, expected, sizeof(expected)) == 0);
    entry = find_entry(&f->kernel, grantor.did, 32U);
    CHECK(entry != NULL && entry->value_length == IDENTITY_BYTES &&
          get_be64(entry->value + IDENTITY_REVOCATION) == 33U &&
          get_be64(entry->value + IDENTITY_SEQUENCE) == 33U);
    event = find_event(&f->effects, 0x7110U, &count);
    CHECK(count == 1U && event->body_length == IDENTITY_BYTES &&
          memcmp(event->body, entry->value, IDENTITY_BYTES) == 0);

    revoke_payload(revoke, grant_id, 34U);
    CHECK(dispatch(f, &grantor, 0x00070006U, revoke, sizeof(revoke), 34U) == LXP_ERR_AUTH_REVOKED);
    revoke_payload(revoke, grant_id, 35U);
    CHECK(dispatch(f, &other, 0x00070006U, revoke, sizeof(revoke), 35U) == LXP_ERR_AUTH_SCOPE);
    key[0] = 6U;
    entry = find_entry(&f->kernel, key, sizeof(key));
    CHECK(entry != NULL && entry->value_length == REVOKED_RECORD_BYTES &&
          memcmp(entry->value, expected, sizeof(expected)) == 0);
    entry = find_entry(&f->kernel, grantor.did, 32U);
    CHECK(entry != NULL && get_be64(entry->value + IDENTITY_REVOCATION) == 33U);
    entry = find_entry(&f->kernel, other.did, 32U);
    CHECK(entry != NULL && get_be64(entry->value + IDENTITY_REVOCATION) == 1U &&
          get_be64(entry->value + IDENTITY_SEQUENCE) == 1U);
    return 0;
}

static int sub_op_6_case(void)
{
    revocation_fixture *fixture = calloc(1U, sizeof(*fixture));
    int failed;
    if (fixture == NULL) return 1;
    failed = sub_op_6_run(fixture);
    if (lxp_state_store_destroy(&fixture->store) != LXP_OK) failed = 1;
    free(fixture);
    return failed;
}

static void initialize(lxp_authority_grant *grant)
{
    (void)memset(grant, 0, sizeof(*grant));
    grant->kind = LXP_AUTHORITY_DELEGATED_CAPABILITY;
    grant->grantor[0] = 1U;
    grant->grantee[0] = 2U;
    grant->key[0] = 3U;
    grant->scope.module_mask = 1U;
    grant->scope.activity_ordinal_max = 10U;
    grant->scope.asset_id[0] = 4U;
    grant->scope.maximum_per_activity = (lxp_u128){ 0U, 100U };
    grant->scope.maximum_total = (lxp_u128){ 0U, 1000U };
    grant->scope.maximum_per_period = (lxp_u128){ 0U, 100U };
    grant->scope.period_length = 10U;
    grant->scope.purpose_hash[0] = 5U;
    grant->not_before = 10U;
    grant->not_after = 100U;
    grant->grantor_revocation_sequence = 1U;
}

int main(void)
{
    lxp_authority_grant grant;
    lxp_authority_grant narrower;
    lxp_authority_grant wider;
    lxp_identity identity;
    if (sub_op_6_case() != 0) return 1;
    initialize(&grant);
    if (lxp_authority_is_live(&grant, 1U, 50U, 8U) != LXP_OK ||
        lxp_authority_is_live(&grant, 1U, 100U, 8U) != LXP_ERR_AUTH_EXPIRED ||
        lxp_authority_revoke(&grant, 2U, 10U) != LXP_OK ||
        lxp_authority_is_live(&grant, 2U, 50U, 9U) != LXP_OK ||
        lxp_authority_is_live(&grant, 2U, 50U, 10U) != LXP_ERR_AUTH_REVOKED ||
        lxp_authority_revoke(&grant, 2U, 11U) != LXP_ERR_STALE_REVOCATION)
        return 1;
    initialize(&grant);
    narrower = grant;
    narrower.scope.maximum_total = (lxp_u128){ 0U, 900U };
    narrower.scope.module_mask = 1U;
    narrower.not_after = 90U;
    narrower.grantor_revocation_sequence = 2U;
    if (lxp_authority_amend(&grant, &narrower) != LXP_OK ||
        grant.scope.maximum_total.lo != 900U) return 1;
    wider = grant;
    wider.scope.maximum_total = (lxp_u128){ 0U, 901U };
    wider.grantor_revocation_sequence = 3U;
    if (lxp_authority_amend(&grant, &wider) != LXP_ERR_AUTH_SCOPE) return 1;
    (void)memset(&identity, 0, sizeof(identity));
    identity.revocation_sequence = 4U;
    if (lxp_identity_bump_revocation_sequence(&identity, 5U) != LXP_OK ||
        lxp_identity_bump_revocation_sequence(&identity, 5U) !=
            LXP_ERR_STALE_REVOCATION ||
        lxp_authority_is_live(&grant, identity.revocation_sequence, 50U, 1U) !=
            LXP_ERR_AUTH_REVOKED) return 1;
    return 0;
}
