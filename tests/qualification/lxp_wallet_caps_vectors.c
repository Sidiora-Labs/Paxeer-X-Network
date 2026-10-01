#include "layerx/lx_budget.h"
#include "../../src/modules/asset/lx_asset_registry.c"
#include <openssl/evp.h>
#include <stdio.h>

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "caps vector line %d\n", __LINE__); return 1; } } while (0)
static lxp_kernel kernel;
static lxp_module_ctx ctx;

static void field(const char *name, const uint8_t *bytes, size_t length)
{
    printf("\"%s\":\"", name);
    for (size_t i = 0; i < length; ++i) printf("%02x", bytes[i]);
    printf("\",");
}

static int budget_vectors(void)
{
    lx_budget_record record = {0}, decoded;
    uint8_t bytes[1024], key[LX_BUDGET_STATE_KEY_BYTES];
    size_t length;
    memset(record.budget_id, 1, 32);
    memset(record.owner, 2, 32);
    memset(record.budget_account, 3, 32);
    memset(record.asset_id, 4, 32);
    memset(record.purpose_hash, 5, 32);
    record.per_period_limit = (lxp_u128){0, 100};
    record.configured_period_limit = record.per_period_limit;
    record.spent_this_period = (lxp_u128){0, 23};
    record.period_length = 1000;
    record.period_start = 1000;
    record.expiry = 9000;
    record.revocation_sequence = 31;
    record.rollover_policy = LX_BUDGET_ROLLOVER_NONE;
    record.delegate_count = 2;
    memset(record.delegates[0], 6, 32);
    memset(record.delegates[1], 7, 32);
    CHECK(lx_budget_state_key(record.budget_id, key) == LXP_OK);
    field("budget_key", key, sizeof(key));
    CHECK(lx_budget_record_encode(&record, bytes, sizeof(bytes), &length) == LXP_OK);
    CHECK(lx_budget_record_decode(bytes, length, &decoded) == LXP_OK);
    field("budget_v1", bytes, length);
    record.native_source = true;
    memset(record.source_account, 8, 32);
    CHECK(lx_budget_record_encode(&record, bytes, sizeof(bytes), &length) == LXP_OK);
    CHECK(lx_budget_record_decode(bytes, length, &decoded) == LXP_OK);
    field("budget_v2", bytes, length);
    record.delegate_count = 16;
    for (size_t i = 0; i < 16; ++i) memset(record.delegates[i], (int)i + 1, 32);
    record.per_period_limit = (lxp_u128){UINT64_MAX, UINT64_MAX};
    record.configured_period_limit = record.per_period_limit;
    record.spent_this_period = record.per_period_limit;
    record.carry_cap = record.per_period_limit;
    record.carried = record.per_period_limit;
    record.rollover_policy = LX_BUDGET_ROLLOVER_CAPPED;
    record.period_start = 0;
    record.period_length = UINT64_MAX;
    record.expiry = UINT64_MAX;
    record.revocation_sequence = UINT64_MAX;
    record.closed = true;
    record.revoked = true;
    CHECK(lx_budget_record_encode(&record, bytes, sizeof(bytes), &length) == LXP_OK);
    CHECK(lx_budget_record_decode(bytes, length, &decoded) == LXP_OK);
    field("budget_extreme", bytes, length);
    return 0;
}

static int grant_vector(const char *name, bool recurring, bool extreme)
{
    static const uint8_t test_seed[32] = {17};
    lxp_grant_state state = {0}, decoded;
    lx_account payer = {0};
    uint8_t message[384], key[38];
    size_t length, public_length = 32, signature_length = 64;
    EVP_PKEY *signer = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, test_seed, 32);
    EVP_MD_CTX *sign_context = EVP_MD_CTX_new();
    CHECK(signer != NULL && sign_context != NULL);
    CHECK(EVP_PKEY_get_raw_public_key(signer, state.grant.public_key, &public_length) == 1 && public_length == 32);
    memset(state.grant.from, 2, 32);
    memset(state.grant.recipient, 3, 32);
    memset(state.grant.asset, 4, 32);
    memset(state.grant.purpose_hash, 5, 32);
    memset(state.grant.reference_hash, 6, 32);
    state.grant.has_reference = true;
    state.grant.per_draw_maximum = extreme ? (lxp_u128){UINT64_MAX, UINT64_MAX} : (lxp_u128){0, 10};
    state.grant.allowance = extreme ? state.grant.per_draw_maximum : (lxp_u128){0, 100};
    state.grant.recurring = recurring;
    state.grant.window_length = recurring ? UINT64_MAX : 0;
    state.grant.expiration = UINT64_MAX;
    state.grant.revocation_sequence = UINT64_MAX;
    CHECK(lxp_grant_authorization_message(&state.grant, message, sizeof(message), &length) == LXP_OK);
    CHECK(lxp_hash_authority(message, length, state.grant.grant_id) == LXP_OK);
    CHECK(EVP_DigestSignInit(sign_context, NULL, NULL, NULL, signer) == 1);
    CHECK(EVP_DigestSign(sign_context, state.grant.signature, &signature_length, state.grant.grant_id, 32) == 1 && signature_length == 64);
    memcpy(payer.id, state.grant.from, 32);
    memcpy(payer.authority_key, state.grant.public_key, 32);
    payer.has_authority_key = true;
    CHECK(lxp_verify_payer_grant(&state.grant, &payer) == LXP_OK);
    CHECK(lxp_grant_draw_record(&state, extreme ? state.grant.allowance : (lxp_u128){0, 7}, 0) == LXP_OK);
    state.window_start = extreme ? UINT64_MAX : 9;
    state.revoked_at_sequence = extreme ? UINT64_MAX : 11;
    state.revoked = extreme;
    state.invoice_settled = extreme;
    memset(&ctx, 0, sizeof(ctx));
    ctx.kernel = &kernel;
    ctx.module_id = 1;
    ctx.mutable = true;
    CHECK(grant_save(&ctx, &state) == LXP_OK);
    CHECK(ctx.staged_count == 1 && ctx.staged[0].value_length == 396);
    CHECK(grant_load(&ctx, state.grant.grant_id, &decoded) == LXP_OK);
    CHECK(decoded.drawn_total.hi == state.drawn_total.hi && decoded.drawn_total.lo == state.drawn_total.lo);
    grant_key(key, state.grant.grant_id);
    char key_name[64];
    CHECK(snprintf(key_name, sizeof(key_name), "%s_key", name) > 0);
    field(key_name, key, sizeof(key));
    field(name, ctx.staged[0].value, ctx.staged[0].value_length);
    EVP_MD_CTX_free(sign_context);
    EVP_PKEY_free(signer);
    return 0;
}

int main(void)
{
    printf("{");
    CHECK(budget_vectors() == 0);
    CHECK(grant_vector("grant_once", false, false) == 0);
    CHECK(grant_vector("grant_recurring", true, false) == 0);
    CHECK(grant_vector("grant_extreme", true, true) == 0);
    printf("\"producer\":\"native-budget-codec-and-grant-save\"}\n");
    return 0;
}
