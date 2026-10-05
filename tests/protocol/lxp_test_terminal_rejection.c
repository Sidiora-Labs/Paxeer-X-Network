#define _POSIX_C_SOURCE 200809L
#include "lxp_daemon_batch_wal.h"
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_genesis.h"
#include "layerx/lxp_maintenance.h"
#include "layerx/lx_asset.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_admission.h"
#include <unistd.h>
#define main terminal_rejection_activity_fixture_main
#include "../programs/test_call_activity.c"
#undef main

#define CHECK(expression) do { if (!(expression)) { \
    fprintf(stderr, "terminal rejection failure at %d: %s\n", __LINE__, #expression); \
    return 1; } } while (0)

static const uint8_t terminal_actor_seed[32] = {0x51U};
static const uint8_t terminal_did[] = "did:lxp:terminal-rejection";
static const uint8_t terminal_actor_name[] = "agent:did:lxp:terminal-rejection:main";
static const uint8_t terminal_treasury_name[] = "system:fees";
static const uint8_t terminal_recipient_name[] = "agent:did:lxp:terminal-recipient:main";

typedef struct terminal_fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lxp_identity *identity;
    lx_account_registry accounts;
    lx_account *actor;
    lx_account *treasury;
    lx_account *recipient;
    lxp_transfer_asset_state asset;
    lx_asset_record asset_record;
    lx_asset_runtime asset_runtime;
    lx_programs_transfer_runtime runtime;
    lxp_authority_scope scope;
    lxp_authority_resolved authority;
    lxp_fee_params fees;
    lxp_sequencer_authorization authorization;
    lxp_log feed_log;
    lxp_log canonical_log;
    lxp_history history;
    lx_programs_state_feed_store feed;
    pthread_mutex_t feed_mutex;
    lxp_arena arena;
    uint8_t *storage;
    uint8_t actor_public_key[32];
    uint8_t actor_id[32];
    uint8_t recipient_id[32];
    char directory[128];
} terminal_fixture;

static int terminal_sign(const uint8_t seed[32], const uint8_t *message,
                         size_t length, uint8_t signature[64])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    size_t signature_length = 64U;
    int ok = key != NULL && context != NULL &&
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) == 1 &&
        EVP_DigestSign(context, signature, &signature_length, message, length) == 1 &&
        signature_length == 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int terminal_fixture_open(terminal_fixture *f)
{
    static const uint64_t parameters = 1U;
    uint8_t treasury_id[32], grant[32] = {0};
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters fees = {0};
    memset(f, 0, sizeof(*f));
    f->storage = malloc(4U * LXP_MAX_BATCH_BODY_BYTES);
    CHECK(f->storage != NULL);
    CHECK(lxp_arena_init(&f->arena, f->storage, 4U * LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
    CHECK(executed_public_key(terminal_actor_seed, f->actor_public_key) == 0);
    CHECK(executed_public_key(executed_sequencer_seed, f->authorization.public_key) == 0);
    memcpy(f->authorization.sequencer_id, f->authorization.public_key, 32U);
    f->authorization.authorized = 1U;
    f->authorization.first_batch_number = 1U;
    f->authorization.last_batch_number = 100U;
    CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    CHECK(lx_account_id_from_string(terminal_actor_name,
        sizeof(terminal_actor_name) - 1U, f->actor_id) == LXP_OK);
    CHECK(lx_account_id_from_string(terminal_treasury_name,
        sizeof(terminal_treasury_name) - 1U, treasury_id) == LXP_OK);
    CHECK(lx_account_id_from_string(terminal_recipient_name,
        sizeof(terminal_recipient_name) - 1U, f->recipient_id) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, terminal_actor_name, sizeof(terminal_actor_name) - 1U,
        f->actor_id, 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->actor) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, terminal_treasury_name, sizeof(terminal_treasury_name) - 1U,
        treasury_id, 2U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->treasury) == LXP_OK);
    CHECK(lx_account_open(&f->accounts, terminal_recipient_name, sizeof(terminal_recipient_name) - 1U,
        f->recipient_id, 3U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->recipient) == LXP_OK);
    f->asset.asset_id[0] = 9U;
    f->asset.registered = true;
    CHECK(lxp_ledger_bootstrap_balance(f->actor, f->asset.asset_id,
        (lxp_u128){0U, 1000U}, 1U) == LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(f->treasury, f->asset.asset_id,
        (lxp_u128){0U, 0U}, 0U) == LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(f->recipient, f->asset.asset_id,
        (lxp_u128){0U, 0U}, 0U) == LXP_OK);
    CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    CHECK(lxp_identity_register(&f->identities, terminal_did, sizeof(terminal_did) - 1U,
        f->actor_public_key, &f->identity) == LXP_OK);
    CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal, &parameters, 1U) == LXP_OK);
    CHECK(install_metering_v1(&f->kernel) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, programs_module_registration_v4()) == LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lx_asset_module_iface()) == LXP_OK);
    memcpy(f->asset_record.asset_id, f->asset.asset_id, 32U);
    f->asset_runtime = (lx_asset_runtime){&f->accounts, &f->asset_record, 1U,
        &f->asset, 1U, 7U, 3U};
    f->actor->has_authority_key = true;
    memcpy(f->actor->authority_key, f->actor_public_key, 32U);
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_ASSET, &f->asset_runtime) == LXP_OK);
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U};
    memcpy(f->runtime.occupancy_asset_id, f->asset.asset_id, 32U);
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS, &f->runtime) == LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    memcpy(manifest.signer_public_key, f->actor_public_key, 32U);
    fees.schedule = f->runtime.fee_schedule;
    memcpy(fees.occupancy_asset_id, f->asset.asset_id, 32U);
    fees.target_occupancy_byte_batches = 3U;
    fees.response_denominator = 1U;
    fees.maximum_change_numerator = 1U;
    fees.maximum_change_denominator = 1U;
    fees.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fees.maximum_fee_units_per_occupancy_byte_batch = 10U;
    CHECK(lxp_programs_fee_genesis_append(&manifest, &fees) == LXP_OK);
    CHECK(lxp_programs_fee_genesis_materialize(&manifest, &f->kernel) == LXP_OK);
    CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    f->scope.module_mask = UINT64_C(1) << LXP_MODULE_ASSET;
    f->scope.activity_ordinal_min = 1U;
    f->scope.activity_ordinal_max = 10U;
    f->scope.maximum_per_activity = (lxp_u128){UINT64_MAX, UINT64_MAX};
    f->scope.maximum_total = f->scope.maximum_per_activity;
    f->scope.maximum_per_period = f->scope.maximum_per_activity;
    f->authority.scope = &f->scope;
    f->authority.kind = LXP_AUTHORITY_OWNER;
    memcpy(f->authority.actor, f->identity->did_id, 32U);
    memcpy(f->authority.principal, f->actor_id, 32U);
    memcpy(f->authority.verified_key, f->actor_public_key, 32U);
    CHECK(lxp_authority_hash(f->authority.kind, grant, f->actor_public_key,
        f->authority.authority_hash) == LXP_OK);
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    {
        char path[192], database[192];
        strcpy(f->directory, "/tmp/lxp-terminal-rejection-XXXXXX");
        CHECK(mkdtemp(f->directory) != NULL);
        CHECK(pthread_mutex_init(&f->feed_mutex, NULL) == 0);
        CHECK(snprintf(path, sizeof(path), "%s/feed.log", f->directory) > 0);
        CHECK(lxp_log_open_or_create(&f->feed_log, path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
        CHECK(snprintf(path, sizeof(path), "%s/canonical.log", f->directory) > 0);
        CHECK(lxp_log_open_or_create(&f->canonical_log, path, LXP_MAX_BATCH_BODY_BYTES) == LXP_OK);
        CHECK(snprintf(database, sizeof(database), "%s/history.db", f->directory) > 0);
        CHECK(lxp_history_open(&f->history, &f->canonical_log, database,
            "migrations/0007_history_index.sql") == LXP_OK);
        CHECK(lxp_programs_state_feed_store_open(&f->feed, &f->feed_log, &f->canonical_log,
            &f->history, &f->arena, &f->feed_mutex) == LXP_OK);
        CHECK(lxp_programs_state_feed_store_anchor(&f->feed, f->state.next_sequence,
            f->kernel.current_state_root) == LXP_OK);
        f->runtime.state_feed = &f->feed.feed;
        CHECK(lxp_programs_bind_state_feed(&f->kernel, f->runtime.state_feed) == LXP_OK);
        CHECK(lxp_programs_state_feed_store_recover(&f->feed, &f->kernel) == LXP_OK);
    }
    return 0;
}

static int terminal_build_send(terminal_fixture *f, lxp_activity *activity,
                               uint8_t *payload, size_t *payload_length)
{
    lxp_send send;
    uint8_t material[144], message[512], preimage[32];
    size_t message_length;
    memset(&send, 0, sizeof(send));
    memcpy(send.from, f->actor_id, 32U);
    memcpy(send.to, f->recipient_id, 32U);
    memcpy(send.asset, f->asset.asset_id, 32U);
    send.amount.lo = 4U;
    send.sequence = f->actor->next_sequence;
    send.expires_at = 100U;
    send.idempotency_key[0] = 0x2AU;
    send.authorization.kind = LXP_AUTH_OWNER;
    send.authorization.network_id = 7U;
    send.authorization.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    memcpy(send.authorization.controller, send.from, 32U);
    memcpy(send.authorization.public_key, f->actor_public_key, 32U);
    memcpy(material, send.from, 32U);
    memcpy(material + 32U, send.to, 32U);
    memcpy(material + 64U, send.asset, 32U);
    CHECK(lxp_u128_to_be(send.amount, material + 96U) == LXP_OK);
    memcpy(material + 112U, send.idempotency_key, 32U);
    CHECK(lxp_hash_context_value(material, sizeof(material), send.context_hash) == LXP_OK);
    memcpy(send.authorization.signed_context_hash, send.context_hash, 32U);
    CHECK(lxp_send_authorization_message(&send, message, sizeof(message), &message_length) == LXP_OK);
    CHECK(lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message, message_length, preimage) == LXP_OK);
    CHECK(terminal_sign(terminal_actor_seed, preimage, 32U, send.authorization.signature) == 0);
    CHECK(lxp_send_encode(&send, payload, 512U, payload_length) == LXP_OK);
    memset(activity, 0, sizeof(*activity));
    activity->protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity->network_id = 7U;
    activity->activity_type = LX_ASSET_SEND;
    activity->actor_did = (lxp_byte_span){terminal_did, sizeof(terminal_did) - 1U};
    activity->authority = (lxp_byte_span){f->actor_public_key, 32U};
    activity->timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity->account_sequence = send.sequence;
    activity->payload = (lxp_byte_span){payload, *payload_length};
    memcpy(activity->idempotency_key, send.idempotency_key, 32U);
    CHECK(lxp_hash_payload(payload, *payload_length, activity->payload_hash) == LXP_OK);
    CHECK(lxp_activity_signing_preimage(activity, preimage) == LXP_OK);
    {
        static uint8_t signature[64];
        CHECK(terminal_sign(terminal_actor_seed, preimage, 32U, signature) == 0);
        activity->signature = (lxp_byte_span){signature, 64U};
    }
    CHECK(lxp_activity_verify_signature(activity) == LXP_OK);
    return 0;
}

static void terminal_execution(const terminal_fixture *f, lxp_kernel_execution *execution,
                               uint64_t global_sequence, uint64_t batch_number)
{
    execution->network_id = 7U;
    execution->batch_number = batch_number;
    execution->batch_timestamp_ms = 10U;
    execution->maximum_timestamp_window = 100U;
    execution->epoch = 1U;
    execution->global_sequence = global_sequence;
    execution->recorded_module_version = lx_asset_module_iface()->abi_version;
    execution->parameter_version = 1U;
    execution->signature_valid = true;
    execution->identities = (lxp_identity_store *)&f->identities;
    execution->authority = (const lxp_authority_resolved *)&f->authority;
    execution->fee_parameters = (const lxp_fee_params *)&f->fees;
    execution->fee_balance = f->actor->balance;
    execution->gas_limit = UINT64_MAX;
    execution->arena = (lxp_arena *)&f->arena;
    execution->sequencer_private_key = executed_sequencer_seed;
}

static int classification_case(void)
{
    CHECK(!lxp_terminal_rejection_applies(LXP_OK));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_IDEMPOTENT_REPLAY));
    CHECK(!lxp_terminal_rejection_applies(LXP_FATAL_INVARIANT));
    CHECK(!lxp_terminal_rejection_applies(LXP_FATAL_REPLAY_DIVERGENCE));
    CHECK(!lxp_terminal_rejection_applies(LXP_FATAL_SUPPLY_MISMATCH));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_BATCH_GAP));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_DA_MISSING));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_IO));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_LOG_CORRUPT));
    CHECK(!lxp_terminal_rejection_applies(LXP_ERR_ARENA_EXHAUSTED));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_TRUNCATED));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_UNKNOWN_ACTIVITY));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_UNKNOWN_DID));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_BAD_SIGNATURE));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_IDENTITY_FROZEN));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_SEQUENCE_MISMATCH));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_INSUFFICIENT_BALANCE));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_OVERFLOW));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_FEE_LIMIT));
    CHECK(lxp_terminal_rejection_applies(LXP_ERR_PROGRAM_REFUSED));
    return 0;
}

static int terminal_wal_record(terminal_fixture *f, const lxp_byte_span *canonical,
    const lxp_kernel_prepared_batch *prepared, const lxp_kernel_execution *execution,
    lxp_byte_span receipt, uint8_t digest[32])
{
    lxp_daemon_batch_wal_input input = {0};
    lxp_batch_header header = {0};
    lxp_batch_body *body = malloc(sizeof(*body));
    lxp_batch_roots roots;
    lxp_merkle_proof proofs[1];
    lxp_byte_span receipts[1];
    lxp_byte_span artifacts[1] = {{NULL, 0U}};
    lxp_byte_span graphs[1] = {{NULL, 0U}};
    uint8_t leaves[1][32], root[32];
    CHECK(body != NULL);
    receipts[0] = receipt;
    CHECK(lxp_batch_roots_compute(&(lxp_batch_root_inputs){canonical, 1U, receipts, 1U,
        lxp_kernel_prepared_batch_events(prepared), 1U, NULL, 0U, NULL, 0U},
        &f->arena, &roots) == LXP_OK);
    header.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    header.network_id = 7U;
    header.epoch = 1U;
    header.batch_number = execution->batch_number;
    header.first_sequence = execution->global_sequence;
    header.last_sequence = execution->global_sequence;
    header.timestamp_ms = execution->batch_timestamp_ms;
    memcpy(header.previous_state_root,
        lxp_kernel_prepared_batch_base_boundary(prepared)->receipt_state_root, 32U);
    memcpy(header.resulting_state_root, lxp_kernel_prepared_batch_final_root(prepared), 32U);
    memcpy(header.activity_merkle_root, roots.activity_merkle_root, 32U);
    memcpy(header.receipt_merkle_root, roots.receipt_merkle_root, 32U);
    memcpy(header.event_merkle_root, roots.event_merkle_root, 32U);
    memcpy(header.oracle_root, roots.oracle_root, 32U);
    memcpy(header.data_availability_root, roots.data_availability_root, 32U);
    memcpy(header.sequencer_id, f->authorization.sequencer_id, 32U);
    CHECK(lxp_merkle_leaf_hash(receipts[0].bytes, receipts[0].length, leaves[0]) == LXP_OK);
    CHECK(lxp_merkle_proof_generate((const uint8_t (*)[32])leaves, 1U, 0U,
        &f->arena, &proofs[0], root) == LXP_OK);
    CHECK(memcmp(root, roots.receipt_merkle_root, 32U) == 0);
    input.protocol_version = header.protocol_version;
    input.network_id = header.network_id;
    input.epoch = header.epoch;
    input.batch_number = header.batch_number;
    input.timestamp_ms = header.timestamp_ms;
    input.parameter_version = execution->parameter_version;
    input.fee_schedule_version = lxp_kernel_prepared_batch_fee_schedule_version(prepared);
    input.metering_schedule_version = lxp_kernel_prepared_batch_metering_schedule_version(prepared);
    input.first_sequence = header.first_sequence;
    input.last_sequence = header.last_sequence;
    input.count = 1U;
    input.base = *lxp_kernel_prepared_batch_base_boundary(prepared);
    input.settled = *lxp_kernel_prepared_batch_final_boundary(prepared);
    memcpy(input.publication_digest, lxp_kernel_prepared_batch_publication_digest(prepared), 32U);
    input.authorization = f->authorization;
    input.activities = canonical;
    input.receipts = receipts;
    input.events = lxp_kernel_prepared_batch_events(prepared);
    input.terminal_payloads = artifacts;
    input.call_graphs = graphs;
    input.receipt_proofs = proofs;
    CHECK(lxp_da_body_from_kernels(&header,
        lxp_kernel_prepared_batch_base_kernel(prepared),
        lxp_kernel_prepared_batch_settled_kernel(prepared),
        canonical, 1U, receipts, 1U, input.events, 1U, NULL, 0U,
        &f->arena, body) == LXP_OK);
    header = body->header;
    input.state_diff = body->state_diff;
    input.recovery_metadata = body->recovery_metadata;
    CHECK(lxp_batch_sign(&header, executed_sequencer_seed, &f->authorization,
        input.header_signature, &f->arena) == LXP_OK);
    CHECK(lxp_batch_header_encode(&header, &f->arena, &input.canonical_header) == LXP_OK);
    CHECK(lxp_daemon_batch_wal_write_prepared(f->directory, &input, digest) == LXP_OK);
    free(body);
    return 0;
}

static int terminal_rejection_case(void)
{
    terminal_fixture *live = malloc(sizeof(*live));
    terminal_fixture *restarted = malloc(sizeof(*restarted));
    lxp_activity activity;
    lxp_activity replay_activity;
    lxp_kernel_execution execution;
    lxp_kernel_execution replay_execution;
    lxp_kernel_execution rejected;
    lxp_kernel_prepared_batch *prepared = NULL;
    lxp_daemon_batch_wal_record *loaded = NULL;
    lxp_daemon_batch_wal_recovery recovery;
    lxp_kernel_batch_boundary live_boundary;
    lxp_kernel_batch_boundary replay_boundary;
    lxp_batch_roots roots, replay_roots;
    lxp_byte_span canonical, replay_canonical, encoded, replay_encoded;
    const lxp_receipt *decoded;
    lxp_receipt replayed;
    lxp_receipt duplicate;
    uint8_t payload[512], replay_payload[512];
    uint8_t canonical_bytes[LXP_MAX_ACTIVITY_BYTES];
    uint8_t receipt_bytes[LXP_MAX_ACTIVITY_BYTES];
    uint8_t batch_id[32], replay_batch_id[32], digest[32];
    uint8_t base_root[32];
    uint64_t first_sequence;
    uint64_t actor_sequence_before;
    lxp_u128 actor_balance_before;
    lxp_u128 recipient_balance_before;
    lxp_u128 treasury_balance_before;
    size_t payload_length = 0U, replay_payload_length = 0U, receipt_length;
    bool present = false;
    CHECK(live != NULL && restarted != NULL);
    CHECK(terminal_fixture_open(live) == 0);
    CHECK(terminal_fixture_open(restarted) == 0);
    CHECK(memcmp(live->kernel.current_state_root,
                 restarted->kernel.current_state_root, 32U) == 0);
    CHECK(terminal_build_send(live, &activity, payload, &payload_length) == 0);
    CHECK(terminal_build_send(restarted, &replay_activity, replay_payload,
                              &replay_payload_length) == 0);
    CHECK(payload_length == replay_payload_length);
    CHECK(memcmp(payload, replay_payload, payload_length) == 0);
    CHECK(lxp_activity_encode(&activity, &live->arena, &canonical) == LXP_OK);
    CHECK(canonical.length <= sizeof(canonical_bytes));
    memcpy(canonical_bytes, canonical.bytes, canonical.length);
    canonical = (lxp_byte_span){canonical_bytes, canonical.length};
    first_sequence = live->state.next_sequence;
    memcpy(base_root, live->kernel.current_state_root, 32U);
    actor_sequence_before = live->actor->next_sequence;
    actor_balance_before = live->actor->balance;
    recipient_balance_before = live->recipient->balance;
    treasury_balance_before = live->treasury->balance;
    memset(&execution, 0, sizeof(execution));
    terminal_execution(live, &execution, first_sequence, 1U);
    CHECK(lxp_daemon_batch_bind_prefix(&canonical, 1U, live->kernel.current_state_root,
        first_sequence, 1U, &live->arena, &execution, &roots, batch_id) == LXP_OK);

    /* An acknowledged activity whose apply refuses becomes a canonical receipt. */
    CHECK(lxp_kernel_prepare_terminal_rejection(&live->kernel, &activity, &execution,
        LXP_ERR_IDENTITY_FROZEN, &prepared) == LXP_OK);
    CHECK(lxp_kernel_prepared_batch_count(prepared) == 1U);
    decoded = lxp_kernel_prepared_batch_receipts(prepared);
    CHECK(decoded[0].result_code == LXP_ERR_IDENTITY_FROZEN);
    CHECK(decoded[0].global_sequence == first_sequence);
    CHECK(decoded[0].protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT);
    CHECK(decoded[0].module_id == LXP_MODULE_ASSET);
    CHECK(decoded[0].module_version == lx_asset_module_iface()->abi_version);
    CHECK(decoded[0].parameter_version == 1U);
    CHECK(decoded[0].fee_charged.hi == 0U && decoded[0].fee_charged.lo == 0U);
    CHECK(decoded[0].effects.count == 0U);
    CHECK(memcmp(decoded[0].previous_state_root, base_root, 32U) == 0);
    CHECK(memcmp(decoded[0].batch_id, batch_id, 32U) == 0);
    CHECK(memcmp(decoded[0].activity_root, roots.activity_merkle_root, 32U) == 0);
    CHECK(memcmp(decoded[0].resulting_state_root, base_root, 32U) != 0);
    CHECK(lxp_receipt_verify(&decoded[0], live->authorization.public_key,
                             &live->arena) == LXP_OK);
    CHECK(lxp_receipt_encode(&decoded[0], true, &live->arena, &encoded) == LXP_OK);
    CHECK(encoded.length <= sizeof(receipt_bytes));
    memcpy(receipt_bytes, encoded.bytes, encoded.length);
    receipt_length = encoded.length;
    encoded = (lxp_byte_span){receipt_bytes, receipt_length};
    /* The prepared batch stages the refusal; the live kernel is untouched. */
    CHECK(live->state.next_sequence == first_sequence);
    CHECK(memcmp(live->kernel.current_state_root, base_root, 32U) == 0);

    /* The refusal reaches the write-ahead log before the kernel commits. */
    CHECK(terminal_wal_record(live, &canonical, prepared, &execution,
                              encoded, digest) == 0);
    CHECK(lxp_daemon_batch_wal_load(live->directory, &live->authorization,
                                    &loaded, &present) == LXP_OK && present);
    CHECK(lxp_daemon_batch_wal_classify(loaded,
        lxp_kernel_prepared_batch_base_boundary(prepared), &recovery) == LXP_OK &&
        recovery == LXP_DAEMON_BATCH_WAL_DISCARD_BASE);
    CHECK(lxp_daemon_batch_wal_view(loaded)->count == 1U);
    CHECK(lxp_daemon_batch_wal_view(loaded)->receipts[0].length == receipt_length);
    CHECK(memcmp(lxp_daemon_batch_wal_view(loaded)->receipts[0].bytes,
                 receipt_bytes, receipt_length) == 0);

    /* The offered global sequence is consumed by the refusal. */
    CHECK(lxp_kernel_commit_prepared_batch(&live->kernel, &live->identities,
                                           prepared, digest) == LXP_OK);
    CHECK(live->state.next_sequence == first_sequence + 1U);
    CHECK(memcmp(live->kernel.current_state_root,
                 decoded[0].resulting_state_root, 32U) == 0);
    CHECK(lxp_kernel_batch_boundary_read(&live->kernel, &live_boundary) == LXP_OK);
    CHECK(lxp_daemon_batch_wal_classify(loaded, &live_boundary, &recovery) == LXP_OK &&
        recovery == LXP_DAEMON_BATCH_WAL_FINALIZE_SETTLED);
    /* No module effect, no fee and no actor sequence is consumed by a refusal. */
    CHECK(live->actor->next_sequence == actor_sequence_before);
    CHECK(live->actor->balance.hi == actor_balance_before.hi &&
          live->actor->balance.lo == actor_balance_before.lo);
    CHECK(live->recipient->balance.hi == recipient_balance_before.hi &&
          live->recipient->balance.lo == recipient_balance_before.lo);
    CHECK(live->treasury->balance.hi == treasury_balance_before.hi &&
          live->treasury->balance.lo == treasury_balance_before.lo);

    /* Publication advances the programs state feed frontier. */
    CHECK(lxp_kernel_finalize_prepared_batch_publication(&live->kernel, &activity,
                                                         prepared, digest) == LXP_OK);
    CHECK(live->feed.scanned_through_sequence == first_sequence);
    CHECK(memcmp(live->feed.head_state_root, decoded[0].resulting_state_root, 32U) == 0);

    /* The recorded refusal is the anti-replay guard for the same activity. */
    memset(&rejected, 0, sizeof(rejected));
    terminal_execution(live, &rejected, live->state.next_sequence, 2U);
    memset(&duplicate, 0, sizeof(duplicate));
    CHECK(lxp_kernel_execute_activity(&live->kernel, &activity, &rejected,
                                      &duplicate) == LXP_ERR_IDEMPOTENT_REPLAY);
    CHECK(duplicate.result_code == LXP_ERR_IDENTITY_FROZEN);
    CHECK(duplicate.global_sequence == first_sequence);
    CHECK(live->state.next_sequence == first_sequence + 1U);

    /* A restarted node replays the same refusal from the same base state. */
    CHECK(lxp_activity_encode(&replay_activity, &restarted->arena,
                              &replay_canonical) == LXP_OK);
    CHECK(replay_canonical.length == canonical.length);
    CHECK(memcmp(replay_canonical.bytes, canonical.bytes, canonical.length) == 0);
    memset(&replay_execution, 0, sizeof(replay_execution));
    terminal_execution(restarted, &replay_execution, first_sequence, 1U);
    CHECK(lxp_daemon_batch_bind_prefix(&replay_canonical, 1U,
        restarted->kernel.current_state_root, first_sequence, 1U,
        &restarted->arena, &replay_execution, &replay_roots,
        replay_batch_id) == LXP_OK);
    CHECK(memcmp(replay_batch_id, batch_id, 32U) == 0);
    /* Outcomes that are not terminal rejections stay fail-stop. */
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &replay_execution, LXP_OK, &replayed) == LXP_ERR_NON_CANONICAL);
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &replay_execution, LXP_ERR_IDEMPOTENT_REPLAY, &replayed) == LXP_ERR_NON_CANONICAL);
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &replay_execution, LXP_FATAL_INVARIANT, &replayed) == LXP_ERR_NON_CANONICAL);
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &replay_execution, LXP_ERR_IO, &replayed) == LXP_ERR_NON_CANONICAL);
    memset(&rejected, 0, sizeof(rejected));
    terminal_execution(restarted, &rejected, first_sequence + 1U, 1U);
    memcpy(rejected.batch_id, replay_execution.batch_id, 32U);
    memcpy(rejected.activity_root, replay_execution.activity_root, 32U);
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &rejected, LXP_ERR_IDENTITY_FROZEN, &replayed) == LXP_FATAL_INVARIANT);
    CHECK(restarted->state.next_sequence == first_sequence);
    CHECK(memcmp(restarted->kernel.current_state_root, base_root, 32U) == 0);
    /* The replayed refusal reproduces the canonical receipt byte for byte. */
    memset(&replayed, 0, sizeof(replayed));
    CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &replay_activity,
        &replay_execution, LXP_ERR_IDENTITY_FROZEN, &replayed) == LXP_OK);
    CHECK(lxp_receipt_encode(&replayed, true, &restarted->arena,
                             &replay_encoded) == LXP_OK);
    CHECK(replay_encoded.length == receipt_length);
    CHECK(memcmp(replay_encoded.bytes, receipt_bytes, receipt_length) == 0);
    CHECK(lxp_kernel_batch_boundary_read(&restarted->kernel, &replay_boundary) == LXP_OK);
    CHECK(replay_boundary.next_sequence == live_boundary.next_sequence);
    CHECK(memcmp(replay_boundary.receipt_state_root,
                 live_boundary.receipt_state_root, 32U) == 0);
    CHECK(memcmp(replay_boundary.canonical_state_root,
                 live_boundary.canonical_state_root, 32U) == 0);
    CHECK(restarted->feed.scanned_through_sequence == first_sequence);
    lxp_daemon_batch_wal_destroy(loaded);
    lxp_kernel_prepared_batch_destroy(prepared);
    CHECK(lxp_history_close(&live->history) == LXP_OK);
    CHECK(lxp_log_close(&live->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&live->canonical_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&live->feed_mutex) == 0);
    CHECK(lxp_state_store_destroy(&live->state) == LXP_OK);
    lx_account_registry_release(&live->accounts);
    CHECK(lxp_history_close(&restarted->history) == LXP_OK);
    CHECK(lxp_log_close(&restarted->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&restarted->canonical_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&restarted->feed_mutex) == 0);
    CHECK(lxp_state_store_destroy(&restarted->state) == LXP_OK);
    lx_account_registry_release(&restarted->accounts);
    free(live->storage);
    free(restarted->storage);
    free(live);
    free(restarted);
    return 0;
}

static int terminal_maintenance_case(void)
{
    terminal_fixture *f = malloc(sizeof(*f));
    terminal_fixture *restarted = malloc(sizeof(*restarted));
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_kernel_prepared_batch *prepared = NULL;
    lxp_programs_occupancy_receipt sweep;
    lxp_batch_maintenance envelope;
    lxp_batch_roots roots;
    lxp_byte_span canonical, maintenance;
    const lxp_receipt *decoded;
    uint8_t payload[512];
    uint8_t canonical_bytes[LXP_MAX_ACTIVITY_BYTES];
    uint8_t batch_id[32], digest[32], settled_root[32];
    uint64_t first_sequence;
    size_t payload_length = 0U;
    CHECK(f != NULL && restarted != NULL);
    CHECK(terminal_fixture_open(f) == 0);
    CHECK(terminal_fixture_open(restarted) == 0);
    CHECK(terminal_build_send(f, &activity, payload, &payload_length) == 0);
    CHECK(lxp_activity_encode(&activity, &f->arena, &canonical) == LXP_OK);
    CHECK(canonical.length <= sizeof(canonical_bytes));
    memcpy(canonical_bytes, canonical.bytes, canonical.length);
    canonical = (lxp_byte_span){canonical_bytes, canonical.length};
    first_sequence = f->state.next_sequence;
    memset(&execution, 0, sizeof(execution));
    terminal_execution(f, &execution, first_sequence, 1U);
    CHECK(lxp_daemon_batch_bind_prefix(&canonical, 1U, f->kernel.current_state_root,
        first_sequence, 1U, &f->arena, &execution, &roots, batch_id) == LXP_OK);
    CHECK(lxp_kernel_prepare_terminal_rejection(&f->kernel, &activity, &execution,
        LXP_ERR_SEQUENCE_MISMATCH, &prepared) == LXP_OK);
    /* The occupancy sweep the batch coordinator runs after every batch runs
     * after a terminal rejection too and chains onto its refusal receipt. */
    CHECK(lxp_kernel_prepare_batch_maintenance(prepared, &activity, &execution) == LXP_OK);
    maintenance = lxp_kernel_prepared_batch_maintenance(prepared);
    CHECK(maintenance.bytes != NULL && maintenance.length != 0U);
    CHECK(lxp_batch_maintenance_decode(maintenance.bytes, maintenance.length,
                                       &envelope) == LXP_OK);
    CHECK(envelope.protocol_version == activity.protocol_version);
    CHECK(envelope.epoch == execution.epoch);
    CHECK(envelope.batch_number == execution.batch_number);
    CHECK(envelope.timestamp_ms == execution.batch_timestamp_ms);
    CHECK(envelope.global_sequence == first_sequence + 1U);
    CHECK(envelope.parameter_version == execution.parameter_version);
    CHECK(lxp_programs_occupancy_receipt_decode(maintenance.bytes,
                                                maintenance.length, &sweep) != LXP_OK);
    CHECK(lxp_programs_occupancy_receipt_decode(envelope.occupancy.bytes,
                                                envelope.occupancy.length, &sweep) == LXP_OK);
    decoded = lxp_kernel_prepared_batch_receipts(prepared);
    CHECK(decoded != NULL);
    CHECK(decoded[0].result_code == LXP_ERR_SEQUENCE_MISMATCH);
    CHECK(decoded[0].global_sequence == first_sequence);
    CHECK(sweep.batch_number == execution.batch_number);
    CHECK(sweep.global_sequence == decoded[0].global_sequence + 1U);
    CHECK(memcmp(sweep.previous_state_root, decoded[0].resulting_state_root, 32U) == 0);
    memcpy(settled_root, lxp_kernel_prepared_batch_final_root(prepared), 32U);
    CHECK(memcmp(settled_root, sweep.resulting_state_root, 32U) == 0);
    {
        lxp_kernel_execution replay_execution = execution;
        lxp_replay_activity_output output;
        lxp_receipt refusal;
        uint8_t previous_root[32], recomputed_root[32];
        uint8_t *tampered = malloc(maintenance.length);
        CHECK(tampered != NULL);
        replay_execution.identities = &restarted->identities;
        replay_execution.authority = &restarted->authority;
        replay_execution.fee_parameters = &restarted->fees;
        replay_execution.arena = &restarted->arena;
        CHECK(lxp_kernel_terminal_rejection(&restarted->kernel, &activity,
            &replay_execution, LXP_ERR_SEQUENCE_MISMATCH, &refusal) == LXP_OK);
        CHECK(memcmp(refusal.resulting_state_root, decoded[0].resulting_state_root, 32U) == 0);
        replay_execution.global_sequence = first_sequence + 1U;
        memcpy(previous_root, restarted->kernel.current_state_root, 32U);
        memcpy(tampered, maintenance.bytes, maintenance.length);
        tampered[maintenance.length - 1U] ^= 1U;
        CHECK(lxp_kernel_finalize_batch_maintenance(&restarted->kernel,
            activity.protocol_version, &replay_execution,
            (lxp_byte_span){tampered, maintenance.length}, &output) == LXP_FATAL_REPLAY_DIVERGENCE);
        CHECK(restarted->state.next_sequence == first_sequence + 1U);
        CHECK(!restarted->journal.open);
        CHECK(lxp_state_root(&restarted->kernel, recomputed_root) == LXP_OK);
        CHECK(memcmp(recomputed_root, previous_root, 32U) == 0);
        CHECK(memcmp(restarted->kernel.current_state_root, previous_root, 32U) == 0);
        CHECK(lxp_kernel_finalize_batch_maintenance(&restarted->kernel,
            activity.protocol_version, &replay_execution, maintenance, &output) == LXP_OK);
        CHECK(output.canonical_receipt.length == maintenance.length);
        CHECK(memcmp(output.canonical_receipt.bytes, maintenance.bytes, maintenance.length) == 0);
        CHECK(restarted->state.next_sequence == first_sequence + 2U);
        CHECK(memcmp(restarted->kernel.current_state_root, settled_root, 32U) == 0);
        free(tampered);
    }
    memcpy(digest, lxp_kernel_prepared_batch_publication_digest(prepared), 32U);
    CHECK(lxp_kernel_commit_prepared_batch(&f->kernel, &f->identities, prepared,
                                           digest) == LXP_OK);
    /* The refusal consumes the offered sequence and the sweep the next one. */
    CHECK(f->state.next_sequence == first_sequence + 2U);
    CHECK(memcmp(f->kernel.current_state_root, settled_root, 32U) == 0);
    {
        lxp_receipt altered = decoded[0];
        altered.timestamp += 1U;
        CHECK(lxp_kernel_finalize_batch_publication_maintenance(&f->kernel, &activity,
            &altered, 1U, maintenance, lxp_kernel_prepared_batch_base_boundary(prepared),
            lxp_kernel_prepared_batch_final_boundary(prepared),
            lxp_kernel_prepared_batch_events(prepared), digest) == LXP_ERR_CONTEXT_MISMATCH);
        CHECK(f->kernel.batch_publication_pending);
        CHECK(f->kernel.pending_batch_publication_index == 0U);
    }
    CHECK(lxp_kernel_finalize_batch_publication_maintenance(&f->kernel, &activity,
        decoded, 1U, maintenance, lxp_kernel_prepared_batch_base_boundary(prepared),
        lxp_kernel_prepared_batch_final_boundary(prepared),
        lxp_kernel_prepared_batch_events(prepared), digest) == LXP_OK);
    CHECK(f->feed.scanned_through_sequence == first_sequence + 1U);
    CHECK(memcmp(f->feed.head_state_root, settled_root, 32U) == 0);
    lxp_kernel_prepared_batch_destroy(prepared);
    CHECK(lxp_history_close(&restarted->history) == LXP_OK);
    CHECK(lxp_log_close(&restarted->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&restarted->canonical_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&restarted->feed_mutex) == 0);
    CHECK(lxp_state_store_destroy(&restarted->state) == LXP_OK);
    lx_account_registry_release(&restarted->accounts);
    free(restarted->storage);
    free(restarted);
    CHECK(lxp_history_close(&f->history) == LXP_OK);
    CHECK(lxp_log_close(&f->feed_log) == LXP_OK);
    CHECK(lxp_log_close(&f->canonical_log) == LXP_OK);
    CHECK(pthread_mutex_destroy(&f->feed_mutex) == 0);
    CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    lx_account_registry_release(&f->accounts);
    free(f->storage);
    free(f);
    return 0;
}

/* Versioned boundary vectors: each line is "VECTOR <name>=<value>" so the
 * versioned boundary table can compare the observed native outcome. */
static void vector_emit(const char *name, long long value)
{
    printf("VECTOR %s=%lld\n", name, value);
}

static void terminal_fixture_close(terminal_fixture *f)
{
    (void)lxp_history_close(&f->history);
    (void)lxp_log_close(&f->feed_log);
    (void)lxp_log_close(&f->canonical_log);
    (void)pthread_mutex_destroy(&f->feed_mutex);
    (void)lxp_state_store_destroy(&f->state);
    lx_account_registry_release(&f->accounts);
    free(f->storage);
    free(f);
}

static int vector_expiry_activity(void)
{
    static const uint64_t points[3] = {99U, 100U, 101U};
    static const char *const names[3] = {"before", "equal", "after"};
    const lxp_timestamp_bound bound = {1U, 100U};
    char name[96];
    size_t i;
    for (i = 0U; i < 3U; ++i) {
        CHECK(snprintf(name, sizeof(name), "expiry.activity.%s", names[i]) > 0);
        vector_emit(name, lxp_activity_check_timestamp_bound(bound, points[i], 100U));
    }
    vector_emit("expiry.activity.not_before_equal",
        lxp_activity_check_timestamp_bound(bound, 1U, 100U));
    vector_emit("expiry.activity.not_before_below",
        lxp_activity_check_timestamp_bound(bound, 0U, 100U));
    return 0;
}

static int vector_expiry_grant(const terminal_fixture *f)
{
    static const uint64_t points[3] = {99U, 100U, 101U};
    static const char *const names[3] = {"before", "equal", "after"};
    const lxp_authority_envelope envelope = {UINT64_C(1) << LXP_MODULE_ASSET, 1U, 10U};
    lxp_authority_grant grant, raw;
    char name[96];
    size_t i;
    /* An owner grant derived from the activity bound [1, 100]. */
    CHECK(lxp_authority_owner_grant(f->identity, f->actor_public_key, &envelope,
        1U, 100U, &grant) == LXP_OK);
    vector_emit("expiry.grant.owner_not_after_stored", (long long)grant.not_after);
    vector_emit("expiry.grant.owner_unbounded",
        lxp_authority_owner_grant(f->identity, f->actor_public_key, &envelope,
            1U, UINT64_MAX, &raw));
    for (i = 0U; i < 3U; ++i) {
        CHECK(snprintf(name, sizeof(name), "expiry.grant.owner_%s", names[i]) > 0);
        vector_emit(name, lxp_authority_is_live(&grant, 0U, points[i], 1U));
    }
    /* A stored grant record whose end is 100 is exclusive at 100. */
    raw = grant;
    raw.not_after = 100U;
    for (i = 0U; i < 3U; ++i) {
        CHECK(snprintf(name, sizeof(name), "expiry.grant.record_%s", names[i]) > 0);
        vector_emit(name, lxp_authority_is_live(&raw, 0U, points[i], 1U));
    }
    return 0;
}

static int vector_expiry_send(terminal_fixture *f, const uint8_t *payload, size_t payload_length)
{
    static const uint64_t points[3] = {99U, 100U, 101U};
    static const char *const names[3] = {"before", "equal", "after"};
    lxp_send send;
    lxp_send_environment environment;
    char name[96];
    size_t i;
    CHECK(lxp_send_decode(payload, payload_length, &send) == LXP_OK);
    CHECK(send.expires_at == 100U);
    /* The first check after expiry is the signed context binding: a broken
     * binding shows that the expiry comparator admitted the timestamp. */
    send.context_hash[0] ^= 1U;
    memset(&environment, 0, sizeof(environment));
    environment.accounts = &f->accounts;
    environment.assets = &f->asset;
    environment.asset_count = 1U;
    environment.network_id = 7U;
    environment.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    for (i = 0U; i < 3U; ++i) {
        environment.batch_timestamp = points[i];
        CHECK(snprintf(name, sizeof(name), "expiry.send.%s", names[i]) > 0);
        vector_emit(name, lxp_send_validate(&send, &environment));
    }
    return 0;
}

static int vector_expiry_receive(terminal_fixture *f)
{
    static const uint64_t points[3] = {99U, 100U, 101U};
    static const char *const names[3] = {"before", "equal", "after"};
    lxp_grant_store *grants = calloc(1U, sizeof(*grants));
    lxp_send_store idempotency;
    lxp_receive_environment environment;
    lxp_receive receive;
    lxp_payer_grant grant;
    lxp_send_receipt_projection projection;
    uint8_t message[384], digest[32];
    size_t length = 0U, i;
    char name[96];
    CHECK(grants != NULL);
    memset(&grant, 0, sizeof(grant));
    memcpy(grant.from, f->actor_id, 32U);
    memcpy(grant.recipient, f->recipient_id, 32U);
    memcpy(grant.asset, f->asset.asset_id, 32U);
    grant.per_draw_maximum = (lxp_u128){0U, 10U};
    grant.allowance = (lxp_u128){0U, 50U};
    grant.expiration = 100U;
    grant.purpose_hash[0] = 8U;
    memcpy(grant.public_key, f->actor_public_key, 32U);
    CHECK(lxp_grant_authorization_message(&grant, message, sizeof(message), &length) == LXP_OK);
    CHECK(lxp_hash_authority(message, length, grant.grant_id) == LXP_OK);
    CHECK(lxp_hash_domain(LXP_DOMAIN_AUTHORITY_HASH, message, length, digest) == LXP_OK);
    CHECK(terminal_sign(terminal_actor_seed, digest, 32U, grant.signature) == 0);
    CHECK(lxp_grant_store_put(grants, &grant, f->actor) == LXP_OK);
    CHECK(lxp_send_store_init(&idempotency, NULL) == LXP_OK);
    memset(&receive, 0, sizeof(receive));
    memcpy(receive.from, f->actor_id, 32U);
    memcpy(receive.to, f->recipient_id, 32U);
    memcpy(receive.asset, f->asset.asset_id, 32U);
    memcpy(receive.grant_id, grant.grant_id, 32U);
    receive.payer_grant = grant;
    receive.idempotency_key[0] = 0x3BU;
    /* One unit above the per-draw maximum: the first check after expiry is
     * the grant scope, so a scope refusal shows expiry admitted the time. */
    receive.amount = (lxp_u128){0U, 11U};
    memset(&environment, 0, sizeof(environment));
    environment.accounts = &f->accounts;
    environment.assets = &f->asset;
    environment.asset_count = 1U;
    environment.grants = grants;
    environment.idempotency = &idempotency;
    environment.global_sequence = 1U;
    environment.network_id = 7U;
    environment.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    for (i = 0U; i < 3U; ++i) {
        environment.batch_timestamp = points[i];
        CHECK(snprintf(name, sizeof(name), "expiry.receive.%s", names[i]) > 0);
        vector_emit(name, lxp_receive_execute(&receive, &environment, &projection));
    }
    vector_emit("expiry.receive.balance_unchanged",
        f->actor->balance.hi == 0U && f->actor->balance.lo == 1000U &&
        lxp_u128_is_zero(f->recipient->balance));
    lxp_send_store_release(&idempotency);
    free(grants);
    return 0;
}

static int vector_resign(lxp_activity *activity, uint16_t version, uint8_t signature[64])
{
    uint8_t preimage[32];
    activity->protocol_version = version;
    /* An unknown version has no signing preimage; admission refuses it. */
    if (!lxp_protocol_version_supported(version)) return 0;
    CHECK(lxp_activity_signing_preimage(activity, preimage) == LXP_OK);
    CHECK(terminal_sign(terminal_actor_seed, preimage, 32U, signature) == 0);
    activity->signature = (lxp_byte_span){signature, 64U};
    return 0;
}

/* One terminal refusal at the given protocol version on a fresh node and on
 * a restarted node, then a duplicate retry of the same activity. */
static int vector_terminal(uint16_t version, lxp_result refusal, const char *label)
{
    terminal_fixture *live = malloc(sizeof(*live));
    terminal_fixture *restarted = malloc(sizeof(*restarted));
    lxp_activity activity, replay;
    lxp_kernel_execution execution, retry;
    lxp_receipt receipt, restart_receipt, duplicate;
    lxp_byte_span encoded, restart_encoded;
    uint8_t payload[512], replay_payload[512], signature[64], replay_signature[64];
    uint8_t base_root[32];
    size_t payload_length = 0U, replay_length = 0U;
    uint64_t first;
    lxp_result status;
    char name[128];
    CHECK(live != NULL && restarted != NULL);
    CHECK(terminal_fixture_open(live) == 0);
    CHECK(terminal_fixture_open(restarted) == 0);
    CHECK(terminal_build_send(live, &activity, payload, &payload_length) == 0);
    CHECK(terminal_build_send(restarted, &replay, replay_payload, &replay_length) == 0);
    CHECK(vector_resign(&activity, version, signature) == 0);
    CHECK(vector_resign(&replay, version, replay_signature) == 0);
    first = live->state.next_sequence;
    memcpy(base_root, live->kernel.current_state_root, 32U);
    memset(&execution, 0, sizeof(execution));
    terminal_execution(live, &execution, first, 1U);
    memset(&receipt, 0, sizeof(receipt));
    status = lxp_kernel_terminal_rejection(&live->kernel, &activity, &execution,
        refusal, &receipt);
#define VECTOR_FIELD(field, value) do { \
        CHECK(snprintf(name, sizeof(name), "terminal.v%u.%s.%s", \
            (unsigned)version, label, field) > 0); \
        vector_emit(name, (long long)(value)); } while (0)
    VECTOR_FIELD("envelope", lxp_activity_check_envelope(&activity, 7U));
    VECTOR_FIELD("status", status);
    VECTOR_FIELD("global_delta", live->state.next_sequence - first);
    VECTOR_FIELD("actor_delta", live->actor->next_sequence - 1U);
    VECTOR_FIELD("actor_balance", live->actor->balance.lo);
    VECTOR_FIELD("recipient_balance", live->recipient->balance.lo);
    VECTOR_FIELD("treasury_balance", live->treasury->balance.lo);
    VECTOR_FIELD("root_unchanged",
        memcmp(live->kernel.current_state_root, base_root, 32U) == 0);
    if (status == LXP_OK) {
        VECTOR_FIELD("receipt_result", receipt.result_code);
        VECTOR_FIELD("receipt_version", receipt.protocol_version);
        VECTOR_FIELD("receipt_sequence_offset", receipt.global_sequence - first);
        VECTOR_FIELD("receipt_fee", receipt.fee_charged.lo | receipt.fee_charged.hi);
        VECTOR_FIELD("receipt_effects", receipt.effects.count);
        VECTOR_FIELD("receipt_root_is_kernel_root",
            memcmp(receipt.resulting_state_root, live->kernel.current_state_root, 32U) == 0);
        /* Restart: the same refusal from the same base reproduces the receipt. */
        retry = execution;
        retry.identities = &restarted->identities;
        retry.authority = &restarted->authority;
        retry.fee_parameters = &restarted->fees;
        retry.arena = &restarted->arena;
        memset(&restart_receipt, 0, sizeof(restart_receipt));
        VECTOR_FIELD("restart_status", lxp_kernel_terminal_rejection(&restarted->kernel,
            &replay, &retry, refusal, &restart_receipt));
        CHECK(lxp_receipt_encode(&receipt, true, &live->arena, &encoded) == LXP_OK);
        CHECK(lxp_receipt_encode(&restart_receipt, true, &restarted->arena,
            &restart_encoded) == LXP_OK);
        VECTOR_FIELD("restart_receipt_identical", encoded.length == restart_encoded.length &&
            memcmp(encoded.bytes, restart_encoded.bytes, encoded.length) == 0);
        VECTOR_FIELD("restart_root_identical", memcmp(live->kernel.current_state_root,
            restarted->kernel.current_state_root, 32U) == 0);
        /* Duplicate retry at the next offered sequence returns the first receipt. */
        memset(&retry, 0, sizeof(retry));
        terminal_execution(live, &retry, live->state.next_sequence, 2U);
        memset(&duplicate, 0, sizeof(duplicate));
        VECTOR_FIELD("duplicate_status",
            lxp_kernel_execute_activity(&live->kernel, &activity, &retry, &duplicate));
        VECTOR_FIELD("duplicate_result", duplicate.result_code);
        VECTOR_FIELD("duplicate_sequence_offset", duplicate.global_sequence - first);
        VECTOR_FIELD("duplicate_global_delta", live->state.next_sequence - first);
        VECTOR_FIELD("duplicate_actor_delta", live->actor->next_sequence - 1U);
    }
#undef VECTOR_FIELD
    terminal_fixture_close(live);
    terminal_fixture_close(restarted);
    return 0;
}

static int versioned_boundary_vectors(void)
{
    static const uint16_t versions[3] = {
        LXP_PROTOCOL_VERSION_LEGACY, LXP_PROTOCOL_VERSION_OCCUPANCY,
        LXP_PROTOCOL_VERSION_STATE_COMMITMENT};
    terminal_fixture *f = malloc(sizeof(*f));
    lxp_activity activity;
    uint8_t payload[512];
    size_t payload_length = 0U, i;
    uint16_t version;
    char name[64];
    for (version = 0U; version <= 4U; ++version) {
        CHECK(snprintf(name, sizeof(name), "version.supported.%u", (unsigned)version) > 0);
        vector_emit(name, lxp_protocol_version_supported(version));
    }
    CHECK(f != NULL);
    CHECK(terminal_fixture_open(f) == 0);
    CHECK(terminal_build_send(f, &activity, payload, &payload_length) == 0);
    CHECK(vector_expiry_activity() == 0);
    CHECK(vector_expiry_grant(f) == 0);
    CHECK(vector_expiry_send(f, payload, payload_length) == 0);
    CHECK(vector_expiry_receive(f) == 0);
    terminal_fixture_close(f);
    for (i = 0U; i < 3U; ++i) {
        CHECK(vector_terminal(versions[i], LXP_ERR_IDENTITY_FROZEN, "authority") == 0);
        CHECK(vector_terminal(versions[i], LXP_ERR_INSUFFICIENT_BALANCE, "module") == 0);
        CHECK(vector_terminal(versions[i], LXP_ERR_FEE_LIMIT, "fee") == 0);
        CHECK(vector_terminal(versions[i], LXP_ERR_EXPIRED, "expired") == 0);
    }
    CHECK(vector_terminal(4U, LXP_ERR_IDENTITY_FROZEN, "authority") == 0);
    CHECK(vector_terminal(0U, LXP_ERR_IDENTITY_FROZEN, "authority") == 0);
    return 0;
}

/* Two-class sequence and fee policy at or above the activation parameter
 * version. Rows print as "TWO_CLASS_ROW" so the harness can compare them
 * with the archived rejection policy; they are not boundary vectors. */
typedef struct two_class_snapshot {
    uint64_t global_sequence;
    uint64_t identity_sequence;
    uint64_t idempotency_count;
    lxp_u128 actor_balance;
    lxp_u128 recipient_balance;
    lxp_u128 treasury_balance;
    uint8_t root[32];
} two_class_snapshot;

static void two_class_capture(const terminal_fixture *f, two_class_snapshot *s)
{
    s->global_sequence = f->state.next_sequence;
    s->identity_sequence = f->identity->next_sequence;
    s->idempotency_count = f->state.idempotency_count;
    s->actor_balance = f->actor->balance;
    s->recipient_balance = f->recipient->balance;
    s->treasury_balance = f->treasury->balance;
    memcpy(s->root, f->kernel.current_state_root, 32U);
}

static int two_class_same(const terminal_fixture *f, const two_class_snapshot *s)
{
    CHECK(f->state.next_sequence == s->global_sequence);
    CHECK(f->identity->next_sequence == s->identity_sequence);
    CHECK(f->state.idempotency_count == s->idempotency_count);
    CHECK(lxp_u128_cmp(f->actor->balance, s->actor_balance) == 0);
    CHECK(lxp_u128_cmp(f->recipient->balance, s->recipient_balance) == 0);
    CHECK(lxp_u128_cmp(f->treasury->balance, s->treasury_balance) == 0);
    CHECK(memcmp(f->kernel.current_state_root, s->root, 32U) == 0);
    return 0;
}

static int two_class_resign(lxp_activity *activity, uint8_t signature[64])
{
    uint8_t preimage[32];
    CHECK(lxp_hash_payload(activity->payload.bytes, activity->payload.length,
                           activity->payload_hash) == LXP_OK);
    CHECK(lxp_activity_signing_preimage(activity, preimage) == LXP_OK);
    CHECK(terminal_sign(terminal_actor_seed, preimage, 32U, signature) == 0);
    activity->signature = (lxp_byte_span){signature, 64U};
    return 0;
}

static int two_class_row(uint32_t version, lxp_fee_stage stage, lxp_result result)
{
    lxp_fee_transition t;
    lxp_result status = lxp_fee_transition_lookup(version, stage, result, &t);
    if (status != LXP_OK) {
        CHECK(status == LXP_ERR_NON_CANONICAL);
        printf("TWO_CLASS_ROW version=%u stage=%d result=%d canonical=0\n",
               (unsigned)version, (int)stage, (int)result);
        return 0;
    }
    printf("TWO_CLASS_ROW version=%u stage=%d result=%d canonical=1 actor=%u "
           "global=%u fee=%d effects=%d receipt=%d retry=%d\n",
           (unsigned)version, (int)stage, (int)result,
           (unsigned)t.actor_sequence, (unsigned)t.global_sequence,
           t.charge_fee ? 1 : 0, t.module_effects ? 1 : 0,
           (int)t.receipt, (int)t.retry);
    CHECK(t.actor_sequence <= 1U && t.global_sequence <= 1U);
    CHECK(!t.module_effects || (result == LXP_OK && t.charge_fee));
    if (stage != LXP_FEE_STAGE_EXECUTION) {
        CHECK(t.actor_sequence == 0U && !t.charge_fee && !t.module_effects);
        if (lxp_fee_two_class_active(version)) CHECK(t.global_sequence == 0U);
    } else {
        CHECK(t.actor_sequence == 1U && t.global_sequence == 1U && t.charge_fee);
        CHECK(t.receipt == (result == LXP_OK ? LXP_FEE_RECEIPT_SUCCESS :
                                               LXP_FEE_RECEIPT_FAILURE));
    }
    return 0;
}

static int two_class_table_case(void)
{
#define TWO_CLASS_CODE(name, value) name,
    static const lxp_result codes[] = { LXP_RESULT_CODE_LIST(TWO_CLASS_CODE) };
#undef TWO_CLASS_CODE
    static const uint32_t versions[2] = {
        1U, (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION};
    static const lxp_result admission[] = {
        LXP_ERR_TRUNCATED, LXP_ERR_MALFORMED_ENVELOPE, LXP_ERR_WRONG_NETWORK,
        LXP_ERR_VERSION_UNSUPPORTED, LXP_ERR_BAD_SIGNATURE,
        LXP_ERR_IDENTITY_FROZEN, LXP_ERR_SEQUENCE_GAP, LXP_ERR_SEQUENCE_REUSED,
        LXP_ERR_EXPIRED, LXP_ERR_FEE_UNPAYABLE};
    lxp_fee_transition t;
    size_t v, i;
    int stage;
    CHECK(!lxp_fee_two_class_active(0U));
    CHECK(!lxp_fee_two_class_active(1U));
    CHECK(lxp_fee_two_class_active((uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION));
    CHECK(lxp_fee_transition_lookup(0U, LXP_FEE_STAGE_EXECUTION, LXP_OK, &t) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_EXECUTION, LXP_OK, NULL) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lxp_fee_transition_lookup(2U, (lxp_fee_stage)4, LXP_ERR_EXPIRED, &t) ==
          LXP_ERR_NON_CANONICAL);
    for (v = 0U; v < 2U; ++v)
        for (stage = LXP_FEE_STAGE_SUBMISSION; stage <= LXP_FEE_STAGE_EXECUTION; ++stage)
            for (i = 0U; i < sizeof(codes) / sizeof(codes[0]); ++i)
                CHECK(two_class_row(versions[v], (lxp_fee_stage)stage, codes[i]) == 0);
    for (i = 0U; i < sizeof(admission) / sizeof(admission[0]); ++i) {
        CHECK(lxp_fee_ordering_refusal(admission[i]));
        CHECK(lxp_terminal_rejection_applies(admission[i]));
        CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_ORDERING, admission[i], &t) == LXP_OK);
        CHECK(t.receipt == LXP_FEE_RECEIPT_QUEUE_DISPOSITION && t.global_sequence == 0U &&
              t.actor_sequence == 0U && !t.charge_fee && t.retry == LXP_FEE_RETRY_RESUBMIT);
        CHECK(lxp_fee_transition_lookup(1U, LXP_FEE_STAGE_ORDERING, admission[i], &t) == LXP_OK);
        CHECK(t.receipt == LXP_FEE_RECEIPT_REFUSAL && t.global_sequence == 1U &&
              t.actor_sequence == 0U && !t.charge_fee);
    }
    CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_ORDERING,
          LXP_ERR_IDEMPOTENT_REPLAY, &t) == LXP_OK);
    CHECK(t.receipt == LXP_FEE_RECEIPT_QUEUE_DISPOSITION && t.retry == LXP_FEE_RETRY_NONE);
    CHECK(lxp_fee_transition_lookup(1U, LXP_FEE_STAGE_ORDERING,
          LXP_ERR_IDEMPOTENT_REPLAY, &t) == LXP_ERR_NON_CANONICAL);
    CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_ORDERING, LXP_OK, &t) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_EXECUTION,
          LXP_ERR_IDEMPOTENT_REPLAY, &t) == LXP_ERR_NON_CANONICAL);
    CHECK(lxp_fee_transition_lookup(2U, LXP_FEE_STAGE_EXECUTION,
          LXP_FATAL_INVARIANT, &t) == LXP_ERR_NON_CANONICAL);
    printf("two-class table case passed\n");
    return 0;
}

/* The old terminal refusal is unavailable once the new table is active. */
static int two_class_terminal_case(void)
{
    terminal_fixture *f = malloc(sizeof(*f));
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_kernel_prepared_batch *prepared = NULL;
    lxp_receipt receipt;
    lxp_byte_span canonical;
    lxp_batch_roots roots;
    two_class_snapshot before;
    uint8_t payload[512], batch_id[32];
    size_t payload_length = 0U;
    CHECK(f != NULL);
    CHECK(terminal_fixture_open(f) == 0);
    CHECK(terminal_build_send(f, &activity, payload, &payload_length) == 0);
    CHECK(lxp_activity_encode(&activity, &f->arena, &canonical) == LXP_OK);
    memset(&execution, 0, sizeof(execution));
    terminal_execution(f, &execution, f->state.next_sequence, 1U);
    execution.parameter_version = (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION;
    CHECK(lxp_daemon_batch_bind_prefix(&canonical, 1U, f->kernel.current_state_root,
        f->state.next_sequence, 1U, &f->arena, &execution, &roots, batch_id) == LXP_OK);
    two_class_capture(f, &before);
    memset(&receipt, 0, sizeof(receipt));
    CHECK(lxp_kernel_terminal_rejection(&f->kernel, &activity, &execution,
        LXP_ERR_IDENTITY_FROZEN, &receipt) == LXP_ERR_VERSION_UNSUPPORTED);
    CHECK(two_class_same(f, &before) == 0);
    CHECK(lxp_kernel_prepare_terminal_rejection(&f->kernel, &activity, &execution,
        LXP_ERR_EXPIRED, &prepared) == LXP_ERR_VERSION_UNSUPPORTED);
    CHECK(prepared == NULL);
    CHECK(two_class_same(f, &before) == 0);
    terminal_fixture_close(f);
    printf("two-class terminal case passed\n");
    return 0;
}

enum {
    TWO_CLASS_MALFORMED = 0,
    TWO_CLASS_WRONG_NETWORK,
    TWO_CLASS_BAD_SIGNATURE,
    TWO_CLASS_SEQUENCE_GAP,
    TWO_CLASS_SEQUENCE_REUSED,
    TWO_CLASS_EXPIRED,
    TWO_CLASS_FEE_UNPAYABLE,
    TWO_CLASS_LATE_REVOCATION,
    TWO_CLASS_LATE_EXPIRY,
    TWO_CLASS_LATE_BALANCE_LOSS,
    TWO_CLASS_ADMISSION_COUNT
};

static const char *const two_class_admission_names[TWO_CLASS_ADMISSION_COUNT] = {
    "malformed", "wrong_network", "bad_signature", "sequence_gap",
    "sequence_reused", "expired", "fee_unpayable", "late_revocation",
    "late_expiry", "late_balance_loss"};

/* One refusal before ordering: no sequence, no fee, no state change, and the
 * queue disposition is the only outcome the table allows. */
static int two_class_admission_case(int kind)
{
    terminal_fixture *f = malloc(sizeof(*f));
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_admission_context queued;
    lxp_admission_result admitted;
    lxp_fee_transition t;
    lxp_receipt receipt;
    two_class_snapshot before;
    uint8_t payload[512], signature[64];
    size_t payload_length = 0U;
    lxp_send decoded;
    lxp_result expected = LXP_OK, status;
    CHECK(f != NULL);
    CHECK(terminal_fixture_open(f) == 0);
    CHECK(terminal_build_send(f, &activity, payload, &payload_length) == 0);
    f->identity->next_sequence = f->actor->next_sequence;
    memset(&execution, 0, sizeof(execution));
    terminal_execution(f, &execution, f->state.next_sequence, 1U);
    execution.parameter_version = (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION;
    activity.fee_limit = (lxp_u128){0U, 2U};
    CHECK(two_class_resign(&activity, signature) == 0);
    /* Every case is admissible at queue time. */
    queued = (lxp_admission_context){7U, 10U, 100U, f->identity->next_sequence,
        true, false, true};
    admitted = lxp_admit_activity(&activity, &queued);
    CHECK(admitted.result_code == LXP_OK);
    switch (kind) {
    case TWO_CLASS_MALFORMED:
        --activity.payload.length;
        CHECK(two_class_resign(&activity, signature) == 0);
        expected = lxp_send_decode(payload, activity.payload.length, &decoded);
        break;
    case TWO_CLASS_WRONG_NETWORK:
        execution.network_id = 8U;
        expected = LXP_ERR_WRONG_NETWORK;
        break;
    case TWO_CLASS_BAD_SIGNATURE:
        execution.signature_valid = false;
        expected = LXP_ERR_BAD_SIGNATURE;
        break;
    case TWO_CLASS_SEQUENCE_GAP:
        activity.account_sequence += 1U;
        CHECK(two_class_resign(&activity, signature) == 0);
        expected = LXP_ERR_SEQUENCE_GAP;
        break;
    case TWO_CLASS_SEQUENCE_REUSED:
        f->identity->next_sequence += 1U;
        expected = LXP_ERR_SEQUENCE_REUSED;
        break;
    case TWO_CLASS_EXPIRED:
    case TWO_CLASS_LATE_EXPIRY:
        execution.batch_timestamp_ms = 101U;
        expected = LXP_ERR_EXPIRED;
        break;
    case TWO_CLASS_FEE_UNPAYABLE:
    case TWO_CLASS_LATE_BALANCE_LOSS:
        execution.fee_balance = (lxp_u128){0U, 1U};
        expected = LXP_ERR_FEE_UNPAYABLE;
        break;
    case TWO_CLASS_LATE_REVOCATION:
        f->identity->status = LXP_IDENTITY_FROZEN;
        expected = LXP_ERR_IDENTITY_FROZEN;
        break;
    default:
        CHECK(false);
    }
    CHECK(expected != LXP_OK);
    two_class_capture(f, &before);
    memset(&receipt, 0, sizeof(receipt));
    status = lxp_kernel_execute_activity(&f->kernel, &activity, &execution, &receipt);
    if (status != expected)
        fprintf(stderr, "two-class admission %s: expected %d got %d\n",
                two_class_admission_names[kind], (int)expected, (int)status);
    CHECK(status == expected);
    CHECK(two_class_same(f, &before) == 0);
    CHECK(!f->kernel.publication_poisoned);
    CHECK(lxp_fee_transition_lookup(execution.parameter_version,
        LXP_FEE_STAGE_ORDERING, status, &t) == LXP_OK);
    CHECK(t.receipt == LXP_FEE_RECEIPT_QUEUE_DISPOSITION);
    CHECK(t.actor_sequence == 0U && t.global_sequence == 0U && !t.charge_fee &&
          !t.module_effects);
    CHECK(lxp_fee_transition_lookup(execution.parameter_version,
        LXP_FEE_STAGE_SUBMISSION, status, &t) == LXP_OK);
    CHECK(t.actor_sequence == 0U && t.global_sequence == 0U && !t.charge_fee);
    /* The refused activity is never terminal-refused into a sequence. */
    CHECK(lxp_kernel_terminal_rejection(&f->kernel, &activity, &execution,
        status, &receipt) == LXP_ERR_VERSION_UNSUPPORTED);
    CHECK(two_class_same(f, &before) == 0);
    terminal_fixture_close(f);
    printf("two-class admission %s passed\n", two_class_admission_names[kind]);
    return 0;
}

static int two_class_failure_open(terminal_fixture *f, lxp_activity *activity,
    uint8_t payload[512], uint8_t signature[64], lxp_kernel_execution *execution)
{
    size_t payload_length = 0U;
    CHECK(terminal_fixture_open(f) == 0);
    CHECK(lxp_programs_bind_fee_transaction(&f->kernel) == LXP_OK);
    f->fees.base_fee = (lxp_u128){0U, 3U};
    f->identity->next_sequence = f->actor->next_sequence;
    CHECK(terminal_build_send(f, activity, payload, &payload_length) == 0);
    activity->fee_limit = (lxp_u128){0U, 2U};
    CHECK(two_class_resign(activity, signature) == 0);
    memset(execution, 0, sizeof(*execution));
    terminal_execution(f, execution, f->state.next_sequence, 1U);
    execution->parameter_version = (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION;
    return 0;
}

/* An ordered failure consumes the actor sequence once, charges the
 * deterministic fee capped at fee_limit, commits a failure receipt with zero
 * module effects, and neither retry nor replay charges it again. */
static int two_class_failure_case(void)
{
    terminal_fixture *live = malloc(sizeof(*live));
    terminal_fixture *restarted = malloc(sizeof(*restarted));
    lxp_activity activity, replay;
    lxp_kernel_execution execution, retry, replay_execution;
    lxp_receipt receipt, duplicate, replayed;
    lxp_fee_transition t;
    lxp_byte_span encoded, replay_encoded;
    two_class_snapshot before, after;
    uint8_t payload[512], replay_payload[512], signature[64], replay_signature[64];
    CHECK(live != NULL && restarted != NULL);
    CHECK(two_class_failure_open(live, &activity, payload, signature, &execution) == 0);
    CHECK(two_class_failure_open(restarted, &replay, replay_payload, replay_signature,
                                 &replay_execution) == 0);
    two_class_capture(live, &before);
    memset(&receipt, 0, sizeof(receipt));
    CHECK(lxp_kernel_execute_activity(&live->kernel, &activity, &execution,
                                      &receipt) == LXP_OK);
    CHECK(receipt.result_code == LXP_ERR_FEE_LIMIT);
    CHECK(lxp_fee_transition_lookup(execution.parameter_version,
        LXP_FEE_STAGE_EXECUTION, receipt.result_code, &t) == LXP_OK);
    CHECK(t.receipt == LXP_FEE_RECEIPT_FAILURE && t.charge_fee && !t.module_effects);
    CHECK(receipt.parameter_version == execution.parameter_version);
    CHECK(receipt.global_sequence == before.global_sequence);
    CHECK(receipt.fee_charged.hi == 0U && receipt.fee_charged.lo == 2U);
    CHECK(receipt.effects.count == 0U);
    CHECK(live->state.next_sequence - before.global_sequence == t.global_sequence);
    CHECK(live->identity->next_sequence - before.identity_sequence == t.actor_sequence);
    CHECK(live->actor->balance.lo == before.actor_balance.lo - 2U);
    CHECK(live->treasury->balance.lo == before.treasury_balance.lo + 2U);
    CHECK(lxp_u128_cmp(live->recipient->balance, before.recipient_balance) == 0);
    CHECK(memcmp(live->kernel.current_state_root, receipt.resulting_state_root, 32U) == 0);
    CHECK(lxp_receipt_verify(&receipt, live->authorization.public_key,
                             &live->arena) == LXP_OK);
    two_class_capture(live, &after);
    /* Retry at the next sequence is a duplicate: same receipt, no charge. */
    memset(&retry, 0, sizeof(retry));
    terminal_execution(live, &retry, live->state.next_sequence, 2U);
    retry.parameter_version = execution.parameter_version;
    memset(&duplicate, 0, sizeof(duplicate));
    CHECK(lxp_kernel_execute_activity(&live->kernel, &activity, &retry,
                                      &duplicate) == LXP_ERR_IDEMPOTENT_REPLAY);
    CHECK(duplicate.result_code == LXP_ERR_FEE_LIMIT);
    CHECK(duplicate.global_sequence == before.global_sequence);
    CHECK(duplicate.fee_charged.lo == 2U);
    CHECK(two_class_same(live, &after) == 0);
    CHECK(lxp_fee_transition_lookup(retry.parameter_version, LXP_FEE_STAGE_ORDERING,
        LXP_ERR_IDEMPOTENT_REPLAY, &t) == LXP_OK);
    CHECK(t.receipt == LXP_FEE_RECEIPT_QUEUE_DISPOSITION && !t.charge_fee &&
          t.global_sequence == 0U && t.retry == LXP_FEE_RETRY_NONE);
    /* Replay on a restarted node from the same base reproduces the receipt
     * and the single charge byte for byte. */
    memset(&replayed, 0, sizeof(replayed));
    CHECK(lxp_kernel_execute_activity(&restarted->kernel, &replay, &replay_execution,
                                      &replayed) == LXP_OK);
    CHECK(lxp_receipt_encode(&receipt, true, &live->arena, &encoded) == LXP_OK);
    CHECK(lxp_receipt_encode(&replayed, true, &restarted->arena, &replay_encoded) == LXP_OK);
    CHECK(encoded.length == replay_encoded.length);
    CHECK(memcmp(encoded.bytes, replay_encoded.bytes, encoded.length) == 0);
    CHECK(restarted->actor->balance.lo == live->actor->balance.lo);
    CHECK(restarted->treasury->balance.lo == live->treasury->balance.lo);
    CHECK(restarted->identity->next_sequence == live->identity->next_sequence);
    CHECK(memcmp(restarted->kernel.current_state_root,
                 live->kernel.current_state_root, 32U) == 0);
    terminal_fixture_close(live);
    terminal_fixture_close(restarted);
    printf("two-class failure case passed\n");
    return 0;
}

static int two_class_disposition_case(void)
{
    lxp_queue_disposition disposition, decoded, invalid;
    uint8_t bytes[LXP_QUEUE_DISPOSITION_BYTES], tampered[LXP_QUEUE_DISPOSITION_BYTES];
    size_t i;
    memset(&disposition, 0, sizeof(disposition));
    disposition.admission_order = 41U;
    disposition.ordering_sequence = 9U;
    disposition.result_code = LXP_ERR_EXPIRED;
    disposition.parameter_version = (uint32_t)LXP_FEE_TWO_CLASS_PARAMETER_VERSION;
    for (i = 0U; i < 32U; ++i) {
        disposition.activity_id[i] = (uint8_t)(i + 1U);
        disposition.idempotency_key[i] = (uint8_t)(0xA0U + i);
    }
    CHECK(lxp_queue_disposition_encode(&disposition, bytes) == LXP_OK);
    CHECK(lxp_queue_disposition_header(bytes, sizeof(bytes)));
    CHECK(!lxp_queue_disposition_header(bytes, LXP_QUEUE_DISPOSITION_HEADER_BYTES - 1U));
    memset(&decoded, 0, sizeof(decoded));
    CHECK(lxp_queue_disposition_decode(bytes, sizeof(bytes), &decoded) == LXP_OK);
    CHECK(decoded.admission_order == disposition.admission_order);
    CHECK(decoded.ordering_sequence == disposition.ordering_sequence);
    CHECK(decoded.result_code == disposition.result_code);
    CHECK(decoded.parameter_version == disposition.parameter_version);
    CHECK(memcmp(decoded.activity_id, disposition.activity_id, 32U) == 0);
    CHECK(memcmp(decoded.idempotency_key, disposition.idempotency_key, 32U) == 0);
    CHECK(lxp_queue_disposition_decode(bytes, sizeof(bytes) - 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    for (i = 0U; i < sizeof(bytes); ++i) {
        memcpy(tampered, bytes, sizeof(bytes));
        tampered[i] ^= 1U;
        CHECK(lxp_queue_disposition_decode(tampered, sizeof(tampered), &decoded) != LXP_OK);
    }
    invalid = disposition;
    invalid.parameter_version = 1U;
    CHECK(lxp_queue_disposition_encode(&invalid, bytes) != LXP_OK);
    invalid = disposition;
    invalid.result_code = LXP_OK;
    CHECK(lxp_queue_disposition_encode(&invalid, bytes) != LXP_OK);
    invalid = disposition;
    invalid.result_code = LXP_FATAL_INVARIANT;
    CHECK(lxp_queue_disposition_encode(&invalid, bytes) != LXP_OK);
    invalid = disposition;
    invalid.admission_order = 0U;
    CHECK(lxp_queue_disposition_encode(&invalid, bytes) != LXP_OK);
    printf("two-class disposition case passed\n");
    return 0;
}

static int two_class_cases(void)
{
    int kind;
    if (two_class_table_case() != 0) return 1;
    if (two_class_terminal_case() != 0) return 1;
    for (kind = 0; kind < TWO_CLASS_ADMISSION_COUNT; ++kind)
        if (two_class_admission_case(kind) != 0) return 1;
    if (two_class_failure_case() != 0) return 1;
    if (two_class_disposition_case() != 0) return 1;
    return 0;
}

#ifndef LXP_TEST_TERMINAL_REJECTION_MAIN
#define LXP_TEST_TERMINAL_REJECTION_MAIN main
#endif
int LXP_TEST_TERMINAL_REJECTION_MAIN(void)
{
    if (classification_case() != 0) return 1;
    if (terminal_rejection_case() != 0) return 1;
    if (terminal_maintenance_case() != 0) return 1;
    if (versioned_boundary_vectors() != 0) return 1;
    if (two_class_cases() != 0) return 1;
    printf("terminal rejection tests passed\n");
    return 0;
}
