#define _GNU_SOURCE

#include "layerx/programs.h"

#include "layerx/lxp_activity.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_fault.h"
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
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_state_proof.h"
#include "layerx/lxp_storage.h"

#include "lxp_daemon_lni_internal.h"

#include <openssl/evp.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <spawn.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

enum {
    ACTORS = 4,
    OWNER = 0,
    OUTSIDER = 2,
    CONTROLLER = 3,
    NETWORK_ID = 7,
    LNI_MAJOR = 1,
    LNI_MINOR = 8,
    NODE_INFO_REQUEST = 1,
    NODE_INFO_RESPONSE = 2,
    ERROR_RESPONSE = 25,
    OBSERVE = 50,
    RESERVE = 52,
    CANCEL = 56,
    ENVELOPE_FIXED_BYTES = 22,
    LNI_PROOF_BYTES = 96,
    ID_BODY_BYTES = 10,
    NODE_INFO_BYTES = 65536,
    OWNER_SCRATCH_BYTES = 2 * 1024 * 1024,
    KEEPER_OUTPUT_BYTES = 16384,
    FLOOR_BLOBS = 64,
    PROOF_CAPACITY = 6144,
    PAYLOAD_CAPACITY = LXP_PROGRAMS_RETIREMENT_MAX_PAYLOAD_BYTES + 512,
    MANIFEST_BYTES = 58,
    MUTATIONS = 520,
    CHECKPOINT_EVERY = 16,
    RESTART_AT = 260,
    MAX_ARCHIVES = 64,
    MAX_SCRIPT_LINES = 256,
    RECORD_HEADER_BYTES = 29,
    ENVELOPE_PAYLOAD = 238,
    UPDATE_METADATA = 0x0107,
    CREATE = 0x0101,
    FUND = 0x0601,
    CLAIM = 0x0602,
    FUND_AMOUNT = 1000,
    POLICY_BYTES = 307,
    LOG_BYTES = 128 * 1024 * 1024,
    BLOB_KEY_BYTES = 1 + LXP_MODULE_MAX_KEY_BYTES
};

enum {
    REC_ID = 0,
    REC_STATE = 9,
    REC_DIGEST = 10
};

_Static_assert(LXP_KERNEL_MAX_BLOBS == 512, "slot capacity changed");
_Static_assert(LXP_KERNEL_MAX_BLOB_TOTAL_BYTES == 67108864,
               "byte capacity changed");
_Static_assert(LXP_KERNEL_MAX_STAGED_BLOBS == 4, "staged capacity changed");
_Static_assert(LXP_KERNEL_MAX_MODULE_KV == 512, "kv capacity changed");
_Static_assert((int)MUTATIONS > (int)LXP_KERNEL_MAX_BLOBS, "mutations must exceed slots");

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
    uint8_t rewards[32];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_receipt receipt;
    lxp_byte_span witness;
    lxp_log log;
    bool log_open;
    bool live;
} node;

typedef struct head_view {
    uint8_t root[32];
    uint64_t next_sequence;
    uint64_t identity_sequence[ACTORS];
} head_view;

typedef struct recovery {
    node *n;
    uint64_t base;
    size_t restored;
    size_t replayed;
    size_t matched;
    size_t cleaned;
    uint64_t activity_sequence;
    bool activity_pending;
    lxp_byte_span encoded;
    lxp_result failure;
} recovery;

typedef struct proof_bytes {
    uint8_t bytes[PROOF_CAPACITY];
    uint16_t length;
} proof_bytes;

typedef struct mutation {
    uint64_t origin;
    uint8_t root[32];
    uint8_t manifest[MANIFEST_BYTES];
    uint8_t manifest_digest[32];
    uint8_t value_digest[32];
    uint32_t value_size;
    uint8_t replay_id[32];
    uint8_t witness_digest[32];
    uint32_t witness_size;
    uint32_t replay_record_bytes;
    proof_bytes head_proof;
    proof_bytes replay_proof;
    uint64_t archive;
    bool retired;
} mutation;

typedef struct archive {
    uint64_t sequence;
    lxp_snapshot_manifest_record manifest;
    size_t length;
} archive;

typedef struct peaks {
    size_t blobs;
    uint64_t bytes;
    size_t staged;
    size_t kv;
} peaks;

typedef struct reply {
    uint16_t tag;
    uint8_t payload[1U + LXP_CAPACITY_RECORD_BYTES + LXP_CAPACITY_OBSERVATION_BYTES];
    size_t length;
    uint8_t refusal_class;
    lxp_result refusal;
} reply;

typedef struct observation {
    uint64_t next_sequence;
    uint8_t root[32];
    uint64_t committed[3];
    uint64_t floor[3];
    uint64_t work[3];
    uint64_t available[3];
    uint32_t active;
} observation;

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
    char keeper_directory[64];
    char socket_path[LXP_DAEMON_LNI_SOCKET_PATH_BYTES];
    const char *keeper;
    size_t applied;
    bool daemon_started;
    bool lni_started;
    bool owner_ready;
    connection link;
    uint8_t node_info[NODE_INFO_BYTES];
    size_t node_info_length;
} lni_fixture;

typedef struct work {
    node *n;
    unsigned actor;
    uint32_t type;
    const uint8_t *payload;
    size_t length;
    uint64_t height;
} work;

static const char *const dids[ACTORS] = {
    "did:lxp:paxai-owner", "did:lxp:paxai-treasury", "did:lxp:paxai-outsider",
    "did:lxp:paxai-controller"
};
static const uint8_t seeds[ACTORS][32] = {{0x63U}, {0x64U}, {0x65U}, {0x67U}};
static const uint8_t sequencer_seed[32] = {0x66U};
static const uint8_t archive_seed_a[32] = {0x68U};
static const uint8_t archive_seed_b[32] = {0x69U};
static const uint8_t asset_id[32] = {9U};
static const uint8_t rewards_seed[] = "paxai/rewards/v1";
static const char request_domain[] = "LayerX/storage-capacity-request/v1";
static const char response_domain[] = "LayerX/storage-capacity-response/v1";

static node *primary, *replica, *scratch_node;
static lni_fixture lni;
static lxp_state_witness witness;
static uint8_t *archive_bytes;
static lxp_arena archive_arena;
static uint8_t *record_buffer;
static uint8_t *call_buffer;
static uint8_t *envelope_buffer;
static uint8_t retirement_payload[PAYLOAD_CAPACITY];
static uint8_t sequencer_public[32];
static uint8_t program_id[32];
static uint8_t chain_id[32];
static uint8_t market_id[32];
static uint8_t profile_digest[32];
static uint8_t controller_id[32];
static archive archives[MAX_ARCHIVES];
static size_t archive_count;
static mutation mutations[MUTATIONS];
static size_t mutation_count;
static size_t retired_count;
static peaks peak;
static char work_directory[64];
static char archive_a[128], archive_b[128];
static char primary_log[128], replica_log[128];
static uint64_t correlation = 1U;
static uint64_t request_serial;
static uint64_t owner_app_sequence = 1U;
static size_t passed, failed;

#define CHECK(condition, name) \
    do { \
        if (!(condition)) { \
            (void)printf("CONTINUITY_FAIL %s line %d: %s\n", name, __LINE__, \
                         #condition); \
            (void)fflush(stdout); \
            return 1; \
        } \
    } while (0)

static int pass(const char *name)
{
    ++passed;
    (void)printf("CONTINUITY_CASE %s\n", name);
    (void)fflush(stdout);
    return 0;
}

static int blocked(const char *name, const char *by)
{
    (void)printf("CONTINUITY_FAIL %s blocked-by %s\n", name, by);
    (void)fflush(stdout);
    return 1;
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

static void put_u128(uint8_t *p, uint64_t v)
{
    put_u64(p, 0U);
    put_u64(p + 8U, v);
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

static int hex_decode(const char *text, size_t text_length, uint8_t *out,
                      size_t capacity, size_t *length)
{
    size_t i;
    if (text_length == 0U || text_length % 2U != 0U ||
        text_length / 2U > capacity)
        return 1;
    for (i = 0U; i < text_length; ++i) {
        char c = text[i];
        unsigned int nibble;
        if (c >= '0' && c <= '9') nibble = (unsigned int)(c - '0');
        else if (c >= 'a' && c <= 'f') nibble = (unsigned int)(c - 'a') + 10U;
        else return 1;
        if ((i & 1U) == 0U) out[i / 2U] = (uint8_t)(nibble << 4U);
        else out[i / 2U] = (uint8_t)(out[i / 2U] | nibble);
    }
    *length = text_length / 2U;
    return 0;
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

static int join_path(char *out, size_t capacity, const char *directory,
                     const char *name)
{
    int written = snprintf(out, capacity, "%s/%s", directory, name);
    return written < 0 || (size_t)written >= capacity ? 1 : 0;
}

static void remove_in(const char *directory, const char *name)
{
    char path[256];
    if (join_path(path, sizeof(path), directory, name) == 0)
        (void)unlink(path);
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
    if (n->log_open) (void)lxp_log_close(&n->log);
    while (n->kernel.blob_count != 0U)
        free(n->kernel.blobs[--n->kernel.blob_count].bytes);
    lx_account_registry_release(&n->accounts);
    (void)lxp_state_store_destroy(&n->state);
    (void)memset(n, 0, sizeof(*n));
}

static int node_genesis(node *n)
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
        lxp_programs_account_derive(program_id, rewards_seed,
                                    sizeof(rewards_seed) - 1U,
                                    n->rewards) != LXP_OK ||
        lxp_state_store_init(&n->state, 1U) != LXP_OK ||
        lxp_state_store_bind_accounts(&n->state, &n->accounts) != LXP_OK ||
        lxp_kernel_create(&n->kernel, &n->state, &n->journal, &n->parameters,
                          0U) != LXP_OK ||
        install_metering_v1(&n->kernel) != LXP_OK ||
        lxp_kernel_register_module(&n->kernel,
                                   programs_module_registration_v5()) != LXP_OK ||
        lxp_state_store_require_account_root(&n->state) != LXP_OK)
        return 1;
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
    n->witness = (lxp_byte_span){NULL, 0U};
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
    execution.recorded_module_version = LX_PROGRAMS_GUEST_ABI_V5_VERSION;
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
    execution.replay_witness_out = type == LX_PROGRAMS_CALL ? &n->witness : NULL;
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

static size_t call_payload(uint8_t *out, const uint8_t *data, size_t length)
{
    static const uint8_t domain[] = "LXP/program-replay-profile/v1";
    static const uint8_t access[] = "LayerX/programs/access-declaration/v1\0";
    static const uint64_t budget[7] = {1000000U, 16777216U, 1048576U,
                                       1048576U, 64U, 1048576U, 4096U};
    static const uint8_t capabilities[] = {0U, 3U, 3U, 7U, 8U};
    size_t n = 34U, i, original;
    (void)memset(out, 0, 34U);
    (void)memcpy(out + n, domain, sizeof(domain)); n += sizeof(domain);
    put_u16(out + n, 1U); n += 2U;
    put_u32(out + n, 128U); n += 4U;
    put_u32(out + n, 1048576U); n += 4U;
    original = n + 4U;
    n = original;
    (void)memcpy(out + n, program_id, 32U); n += 32U;
    put_u16(out + n, LX_PROGRAMS_GUEST_ABI_V5_VERSION); n += 2U;
    put_u16(out + n, sizeof("layerx_call") - 1U); n += 2U;
    put_u32(out + n, (uint32_t)length); n += 4U;
    put_u16(out + n, (uint16_t)sizeof(capabilities)); n += 2U;
    put_u32(out + n, sizeof(access)); n += 4U;
    put_u32(out + n, 16U); n += 4U;
    for (i = 0U; i < 7U; ++i) {
        put_u64(out + n, budget[i]);
        n += 8U;
    }
    (void)memcpy(out + n, "layerx_call", sizeof("layerx_call") - 1U);
    n += sizeof("layerx_call") - 1U;
    (void)memcpy(out + n, data, length); n += length;
    (void)memcpy(out + n, capabilities, sizeof(capabilities));
    n += sizeof(capabilities);
    (void)memcpy(out + n, access, sizeof(access)); n += sizeof(access);
    put_u32(out + original - 4U, (uint32_t)(n - original));
    return n;
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

static void head_key(uint8_t key[41])
{
    (void)memcpy(key, "progstor", 8U);
    (void)memcpy(key + 8U, program_id, 32U);
    key[40] = 1U;
}

static const lxp_module_kv_entry *state_head(const node *n)
{
    uint8_t key[41];
    const lxp_module_kv_entry *entry;
    head_key(key);
    entry = find_kv(&n->kernel, key, sizeof(key));
    return entry != NULL && entry->value_length == 38U ? entry : NULL;
}

static uint64_t balance(const node *n, const uint8_t id[32])
{
    size_t i;
    for (i = 0U; i < n->accounts.count; ++i)
        if (memcmp(n->accounts.accounts[i].id, id, 32U) == 0 &&
            n->accounts.accounts[i].balance.hi == 0U)
            return n->accounts.accounts[i].balance.lo;
    return UINT64_MAX;
}

static void head_take(const node *n, head_view *view)
{
    size_t i;
    (void)memcpy(view->root, n->kernel.current_state_root, 32U);
    view->next_sequence = n->state.next_sequence;
    for (i = 0U; i < ACTORS; ++i)
        view->identity_sequence[i] = n->identities.identities[i].next_sequence;
}

static bool head_equal(const head_view *a, const head_view *b)
{
    return memcmp(a->root, b->root, 32U) == 0 &&
        a->next_sequence == b->next_sequence &&
        memcmp(a->identity_sequence, b->identity_sequence,
               sizeof(a->identity_sequence)) == 0;
}

static int seed_open(node *n, lxp_module_ctx *ctx, lxp_arena *arena,
                     uint8_t *bytes, size_t length)
{
    return lxp_arena_init(arena, bytes, length) != LXP_OK ||
        lxp_module_ctx_init(ctx, &n->kernel, LXP_MODULE_PROGRAMS, 10U,
                            n->kernel.epoch, n->state.next_sequence,
                            UINT64_C(100000000), arena, true) != LXP_OK;
}

static int register_rewards(node *n)
{
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lx_account *account;
    uint8_t id[32];
    bool created;
    if (lxp_arena_reset(&n->arena, 0U) != LXP_OK ||
        lxp_state_journal_open(&n->state, n->state.next_sequence,
                               &n->journal) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &n->kernel, LXP_MODULE_PROGRAMS, 10U, 0U,
                            n->state.next_sequence, 100000U, &n->arena,
                            true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    return lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK ||
        lxp_programs_account_register(&ctx, program_id, rewards_seed,
                                      sizeof(rewards_seed) - 1U, asset_id,
                                      &account, &created) != LXP_OK ||
        !created ||
        lxp_module_ctx_prepare_commit(&ctx) != LXP_OK ||
        lxp_state_journal_commit(&n->journal) != LXP_OK ||
        lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lxp_state_root(&n->kernel, n->kernel.current_state_root) != LXP_OK ||
        lxp_programs_account_derive(program_id, rewards_seed,
                                    sizeof(rewards_seed) - 1U, id) != LXP_OK ||
        memcmp(id, n->rewards, 32U) != 0;
}

static size_t live_blob_keys(const node *n, uint8_t keys[][32],
                             uint64_t *bytes)
{
    size_t i, count = 0U;
    *bytes = 0U;
    for (i = 0U; i < n->kernel.blob_count; ++i)
        if (!n->kernel.blobs[i].deleted) {
            (void)memcpy(keys[count++], n->kernel.blobs[i].key, 32U);
            *bytes += n->kernel.blobs[i].length;
        }
    return count;
}

static size_t key_difference(uint8_t a[][32], size_t a_count,
                             uint8_t b[][32], size_t b_count)
{
    size_t i, j, missing = 0U;
    for (i = 0U; i < a_count; ++i) {
        for (j = 0U; j < b_count; ++j)
            if (memcmp(a[i], b[j], 32U) == 0) break;
        if (j == b_count) ++missing;
    }
    return missing;
}

static void peak_after(const node *n, uint8_t before_keys[][32],
                       size_t before_count)
{
    static uint8_t after_keys[LXP_KERNEL_MAX_BLOBS][32];
    uint64_t bytes_after;
    size_t after_count = live_blob_keys(n, after_keys, &bytes_after);
    size_t staged = key_difference(after_keys, after_count, before_keys,
                                   before_count) +
        key_difference(before_keys, before_count, after_keys, after_count);
    if (after_count > peak.blobs) peak.blobs = after_count;
    if (bytes_after > peak.bytes) peak.bytes = bytes_after;
    if (staged > peak.staged) peak.staged = staged;
    if (n->kernel.module_kv_count > peak.kv)
        peak.kv = n->kernel.module_kv_count;
}

static int run(node *n, unsigned actor, uint32_t type, const uint8_t *payload,
               size_t length, lxp_result *status)
{
    static uint8_t before_keys[LXP_KERNEL_MAX_BLOBS][32];
    lxp_identity *identity = &n->identities.identities[actor];
    const uint64_t sequence = n->state.next_sequence;
    const uint64_t account_sequence = identity->next_sequence;
    uint64_t bytes_before;
    size_t before_count;
    lxp_byte_span encoded;
    if (length > LXP_MAX_ACTIVITY_BYTES) return 1;
    before_count = live_blob_keys(n, before_keys, &bytes_before);
    *status = execute(n, actor, type, payload, length);
    if (*status != LXP_OK)
        return n->state.next_sequence == sequence &&
            identity->next_sequence == account_sequence ? 0 : 1;
    if (n->state.next_sequence != sequence + 1U) return 1;
    if (n->log_open) {
        record_buffer[0] = (uint8_t)actor;
        put_u64(record_buffer + 1U, n->height);
        put_u32(record_buffer + 9U, type);
        put_u64(record_buffer + 13U, account_sequence);
        put_u64(record_buffer + 21U, identity->next_sequence);
        (void)memcpy(record_buffer + RECORD_HEADER_BYTES, payload, length);
        if (lxp_receipt_encode(&n->receipt, true, &n->arena, &encoded) !=
                LXP_OK ||
            encoded.length > UINT32_MAX ||
            lxp_log_append(&n->log, LXP_LOG_ACTIVITY, sequence, record_buffer,
                           (uint32_t)(RECORD_HEADER_BYTES + length), NULL) !=
                LXP_OK ||
            lxp_log_append(&n->log, LXP_LOG_RECEIPT, sequence, encoded.bytes,
                           (uint32_t)encoded.length, NULL) != LXP_OK ||
            lxp_log_sync(&n->log) != LXP_OK)
            return 1;
    }
    if (n == primary) peak_after(n, before_keys, before_count);
    return 0;
}

static lxp_result work_run(void *context)
{
    work *w = (work *)context;
    lxp_result status;
    w->n->height = w->height;
    if (run(w->n, w->actor, w->type, w->payload, w->length, &status) != 0)
        return LXP_FATAL_INVARIANT;
    return status;
}

static int archive_path(char *out, size_t capacity, const char *directory,
                        uint64_t sequence)
{
    int written = snprintf(out, capacity, "%s/%020llu.lxs", directory,
                           (unsigned long long)sequence);
    return written < 0 || (size_t)written >= capacity ? 1 : 0;
}

static int archive_write(node *n, const char *directory,
                         lxp_snapshot_manifest_record *manifest,
                         size_t *length)
{
    lxp_kernel_batch_boundary boundary;
    lxp_byte_span snapshot;
    uint64_t sequence;
    if (lxp_arena_reset(&archive_arena, 0U) != LXP_OK ||
        lxp_kernel_batch_boundary_read(&n->kernel, &boundary) != LXP_OK ||
        boundary.next_sequence == 0U)
        return 1;
    sequence = boundary.next_sequence - 1U;
    if (lxp_snapshot_write(&n->kernel, sequence, &archive_arena, &snapshot) !=
            LXP_OK ||
        lxp_snapshot_manifest(snapshot.bytes, snapshot.length, sequence,
                              boundary.canonical_state_root,
                              boundary.receipt_state_root, manifest) != LXP_OK ||
        lxp_snapshot_store_write(directory, manifest, snapshot.bytes,
                                 snapshot.length) != LXP_OK)
        return 1;
    *length = snapshot.length;
    return 0;
}

static lxp_result archive_work(void *context)
{
    lxp_snapshot_manifest_record manifest;
    size_t length;
    (void)context;
    return archive_write(primary, archive_a, &manifest, &length) == 0 ?
        LXP_OK : LXP_FATAL_INVARIANT;
}

static bool manifest_equal(const lxp_snapshot_manifest_record *a,
                           const lxp_snapshot_manifest_record *b)
{
    return a->global_sequence == b->global_sequence &&
        memcmp(a->canonical_state_root, b->canonical_state_root, 32U) == 0 &&
        memcmp(a->receipt_state_root, b->receipt_state_root, 32U) == 0 &&
        memcmp(a->snapshot_digest, b->snapshot_digest, 32U) == 0 &&
        a->migration.present == b->migration.present;
}

static int archive_load(node *n, const char *directory, uint64_t sequence,
                        lxp_snapshot_manifest_record *manifest)
{
    char path[256];
    lxp_byte_span snapshot;
    return archive_path(path, sizeof(path), directory, sequence) != 0 ||
        lxp_arena_reset(&archive_arena, 0U) != LXP_OK ||
        lxp_snapshot_store_read(path, &archive_arena, manifest, &snapshot) !=
            LXP_OK ||
        manifest->global_sequence != sequence ||
        lxp_snapshot_load(snapshot.bytes, snapshot.length, manifest,
                          &n->kernel) != LXP_OK ||
        lxp_snapshot_verify_root(&n->kernel, manifest) != LXP_OK;
}

static int newest_archive(const char *directory, uint64_t *sequence,
                          size_t *cleaned)
{
    DIR *listing = opendir(directory);
    struct dirent *entry;
    bool found = false;
    if (listing == NULL) return 1;
    *sequence = 0U;
    while ((entry = readdir(listing)) != NULL) {
        size_t length = strlen(entry->d_name);
        if (length == 28U && strcmp(entry->d_name + 20U, ".lxs.tmp") == 0) {
            if (cleaned != NULL) {
                remove_in(directory, entry->d_name);
                ++*cleaned;
            }
        } else if (length == 24U && strcmp(entry->d_name + 20U, ".lxs") == 0) {
            char *end = NULL;
            unsigned long long value = strtoull(entry->d_name, &end, 10);
            if (end != entry->d_name + 20U) continue;
            if (!found || value > *sequence) *sequence = (uint64_t)value;
            found = true;
        }
    }
    (void)closedir(listing);
    return found ? 0 : 1;
}

static lxp_result replay_record(void *context,
                                const lxp_log_record_header *header,
                                const uint8_t *body)
{
    recovery *r = (recovery *)context;
    node *n = r->n;
    const uint64_t sequence = header->global_sequence;
    lxp_result status = LXP_OK;
    if (header->record_kind == LXP_LOG_ACTIVITY) {
        lxp_identity *identity;
        uint64_t account_sequence, next_after;
        if (r->activity_pending || header->body_length < RECORD_HEADER_BYTES ||
            body[0] >= ACTORS)
            status = LXP_ERR_LOG_CORRUPT;
        if (status == LXP_OK) {
            identity = &n->identities.identities[body[0]];
            account_sequence = get_u64(body + 13U);
            next_after = get_u64(body + 21U);
            r->activity_pending = true;
            r->activity_sequence = sequence;
            r->encoded = (lxp_byte_span){NULL, 0U};
            if (sequence <= r->base) {
                identity->next_sequence = next_after;
                ++r->restored;
                return LXP_OK;
            }
            if (n->state.next_sequence != sequence ||
                identity->next_sequence != account_sequence)
                status = LXP_ERR_SEQUENCE_MISMATCH;
            if (status == LXP_OK) {
                n->height = get_u64(body + 1U);
                status = execute(n, body[0], get_u32(body + 9U),
                                 body + RECORD_HEADER_BYTES,
                                 header->body_length - RECORD_HEADER_BYTES);
            }
            if (status == LXP_OK &&
                (identity->next_sequence != next_after ||
                 n->state.next_sequence != sequence + 1U))
                status = LXP_ERR_SEQUENCE_MISMATCH;
            if (status == LXP_OK &&
                lxp_receipt_encode(&n->receipt, true, &n->arena, &r->encoded) !=
                    LXP_OK)
                status = LXP_FATAL_INVARIANT;
            if (status == LXP_OK) ++r->replayed;
        }
    } else if (header->record_kind == LXP_LOG_RECEIPT) {
        if (!r->activity_pending || sequence != r->activity_sequence)
            status = LXP_ERR_LOG_CORRUPT;
        r->activity_pending = false;
        if (status == LXP_OK && sequence > r->base) {
            if (r->encoded.length != header->body_length ||
                memcmp(r->encoded.bytes, body, header->body_length) != 0)
                status = LXP_ERR_ROOT_MISMATCH;
            else
                ++r->matched;
        }
    } else {
        status = LXP_ERR_LOG_CORRUPT;
    }
    if (status != LXP_OK) r->failure = status;
    return status;
}

static int node_recover(node *n, const char *directory, uint64_t sequence,
                        const char *log_path, recovery *r)
{
    lxp_snapshot_manifest_record manifest;
    (void)memset(r, 0, sizeof(*r));
    node_destroy(n);
    if (node_genesis(n) != 0) return 1;
    if (sequence == 0U && newest_archive(directory, &sequence, &r->cleaned) != 0)
        return 1;
    if (archive_load(n, directory, sequence, &manifest) != 0 ||
        n->state.next_sequence != sequence + 1U)
        return 1;
    r->n = n;
    r->base = sequence;
    if (log_path == NULL) return 0;
    if (lxp_log_open(&n->log, log_path) != LXP_OK) return 1;
    n->log_open = true;
    if (lxp_log_recover(&n->log, replay_record, r) != LXP_OK ||
        r->failure != LXP_OK || r->activity_pending) {
        (void)printf("CONTINUITY_NOTE recovery failure=%d replayed=%zu\n",
                     (int)r->failure, r->replayed);
        return 1;
    }
    return 0;
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

static int observe(observation *o)
{
    uint8_t body[2];
    reply answer;
    size_t axis;
    put_u16(body, 1U);
    if (lni_call(OBSERVE, body, sizeof(body), &answer) != 0 ||
        answer.tag != OBSERVE + 1U ||
        answer.length != LXP_CAPACITY_OBSERVATION_BYTES ||
        get_u16(answer.payload) != 1U ||
        get_u32(answer.payload + 76U) != LXP_KERNEL_MAX_BLOBS ||
        get_u64(answer.payload + 80U) != LXP_KERNEL_MAX_BLOB_TOTAL_BYTES ||
        get_u32(answer.payload + 88U) != LXP_KERNEL_MAX_MODULE_KV)
        return 1;
    (void)memset(o, 0, sizeof(*o));
    o->next_sequence = get_u64(answer.payload + 36U);
    (void)memcpy(o->root, answer.payload + 44U, 32U);
    for (axis = 0U; axis < 5U; ++axis) {
        uint64_t *target[5] = {o->committed, o->floor, NULL, o->work,
                               o->available};
        const uint8_t *demand = answer.payload + 92U + axis * 16U;
        if (target[axis] == NULL) continue;
        target[axis][0] = get_u32(demand);
        target[axis][1] = get_u64(demand + 4U);
        target[axis][2] = get_u32(demand + 12U);
    }
    o->active = get_u32(answer.payload + 172U);
    return 0;
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

static int reserve(const cap_request *r, reply *answer)
{
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    uint8_t message[sizeof(request_domain) - 1U + LXP_CAPACITY_REQUEST_BYTES];
    uint8_t digest[32];
    request_encode(r, body);
    (void)memcpy(message, request_domain, sizeof(request_domain) - 1U);
    (void)memcpy(message + sizeof(request_domain) - 1U, body, sizeof(body));
    if (lxp_hash_sha256(message, sizeof(message), digest) != LXP_OK ||
        lni_call(RESERVE, body, sizeof(body), answer) != 0 ||
        answer->tag != RESERVE + 1U)
        return 1;
    return answer->length != 1U + LXP_CAPACITY_RECORD_BYTES ||
        answer->payload[0] != 0U ||
        memcmp(answer->payload + 1U + REC_DIGEST, digest, 32U) != 0;
}

static int cancel(uint64_t id, reply *answer)
{
    uint8_t body[ID_BODY_BYTES];
    put_u16(body, 1U);
    put_u64(body + 2U, id);
    return lni_call(CANCEL, body, sizeof(body), answer) != 0 ||
        answer->tag != CANCEL + 1U ||
        answer->payload[REC_STATE] != LXP_CAPACITY_CANCELLED;
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

static int lni_init(const char *keeper)
{
    lxp_daemon_configuration daemon_configuration;
    char key_hex[65];
    int written;
    lni.keeper = keeper;
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
    (void)strcpy(lni.socket_directory, "/tmp/lxp-continuity-socket-XXXXXX");
    (void)strcpy(lni.admission_directory, "/tmp/lxp-continuity-node-XXXXXX");
    (void)strcpy(lni.keeper_directory, "/tmp/lxp-continuity-keeper-XXXXXX");
    if (mkdtemp(lni.socket_directory) == NULL ||
        mkdtemp(lni.admission_directory) == NULL ||
        mkdtemp(lni.keeper_directory) == NULL ||
        chmod(lni.socket_directory, 0750) != 0 ||
        chmod(lni.admission_directory, 0700) != 0)
        return 1;
    written = snprintf(lni.socket_path, sizeof(lni.socket_path), "%s/lni.sock",
                       lni.socket_directory);
    if (written < 0 || (size_t)written >= sizeof(lni.socket_path)) return 1;
    return lni_serve() != 0 || connection_open(&lni.link) != 0;
}

static int keeper_run(const char *const *extra, size_t extra_count,
                      char *output, int *exit_code)
{
    char network[16];
    char sequencer[65];
    char *argv[40];
    char *environment[] = {NULL};
    posix_spawn_file_actions_t actions;
    size_t argc = 0U, length = 0U, i;
    pid_t child;
    int pipe_descriptors[2];
    int status;
    if (extra_count + 10U > sizeof(argv) / sizeof(argv[0])) return 1;
    (void)snprintf(network, sizeof(network), "%u", (unsigned)NETWORK_ID);
    hex_encode(sequencer_public, 32U, sequencer);
    argv[argc++] = (char *)lni.keeper;
    argv[argc++] = (char *)extra[0];
    argv[argc++] = (char *)"--state";
    argv[argc++] = lni.keeper_directory;
    argv[argc++] = (char *)"--network-id";
    argv[argc++] = network;
    argv[argc++] = (char *)"--sequencer";
    argv[argc++] = sequencer;
    argv[argc++] = (char *)"--socket";
    argv[argc++] = lni.socket_path;
    for (i = 1U; i < extra_count; ++i) argv[argc++] = (char *)extra[i];
    argv[argc] = NULL;
    (void)fflush(stdout);
    if (pipe2(pipe_descriptors, O_CLOEXEC) != 0) return 1;
    if (posix_spawn_file_actions_init(&actions) != 0 ||
        posix_spawn_file_actions_adddup2(&actions, pipe_descriptors[1], 1) != 0 ||
        posix_spawn(&child, lni.keeper, &actions, NULL, argv, environment) != 0) {
        (void)close(pipe_descriptors[0]);
        (void)close(pipe_descriptors[1]);
        return 1;
    }
    (void)posix_spawn_file_actions_destroy(&actions);
    (void)close(pipe_descriptors[1]);
    for (;;) {
        ssize_t got;
        if (length + 1U >= KEEPER_OUTPUT_BYTES) break;
        got = read(pipe_descriptors[0], output + length,
                   KEEPER_OUTPUT_BYTES - 1U - length);
        if (got > 0) length += (size_t)got;
        else if (got < 0 && errno == EINTR) continue;
        else break;
    }
    output[length] = '\0';
    (void)close(pipe_descriptors[0]);
    while (waitpid(child, &status, 0) < 0)
        if (errno != EINTR) return 1;
    if (!WIFEXITED(status)) return 1;
    *exit_code = WEXITSTATUS(status);
    (void)fprintf(stdout, "%s", output);
    return 0;
}

static int primary_pause(void)
{
    int failure = connection_close(&lni.link);
    return lni_stop() != 0 || failure != 0;
}

static int primary_resume(void)
{
    return lni_serve() != 0 || connection_open(&lni.link) != 0;
}

static int primary_restart(const char *name, recovery *rec)
{
    head_view pre, post;
    uint64_t newest;
    CHECK(primary_pause() == 0, name);
    head_take(primary, &pre);
    CHECK(newest_archive(archive_a, &newest, NULL) == 0, name);
    CHECK(node_recover(primary, archive_a, 0U, primary_log, rec) == 0, name);
    head_take(primary, &post);
    CHECK(head_equal(&pre, &post), name);
    CHECK(rec->base == newest && rec->cleaned == 0U &&
          rec->replayed == pre.next_sequence - 1U - newest &&
          rec->matched == rec->replayed, name);
    CHECK(primary_resume() == 0, name);
    return 0;
}

static int crash_pair(const char *name, work *w)
{
    static uint8_t before_keys[LXP_KERNEL_MAX_BLOBS][32];
    head_view pre, post;
    recovery rec;
    uint64_t newest, expected, before_bytes;
    size_t before_count;
    int exit_status = 0;
    CHECK(primary_pause() == 0, name);
    head_take(primary, &pre);
    before_count = live_blob_keys(primary, before_keys, &before_bytes);
    CHECK(newest_archive(archive_a, &newest, NULL) == 0, name);
    expected = pre.next_sequence - 1U - newest;
    (void)fflush(stdout);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_BODY_WRITTEN, 2U, work_run,
                                      w, &exit_status) == LXP_OK, name);
    CHECK(node_recover(primary, archive_a, 0U, primary_log, &rec) == 0, name);
    head_take(primary, &post);
    CHECK(head_equal(&pre, &post), name);
    CHECK(rec.base == newest && rec.cleaned == 0U && rec.replayed == expected &&
          rec.matched == expected, name);
    (void)fflush(stdout);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_LOG_SYNCED, 1U, work_run, w,
                                      &exit_status) == LXP_OK, name);
    CHECK(node_recover(primary, archive_a, 0U, primary_log, &rec) == 0, name);
    CHECK(primary->state.next_sequence == pre.next_sequence + 1U &&
          rec.replayed == expected + 1U && rec.matched == rec.replayed, name);
    peak_after(primary, before_keys, before_count);
    CHECK(primary_resume() == 0, name);
    return 0;
}

static int checkpoint(bool write_a, const char *name)
{
    lxp_snapshot_manifest_record written, read_a, read_b;
    lxp_byte_span snapshot_a, snapshot_b;
    recovery rec;
    char path_a[256], path_b[256];
    uint8_t root[32];
    size_t length_a = 0U, length_b = 0U;
    const uint64_t sequence = primary->state.next_sequence - 1U;
    CHECK(archive_count < MAX_ARCHIVES, name);
    if (write_a)
        CHECK(archive_write(primary, archive_a, &written, &length_a) == 0, name);
    CHECK(archive_write(primary, archive_b, &written, &length_b) == 0, name);
    CHECK(archive_path(path_a, sizeof(path_a), archive_a, sequence) == 0 &&
          archive_path(path_b, sizeof(path_b), archive_b, sequence) == 0, name);
    CHECK(lxp_arena_reset(&archive_arena, 0U) == LXP_OK &&
          lxp_snapshot_store_read(path_a, &archive_arena, &read_a,
                                  &snapshot_a) == LXP_OK &&
          lxp_snapshot_store_read(path_b, &archive_arena, &read_b,
                                  &snapshot_b) == LXP_OK, name);
    CHECK(snapshot_a.length == length_b && snapshot_b.length == length_b &&
          (!write_a || length_a == length_b) &&
          memcmp(snapshot_a.bytes, snapshot_b.bytes, length_b) == 0 &&
          manifest_equal(&read_a, &read_b) &&
          manifest_equal(&written, &read_b) &&
          read_b.global_sequence == sequence, name);
    CHECK(node_recover(scratch_node, archive_b, sequence, NULL, &rec) == 0,
          name);
    CHECK(memcmp(scratch_node->kernel.current_state_root,
                 primary->kernel.current_state_root, 32U) == 0, name);
    CHECK(lxp_state_root(&scratch_node->kernel, root) == LXP_OK &&
          memcmp(root, read_b.canonical_state_root, 32U) == 0, name);
    archives[archive_count].sequence = sequence;
    archives[archive_count].manifest = read_b;
    archives[archive_count].length = length_b;
    ++archive_count;
    return 0;
}

static int prove(const node *n, uint16_t module_id, const uint8_t *key,
                 size_t key_length, const uint8_t root[32], proof_bytes *out)
{
    size_t length = 0U;
    if (lxp_state_proof_build(&n->kernel, module_id,
                              (lxp_byte_span){key, key_length},
                              &witness) != LXP_OK ||
        lxp_state_proof_verify(&witness, root) != LXP_OK ||
        lxp_state_proof_encode(&witness, out->bytes, sizeof(out->bytes),
                               &length) != LXP_OK ||
        length > UINT16_MAX)
        return 1;
    out->length = (uint16_t)length;
    return 0;
}

typedef struct programs_view {
    uint32_t blobs;
    uint32_t entries;
    uint8_t programs_digest[32];
    uint8_t others_digest[32];
    uint8_t control[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
} programs_view;

static int view_take(const lxp_kernel *kernel, programs_view *view)
{
    static const uint8_t control_key[] = "progretire/v1";
    lxp_hash_context programs, others;
    const lxp_module_kv_entry *control;
    size_t i;
    lxp_result status = LXP_OK;
    (void)memset(view, 0, sizeof(*view));
    lxp_hash_init(&programs);
    lxp_hash_init(&others);
    for (i = 0U; status == LXP_OK && i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[i];
        lxp_hash_context *target =
            entry->module_id == LXP_MODULE_PROGRAMS ? &programs : &others;
        uint8_t header[8];
        put_u16(header, entry->module_id);
        put_u16(header + 2U, entry->key_length);
        put_u32(header + 4U, entry->value_length);
        if (entry->module_id == LXP_MODULE_PROGRAMS) ++view->entries;
        status = lxp_hash_update(target, header, sizeof(header));
        if (status == LXP_OK)
            status = lxp_hash_update(target, entry->key, entry->key_length);
        if (status == LXP_OK)
            status = lxp_hash_update(target, entry->value, entry->value_length);
    }
    for (i = 0U; status == LXP_OK && i < kernel->blob_count; ++i) {
        const lxp_module_blob *blob = &kernel->blobs[i];
        lxp_hash_context *target =
            blob->module_id == LXP_MODULE_PROGRAMS ? &programs : &others;
        if (blob->module_id == LXP_MODULE_PROGRAMS) ++view->blobs;
        status = lxp_hash_update(target, blob->key, 32U);
        if (status == LXP_OK && blob->length != 0U)
            status = lxp_hash_update(target, blob->bytes, blob->length);
    }
    if (status == LXP_OK) status = lxp_hash_final(&programs, view->programs_digest);
    if (status == LXP_OK) status = lxp_hash_final(&others, view->others_digest);
    control = find_kv(kernel, control_key, sizeof(control_key) - 1U);
    if (control != NULL) {
        if (control->value_length != LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES)
            return 1;
        (void)memcpy(view->control, control->value, control->value_length);
    }
    return status == LXP_OK ? 0 : 1;
}

typedef struct candidate {
    uint8_t class_id;
    bool has_ns;
    uint8_t digest[32];
    uint32_t size;
    uint64_t origin;
    uint8_t origin_root[32];
    uint8_t replay_id[32];
    const proof_bytes *proof;
    const uint8_t *manifest;
    uint16_t manifest_length;
} candidate;

static void mutation_candidates(const mutation *m, candidate c[3])
{
    size_t i;
    (void)memset(c, 0, 3U * sizeof(*c));
    for (i = 0U; i < 3U; ++i) {
        c[i].origin = m->origin;
        (void)memcpy(c[i].origin_root, m->root, 32U);
    }
    c[0].class_id = LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST;
    c[0].has_ns = true;
    (void)memcpy(c[0].digest, m->manifest_digest, 32U);
    c[0].size = MANIFEST_BYTES;
    c[0].proof = &m->head_proof;
    c[0].manifest = m->manifest;
    c[0].manifest_length = MANIFEST_BYTES;
    c[1] = c[0];
    c[1].class_id = LXP_PROGRAMS_RETIREMENT_CLASS_VALUE;
    (void)memcpy(c[1].digest, m->value_digest, 32U);
    c[1].size = m->value_size;
    c[2].class_id = LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY;
    (void)memcpy(c[2].digest, m->witness_digest, 32U);
    c[2].size = m->witness_size;
    (void)memcpy(c[2].replay_id, m->replay_id, 32U);
    c[2].proof = &m->replay_proof;
}

static size_t retirement_encode(const archive *a, const candidate *c,
                                uint8_t count, size_t present)
{
    uint8_t *out = retirement_payload;
    uint8_t digest[32];
    size_t cursor = LXP_PROGRAMS_RETIREMENT_HEADER_BYTES, i;
    (void)memset(out, 0, sizeof(retirement_payload));
    out[0] = LXP_PROGRAMS_RETIREMENT_REQUEST_VERSION;
    out[1] = count;
    put_u32(out + 4U, NETWORK_ID);
    put_u16(out + 8U, LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION);
    (void)memcpy(out + 10U, profile_digest, 32U);
    (void)memcpy(out + 42U, controller_id, 32U);
    put_u64(out + 74U, primary->state.next_sequence);
    (void)memcpy(out + 82U, primary->kernel.current_state_root, 32U);
    put_u64(out + 114U, a->sequence);
    (void)memcpy(out + 122U, a->manifest.canonical_state_root, 32U);
    (void)memcpy(out + 154U, a->manifest.snapshot_digest, 32U);
    put_u64(out + 186U, (uint64_t)a->length);
    put_u64(out + 194U, 1U);
    put_u64(out + 202U, a->sequence);
    (void)memcpy(out + 210U, a->manifest.receipt_state_root, 32U);
    for (i = 0U; i < present; ++i) {
        uint8_t *fixed = out + cursor;
        if (cursor + LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES +
                c[i].proof->length + c[i].manifest_length >
            sizeof(retirement_payload))
            return 0U;
        fixed[0] = c[i].class_id;
        if (c[i].has_ns) {
            fixed[1] = 33U;
            (void)memcpy(fixed + 4U, program_id, 32U);
            fixed[36] = 1U;
        }
        (void)memcpy(fixed + 37U, c[i].digest, 32U);
        put_u32(fixed + 69U, c[i].size);
        put_u64(fixed + 73U, c[i].origin);
        (void)memcpy(fixed + 81U, c[i].origin_root, 32U);
        (void)memcpy(fixed + 113U, c[i].replay_id, 32U);
        put_u16(fixed + 145U, c[i].proof->length);
        put_u16(fixed + 147U, c[i].manifest_length);
        cursor += LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES;
        (void)memcpy(out + cursor, c[i].proof->bytes, c[i].proof->length);
        cursor += c[i].proof->length;
        if (c[i].manifest_length != 0U)
            (void)memcpy(out + cursor, c[i].manifest, c[i].manifest_length);
        cursor += c[i].manifest_length;
    }
    if (lxp_programs_retirement_certificate_digest(out, cursor, digest) == LXP_OK &&
        (sign_raw(archive_seed_a, digest, 32U, out + 242U) != 0 ||
         sign_raw(archive_seed_b, digest, 32U, out + 306U) != 0))
        return 0U;
    return cursor;
}

typedef struct ledger_view {
    uint8_t ids[64][32];
    lxp_u128 balances[64];
    size_t count;
} ledger_view;

static int ledger_take(const node *n, ledger_view *view)
{
    size_t i;
    if (n->accounts.count > 64U) return 1;
    view->count = n->accounts.count;
    for (i = 0U; i < view->count; ++i) {
        (void)memcpy(view->ids[i], n->accounts.accounts[i].id, 32U);
        view->balances[i] = n->accounts.accounts[i].balance;
    }
    return 0;
}

static int ledger_fee_only(const node *n, const ledger_view *before,
                           const uint8_t payer[32], lxp_u128 fee)
{
    static const uint8_t treasury_name[] = "system:fees";
    ledger_view after;
    uint8_t treasury[32];
    size_t i;
    if (ledger_take(n, &after) != 0 || after.count != before->count ||
        lx_account_id_from_string(treasury_name, sizeof(treasury_name) - 1U,
                                  treasury) != LXP_OK)
        return 1;
    for (i = 0U; i < after.count; ++i) {
        lxp_u128 expected = before->balances[i];
        if (memcmp(after.ids[i], before->ids[i], 32U) != 0) return 1;
        if (memcmp(after.ids[i], payer, 32U) == 0 &&
            lxp_u128_sub(before->balances[i], fee, &expected) != LXP_OK)
            return 1;
        if (memcmp(after.ids[i], treasury, 32U) == 0 &&
            lxp_u128_add(before->balances[i], fee, &expected) != LXP_OK)
            return 1;
        if (lxp_u128_cmp(after.balances[i], expected) != 0) return 1;
    }
    return 0;
}

static int retire_mutation(const char *name, size_t index, const archive *a,
                           bool crash)
{
    static const uint8_t deleted[3] = {1U, 1U, 1U};
    mutation *m = &mutations[index];
    programs_view before, after;
    ledger_view accounts;
    candidate c[3];
    uint8_t root_before[32], root[32], key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
    const lxp_effect *header;
    lxp_result status = LXP_OK;
    size_t length, i;
    mutation_candidates(m, c);
    length = retirement_encode(a, c, 3U, 3U);
    CHECK(length != 0U, name);
    for (i = 0U; i < 3U; ++i) CHECK(find_blob(&primary->kernel, c[i].digest) != NULL, name);
    CHECK(view_take(&primary->kernel, &before) == 0 &&
          ledger_take(primary, &accounts) == 0, name);
    (void)memcpy(root_before, primary->kernel.current_state_root, 32U);
    if (crash) {
        work w = {primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
                  retirement_payload, length, primary->height};
        CHECK(crash_pair(name, &w) == 0, name);
    } else {
        CHECK(run(primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
                  retirement_payload, length, &status) == 0, name);
    }
    if (status != LXP_OK || primary->receipt.result_code != LXP_OK)
        (void)printf("CONTINUITY_NOTE %s status=%d result=%d\n", name,
                     (int)status, (int)primary->receipt.result_code);
    CHECK(status == LXP_OK && primary->receipt.result_code == LXP_OK, name);
    CHECK(view_take(&primary->kernel, &after) == 0 &&
          lxp_state_root(&primary->kernel, root) == LXP_OK &&
          memcmp(root, primary->kernel.current_state_root, 32U) == 0 &&
          memcmp(root, primary->receipt.resulting_state_root, 32U) == 0 &&
          memcmp(root, root_before, 32U) != 0, name);
    CHECK(after.blobs + 3U == before.blobs && after.entries + 1U == before.entries &&
          memcmp(after.others_digest, before.others_digest, 32U) == 0, name);
    CHECK(get_u64(after.control + 179U) == a->sequence &&
          get_u64(after.control + 187U) == get_u64(before.control + 187U) + 1U &&
          get_u64(after.control + 195U) == get_u64(before.control + 195U) + 3U &&
          get_u64(after.control + 211U) == get_u64(before.control + 211U) + 1U &&
          memcmp(after.control, before.control, 179U) == 0, name);
    CHECK(primary->receipt.effects.count == 4U, name);
    for (i = 0U; i < 4U; ++i)
        CHECK(primary->receipt.effects.effects[i].kind == LXP_EFFECT_EVENT &&
              primary->receipt.effects.effects[i].event_type ==
                  LXP_PROGRAMS_RETIREMENT_EVENT, name);
    header = &primary->receipt.effects.effects[0];
    CHECK(header->body_length == 226U && header->body[199] == 3U &&
          header->body[200] == 3U && header->body[201] == 1U &&
          memcmp(header->body + 39U, root_before, 32U) == 0 &&
          get_u32(header->body + 210U) == before.blobs &&
          get_u32(header->body + 214U) == after.blobs &&
          get_u32(header->body + 218U) == before.entries &&
          get_u32(header->body + 222U) == after.entries, name);
    for (i = 0U; i < 3U; ++i) {
        const lxp_effect *frame = &primary->receipt.effects.effects[i + 1U];
        CHECK(frame->body_length == 117U && frame->body[7] == c[i].class_id &&
              frame->body[8] == deleted[i] &&
              memcmp(frame->body + 9U, c[i].digest, 32U) == 0 &&
              find_blob(&primary->kernel, c[i].digest) == NULL, name);
    }
    lxp_programs_replay_record_key(m->replay_id, key);
    CHECK(find_kv(&primary->kernel, key, sizeof(key)) == NULL, name);
    CHECK(ledger_fee_only(primary, &accounts, primary->account_ids[CONTROLLER],
                          primary->receipt.fee_charged) == 0, name);
    m->retired = true;
    m->archive = a->sequence;
    ++retired_count;
    return 0;
}

static size_t market_envelope(uint16_t selector, unsigned actor,
                              uint64_t sequence, const uint8_t *payload,
                              size_t length)
{
    uint8_t *e = envelope_buffer;
    const size_t total = ENVELOPE_PAYLOAD + length + 1U;
    (void)memset(e, 0, total);
    (void)memcpy(e, "PAXAI1", 6U);
    put_u16(e + 6U, 1U);
    put_u16(e + 8U, selector);
    (void)memcpy(e + 10U, chain_id, 32U);
    (void)memcpy(e + 42U, program_id, 32U);
    (void)memcpy(e + 74U, market_id, 32U);
    (void)memcpy(e + 106U, primary->principals[actor], 32U);
    put_u64(e + 138U, 0U);
    put_u64(e + 146U, 1U);
    put_u64(e + 186U, sequence);
    put_u64(e + 194U, 1000000U);
    e[202] = 0x5aU;
    put_u64(e + 226U, ++request_serial);
    put_u32(e + 234U, (uint32_t)length);
    (void)memcpy(e + ENVELOPE_PAYLOAD, payload, length);
    return total;
}

static int app_call(const char *name, unsigned actor, uint64_t height,
                    const uint8_t *envelope, size_t length, bool crash,
                    bool *applied)
{
    const lxp_module_kv_entry *head = state_head(primary);
    const lxp_program_outcome *outcome;
    uint8_t before[32] = {0U};
    const bool had = head != NULL;
    lxp_result status = LXP_OK;
    size_t size;
    *applied = false;
    if (had) (void)memcpy(before, head->value + 6U, 32U);
    size = call_payload(call_buffer, envelope, length);
    if (crash) {
        work w = {primary, actor, LX_PROGRAMS_CALL, call_buffer, size, height};
        CHECK(crash_pair(name, &w) == 0, name);
    } else {
        primary->height = height;
        CHECK(run(primary, actor, LX_PROGRAMS_CALL, call_buffer, size,
                  &status) == 0, name);
    }
    outcome = &primary->receipt.program_outcome;
    head = state_head(primary);
    *applied = status == LXP_OK && primary->receipt.result_code == LXP_OK &&
        outcome->present &&
        outcome->terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS &&
        head != NULL && (!had || memcmp(before, head->value + 6U, 32U) != 0);
    if (!*applied)
        (void)printf("CONTINUITY_NOTE %s status=%d result=%d terminal=%u\n",
                     name, (int)status, (int)primary->receipt.result_code,
                     outcome->present ? (unsigned)outcome->terminal_kind : 0U);
    return 0;
}

static int capture(const char *name, mutation *m, uint64_t origin)
{
    static const uint8_t replay_prefix[] = "progreplay/v1/";
    const lxp_module_kv_entry *head = state_head(primary);
    const lxp_module_kv_entry *record = NULL;
    const lxp_module_blob *blob;
    uint8_t key[41];
    size_t i;
    (void)memset(m, 0, sizeof(*m));
    m->origin = origin;
    (void)memcpy(m->root, primary->kernel.current_state_root, 32U);
    CHECK(head != NULL && get_u16(head->value) == 1U &&
          get_u32(head->value + 2U) == 1U, name);
    (void)memcpy(m->manifest_digest, head->value + 6U, 32U);
    blob = find_blob(&primary->kernel, m->manifest_digest);
    CHECK(blob != NULL && blob->length == MANIFEST_BYTES, name);
    (void)memcpy(m->manifest, blob->bytes, MANIFEST_BYTES);
    (void)memcpy(m->value_digest, m->manifest + 22U, 32U);
    m->value_size = get_u32(m->manifest + 54U);
    blob = find_blob(&primary->kernel, m->value_digest);
    CHECK(blob != NULL && blob->length == m->value_size, name);
    head_key(key);
    CHECK(prove(primary, LXP_MODULE_PROGRAMS, key, sizeof(key), m->root,
                &m->head_proof) == 0, name);
    for (i = 0U; i < primary->kernel.module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &primary->kernel.module_kv[i];
        if (entry->module_id == LXP_MODULE_PROGRAMS &&
            entry->key_length == LXP_PROGRAMS_REPLAY_KEY_BYTES &&
            memcmp(entry->key, replay_prefix, sizeof(replay_prefix) - 1U) == 0 &&
            entry->value_length >= 29U + 46U &&
            get_u64(entry->value + 29U + 38U) == origin) {
            CHECK(record == NULL, name);
            record = entry;
        }
    }
    CHECK(record != NULL, name);
    (void)memcpy(m->replay_id, record->value + 29U + 6U, 32U);
    m->replay_record_bytes = record->value_length;
    CHECK(lxp_programs_replay_record_blob_key(
              (lxp_byte_span){record->value, record->value_length},
              m->witness_digest) == LXP_OK, name);
    blob = find_blob(&primary->kernel, m->witness_digest);
    CHECK(blob != NULL && blob->length != 0U, name);
    m->witness_size = (uint32_t)blob->length;
    CHECK(prove(primary, LXP_MODULE_PROGRAMS, record->key, record->key_length,
                m->root, &m->replay_proof) == 0, name);
    return 0;
}

static int case_profile_stage(void)
{
    static const char name[] = "retirement_profile_staged_once";
    static const uint8_t control_key[] = "progretire/v1";
    lxp_programs_retirement_profile profile;
    lxp_module_ctx ctx;
    lxp_arena arena;
    static uint8_t bytes[65536];
    lxp_result status;
    (void)memset(&profile, 0, sizeof(profile));
    profile.version = LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION;
    profile.network_id = NETWORK_ID;
    (void)memcpy(profile.controller, controller_id, 32U);
    (void)memcpy(profile.paxai_program_id, program_id, 32U);
    profile.hot_horizon = 1U;
    CHECK(public_key_for(archive_seed_a, profile.archive_keys[0]) == 0 &&
          public_key_for(archive_seed_b, profile.archive_keys[1]) == 0 &&
          lxp_programs_retirement_profile_digest(&profile, profile_digest) ==
              LXP_OK, name);
    CHECK(seed_open(primary, &ctx, &arena, bytes, sizeof(bytes)) == 0, name);
    status = lxp_programs_retirement_profile_stage(&ctx, &profile);
    if (status != LXP_OK) lxp_module_ctx_rollback(&ctx);
    CHECK(status == LXP_OK, name);
    CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK &&
          lxp_state_root(&primary->kernel,
                         primary->kernel.current_state_root) == LXP_OK, name);
    CHECK(find_kv(&primary->kernel, control_key, sizeof(control_key) - 1U) !=
          NULL, name);
    CHECK(seed_open(primary, &ctx, &arena, bytes, sizeof(bytes)) == 0, name);
    status = lxp_programs_retirement_profile_stage(&ctx, &profile);
    lxp_module_ctx_rollback(&ctx);
    CHECK(status == LXP_ERR_DUPLICATE_ENTRY, name);
    return pass(name);
}

static int case_node_info_capacity(void)
{
    static const char name[] = "node_info_advertises_capacity";
    const uint8_t *p = lni.node_info;
    size_t cursor = 93U, i, count;
    bool found = false;
    CHECK(lni.node_info_length >= 93U && get_u32(p + 6U) == NETWORK_ID &&
          memcmp(p + 59U, sequencer_public, 32U) == 0, name);
    count = get_u16(p + 91U);
    for (i = 0U; i < count; ++i) {
        size_t length;
        CHECK(cursor + 2U <= lni.node_info_length, name);
        length = get_u16(p + cursor);
        cursor += 2U;
        CHECK(cursor + length <= lni.node_info_length, name);
        if (memmem(p + cursor, length, "capacity", 8U) != NULL) found = true;
        cursor += length;
    }
    CHECK(cursor == lni.node_info_length, name);
    CHECK(found, name);
    return pass(name);
}

static int case_keeper_install(void)
{
    static const char name[] = "keeper_installs_capacity_profile";
    static const char *const install[] = {
        "install", "--profile-version", "1", "--profile-digest",
        "5151515151515151515151515151515151515151515151515151515151515151",
        "--floor-blobs", "64", "--floor-bytes", "8388608", "--floor-kv", "16",
        "--max-work-lifetime", "8"
    };
    char output[KEEPER_OUTPUT_BYTES];
    int exit_code = -1;
    CHECK(keeper_run(install, sizeof(install) / sizeof(install[0]), output,
                     &exit_code) == 0 && exit_code == 0, name);
    CHECK(strstr(output, "profile version=1 ") != NULL, name);
    return pass(name);
}

static int case_observe_matches(const char *name)
{
    observation o;
    CHECK(observe(&o) == 0, name);
    CHECK(o.next_sequence == primary->state.next_sequence &&
          memcmp(o.root, primary->kernel.current_state_root, 32U) == 0, name);
    CHECK(o.committed[0] == primary->kernel.blob_count &&
          o.committed[1] == primary->kernel.blob_total_bytes &&
          o.committed[2] == primary->kernel.module_kv_count, name);
    CHECK(o.floor[0] == FLOOR_BLOBS, name);
    return pass(name);
}

static int deploy(const char *path)
{
    static const char name[] = "market_program_deployed";
    FILE *artifact = fopen(path, "rb");
    uint8_t *payload = NULL, *upgrade = NULL;
    long file_length;
    size_t length = 0U;
    lxp_result status = LXP_ERR_IO;
    int failure = 1;
    CHECK(artifact != NULL, name);
    if (fseek(artifact, 0L, SEEK_END) == 0 &&
        (file_length = ftell(artifact)) > 0L &&
        (unsigned long)file_length <= LXP_MAX_ACTIVITY_BYTES - 106U &&
        fseek(artifact, 0L, SEEK_SET) == 0) {
        length = (size_t)file_length;
        payload = (uint8_t *)calloc(1U, length + 104U);
        upgrade = (uint8_t *)calloc(1U, length + 106U);
        if (payload != NULL && upgrade != NULL &&
            fread(payload + 104U, 1U, length, artifact) == length &&
            lxp_hash_sha256(payload + 104U, length, payload + 68U) == LXP_OK) {
            (void)memcpy(payload, program_id, 32U);
            put_u16(payload + 32U, LX_PROGRAMS_ACCOUNT_ABI_VERSION);
            payload[34] = 1U;
            (void)memcpy(payload + 36U, primary->principals[OWNER], 32U);
            put_u32(payload + 100U, (uint32_t)length);
            (void)memcpy(upgrade, program_id, 32U);
            put_u16(upgrade + 32U, LX_PROGRAMS_GUEST_ABI_V5_VERSION);
            (void)memcpy(upgrade + 36U, payload + 68U, 32U);
            (void)memcpy(upgrade + 68U, payload + 68U, 32U);
            put_u32(upgrade + 102U, (uint32_t)length);
            (void)memcpy(upgrade + 106U, payload + 104U, length);
            primary->height = 100U;
            failure = run(primary, OWNER, LX_PROGRAMS_DEPLOY, payload,
                          length + 104U, &status) != 0 ||
                status != LXP_OK || primary->receipt.result_code != LXP_OK ||
                register_rewards(primary) != 0;
        }
    }
    (void)fclose(artifact);
    free(payload);
    if (failure != 0) {
        free(upgrade);
        (void)printf("CONTINUITY_NOTE %s status=%d result=%d\n", name,
                     (int)status, (int)primary->receipt.result_code);
    }
    CHECK(failure == 0, name);
    if (pass(name) != 0 || tally(checkpoint(true, "base_checkpoint")) != 0) {
        free(upgrade);
        return 1;
    }
    (void)pass("base_checkpoint");
    {
        work w = {primary, OWNER, LX_PROGRAMS_UPGRADE, upgrade, length + 106U,
                  101U};
        failure = crash_pair("guest_abi5_upgrade_survives_crash", &w);
    }
    free(upgrade);
    CHECK(failure == 0, "guest_abi5_upgrade_survives_crash");
    CHECK(primary->receipt.result_code == LXP_OK,
          "guest_abi5_upgrade_survives_crash");
    return pass("guest_abi5_upgrade_survives_crash");
}

static int case_archive_temp_crash(void)
{
    static const char name[] = "archive_crash_before_rename_cleaned";
    head_view pre, post;
    recovery rec;
    char final_path[256], temp_path[264];
    uint64_t newest;
    int exit_status = 0;
    const uint64_t sequence = primary->state.next_sequence - 1U;
    CHECK(primary_pause() == 0, name);
    head_take(primary, &pre);
    CHECK(newest_archive(archive_a, &newest, NULL) == 0 && newest < sequence,
          name);
    CHECK(archive_path(final_path, sizeof(final_path), archive_a, sequence) == 0,
          name);
    (void)snprintf(temp_path, sizeof(temp_path), "%s.tmp", final_path);
    (void)fflush(stdout);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_CHECKPOINT_FILE_SYNCED, 1U,
                                      archive_work, NULL, &exit_status) ==
          LXP_OK, name);
    CHECK(access(temp_path, F_OK) == 0 && access(final_path, F_OK) != 0, name);
    CHECK(node_recover(primary, archive_a, 0U, primary_log, &rec) == 0, name);
    head_take(primary, &post);
    CHECK(rec.cleaned == 1U && rec.base == newest &&
          rec.replayed == pre.next_sequence - 1U - newest &&
          rec.matched == rec.replayed && head_equal(&pre, &post), name);
    CHECK(access(temp_path, F_OK) != 0 && access(final_path, F_OK) != 0, name);
    CHECK(primary_resume() == 0, name);
    return pass(name);
}

static int case_archive_rename_crash(void)
{
    static const char name[] = "archive_crash_after_rename_restores";
    head_view pre, post;
    recovery rec;
    char final_path[256];
    int exit_status = 0;
    const uint64_t sequence = primary->state.next_sequence - 1U;
    CHECK(primary_pause() == 0, name);
    head_take(primary, &pre);
    CHECK(archive_path(final_path, sizeof(final_path), archive_a, sequence) == 0,
          name);
    (void)fflush(stdout);
    CHECK(lxp_fault_crash_at_boundary(LXP_FAULT_CHECKPOINT_RENAMED, 1U,
                                      archive_work, NULL, &exit_status) ==
          LXP_OK, name);
    CHECK(access(final_path, F_OK) == 0, name);
    CHECK(node_recover(primary, archive_a, 0U, primary_log, &rec) == 0, name);
    head_take(primary, &post);
    CHECK(rec.cleaned == 0U && rec.base == sequence && rec.replayed == 0U &&
          rec.matched == 0U && head_equal(&pre, &post), name);
    CHECK(primary_resume() == 0, name);
    CHECK(tally(checkpoint(false, name)) == 0, name);
    return pass(name);
}

static int case_prepare_recheck(void)
{
    static const char name[] = "prepare_rechecks_open_reservations";
    static uint8_t bytes[65536];
    observation head, held, after;
    cap_request request;
    reply answer, cancelled;
    lxp_module_ctx ctx;
    lxp_arena arena;
    uint8_t key[32], blob[64];
    lxp_result status = LXP_OK;
    uint64_t id;
    int cancel_failed;
    CHECK(observe(&head) == 0 && head.available[0] != 0U, name);
    (void)memset(&request, 0, sizeof(request));
    request.kind = LXP_CAPACITY_WORK;
    (void)memset(request.activity, 0x41, 32U);
    (void)memset(request.idempotency, 0x41 ^ 0xa5, 32U);
    request.actor = dids[OWNER];
    request.sequence = head.next_sequence;
    (void)memcpy(request.root, head.root, 32U);
    request.lifetime = 4U;
    request.blobs = (uint32_t)head.available[0];
    request.bytes = 1U;
    CHECK(reserve(&request, &answer) == 0 &&
          answer.payload[1U + REC_STATE] == LXP_CAPACITY_RESERVED, name);
    id = get_u64(answer.payload + 1U + REC_ID);
    if (observe(&held) != 0 || held.available[0] != 0U ||
        held.active != head.active + 1U)
        status = LXP_FATAL_INVARIANT;
    (void)memset(blob, 0x6b, sizeof(blob));
    if (status == LXP_OK &&
        (lxp_hash_sha256(blob, sizeof(blob), key) != LXP_OK ||
         seed_open(primary, &ctx, &arena, bytes, sizeof(bytes)) != 0))
        status = LXP_FATAL_INVARIANT;
    if (status == LXP_OK) {
        status = lxp_ctx_blob_put(&ctx, key, blob, sizeof(blob));
        if (status == LXP_OK) status = lxp_module_ctx_prepare_commit(&ctx);
        lxp_module_ctx_rollback(&ctx);
    }
    cancel_failed = cancel(id, &cancelled);
    CHECK(cancel_failed == 0, name);
    CHECK(observe(&after) == 0 && after.available[0] == head.available[0] &&
          after.active == head.active &&
          memcmp(after.root, head.root, 32U) == 0, name);
    (void)printf("CONTINUITY_NOTE %s prepare_status=%d\n", name, (int)status);
    CHECK(status == LXP_ERR_ARENA_EXHAUSTED, name);
    return pass(name);
}

static int case_retirement_registered(void)
{
    static const char name[] = "retirement_activity_registered";
    const uint64_t sequence = primary->state.next_sequence;
    lxp_result status = LXP_OK;
    size_t length;
    CHECK(archive_count != 0U, name);
    length = retirement_encode(&archives[archive_count - 1U], NULL, 1U, 0U);
    CHECK(length == LXP_PROGRAMS_RETIREMENT_HEADER_BYTES, name);
    primary->height = 102U;
    CHECK(run(primary, CONTROLLER, LX_PROGRAMS_STORAGE_RETIREMENT,
              retirement_payload, length, &status) == 0, name);
    (void)printf("CONTINUITY_NOTE %s status=%d result=%d\n", name, (int)status,
                 status == LXP_OK ? (int)primary->receipt.result_code : 0);
    CHECK(status == LXP_OK && primary->receipt.result_code == LXP_ERR_TRUNCATED &&
          primary->state.next_sequence == sequence + 1U, name);
    return pass(name);
}

static size_t create_payload(uint8_t *out)
{
    size_t n = 0U, i;
    (void)memcpy(out + n, primary->principals[OWNER], 32U); n += 32U;
    (void)memcpy(out + n, asset_id, 32U); n += 32U;
    (void)memcpy(out + n, primary->rewards, 32U); n += 32U;
    (void)memset(out + n, 14, 32U); n += 32U;
    out[n++] = 0U;
    put_u64(out + n, 1U); n += 8U;
    out[n++] = 1U;
    out[n++] = 1U;
    for (i = 1U; i <= 7U; ++i) {
        (void)memset(out + n, (int)i, 32U);
        n += 32U;
    }
    out[n++] = 32U;
    out[n++] = 8U;
    put_u16(out + n, 64U); n += 2U;
    put_u32(out + n, 1048576U); n += 4U;
    put_u32(out + n, 1048576U); n += 4U;
    put_u16(out + n, 32U); n += 2U;
    put_u32(out + n, 0U); n += 4U;
    put_u32(out + n, 1000000U); n += 4U;
    put_u128(out + n, 100U); n += 16U;
    put_u128(out + n, 1U); n += 16U;
    out[n++] = 1U;
    out[n++] = 3U;
    out[n++] = 1U;
    (void)memset(out + n, 0, 16U); n += 16U;
    (void)memset(out + n, 16, 32U); n += 32U;
    return n;
}

static int retire_round(bool *crash_pending)
{
    const archive *a = &archives[archive_count - 1U];
    size_t j;
    for (j = 0U; j + 1U < mutation_count; ++j) {
        if (mutations[j].retired || mutations[j].origin + 1U > a->sequence)
            continue;
        if (tally(retire_mutation(*crash_pending ?
                                      "superseded_state_retires_across_crash" :
                                      "superseded_state_retires",
                                  j, a, *crash_pending)) != 0)
            return 1;
        if (*crash_pending) {
            (void)pass("superseded_state_retires_across_crash");
            *crash_pending = false;
        }
    }
    return 0;
}

static int case_mutations(void)
{
    static const char name[] = "authoritative_mutations_exceed_slots";
    uint8_t payload[POLICY_BYTES + 256U];
    unsigned long long namespace_bytes = 0U, replay_bytes = 0U;
    bool applied = false, crash_retire = true;
    size_t i, length;
    recovery rec;
    length = market_envelope(CREATE, OWNER, 1U, payload,
                             create_payload(payload));
    CHECK(app_call("market_created", OWNER, 1000U, envelope_buffer, length,
                   false, &applied) == 0, "market_created");
    if (!applied) return blocked("market_created", "guest-create-not-applied");
    (void)pass("market_created");
    for (i = 0U; i < MUTATIONS; ++i) {
        const uint64_t origin = primary->state.next_sequence;
        const uint64_t sequence = owner_app_sequence + 1U;
        mutation *m = &mutations[mutation_count];
        put_u64(payload, 1U + i);
        (void)memset(payload + 8U, 0xd1, 32U);
        put_u64(payload + 32U, 1U + i);
        length = market_envelope(UPDATE_METADATA, OWNER, sequence, payload, 40U);
        CHECK(app_call(i == 1U ? "app_mutation_survives_crash" : name, OWNER,
                       1010U + i, envelope_buffer, length, i == 1U,
                       &applied) == 0, name);
        CHECK(applied, name);
        if (i == 1U) (void)pass("app_mutation_survives_crash");
        owner_app_sequence = sequence;
        CHECK(capture(name, m, origin) == 0, name);
        if (mutation_count != 0U) {
            const mutation *previous = &mutations[mutation_count - 1U];
            CHECK(memcmp(previous->manifest_digest, m->manifest_digest, 32U) != 0 &&
                  memcmp(previous->value_digest, m->value_digest, 32U) != 0 &&
                  memcmp(previous->replay_id, m->replay_id, 32U) != 0 &&
                  memcmp(previous->witness_digest, m->witness_digest, 32U) != 0,
                  name);
        }
        ++mutation_count;
        namespace_bytes += 38U + MANIFEST_BYTES + m->value_size;
        replay_bytes += (unsigned long long)m->replay_record_bytes +
            m->witness_size;
        if (mutation_count % CHECKPOINT_EVERY == 0U) {
            CHECK(tally(checkpoint(true, "mutation_checkpoint")) == 0, name);
            if (retire_round(&crash_retire) != 0)
                return blocked(name, "retirement-failed");
        }
        if (mutation_count == RESTART_AT) {
            CHECK(tally(primary_restart("restart_mid_mutations", &rec)) == 0,
                  name);
            (void)pass("restart_mid_mutations");
        }
    }
    (void)printf("CONTINUITY_OVERHEAD mutations=%zu retired=%zu "
                 "namespace_bytes=%llu replay_bytes=%llu log_bytes=%llu "
                 "archives=%zu\n",
                 mutation_count, retired_count, namespace_bytes, replay_bytes,
                 (unsigned long long)primary->log.write_offset, archive_count);
    CHECK(mutation_count == MUTATIONS && mutation_count > LXP_KERNEL_MAX_BLOBS,
          name);
    CHECK(retired_count != 0U, name);
    return pass(name);
}

static int claim_run(unsigned actor, uint64_t height, uint8_t *envelope,
                     size_t length)
{
    static const char name[] = "funded_claim_pays_once_after_restart";
    const uint8_t *recipient = envelope + ENVELOPE_PAYLOAD + 32U;
    recovery rec;
    uint64_t recipient_before, rewards_before, amount;
    bool applied = false;
    CHECK(length >= ENVELOPE_PAYLOAD + 80U + 1U &&
          get_u16(envelope + 8U) == CLAIM &&
          get_u32(envelope + 234U) == 80U &&
          get_u64(envelope + ENVELOPE_PAYLOAD + 64U) == 0U, name);
    amount = get_u64(envelope + ENVELOPE_PAYLOAD + 72U);
    CHECK(amount != 0U, name);
    CHECK(tally(primary_restart("restart_before_claim", &rec)) == 0, name);
    recipient_before = balance(primary, recipient);
    rewards_before = balance(primary, primary->rewards);
    CHECK(recipient_before != UINT64_MAX && rewards_before >= amount, name);
    CHECK(app_call(name, actor, height, envelope, length, false, &applied) == 0 &&
          applied, name);
    CHECK(balance(primary, recipient) == recipient_before + amount &&
          balance(primary, primary->rewards) == rewards_before - amount, name);
    put_u64(envelope + 186U, get_u64(envelope + 186U) + 1U);
    envelope[203] ^= 0x5aU;
    CHECK(app_call("duplicate_claim_refused", actor, height + 1U, envelope,
                   length, false, &applied) == 0, name);
    CHECK(balance(primary, recipient) == recipient_before + amount &&
          balance(primary, primary->rewards) == rewards_before - amount, name);
    return pass(name);
}

static int case_funded(const char *script)
{
    static const char name[] = "funded_epoch_settles";
    uint8_t payload[57];
    char *line = NULL;
    size_t capacity = 0U, claims = 0U, lines = 0U;
    ssize_t got;
    FILE *file;
    uint64_t rewards_before = balance(primary, primary->rewards);
    bool applied = false;
    int failure = 0;
    recovery rec;
    put_u128(payload, FUND_AMOUNT);
    (void)memcpy(payload + 16U, primary->account_ids[OWNER], 32U);
    put_u64(payload + 48U, 1U);
    payload[56] = 1U;
    owner_app_sequence += 1U;
    CHECK(app_call("epoch_funded", OWNER, 2000U, envelope_buffer,
                   market_envelope(FUND, OWNER, owner_app_sequence, payload,
                                   sizeof(payload)),
                   false, &applied) == 0, "epoch_funded");
    if (!applied) {
        owner_app_sequence -= 1U;
        return blocked("epoch_funded", "fund-selector-not-applied");
    }
    CHECK(balance(primary, primary->rewards) == rewards_before + FUND_AMOUNT,
          "epoch_funded");
    (void)pass("epoch_funded");
    if (script == NULL) return blocked(name, "settlement-script-absent");
    file = fopen(script, "r");
    CHECK(file != NULL, name);
    while (failure == 0 && (got = getline(&line, &capacity, file)) > 0) {
        unsigned actor = 0U;
        unsigned long long height = 0U;
        int offset = 0;
        bool claim;
        size_t text, length = 0U;
        line[strcspn(line, "\n")] = '\0';
        if (++lines > MAX_SCRIPT_LINES) { failure = 1; break; }
        if (strcmp(line, "restart") == 0) {
            failure = tally(primary_restart("script_restart", &rec));
            continue;
        }
        claim = strncmp(line, "claim ", 6U) == 0;
        if (sscanf(line + (claim ? 6 : 0), "%u %llu %n", &actor, &height,
                   &offset) != 2 || actor >= ACTORS || height == 0U ||
            offset <= 0) {
            failure = 1;
            break;
        }
        text = strlen(line + (claim ? 6 : 0) + offset);
        if (hex_decode(line + (claim ? 6 : 0) + offset, text, envelope_buffer,
                       LXP_MAX_ACTIVITY_BYTES / 2U, &length) != 0) {
            failure = 1;
            break;
        }
        if (claim) {
            ++claims;
            failure = tally(claim_run(actor, (uint64_t)height, envelope_buffer,
                                      length));
        } else {
            failure = app_call(name, actor, (uint64_t)height, envelope_buffer,
                               length, false, &applied) != 0 || !applied;
        }
    }
    free(line);
    (void)fclose(file);
    CHECK(failure == 0 && claims == 1U, name);
    return pass(name);
}

static int case_keeper_after_restart(void)
{
    static const char name[] = "keeper_profile_survives_restart";
    static const char *const observe_args[] = {"observe"};
    char output[KEEPER_OUTPUT_BYTES];
    recovery rec;
    int exit_code = -1;
    CHECK(tally(primary_restart("final_restart", &rec)) == 0, name);
    (void)pass("final_restart");
    CHECK(keeper_run(observe_args, 1U, output, &exit_code) == 0 &&
          exit_code == 0 && strstr(output, "profile_match=1") != NULL, name);
    return pass(name);
}

static int copy_file(const char *from, const char *to)
{
    static uint8_t chunk[1U << 20];
    int in = open(from, O_RDONLY | O_CLOEXEC);
    int out = in < 0 ? -1 :
        open(to, O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, 0600);
    int failure = in < 0 || out < 0;
    while (failure == 0) {
        ssize_t got = read(in, chunk, sizeof(chunk));
        if (got == 0) break;
        if (got < 0) {
            if (errno == EINTR) continue;
            failure = 1;
        } else {
            failure = write_all(out, chunk, (size_t)got);
        }
    }
    if (out >= 0 && (fsync(out) != 0 || close(out) != 0)) failure = 1;
    if (in >= 0) (void)close(in);
    return failure;
}

static int account_proofs_equal(const uint8_t id[32])
{
    static proof_bytes left, right;
    uint8_t key[33], root_primary[32], root_replica[32];
    key[0] = 4U;
    (void)memcpy(key + 1U, id, 32U);
    return lxp_state_root(&primary->kernel, root_primary) != LXP_OK ||
        lxp_state_root(&replica->kernel, root_replica) != LXP_OK ||
        prove(primary, 0U, key, sizeof(key), root_primary, &left) != 0 ||
        prove(replica, 0U, key, sizeof(key), root_replica, &right) != 0 ||
        left.length != right.length ||
        memcmp(left.bytes, right.bytes, left.length) != 0;
}

static int case_replica(void)
{
    static const char name[] = "independent_replica_replays_identically";
    programs_view left, right;
    head_view head_primary, head_replica;
    recovery rec;
    CHECK(archive_count != 0U, name);
    CHECK(primary_pause() == 0, name);
    CHECK(copy_file(primary_log, replica_log) == 0, name);
    CHECK(primary_resume() == 0, name);
    CHECK(node_recover(replica, archive_b, archives[0].sequence, replica_log,
                       &rec) == 0, name);
    CHECK(rec.replayed == primary->state.next_sequence - 1U - archives[0].sequence &&
          rec.matched == rec.replayed, name);
    head_take(primary, &head_primary);
    head_take(replica, &head_replica);
    CHECK(head_equal(&head_primary, &head_replica), name);
    CHECK(view_take(&primary->kernel, &left) == 0 &&
          view_take(&replica->kernel, &right) == 0 &&
          memcmp(&left, &right, sizeof(left)) == 0, name);
    CHECK(account_proofs_equal(primary->account_ids[OWNER]) == 0 &&
          account_proofs_equal(primary->account_ids[OUTSIDER]) == 0 &&
          account_proofs_equal(primary->rewards) == 0, name);
    (void)printf("CONTINUITY_REPLICA base=%llu replayed=%zu\n",
                 (unsigned long long)archives[0].sequence, rec.replayed);
    return pass(name);
}

static int blob_proof(const node *n, const uint8_t key32[32],
                      const uint8_t root[32])
{
    static proof_bytes out;
    uint8_t key[BLOB_KEY_BYTES] = {0U};
    key[0] = 0xffU;
    (void)memcpy(key + BLOB_KEY_BYTES - 32U, key32, 32U);
    return prove(n, LXP_MODULE_PROGRAMS, key, sizeof(key), root, &out);
}

static int case_historical(void)
{
    static const char name[] = "historical_proofs_survive_retirement";
    recovery rec;
    size_t a, i, checked = 0U;
    if (retired_count == 0U) return blocked(name, "nothing-retired");
    for (i = 0U; i < mutation_count; ++i) {
        const mutation *m = &mutations[i];
        if (!m->retired) continue;
        CHECK(find_blob(&primary->kernel, m->manifest_digest) == NULL &&
              find_blob(&primary->kernel, m->value_digest) == NULL &&
              find_blob(&primary->kernel, m->witness_digest) == NULL, name);
        {
            uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
            lxp_programs_replay_record_key(m->replay_id, key);
            CHECK(find_kv(&primary->kernel, key, sizeof(key)) == NULL, name);
        }
        CHECK(lxp_state_proof_decode(m->head_proof.bytes, m->head_proof.length,
                                     &witness) == LXP_OK &&
              lxp_state_proof_verify(&witness, m->root) == LXP_OK, name);
        CHECK(lxp_state_proof_decode(m->replay_proof.bytes,
                                     m->replay_proof.length, &witness) ==
                  LXP_OK &&
              lxp_state_proof_verify(&witness, m->root) == LXP_OK, name);
    }
    for (a = 0U; a < archive_count; ++a) {
        uint8_t root[32];
        bool loaded = false;
        for (i = 0U; i < mutation_count; ++i) {
            const mutation *m = &mutations[i];
            const lxp_module_blob *value;
            uint8_t digest[32];
            if (!m->retired || m->archive != archives[a].sequence) continue;
            if (!loaded) {
                CHECK(node_recover(scratch_node, archive_a,
                                   archives[a].sequence, NULL, &rec) == 0 &&
                      lxp_state_root(&scratch_node->kernel, root) == LXP_OK &&
                      memcmp(root, archives[a].manifest.canonical_state_root,
                             32U) == 0, name);
                loaded = true;
            }
            CHECK(blob_proof(scratch_node, m->manifest_digest, root) == 0 &&
                  blob_proof(scratch_node, m->value_digest, root) == 0 &&
                  blob_proof(scratch_node, m->witness_digest, root) == 0, name);
            value = find_blob(&scratch_node->kernel, m->value_digest);
            CHECK(value != NULL && value->length == m->value_size &&
                  lxp_hash_sha256(value->bytes, value->length, digest) ==
                      LXP_OK &&
                  memcmp(digest, m->value_digest, 32U) == 0, name);
            ++checked;
        }
    }
    CHECK(checked == retired_count, name);
    return pass(name);
}

static int case_peaks(void)
{
    static const char name[] = "peak_capacity_within_unchanged_limits";
    (void)printf("CONTINUITY_PEAKS blobs=%zu/%u bytes=%llu/%llu staged=%zu/%u "
                 "kv=%zu/%u\n",
                 peak.blobs, (unsigned)LXP_KERNEL_MAX_BLOBS,
                 (unsigned long long)peak.bytes,
                 (unsigned long long)LXP_KERNEL_MAX_BLOB_TOTAL_BYTES,
                 peak.staged, (unsigned)LXP_KERNEL_MAX_STAGED_BLOBS, peak.kv,
                 (unsigned)LXP_KERNEL_MAX_MODULE_KV);
    CHECK(peak.blobs <= LXP_KERNEL_MAX_BLOBS &&
          peak.bytes <= LXP_KERNEL_MAX_BLOB_TOTAL_BYTES &&
          peak.staged <= LXP_KERNEL_MAX_STAGED_BLOBS &&
          peak.kv <= LXP_KERNEL_MAX_MODULE_KV, name);
    CHECK(mutation_count > LXP_KERNEL_MAX_BLOBS, name);
    return pass(name);
}

static void remove_tree(const char *directory)
{
    DIR *listing;
    struct dirent *entry;
    if (directory[0] == '\0') return;
    if ((listing = opendir(directory)) != NULL) {
        while ((entry = readdir(listing)) != NULL)
            if (strcmp(entry->d_name, ".") != 0 &&
                strcmp(entry->d_name, "..") != 0)
                remove_in(directory, entry->d_name);
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
    remove_tree(lni.keeper_directory);
    remove_tree(archive_a);
    remove_tree(archive_b);
    if (primary != NULL) node_destroy(primary);
    if (replica != NULL) node_destroy(replica);
    if (scratch_node != NULL) node_destroy(scratch_node);
    if (primary_log[0] != '\0') (void)unlink(primary_log);
    if (replica_log[0] != '\0') (void)unlink(replica_log);
    if (work_directory[0] != '\0') (void)rmdir(work_directory);
    if (lni.owner_ready) {
        (void)pthread_mutex_destroy(&lni.owner.mutex);
        (void)pthread_mutex_destroy(&lni.owner.receipt_mutex);
        (void)pthread_mutex_destroy(&lni.owner.publication_mutex);
        (void)pthread_mutex_destroy(&lni.owner.receipt_authority_mutex);
    }
    free(lni.scratch_bytes);
    free(primary);
    free(replica);
    free(scratch_node);
    free(archive_bytes);
    free(record_buffer);
    free(call_buffer);
    free(envelope_buffer);
}

static int setup(const char *chain_hex, const char *keeper)
{
    static const uint8_t market_domain[] = "PAXAI/market/v1";
    uint8_t preimage[sizeof(market_domain) + 64U];
    size_t length = 0U;
    const size_t archive_capacity = (size_t)LXP_KERNEL_MAX_BLOB_TOTAL_BYTES * 3U +
        (size_t)16U * 1024U * 1024U;
    primary = (node *)calloc(1U, sizeof(node));
    replica = (node *)calloc(1U, sizeof(node));
    scratch_node = (node *)calloc(1U, sizeof(node));
    archive_bytes = (uint8_t *)malloc(archive_capacity);
    record_buffer = (uint8_t *)malloc(LXP_MAX_ACTIVITY_BYTES + RECORD_HEADER_BYTES);
    call_buffer = (uint8_t *)malloc(LXP_MAX_ACTIVITY_BYTES + RECORD_HEADER_BYTES);
    envelope_buffer = (uint8_t *)malloc(LXP_MAX_ACTIVITY_BYTES + RECORD_HEADER_BYTES);
    if (primary == NULL || replica == NULL || scratch_node == NULL ||
        archive_bytes == NULL || record_buffer == NULL || call_buffer == NULL ||
        envelope_buffer == NULL ||
        lxp_arena_init(&archive_arena, archive_bytes, archive_capacity) != LXP_OK ||
        hex_decode(chain_hex, strlen(chain_hex), chain_id, sizeof(chain_id),
                   &length) != 0 || length != 32U ||
        public_key_for(sequencer_seed, sequencer_public) != 0)
        return 1;
    (void)memset(program_id, 0x71, 32U);
    (void)memcpy(preimage, market_domain, sizeof(market_domain) - 1U);
    preimage[sizeof(market_domain) - 1U] = 0U;
    (void)memcpy(preimage + sizeof(market_domain), chain_id, 32U);
    (void)memcpy(preimage + sizeof(market_domain) + 32U, program_id, 32U);
    if (lxp_hash_sha256(preimage, sizeof(preimage), market_id) != LXP_OK)
        return 1;
    (void)strcpy(work_directory, "/tmp/lxp-continuity-XXXXXX");
    if (mkdtemp(work_directory) == NULL ||
        join_path(archive_a, sizeof(archive_a), work_directory, "archive-a") != 0 ||
        join_path(archive_b, sizeof(archive_b), work_directory, "archive-b") != 0 ||
        join_path(primary_log, sizeof(primary_log), work_directory,
                  "primary.log") != 0 ||
        join_path(replica_log, sizeof(replica_log), work_directory,
                  "replica.log") != 0 ||
        mkdir(archive_a, 0700) != 0 || mkdir(archive_b, 0700) != 0 ||
        node_genesis(primary) != 0 ||
        lxp_log_open_or_create(&primary->log, primary_log, LOG_BYTES) != LXP_OK)
        return 1;
    primary->log_open = true;
    (void)memcpy(controller_id, primary->principals[CONTROLLER], 32U);
    return lni_init(keeper);
}

int main(int argc, char **argv)
{
    int status = 0;
    if (argc != 4 && argc != 5) {
        (void)fprintf(stderr,
                      "usage: %s ai_market.wasm chain-hex64 keeper [script]\n",
                      argv[0]);
        return 2;
    }
    if (setup(argv[2], argv[3]) != 0) {
        (void)printf("CONTINUITY_FAIL setup\n");
        cleanup();
        return 1;
    }
    (void)tally(case_profile_stage());
    (void)tally(case_node_info_capacity());
    (void)tally(case_keeper_install());
    (void)tally(case_observe_matches("observation_matches_kernel"));
    if (tally(deploy(argv[1])) == 0) {
        (void)tally(case_archive_temp_crash());
        (void)tally(case_archive_rename_crash());
        (void)tally(case_prepare_recheck());
        (void)tally(case_retirement_registered());
        (void)tally(case_mutations());
        (void)tally(case_funded(argc == 5 ? argv[4] : NULL));
        (void)tally(case_keeper_after_restart());
        (void)tally(case_observe_matches("observation_matches_after_restart"));
        (void)tally(case_replica());
        (void)tally(case_historical());
    }
    (void)tally(case_peaks());
    cleanup();
    (void)printf("PASSED %zu FAILED %zu\n", passed, failed);
    if (failed != 0U) status = 1;
    return status;
}
