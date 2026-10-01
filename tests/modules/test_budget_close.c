#include "layerx/lx_asset.h"
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

static size_t transfer_calls;

static lxp_result apply_capability(lxp_kernel *kernel,
                                   const lxp_transfer_set *set,
                                   lxp_receipt *receipt)
{
    lxp_transfer_set_result result;
    lxp_transfer_context context = set->context;
    lxp_result status;
    (void)kernel;
    ++transfer_calls;
    status = lxp_apply_transfer_set((lxp_transfer_leg *)set->legs,
                                    set->leg_count, &context, &result);
    if (status == LXP_OK)
        (void)memcpy(receipt->transfer_set_root, result.transfer_set_root, 32U);
    return status;
}

static int helper_path(void)
{
    lx_account budget_account;
    lx_account owner;
    lx_account recipient;
    lx_asset_record asset;
    lxp_transfer_asset_state asset_state;
    lx_budget_store store;
    lx_budget_spend_request spend;
    lx_budget_close_request close_request;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_arena arena;
    lxp_receipt receipt;
    uint8_t arena_bytes[4096];
    uint64_t parameters = 1U;
    lxp_transfer_leg direct;
    lxp_transfer_context direct_context;
    lxp_transfer_result direct_result;

    (void)memset(&budget_account, 0, sizeof(budget_account));
    (void)memset(&owner, 0, sizeof(owner));
    (void)memset(&recipient, 0, sizeof(recipient));
    (void)memset(&asset, 0, sizeof(asset));
    (void)memset(&store, 0, sizeof(store));
    budget_account.id[0] = 1U; budget_account.kind = LX_ACCOUNT_AGENT_BUDGET;
    owner.id[0] = 2U; owner.kind = LX_ACCOUNT_AGENT_MAIN;
    recipient.id[0] = 3U; recipient.kind = LX_ACCOUNT_AGENT_MAIN;
    asset.asset_id[0] = 4U;
    if (lxp_ledger_bootstrap_balance(&budget_account, asset.asset_id,
                                     (lxp_u128){ 0U, 100U }, 0U) != LXP_OK ||
        lxp_ledger_bootstrap_balance(&owner, asset.asset_id,
                                     (lxp_u128){ 0U, 0U }, 0U) != LXP_OK ||
        lxp_ledger_bootstrap_balance(&recipient, asset.asset_id,
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
    store.records[0].budget_id[0] = 5U;
    (void)memcpy(store.records[0].owner, owner.id, 32U);
    (void)memcpy(store.records[0].budget_account, budget_account.id, 32U);
    (void)memcpy(store.records[0].asset_id, asset.asset_id, 32U);
    store.records[0].per_period_limit = (lxp_u128){ 0U, 100U };
    store.records[0].configured_period_limit = (lxp_u128){ 0U, 100U };
    store.records[0].period_start = 1U;
    store.records[0].period_length = 1000U;
    store.records[0].expiry = 1000U;
    store.records[0].revocation_sequence = 1U;
    (void)memset(&spend, 0, sizeof(spend));
    spend.store = &store;
    spend.budget_id = store.records[0].budget_id;
    spend.budget_account = &budget_account;
    spend.recipient = &recipient;
    spend.asset = &asset;
    spend.amount = (lxp_u128){ 0U, 20U };
    spend.context.assets = &asset_state;
    spend.context.asset_count = 1U;
    spend.context.sequence_account = &budget_account;
    (void)memcpy(spend.context.authorized_from, budget_account.id, 32U);
    if (lx_budget_spend_execute(&ctx, &spend, &receipt) != LXP_OK ||
        recipient.balance.lo != 20U || budget_account.balance.lo != 80U)
        return 1;

    (void)memset(&direct, 0, sizeof(direct));
    (void)memset(&direct_context, 0, sizeof(direct_context));
    direct.from = &budget_account;
    direct.to = &owner;
    (void)memcpy(direct.asset_id, asset.asset_id, 32U);
    direct.amount = (lxp_u128){ 0U, 1U };
    direct.reason = LXP_REASON_PAYMENT;
    direct_context.assets = &asset_state;
    direct_context.asset_count = 1U;
    direct_context.sequence_account = &budget_account;
    direct_context.actor_sequence = budget_account.next_sequence;
    direct_context.debit_authority_kind = LXP_AUTH_OWNER;
    (void)memcpy(direct_context.authorized_from, budget_account.id, 32U);
    if (lxp_apply_transfer(&direct, &direct_context, &direct_result) !=
        LXP_ERR_UNAUTHORIZED_DEBIT) return 1;

    (void)memset(&close_request, 0, sizeof(close_request));
    close_request.store = &store;
    close_request.budget_id = store.records[0].budget_id;
    close_request.budget_account = &budget_account;
    close_request.owner = &owner;
    close_request.asset = &asset;
    close_request.amount = (lxp_u128){ 0U, 30U };
    close_request.context.assets = &asset_state;
    close_request.context.asset_count = 1U;
    close_request.context.sequence_account = &budget_account;
    close_request.context.actor_sequence = budget_account.next_sequence;
    (void)memcpy(close_request.context.authorized_from,
                 budget_account.id, 32U);
    if (lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_BUDGET, 100U, 0U, 2U,
                            1000U, &arena, true) != LXP_OK ||
        lx_budget_defund_execute(&ctx, &close_request, &receipt) != LXP_OK ||
        budget_account.balance.lo != 50U || owner.balance.lo != 30U ||
        store.records[0].per_period_limit.lo != 70U)
        return 1;
    close_request.context.actor_sequence = budget_account.next_sequence;
    close_request.revocation_sequence = 1U;
    if (lx_budget_revoke_execute(&ctx, &close_request, &receipt) !=
        LXP_ERR_STALE_REVOCATION) return 1;
    close_request.revocation_sequence = 2U;
    if (lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_BUDGET, 100U, 0U, 3U,
                            1000U, &arena, true) != LXP_OK ||
        lx_budget_revoke_execute(&ctx, &close_request, &receipt) != LXP_OK ||
        budget_account.balance.lo != 0U || owner.balance.lo != 80U ||
        !store.records[0].revoked || transfer_calls != 3U)
        return 1;
    spend.context.actor_sequence = budget_account.next_sequence;
    spend.amount = (lxp_u128){ 0U, 1U };
    if (lx_budget_spend_execute(&ctx, &spend, &receipt) !=
            LXP_ERR_BUDGET_REVOKED || recipient.balance.lo != 20U)
        return 1;

    store.records[0].revoked = false;
    store.records[0].closed = false;
    close_request.revocation_sequence = 3U;
    close_request.context.actor_sequence = budget_account.next_sequence;
    if (lx_budget_close_execute(&ctx, &close_request, &receipt) != LXP_OK ||
        transfer_calls != 3U || !store.records[0].closed ||
        lxp_state_store_destroy(&state) != LXP_OK)
        return 1;
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

enum {
    BUDGET_TAMPER_NONE = 0,
    BUDGET_TAMPER_SIGNATURE = 1,
    BUDGET_TAMPER_PAYLOAD = 2,
    BUDGET_TAMPER_AUTHORITY_KIND = 3,
    BUDGET_TAMPER_VERIFIED_KEY = 4,
    BUDGET_TAMPER_ACTOR = 5,
    BUDGET_TAMPER_TRUNCATE = 6
};

static const char owner_did[] = "did:key:owner";
static const char delegate_did[] = "did:key:dele";
static const char owner_name[] = "agent:did:key:owner:main";
static const char budget_name[] = "agent:did:key:owner:budget:b1";
static const char recipient_name[] = "agent:did:key:recv:main";
static const char delegate_name[] = "agent:did:key:dele:main";
static const char foreign_name[] = "agent:did:key:recv:budget:x";
static const uint8_t owner_seed[32] = { 11U };
static const uint8_t delegate_seed[32] = { 22U };

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
    lx_account *budget_account;
    lx_account *recipient;
    lx_account *delegate;
    lx_account *foreign;
    uint64_t parameters;
    uint64_t global_sequence;
} budget_env;

typedef struct budget_call {
    uint16_t protocol_version;
    uint32_t activity_type;
    const char *did;
    const uint8_t *seed;
    const uint8_t *payload;
    size_t payload_length;
    uint64_t sequence;
    const uint8_t *principal;
    uint64_t timestamp;
    unsigned tamper;
} budget_call;

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

static budget_env env;

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
    CHECK(lx_asset_transfer_state(&env.asset, &env.asset_state) == LXP_OK);
    CHECK(lx_account_registry_init(&env.accounts) == LXP_OK);
    CHECK(open_account(owner_name, 1000U, owner_key, &env.owner) == 0);
    CHECK(open_account(budget_name, 0U, NULL, &env.budget_account) == 0);
    CHECK(open_account(recipient_name, 0U, NULL, &env.recipient) == 0);
    CHECK(open_account(delegate_name, 0U, delegate_key, &env.delegate) == 0);
    CHECK(open_account(foreign_name, 0U, NULL, &env.foreign) == 0);
    CHECK(env.owner->kind == LX_ACCOUNT_AGENT_MAIN);
    CHECK(env.budget_account->kind == LX_ACCOUNT_AGENT_BUDGET);
    CHECK(env.foreign->kind == LX_ACCOUNT_AGENT_BUDGET);
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
    CHECK(lxp_kernel_module_for_activity(&env.kernel, LX_BUDGET_CREATE, 0U,
                                         &env.registration) == LXP_OK);
    return 0;
}

static int submit(const budget_call *call, lxp_effect_buffer *effects,
                  lxp_result *result)
{
    static uint8_t arena_bytes[262144];
    static uint8_t native_arena_bytes[LXP_MAX_ACTIVITY_BYTES + sizeof(arena_bytes)];
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
    if (call->tamper == BUDGET_TAMPER_TRUNCATE) {
        CHECK(length != 0U);
        --length;
    }
    if (call->principal != NULL) {
        (void)memcpy(principal, call->principal, 32U);
    } else {
        CHECK(11U + did_length <= sizeof(name));
        (void)memcpy(name, "agent:", 6U);
        (void)memcpy(name + 6U, call->did, did_length);
        (void)memcpy(name + 6U + did_length, ":main", 5U);
        CHECK(lx_account_id_from_string(name, 11U + did_length,
                                        principal) == LXP_OK);
    }
    CHECK(sign_raw(call->seed, payload_copy, 0U, signature, public_key) == 0);
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = call->protocol_version == 0U ? LXP_PROTOCOL_VERSION_OCCUPANCY : call->protocol_version;
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
    if (call->tamper == BUDGET_TAMPER_SIGNATURE) signature[0] ^= 1U;
    if (call->tamper == BUDGET_TAMPER_PAYLOAD) {
        CHECK(length != 0U);
        payload_copy[length - 1U] ^= 1U;
    }
    (void)memset(&authority, 0, sizeof(authority));
    authority.kind = call->tamper == BUDGET_TAMPER_AUTHORITY_KIND ?
        LXP_AUTHORITY_SESSION_KEY : LXP_AUTHORITY_OWNER;
    (void)memcpy(authority.verified_key, public_key, 32U);
    if (call->tamper == BUDGET_TAMPER_VERIFIED_KEY)
        authority.verified_key[0] ^= 1U;
    CHECK(lxp_did_id_derive(activity.actor_did.bytes, did_length,
                            authority.actor) == LXP_OK);
    if (call->tamper == BUDGET_TAMPER_ACTOR) authority.actor[0] ^= 1U;
    (void)memcpy(authority.principal, principal, 32U);
    CHECK(lxp_arena_init(&wire_arena, wire_bytes,
                         sizeof(wire_bytes)) == LXP_OK);
    CHECK(lxp_activity_encode(&activity, &wire_arena, &wire) == LXP_OK);
    CHECK(lxp_arena_init(&arena, call->protocol_version == 3U ? native_arena_bytes : arena_bytes,
        call->protocol_version == 3U ? sizeof(native_arena_bytes) : sizeof(arena_bytes)) == LXP_OK);
    CHECK(lxp_effect_buffer_init(effects) == LXP_OK);
    ++env.global_sequence;
    if (activity.protocol_version == 3U)
        CHECK(lxp_state_journal_open(&env.state, env.global_sequence, &env.journal) == LXP_OK);
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
    if (activity.protocol_version == 3U)
        CHECK(lxp_state_journal_commit(&env.journal) == LXP_OK);
    return 0;
}

static int read_record(const uint8_t budget_id[32], lx_budget_record *record)
{
    uint8_t key[LX_BUDGET_STATE_KEY_BYTES];
    uint8_t root[32];
    lxp_byte_span span;
    lxp_state_witness *proof = (lxp_state_witness *)malloc(sizeof(*proof));
    int failed;
    CHECK(proof != NULL);
    span.bytes = key;
    span.length = sizeof(key);
    failed = lx_budget_state_key(budget_id, key) != LXP_OK ||
             lxp_state_root(&env.kernel, root) != LXP_OK ||
             lxp_state_proof_build(&env.kernel, LXP_MODULE_BUDGET, span,
                                   proof) != LXP_OK ||
             lxp_state_proof_verify(proof, root) != LXP_OK ||
             proof->key_length != (uint32_t)sizeof(key) ||
             memcmp(proof->key, key, sizeof(key)) != 0 ||
             lx_budget_record_decode(proof->value, proof->value_length,
                                     record) != LXP_OK;
    free(proof);
    CHECK(failed == 0);
    return 0;
}

static int expect_event(const lxp_effect_buffer *effects, uint16_t event_type,
                        const uint8_t *body, size_t body_length)
{
    CHECK(effects->count == 1U);
    CHECK(effects->effects[0].module_id == LXP_MODULE_BUDGET);
    CHECK(effects->effects[0].kind == LXP_EFFECT_EVENT);
    CHECK(effects->effects[0].event_type == event_type);
    CHECK((size_t)effects->effects[0].body_length == body_length);
    CHECK(memcmp(effects->effects[0].body, body, body_length) == 0);
    return 0;
}

typedef struct replica_result {
    uint8_t root[32];
    uint8_t record[LX_BUDGET_RECORD_MAX_BYTES];
    size_t record_length;
    uint64_t owner_balance;
    uint64_t budget_balance;
} replica_result;

static int unchanged(const uint8_t before[32])
{
    uint8_t after[32];
    CHECK(lxp_state_root(&env.kernel, after) == LXP_OK);
    return memcmp(before, after, 32U) == 0;
}

static int lifecycle_replica(int report, replica_result *out)
{
    static const uint8_t budget_id[32] = { 0x0DU, 0xEFU, 0x01U };
    uint8_t create[LX_BUDGET_CREATE_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t spend[LX_BUDGET_SPEND_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t fund[LX_BUDGET_FUND_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t defund[LX_BUDGET_DEFUND_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t revoke[LX_BUDGET_REVOKE_PAYLOAD_BYTES] = {0U, 1U};
    uint8_t expected[64];
    uint8_t before[32];
    lx_budget_record record;
    lx_budget_record decoded;
    lxp_effect_buffer effects;
    budget_call call;
    budget_call other;
    lxp_result result = LXP_OK;
    unsigned before_cases = cases;
#define LCASE(name, x) do { if (report) CASE(name, x); else CHECK(x); } while (0)

    CHECK(env_init() == 0);
    (void)memcpy(create + 2U, budget_id, 32U);
    (void)memcpy(create + 34U, env.budget_account->id, 32U);
    (void)memcpy(create + 66U, env.asset.asset_id, 32U);
    create[98] = 0x55U;
    put_u128(create + 130U, 0U, 200U);
    put_u128(create + 146U, 0U, 50U);
    put_u128(create + 162U, 0U, 300U);
    put_u64(create + 178U, 1000U);
    put_u64(create + 186U, 0U);
    put_u64(create + 194U, 1000000U);
    put_u64(create + 202U, 1U);
    create[210] = (uint8_t)LX_BUDGET_ROLLOVER_CAPPED;
    (void)memcpy(spend + 2U, budget_id, 32U);
    (void)memcpy(spend + 34U, env.recipient->id, 32U);
    (void)memcpy(fund + 2U, budget_id, 32U);
    put_u128(fund + 34U, 0U, 1U);
    (void)memcpy(defund + 2U, budget_id, 32U);
    (void)memcpy(revoke + 2U, budget_id, 32U);

    (void)memset(&call, 0, sizeof(call));
    call.did = owner_did;
    call.seed = owner_seed;
    call.timestamp = 500U;
    call.activity_type = LX_BUDGET_CREATE;
    call.payload = create;
    call.payload_length = sizeof(create);
    CHECK(submit(&call, &effects, &result) == 0 && result == LXP_OK);
    CHECK(env.owner->balance.lo == 700U && env.budget_account->balance.lo == 300U);

    put_u128(spend + 66U, 0U, 120U);
    call.activity_type = LX_BUDGET_SPEND;
    call.payload = spend;
    call.payload_length = sizeof(spend);
    call.sequence = env.owner->next_sequence;
    CHECK(submit(&call, &effects, &result) == 0 && result == LXP_OK);
    CHECK(env.budget_account->balance.lo == 180U && env.recipient->balance.lo == 120U);

    /* Unauthorized defund and revoke by a signed non-owner. */
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    other = call;
    other.did = delegate_did;
    other.seed = delegate_seed;
    other.sequence = env.delegate->next_sequence;
    other.activity_type = LX_BUDGET_DEFUND;
    other.payload = defund;
    other.payload_length = sizeof(defund);
    put_u128(defund + 34U, 0U, 10U);
    CHECK(submit(&other, &effects, &result) == 0);
    LCASE("defund-unauthorized", result == LXP_ERR_UNAUTHORIZED_DEBIT &&
          unchanged(before) && env.budget_account->balance.lo == 180U);
    other.activity_type = LX_BUDGET_REVOKE;
    other.payload = revoke;
    other.payload_length = sizeof(revoke);
    put_u64(revoke + 34U, 2U);
    CHECK(submit(&other, &effects, &result) == 0);
    LCASE("revoke-unauthorized", result == LXP_ERR_UNAUTHORIZED_DEBIT &&
          unchanged(before) && env.budget_account->balance.lo == 180U);

    /* Excessive and maximum-width amounts refuse without partial state. */
    call.activity_type = LX_BUDGET_DEFUND;
    call.payload = defund;
    call.payload_length = sizeof(defund);
    call.sequence = env.owner->next_sequence;
    put_u128(defund + 34U, 0U, 181U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-overdraw", result == LXP_ERR_INSUFFICIENT_BUDGET_FUNDS &&
          unchanged(before) && env.owner->balance.lo == 700U);
    put_u128(defund + 34U, UINT64_MAX, UINT64_MAX);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-u128-max", result == LXP_ERR_INSUFFICIENT_BUDGET_FUNDS &&
          unchanged(before));
    put_u128(defund + 34U, 0U, 0U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-zero", result == LXP_ERR_INVALID_AMOUNT && unchanged(before));

    /* CLOSE stays a distinct operation: a defund payload is not a CLOSE. */
    put_u128(defund + 34U, 0U, 150U);
    call.activity_type = LX_BUDGET_CLOSE;
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("close-distinct-payload", result == LXP_ERR_NON_CANONICAL &&
          unchanged(before));
    call.activity_type = LX_BUDGET_DEFUND;
    call.payload = revoke;
    call.payload_length = sizeof(revoke);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-rejects-revoke-payload", result == LXP_ERR_NON_CANONICAL &&
          unchanged(before));

    /* Partial defund: exact amount, budget retained, allowance clamped. */
    call.payload = defund;
    call.payload_length = sizeof(defund);
    CHECK(submit(&call, &effects, &result) == 0);
    CHECK(read_record(budget_id, &record) == 0);
    (void)memcpy(expected, budget_id, 32U);
    put_u128(expected + 32U, 0U, 150U);
    put_u128(expected + 48U, 0U, 150U);
    LCASE("defund-partial", result == LXP_OK &&
          env.budget_account->balance.lo == 30U &&
          env.owner->balance.lo == 850U && !record.closed && !record.revoked &&
          record.per_period_limit.lo == 150U &&
          record.spent_this_period.lo == 120U &&
          record.configured_period_limit.lo == 200U &&
          record.revocation_sequence == 1U &&
          expect_event(&effects, 8U, expected, 64U) == 0);

    /* Replaying the same signed defund refunds nothing a second time. */
    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-duplicate", result == LXP_ERR_SEQUENCE_REUSED &&
          unchanged(before) && env.owner->balance.lo == 850U &&
          env.budget_account->balance.lo == 30U);

    put_u128(spend + 66U, 0U, 31U);
    call.activity_type = LX_BUDGET_SPEND;
    call.payload = spend;
    call.payload_length = sizeof(spend);
    call.sequence = env.owner->next_sequence;
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("spend-after-defund-clamped",
          result == LXP_ERR_BUDGET_ALLOWANCE_EXCEEDED && unchanged(before));

    call.activity_type = LX_BUDGET_REVOKE;
    call.payload = revoke;
    call.payload_length = sizeof(revoke);
    put_u64(revoke + 34U, 1U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("revoke-stale-sequence", result == LXP_ERR_STALE_REVOCATION &&
          unchanged(before) && env.budget_account->balance.lo == 30U);

    /* Period rollover, then spend within the clamped balance. */
    call.timestamp = 1500U;
    put_u128(spend + 66U, 0U, 10U);
    call.activity_type = LX_BUDGET_SPEND;
    call.payload = spend;
    call.payload_length = sizeof(spend);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("spend-after-rollover", result == LXP_OK &&
          env.budget_account->balance.lo == 20U &&
          env.recipient->balance.lo == 130U);

    /* Revoke refunds everything, persists revoked state and sequence. */
    call.timestamp = 1600U;
    call.activity_type = LX_BUDGET_REVOKE;
    call.payload = revoke;
    call.payload_length = sizeof(revoke);
    call.sequence = env.owner->next_sequence;
    put_u64(revoke + 34U, 2U);
    CHECK(submit(&call, &effects, &result) == 0);
    CHECK(read_record(budget_id, &record) == 0);
    (void)memcpy(expected, budget_id, 32U);
    put_u128(expected + 32U, 0U, 20U);
    put_u64(expected + 48U, 2U);
    LCASE("revoke", result == LXP_OK && env.budget_account->balance.lo == 0U &&
          env.owner->balance.lo == 870U && record.revoked && !record.closed &&
          record.revocation_sequence == 2U &&
          lxp_u128_cmp(record.per_period_limit, record.spent_this_period) == 0 &&
          expect_event(&effects, 9U, expected, 56U) == 0);

    CHECK(lxp_state_root(&env.kernel, before) == LXP_OK);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("revoke-duplicate", result == LXP_ERR_SEQUENCE_REUSED &&
          unchanged(before) && env.owner->balance.lo == 870U);
    call.sequence = env.owner->next_sequence;
    put_u64(revoke + 34U, 3U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("revoke-after-revoke", result == LXP_ERR_BUDGET_REVOKED &&
          unchanged(before));
    call.activity_type = LX_BUDGET_SPEND;
    call.payload = spend;
    call.payload_length = sizeof(spend);
    put_u128(spend + 66U, 0U, 1U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("spend-after-revoke", result == LXP_ERR_BUDGET_REVOKED &&
          unchanged(before) && env.recipient->balance.lo == 130U);
    call.activity_type = LX_BUDGET_DEFUND;
    call.payload = defund;
    call.payload_length = sizeof(defund);
    put_u128(defund + 34U, 0U, 1U);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("defund-after-revoke", result == LXP_ERR_BUDGET_REVOKED &&
          unchanged(before));
    call.activity_type = LX_BUDGET_FUND;
    call.payload = fund;
    call.payload_length = sizeof(fund);
    CHECK(submit(&call, &effects, &result) == 0);
    LCASE("fund-after-revoke", result == LXP_ERR_BUDGET_REVOKED &&
          unchanged(before) && env.owner->balance.lo == 870U);

    /* The committed record decodes back to the same persisted state. */
    CHECK(read_record(budget_id, &record) == 0);
    CHECK(lx_budget_record_encode(&record, out->record, sizeof(out->record),
                                  &out->record_length) == LXP_OK);
    LCASE("record-durable-roundtrip",
          lx_budget_record_decode(out->record, out->record_length,
                                  &decoded) == LXP_OK &&
          decoded.revoked && decoded.revocation_sequence == 2U &&
          lxp_u128_cmp(decoded.spent_this_period,
                       record.spent_this_period) == 0 &&
          lxp_u128_cmp(decoded.per_period_limit,
                       record.per_period_limit) == 0);
    CHECK(lxp_state_root(&env.kernel, out->root) == LXP_OK);
    out->owner_balance = env.owner->balance.lo;
    out->budget_balance = env.budget_account->balance.lo;
    CHECK(lxp_state_store_destroy(&env.state) == LXP_OK);
    if (!report) cases = before_cases;
#undef LCASE
    return 0;
}

static int replica_replay(void)
{
    replica_result first;
    replica_result second;
    (void)memset(&first, 0, sizeof(first));
    (void)memset(&second, 0, sizeof(second));
    if (lifecycle_replica(1, &first) != 0) return 1;
    if (lifecycle_replica(0, &second) != 0) return 1;
    CASE("replica-replay-identical",
         memcmp(first.root, second.root, 32U) == 0 &&
         first.record_length == second.record_length &&
         memcmp(first.record, second.record, first.record_length) == 0 &&
         first.owner_balance == second.owner_balance &&
         first.budget_balance == second.budget_balance);
    return 0;
}

static int codec_cases(void)
{
    uint8_t bytes[LX_BUDGET_DEFUND_PAYLOAD_BYTES] = {0U, 1U, 7U};
    lx_budget_defund_payload defund;
    lx_budget_revoke_payload revoke;
    put_u128(bytes + 34U, 0U, 5U);
    CASE("codec-defund", lx_budget_defund_decode(bytes, sizeof(bytes),
                                                 &defund) == LXP_OK &&
         defund.amount.lo == 5U && defund.budget_id[0] == 7U);
    CASE("codec-defund-length",
         lx_budget_defund_decode(bytes, sizeof(bytes) - 1U, &defund) ==
             LXP_ERR_NON_CANONICAL);
    bytes[1] = 2U;
    CASE("codec-defund-version",
         lx_budget_defund_decode(bytes, sizeof(bytes), &defund) ==
             LXP_ERR_NON_CANONICAL);
    bytes[1] = 1U;
    put_u64(bytes + 34U, 4U);
    CASE("codec-revoke", lx_budget_revoke_decode(
             bytes, LX_BUDGET_REVOKE_PAYLOAD_BYTES, &revoke) == LXP_OK &&
         revoke.revocation_sequence == 4U);
    put_u64(bytes + 34U, 0U);
    CASE("codec-revoke-zero-sequence", lx_budget_revoke_decode(
             bytes, LX_BUDGET_REVOKE_PAYLOAD_BYTES, &revoke) ==
             LXP_ERR_NON_CANONICAL);
    CASE("activity-ids-distinct", LX_BUDGET_DEFUND == 0x00030008 &&
         LX_BUDGET_REVOKE == 0x00030009 && LX_BUDGET_CLOSE == 0x00030007);
    return 0;
}

int main(void)
{
    if (helper_path() != 0) {
        printf("BUDGET_CASE helper-path fail\n");
        return 1;
    }
    printf("BUDGET_CASE helper-path ok\n");
    ++cases;
    if (codec_cases() != 0 || replica_replay() != 0) return 1;
    printf("BUDGET_LIFECYCLE cases=%u skipped=0\n", cases);
    return 0;
}
