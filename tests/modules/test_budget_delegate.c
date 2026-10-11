#include "layerx/lx_budget.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_state_proof.h"

#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK(x) do { \
    if (!(x)) { fprintf(stderr, "%s line %d\n", __func__, __LINE__); \
                return 1; } \
} while (0)

static unsigned cases;

#define CASE(name, x) do { \
    if (!(x)) { fprintf(stderr, "%s line %d\n", __func__, __LINE__); \
                printf("BUDGET_CASE %s fail\n", name); return 1; } \
    printf("BUDGET_CASE %s ok\n", name); ++cases; \
} while (0)

static lxp_result apply_capability(lxp_kernel *kernel,
                                   const lxp_transfer_set *set,
                                   lxp_receipt *receipt)
{
    lxp_transfer_set_result result;
    lxp_transfer_context context = set->context;
    lxp_result status;
    (void)kernel;
    status = lxp_apply_transfer_set((lxp_transfer_leg *)set->legs,
                                    set->leg_count, &context, &result);
    if (status == LXP_OK)
        (void)memcpy(receipt->transfer_set_root, result.transfer_set_root, 32U);
    return status;
}

static int sign_grant(lxp_payer_grant *grant, const uint8_t seed[32])
{
    uint8_t message[384];
    uint8_t digest[32];
    size_t message_length;
    size_t signature_length = 64U;
    size_t public_length = 32U;
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                                  seed, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    int failed = key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, grant->public_key,
                                    &public_length) != 1 ||
        lxp_grant_authorization_message(grant, message, sizeof(message),
                                        &message_length) != LXP_OK ||
        lxp_hash_authority(message, message_length, grant->grant_id) != LXP_OK ||
        lxp_hash_domain(LXP_DOMAIN_AUTHORITY_HASH, message, message_length,
                        digest) != LXP_OK ||
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
        EVP_DigestSign(context, grant->signature, &signature_length,
                       digest, sizeof(digest)) != 1 ||
        signature_length != 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return failed;
}

static int library_vectors(void)
{
    static const uint8_t seed[32] = { 9U };
    lx_account budget_account;
    lx_account recipient;
    lx_account other;
    lx_account grantor;
    lx_asset_record asset;
    lxp_transfer_asset_state asset_state;
    lx_budget_store store;
    lx_budget_delegate_capability capability;
    lx_budget_delegate_spend_request delegated;
    lx_budget_pull_request pull;
    lxp_payer_grant grant;
    lxp_grant_state grant_state;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lxp_receipt receipt;
    uint8_t arena_bytes[4096];
    uint64_t parameters = 1U;
    uint8_t delegate_a[32] = { 2U };
    uint8_t delegate_b[32] = { 1U };

    (void)memset(&budget_account, 0, sizeof(budget_account));
    (void)memset(&recipient, 0, sizeof(recipient));
    (void)memset(&other, 0, sizeof(other));
    (void)memset(&grantor, 0, sizeof(grantor));
    (void)memset(&asset, 0, sizeof(asset));
    (void)memset(&store, 0, sizeof(store));
    budget_account.id[0] = 3U; budget_account.kind = LX_ACCOUNT_AGENT_BUDGET;
    recipient.id[0] = 4U; recipient.kind = LX_ACCOUNT_AGENT_MAIN;
    other.id[0] = 5U; other.kind = LX_ACCOUNT_AGENT_MAIN;
    grantor.id[0] = 6U; grantor.kind = LX_ACCOUNT_AGENT_MAIN;
    asset.asset_id[0] = 7U;
    if (lxp_ledger_bootstrap_balance(&budget_account, asset.asset_id,
                                     (lxp_u128){ 0U, 100U }, 0U) != LXP_OK ||
        lxp_ledger_bootstrap_balance(&recipient, asset.asset_id,
                                     (lxp_u128){ 0U, 0U }, 0U) != LXP_OK ||
        lxp_ledger_bootstrap_balance(&other, asset.asset_id,
                                     (lxp_u128){ 0U, 0U }, 0U) != LXP_OK ||
        lx_asset_transfer_state(&asset, &asset_state) != LXP_OK ||
        lxp_state_store_init(&state, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, lx_budget_module_iface()) != LXP_OK ||
        lxp_kernel_set_capabilities(&kernel, NULL, apply_capability) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_BUDGET, 100U, 0U, 1U,
                            1000U, &arena, true) != LXP_OK)
        return 1;
    store.count = 1U;
    store.records[0].budget_id[0] = 8U;
    (void)memcpy(store.records[0].owner, grantor.id, 32U);
    (void)memcpy(store.records[0].budget_account, budget_account.id, 32U);
    (void)memcpy(store.records[0].asset_id, asset.asset_id, 32U);
    store.records[0].per_period_limit = (lxp_u128){ 0U, 100U };
    store.records[0].configured_period_limit = (lxp_u128){ 0U, 100U };
    store.records[0].period_start = 1U;
    store.records[0].period_length = 1000U;
    store.records[0].expiry = 1000U;
    store.records[0].revocation_sequence = 5U;
    store.records[0].purpose_hash[0] = 9U;
    if (lx_budget_delegate_add_execute(&store.records[0], delegate_a) != LXP_OK ||
        lx_budget_delegate_add_execute(&store.records[0], delegate_b) != LXP_OK ||
        memcmp(store.records[0].delegates[0], delegate_b, 32U) != 0 ||
        memcmp(store.records[0].delegates[1], delegate_a, 32U) != 0)
        return 1;
    store.records[0].delegate_count = LX_BUDGET_MAX_DELEGATES + 1U;
    if (lx_budget_delegate_add_execute(&store.records[0], delegate_a) !=
            LXP_ERR_NON_CANONICAL ||
        lx_budget_delegate_remove_execute(&store.records[0], delegate_a) !=
            LXP_ERR_NON_CANONICAL)
        return 1;
    store.records[0].delegate_count = 2U;
    (void)memset(&capability, 0, sizeof(capability));
    (void)memcpy(capability.holder, delegate_a, 32U);
    (void)memcpy(capability.asset_id, asset.asset_id, 32U);
    (void)memcpy(capability.recipient, recipient.id, 32U);
    (void)memcpy(capability.purpose_hash, store.records[0].purpose_hash, 32U);
    capability.maximum_per_spend = (lxp_u128){ 0U, 20U };
    capability.maximum_total = (lxp_u128){ 0U, 30U };
    capability.expiry = 200U;
    capability.revocation_sequence = 5U;
    (void)memset(&delegated, 0, sizeof(delegated));
    delegated.spend.store = &store;
    delegated.spend.budget_id = store.records[0].budget_id;
    delegated.spend.budget_account = &budget_account;
    delegated.spend.recipient = &recipient;
    delegated.spend.asset = &asset;
    delegated.spend.amount = (lxp_u128){ 0U, 20U };
    delegated.spend.context.assets = &asset_state;
    delegated.spend.context.asset_count = 1U;
    delegated.spend.context.sequence_account = &budget_account;
    (void)memcpy(delegated.spend.context.authorized_from,
                 budget_account.id, 32U);
    delegated.submitter = delegate_a;
    delegated.capability = &capability;
    if (lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) != LXP_OK ||
        capability.consumed.lo != 20U || budget_account.balance.lo != 80U)
        return 1;
    delegated.spend.amount = (lxp_u128){ 0U, 11U };
    delegated.spend.context.actor_sequence = budget_account.next_sequence;
    if (lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) !=
            LXP_ERR_UNAUTHORIZED_DELEGATE || capability.consumed.lo != 20U)
        return 1;
    capability.expiry = 99U;
    delegated.spend.amount = (lxp_u128){ 0U, 1U };
    if (lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) !=
        LXP_ERR_UNAUTHORIZED_DELEGATE) return 1;
    capability.expiry = 200U;
    (void)memcpy(capability.recipient, other.id, 32U);
    if (lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) !=
        LXP_ERR_UNAUTHORIZED_DELEGATE) return 1;
    (void)memcpy(capability.recipient, recipient.id, 32U);
    capability.revoked = true;
    if (lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) !=
        LXP_ERR_UNAUTHORIZED_DELEGATE) return 1;
    capability.revoked = false;
    if (lx_budget_delegate_remove_execute(&store.records[0], delegate_a) != LXP_OK ||
        lx_budget_delegate_spend_execute(&ctx, &delegated, &receipt) !=
            LXP_ERR_UNAUTHORIZED_DELEGATE)
        return 1;

    (void)memset(&grant, 0, sizeof(grant));
    (void)memcpy(grant.from, grantor.id, 32U);
    (void)memcpy(grant.recipient, recipient.id, 32U);
    (void)memcpy(grant.asset, asset.asset_id, 32U);
    grant.per_draw_maximum = (lxp_u128){ 0U, 10U };
    grant.allowance = (lxp_u128){ 0U, 20U };
    grant.expiration = 200U;
    grant.revocation_sequence = 5U;
    (void)memcpy(grant.purpose_hash, store.records[0].purpose_hash, 32U);
    if (sign_grant(&grant, seed) != 0) return 1;
    (void)memcpy(grantor.authority_key, grant.public_key, 32U);
    grantor.has_authority_key = true;
    (void)memset(&grant_state, 0, sizeof(grant_state));
    grant_state.grant = grant;
    (void)memset(&pull, 0, sizeof(pull));
    pull.spend = delegated.spend;
    pull.spend.amount = (lxp_u128){ 0U, 10U };
    pull.grant_state = &grant_state;
    pull.grantor = &grantor;
    if (lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_BUDGET, 100U, 0U, 2U,
                            1000U, &arena, true) != LXP_OK ||
        lx_budget_pull_execute(&ctx, &pull, &receipt) != LXP_OK ||
        budget_account.balance.lo != 70U || recipient.balance.lo != 30U ||
        grant_state.drawn_total.lo != 10U)
        return 1;
    pull.spend.amount = (lxp_u128){ 0U, 11U };
    pull.spend.context.actor_sequence = budget_account.next_sequence;
    if (lx_budget_pull_execute(&ctx, &pull, &receipt) !=
            LXP_ERR_GRANT_SCOPE_VIOLATION ||
        grant_state.drawn_total.lo != 10U ||
        lxp_state_store_destroy(&state) != LXP_OK)
        return 1;
    return 0;
}

static lxp_result pull_once(lxp_kernel *kernel, lx_budget_pull_request *pull,
                            const lx_account *sequence_account,
                            uint64_t global_sequence, uint64_t amount)
{
    static uint8_t arena_bytes[4096];
    lxp_module_ctx ctx;
    lxp_arena arena;
    lxp_receipt receipt;
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_BUDGET, 100U, 0U,
                            global_sequence, 1000U, &arena, true) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    pull->spend.amount = (lxp_u128){ 0U, amount };
    pull->spend.context.actor_sequence = sequence_account->next_sequence;
    return lx_budget_pull_execute(&ctx, pull, &receipt);
}

static int pull_unchanged(const lx_budget_record *record,
                          const lx_budget_record *before,
                          const lx_account *budget_account,
                          const lx_account *recipient,
                          const lxp_grant_state *grant_state)
{
    return memcmp(record, before, sizeof(*record)) == 0 &&
           budget_account->balance.hi == 0U &&
           budget_account->balance.lo == 75U &&
           recipient->balance.hi == 0U && recipient->balance.lo == 25U &&
           grant_state->drawn_total.hi == 0U &&
           grant_state->drawn_total.lo == 25U;
}

static int test_pull_activity(void)
{
    static const uint8_t seed[32] = { 13U };
    static lx_budget_store store;
    lx_account budget_account;
    lx_account recipient;
    lx_account grantor;
    lx_asset_record asset;
    lxp_transfer_asset_state asset_state;
    lx_budget_record before;
    lx_budget_pull_request pull;
    lxp_grant_state grant_state;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    uint64_t parameters = 1U;
    uint64_t sequence = 0U;
    lx_budget_record *record = &store.records[0];

    (void)memset(&budget_account, 0, sizeof(budget_account));
    (void)memset(&recipient, 0, sizeof(recipient));
    (void)memset(&grantor, 0, sizeof(grantor));
    (void)memset(&asset, 0, sizeof(asset));
    (void)memset(&store, 0, sizeof(store));
    budget_account.id[0] = 0x31U;
    budget_account.kind = LX_ACCOUNT_AGENT_BUDGET;
    recipient.id[0] = 0x32U;
    recipient.kind = LX_ACCOUNT_AGENT_MAIN;
    grantor.id[0] = 0x33U;
    grantor.kind = LX_ACCOUNT_AGENT_MAIN;
    asset.asset_id[0] = 0x34U;
    CHECK(lxp_ledger_bootstrap_balance(&budget_account, asset.asset_id,
                                       (lxp_u128){ 0U, 100U }, 0U) == LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(&recipient, asset.asset_id,
                                       (lxp_u128){ 0U, 0U }, 0U) == LXP_OK);
    CHECK(lx_asset_transfer_state(&asset, &asset_state) == LXP_OK);
    CHECK(lxp_state_store_init(&state, 0U) == LXP_OK);
    CHECK(lxp_kernel_create(&kernel, &state, &journal, &parameters, 0U) ==
          LXP_OK);
    CHECK(lxp_kernel_register_module(&kernel, lx_budget_module_iface()) ==
          LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&kernel, NULL, apply_capability) ==
          LXP_OK);

    store.count = 1U;
    record->budget_id[0] = 0x35U;
    (void)memcpy(record->owner, grantor.id, 32U);
    (void)memcpy(record->budget_account, budget_account.id, 32U);
    (void)memcpy(record->asset_id, asset.asset_id, 32U);
    record->per_period_limit = (lxp_u128){ 0U, 100U };
    record->configured_period_limit = (lxp_u128){ 0U, 100U };
    record->period_start = 1U;
    record->period_length = 1000U;
    record->expiry = 1000U;
    record->revocation_sequence = 5U;
    record->purpose_hash[0] = 0x36U;
    CHECK(lx_budget_delegate_add_execute(record, recipient.id) == LXP_OK);

    (void)memset(&grant_state, 0, sizeof(grant_state));
    (void)memcpy(grant_state.grant.from, grantor.id, 32U);
    (void)memcpy(grant_state.grant.recipient, recipient.id, 32U);
    (void)memcpy(grant_state.grant.asset, asset.asset_id, 32U);
    grant_state.grant.per_draw_maximum = (lxp_u128){ 0U, 40U };
    grant_state.grant.allowance = (lxp_u128){ 0U, 40U };
    grant_state.grant.expiration = 200U;
    grant_state.grant.revocation_sequence = 5U;
    (void)memcpy(grant_state.grant.purpose_hash, record->purpose_hash, 32U);
    CHECK(sign_grant(&grant_state.grant, seed) == 0);
    (void)memcpy(grantor.authority_key, grant_state.grant.public_key, 32U);
    grantor.has_authority_key = true;

    (void)memset(&pull, 0, sizeof(pull));
    pull.spend.store = &store;
    pull.spend.budget_id = record->budget_id;
    pull.spend.budget_account = &budget_account;
    pull.spend.recipient = &recipient;
    pull.spend.asset = &asset;
    pull.spend.context.assets = &asset_state;
    pull.spend.context.asset_count = 1U;
    pull.spend.context.sequence_account = &budget_account;
    (void)memcpy(pull.spend.context.authorized_from, budget_account.id, 32U);
    pull.grant_state = &grant_state;
    pull.grantor = &grantor;

    CASE("pull-activity-draws",
         pull_once(&kernel, &pull, &budget_account, ++sequence, 25U) ==
             LXP_OK &&
         budget_account.balance.lo == 75U && recipient.balance.lo == 25U &&
         record->spent_this_period.hi == 0U &&
         record->spent_this_period.lo == 25U &&
         grant_state.drawn_total.lo == 25U &&
         grant_state.drawn_this_period.lo == 25U);
    (void)memcpy(&before, record, sizeof(before));

    CASE("pull-activity-allowance-exhausted",
         pull_once(&kernel, &pull, &budget_account, ++sequence, 20U) ==
             LXP_ERR_GRANT_EXHAUSTED &&
         pull_unchanged(record, &before, &budget_account, &recipient,
                        &grant_state));
    CASE("pull-activity-over-allowance",
         pull_once(&kernel, &pull, &budget_account, ++sequence, 41U) ==
             LXP_ERR_GRANT_SCOPE_VIOLATION &&
         pull_unchanged(record, &before, &budget_account, &recipient,
                        &grant_state));

    /* A refused spend leaves the grant draw where it was. */
    record->per_period_limit = (lxp_u128){ 0U, 30U };
    (void)memcpy(&before, record, sizeof(before));
    CASE("pull-activity-draw-rollback",
         pull_once(&kernel, &pull, &budget_account, ++sequence, 10U) ==
             LXP_ERR_BUDGET_ALLOWANCE_EXCEEDED &&
         pull_unchanged(record, &before, &budget_account, &recipient,
                        &grant_state));
    record->per_period_limit = (lxp_u128){ 0U, 100U };
    (void)memcpy(&before, record, sizeof(before));

    grant_state.revoked = true;
    grant_state.revoked_at_sequence = sequence + 1U;
    CASE("pull-activity-revoked",
         pull_once(&kernel, &pull, &budget_account, ++sequence, 5U) ==
             LXP_ERR_GRANT_REVOKED &&
         pull_unchanged(record, &before, &budget_account, &recipient,
                        &grant_state));
    CHECK(lxp_state_store_destroy(&state) == LXP_OK);
    return 0;
}

static int sign_raw(const uint8_t seed[32], const uint8_t *message,
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

enum { CAPABILITY_KEY_BYTES = 80, CAPABILITY_BYTES = 195 };

static const char owner_did[] = "did:key:owner";
static const char delegate_did[] = "did:key:dele";
static const uint8_t owner_seed[32] = { 11U };
static const uint8_t delegate_seed[32] = { 22U };
static const uint8_t budget_one[32] = { 0x0DU, 0xEFU, 0x01U };
static const uint8_t budget_two[32] = { 0x0DU, 0xEFU, 0x02U };
static const uint8_t budget_three[32] = { 0x0DU, 0xEFU, 0x03U };

typedef struct budget_env {
    lx_account_registry accounts;
    lx_asset_record asset;
    lxp_transfer_asset_state asset_state;
    lx_asset_runtime asset_runtime;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    const lxp_module_registration *registration;
    lx_account *owner;
    lx_account *budget_one;
    lx_account *budget_two;
    lx_account *budget_three;
    lx_account *recipient;
    lx_account *other;
    lx_account *delegate;
    uint8_t delegate_actor[32];
    uint8_t owner_actor[32];
    uint64_t parameters;
    uint64_t global_sequence;
} budget_env;

typedef struct budget_call {
    uint32_t activity_type;
    const char *did;
    const uint8_t *seed;
    const uint8_t *payload;
    size_t payload_length;
    uint64_t sequence;
    uint64_t timestamp;
} budget_call;

static budget_env env;

static void put_u64(uint8_t *bytes, uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bytes[i] = (uint8_t)(value >> (56U - i * 8U));
}

static void put_u128(uint8_t *bytes, uint64_t high, uint64_t low)
{
    put_u64(bytes, high);
    put_u64(bytes + 8U, low);
}

static uint64_t get_u64(const uint8_t *bytes)
{
    uint64_t value = 0U;
    size_t i;
    for (i = 0U; i < 8U; ++i) value = (value << 8U) | bytes[i];
    return value;
}

static int open_account(const char *name, uint64_t balance,
                        const uint8_t *authority_key, lx_account **account)
{
    uint8_t id[32];
    size_t length = strlen(name);
    if (lx_account_id_from_string((const uint8_t *)name, length, id) != LXP_OK ||
        lx_account_open(&env.accounts, (const uint8_t *)name, length, id, 1U,
                        LX_ACCOUNT_OPEN_CREDIT, NULL, account) != LXP_OK ||
        lxp_ledger_bootstrap_balance(*account, env.asset.asset_id,
                                     (lxp_u128){ 0U, balance }, 0U) != LXP_OK)
        return 1;
    if (authority_key != NULL) {
        (void)memcpy((*account)->authority_key, authority_key, 32U);
        (*account)->has_authority_key = true;
    }
    return lx_account_validate_canonical(*account) == LXP_OK ? 0 : 1;
}

static int env_init(void)
{
    uint8_t owner_key[32];
    uint8_t delegate_key[32];
    uint8_t signature[64];
    (void)memset(&env, 0, sizeof(env));
    env.parameters = 1U;
    env.asset.asset_id[0] = 0x5AU;
    env.asset.asset_id[31] = 0xC3U;
    CHECK(sign_raw(owner_seed, env.asset.asset_id, 0U, signature,
                   owner_key) == 0);
    CHECK(sign_raw(delegate_seed, env.asset.asset_id, 0U, signature,
                   delegate_key) == 0);
    CHECK(lxp_did_id_derive((const uint8_t *)owner_did, strlen(owner_did),
                            env.owner_actor) == LXP_OK);
    CHECK(lxp_did_id_derive((const uint8_t *)delegate_did,
                            strlen(delegate_did),
                            env.delegate_actor) == LXP_OK);
    CHECK(lx_asset_transfer_state(&env.asset, &env.asset_state) == LXP_OK);
    CHECK(lx_account_registry_init(&env.accounts) == LXP_OK);
    CHECK(open_account("agent:did:key:owner:main", 1000U, owner_key,
                       &env.owner) == 0);
    CHECK(open_account("agent:did:key:owner:budget:b1", 0U, NULL,
                       &env.budget_one) == 0);
    CHECK(open_account("agent:did:key:owner:budget:b2", 0U, NULL,
                       &env.budget_two) == 0);
    CHECK(open_account("agent:did:key:owner:budget:b3", 0U, NULL,
                       &env.budget_three) == 0);
    CHECK(open_account("agent:did:key:recv:main", 0U, NULL,
                       &env.recipient) == 0);
    CHECK(open_account("agent:did:key:other:main", 0U, NULL,
                       &env.other) == 0);
    CHECK(open_account("agent:did:key:dele:main", 0U, delegate_key,
                       &env.delegate) == 0);
    CHECK(lxp_state_store_init(&env.state, 0U) == LXP_OK);
    CHECK(lxp_state_store_bind_accounts(&env.state, &env.accounts) == LXP_OK);
    CHECK(lxp_state_store_require_account_root(&env.state) == LXP_OK);
    CHECK(lxp_kernel_create(&env.kernel, &env.state, &env.journal,
                            &env.parameters, 0U) == LXP_OK);
    CHECK(lxp_kernel_register_module(&env.kernel,
                                     lx_budget_module_iface()) == LXP_OK);
    CHECK(lxp_kernel_set_capabilities(
              &env.kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    env.asset_runtime.accounts = &env.accounts;
    env.asset_runtime.assets = &env.asset;
    env.asset_runtime.asset_count = 1U;
    env.asset_runtime.transfer_assets = &env.asset_state;
    env.asset_runtime.transfer_asset_count = 1U;
    env.asset_runtime.network_id = 7U;
    env.asset_runtime.protocol_version = LXP_PROTOCOL_VERSION_OCCUPANCY;
    CHECK(lxp_kernel_bind_module_runtime(&env.kernel, LXP_MODULE_ASSET,
                                         &env.asset_runtime) == LXP_OK);
    CHECK(lxp_kernel_module_for_activity(&env.kernel, LX_BUDGET_DELEGATE_SPEND,
                                         0U, &env.registration) == LXP_OK);
    return 0;
}

static int submit(const budget_call *call, lxp_effect_buffer *effects,
                  lxp_result *result)
{
    static uint8_t arena_bytes[262144];
    static uint8_t wire_bytes[LXP_MAX_ACTIVITY_BYTES];
    uint8_t payload_copy[256];
    uint8_t name[LX_ACCOUNT_NAME_MAX];
    uint8_t principal[32];
    uint8_t public_key[32];
    uint8_t signature[64];
    uint8_t digest[32];
    lxp_activity activity;
    lxp_authority_resolved authority;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lxp_arena wire_arena;
    lxp_byte_span wire;
    size_t did_length = strlen(call->did);
    size_t length = call->payload_length;
    CHECK(length <= sizeof(payload_copy));
    (void)memcpy(payload_copy, call->payload, length);
    CHECK(11U + did_length <= sizeof(name));
    (void)memcpy(name, "agent:", 6U);
    (void)memcpy(name + 6U, call->did, did_length);
    (void)memcpy(name + 6U + did_length, ":main", 5U);
    CHECK(lx_account_id_from_string(name, 11U + did_length,
                                    principal) == LXP_OK);
    CHECK(sign_raw(call->seed, payload_copy, 0U, signature, public_key) == 0);
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION_OCCUPANCY;
    activity.network_id = 7U;
    activity.activity_type = call->activity_type;
    activity.actor_did.bytes = (const uint8_t *)call->did;
    activity.actor_did.length = did_length;
    activity.authority.bytes = public_key;
    activity.authority.length = 32U;
    activity.signature.bytes = signature;
    activity.signature.length = 64U;
    activity.payload.bytes = payload_copy;
    activity.payload.length = length;
    activity.account_sequence = call->sequence;
    activity.idempotency_key[0] = (uint8_t)(env.global_sequence + 1U);
    activity.idempotency_key[1] = (uint8_t)call->activity_type;
    activity.fee_limit.lo = 1000000U;
    activity.timestamp_bound.not_after = call->timestamp + 1000U;
    CHECK(lxp_hash_payload(payload_copy, length,
                           activity.payload_hash) == LXP_OK);
    CHECK(lxp_activity_signing_preimage(&activity, digest) == LXP_OK);
    CHECK(sign_raw(call->seed, digest, 32U, signature, public_key) == 0);
    (void)memset(&authority, 0, sizeof(authority));
    authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.verified_key, public_key, 32U);
    CHECK(lxp_did_id_derive(activity.actor_did.bytes, did_length,
                            authority.actor) == LXP_OK);
    (void)memcpy(authority.principal, principal, 32U);
    CHECK(lxp_arena_init(&wire_arena, wire_bytes,
                         sizeof(wire_bytes)) == LXP_OK);
    CHECK(lxp_activity_encode(&activity, &wire_arena, &wire) == LXP_OK);
    CHECK(lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) == LXP_OK);
    CHECK(lxp_effect_buffer_init(effects) == LXP_OK);
    ++env.global_sequence;
    CHECK(lxp_module_ctx_init(&ctx, &env.kernel, LXP_MODULE_BUDGET,
                              call->timestamp, 0U, env.global_sequence,
                              1000000U, &arena, true) == LXP_OK);
    ctx.protocol_version = activity.protocol_version;
    ctx.batch_number = env.global_sequence;
    CHECK(lxp_activity_id(wire.bytes, wire.length, ctx.activity_id) == LXP_OK);
    CHECK(lxp_module_ctx_bind_effects(&ctx, effects) == LXP_OK);
    CHECK(lxp_kernel_dispatch(env.registration, &ctx, &activity, &authority,
                              effects, result) == LXP_OK);
    if (*result == LXP_OK) CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    return 0;
}

static void capability_key(const uint8_t budget_id[32],
                           const uint8_t holder[32],
                           uint8_t key[CAPABILITY_KEY_BYTES])
{
    (void)memcpy(key, "budget-delegate:", 16U);
    (void)memcpy(key + 16U, budget_id, 32U);
    (void)memcpy(key + 48U, holder, 32U);
}

static int seed_capability(const uint8_t budget_id[32], uint64_t per_spend,
                           uint64_t total)
{
    static uint8_t arena_bytes[4096];
    uint8_t key[CAPABILITY_KEY_BYTES];
    uint8_t bytes[CAPABILITY_BYTES];
    lxp_effect_buffer effects;
    lxp_module_ctx ctx;
    lxp_arena arena;
    (void)memset(bytes, 0, sizeof(bytes));
    bytes[1] = 1U;
    (void)memcpy(bytes + 2U, env.delegate_actor, 32U);
    (void)memcpy(bytes + 34U, env.asset.asset_id, 32U);
    (void)memcpy(bytes + 66U, env.recipient->id, 32U);
    bytes[98] = 0x55U;
    put_u128(bytes + 130U, 0U, per_spend);
    put_u128(bytes + 146U, 0U, total);
    put_u64(bytes + 178U, 1000000U);
    put_u64(bytes + 186U, 1U);
    capability_key(budget_id, env.delegate_actor, key);
    CHECK(lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) == LXP_OK);
    CHECK(lxp_effect_buffer_init(&effects) == LXP_OK);
    ++env.global_sequence;
    CHECK(lxp_module_ctx_init(&ctx, &env.kernel, LXP_MODULE_BUDGET, 400U, 0U,
                              env.global_sequence, 1000000U, &arena,
                              true) == LXP_OK);
    CHECK(lxp_module_ctx_bind_effects(&ctx, &effects) == LXP_OK);
    CHECK(lxp_ctx_kv_put(&ctx, key, sizeof(key), bytes,
                         sizeof(bytes)) == LXP_OK);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    return 0;
}

static int read_consumed(const uint8_t budget_id[32], uint64_t *consumed)
{
    uint8_t key[CAPABILITY_KEY_BYTES];
    uint8_t root[32];
    lxp_byte_span span;
    lxp_state_witness *proof = (lxp_state_witness *)malloc(sizeof(*proof));
    int failed;
    CHECK(proof != NULL);
    capability_key(budget_id, env.delegate_actor, key);
    span.bytes = key;
    span.length = sizeof(key);
    failed = lxp_state_root(&env.kernel, root) != LXP_OK ||
             lxp_state_proof_build(&env.kernel, LXP_MODULE_BUDGET, span,
                                   proof) != LXP_OK ||
             lxp_state_proof_verify(proof, root) != LXP_OK ||
             proof->key_length != (uint32_t)sizeof(key) ||
             memcmp(proof->key, key, sizeof(key)) != 0 ||
             proof->value_length != (uint32_t)CAPABILITY_BYTES ||
             memcmp(proof->value + 2U, env.delegate_actor, 32U) != 0 ||
             get_u64(proof->value + 162U) != 0U;
    if (!failed) *consumed = get_u64(proof->value + 170U);
    free(proof);
    CHECK(failed == 0);
    return 0;
}

static int expect_spent(const lxp_effect_buffer *effects,
                        const uint8_t budget_id[32], uint64_t amount)
{
    uint8_t body[80];
    (void)memcpy(body, budget_id, 32U);
    (void)memcpy(body + 32U, env.recipient->id, 32U);
    put_u128(body + 64U, 0U, amount);
    CHECK(effects->count == 1U);
    CHECK(effects->effects[0].module_id == LXP_MODULE_BUDGET);
    CHECK(effects->effects[0].kind == LXP_EFFECT_EVENT);
    CHECK(effects->effects[0].event_type == 6U);
    CHECK((size_t)effects->effects[0].body_length == sizeof(body));
    CHECK(memcmp(effects->effects[0].body, body, sizeof(body)) == 0);
    return 0;
}

static int unchanged(const uint8_t before[32])
{
    uint8_t after[32];
    if (lxp_state_root(&env.kernel, after) != LXP_OK) return 0;
    return memcmp(before, after, 32U) == 0;
}

static int owner_setup(const uint8_t budget_id[32], lx_account *account,
                       uint64_t amount)
{
    uint8_t create[LX_BUDGET_CREATE_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t add[LX_BUDGET_DELEGATE_PAYLOAD_BYTES] = {0U, 1U};
    lxp_effect_buffer effects;
    budget_call call;
    lxp_result result = LXP_OK;
    (void)memcpy(create + 2U, budget_id, 32U);
    (void)memcpy(create + 34U, account->id, 32U);
    (void)memcpy(create + 66U, env.asset.asset_id, 32U);
    create[98] = 0x55U;
    put_u128(create + 130U, 0U, 200U);
    put_u128(create + 146U, 0U, 50U);
    put_u128(create + 162U, 0U, amount);
    put_u64(create + 178U, 1000U);
    put_u64(create + 186U, 0U);
    put_u64(create + 194U, 1000000U);
    put_u64(create + 202U, 1U);
    create[210] = (uint8_t)LX_BUDGET_ROLLOVER_CAPPED;
    (void)memcpy(add + 2U, budget_id, 32U);
    (void)memcpy(add + 34U, env.delegate_actor, 32U);
    (void)memset(&call, 0, sizeof(call));
    call.did = owner_did;
    call.seed = owner_seed;
    call.timestamp = 400U;
    call.activity_type = LX_BUDGET_CREATE;
    call.payload = create;
    call.payload_length = sizeof(create);
    call.sequence = env.owner->next_sequence;
    CHECK(submit(&call, &effects, &result) == 0 && result == LXP_OK);
    CHECK(account->balance.lo == amount);
    call.activity_type = LX_BUDGET_DELEGATE_ADD;
    call.payload = add;
    call.payload_length = sizeof(add);
    call.sequence = env.owner->next_sequence;
    CHECK(submit(&call, &effects, &result) == 0 && result == LXP_OK);
    return 0;
}

static int delegate_spend(const uint8_t budget_id[32],
                          const uint8_t recipient[32], uint64_t amount,
                          uint64_t timestamp, lxp_effect_buffer *effects,
                          lxp_result *result)
{
    uint8_t payload[LX_BUDGET_DELEGATE_SPEND_PAYLOAD_BYTES] = {0U, 1U};
    budget_call call;
    (void)memcpy(payload + 2U, budget_id, 32U);
    (void)memcpy(payload + 34U, recipient, 32U);
    put_u128(payload + 66U, 0U, amount);
    payload[82] = 0xA5U;
    payload[113] = (uint8_t)env.global_sequence;
    (void)memset(&call, 0, sizeof(call));
    call.did = delegate_did;
    call.seed = delegate_seed;
    call.timestamp = timestamp;
    call.activity_type = LX_BUDGET_DELEGATE_SPEND;
    call.payload = payload;
    call.payload_length = sizeof(payload);
    call.sequence = env.delegate->next_sequence;
    return submit(&call, effects, result);
}

static int test_delegate_spend_activity(void)
{
    lxp_effect_buffer effects;
    lxp_result result = LXP_OK;
    uint64_t consumed = 0U;
    uint64_t received = env.recipient->balance.lo;
    uint8_t before[32];
    CHECK(owner_setup(budget_three, env.budget_three, 30U) == 0);
    CHECK(seed_capability(budget_three, 10U, 10U) == 0);
    CHECK(delegate_spend(budget_three, env.recipient->id, 10U, 700U,
                         &effects, &result) == 0);
    CHECK(read_consumed(budget_three, &consumed) == 0);
    CASE("delegate-spend-activity",
         LX_BUDGET_DELEGATE_SPEND == 0x0003000a &&
         env.registration->module_id == LXP_MODULE_BUDGET &&
         env.registration->activity_types[9] == LX_BUDGET_DELEGATE_SPEND &&
         result == LXP_OK && consumed == 10U &&
         env.recipient->balance.hi == 0U &&
         env.recipient->balance.lo == received + 10U &&
         env.budget_three->balance.lo == 20U &&
         expect_spent(&effects, budget_three, 10U) == 0);
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_three, env.recipient->id, 1U, 710U,
                         &effects, &result) == 0);
    CASE("delegate-spend-activity-right-used",
         result == LXP_ERR_UNAUTHORIZED_DELEGATE && unchanged(before) &&
         env.recipient->balance.lo == received + 10U &&
         env.budget_three->balance.lo == 20U && effects.count == 0U);
    return 0;
}

static int decode_vectors(void)
{
    static const uint8_t seed[32] = { 9U };
    static uint8_t arena_bytes[8192];
    const lxp_module_iface *iface = lx_budget_module_iface();
    uint8_t spend[LX_BUDGET_DELEGATE_SPEND_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t pull[LX_BUDGET_PULL_PAYLOAD_BYTES];
    lxp_payer_grant grant;
    lxp_module_ctx ctx;
    lxp_arena arena;
    size_t grant_length = 0U;
    void *decoded = NULL;
    CASE("activity-list", iface->activity_type_count == 11U &&
         iface->activity_types[8] == LX_BUDGET_REVOKE &&
         iface->activity_types[9] == LX_BUDGET_DELEGATE_SPEND &&
         iface->activity_types[10] == LX_BUDGET_PULL &&
         LX_BUDGET_DELEGATE_SPEND == 0x0003000a &&
         LX_BUDGET_PULL == 0x0003000b);
    CHECK(lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) == LXP_OK);
    CHECK(lxp_module_ctx_init(&ctx, &env.kernel, LXP_MODULE_BUDGET, 400U, 0U,
                              env.global_sequence + 1U, 1000000U, &arena,
                              true) == LXP_OK);
    (void)memcpy(spend + 2U, budget_one, 32U);
    (void)memcpy(spend + 34U, env.recipient->id, 32U);
    put_u128(spend + 66U, 0U, 25U);
    spend[82] = 0xA5U;
    CASE("decode-delegate-spend",
         iface->decode(&ctx, 10U, spend, sizeof(spend), &decoded) == LXP_OK &&
         decoded != NULL);
    CASE("decode-delegate-spend-length",
         iface->decode(&ctx, 10U, spend, sizeof(spend) - 1U, &decoded) ==
             LXP_ERR_NON_CANONICAL);
    spend[1] = 2U;
    CASE("decode-delegate-spend-version",
         iface->decode(&ctx, 10U, spend, sizeof(spend), &decoded) ==
             LXP_ERR_NON_CANONICAL);
    spend[1] = 1U;
    spend[82] = 0U;
    CASE("decode-delegate-spend-zero-key",
         iface->decode(&ctx, 10U, spend, sizeof(spend), &decoded) ==
             LXP_ERR_NON_CANONICAL);
    spend[82] = 0xA5U;
    put_u128(spend + 66U, 0U, 0U);
    CASE("decode-delegate-spend-zero-amount",
         iface->decode(&ctx, 10U, spend, sizeof(spend), &decoded) ==
             LXP_ERR_INVALID_AMOUNT);
    put_u128(spend + 66U, 0U, 25U);

    (void)memset(&grant, 0, sizeof(grant));
    (void)memcpy(grant.from, env.owner->id, 32U);
    (void)memcpy(grant.recipient, env.recipient->id, 32U);
    (void)memcpy(grant.asset, env.asset.asset_id, 32U);
    grant.per_draw_maximum = (lxp_u128){ 0U, 10U };
    grant.allowance = (lxp_u128){ 0U, 20U };
    grant.expiration = 200U;
    grant.revocation_sequence = 1U;
    grant.purpose_hash[0] = 0x55U;
    CHECK(sign_grant(&grant, seed) == 0);
    (void)memcpy(pull, spend, sizeof(spend));
    CHECK(lxp_payer_grant_encode(&grant, pull + sizeof(spend),
                                 sizeof(pull) - sizeof(spend),
                                 &grant_length) == LXP_OK);
    CHECK(grant_length == sizeof(pull) - sizeof(spend));
    CASE("decode-pull",
         iface->decode(&ctx, 11U, pull, sizeof(pull), &decoded) == LXP_OK);
    CASE("decode-pull-length",
         iface->decode(&ctx, 11U, pull, sizeof(pull) - 1U, &decoded) ==
             LXP_ERR_NON_CANONICAL);
    pull[1] = 2U;
    CASE("decode-pull-version",
         iface->decode(&ctx, 11U, pull, sizeof(pull), &decoded) ==
             LXP_ERR_NON_CANONICAL);
    return 0;
}

static int execute_vectors(void)
{
    uint8_t before[32];
    uint8_t fund[LX_BUDGET_FUND_PAYLOAD_BYTES] = {0U, 1U};
    lxp_effect_buffer effects;
    budget_call call;
    lxp_result result = LXP_OK;
    uint64_t consumed = 0U;

    CHECK(env_init() == 0);
    CHECK(decode_vectors() == 0);
    CHECK(owner_setup(budget_one, env.budget_one, 300U) == 0);
    CHECK(owner_setup(budget_two, env.budget_two, 60U) == 0);

    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_one, env.recipient->id, 25U, 500U, &effects,
                         &result) == 0);
    CASE("delegate-spend-no-capability",
         result == LXP_ERR_UNAUTHORIZED_DELEGATE && unchanged(before));
    CHECK(seed_capability(budget_one, 50U, 120U) == 0);
    CHECK(seed_capability(budget_two, 50U, 120U) == 0);

    /* Per-spend and total caps (maximum_per_spend 50, maximum_total 120). */
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_one, env.recipient->id, 60U, 500U, &effects,
                         &result) == 0);
    CASE("delegate-spend-per-spend-cap",
         result == LXP_ERR_UNAUTHORIZED_DELEGATE && unchanged(before) &&
         env.budget_one->balance.lo == 300U);
    CHECK(delegate_spend(budget_one, env.recipient->id, 50U, 500U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_one, &consumed) == 0);
    CASE("delegate-spend-authorized",
         result == LXP_OK && env.budget_one->balance.lo == 250U &&
         env.recipient->balance.lo == 50U && consumed == 50U &&
         expect_spent(&effects, budget_one, 50U) == 0);
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_one, env.other->id, 10U, 510U, &effects,
                         &result) == 0);
    CASE("delegate-spend-wrong-recipient",
         result == LXP_ERR_UNAUTHORIZED_DELEGATE && unchanged(before) &&
         env.other->balance.lo == 0U && effects.count == 0U);
    CHECK(delegate_spend(budget_one, env.recipient->id, 50U, 520U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_one, &consumed) == 0);
    CASE("delegate-spend-second", result == LXP_OK && consumed == 100U &&
         env.budget_one->balance.lo == 200U &&
         expect_spent(&effects, budget_one, 50U) == 0);
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_one, env.recipient->id, 30U, 530U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_one, &consumed) == 0);
    CASE("delegate-spend-total-cap",
         result == LXP_ERR_UNAUTHORIZED_DELEGATE && unchanged(before) &&
         consumed == 100U && env.budget_one->balance.lo == 200U);

    /* A failed spend leaves consumed as it was before the activity. */
    CHECK(delegate_spend(budget_two, env.recipient->id, 50U, 500U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_two, &consumed) == 0);
    CHECK(result == LXP_OK && consumed == 50U &&
          env.budget_two->balance.lo == 10U);
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(delegate_spend(budget_two, env.recipient->id, 40U, 510U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_two, &consumed) == 0);
    CASE("delegate-spend-rollback",
         result == LXP_ERR_INSUFFICIENT_BUDGET_FUNDS && unchanged(before) &&
         consumed == 50U && env.budget_two->balance.lo == 10U &&
         effects.count == 0U);

    /* Consumed accumulates across activities in separate batches. */
    (void)memcpy(fund + 2U, budget_two, 32U);
    put_u128(fund + 34U, 0U, 40U);
    (void)memset(&call, 0, sizeof(call));
    call.did = owner_did;
    call.seed = owner_seed;
    call.timestamp = 550U;
    call.activity_type = LX_BUDGET_FUND;
    call.payload = fund;
    call.payload_length = sizeof(fund);
    call.sequence = env.owner->next_sequence;
    CHECK(submit(&call, &effects, &result) == 0 && result == LXP_OK);
    CHECK(env.budget_two->balance.lo == 50U);
    CHECK(delegate_spend(budget_two, env.recipient->id, 40U, 600U, &effects,
                         &result) == 0);
    CHECK(read_consumed(budget_two, &consumed) == 0);
    CASE("delegate-spend-accumulates",
         result == LXP_OK && consumed == 90U &&
         env.budget_two->balance.lo == 10U &&
         env.recipient->balance.lo == 190U &&
         expect_spent(&effects, budget_two, 40U) == 0);
    CHECK(test_delegate_spend_activity() == 0);
    CHECK(lxp_state_store_destroy(&env.state) == LXP_OK);
    return 0;
}

int main(void)
{
    if (library_vectors() != 0) {
        printf("BUDGET_CASE library-vectors fail\n");
        return 1;
    }
    printf("BUDGET_CASE library-vectors ok\n");
    ++cases;
    if (test_pull_activity() != 0) return 1;
    if (execute_vectors() != 0) return 1;
    printf("BUDGET_DELEGATE cases=%u skipped=0\n", cases);
    return 0;
}
