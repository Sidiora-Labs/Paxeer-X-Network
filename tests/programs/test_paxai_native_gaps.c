#define _GNU_SOURCE

#include "layerx/programs.h"

#include "layerx/lxp_activity.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_genesis.h"
#include "layerx/lxp_handover.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_history.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_module_ctx.h"
#include "layerx/lxp_protocol.h"
#include "layerx/lxp_receipt.h"
#include "layerx/lxp_state_proof.h"
#include "layerx/lxp_storage.h"

#include "lxp_daemon_lni_internal.h"

#include <openssl/evp.h>

#include <dirent.h>
#include <errno.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

enum {
    ACTORS = 4,
    OWNER = 0,
    CONTROLLER = 3,
    NETWORK_ID = 7,
    LNI_MAJOR = 1,
    LNI_MINOR = 8,
    NODE_INFO_REQUEST = 1,
    NODE_INFO_RESPONSE = 2,
    ERROR_RESPONSE = 25,
    RESERVE = 52,
    CANCEL = 56,
    INSTALL = 58,
    ENVELOPE_FIXED_BYTES = 22,
    LNI_PROOF_BYTES = 96,
    ID_BODY_BYTES = 10,
    NODE_INFO_BYTES = 65536,
    NODE_INFO_FIXED_BYTES = 93,
    CAPABILITY_MAX_BYTES = 64,
    OWNER_SCRATCH_BYTES = 2 * 1024 * 1024,
    FLOOR_BLOBS = 64,
    FLOOR_BYTES = 8388608,
    FLOOR_KV = 16,
    MAX_WORK_LIFETIME = 8,
    PROOF_CAPACITY = 6144,
    REPLAY_DOMAIN_BYTES = 29,
    REPLAY_BODY_BYTES = 365,
    WITNESS_BYTES = 600,
    ORIGIN_SEQUENCE = 1,
    CUTOFF_SEQUENCE = 2,
    V4_ACTIVITY_TYPES = 10,
    CONTROL_LAST_CUTOFF = 179,
    CONTROL_RETIREMENTS = 187,
    CONTROL_BLOBS = 195,
    CONTROL_BYTES = 203,
    CONTROL_REPLAYS = 211,
    REC_ID = 0,
    REC_STATE = 9,
    REC_DIGEST = 10
};

_Static_assert((int)REPLAY_DOMAIN_BYTES + (int)REPLAY_BODY_BYTES == 394,
               "replay record size changed");

typedef struct node {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    uint64_t parameters;
    uint64_t height;
    uint8_t keys[ACTORS][32];
    uint8_t principals[ACTORS][32];
    uint8_t account_ids[ACTORS][32];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_receipt receipt;
    bool live;
} node;

typedef struct reply {
    uint16_t tag;
    uint8_t payload[1U + LXP_CAPACITY_RECORD_BYTES + LXP_CAPACITY_OBSERVATION_BYTES];
    size_t length;
    uint8_t refusal_class;
    lxp_result refusal;
} reply;

typedef struct cap_request {
    uint8_t kind;
    uint8_t activity[32];
    uint8_t idempotency[32];
    const char *actor;
    uint32_t blobs;
    uint64_t bytes;
    uint32_t kv;
    uint64_t sequence;
    uint8_t root[32];
    uint64_t lifetime;
} cap_request;

typedef struct connection {
    int descriptor;
    int server_descriptor;
    pthread_t thread;
    lxp_result status;
    bool open;
} connection;

typedef struct lni_fixture {
    lxp_daemon_protocol_owner owner;
    lxp_daemon_receipt_authority_store receipt_authority;
    lxp_history history;
    lxp_log canonical_log;
    lxp_arena scratch;
    uint8_t *scratch_bytes;
    lxp_daemon daemon;
    lxp_daemon_lni_server server;
    char socket_directory[64];
    char admission_directory[64];
    char socket_path[LXP_DAEMON_LNI_SOCKET_PATH_BYTES];
    size_t applied;
    bool daemon_started;
    bool lni_started;
    bool owner_ready;
    connection link;
    uint8_t node_info[NODE_INFO_BYTES];
    size_t node_info_length;
} lni_fixture;

static const char *const dids[ACTORS] = {
    "did:lxp:paxai-owner", "did:lxp:paxai-treasury", "did:lxp:paxai-outsider",
    "did:lxp:paxai-controller"
};
static const uint8_t seeds[ACTORS][32] = {{0x63U}, {0x64U}, {0x65U}, {0x67U}};
static const uint8_t sequencer_seed[32] = {0x66U};
static const uint8_t archive_seed_a[32] = {0x68U};
static const uint8_t archive_seed_b[32] = {0x69U};
static const uint8_t asset_id[32] = {9U};
static const uint8_t replay_domain[REPLAY_DOMAIN_BYTES] =
    "LXP/program-replay-native/v1";
static const char request_domain[] = "LayerX/storage-capacity-request/v1";
static const char response_domain[] = "LayerX/storage-capacity-response/v1";
static const char capacity_prefix[] = "capacity:";

static node *primary, *scratch_node;
static lni_fixture lni;
static lxp_state_witness witness;
static uint8_t retirement_payload[LXP_PROGRAMS_RETIREMENT_HEADER_BYTES +
                                  LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES +
                                  PROOF_CAPACITY];
static uint8_t sequencer_public[32];
static uint8_t program_id[32];
static uint8_t profile_digest[32];
static uint64_t correlation = 1U;
static size_t passed, failed;

#define CHECK(condition, name) \
    do { \
        if (!(condition)) { \
            (void)printf("NATIVE_GAPS_FAIL %s line %d: %s\n", name, __LINE__, \
                         #condition); \
            (void)fflush(stdout); \
            return 1; \
        } \
    } while (0)

static int pass(const char *name)
{
    ++passed;
    (void)printf("NATIVE_GAPS_CASE %s\n", name);
    (void)fflush(stdout);
    return 0;
}

static int tally(int result)
{
    if (result != 0) ++failed;
    return result;
}

static void put_u16(uint8_t *p, uint16_t v)
{
    p[0] = (uint8_t)(v >> 8U);
    p[1] = (uint8_t)v;
}

static void put_u32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)(v >> 24U);
    p[1] = (uint8_t)(v >> 16U);
    p[2] = (uint8_t)(v >> 8U);
    p[3] = (uint8_t)v;
}

static void put_u64(uint8_t *p, uint64_t v)
{
    put_u32(p, (uint32_t)(v >> 32U));
    put_u32(p + 4U, (uint32_t)v);
}

static uint16_t get_u16(const uint8_t *p)
{
    return (uint16_t)(((uint16_t)p[0] << 8U) | p[1]);
}

static uint32_t get_u32(const uint8_t *p)
{
    return ((uint32_t)p[0] << 24U) | ((uint32_t)p[1] << 16U) |
           ((uint32_t)p[2] << 8U) | p[3];
}

static uint64_t get_u64(const uint8_t *p)
{
    return ((uint64_t)get_u32(p) << 32U) | get_u32(p + 4U);
}

static void hex_encode(const uint8_t *bytes, size_t length, char *out)
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    for (i = 0U; i < length; ++i) {
        out[i * 2U] = digits[bytes[i] >> 4U];
        out[i * 2U + 1U] = digits[bytes[i] & 0x0fU];
    }
    out[length * 2U] = '\0';
}

static int public_key_for(const uint8_t private_key[32], uint8_t public_key[32])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(
        EVP_PKEY_ED25519, NULL, private_key, 32U);
    size_t length = 32U;
    int ok = key != NULL && EVP_PKEY_get_raw_public_key(
        key, public_key, &length) == 1 && length == 32U;
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int sign_raw(const uint8_t private_key[32], const uint8_t *message,
                    size_t message_length, uint8_t signature[64])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(
        EVP_PKEY_ED25519, NULL, private_key, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    size_t signature_length = 64U;
    int ok = key != NULL && context != NULL &&
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) == 1 &&
        EVP_DigestSign(context, signature, &signature_length,
                       message, message_length) == 1 &&
        signature_length == 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int write_all(int descriptor, const uint8_t *bytes, size_t length)
{
    size_t offset = 0U;
    while (offset < length) {
        ssize_t written = write(descriptor, bytes + offset, length - offset);
        if (written > 0) offset += (size_t)written;
        else if (written < 0 && errno == EINTR) continue;
        else return 1;
    }
    return 0;
}

static int read_all(int descriptor, uint8_t *bytes, size_t length)
{
    size_t offset = 0U;
    while (offset < length) {
        ssize_t got = read(descriptor, bytes + offset, length - offset);
        if (got > 0) offset += (size_t)got;
        else if (got < 0 && errno == EINTR) continue;
        else return 1;
    }
    return 0;
}

static lxp_result install_metering_v1(lxp_kernel *kernel)
{
    static const uint8_t active_key[] = "progmet/active/v1";
    static const uint8_t history_key[] = {
        'p', 'r', 'o', 'g', 'm', 'e', 't', '/', 'h', 'i', 's', 't', 'o',
        'r', 'y', '/', 'v', '1', '/', 0U, 0U, 0U, 1U
    };
    static const uint64_t coefficients[9] = {1U, 1U, 1U, 1U, 1U,
                                              8U, 8U, 64U, 8U};
    uint8_t record[LX_PROGRAMS_METERING_RECORD_BYTES] = {0U};
    size_t offset = 0U;
    size_t index;
    if (kernel == NULL || kernel->module_kv_count != 0U)
        return LXP_ERR_NON_CANONICAL;
    (void)memcpy(record + offset, "LXMR1", 5U);
    offset += 5U;
    put_u32(record + offset, 1U);
    offset += 4U;
    for (index = 0U; index < 9U; ++index) {
        put_u64(record + offset, coefficients[index]);
        offset += 8U;
    }
    put_u64(record + offset, 1U);
    offset += 8U;
    record[offset++] = LX_PROGRAMS_METERING_AUTHORITY_GENESIS;
    (void)memset(record + offset, 0xa5, 32U);
    if (offset + 32U != sizeof(record)) return LXP_FATAL_INVARIANT;
    kernel->module_kv[0].module_id = LXP_MODULE_PROGRAMS;
    kernel->module_kv[0].key_length = sizeof(active_key) - 1U;
    kernel->module_kv[0].value_length = sizeof(record);
    (void)memcpy(kernel->module_kv[0].key, active_key,
                 sizeof(active_key) - 1U);
    (void)memcpy(kernel->module_kv[0].value, record, sizeof(record));
    kernel->module_kv[1].module_id = LXP_MODULE_PROGRAMS;
    kernel->module_kv[1].key_length = sizeof(history_key);
    kernel->module_kv[1].value_length = sizeof(record);
    (void)memcpy(kernel->module_kv[1].key, history_key, sizeof(history_key));
    (void)memcpy(kernel->module_kv[1].value, record, sizeof(record));
    kernel->module_kv_count = 2U;
    return LXP_OK;
}

static void node_destroy(node *n)
{
    if (!n->live) return;
    while (n->kernel.blob_count != 0U)
        free(n->kernel.blobs[--n->kernel.blob_count].bytes);
    lx_account_registry_release(&n->accounts);
    (void)lxp_state_store_destroy(&n->state);
    (void)memset(n, 0, sizeof(*n));
}

static int node_genesis(node *n, const lxp_module_iface *const *registrations,
                        size_t registration_count)
{
    static const uint8_t treasury_name[] = "system:fees";
    uint8_t treasury_id[32];
    lx_account *account;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters genesis = {0};
    size_t i;
    (void)memset(n, 0, sizeof(*n));
    n->live = true;
    n->parameters = 1U;
    n->height = 10U;
    if (lx_account_registry_init(&n->accounts) != LXP_OK) return 1;
    for (i = 0U; i < ACTORS; ++i) {
        char account_name[128];
        lxp_identity *identity;
        int length = snprintf(account_name, sizeof(account_name),
                              "agent:%s:main", dids[i]);
        if (length <= 0 || (size_t)length >= sizeof(account_name) ||
            public_key_for(seeds[i], n->keys[i]) != 0 ||
            lxp_identity_register(&n->identities, (const uint8_t *)dids[i],
                                  strlen(dids[i]), n->keys[i],
                                  &identity) != LXP_OK ||
            lxp_did_id_derive((const uint8_t *)dids[i], strlen(dids[i]),
                              n->principals[i]) != LXP_OK ||
            lx_account_id_from_string((const uint8_t *)account_name,
                                      (size_t)length,
                                      n->account_ids[i]) != LXP_OK ||
            lx_account_open(&n->accounts, (const uint8_t *)account_name,
                            (size_t)length, n->account_ids[i], 1U,
                            LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
            lxp_ledger_bootstrap_balance(account, asset_id,
                (lxp_u128){0U, UINT64_C(100000000000000)}, 1U) != LXP_OK)
            return 1;
    }
    if (lx_account_id_from_string(treasury_name, sizeof(treasury_name) - 1U,
                                  treasury_id) != LXP_OK ||
        lx_account_open(&n->accounts, treasury_name,
                        sizeof(treasury_name) - 1U, treasury_id, 2U,
                        LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
        lxp_ledger_bootstrap_balance(account, asset_id, (lxp_u128){0U, 0U},
                                     0U) != LXP_OK ||
        lxp_state_store_init(&n->state, 1U) != LXP_OK ||
        lxp_state_store_bind_accounts(&n->state, &n->accounts) != LXP_OK ||
        lxp_kernel_create(&n->kernel, &n->state, &n->journal, &n->parameters,
                          0U) != LXP_OK ||
        install_metering_v1(&n->kernel) != LXP_OK)
        return 1;
    for (i = 0U; i < registration_count; ++i)
        if (lxp_kernel_register_module(&n->kernel, registrations[i]) != LXP_OK)
            return 1;
    if (registration_count == 0U)
        return lxp_state_store_require_account_root(&n->state) != LXP_OK;
    if (lxp_state_store_require_account_root(&n->state) != LXP_OK) return 1;
    (void)memcpy(n->asset.asset_id, asset_id, 32U);
    n->asset.registered = true;
    n->runtime.accounts = &n->accounts;
    n->runtime.assets = &n->asset;
    n->runtime.asset_count = 1U;
    n->runtime.fee_schedule =
        (lx_programs_fee_schedule){1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U};
    n->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    n->runtime.metering_schedule_context = &n->kernel;
    (void)memcpy(n->runtime.occupancy_asset_id, asset_id, 32U);
    n->runtime.resolve_occupancy_parameters =
        lxp_programs_fee_governance_resolve_runtime;
    n->runtime.occupancy_parameter_context = &n->kernel;
    (void)memcpy(manifest.signer_public_key, n->keys[OWNER], 32U);
    genesis.schedule = n->runtime.fee_schedule;
    (void)memcpy(genesis.occupancy_asset_id, asset_id, 32U);
    genesis.target_occupancy_byte_batches = 3U;
    genesis.response_denominator = 1U;
    genesis.maximum_change_numerator = 1U;
    genesis.maximum_change_denominator = 1U;
    genesis.minimum_fee_units_per_occupancy_byte_batch = 1U;
    genesis.maximum_fee_units_per_occupancy_byte_batch = 10U;
    if (lxp_programs_fee_genesis_append(&manifest, &genesis) != LXP_OK ||
        lxp_programs_fee_genesis_materialize(&manifest, &n->kernel) != LXP_OK)
        return 1;
    n->fees.version = 1U;
    n->fees.multiplier_basis_points = 10000U;
    return lxp_kernel_bind_module_runtime(&n->kernel, LXP_MODULE_PROGRAMS,
                                          &n->runtime) != LXP_OK ||
        lxp_programs_bind_fee_transaction(&n->kernel) != LXP_OK ||
        lxp_kernel_set_capabilities(&n->kernel, NULL,
                                    lxp_kernel_canonical_ledger_apply) != LXP_OK ||
        lxp_state_root(&n->kernel, n->kernel.current_state_root) != LXP_OK ||
        lxp_arena_init(&n->arena, n->arena_bytes, sizeof(n->arena_bytes)) !=
            LXP_OK;
}

static lxp_result execute(node *n, unsigned actor, uint32_t type,
                          const uint8_t *payload, size_t length)
{
    lxp_activity activity = {0};
    lxp_authority_resolved authority = {0};
    lxp_authority_grant grant = {0};
    lxp_transfer_allowance allowance = {0};
    lxp_kernel_execution execution = {0};
    lxp_byte_span encoded;
    lxp_batch_roots roots;
    uint8_t signature[64], preimage[88], digest[32];
    lxp_result result;
    size_t index;
    if (lxp_arena_reset(&n->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = NETWORK_ID;
    activity.activity_type = type;
    activity.actor_did =
        (lxp_byte_span){(const uint8_t *)dids[actor], strlen(dids[actor])};
    activity.authority = (lxp_byte_span){n->keys[actor], 32U};
    activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity.account_sequence = n->identities.identities[actor].next_sequence;
    put_u64(activity.idempotency_key + 24U, n->state.next_sequence);
    activity.payload = (lxp_byte_span){payload, length};
    activity.fee_limit = (lxp_u128){0U, UINT64_C(10000000000)};
    if (lxp_hash_payload(payload, length, activity.payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(&activity, digest) != LXP_OK ||
        sign_raw(seeds[actor], digest, 32U, signature) != 0)
        return LXP_ERR_BAD_SIGNATURE;
    activity.signature = (lxp_byte_span){signature, 64U};
    if (lxp_activity_verify_signature(&activity) != LXP_OK)
        return LXP_ERR_BAD_SIGNATURE;
    result = lxp_authority_resolve_activity(
        &n->kernel, &n->identities.identities[actor], &activity, true, true,
        10U, 100U, n->state.next_sequence, &grant, &authority);
    if (result != LXP_OK) return result;
    lxp_authority_allowance_bind(&grant, &authority, &allowance);
    execution.network_id = NETWORK_ID;
    execution.epoch = n->kernel.epoch;
    execution.batch_number = n->height;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.global_sequence = n->state.next_sequence;
    execution.recorded_module_version = lxp_programs_module_version(&n->kernel);
    execution.recorded_metering_schedule_version = 1U;
    execution.recorded_fee_schedule_version = 1U;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &n->identities;
    execution.authority = &authority;
    execution.allowance = &allowance;
    execution.fee_parameters = &n->fees;
    for (index = 0U; index < n->accounts.count; ++index)
        if (memcmp(n->accounts.accounts[index].id, n->account_ids[actor], 32U) == 0)
            execution.fee_balance = n->accounts.accounts[index].balance;
    execution.gas_limit = UINT64_C(1000000000);
    execution.arena = &n->arena;
    execution.sequencer_private_key = sequencer_seed;
    if (lxp_activity_encode(&activity, &n->arena, &encoded) != LXP_OK ||
        lxp_batch_roots_compute(
            &(lxp_batch_root_inputs){&encoded, 1U, NULL, 0U, NULL, 0U, NULL, 0U,
                                     NULL, 0U},
            &n->arena, &roots) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(preimage, n->kernel.current_state_root, 32U);
    (void)memcpy(preimage + 32U, roots.activity_merkle_root, 32U);
    put_u64(preimage + 64U, execution.global_sequence);
    put_u64(preimage + 72U, execution.global_sequence);
    put_u64(preimage + 80U, execution.batch_number);
    if (lxp_hash_context_value(preimage, sizeof(preimage), execution.batch_id) !=
        LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(execution.activity_root, roots.activity_merkle_root, 32U);
    if (lxp_arena_reset(&n->arena, 0U) != LXP_OK) return LXP_FATAL_INVARIANT;
    (void)memset(&n->receipt, 0, sizeof(n->receipt));
    result = lxp_kernel_execute_activity(&n->kernel, &activity, &execution,
                                         &n->receipt);
    if (result == LXP_OK) {
        uint8_t root[32];
        if (lxp_receipt_verify(&n->receipt, sequencer_public, &n->arena) !=
            LXP_OK)
            return LXP_ERR_BAD_SIGNATURE;
        if (lxp_state_root(&n->kernel, root) != LXP_OK ||
            memcmp(root, n->receipt.resulting_state_root, 32U) != 0 ||
            memcmp(root, n->kernel.current_state_root, 32U) != 0)
            return LXP_FATAL_INVARIANT;
    }
    return result;
}

static int run(node *n, unsigned actor, uint32_t type, const uint8_t *payload,
               size_t length, lxp_result *status)
{
    const lxp_identity *identity = &n->identities.identities[actor];
    const uint64_t sequence = n->state.next_sequence;
    const uint64_t account_sequence = identity->next_sequence;
    *status = execute(n, actor, type, payload, length);
    if (*status != LXP_OK)
        return n->state.next_sequence == sequence &&
            identity->next_sequence == account_sequence ? 0 : 1;
    return n->state.next_sequence == sequence + 1U ? 0 : 1;
}

static const lxp_module_kv_entry *find_kv(const lxp_kernel *kernel,
                                          const uint8_t *key, size_t length)
{
    size_t i;
    for (i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[i];
        if (entry->module_id == LXP_MODULE_PROGRAMS &&
            entry->key_length == length && memcmp(entry->key, key, length) == 0)
            return entry;
    }
    return NULL;
}

static const lxp_module_blob *find_blob(const lxp_kernel *kernel,
                                        const uint8_t key[32])
{
    size_t i;
    for (i = 0U; i < kernel->blob_count; ++i) {
        const lxp_module_blob *blob = &kernel->blobs[i];
        if (!blob->deleted && blob->module_id == LXP_MODULE_PROGRAMS &&
            memcmp(blob->key, key, 32U) == 0)
            return blob;
    }
    return NULL;
}

static int seed_open(node *n, lxp_module_ctx *ctx, lxp_arena *arena,
                     uint8_t *bytes, size_t length)
{
    return lxp_arena_init(arena, bytes, length) != LXP_OK ||
        lxp_module_ctx_init(ctx, &n->kernel, LXP_MODULE_PROGRAMS, 10U,
                            n->kernel.epoch, n->state.next_sequence,
                            UINT64_C(100000000), arena, true) != LXP_OK;
}

static int send_frame(int descriptor, uint16_t minor, uint16_t tag,
                      uint64_t correlation_id, const uint8_t *payload,
                      size_t length)
{
    uint8_t frame[4U + ENVELOPE_FIXED_BYTES + LXP_CAPACITY_REQUEST_BYTES + 8U];
    size_t cursor = 4U;
    if (length > LXP_CAPACITY_REQUEST_BYTES + 8U) return 1;
    put_u32(frame, (uint32_t)(ENVELOPE_FIXED_BYTES + length));
    put_u16(frame + cursor, LNI_MAJOR); cursor += 2U;
    put_u16(frame + cursor, minor); cursor += 2U;
    put_u16(frame + cursor, tag); cursor += 2U;
    put_u64(frame + cursor, correlation_id); cursor += 8U;
    put_u32(frame + cursor, (uint32_t)length); cursor += 4U;
    if (length != 0U) (void)memcpy(frame + cursor, payload, length);
    cursor += length;
    put_u32(frame + cursor, 0U); cursor += 4U;
    return write_all(descriptor, frame, cursor);
}

static int verify_proof(uint16_t tag, const uint8_t *payload, size_t length,
                        const uint8_t *proof)
{
    uint8_t *message = (uint8_t *)malloc(sizeof(response_domain) - 1U + 6U +
                                         length);
    uint8_t digest[32];
    size_t cursor = sizeof(response_domain) - 1U;
    int failure;
    if (message == NULL) return 1;
    (void)memcpy(message, response_domain, cursor);
    put_u32(message + cursor, NETWORK_ID); cursor += 4U;
    put_u16(message + cursor, tag); cursor += 2U;
    (void)memcpy(message + cursor, payload, length);
    cursor += length;
    failure = lxp_hash_sha256(message, cursor, digest) != LXP_OK ||
        memcmp(proof, sequencer_public, 32U) != 0 ||
        lxp_ed25519_verify_raw(proof, proof + 32U, digest, 32U) != LXP_OK;
    free(message);
    return failure;
}

static int receive_reply(int descriptor, uint64_t correlation_id, reply *out)
{
    uint8_t prefix[4];
    uint8_t *frame;
    uint32_t length, payload_length, proof_length;
    int failure = 0;
    (void)memset(out, 0, sizeof(*out));
    if (read_all(descriptor, prefix, 4U) != 0) return 1;
    length = get_u32(prefix);
    if (length < ENVELOPE_FIXED_BYTES || length > 65536U) return 1;
    frame = (uint8_t *)malloc(length);
    if (frame == NULL) return 1;
    if (read_all(descriptor, frame, length) != 0) failure = 1;
    if (failure == 0) {
        payload_length = get_u32(frame + 14U);
        if (get_u16(frame) != LNI_MAJOR || get_u16(frame + 2U) != LNI_MINOR ||
            get_u64(frame + 6U) != correlation_id ||
            payload_length > length - ENVELOPE_FIXED_BYTES ||
            payload_length > sizeof(out->payload))
            failure = 1;
    }
    if (failure == 0) {
        proof_length = get_u32(frame + 18U + payload_length);
        out->tag = get_u16(frame + 4U);
        out->length = payload_length;
        (void)memcpy(out->payload, frame + 18U, payload_length);
        if (18U + payload_length + 4U + proof_length != length) failure = 1;
        else if (out->tag == ERROR_RESPONSE) {
            if (payload_length != 5U || proof_length != 0U) failure = 1;
            else {
                out->refusal_class = out->payload[0];
                out->refusal = (lxp_result)(int32_t)get_u32(out->payload + 1U);
            }
        } else if (proof_length != LNI_PROOF_BYTES ||
                   verify_proof(out->tag, out->payload, payload_length,
                                frame + 22U + payload_length) != 0) {
            failure = 1;
        }
    }
    free(frame);
    return failure;
}

static int lni_call(uint16_t tag, const uint8_t *body, size_t length,
                    reply *out)
{
    uint64_t id = correlation++;
    if (send_frame(lni.link.descriptor, LNI_MINOR, tag, id, body, length) != 0 ||
        receive_reply(lni.link.descriptor, id, out) != 0)
        return 1;
    return out->tag == ERROR_RESPONSE || out->tag == tag + 1U ? 0 : 1;
}

static void *connection_run(void *context)
{
    connection *link = (connection *)context;
    link->status = lxp_daemon_lni_serve_connected(&lni.server,
                                                  link->server_descriptor);
    (void)close(link->server_descriptor);
    return NULL;
}

static int connection_open(connection *link)
{
    int sockets[2];
    uint8_t prefix[4];
    uint8_t *frame;
    uint32_t length, payload_length;
    int failure;
    (void)memset(link, 0, sizeof(*link));
    if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, sockets) != 0)
        return 1;
    link->descriptor = sockets[0];
    link->server_descriptor = sockets[1];
    if (pthread_create(&link->thread, NULL, connection_run, link) != 0) {
        (void)close(sockets[0]);
        (void)close(sockets[1]);
        return 1;
    }
    link->open = true;
    if (send_frame(link->descriptor, LNI_MINOR, NODE_INFO_REQUEST, 0U, NULL,
                   0U) != 0 ||
        read_all(link->descriptor, prefix, 4U) != 0)
        return 1;
    length = get_u32(prefix);
    if (length < ENVELOPE_FIXED_BYTES || length > 1048576U) return 1;
    frame = (uint8_t *)malloc(length);
    if (frame == NULL) return 1;
    failure = read_all(link->descriptor, frame, length) != 0 ||
        get_u16(frame + 2U) != LNI_MINOR ||
        get_u16(frame + 4U) != NODE_INFO_RESPONSE ||
        get_u64(frame + 6U) != 0U;
    if (failure == 0) {
        payload_length = get_u32(frame + 14U);
        failure = payload_length > length - ENVELOPE_FIXED_BYTES ||
            payload_length > sizeof(lni.node_info);
        if (failure == 0) {
            (void)memcpy(lni.node_info, frame + 18U, payload_length);
            lni.node_info_length = payload_length;
        }
    }
    free(frame);
    return failure;
}

static int connection_close(connection *link)
{
    if (!link->open) return 0;
    link->open = false;
    (void)shutdown(link->descriptor, SHUT_RDWR);
    (void)close(link->descriptor);
    if (pthread_join(link->thread, NULL) != 0) return 1;
    return link->status == LXP_OK ? 0 : 1;
}

static int connection_reopen(void)
{
    return connection_close(&lni.link) != 0 || connection_open(&lni.link) != 0;
}

static void request_encode(const cap_request *r,
                           uint8_t out[LXP_CAPACITY_REQUEST_BYTES])
{
    size_t cursor = 0U;
    size_t actor_length = strlen(r->actor);
    (void)memset(out, 0, LXP_CAPACITY_REQUEST_BYTES);
    put_u16(out, 1U); cursor += 2U;
    out[cursor++] = r->kind;
    (void)memcpy(out + cursor, r->activity, 32U); cursor += 32U;
    (void)memcpy(out + cursor, r->idempotency, 32U); cursor += 32U;
    put_u16(out + cursor, (uint16_t)actor_length); cursor += 2U;
    (void)memcpy(out + cursor, r->actor, actor_length); cursor += 255U;
    put_u32(out + cursor, r->blobs); cursor += 4U;
    put_u64(out + cursor, r->bytes); cursor += 8U;
    put_u32(out + cursor, r->kv); cursor += 4U;
    put_u64(out + cursor, r->sequence); cursor += 8U;
    (void)memcpy(out + cursor, r->root, 32U); cursor += 32U;
    put_u64(out + cursor, r->lifetime); cursor += 8U;
    put_u64(out + cursor, 0U);
}

static void work_request(cap_request *r, uint8_t activity, uint32_t blobs,
                         uint64_t bytes, uint32_t kv)
{
    (void)memset(r, 0, sizeof(*r));
    r->kind = LXP_CAPACITY_WORK;
    (void)memset(r->activity, activity, 32U);
    (void)memset(r->idempotency, activity ^ 0xa5, 32U);
    r->actor = dids[OWNER];
    r->blobs = blobs;
    r->bytes = bytes;
    r->kv = kv;
    r->sequence = primary->state.next_sequence;
    (void)memcpy(r->root, primary->kernel.current_state_root, 32U);
    r->lifetime = 4U;
}

static int reserve(const cap_request *r, uint64_t *id)
{
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    uint8_t message[sizeof(request_domain) - 1U + LXP_CAPACITY_REQUEST_BYTES];
    uint8_t digest[32];
    reply answer;
    request_encode(r, body);
    (void)memcpy(message, request_domain, sizeof(request_domain) - 1U);
    (void)memcpy(message + sizeof(request_domain) - 1U, body, sizeof(body));
    if (lxp_hash_sha256(message, sizeof(message), digest) != LXP_OK ||
        lni_call(RESERVE, body, sizeof(body), &answer) != 0 ||
        answer.tag != RESERVE + 1U ||
        answer.length != 1U + LXP_CAPACITY_RECORD_BYTES ||
        answer.payload[0] != 0U ||
        answer.payload[1U + REC_STATE] != LXP_CAPACITY_RESERVED ||
        memcmp(answer.payload + 1U + REC_DIGEST, digest, 32U) != 0)
        return 1;
    *id = get_u64(answer.payload + 1U + REC_ID);
    return 0;
}

static int cancel(uint64_t id)
{
    uint8_t body[ID_BODY_BYTES];
    reply answer;
    put_u16(body, 1U);
    put_u64(body + 2U, id);
    return lni_call(CANCEL, body, sizeof(body), &answer) != 0 ||
        answer.tag != CANCEL + 1U ||
        answer.length != LXP_CAPACITY_RECORD_BYTES ||
        get_u64(answer.payload + REC_ID) != id ||
        answer.payload[REC_STATE] != LXP_CAPACITY_CANCELLED;
}

static lxp_result apply_none(void *context, uint64_t global_sequence,
                             const uint8_t *bytes, size_t length)
{
    (void)global_sequence;
    (void)bytes;
    (void)length;
    ++*(size_t *)context;
    return LXP_ERR_MODULE_DISABLED;
}

static int lni_serve(void)
{
    lxp_daemon_lni_configuration configuration;
    (void)memset(&configuration, 0, sizeof(configuration));
    configuration.socket_path = lni.socket_path;
    configuration.admission_directory = lni.admission_directory;
    configuration.allowed_peer_uid = (uint32_t)geteuid() + 1U;
    configuration.allowed_peer_gid = (uint32_t)getegid();
    configuration.frame_bytes = LXP_DAEMON_LNI_MAX_FRAME_BYTES;
    configuration.deadline_milliseconds = 10000U;
    configuration.socket_mode = 0660U;
    if (lxp_daemon_lni_serve(&lni.server, &lni.daemon, &lni.owner,
                             &configuration) != LXP_OK)
        return 1;
    lni.lni_started = true;
    if (pthread_mutex_lock(&lni.server.mutex) != 0) return 1;
    lni.server.allowed_peer_uid = (uint32_t)geteuid();
    lni.server.allowed_peer_gid = (uint32_t)getegid();
    return pthread_mutex_unlock(&lni.server.mutex) == 0 ? 0 : 1;
}

static int lni_stop(void)
{
    if (!lni.lni_started) return 0;
    lni.lni_started = false;
    return lxp_daemon_lni_stop(&lni.server) == LXP_OK ? 0 : 1;
}

static int lni_init(void)
{
    lxp_daemon_configuration daemon_configuration;
    char key_hex[65];
    int written;
    lni.scratch_bytes = (uint8_t *)malloc(OWNER_SCRATCH_BYTES);
    if (lni.scratch_bytes == NULL ||
        lxp_arena_init(&lni.scratch, lni.scratch_bytes, OWNER_SCRATCH_BYTES) !=
            LXP_OK ||
        pthread_mutex_init(&lni.owner.mutex, NULL) != 0 ||
        pthread_mutex_init(&lni.owner.receipt_mutex, NULL) != 0 ||
        pthread_mutex_init(&lni.owner.publication_mutex, NULL) != 0 ||
        pthread_mutex_init(&lni.owner.receipt_authority_mutex, NULL) != 0)
        return 1;
    lni.owner_ready = true;
    lni.canonical_log.descriptor = -1;
    lni.history.log = &lni.canonical_log;
    lni.owner.kernel = &primary->kernel;
    lni.owner.identities = &primary->identities;
    lni.owner.programs_runtime = &primary->runtime;
    lni.owner.network_id = NETWORK_ID;
    lni.owner.protocol_version = LXP_PROTOCOL_VERSION;
    lni.owner.history = &lni.history;
    lni.owner.receipt_authority = &lni.receipt_authority;
    lni.owner.scratch = &lni.scratch;
    lni.owner.feed_store.baseline_present = true;
    lni.owner.feed_store.baseline_next_sequence = 1U;
    lni.owner.attached = true;
    (void)memcpy(lni.receipt_authority.authorization.public_key,
                 sequencer_public, 32U);
    if (lxp_handover_sequencer_id(sequencer_public,
            lni.receipt_authority.authorization.sequencer_id) != LXP_OK)
        return 1;
    lni.receipt_authority.authorization.first_batch_number = 1U;
    lni.receipt_authority.authorization.last_batch_number = 1000000U;
    lni.receipt_authority.authorization.authorized = 1U;
    hex_encode(sequencer_seed, 32U, key_hex);
    if (setenv("LAYERX_NODE_SEQUENCER_PRIVATE_KEY", key_hex, 1) != 0)
        return 1;
    (void)memset(&daemon_configuration, 0, sizeof(daemon_configuration));
    daemon_configuration.role = LXP_DAEMON_SEQUENCER;
    daemon_configuration.network_id = NETWORK_ID;
    daemon_configuration.start_sequence = 1U;
    daemon_configuration.serial_execution = true;
    if (lxp_daemon_start(&lni.daemon, &daemon_configuration, apply_none,
                         &lni.applied) != LXP_OK)
        return 1;
    lni.daemon_started = true;
    (void)strcpy(lni.socket_directory, "/tmp/lxp-native-gaps-socket-XXXXXX");
    (void)strcpy(lni.admission_directory, "/tmp/lxp-native-gaps-node-XXXXXX");
    if (mkdtemp(lni.socket_directory) == NULL ||
        mkdtemp(lni.admission_directory) == NULL ||
        chmod(lni.socket_directory, 0750) != 0 ||
        chmod(lni.admission_directory, 0700) != 0)
        return 1;
    written = snprintf(lni.socket_path, sizeof(lni.socket_path), "%s/lni.sock",
                       lni.socket_directory);
    if (written < 0 || (size_t)written >= sizeof(lni.socket_path)) return 1;
    return lni_serve() != 0 || connection_open(&lni.link) != 0;
}

static int capacity_entry(char *out, size_t capacity)
{
    const uint8_t *p = lni.node_info;
    const uint8_t *previous = NULL;
    size_t previous_length = 0U;
    size_t cursor = NODE_INFO_FIXED_BYTES, i, count, found = 0U;
    if (lni.node_info_length < NODE_INFO_FIXED_BYTES ||
        get_u32(p + 6U) != NETWORK_ID ||
        memcmp(p + 59U, sequencer_public, 32U) != 0)
        return 1;
    count = get_u16(p + 91U);
    for (i = 0U; i < count; ++i) {
        size_t length, shared;
        int order;
        if (cursor + 2U > lni.node_info_length) return 1;
        length = get_u16(p + cursor);
        cursor += 2U;
        if (length == 0U || length > CAPABILITY_MAX_BYTES ||
            cursor + length > lni.node_info_length)
            return 1;
        if (previous != NULL) {
            shared = previous_length < length ? previous_length : length;
            order = memcmp(previous, p + cursor, shared);
            if (order > 0 || (order == 0 && previous_length >= length))
                return 1;
        }
        if (length >= sizeof(capacity_prefix) - 1U &&
            memcmp(p + cursor, capacity_prefix, sizeof(capacity_prefix) - 1U) ==
                0) {
            if (length >= capacity) return 1;
            (void)memcpy(out, p + cursor, length);
            out[length] = '\0';
            ++found;
        }
        previous = p + cursor;
        previous_length = length;
        cursor += length;
    }
    return cursor == lni.node_info_length && found == 1U ? 0 : 1;
}

static int expected_capacity(char *out, size_t capacity)
{
    lxp_capacity_observation observation;
    int written;
    if (primary->kernel.capacity == NULL) {
        written = snprintf(out, capacity, "capacity:granted=0,0,0");
    } else {
        if (lxp_kernel_capacity_observe(&primary->kernel,
                                        primary->kernel.capacity,
                                        &observation) != LXP_OK)
            return 1;
        written = snprintf(
            out, capacity, "capacity:granted=%llu,%llu,%llu",
            (unsigned long long)observation.obligations.blobs +
                observation.work.blobs,
            (unsigned long long)observation.obligations.bytes +
                observation.work.bytes,
            (unsigned long long)observation.obligations.kv +
                observation.work.kv);
    }
    return written < 0 || (size_t)written >= capacity ? 1 : 0;
}

static int install_profile(void)
{
    lxp_capacity_profile profile;
    uint8_t body[LXP_CAPACITY_PROFILE_BYTES];
    reply answer;
    (void)memset(&profile, 0, sizeof(profile));
    profile.version = 1U;
    (void)memset(profile.digest, 0x51, 32U);
    profile.floor = (lxp_capacity_demand){FLOOR_BLOBS, FLOOR_BYTES, FLOOR_KV};
    profile.maximum_work_lifetime = MAX_WORK_LIFETIME;
    lxp_capacity_profile_encode(&profile, body);
    return lni_call(INSTALL, body, sizeof(body), &answer) != 0 ||
        answer.tag != INSTALL + 1U || answer.length != sizeof(body) ||
        memcmp(answer.payload, body, sizeof(body)) != 0 ||
        primary->kernel.capacity == NULL ||
        primary->kernel.capacity->profile.version != 1U;
}

static size_t retirement_encode(uint8_t count, uint64_t cutoff,
                                const uint8_t digest[32], uint32_t size,
                                const uint8_t origin_root[32],
                                const uint8_t replay_id[32],
                                const uint8_t *proof, uint16_t proof_length,
                                const uint8_t controller[32])
{
    uint8_t *out = retirement_payload;
    uint8_t certificate[32];
    size_t cursor = LXP_PROGRAMS_RETIREMENT_HEADER_BYTES;
    (void)memset(out, 0, sizeof(retirement_payload));
    out[0] = LXP_PROGRAMS_RETIREMENT_REQUEST_VERSION;
    out[1] = count;
    put_u32(out + 4U, NETWORK_ID);
    put_u16(out + 8U, LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION);
    (void)memcpy(out + 10U, profile_digest, 32U);
    (void)memcpy(out + 42U, controller, 32U);
    put_u64(out + 74U, primary->state.next_sequence);
    (void)memcpy(out + 82U, primary->kernel.current_state_root, 32U);
    put_u64(out + 114U, cutoff);
    (void)memset(out + 122U, 0x5a, 32U);
    (void)memset(out + 154U, 0x5b, 32U);
    put_u64(out + 186U, 4096U);
    put_u64(out + 194U, ORIGIN_SEQUENCE);
    put_u64(out + 202U, cutoff);
    (void)memset(out + 210U, 0x5c, 32U);
    if (proof != NULL) {
        uint8_t *fixed = out + cursor;
        fixed[0] = LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY;
        (void)memcpy(fixed + 37U, digest, 32U);
        put_u32(fixed + 69U, size);
        put_u64(fixed + 73U, ORIGIN_SEQUENCE);
        (void)memcpy(fixed + 81U, origin_root, 32U);
        (void)memcpy(fixed + 113U, replay_id, 32U);
        put_u16(fixed + 145U, proof_length);
        put_u16(fixed + 147U, 0U);
        cursor += LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES;
        (void)memcpy(out + cursor, proof, proof_length);
        cursor += proof_length;
        if (lxp_programs_retirement_certificate_digest(out, cursor,
                                                       certificate) != LXP_OK ||
            sign_raw(archive_seed_a, certificate, 32U, out + 242U) != 0 ||
            sign_raw(archive_seed_b, certificate, 32U, out + 306U) != 0)
            return 0U;
    }
    return cursor;
}

static void replay_record(uint8_t record[REPLAY_DOMAIN_BYTES + REPLAY_BODY_BYTES],
                          const uint8_t replay_id[32],
                          const uint8_t blob_key[32])
{
    uint8_t *body = record + REPLAY_DOMAIN_BYTES;
    (void)memset(record, 0, REPLAY_DOMAIN_BYTES + REPLAY_BODY_BYTES);
    (void)memcpy(record, replay_domain, REPLAY_DOMAIN_BYTES);
    put_u16(body, 1U);
    put_u32(body + 2U, NETWORK_ID);
    (void)memcpy(body + 6U, replay_id, 32U);
    put_u64(body + 38U, ORIGIN_SEQUENCE);
    (void)memset(body + 46U, 0x74, 32U);
    (void)memcpy(body + 78U, program_id, 32U);
    (void)memset(body + 110U, 0x75, 32U);
    (void)memset(body + 142U, 0x76, 32U);
    put_u16(body + 174U, 1U);
    put_u16(body + 176U, LX_PROGRAMS_GUEST_ABI_V5_VERSION);
    put_u32(body + 178U, 1U);
    put_u32(body + 182U, 1U);
    put_u16(body + 186U, 1U);
    put_u32(body + 188U, 2U);
    put_u32(body + 192U, LXP_PROGRAMS_REPLAY_MAX_BYTES);
    body[196] = 0U;
    put_u32(body + 197U, 1U);
    (void)memset(body + 201U, 0x77, 32U);
    (void)memset(body + 233U, 0x78, 32U);
    (void)memset(body + 265U, 0x79, 32U);
    (void)memset(body + 297U, 0x7a, 32U);
    put_u32(body + 329U, 0U);
    (void)memcpy(body + 333U, blob_key, 32U);
}

static int case_module5_retirement(void)
{
    static const char name[] = "module5_retirement_resolves_and_applies";
    static const uint8_t control_key[] = "progretire/v1";
    static uint8_t bytes[65536];
    static uint8_t witness_bytes[WITNESS_BYTES];
    static uint8_t proof[PROOF_CAPACITY];
    const lxp_module_iface *registration = programs_module_registration_v4();
    lxp_programs_retirement_profile profile;
    lxp_module_ctx ctx;
    lxp_arena arena;
    uint8_t record[REPLAY_DOMAIN_BYTES + REPLAY_BODY_BYTES];
    uint8_t replay_id[32], blob_key[32], origin_root[32], recovered[32];
    uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
    const lxp_module_kv_entry *control;
    const lxp_effect *header;
    lxp_result status = LXP_OK;
    size_t length, proof_length = 0U;
    uint64_t sequence;
    (void)memset(&profile, 0, sizeof(profile));
    profile.version = LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION;
    profile.network_id = NETWORK_ID;
    (void)memcpy(profile.controller, primary->principals[CONTROLLER], 32U);
    (void)memcpy(profile.paxai_program_id, program_id, 32U);
    profile.hot_horizon = 1U;
    CHECK(public_key_for(archive_seed_a, profile.archive_keys[0]) == 0 &&
          public_key_for(archive_seed_b, profile.archive_keys[1]) == 0 &&
          lxp_programs_retirement_profile_digest(&profile, profile_digest) ==
              LXP_OK, name);
    (void)memset(replay_id, 0x73, 32U);
    (void)memset(witness_bytes, 0x5e, sizeof(witness_bytes));
    CHECK(lxp_hash_sha256(witness_bytes, sizeof(witness_bytes), blob_key) ==
          LXP_OK, name);
    replay_record(record, replay_id, blob_key);
    CHECK(lxp_programs_replay_record_blob_key(
              (lxp_byte_span){record, sizeof(record)}, recovered) == LXP_OK &&
          memcmp(recovered, blob_key, 32U) == 0, name);
    lxp_programs_replay_record_key(replay_id, key);
    CHECK(seed_open(primary, &ctx, &arena, bytes, sizeof(bytes)) == 0, name);
    status = lxp_programs_retirement_profile_stage(&ctx, &profile);
    if (status == LXP_OK)
        status = lxp_ctx_blob_put(&ctx, blob_key, witness_bytes,
                                  sizeof(witness_bytes));
    if (status == LXP_OK)
        status = lxp_ctx_kv_put(&ctx, key, sizeof(key), record, sizeof(record));
    if (status != LXP_OK) lxp_module_ctx_rollback(&ctx);
    CHECK(status == LXP_OK, name);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK &&
          lxp_state_root(&primary->kernel,
                         primary->kernel.current_state_root) == LXP_OK, name);
    (void)memcpy(origin_root, primary->kernel.current_state_root, 32U);
    CHECK(lxp_state_proof_build(&primary->kernel, LXP_MODULE_PROGRAMS,
                                (lxp_byte_span){key, sizeof(key)},
                                &witness) == LXP_OK &&
          lxp_state_proof_verify(&witness, origin_root) == LXP_OK &&
          lxp_state_proof_encode(&witness, proof, sizeof(proof),
                                 &proof_length) == LXP_OK &&
          proof_length != 0U && proof_length <= UINT16_MAX, name);

    length = retirement_encode(1U, CUTOFF_SEQUENCE, NULL, 0U, NULL, NULL, NULL,
                               0U, primary->principals[CONTROLLER]);
    CHECK(registration->abi_version == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION,
          name);
    node_destroy(scratch_node);
    CHECK(node_genesis(scratch_node, &registration, 1U) == 0 &&
          lxp_programs_module_version(&scratch_node->kernel) ==
              LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, name);
    CHECK(run(scratch_node, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
              retirement_payload, length, &status) == 0, name);
    (void)printf("NATIVE_GAPS_NOTE %s module4_status=%d\n", name, (int)status);
    CHECK(status == LXP_ERR_AUTH_SCOPE, name);
    node_destroy(scratch_node);

    sequence = primary->state.next_sequence;
    CHECK(sequence == ORIGIN_SEQUENCE, name);
    CHECK(run(primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
              retirement_payload, length, &status) == 0, name);
    (void)printf("NATIVE_GAPS_NOTE %s header_only status=%d result=%d\n", name,
                 (int)status,
                 status == LXP_OK ? (int)primary->receipt.result_code : 0);
    CHECK(status == LXP_OK &&
          primary->receipt.result_code == LXP_ERR_TRUNCATED &&
          primary->receipt.module_id == LXP_MODULE_PROGRAMS &&
          primary->receipt.module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION &&
          primary->state.next_sequence == sequence + 1U, name);

    length = retirement_encode(1U, CUTOFF_SEQUENCE, blob_key, WITNESS_BYTES,
                               origin_root, replay_id, proof,
                               (uint16_t)proof_length,
                               primary->principals[CONTROLLER]);
    CHECK(length == LXP_PROGRAMS_RETIREMENT_HEADER_BYTES +
                        LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES + proof_length,
          name);
    CHECK(run(primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
              retirement_payload, length, &status) == 0, name);
    (void)printf("NATIVE_GAPS_NOTE %s early status=%d result=%d\n", name,
                 (int)status,
                 status == LXP_OK ? (int)primary->receipt.result_code : 0);
    CHECK(status == LXP_OK &&
          primary->receipt.result_code == LXP_ERR_NOT_YET_VALID &&
          primary->receipt.module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION &&
          find_kv(&primary->kernel, key, sizeof(key)) != NULL &&
          find_blob(&primary->kernel, blob_key) != NULL, name);

    sequence = primary->state.next_sequence;
    CHECK(sequence == CUTOFF_SEQUENCE + 1U, name);
    length = retirement_encode(1U, CUTOFF_SEQUENCE, blob_key, WITNESS_BYTES,
                               origin_root, replay_id, proof,
                               (uint16_t)proof_length,
                               primary->principals[CONTROLLER]);
    CHECK(length != 0U, name);
    CHECK(run(primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
              retirement_payload, length, &status) == 0, name);
    (void)printf("NATIVE_GAPS_NOTE %s apply status=%d result=%d\n", name,
                 (int)status,
                 status == LXP_OK ? (int)primary->receipt.result_code : 0);
    CHECK(status == LXP_OK && primary->receipt.result_code == LXP_OK &&
          primary->receipt.module_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION &&
          primary->state.next_sequence == sequence + 1U, name);
    CHECK(find_kv(&primary->kernel, key, sizeof(key)) == NULL &&
          find_blob(&primary->kernel, blob_key) == NULL, name);
    control = find_kv(&primary->kernel, control_key, sizeof(control_key) - 1U);
    CHECK(control != NULL &&
          control->value_length == LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES &&
          get_u64(control->value + CONTROL_LAST_CUTOFF) == CUTOFF_SEQUENCE &&
          get_u64(control->value + CONTROL_RETIREMENTS) == 1U &&
          get_u64(control->value + CONTROL_BLOBS) == 1U &&
          get_u64(control->value + CONTROL_BYTES) == WITNESS_BYTES &&
          get_u64(control->value + CONTROL_REPLAYS) == 1U, name);
    CHECK(primary->receipt.effects.count == 2U &&
          primary->receipt.effects.effects[0].kind == LXP_EFFECT_EVENT &&
          primary->receipt.effects.effects[0].event_type ==
              LXP_PROGRAMS_RETIREMENT_EVENT &&
          primary->receipt.effects.effects[1].kind == LXP_EFFECT_EVENT &&
          primary->receipt.effects.effects[1].event_type ==
              LXP_PROGRAMS_RETIREMENT_EVENT, name);
    header = &primary->receipt.effects.effects[0];
    CHECK(header->body_length == 226U && header->body[199] == 1U &&
          header->body[200] == 1U && header->body[201] == 1U, name);
    return pass(name);
}

static int case_prepare_refusal(void)
{
    static const char name[] = "commit_above_open_reservation_refused";
    static uint8_t bytes[65536];
    static uint8_t holder_bytes[65536];
    lxp_capacity_observation head, held;
    cap_request request;
    lxp_module_ctx ctx, holder;
    lxp_arena arena, holder_arena;
    uint8_t key[32], blob[64], root[32];
    const size_t blobs_before = primary->kernel.blob_count;
    const uint64_t blob_bytes_before = primary->kernel.blob_total_bytes;
    lxp_result status, holder_status = LXP_OK;
    uint64_t id;
    CHECK(primary->kernel.capacity != NULL &&
          lxp_kernel_capacity_observe(&primary->kernel,
                                      primary->kernel.capacity,
                                      &head) == LXP_OK &&
          head.available.blobs != 0U && head.active_reservations == 0U, name);
    (void)memcpy(root, primary->kernel.current_state_root, 32U);
    work_request(&request, 0x41, head.available.blobs, 1U, 0U);
    CHECK(reserve(&request, &id) == 0, name);
    CHECK(lxp_kernel_capacity_observe(&primary->kernel,
                                      primary->kernel.capacity,
                                      &held) == LXP_OK &&
          held.available.blobs == 0U && held.active_reservations == 1U &&
          held.work.blobs == head.available.blobs, name);
    (void)memset(blob, 0x6b, sizeof(blob));
    CHECK(lxp_hash_sha256(blob, sizeof(blob), key) == LXP_OK &&
          seed_open(primary, &ctx, &arena, bytes, sizeof(bytes)) == 0, name);
    status = lxp_ctx_blob_put(&ctx, key, blob, sizeof(blob));
    if (status == LXP_OK) status = lxp_module_ctx_prepare_commit(&ctx);
    (void)printf("NATIVE_GAPS_NOTE %s prepare_status=%d\n", name, (int)status);
    if (status != LXP_ERR_ARENA_EXHAUSTED) {
        lxp_module_ctx_rollback(&ctx);
        (void)cancel(id);
    }
    CHECK(status == LXP_ERR_ARENA_EXHAUSTED, name);
    if (!(ctx.staged_blob_count == 1U && !ctx.commit_prepared &&
          !ctx.staged_blobs[0].deleted &&
          ctx.staged_blobs[0].length == sizeof(blob) &&
          memcmp(ctx.staged_blobs[0].key, key, 32U) == 0 &&
          memcmp(ctx.staged_blobs[0].bytes, blob, sizeof(blob)) == 0 &&
          primary->kernel.blob_count == blobs_before &&
          primary->kernel.blob_total_bytes == blob_bytes_before &&
          find_blob(&primary->kernel, key) == NULL))
        status = LXP_FATAL_INVARIANT;
    if (status == LXP_ERR_ARENA_EXHAUSTED) {
        if (seed_open(primary, &holder, &holder_arena, holder_bytes,
                      sizeof(holder_bytes)) != 0) {
            holder_status = LXP_FATAL_INVARIANT;
        } else {
            (void)memset(holder.activity_id, 0x41, 32U);
            holder_status = lxp_ctx_blob_put(&holder, key, blob, sizeof(blob));
            if (holder_status == LXP_OK)
                holder_status = lxp_module_ctx_prepare_commit(&holder);
            lxp_module_ctx_rollback(&holder);
        }
    }
    if (cancel(id) != 0) {
        lxp_module_ctx_rollback(&ctx);
        CHECK(false, name);
    }
    if (status != LXP_ERR_ARENA_EXHAUSTED) lxp_module_ctx_rollback(&ctx);
    CHECK(status == LXP_ERR_ARENA_EXHAUSTED, name);
    CHECK(holder_status == LXP_OK, name);
    CHECK(memcmp(root, primary->kernel.current_state_root, 32U) == 0, name);
    status = lxp_module_ctx_prepare_commit(&ctx);
    if (status != LXP_OK) lxp_module_ctx_rollback(&ctx);
    CHECK(status == LXP_OK, name);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK &&
          lxp_state_root(&primary->kernel,
                         primary->kernel.current_state_root) == LXP_OK, name);
    CHECK(primary->kernel.blob_count == blobs_before + 1U &&
          primary->kernel.blob_total_bytes == blob_bytes_before + sizeof(blob) &&
          find_blob(&primary->kernel, key) != NULL, name);
    return pass(name);
}

static int case_node_info_capacity(void)
{
    static const char name[] = "node_info_carries_granted_capacity";
    char advertised[CAPABILITY_MAX_BYTES + 1U];
    char expected[CAPABILITY_MAX_BYTES + 1U];
    lxp_capacity_observation observation;
    cap_request request;
    uint64_t id;
    CHECK(primary->kernel.capacity == NULL, name);
    CHECK(capacity_entry(advertised, sizeof(advertised)) == 0 &&
          expected_capacity(expected, sizeof(expected)) == 0 &&
          strcmp(advertised, expected) == 0 &&
          strcmp(advertised, "capacity:granted=0,0,0") == 0, name);
    CHECK(install_profile() == 0, name);
    work_request(&request, 0x52, 3U, 4096U, 2U);
    CHECK(reserve(&request, &id) == 0, name);
    CHECK(connection_reopen() == 0, name);
    CHECK(lxp_kernel_capacity_observe(&primary->kernel,
                                      primary->kernel.capacity,
                                      &observation) == LXP_OK &&
          observation.obligations.blobs == 0U &&
          observation.obligations.bytes == 0U &&
          observation.obligations.kv == 0U &&
          observation.work.blobs == 3U && observation.work.bytes == 4096U &&
          observation.work.kv == 2U, name);
    CHECK(capacity_entry(advertised, sizeof(advertised)) == 0 &&
          expected_capacity(expected, sizeof(expected)) == 0, name);
    (void)printf("NATIVE_GAPS_NOTE %s advertised=%s\n", name, advertised);
    CHECK(strcmp(advertised, expected) == 0 &&
          strcmp(advertised, "capacity:granted=3,4096,2") == 0, name);
    CHECK(cancel(id) == 0 && connection_reopen() == 0, name);
    CHECK(capacity_entry(advertised, sizeof(advertised)) == 0 &&
          expected_capacity(expected, sizeof(expected)) == 0 &&
          strcmp(advertised, expected) == 0 &&
          strcmp(advertised, "capacity:granted=0,0,0") == 0, name);
    return pass(name);
}

static bool lists_v4(const lxp_module_iface *iface)
{
    size_t i;
    if (iface->activity_type_count < V4_ACTIVITY_TYPES) return false;
    for (i = 0U; i < V4_ACTIVITY_TYPES; ++i)
        if (iface->activity_types[i] !=
            programs_module_registration_v4()->activity_types[i])
            return false;
    return true;
}

static bool lists(const lxp_module_iface *iface, uint32_t type)
{
    size_t i;
    for (i = 0U; i < iface->activity_type_count; ++i)
        if (iface->activity_types[i] == type) return true;
    return false;
}

static int pin_for(const lxp_module_iface *const *registrations, size_t count,
                   uint32_t *pin)
{
    node_destroy(scratch_node);
    if (node_genesis(scratch_node, registrations, count) != 0) return 1;
    *pin = lxp_programs_module_version(&scratch_node->kernel);
    node_destroy(scratch_node);
    return 0;
}

static int case_module_pin(void)
{
    static const char name[] = "module_pin_follows_highest_registration";
    const lxp_module_iface *v4 = programs_module_registration_v4();
    const lxp_module_iface *v4r =
        programs_module_registration_v4_storage_retirement();
    const lxp_module_iface *v5 = programs_module_registration_v5();
    const lxp_module_iface *only_v4[] = {v4};
    const lxp_module_iface *only_v4r[] = {v4r};
    const lxp_module_iface *only_v5[] = {v5};
    const lxp_module_iface *v4_then_v5[] = {v4, v5};
    const lxp_module_iface *v4r_then_v5[] = {v4r, v5};
    uint32_t pin = 0U;
    CHECK(v4->abi_version == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION &&
          v4->activity_type_count == V4_ACTIVITY_TYPES &&
          !lists(v4, LX_PROGRAMS_STORAGE_RETIREMENT), name);
    CHECK(v4r->abi_version == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION &&
          v4r->activity_type_count == V4_ACTIVITY_TYPES + 1U && lists_v4(v4r) &&
          v4r->activity_types[V4_ACTIVITY_TYPES] ==
              LX_PROGRAMS_STORAGE_RETIREMENT, name);
    CHECK(v5->module_id == LXP_MODULE_PROGRAMS &&
          v5->abi_version == LX_PROGRAMS_GUEST_ABI_V5_VERSION &&
          v5->activity_type_count == V4_ACTIVITY_TYPES + 1U && lists_v4(v5) &&
          v5->activity_types[V4_ACTIVITY_TYPES] ==
              LX_PROGRAMS_STORAGE_RETIREMENT &&
          memcmp(v5->activity_types, v4r->activity_types,
                 v5->activity_type_count * sizeof(v5->activity_types[0])) == 0,
          name);
    CHECK(lxp_programs_module_version(NULL) ==
          LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, name);
    CHECK(pin_for(NULL, 0U, &pin) == 0 &&
          pin == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, name);
    CHECK(pin_for(only_v4, 1U, &pin) == 0 &&
          pin == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, name);
    CHECK(pin_for(only_v4r, 1U, &pin) == 0 &&
          pin == LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, name);
    CHECK(pin_for(only_v5, 1U, &pin) == 0 &&
          pin == LX_PROGRAMS_GUEST_ABI_V5_VERSION, name);
    CHECK(pin_for(v4_then_v5, 2U, &pin) == 0 &&
          pin == LX_PROGRAMS_GUEST_ABI_V5_VERSION, name);
    CHECK(pin_for(v4r_then_v5, 2U, &pin) == 0 &&
          pin == LX_PROGRAMS_GUEST_ABI_V5_VERSION, name);
    CHECK(lxp_programs_module_version(&primary->kernel) ==
          LX_PROGRAMS_GUEST_ABI_V5_VERSION, name);
    return pass(name);
}

static void remove_tree(const char *directory)
{
    DIR *listing;
    struct dirent *entry;
    char path[256];
    if (directory[0] == '\0') return;
    if ((listing = opendir(directory)) != NULL) {
        while ((entry = readdir(listing)) != NULL) {
            int written;
            if (strcmp(entry->d_name, ".") == 0 ||
                strcmp(entry->d_name, "..") == 0)
                continue;
            written = snprintf(path, sizeof(path), "%s/%s", directory,
                               entry->d_name);
            if (written > 0 && (size_t)written < sizeof(path))
                (void)unlink(path);
        }
        (void)closedir(listing);
    }
    (void)rmdir(directory);
}

static void cleanup(void)
{
    (void)connection_close(&lni.link);
    (void)lni_stop();
    if (lni.daemon_started) (void)lxp_daemon_shutdown(&lni.daemon);
    remove_tree(lni.socket_directory);
    remove_tree(lni.admission_directory);
    if (primary != NULL) node_destroy(primary);
    if (scratch_node != NULL) node_destroy(scratch_node);
    if (lni.owner_ready) {
        (void)pthread_mutex_destroy(&lni.owner.mutex);
        (void)pthread_mutex_destroy(&lni.owner.receipt_mutex);
        (void)pthread_mutex_destroy(&lni.owner.publication_mutex);
        (void)pthread_mutex_destroy(&lni.owner.receipt_authority_mutex);
    }
    (void)unsetenv("LAYERX_NODE_SEQUENCER_PRIVATE_KEY");
    free(lni.scratch_bytes);
    free(primary);
    free(scratch_node);
}

static int setup(void)
{
    const lxp_module_iface *registration = programs_module_registration_v5();
    primary = (node *)calloc(1U, sizeof(node));
    scratch_node = (node *)calloc(1U, sizeof(node));
    if (primary == NULL || scratch_node == NULL ||
        public_key_for(sequencer_seed, sequencer_public) != 0)
        return 1;
    (void)memset(program_id, 0x71, 32U);
    return node_genesis(primary, &registration, 1U) != 0 || lni_init() != 0;
}

int main(void)
{
    if (setup() != 0) {
        (void)printf("NATIVE_GAPS_FAIL setup\n");
        cleanup();
        return 1;
    }
    (void)tally(case_module_pin());
    (void)tally(case_module5_retirement());
    (void)tally(case_node_info_capacity());
    (void)tally(case_prepare_refusal());
    cleanup();
    (void)printf("PASSED %zu FAILED %zu\n", passed, failed);
    return failed == 0U ? 0 : 1;
}
