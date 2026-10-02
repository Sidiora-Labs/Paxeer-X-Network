#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lx_asset.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_state.h"

#include <openssl/evp.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define REQUIRE(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "idempotency lifecycle check failed at line %d\n", __LINE__); \
    return 1; } } while (0)

enum {
    SUSTAINED_ACTIVITIES = 4096,
    LOCKSTEP_ACTIVITIES = 600,
    ACTIVITIES_PER_BATCH = 64,
    FAILURE_STRIDE = 8,
    BOUNDARY_ACCEPTED_KEY = 8200,
    BOUNDARY_REFUSED_KEY = 8201
};

#define ARENA_BYTES ((size_t)64U * 1024U * 1024U)
#define ALICE_START 1000000U
#define CAROL_START 10U

typedef struct fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lx_asset_runtime runtime;
    lx_asset_record asset;
    lxp_transfer_asset_state transfer_asset;
    lxp_arena arena;
    uint8_t *arena_bytes;
    uint64_t parameters;
    uint8_t alice_key[32];
    uint8_t carol_key[32];
    lxp_kernel_execution execution;
    lxp_fee_params fees;
} fixture;

typedef struct signed_activity {
    uint8_t payload[512];
    uint8_t signature[64];
    lxp_activity activity;
    lxp_authority_resolved authority;
} signed_activity;

typedef struct expectation {
    uint64_t global_sequence;
    uint64_t idempotency_count;
    uint64_t alice_balance;
    uint64_t bob_balance;
    uint64_t carol_balance;
    uint64_t oldest_global_sequence;
    int32_t oldest_result;
    uint32_t oldest_length;
    uint32_t carol_length;
    uint32_t refused_key;
    uint32_t oldest_canonical_length;
    uint8_t oldest_canonical[LXP_STATE_MAX_RECEIPT_BYTES];
    uint8_t oldest_activity_id[32];
    uint8_t oldest_receipt[LXP_STATE_MAX_RECEIPT_BYTES];
    uint8_t carol_receipt[LXP_STATE_MAX_RECEIPT_BYTES];
} expectation;

static const uint8_t alice_seed[32] = {1U};
static const uint8_t carol_seed[32] = {2U};
static const uint8_t alice_did[] = "did:key:alice";
static const uint8_t carol_did[] = "did:key:carol";
static const char alice_name[] = "agent:did:key:alice:main";
static const char bob_name[] = "agent:did:key:bob:main";
static const char carol_name[] = "agent:did:key:carol:main";

static lxp_receipt receipt;
static lxp_receipt replica_receipt;
static signed_activity first_activity;
static signed_activity work_activity;
static signed_activity carol_activity;
static unsigned cases;

static void case_ok(const char *name)
{
    ++cases;
    (void)printf("IDEMPOTENCY_CASE %s ok\n", name);
    (void)fflush(stdout);
}

static int sign_digest(const uint8_t seed[32], const uint8_t digest[32],
                       uint8_t signature[64], uint8_t public_key[32])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    size_t key_length = 32U;
    size_t signature_length = 64U;
    int ok = key != NULL && ctx != NULL &&
        EVP_PKEY_get_raw_public_key(key, public_key, &key_length) == 1 &&
        key_length == 32U && EVP_DigestSignInit(ctx, NULL, NULL, NULL, key) == 1 &&
        EVP_DigestSign(ctx, signature, &signature_length, digest, 32U) == 1 &&
        signature_length == 64U;
    EVP_MD_CTX_free(ctx);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int account(fixture *f, const char *name, lx_account **out)
{
    uint8_t id[32];
    REQUIRE(lx_account_id_from_string((const uint8_t *)name, strlen(name), id) == LXP_OK);
    REQUIRE(lx_account_lookup(&f->accounts, (const uint8_t *)name, strlen(name),
                              id, out) == LXP_OK);
    return 0;
}

static int balance(fixture *f, const char *name, uint64_t *value)
{
    lx_account *found;
    REQUIRE(account(f, name, &found) == 0);
    REQUIRE(found->balance.hi == 0U);
    *value = found->balance.lo;
    return 0;
}

static int open_account(fixture *f, const char *name, uint64_t units,
                        const uint8_t *authority_key)
{
    uint8_t id[32];
    uint8_t asset[32] = {3U};
    lx_account *opened;
    REQUIRE(lx_account_id_from_string((const uint8_t *)name, strlen(name), id) == LXP_OK);
    REQUIRE(lx_account_open(&f->accounts, (const uint8_t *)name, strlen(name),
                            id, 1U, LX_ACCOUNT_OPEN_CREDIT, NULL, &opened) == LXP_OK);
    REQUIRE(lxp_ledger_bootstrap_balance(opened, asset, (lxp_u128){0U, units}, 0U) == LXP_OK);
    if (authority_key != NULL) {
        opened->has_authority_key = true;
        (void)memcpy(opened->authority_key, authority_key, 32U);
    }
    return 0;
}

static int fixture_init(fixture *f)
{
    uint8_t digest[32] = {0};
    uint8_t signature[64];
    lxp_identity *identity;
    (void)memset(f, 0, sizeof(*f));
    REQUIRE(sign_digest(alice_seed, digest, signature, f->alice_key) == 0);
    REQUIRE(sign_digest(carol_seed, digest, signature, f->carol_key) == 0);
    f->arena_bytes = (uint8_t *)malloc(ARENA_BYTES);
    REQUIRE(f->arena_bytes != NULL);
    REQUIRE(lxp_arena_init(&f->arena, f->arena_bytes, ARENA_BYTES) == LXP_OK);
    REQUIRE(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    f->parameters = 1U;
    REQUIRE(lxp_kernel_create(&f->kernel, &f->state, &f->journal,
                              &f->parameters, 0U) == LXP_OK);
    REQUIRE(lxp_kernel_set_capabilities(&f->kernel, NULL,
                                        lxp_kernel_canonical_ledger_apply) == LXP_OK);
    REQUIRE(lxp_kernel_register_module(&f->kernel, lx_asset_module_iface()) == LXP_OK);
    REQUIRE(lxp_identity_register(&f->identities, alice_did, sizeof(alice_did) - 1U,
                                  f->alice_key, &identity) == LXP_OK);
    REQUIRE(lxp_identity_register(&f->identities, carol_did, sizeof(carol_did) - 1U,
                                  f->carol_key, &identity) == LXP_OK);
    REQUIRE(lx_account_registry_init(&f->accounts) == LXP_OK);
    REQUIRE(open_account(f, alice_name, ALICE_START, f->alice_key) == 0);
    REQUIRE(open_account(f, bob_name, 0U, NULL) == 0);
    REQUIRE(open_account(f, carol_name, CAROL_START, f->carol_key) == 0);
    f->asset.asset_id[0] = 3U;
    f->transfer_asset.asset_id[0] = 3U;
    f->transfer_asset.registered = true;
    f->runtime = (lx_asset_runtime){&f->accounts, &f->asset, 1U,
        &f->transfer_asset, 1U, 7U, LXP_PROTOCOL_VERSION_STATE_COMMITMENT};
    REQUIRE(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_ASSET, &f->runtime) == LXP_OK);
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    f->execution.network_id = 7U;
    f->execution.batch_number = 1U;
    f->execution.batch_timestamp_ms = 10U;
    f->execution.maximum_timestamp_window = 100U;
    f->execution.recorded_module_version = 1U;
    f->execution.parameter_version = 1U;
    f->execution.signature_valid = true;
    f->execution.identities = &f->identities;
    f->execution.fee_parameters = &f->fees;
    f->execution.gas_limit = 10000U;
    f->execution.arena = &f->arena;
    f->execution.sequencer_private_key = alice_seed;
    f->execution.batch_id[0] = 5U;
    REQUIRE(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    return 0;
}

static void fixture_destroy(fixture *f)
{
    (void)lxp_state_store_destroy(&f->state);
    lx_account_registry_release(&f->accounts);
    free(f->arena_bytes);
    free(f);
}

static void raw_key(uint32_t index, uint8_t marker, uint8_t key[32])
{
    (void)memset(key, 0, 32U);
    key[0] = (uint8_t)(index >> 24);
    key[1] = (uint8_t)(index >> 16);
    key[2] = (uint8_t)(index >> 8);
    key[3] = (uint8_t)index;
    key[30] = marker;
    key[31] = 1U;
}

static int build_send(fixture *f, signed_activity *s, bool carol,
                      uint32_t key_index, uint64_t amount)
{
    const uint8_t *seed = carol ? carol_seed : alice_seed;
    const uint8_t *did = carol ? carol_did : alice_did;
    size_t did_length = carol ? sizeof(carol_did) - 1U : sizeof(alice_did) - 1U;
    const uint8_t *public_key = carol ? f->carol_key : f->alice_key;
    const char *from_name = carol ? carol_name : alice_name;
    uint8_t digest[32];
    uint8_t material[144];
    uint8_t message[512];
    size_t message_length;
    size_t payload_length;
    lx_account *from;
    lxp_identity *identity;
    lxp_send send;
    (void)memset(s, 0, sizeof(*s));
    (void)memset(&send, 0, sizeof(send));
    REQUIRE(account(f, from_name, &from) == 0);
    REQUIRE(lxp_identity_resolve(&f->identities, did, did_length, &identity) == LXP_OK);
    (void)memcpy(send.from, from->id, 32U);
    REQUIRE(lx_account_id_from_string((const uint8_t *)bob_name, strlen(bob_name), send.to) == LXP_OK);
    send.asset[0] = 3U;
    send.amount.lo = amount;
    send.sequence = from->next_sequence;
    send.expires_at = 100U;
    raw_key(key_index, 0U, send.idempotency_key);
    send.authorization.kind = LXP_AUTH_OWNER;
    send.authorization.network_id = 7U;
    send.authorization.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    (void)memcpy(send.authorization.controller, send.from, 32U);
    (void)memcpy(material, send.from, 32U);
    (void)memcpy(material + 32U, send.to, 32U);
    (void)memcpy(material + 64U, send.asset, 32U);
    REQUIRE(lxp_u128_to_be(send.amount, material + 96U) == LXP_OK);
    (void)memcpy(material + 112U, send.idempotency_key, 32U);
    REQUIRE(lxp_hash_context_value(material, sizeof(material), send.context_hash) == LXP_OK);
    (void)memcpy(send.authorization.signed_context_hash, send.context_hash, 32U);
    (void)memcpy(send.authorization.public_key, public_key, 32U);
    REQUIRE(lxp_send_authorization_message(&send, message, sizeof(message), &message_length) == LXP_OK);
    REQUIRE(lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message, message_length, digest) == LXP_OK);
    REQUIRE(sign_digest(seed, digest, send.authorization.signature, send.authorization.public_key) == 0);
    REQUIRE(lxp_send_encode(&send, s->payload, sizeof(s->payload), &payload_length) == LXP_OK);
    s->activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    s->activity.network_id = 7U;
    s->activity.activity_type = LX_ASSET_SEND;
    s->activity.actor_did = (lxp_byte_span){did, did_length};
    s->activity.authority = (lxp_byte_span){public_key, 32U};
    s->activity.signature = (lxp_byte_span){s->signature, 64U};
    s->activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    s->activity.payload = (lxp_byte_span){s->payload, payload_length};
    s->activity.account_sequence = identity->next_sequence;
    (void)memcpy(s->activity.idempotency_key, send.idempotency_key, 32U);
    REQUIRE(lxp_hash_payload(s->payload, payload_length, s->activity.payload_hash) == LXP_OK);
    REQUIRE(lxp_activity_signing_preimage(&s->activity, digest) == LXP_OK);
    {
        uint8_t derived[32];
        REQUIRE(sign_digest(seed, digest, s->signature, derived) == 0);
    }
    REQUIRE(lxp_activity_verify_signature(&s->activity) == LXP_OK);
    s->authority.kind = LXP_AUTHORITY_OWNER;
    (void)memcpy(s->authority.principal, send.from, 32U);
    (void)memcpy(s->authority.verified_key, public_key, 32U);
    (void)memcpy(s->authority.actor, identity->did_id, 32U);
    return 0;
}

static lxp_result execute(fixture *f, signed_activity *s, uint64_t batch_number,
                          uint64_t base_fee, lxp_receipt *out)
{
    f->fees.base_fee.lo = base_fee;
    f->execution.batch_number = batch_number;
    f->execution.global_sequence = f->state.next_sequence;
    f->execution.authority = &s->authority;
    if (lxp_arena_reset(&f->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    return lxp_kernel_execute_activity(&f->kernel, &s->activity, &f->execution, out);
}

static int lookup(fixture *f, const uint8_t *did, size_t did_length,
                  uint32_t key_index, uint8_t marker, const uint8_t **bytes,
                  size_t *length)
{
    uint8_t key[32];
    raw_key(key_index, marker, key);
    *bytes = NULL;
    *length = 0U;
    REQUIRE(lxp_idempotency_lookup(&f->state, did, did_length, key, bytes, length) ==
            LXP_ERR_IDEMPOTENT_REPLAY);
    return 0;
}

static int absent(fixture *f, const uint8_t *did, size_t did_length, uint32_t key_index)
{
    uint8_t key[32];
    const uint8_t *bytes = NULL;
    size_t length = 0U;
    raw_key(key_index, 0U, key);
    REQUIRE(lxp_idempotency_lookup(&f->state, did, did_length, key, &bytes, &length) == LXP_OK);
    REQUIRE(bytes == NULL || length == 0U);
    return 0;
}

static int journal_single_entry(void)
{
    static const uint8_t actor[] = "did:lxp:alice";
    static const uint8_t first_receipt[] = { 1U, 2U, 3U, 4U };
    uint8_t idempotency_key[32] = { 9U };
    uint8_t balance_key[32] = { 5U };
    lxp_state_store store;
    lxp_state_journal journal;
    const uint8_t *replayed_receipt;
    size_t replayed_length;
    lxp_u128 value;
    bool found;
    REQUIRE(lxp_state_store_init(&store, 0U) == LXP_OK);
    REQUIRE(lxp_state_journal_open(&store, 0U, &journal) == LXP_OK);
    REQUIRE(lxp_state_journal_set(&journal, balance_key, (lxp_u128){ 0U, 90U }) == LXP_OK);
    REQUIRE(lxp_idempotency_record(&journal, actor, sizeof(actor) - 1U,
                                   idempotency_key, first_receipt,
                                   sizeof(first_receipt)) == LXP_OK);
    REQUIRE(lxp_state_journal_commit(&journal) == LXP_OK);
    REQUIRE(lxp_idempotency_lookup(&store, actor, sizeof(actor) - 1U,
                                   idempotency_key, &replayed_receipt,
                                   &replayed_length) == LXP_ERR_IDEMPOTENT_REPLAY);
    REQUIRE(replayed_length == sizeof(first_receipt));
    REQUIRE(memcmp(replayed_receipt, first_receipt, replayed_length) == 0);
    REQUIRE(lxp_idempotency_canonical_lookup(&store, actor, sizeof(actor)-1U,
        idempotency_key, &replayed_receipt, &replayed_length) == LXP_ERR_VERSION_UNSUPPORTED);
    REQUIRE(replayed_receipt == NULL && replayed_length == 0U);
    /* A changed second payload is short-circuited before opening a journal. */
    REQUIRE(store.next_sequence == 1U && store.idempotency_count == 1U);
    REQUIRE(lxp_state_store_get(&store, balance_key, &value, &found) == LXP_OK);
    REQUIRE(found && value.hi == 0U && value.lo == 90U);
    REQUIRE(lxp_state_store_destroy(&store) == LXP_OK);
    case_ok("journal_single_entry");
    return 0;
}

/* Every committed alice key replays its recorded first receipt bytes, and a
 * snapshot of the store loads into an independent kernel with an equal
 * canonical root, an equal count and the same retrievable receipts. */
static int checkpoint_case(fixture *f, size_t entries, size_t alice_keys,
                           uint8_t (*saved)[LXP_STATE_MAX_RECEIPT_BYTES],
                           const size_t *saved_length, const char *name)
{
    fixture *restored = (fixture *)malloc(sizeof(*restored));
    lxp_snapshot_manifest_record manifest;
    lxp_byte_span snapshot;
    uint8_t canonical[32];
    uint8_t loaded[32];
    const uint8_t *bytes;
    size_t length;
    size_t i;
    REQUIRE(restored != NULL);
    REQUIRE(f->state.idempotency_count == entries);
    REQUIRE(lxp_state_root(&f->kernel, canonical) == LXP_OK);
    REQUIRE(memcmp(canonical, f->kernel.current_state_root, 32U) == 0);
    for (i = 0U; i < alice_keys; ++i) {
        REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, (uint32_t)i, 0U, &bytes, &length) == 0);
        REQUIRE(length == saved_length[i] && memcmp(bytes, saved[i], length) == 0);
    }
    REQUIRE(fixture_init(restored) == 0);
    REQUIRE(lxp_snapshot_write(&f->kernel, f->state.next_sequence - 1U,
                               &restored->arena, &snapshot) == LXP_OK);
    REQUIRE(lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length,
                                        f->state.next_sequence - 1U, canonical,
                                        f->kernel.current_state_root, &manifest) == LXP_OK);
    REQUIRE(lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest,
                              &restored->kernel) == LXP_OK);
    REQUIRE(lxp_state_root(&restored->kernel, loaded) == LXP_OK);
    REQUIRE(memcmp(canonical, loaded, 32U) == 0);
    REQUIRE(memcmp(restored->kernel.current_state_root, f->kernel.current_state_root, 32U) == 0);
    REQUIRE(restored->state.idempotency_count == entries);
    REQUIRE(restored->state.next_sequence == f->state.next_sequence);
    for (i = 0U; i < alice_keys; ++i) {
        REQUIRE(lookup(restored, alice_did, sizeof(alice_did) - 1U, (uint32_t)i, 0U, &bytes, &length) == 0);
        REQUIRE(length == saved_length[i] && memcmp(bytes, saved[i], length) == 0);
        {
            uint8_t key[32];
            const uint8_t *original, *recovered;
            size_t original_length, recovered_length;
            raw_key((uint32_t)i, 0U, key);
            REQUIRE(lxp_idempotency_canonical_lookup(&f->state, alice_did,
                sizeof(alice_did)-1U, key, &original, &original_length) == LXP_ERR_IDEMPOTENT_REPLAY);
            REQUIRE(lxp_idempotency_canonical_lookup(&restored->state, alice_did,
                sizeof(alice_did)-1U, key, &recovered, &recovered_length) == LXP_ERR_IDEMPOTENT_REPLAY);
            REQUIRE(original_length == recovered_length && memcmp(original, recovered, original_length) == 0);
        }
    }
    fixture_destroy(restored);
    case_ok(name);
    return 0;
}

static int legacy_snapshot_case(fixture *source)
{
    fixture *legacy = malloc(sizeof(*legacy)), *restored = malloc(sizeof(*restored));
    lxp_snapshot_manifest_record manifest;
    lxp_byte_span snapshot;
    uint8_t root[32], loaded[32], key[32];
    const uint8_t *bytes;
    size_t length;
    REQUIRE(legacy != NULL && restored != NULL);
    REQUIRE(source->state.idempotency_count == 511U);
    REQUIRE(fixture_init(legacy) == 0 && fixture_init(restored) == 0);
    REQUIRE(lxp_state_root(&source->kernel, root) == LXP_OK);
    REQUIRE(lxp_snapshot_write(&source->kernel, source->state.next_sequence-1U,
        &legacy->arena, &snapshot) == LXP_OK);
    REQUIRE(lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length,
        source->state.next_sequence-1U, root, source->kernel.current_state_root, &manifest) == LXP_OK);
    REQUIRE(lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest, &legacy->kernel) == LXP_OK);
    for (size_t i = 0U; i < legacy->state.idempotency_count; ++i)
        legacy->state.idempotency[i].canonical_length = 0U;
    REQUIRE(lxp_state_root(&legacy->kernel, loaded) == LXP_OK);
    REQUIRE(memcmp(root, loaded, 32U) == 0);
    REQUIRE(lxp_snapshot_write(&legacy->kernel, legacy->state.next_sequence-1U,
        &restored->arena, &snapshot) == LXP_OK);
    REQUIRE(lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length,
        legacy->state.next_sequence-1U, root, legacy->kernel.current_state_root, &manifest) == LXP_OK);
    REQUIRE(lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest, &restored->kernel) == LXP_OK);
    REQUIRE(lxp_state_root(&restored->kernel, loaded) == LXP_OK);
    REQUIRE(memcmp(root, loaded, 32U) == 0 && restored->state.idempotency_count == 511U);
    raw_key(0U, 0U, key);
    REQUIRE(lxp_idempotency_lookup(&restored->state, alice_did, sizeof(alice_did)-1U,
        key, &bytes, &length) == LXP_ERR_IDEMPOTENT_REPLAY);
    REQUIRE(length != 0U);
    REQUIRE(lxp_idempotency_canonical_lookup(&restored->state, alice_did, sizeof(alice_did)-1U,
        key, &bytes, &length) == LXP_ERR_VERSION_UNSUPPORTED);
    REQUIRE(bytes == NULL && length == 0U);
    fixture_destroy(legacy);
    fixture_destroy(restored);
    case_ok("legacy_snapshot_root_preserved");
    return 0;
}

typedef struct observed {
    uint64_t alice;
    uint64_t bob;
    uint64_t carol;
    uint64_t alice_identity_sequence;
    uint64_t alice_account_sequence;
    uint64_t next_sequence;
    size_t count;
    uint8_t root[32];
} observed;

static int observe(fixture *f, observed *o)
{
    lxp_identity *identity;
    lx_account *from;
    REQUIRE(balance(f, alice_name, &o->alice) == 0);
    REQUIRE(balance(f, bob_name, &o->bob) == 0);
    REQUIRE(balance(f, carol_name, &o->carol) == 0);
    REQUIRE(lxp_identity_resolve(&f->identities, alice_did, sizeof(alice_did) - 1U,
                                 &identity) == LXP_OK);
    REQUIRE(account(f, alice_name, &from) == 0);
    o->alice_identity_sequence = identity->next_sequence;
    o->alice_account_sequence = from->next_sequence;
    o->next_sequence = f->state.next_sequence;
    o->count = f->state.idempotency_count;
    (void)memcpy(o->root, f->kernel.current_state_root, 32U);
    return 0;
}

static int unchanged(fixture *f, const observed *before)
{
    observed after;
    uint8_t canonical[32];
    REQUIRE(observe(f, &after) == 0);
    REQUIRE(after.alice == before->alice && after.bob == before->bob &&
            after.carol == before->carol);
    REQUIRE(after.alice_identity_sequence == before->alice_identity_sequence);
    REQUIRE(after.alice_account_sequence == before->alice_account_sequence);
    REQUIRE(after.next_sequence == before->next_sequence);
    REQUIRE(after.count == before->count);
    REQUIRE(memcmp(after.root, before->root, 32U) == 0);
    REQUIRE(lxp_state_root(&f->kernel, canonical) == LXP_OK);
    REQUIRE(memcmp(canonical, before->root, 32U) == 0);
    return 0;
}

static int replay_matches(lxp_result status, const expectation *e)
{
    uint8_t *storage = malloc(LXP_MAX_ACTIVITY_BYTES);
    lxp_arena arena;
    lxp_byte_span canonical;
    uint8_t public_key[32], signature[64], zero[32] = {0};
    REQUIRE(storage != NULL);
    REQUIRE(lxp_arena_init(&arena, storage, LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_receipt_encode(&receipt, true, &arena, &canonical) == LXP_OK);
    REQUIRE(canonical.length == e->oldest_canonical_length);
    REQUIRE(memcmp(canonical.bytes, e->oldest_canonical, canonical.length) == 0);
    REQUIRE(lxp_arena_reset(&arena, 0U) == LXP_OK);
    REQUIRE(sign_digest(alice_seed, zero, signature, public_key) == 0);
    REQUIRE(lxp_receipt_verify(&receipt, public_key, &arena) == LXP_OK);
    free(storage);
    REQUIRE(status == LXP_ERR_IDEMPOTENT_REPLAY);
    REQUIRE(memcmp(receipt.activity_id, e->oldest_activity_id, 32U) == 0);
    REQUIRE(receipt.global_sequence == e->oldest_global_sequence);
    REQUIRE((int32_t)receipt.result_code == e->oldest_result);
    return 0;
}

static int write_file(const char *directory, const char *name, const void *bytes, size_t length)
{
    char path[4096];
    FILE *file;
    int written = snprintf(path, sizeof(path), "%s/%s", directory, name);
    REQUIRE(written > 0 && (size_t)written < sizeof(path));
    file = fopen(path, "wb");
    REQUIRE(file != NULL);
    REQUIRE(fwrite(bytes, 1U, length, file) == length);
    REQUIRE(fclose(file) == 0);
    return 0;
}

static int read_file(const char *directory, const char *name, void *bytes, size_t length)
{
    char path[4096];
    FILE *file;
    int written = snprintf(path, sizeof(path), "%s/%s", directory, name);
    REQUIRE(written > 0 && (size_t)written < sizeof(path));
    file = fopen(path, "rb");
    REQUIRE(file != NULL);
    REQUIRE(fread(bytes, 1U, length, file) == length);
    REQUIRE(fgetc(file) == EOF);
    REQUIRE(fclose(file) == 0);
    return 0;
}

/* Writes the checkpoint exactly as the node does at a batch boundary:
 * boundary read, snapshot encode, manifest, durable snapshot store. */
static int node_checkpoint(fixture *f, const char *directory, uint64_t *sequence)
{
    lxp_kernel_batch_boundary boundary;
    lxp_snapshot_manifest_record manifest;
    lxp_byte_span snapshot;
    REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    REQUIRE(lxp_kernel_batch_boundary_read(&f->kernel, &boundary) == LXP_OK);
    REQUIRE(boundary.next_sequence != 0U);
    *sequence = boundary.next_sequence - 1U;
    REQUIRE(lxp_snapshot_write(&f->kernel, *sequence, &f->arena, &snapshot) == LXP_OK);
    REQUIRE(lxp_snapshot_manifest(snapshot.bytes, snapshot.length, *sequence,
                                  boundary.canonical_state_root,
                                  boundary.receipt_state_root, &manifest) == LXP_OK);
    REQUIRE(lxp_snapshot_store_write(directory, &manifest, snapshot.bytes,
                                     snapshot.length) == LXP_OK);
    return 0;
}

static int lifecycle(const char *directory)
{
    fixture *f = (fixture *)malloc(sizeof(*f));
    fixture *replica = (fixture *)malloc(sizeof(*replica));
    expectation *e = (expectation *)calloc(1U, sizeof(*e));
    uint8_t (*saved)[LXP_STATE_MAX_RECEIPT_BYTES] =
        (uint8_t (*)[LXP_STATE_MAX_RECEIPT_BYTES])malloc(
            (size_t)SUSTAINED_ACTIVITIES * LXP_STATE_MAX_RECEIPT_BYTES);
    size_t *saved_length = (size_t *)calloc(SUSTAINED_ACTIVITIES, sizeof(size_t));
    const uint8_t *bytes;
    size_t length;
    size_t successes = 0U;
    size_t failures = 0U;
    uint32_t i;
    observed before;
    lxp_result status;
    REQUIRE(f != NULL && replica != NULL && e != NULL && saved != NULL && saved_length != NULL);
    REQUIRE(fixture_init(f) == 0);
    REQUIRE(fixture_init(replica) == 0);
    REQUIRE(memcmp(f->kernel.current_state_root, replica->kernel.current_state_root, 32U) == 0);

    for (i = 0U; i < SUSTAINED_ACTIVITIES; ++i) {
        bool fail = (i % FAILURE_STRIDE) == FAILURE_STRIDE - 1U;
        signed_activity *s = i == 0U ? &first_activity : &work_activity;
        uint64_t batch = 1U + i / ACTIVITIES_PER_BATCH;
        uint64_t alice_before;
        uint64_t bob_before;
        uint64_t alice_after;
        uint64_t bob_after;
        REQUIRE(balance(f, alice_name, &alice_before) == 0);
        REQUIRE(balance(f, bob_name, &bob_before) == 0);
        REQUIRE(build_send(f, s, false, i, 1U) == 0);
        status = execute(f, s, batch, fail ? 1U : 0U, &receipt);
        if (status != LXP_OK || receipt.result_code != (fail ? LXP_ERR_FEE_LIMIT : LXP_OK))
            (void)fprintf(stderr, "activity %u status %d result %d\n", (unsigned)i,
                          (int)status, (int)receipt.result_code);
        REQUIRE(status == LXP_OK);
        REQUIRE(receipt.result_code == (fail ? LXP_ERR_FEE_LIMIT : LXP_OK));
        REQUIRE(f->state.idempotency_count == (size_t)i + 1U);
        REQUIRE(memcmp(receipt.resulting_state_root, f->kernel.current_state_root, 32U) == 0);
        REQUIRE(balance(f, alice_name, &alice_after) == 0);
        REQUIRE(balance(f, bob_name, &bob_after) == 0);
        REQUIRE(alice_after == alice_before - (fail ? 0U : 1U));
        REQUIRE(bob_after == bob_before + (fail ? 0U : 1U));
        if (fail) ++failures; else ++successes;
        REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, i, 0U, &bytes, &length) == 0);
        REQUIRE(length <= LXP_STATE_MAX_RECEIPT_BYTES);
        {
            const uint8_t *canonical;
            size_t canonical_length;
            lxp_byte_span encoded;
            REQUIRE(lxp_idempotency_canonical_lookup(&f->state, alice_did,
                sizeof(alice_did)-1U, s->activity.idempotency_key,
                &canonical, &canonical_length) == LXP_ERR_IDEMPOTENT_REPLAY);
            REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
            REQUIRE(lxp_receipt_verify(&receipt, f->alice_key, &f->arena) == LXP_OK);
            REQUIRE(lxp_receipt_encode(&receipt, true, &f->arena, &encoded) == LXP_OK);
            REQUIRE(canonical_length == encoded.length &&
                memcmp(canonical, encoded.bytes, canonical_length) == 0);
        }
        (void)memcpy(saved[i], bytes, length);
        saved_length[i] = length;
        if (i == 0U) {
            (void)memcpy(e->oldest_activity_id, receipt.activity_id, 32U);
            e->oldest_global_sequence = receipt.global_sequence;
            e->oldest_result = (int32_t)receipt.result_code;
            e->oldest_length = (uint32_t)length;
            (void)memcpy(e->oldest_receipt, bytes, length);
            {
                const uint8_t *canonical;
                size_t canonical_length;
                lxp_byte_span encoded;
                REQUIRE(lxp_idempotency_canonical_lookup(&f->state, alice_did,
                    sizeof(alice_did)-1U, s->activity.idempotency_key,
                    &canonical, &canonical_length) == LXP_ERR_IDEMPOTENT_REPLAY);
                REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
                REQUIRE(lxp_receipt_encode(&receipt, true, &f->arena, &encoded) == LXP_OK);
                REQUIRE(canonical_length == encoded.length &&
                    memcmp(canonical, encoded.bytes, canonical_length) == 0);
                e->oldest_canonical_length = (uint32_t)canonical_length;
                (void)memcpy(e->oldest_canonical, canonical, canonical_length);
            }
        }
        if (i < LOCKSTEP_ACTIVITIES) {
            status = execute(replica, s, batch, fail ? 1U : 0U, &replica_receipt);
            REQUIRE(status == LXP_OK);
            REQUIRE(replica_receipt.result_code == receipt.result_code);
            REQUIRE(memcmp(replica_receipt.activity_id, receipt.activity_id, 32U) == 0);
            REQUIRE(memcmp(replica_receipt.resulting_state_root,
                           receipt.resulting_state_root, 32U) == 0);
            REQUIRE(replica->state.idempotency_count == f->state.idempotency_count);
            if (i + 1U == LOCKSTEP_ACTIVITIES) {
                uint8_t left[32];
                uint8_t right[32];
                REQUIRE(lxp_state_root(&f->kernel, left) == LXP_OK);
                REQUIRE(lxp_state_root(&replica->kernel, right) == LXP_OK);
                REQUIRE(memcmp(left, right, 32U) == 0);
                fixture_destroy(replica);
                replica = NULL;
                case_ok("replica_lockstep_600");
            }
        }
        if (i + 1U == 511U) {
            REQUIRE(checkpoint_case(f, 511U, 511U, saved, saved_length, "checkpoint_511") == 0);
            REQUIRE(legacy_snapshot_case(f) == 0);
        }
        if (i + 1U == 512U)
            REQUIRE(checkpoint_case(f, 512U, 512U, saved, saved_length, "checkpoint_512") == 0);
        if (i + 1U == 513U)
            REQUIRE(checkpoint_case(f, 513U, 513U, saved, saved_length, "checkpoint_513") == 0);
    }
    REQUIRE(successes + failures == SUSTAINED_ACTIVITIES && failures != 0U);
    REQUIRE(f->execution.batch_number > 1U);
    case_ok("sustained_4096");
    REQUIRE(checkpoint_case(f, SUSTAINED_ACTIVITIES, SUSTAINED_ACTIVITIES, saved, saved_length, "checkpoint_4096") == 0);

    {
        lxp_idempotency_key_state original = f->state.idempotency[0], decoded;
        uint8_t envelope[LXP_STATE_MAX_REPLAY_ENTRY_BYTES];
        size_t encoded_length;
        (void)memset(&decoded, 0, sizeof(decoded));
        REQUIRE(lxp_idempotency_snapshot_encode(&original, envelope,
            sizeof(envelope), &encoded_length) == LXP_OK);
        REQUIRE(lxp_idempotency_snapshot_decode(&decoded, envelope, encoded_length) == LXP_OK);
        REQUIRE(decoded.canonical_length == original.canonical_length);
        REQUIRE(memcmp(decoded.canonical_receipt, original.canonical_receipt,
            original.canonical_length) == 0);
        REQUIRE(lxp_idempotency_snapshot_decode(&decoded, envelope, encoded_length-1U) != LXP_OK);
        envelope[5U] = 0xffU;
        REQUIRE(lxp_idempotency_snapshot_decode(&decoded, envelope, encoded_length) != LXP_OK);
        REQUIRE(lxp_idempotency_snapshot_encode(&original, envelope,
            sizeof(envelope), &encoded_length) == LXP_OK);
        envelope[13U + 7U] ^= 1U;
        REQUIRE(lxp_idempotency_snapshot_decode(&decoded, envelope, encoded_length) == LXP_OK);
        REQUIRE(lxp_kernel_idempotency_receipt_validate(&decoded) == LXP_ERR_SNAPSHOT_MISMATCH);
        original.canonical_length = LXP_STATE_MAX_RECEIPT_BYTES + 1U;
        REQUIRE(lxp_idempotency_snapshot_encode(&original, envelope,
            sizeof(envelope), &encoded_length) == LXP_ERR_NON_CANONICAL);
        original.canonical_length = 0U;
        REQUIRE(lxp_idempotency_snapshot_encode(&original, envelope,
            sizeof(envelope), &encoded_length) == LXP_OK);
        REQUIRE(encoded_length == original.receipt_length);
        REQUIRE(lxp_idempotency_snapshot_decode(&decoded, envelope, encoded_length) == LXP_OK);
        REQUIRE(decoded.canonical_length == 0U && decoded.receipt_length == original.receipt_length);
        REQUIRE(memcmp(decoded.receipt, original.receipt, original.receipt_length) == 0);
        case_ok("canonical_sidecar_codec");
    }

    /* Oldest actor/key, byte-identical first payload. */
    REQUIRE(observe(f, &before) == 0);
    status = execute(f, &first_activity, f->execution.batch_number + 1U, 0U, &receipt);
    REQUIRE(replay_matches(status, e) == 0);
    REQUIRE(unchanged(f, &before) == 0);
    case_ok("oldest_replay_identical");

    /* Oldest actor/key, changed amount and current sequences. */
    REQUIRE(build_send(f, &work_activity, false, 0U, 7U) == 0);
    status = execute(f, &work_activity, f->execution.batch_number + 1U, 0U, &receipt);
    REQUIRE(replay_matches(status, e) == 0);
    REQUIRE(unchanged(f, &before) == 0);
    REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, 0U, 0U, &bytes, &length) == 0);
    REQUIRE(length == e->oldest_length && memcmp(bytes, e->oldest_receipt, length) == 0);
    case_ok("oldest_replay_changed");

    /* A different actor presenting the same raw key is a fresh activity. */
    REQUIRE(build_send(f, &carol_activity, true, 0U, 1U) == 0);
    status = execute(f, &carol_activity, f->execution.batch_number + 1U, 0U, &receipt);
    REQUIRE(status == LXP_OK && receipt.result_code == LXP_OK);
    REQUIRE(memcmp(receipt.activity_id, e->oldest_activity_id, 32U) != 0);
    REQUIRE(f->state.idempotency_count == before.count + 1U);
    {
        uint64_t carol_now;
        uint64_t bob_now;
        REQUIRE(balance(f, carol_name, &carol_now) == 0);
        REQUIRE(balance(f, bob_name, &bob_now) == 0);
        REQUIRE(carol_now == CAROL_START - 1U && bob_now == before.bob + 1U);
    }
    REQUIRE(lookup(f, carol_did, sizeof(carol_did) - 1U, 0U, 0U, &bytes, &length) == 0);
    REQUIRE(length != e->oldest_length || memcmp(bytes, e->oldest_receipt, length) != 0);
    e->carol_length = (uint32_t)length;
    (void)memcpy(e->carol_receipt, bytes, length);
    REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, 0U, 0U, &bytes, &length) == 0);
    REQUIRE(length == e->oldest_length && memcmp(bytes, e->oldest_receipt, length) == 0);
    REQUIRE(observe(f, &before) == 0);
    REQUIRE(build_send(f, &carol_activity, true, 0U, 3U) == 0);
    status = execute(f, &carol_activity, f->execution.batch_number + 1U, 0U, &receipt);
    REQUIRE(status == LXP_ERR_IDEMPOTENT_REPLAY);
    REQUIRE(unchanged(f, &before) == 0);
    case_ok("distinct_actor");

    /* Epoch transition keeps every retained identity. */
    {
        uint64_t epoch = f->kernel.epoch + 1U;
        REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
        REQUIRE(lxp_kernel_epoch_transition(&f->kernel, epoch, 20U, &f->arena) == LXP_OK);
        REQUIRE(f->kernel.epoch == epoch);
        f->execution.epoch = epoch;
        REQUIRE(f->state.idempotency_count == before.count);
        REQUIRE(observe(f, &before) == 0);
        REQUIRE(build_send(f, &work_activity, false, 0U, 9U) == 0);
        status = execute(f, &work_activity, f->execution.batch_number + 1U, 0U, &receipt);
        REQUIRE(replay_matches(status, e) == 0);
        REQUIRE(unchanged(f, &before) == 0);
        REQUIRE(checkpoint_case(f, before.count, SUSTAINED_ACTIVITIES, saved, saved_length, "epoch_checkpoint") == 0);
        case_ok("epoch_transition");
    }

    {
        uint32_t next_key = SUSTAINED_ACTIVITIES;
        while (f->state.idempotency_count < (size_t)LXP_STATE_MAX_IDEMPOTENCY - 1U) {
            size_t count = f->state.idempotency_count;
            REQUIRE(build_send(f, &work_activity, false, next_key, 1U) == 0);
            status = execute(f, &work_activity, 1U + next_key / ACTIVITIES_PER_BATCH,
                             0U, &receipt);
            REQUIRE(status == LXP_OK && receipt.result_code == LXP_OK);
            REQUIRE(f->state.idempotency_count == count + 1U);
            ++next_key;
        }
        REQUIRE(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
        REQUIRE(build_send(f, &work_activity, false, BOUNDARY_ACCEPTED_KEY, 1U) == 0);
        status = execute(f, &work_activity, f->execution.batch_number + 1U, 0U, &receipt);
        REQUIRE(status == LXP_OK && receipt.result_code == LXP_OK);
        REQUIRE(f->state.idempotency_count == (size_t)LXP_STATE_MAX_IDEMPOTENCY);
        REQUIRE(observe(f, &before) == 0);
        REQUIRE(build_send(f, &work_activity, false, BOUNDARY_REFUSED_KEY, 1U) == 0);
        status = execute(f, &work_activity, f->execution.batch_number + 1U, 0U, &receipt);
        REQUIRE(status == LXP_ERR_ARENA_EXHAUSTED);
        REQUIRE(unchanged(f, &before) == 0);
        REQUIRE(absent(f, alice_did, sizeof(alice_did) - 1U, BOUNDARY_REFUSED_KEY) == 0);
        REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, BOUNDARY_ACCEPTED_KEY, 0U,
                       &bytes, &length) == 0);
        REQUIRE(build_send(f, &work_activity, false, 0U, 11U) == 0);
        status = execute(f, &work_activity, f->execution.batch_number + 1U, 0U, &receipt);
        REQUIRE(replay_matches(status, e) == 0);
        REQUIRE(unchanged(f, &before) == 0);
        for (i = 0U; i < SUSTAINED_ACTIVITIES; ++i) {
            REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, i, 0U, &bytes, &length) == 0);
            REQUIRE(length == saved_length[i] && memcmp(bytes, saved[i], length) == 0);
        }
        case_ok("storage_boundary");
    }

    REQUIRE(node_checkpoint(f, directory, &e->global_sequence) == 0);
    e->idempotency_count = f->state.idempotency_count;
    e->alice_balance = before.alice;
    e->bob_balance = before.bob;
    e->carol_balance = before.carol;
    e->refused_key = BOUNDARY_REFUSED_KEY;
    REQUIRE(write_file(directory, "expectation.bin", e, sizeof(*e)) == 0);
    case_ok("checkpoint_written");
    fixture_destroy(f);
    free(saved);
    free(saved_length);
    free(e);
    return 0;
}

static int restart(const char *directory)
{
    fixture *f = (fixture *)malloc(sizeof(*f));
    expectation *e = (expectation *)calloc(1U, sizeof(*e));
    lxp_snapshot_manifest_record manifest;
    lxp_byte_span snapshot;
    char path[4096];
    uint8_t canonical[32];
    const uint8_t *bytes;
    size_t length;
    observed before;
    lxp_result status;
    int written;
    REQUIRE(f != NULL && e != NULL);
    REQUIRE(read_file(directory, "expectation.bin", e, sizeof(*e)) == 0);
    REQUIRE(fixture_init(f) == 0);
    written = snprintf(path, sizeof(path), "%s/%020llu.lxs", directory,
                       (unsigned long long)e->global_sequence);
    REQUIRE(written > 0 && (size_t)written < sizeof(path));
    REQUIRE(lxp_snapshot_store_read(path, &f->arena, &manifest, &snapshot) == LXP_OK);
    REQUIRE(manifest.global_sequence == e->global_sequence);
    REQUIRE(lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest, &f->kernel) == LXP_OK);
    REQUIRE(lxp_state_root(&f->kernel, canonical) == LXP_OK);
    REQUIRE(memcmp(canonical, manifest.canonical_state_root, 32U) == 0);
    REQUIRE(memcmp(f->kernel.current_state_root, manifest.receipt_state_root, 32U) == 0);
    REQUIRE(f->state.idempotency_count == e->idempotency_count);
    REQUIRE(f->state.next_sequence == e->global_sequence + 1U);
    f->execution.epoch = f->kernel.epoch;
    REQUIRE(observe(f, &before) == 0);
    REQUIRE(before.alice == e->alice_balance && before.bob == e->bob_balance &&
            before.carol == e->carol_balance);
    case_ok("restart_root");

    REQUIRE(build_send(f, &work_activity, false, 0U, 13U) == 0);
    status = execute(f, &work_activity, 1U, 0U, &receipt);
    REQUIRE(replay_matches(status, e) == 0);
    REQUIRE(unchanged(f, &before) == 0);
    REQUIRE(lookup(f, alice_did, sizeof(alice_did) - 1U, 0U, 0U, &bytes, &length) == 0);
    REQUIRE(length == e->oldest_length && memcmp(bytes, e->oldest_receipt, length) == 0);
    case_ok("restart_replay_changed");

    REQUIRE(lookup(f, carol_did, sizeof(carol_did) - 1U, 0U, 0U, &bytes, &length) == 0);
    REQUIRE(length == e->carol_length && memcmp(bytes, e->carol_receipt, length) == 0);
    REQUIRE(length != e->oldest_length || memcmp(bytes, e->oldest_receipt, length) != 0);
    case_ok("restart_distinct_actor");

    REQUIRE(absent(f, alice_did, sizeof(alice_did) - 1U, e->refused_key) == 0);
    REQUIRE(before.alice == e->alice_balance);
    case_ok("restart_refused_absent");

    {
        uint64_t epoch = f->kernel.epoch + 1U;
        REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
        REQUIRE(lxp_kernel_epoch_transition(&f->kernel, epoch, 20U, &f->arena) == LXP_OK);
        f->execution.epoch = epoch;
        REQUIRE(f->state.idempotency_count == e->idempotency_count);
        REQUIRE(observe(f, &before) == 0);
        REQUIRE(build_send(f, &work_activity, false, 0U, 17U) == 0);
        status = execute(f, &work_activity, 2U, 0U, &receipt);
        REQUIRE(replay_matches(status, e) == 0);
        REQUIRE(unchanged(f, &before) == 0);
        REQUIRE(build_send(f, &carol_activity, true, 0U, 5U) == 0);
        status = execute(f, &carol_activity, 2U, 0U, &receipt);
        REQUIRE(status == LXP_ERR_IDEMPOTENT_REPLAY);
        REQUIRE(unchanged(f, &before) == 0);
        case_ok("restart_epoch_transition");
    }
    fixture_destroy(f);
    free(e);
    return 0;
}

int main(int argc, char **argv)
{
    REQUIRE(journal_single_entry() == 0);
    if (argc == 3 && strcmp(argv[1], "--checkpoint") == 0) {
        REQUIRE(lifecycle(argv[2]) == 0);
    } else if (argc == 3 && strcmp(argv[1], "--restart") == 0) {
        REQUIRE(restart(argv[2]) == 0);
    } else {
        REQUIRE(argc == 1);
    }
    (void)printf("IDEMPOTENCY_LIFECYCLE cases=%u skipped=0\n", cases);
    return 0;
}
