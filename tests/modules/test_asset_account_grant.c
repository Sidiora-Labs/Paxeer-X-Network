#include "layerx/lx_asset.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"

#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define GRANT_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "account grant check failed at line %d\n", __LINE__); \
    return 1; } } while (0)

typedef struct grant_fixture {
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lx_account_registry accounts;
    lx_asset_record genesis_asset;
    lxp_transfer_asset_state genesis_state;
    lx_asset_runtime runtime;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity;
    lxp_authority_resolved authority;
    lxp_arena arena;
    uint64_t parameters;
    uint8_t public_key[32];
    uint8_t signature[64];
    uint8_t payload[1024];
    uint8_t asset_id[32];
} grant_fixture;

static const uint8_t issuer_seed[32] = {21U};
static const uint8_t issuer_did[] = "did:key:account-grant-issuer";
static const uint8_t holder_seed[32] = {22U};
static const uint8_t holder_did[] = "did:key:account-grant-holder";

static uint8_t grant_arena_bytes[1048576];
static uint8_t grant_canonical[LXP_MAX_ACTIVITY_BYTES];

static int grant_sign(const uint8_t seed[32], const uint8_t *message,
                      size_t message_length, uint8_t signature[64],
                      uint8_t public_key[32])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                                  seed, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    size_t public_length = 32U;
    size_t signature_length = 64U;
    int ok = key != NULL && context != NULL &&
             EVP_PKEY_get_raw_public_key(key, public_key, &public_length) == 1 &&
             public_length == 32U &&
             EVP_DigestSignInit(context, NULL, NULL, NULL, key) == 1 &&
             EVP_DigestSign(context, signature, &signature_length,
                            message, message_length) == 1 &&
             signature_length == 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static const lxp_module_kv_entry *grant_kv(const grant_fixture *f,
                                           const char *prefix,
                                           const uint8_t id[32])
{
    uint8_t key[38];
    size_t index;
    (void)memcpy(key, prefix, 6U);
    (void)memcpy(key + 6U, id, 32U);
    for (index = 0U; index < f->kernel.module_kv_count; ++index) {
        const lxp_module_kv_entry *entry = &f->kernel.module_kv[index];
        if (entry->module_id == LXP_MODULE_ASSET && entry->key_length == 38U &&
            memcmp(entry->key, key, 38U) == 0) return entry;
    }
    return NULL;
}

static int grant_dispatch(grant_fixture *f, const uint8_t seed[32],
                          const uint8_t *did, size_t did_length,
                          uint32_t activity_type, const uint8_t *payload,
                          size_t payload_length, uint8_t idempotency_tag,
                          lxp_result *result)
{
    const lxp_module_registration *registration;
    lxp_arena wire_arena;
    lxp_byte_span wire;
    uint8_t digest[32] = {0};
    uint8_t actor[32];
    const uint64_t sequence = f->state.next_sequence;
    GRANT_CHECK(payload_length <= sizeof(f->payload));
    GRANT_CHECK(grant_sign(seed, digest, 32U, f->signature, f->public_key) == 0);
    GRANT_CHECK(lxp_did_id_derive(did, did_length, actor) == LXP_OK);
    (void)memcpy(f->payload, payload, payload_length);
    (void)memset(&f->activity, 0, sizeof(f->activity));
    f->activity.protocol_version = LXP_PROTOCOL_VERSION_OCCUPANCY;
    f->activity.network_id = 7U;
    f->activity.activity_type = activity_type;
    f->activity.actor_did = (lxp_byte_span){did, did_length};
    f->activity.authority = (lxp_byte_span){f->public_key, 32U};
    f->activity.signature = (lxp_byte_span){f->signature, 64U};
    f->activity.payload = (lxp_byte_span){f->payload, payload_length};
    f->activity.idempotency_key[0] = idempotency_tag;
    f->activity.fee_limit.lo = 1000U;
    f->activity.timestamp_bound.not_after = 100U;
    GRANT_CHECK(lxp_hash_payload(f->payload, payload_length,
                                 f->activity.payload_hash) == LXP_OK);
    GRANT_CHECK(lxp_activity_signing_preimage(&f->activity, digest) == LXP_OK);
    GRANT_CHECK(grant_sign(seed, digest, 32U, f->signature, f->public_key) == 0);
    (void)memset(&f->authority, 0, sizeof(f->authority));
    f->authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(f->authority.verified_key, f->public_key, 32U);
    (void)memcpy(f->authority.actor, actor, 32U);
    (void)memcpy(f->authority.principal, actor, 32U);
    GRANT_CHECK(lxp_kernel_module_for_activity(&f->kernel, activity_type, 0U,
                                               &registration) == LXP_OK);
    GRANT_CHECK(lxp_arena_init(&wire_arena, grant_canonical,
                               sizeof(grant_canonical)) == LXP_OK);
    GRANT_CHECK(lxp_activity_encode(&f->activity, &wire_arena, &wire) == LXP_OK);
    GRANT_CHECK(lxp_arena_init(&f->arena, grant_arena_bytes,
                               sizeof(grant_arena_bytes)) == LXP_OK);
    GRANT_CHECK(lxp_effect_buffer_init(&f->effects) == LXP_OK);
    GRANT_CHECK(lxp_state_journal_open(&f->state, sequence,
                                       &f->journal) == LXP_OK);
    GRANT_CHECK(lxp_module_ctx_init(&f->ctx, &f->kernel, LXP_MODULE_ASSET, 10U,
                                    0U, sequence, 100000U, &f->arena,
                                    true) == LXP_OK);
    f->ctx.protocol_version = LXP_PROTOCOL_VERSION_OCCUPANCY;
    f->ctx.batch_number = sequence;
    GRANT_CHECK(lxp_activity_id(wire.bytes, wire.length,
                                f->ctx.activity_id) == LXP_OK);
    GRANT_CHECK(lxp_module_ctx_bind_effects(&f->ctx, &f->effects) == LXP_OK);
    *result = LXP_FATAL_INVARIANT;
    GRANT_CHECK(lxp_kernel_dispatch(registration, &f->ctx, &f->activity,
                                    &f->authority, &f->effects,
                                    result) == LXP_OK);
    return 0;
}

static int grant_finish(grant_fixture *f, lxp_result result)
{
    if (result != LXP_OK) {
        GRANT_CHECK(lxp_state_journal_rollback(&f->journal) == LXP_OK);
        return 0;
    }
    GRANT_CHECK(lxp_state_journal_commit(&f->journal) == LXP_OK);
    GRANT_CHECK(lxp_module_ctx_commit(&f->ctx) == LXP_OK);
    return 0;
}

static int grant_submit(grant_fixture *f, const uint8_t seed[32],
                        const uint8_t *did, size_t did_length,
                        uint32_t activity_type, const uint8_t *payload,
                        size_t payload_length, uint8_t idempotency_tag,
                        lxp_result *result)
{
    GRANT_CHECK(grant_dispatch(f, seed, did, did_length, activity_type,
                               payload, payload_length, idempotency_tag,
                               result) == 0);
    return grant_finish(f, *result);
}

static int grant_event(const grant_fixture *f, uint16_t event_type,
                       const uint8_t body[32])
{
    const lxp_effect *effect = &f->effects.effects[0];
    GRANT_CHECK(f->effects.count == 1U);
    GRANT_CHECK(effect->kind == LXP_EFFECT_EVENT && !effect->monetary);
    GRANT_CHECK(effect->module_id == LXP_MODULE_ASSET);
    GRANT_CHECK(effect->event_type == event_type);
    GRANT_CHECK(effect->body_length == 32U &&
                memcmp(effect->body, body, 32U) == 0);
    return 0;
}

static int grant_setup(grant_fixture *f, uint8_t salt_tag)
{
    uint8_t issuer_actor[32];
    uint8_t salt[32] = {0};
    uint8_t register_payload[89] = {0};
    lx_asset_record record;
    lxp_hash_context hash;
    const lxp_module_kv_entry *entry;
    lxp_result result = LXP_OK;

    salt[0] = salt_tag;
    GRANT_CHECK(lxp_did_id_derive(issuer_did, sizeof(issuer_did) - 1U,
                                  issuer_actor) == LXP_OK);
    lxp_hash_init(&hash);
    GRANT_CHECK(lxp_hash_update(&hash, (const uint8_t *)"LX:ASSET:v1",
                                11U) == LXP_OK);
    GRANT_CHECK(lxp_hash_update(&hash, issuer_actor, 32U) == LXP_OK);
    GRANT_CHECK(lxp_hash_update(&hash, salt, 32U) == LXP_OK);
    GRANT_CHECK(lxp_hash_final(&hash, f->asset_id) == LXP_OK);

    GRANT_CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    f->genesis_asset.asset_id[0] = 0x6aU;
    f->genesis_asset.custody_kind = LX_ASSET_CUSTODY_PAXEER;
    GRANT_CHECK(lx_asset_transfer_state(&f->genesis_asset,
                                        &f->genesis_state) == LXP_OK);
    f->parameters = 1U;
    GRANT_CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    GRANT_CHECK(lxp_state_store_bind_accounts(&f->state, &f->accounts) == LXP_OK);
    GRANT_CHECK(lxp_state_store_require_account_root(&f->state) == LXP_OK);
    GRANT_CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal,
                                  &f->parameters, 0U) == LXP_OK);
    GRANT_CHECK(lxp_kernel_register_module(&f->kernel,
                                           lx_asset_module_iface()) == LXP_OK);
    GRANT_CHECK(lxp_kernel_set_capabilities(
        &f->kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    f->runtime = (lx_asset_runtime){&f->accounts, &f->genesis_asset, 1U,
        &f->genesis_state, 1U, 7U, LXP_PROTOCOL_VERSION_OCCUPANCY};
    GRANT_CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_ASSET,
                                               &f->runtime) == LXP_OK);

    register_payload[1] = 1U;
    (void)memcpy(register_payload + 2U, f->asset_id, 32U);
    (void)memcpy(register_payload + 34U, salt, 32U);
    register_payload[66] = 1U;
    register_payload[67] = 'G';
    register_payload[68] = 1U;
    register_payload[69] = 'G';
    GRANT_CHECK(lxp_u128_to_be((lxp_u128){0U, 100U},
                               register_payload + 71U) == LXP_OK);
    register_payload[87] = 1U;
    GRANT_CHECK(grant_submit(f, issuer_seed, issuer_did,
                             sizeof(issuer_did) - 1U, LX_ASSET_REGISTER,
                             register_payload, sizeof(register_payload), 1U,
                             &result) == 0);
    GRANT_CHECK(result == LXP_OK);
    entry = grant_kv(f, "asset:", f->asset_id);
    GRANT_CHECK(entry != NULL);
    GRANT_CHECK(lx_asset_record_decode(entry->value, entry->value_length,
                                       &record) == LXP_OK);
    GRANT_CHECK(!record.paused &&
                memcmp(record.issuer_did32, issuer_actor, 32U) == 0);
    return 0;
}

static int grant_open_account(grant_fixture *f, uint8_t idempotency_tag,
                              lx_account **opened)
{
    static const char hex[] = "0123456789abcdef";
    char expected[LX_ACCOUNT_NAME_MAX];
    uint8_t open_payload[34] = {0};
    uint8_t expected_id[32];
    const lx_account *staged;
    size_t length;
    size_t accounts_before = f->accounts.count;
    lxp_result result = LXP_OK;

    length = (size_t)snprintf(expected, sizeof(expected), "agent:%s:asset:",
                              (const char *)holder_did);
    for (size_t i = 0U; i < 32U; ++i) {
        expected[length++] = hex[f->asset_id[i] >> 4U];
        expected[length++] = hex[f->asset_id[i] & 15U];
    }
    GRANT_CHECK(length == 6U + (sizeof(holder_did) - 1U) + 7U + 64U);
    GRANT_CHECK(lx_account_id_from_string((const uint8_t *)expected, length,
                                          expected_id) == LXP_OK);

    open_payload[1] = 1U;
    (void)memcpy(open_payload + 2U, f->asset_id, 32U);
    GRANT_CHECK(grant_dispatch(f, holder_seed, holder_did,
                               sizeof(holder_did) - 1U, LX_ASSET_ACCOUNT_OPEN,
                               open_payload, sizeof(open_payload),
                               idempotency_tag, &result) == 0);
    GRANT_CHECK(result == LXP_OK);
    GRANT_CHECK(f->ctx.staged_account_count == 1U);
    staged = &f->ctx.staged_accounts[0].account;
    GRANT_CHECK(staged->kind == LX_ACCOUNT_AGENT_ASSET);
    GRANT_CHECK(staged->name_length == length &&
                memcmp(staged->name, expected, length) == 0);
    GRANT_CHECK(memcmp(staged->id, expected_id, 32U) == 0);
    GRANT_CHECK(staged->has_asset &&
                memcmp(staged->asset_id, f->asset_id, 32U) == 0);
    GRANT_CHECK(staged->has_authority_key &&
                memcmp(staged->authority_key, f->public_key, 32U) == 0);
    GRANT_CHECK(lxp_u128_is_zero(staged->balance));
    GRANT_CHECK(grant_event(f, 4U, expected_id) == 0);
    GRANT_CHECK(f->ctx.ledger_receipt_present &&
                f->ctx.ledger_receipt.operation == 4U &&
                memcmp(f->ctx.ledger_receipt.to, expected_id, 32U) == 0 &&
                memcmp(f->ctx.ledger_receipt.asset, f->asset_id, 32U) == 0);
    GRANT_CHECK(grant_finish(f, result) == 0);
    GRANT_CHECK(f->accounts.count == accounts_before + 1U);
    GRANT_CHECK(lx_account_lookup(&f->accounts, (const uint8_t *)expected,
                                  length, expected_id, opened) == LXP_OK);
    GRANT_CHECK((*opened)->kind == LX_ACCOUNT_AGENT_ASSET);
    return 0;
}

static int account_open_case(void)
{
    grant_fixture *f = (grant_fixture *)calloc(1U, sizeof(*f));
    lx_account *opened = NULL;
    GRANT_CHECK(f != NULL);
    GRANT_CHECK(grant_setup(f, 31U) == 0);
    GRANT_CHECK(grant_open_account(f, 2U, &opened) == 0);
    GRANT_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f);
    return 0;
}

static int account_open_paused_case(void)
{
    grant_fixture *f = (grant_fixture *)calloc(1U, sizeof(*f));
    uint8_t pause_payload[34] = {0};
    uint8_t open_payload[34] = {0};
    const lxp_module_kv_entry *entry;
    lx_asset_record record;
    size_t accounts_before;
    lxp_result result = LXP_OK;

    GRANT_CHECK(f != NULL);
    GRANT_CHECK(grant_setup(f, 32U) == 0);
    pause_payload[1] = 1U;
    (void)memcpy(pause_payload + 2U, f->asset_id, 32U);
    GRANT_CHECK(grant_submit(f, issuer_seed, issuer_did,
                             sizeof(issuer_did) - 1U, LX_ASSET_PAUSE,
                             pause_payload, sizeof(pause_payload), 2U,
                             &result) == 0);
    GRANT_CHECK(result == LXP_OK);
    entry = grant_kv(f, "asset:", f->asset_id);
    GRANT_CHECK(entry != NULL);
    GRANT_CHECK(lx_asset_record_decode(entry->value, entry->value_length,
                                       &record) == LXP_OK && record.paused);

    accounts_before = f->accounts.count;
    open_payload[1] = 1U;
    (void)memcpy(open_payload + 2U, f->asset_id, 32U);
    GRANT_CHECK(grant_dispatch(f, holder_seed, holder_did,
                               sizeof(holder_did) - 1U, LX_ASSET_ACCOUNT_OPEN,
                               open_payload, sizeof(open_payload), 3U,
                               &result) == 0);
    GRANT_CHECK(result == LXP_ERR_ASSET_PAUSED);
    GRANT_CHECK(f->ctx.staged_account_count == 0U);
    GRANT_CHECK(f->effects.count == 0U && !f->ctx.ledger_receipt_present);
    GRANT_CHECK(grant_finish(f, result) == 0);
    GRANT_CHECK(f->accounts.count == accounts_before);
    GRANT_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f);
    return 0;
}

static int grant_issue_case(void)
{
    grant_fixture *f = (grant_fixture *)calloc(1U, sizeof(*f));
    lx_account *opened = NULL;
    lxp_payer_grant grant;
    lxp_grant_state stored;
    const lxp_module_kv_entry *entry;
    uint8_t message[384];
    uint8_t digest[32] = {0};
    uint8_t holder_key[32];
    uint8_t issued[512];
    uint8_t reencoded[512];
    size_t message_length = 0U;
    size_t issued_length = 0U;
    size_t reencoded_length = 0U;
    size_t length;
    size_t kv_before;
    lxp_result result = LXP_OK;

    GRANT_CHECK(f != NULL);
    GRANT_CHECK(grant_setup(f, 33U) == 0);
    GRANT_CHECK(grant_open_account(f, 2U, &opened) == 0);

    (void)memset(&grant, 0, sizeof(grant));
    (void)memcpy(grant.from, opened->id, 32U);
    grant.recipient[0] = 0x42U;
    (void)memcpy(grant.asset, f->asset_id, 32U);
    grant.per_draw_maximum.lo = 5U;
    grant.allowance.lo = 50U;
    grant.expiration = 1000U;
    grant.purpose_hash[0] = 3U;
    GRANT_CHECK(grant_sign(holder_seed, digest, 32U, grant.signature,
                           holder_key) == 0);
    GRANT_CHECK(memcmp(holder_key, opened->authority_key, 32U) == 0);
    (void)memcpy(grant.public_key, holder_key, 32U);
    GRANT_CHECK(lxp_grant_authorization_message(&grant, message,
                                                sizeof(message),
                                                &message_length) == LXP_OK);
    GRANT_CHECK(lxp_hash_authority(message, message_length,
                                   grant.grant_id) == LXP_OK);
    GRANT_CHECK(lxp_hash_domain(LXP_DOMAIN_AUTHORITY_HASH, message,
                                message_length, digest) == LXP_OK);
    GRANT_CHECK(grant_sign(holder_seed, digest, 32U, grant.signature,
                           holder_key) == 0);
    GRANT_CHECK(lxp_verify_payer_grant(&grant, opened) == LXP_OK);
    GRANT_CHECK(lxp_payer_grant_encode(&grant, issued, sizeof(issued),
                                       &issued_length) == LXP_OK);

    GRANT_CHECK(grant_dispatch(f, holder_seed, holder_did,
                               sizeof(holder_did) - 1U, LX_ASSET_GRANT_ISSUE,
                               issued, issued_length, 3U, &result) == 0);
    GRANT_CHECK(result == LXP_OK);
    GRANT_CHECK(grant_event(f, 7U, grant.grant_id) == 0);
    GRANT_CHECK(grant_finish(f, result) == 0);

    entry = grant_kv(f, "grant:", grant.grant_id);
    GRANT_CHECK(entry != NULL && entry->value_length == issued_length + 50U);
    length = entry->value_length - 50U;
    (void)memset(&stored, 0, sizeof(stored));
    GRANT_CHECK(lxp_payer_grant_decode(entry->value, length,
                                       &stored.grant) == LXP_OK);
    GRANT_CHECK(entry->value[length + 48U] <= 1U &&
                entry->value[length + 49U] <= 1U);
    GRANT_CHECK(lxp_u128_from_be(entry->value + length,
                                 &stored.drawn_total) == LXP_OK);
    GRANT_CHECK(lxp_u128_from_be(entry->value + length + 16U,
                                 &stored.drawn_this_period) == LXP_OK);
    for (size_t i = 0U; i < 8U; ++i) {
        stored.window_start = (stored.window_start << 8U) |
                              entry->value[length + 32U + i];
        stored.revoked_at_sequence = (stored.revoked_at_sequence << 8U) |
                                     entry->value[length + 40U + i];
    }
    stored.revoked = entry->value[length + 48U] != 0U;
    stored.invoice_settled = entry->value[length + 49U] != 0U;
    GRANT_CHECK(memcmp(stored.grant.grant_id, grant.grant_id, 32U) == 0);
    GRANT_CHECK(lxp_payer_grant_encode(&stored.grant, reencoded,
                                       sizeof(reencoded),
                                       &reencoded_length) == LXP_OK);
    GRANT_CHECK(reencoded_length == issued_length &&
                memcmp(reencoded, issued, issued_length) == 0);
    GRANT_CHECK(memcmp(stored.grant.from, opened->id, 32U) == 0 &&
                memcmp(stored.grant.asset, f->asset_id, 32U) == 0 &&
                stored.grant.per_draw_maximum.lo == 5U &&
                stored.grant.allowance.lo == 50U &&
                stored.grant.expiration == 1000U);
    GRANT_CHECK(lxp_u128_is_zero(stored.drawn_total) &&
                lxp_u128_is_zero(stored.drawn_this_period));
    GRANT_CHECK(stored.window_start == 0U &&
                stored.revoked_at_sequence == 0U);
    GRANT_CHECK(!stored.revoked && entry->value[length + 48U] == 0U);
    GRANT_CHECK(!stored.invoice_settled);

    kv_before = f->kernel.module_kv_count;
    GRANT_CHECK(grant_dispatch(f, holder_seed, holder_did,
                               sizeof(holder_did) - 1U, LX_ASSET_GRANT_ISSUE,
                               issued, issued_length, 4U, &result) == 0);
    GRANT_CHECK(result == LXP_ERR_SEQUENCE_REUSED);
    GRANT_CHECK(f->effects.count == 0U);
    GRANT_CHECK(grant_finish(f, result) == 0);
    GRANT_CHECK(f->kernel.module_kv_count == kv_before);
    GRANT_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    free(f);
    return 0;
}

#undef GRANT_CHECK

int main(void)
{
    if (account_open_case() != 0) return 1;
    if (account_open_paused_case() != 0) return 1;
    if (grant_issue_case() != 0) return 1;
    return 0;
}
