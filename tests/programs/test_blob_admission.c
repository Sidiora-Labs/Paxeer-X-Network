#define _GNU_SOURCE

#include "layerx/programs.h"

#include "layerx/lxp_activity.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_handover.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_history.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_module_ctx.h"
#include "layerx/lxp_protocol.h"

#include "lxp_daemon_lni_internal.h"

#include <openssl/evp.h>

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
    NETWORK_ID = 77,
    LNI_MAJOR = 1,
    LNI_MINOR = 8,
    NODE_INFO_REQUEST = 1,
    NODE_INFO_RESPONSE = 2,
    ERROR_RESPONSE = 25,
    OBSERVE = 50,
    RESERVE = 52,
    RECONCILE = 54,
    CANCEL = 56,
    INSTALL = 58,
    ENVELOPE_FIXED_BYTES = 22,
    PROOF_BYTES = 96,
    ID_BODY_BYTES = 10,
    OWNER_SCRATCH_BYTES = 2 * 1024 * 1024,
    KEEPER_OUTPUT_BYTES = 16384,
    FLOOR_BLOBS = 64,
    FLOOR_BYTES = 8388608,
    FLOOR_KV = 16,
    MAX_WORK_LIFETIME = 8
};

enum {
    REC_ID = 0,
    REC_KIND = 8,
    REC_STATE = 9,
    REC_DIGEST = 10,
    REC_BOUND_SEQUENCE = 379,
    REC_BOUND_ROOT = 387,
    REC_EXPIRES = 419,
    REC_SUPERSEDES = 427,
    REC_OUTCOME_SEQUENCE = 435,
    REC_OUTCOME_RESULT = 443,
    REC_OUTCOME_DIGEST = 447
};

static const char payer_did[] = "did:lxp:capacity-payer";
static const char debtor_did[] = "did:lxp:capacity-debtor";
static const char *account_names[] = {
    "agent:did:lxp:capacity-payer:main",
    "agent:did:lxp:capacity-debtor:main", "system:fees"};
static const uint8_t asset_id[32] = {1U};
static const char ledger_name[] = ".layerxd-lni-capacity.ledger";
static const char ledger_temp_name[] = ".layerxd-lni-capacity.tmp";
static const char response_domain[] = "LayerX/storage-capacity-response/v1";
static const char request_domain[] = "LayerX/storage-capacity-request/v1";

typedef struct fixture {
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_identity_store identities;
    lxp_identity *payer;
    lxp_identity *debtor;
    lx_programs_transfer_runtime *runtime;
    lxp_module_ctx ctx;
    lxp_arena seed_arena;
    uint8_t seed_arena_bytes[65536];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_receipt receipt;
    uint64_t parameters;
    uint16_t nonce;
    uint32_t blob_serial;
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
} fixture;

typedef struct reply {
    uint16_t tag;
    uint8_t payload[1U + LXP_CAPACITY_RECORD_BYTES + LXP_CAPACITY_OBSERVATION_BYTES];
    size_t length;
    uint8_t refusal_class;
    lxp_result refusal;
} reply;

typedef struct observation {
    uint16_t profile_version;
    uint64_t next_sequence;
    uint8_t root[32];
    uint64_t committed[3];
    uint64_t floor[3];
    uint64_t obligations[3];
    uint64_t work[3];
    uint64_t available[3];
    uint32_t active;
    uint64_t next_request_id;
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
    uint64_t supersedes;
} cap_request;

typedef struct program_activity {
    lxp_activity activity;
    uint8_t payload[32];
    uint8_t signature[64];
    uint8_t id[32];
} program_activity;

typedef struct racer {
    int descriptor;
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    pthread_barrier_t *barrier;
    reply answer;
    int failed;
} racer;

typedef struct connection {
    int descriptor;
    int server_descriptor;
    pthread_t thread;
    lxp_daemon_lni_server *server;
    lxp_result status;
} connection;

static fixture fx;
static uint8_t payer_seed[32], debtor_seed[32], sequencer_seed[32];
static uint8_t sequencer_public[32];
static uint64_t correlation = 1U;
static size_t passed;

#define CHECK(condition, name) \
    do { \
        if (!(condition)) { \
            (void)fprintf(stderr, "BLOB_ADMISSION_FAIL %s line %d: %s\n", \
                          name, __LINE__, #condition); \
            return 1; \
        } \
    } while (0)

static int pass(const char *name)
{
    ++passed;
    (void)printf("BLOB_ADMISSION_CASE %s\n", name);
    return 0;
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

static int hex_decode(const char *text, size_t length, uint8_t *out)
{
    size_t i;
    for (i = 0U; i < length * 2U; ++i) {
        char c = text[i];
        unsigned int nibble;
        if (c >= '0' && c <= '9') nibble = (unsigned int)(c - '0');
        else if (c >= 'a' && c <= 'f') nibble = (unsigned int)(c - 'a') + 10U;
        else return 1;
        if ((i & 1U) == 0U) out[i / 2U] = (uint8_t)(nibble << 4U);
        else out[i / 2U] = (uint8_t)(out[i / 2U] | nibble);
    }
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
    int failed;
    if (message == NULL) return 1;
    (void)memcpy(message, response_domain, cursor);
    put_u32(message + cursor, NETWORK_ID); cursor += 4U;
    put_u16(message + cursor, tag); cursor += 2U;
    (void)memcpy(message + cursor, payload, length);
    cursor += length;
    failed = lxp_hash_sha256(message, cursor, digest) != LXP_OK ||
        memcmp(proof, sequencer_public, 32U) != 0 ||
        lxp_ed25519_verify_raw(proof, proof + 32U, digest, 32U) != LXP_OK;
    free(message);
    return failed;
}

static int receive_reply(int descriptor, uint64_t correlation_id, reply *out)
{
    uint8_t prefix[4];
    uint8_t *frame;
    uint32_t length;
    uint32_t payload_length;
    uint32_t proof_length;
    int failed = 0;
    (void)memset(out, 0, sizeof(*out));
    if (read_all(descriptor, prefix, 4U) != 0) return 1;
    length = get_u32(prefix);
    if (length < ENVELOPE_FIXED_BYTES || length > 65536U) return 1;
    frame = (uint8_t *)malloc(length);
    if (frame == NULL) return 1;
    if (read_all(descriptor, frame, length) != 0) failed = 1;
    if (failed == 0) {
        payload_length = get_u32(frame + 14U);
        if (get_u16(frame) != LNI_MAJOR || get_u16(frame + 2U) != LNI_MINOR ||
            get_u64(frame + 6U) != correlation_id ||
            payload_length > length - ENVELOPE_FIXED_BYTES ||
            payload_length > sizeof(out->payload))
            failed = 1;
    }
    if (failed == 0) {
        proof_length = get_u32(frame + 18U + payload_length);
        out->tag = get_u16(frame + 4U);
        out->length = payload_length;
        (void)memcpy(out->payload, frame + 18U, payload_length);
        if (18U + payload_length + 4U + proof_length != length) failed = 1;
        else if (out->tag == ERROR_RESPONSE) {
            if (payload_length != 5U || proof_length != 0U) failed = 1;
            else {
                out->refusal_class = out->payload[0];
                out->refusal = (lxp_result)(int32_t)get_u32(out->payload + 1U);
            }
        } else if (proof_length != PROOF_BYTES ||
                   verify_proof(out->tag, out->payload, payload_length,
                                frame + 22U + payload_length) != 0) {
            failed = 1;
        }
    }
    free(frame);
    return failed;
}

static int call_minor(int descriptor, uint16_t minor, uint16_t tag,
                      const uint8_t *body, size_t length, reply *out)
{
    uint64_t id = correlation++;
    if (send_frame(descriptor, minor, tag, id, body, length) != 0) return 1;
    if (receive_reply(descriptor, id, out) != 0) return 1;
    return out->tag == ERROR_RESPONSE || out->tag == tag + 1U ? 0 : 1;
}

static int call(int descriptor, uint16_t tag, const uint8_t *body,
                size_t length, reply *out)
{
    return call_minor(descriptor, LNI_MINOR, tag, body, length, out);
}

static int refused(int descriptor, uint16_t tag, const uint8_t *body,
                   size_t length, uint8_t refusal_class, lxp_result result)
{
    reply answer;
    if (call(descriptor, tag, body, length, &answer) != 0) return 1;
    if (answer.tag != ERROR_RESPONSE || answer.refusal_class != refusal_class ||
        answer.refusal != result) {
        (void)fprintf(stderr,
                      "BLOB_ADMISSION_REFUSAL tag=%u got=%u class=%u result=%d"
                      " expected class=%u result=%d\n",
                      (unsigned)tag, (unsigned)answer.tag,
                      (unsigned)answer.refusal_class, (int)answer.refusal,
                      (unsigned)refusal_class, (int)result);
        return 1;
    }
    return 0;
}

static void *connection_run(void *context)
{
    connection *link = (connection *)context;
    link->status = lxp_daemon_lni_serve_connected(link->server,
                                                  link->server_descriptor);
    (void)close(link->server_descriptor);
    return NULL;
}

static int connection_open(connection *link)
{
    int sockets[2];
    reply hello;
    uint64_t id = 0U;
    (void)memset(link, 0, sizeof(*link));
    if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, sockets) != 0)
        return 1;
    link->descriptor = sockets[0];
    link->server_descriptor = sockets[1];
    link->server = &fx.server;
    if (pthread_create(&link->thread, NULL, connection_run, link) != 0) {
        (void)close(sockets[0]);
        (void)close(sockets[1]);
        return 1;
    }
    if (send_frame(link->descriptor, LNI_MINOR, NODE_INFO_REQUEST, id, NULL,
                   0U) != 0)
        return 1;
    {
        uint8_t prefix[4];
        uint8_t *frame;
        uint32_t length;
        int failed;
        if (read_all(link->descriptor, prefix, 4U) != 0) return 1;
        length = get_u32(prefix);
        if (length < ENVELOPE_FIXED_BYTES || length > 1048576U) return 1;
        frame = (uint8_t *)malloc(length);
        if (frame == NULL) return 1;
        failed = read_all(link->descriptor, frame, length) != 0 ||
            get_u16(frame + 2U) != LNI_MINOR ||
            get_u16(frame + 4U) != NODE_INFO_RESPONSE ||
            get_u64(frame + 6U) != 0U;
        free(frame);
        (void)hello;
        return failed;
    }
}

static int connection_close(connection *link)
{
    (void)shutdown(link->descriptor, SHUT_RDWR);
    (void)close(link->descriptor);
    if (pthread_join(link->thread, NULL) != 0) return 1;
    return link->status == LXP_OK ? 0 : 1;
}

static void observation_decode(const uint8_t *p, observation *o)
{
    size_t axis;
    (void)memset(o, 0, sizeof(*o));
    o->profile_version = get_u16(p + 2U);
    o->next_sequence = get_u64(p + 36U);
    (void)memcpy(o->root, p + 44U, 32U);
    for (axis = 0U; axis < 5U; ++axis) {
        uint64_t *target[5] = {o->committed, o->floor, o->obligations,
                               o->work, o->available};
        const uint8_t *demand = p + 92U + axis * 16U;
        target[axis][0] = get_u32(demand);
        target[axis][1] = get_u64(demand + 4U);
        target[axis][2] = get_u32(demand + 12U);
    }
    o->active = get_u32(p + 172U);
    o->next_request_id = get_u64(p + 176U);
}

static int observe(int descriptor, observation *o, uint8_t *raw)
{
    uint8_t body[2];
    reply answer;
    put_u16(body, 1U);
    if (call(descriptor, OBSERVE, body, sizeof(body), &answer) != 0 ||
        answer.tag != OBSERVE + 1U ||
        answer.length != LXP_CAPACITY_OBSERVATION_BYTES ||
        get_u16(answer.payload) != 1U ||
        get_u32(answer.payload + 76U) != LXP_KERNEL_MAX_BLOBS ||
        get_u64(answer.payload + 80U) != LXP_KERNEL_MAX_BLOB_TOTAL_BYTES ||
        get_u32(answer.payload + 88U) != LXP_KERNEL_MAX_MODULE_KV)
        return 1;
    observation_decode(answer.payload, o);
    if (raw != NULL)
        (void)memcpy(raw, answer.payload, LXP_CAPACITY_OBSERVATION_BYTES);
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
    put_u64(out + cursor, r->supersedes);
}

static int request_digest(const uint8_t body[LXP_CAPACITY_REQUEST_BYTES],
                          uint8_t digest[32])
{
    uint8_t message[sizeof(request_domain) - 1U + LXP_CAPACITY_REQUEST_BYTES];
    (void)memcpy(message, request_domain, sizeof(request_domain) - 1U);
    (void)memcpy(message + sizeof(request_domain) - 1U, body,
                 LXP_CAPACITY_REQUEST_BYTES);
    return lxp_hash_sha256(message, sizeof(message), digest) == LXP_OK ? 0 : 1;
}

static void request_at(cap_request *r, uint8_t kind, uint8_t tag,
                       const char *actor, const observation *head)
{
    (void)memset(r, 0, sizeof(*r));
    r->kind = kind;
    (void)memset(r->activity, tag, 32U);
    (void)memset(r->idempotency, (uint8_t)(tag ^ 0xa5U), 32U);
    r->actor = actor;
    r->sequence = head->next_sequence;
    (void)memcpy(r->root, head->root, 32U);
    r->lifetime = kind == LXP_CAPACITY_WORK ? 4U : 0U;
}

static int reserve(int descriptor, const cap_request *r, reply *answer,
                   bool expect_replayed)
{
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    uint8_t digest[32];
    request_encode(r, body);
    if (request_digest(body, digest) != 0 ||
        call(descriptor, RESERVE, body, sizeof(body), answer) != 0)
        return 1;
    if (answer->tag != RESERVE + 1U) {
        (void)fprintf(stderr, "BLOB_ADMISSION_RESERVE class=%u result=%d\n",
                      (unsigned)answer->refusal_class, (int)answer->refusal);
        return 1;
    }
    return answer->length != 1U + LXP_CAPACITY_RECORD_BYTES ||
        answer->payload[0] != (expect_replayed ? 1U : 0U) ||
        memcmp(answer->payload + 1U + REC_DIGEST, digest, 32U) != 0;
}

static int reserve_refused(int descriptor, const cap_request *r,
                           uint8_t refusal_class, lxp_result result)
{
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    request_encode(r, body);
    return refused(descriptor, RESERVE, body, sizeof(body), refusal_class,
                   result);
}

static int by_id(int descriptor, uint16_t tag, uint64_t id, reply *answer)
{
    uint8_t body[ID_BODY_BYTES];
    put_u16(body, 1U);
    put_u64(body + 2U, id);
    return call(descriptor, tag, body, sizeof(body), answer);
}

static const uint8_t *record_of(const reply *answer)
{
    return answer->tag == RESERVE + 1U ? answer->payload + 1U : answer->payload;
}

static int ledger_path(char *out, size_t capacity, const char *name)
{
    int written = snprintf(out, capacity, "%s/%s", fx.admission_directory,
                           name);
    return written < 0 || (size_t)written >= capacity ? 1 : 0;
}

static int ledger_read(uint8_t **bytes, size_t *length, struct stat *metadata)
{
    char path[256];
    int descriptor;
    int failed;
    *bytes = NULL;
    if (ledger_path(path, sizeof(path), ledger_name) != 0) return 1;
    descriptor = open(path, O_RDONLY | O_CLOEXEC);
    if (descriptor < 0) return 1;
    failed = fstat(descriptor, metadata) != 0 || metadata->st_size <= 0;
    if (failed == 0) {
        *length = (size_t)metadata->st_size;
        *bytes = (uint8_t *)malloc(*length);
        failed = *bytes == NULL || read_all(descriptor, *bytes, *length) != 0;
    }
    (void)close(descriptor);
    return failed;
}

static int ledger_write(const uint8_t *bytes, size_t length)
{
    char path[256];
    int descriptor;
    int failed;
    if (ledger_path(path, sizeof(path), ledger_name) != 0) return 1;
    descriptor = open(path, O_WRONLY | O_TRUNC | O_CLOEXEC);
    if (descriptor < 0) return 1;
    failed = write_all(descriptor, bytes, length) != 0 || fsync(descriptor) != 0;
    return close(descriptor) != 0 || failed;
}

static bool ledger_absent(void)
{
    char path[256];
    struct stat metadata;
    return ledger_path(path, sizeof(path), ledger_name) == 0 &&
        stat(path, &metadata) != 0 && errno == ENOENT;
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

static int lni_serve(lxp_result *result)
{
    lxp_daemon_lni_configuration configuration;
    lxp_result status;
    (void)memset(&configuration, 0, sizeof(configuration));
    configuration.socket_path = fx.socket_path;
    configuration.admission_directory = fx.admission_directory;
    configuration.allowed_peer_uid = (uint32_t)geteuid() + 1U;
    configuration.allowed_peer_gid = (uint32_t)getegid();
    configuration.frame_bytes = LXP_DAEMON_LNI_MAX_FRAME_BYTES;
    configuration.deadline_milliseconds = 10000U;
    configuration.socket_mode = 0660U;
    status = lxp_daemon_lni_serve(&fx.server, &fx.daemon, &fx.owner,
                                  &configuration);
    if (result != NULL) *result = status;
    if (status != LXP_OK) return 1;
    fx.lni_started = true;
    if (pthread_mutex_lock(&fx.server.mutex) != 0) return 1;
    fx.server.allowed_peer_uid = (uint32_t)geteuid();
    fx.server.allowed_peer_gid = (uint32_t)getegid();
    return pthread_mutex_unlock(&fx.server.mutex) == 0 ? 0 : 1;
}

static int lni_stop(void)
{
    lxp_result status;
    if (!fx.lni_started) return 0;
    fx.lni_started = false;
    status = lxp_daemon_lni_stop(&fx.server);
    return status == LXP_OK ? 0 : 1;
}

static int fixture_init(const char *keeper)
{
    static const char *dids[] = {payer_did, debtor_did};
    const uint8_t *seeds[] = {payer_seed, debtor_seed};
    lxp_identity **identities[] = {&fx.payer, &fx.debtor};
    lxp_daemon_configuration daemon_configuration;
    lxp_transfer_asset_state *asset_state;
    uint8_t public_key[32], id[32];
    char key_hex[65];
    size_t i;
    int written;
    (void)memset(&fx, 0, sizeof(fx));
    fx.keeper = keeper;
    fx.parameters = 1U;
    (void)memset(payer_seed, 0x31, 32U);
    (void)memset(debtor_seed, 0x42, 32U);
    (void)memset(sequencer_seed, 0x53, 32U);
    sequencer_seed[31] = 0x07U;
    if (public_key_for(sequencer_seed, sequencer_public) != 0) return 1;
    for (i = 0U; i < 2U; ++i)
        if (public_key_for(seeds[i], public_key) != 0 ||
            lxp_identity_register(&fx.identities, (const uint8_t *)dids[i],
                                  strlen(dids[i]), public_key,
                                  identities[i]) != LXP_OK)
            return 1;
    if (lxp_state_store_init(&fx.store, 1U) != LXP_OK ||
        lxp_kernel_create(&fx.kernel, &fx.store, &fx.journal, &fx.parameters,
                          0U) != LXP_OK ||
        lxp_kernel_register_module(
            &fx.kernel, programs_module_registration_v4_storage_retirement()) !=
            LXP_OK)
        return 1;
    fx.runtime = calloc(1U, sizeof(*fx.runtime));
    asset_state = calloc(1U, sizeof(*asset_state));
    if (fx.runtime == NULL || asset_state == NULL) {
        free(asset_state);
        return 1;
    }
    fx.runtime->accounts = calloc(1U, sizeof(*fx.runtime->accounts));
    (void)memcpy(asset_state->asset_id, asset_id, 32U);
    asset_state->registered = true;
    fx.runtime->assets = asset_state;
    fx.runtime->asset_count = 1U;
    (void)memcpy(fx.runtime->occupancy_asset_id, asset_id, 32U);
    if (fx.runtime->accounts == NULL ||
        lx_account_registry_init(fx.runtime->accounts) != LXP_OK ||
        lxp_state_store_bind_accounts(&fx.store, fx.runtime->accounts) != LXP_OK)
        return 1;
    for (i = 0U; i < sizeof(account_names) / sizeof(account_names[0]); ++i) {
        lx_account *account;
        const bool treasury = i == 2U;
        if (lx_account_id_from_string((const uint8_t *)account_names[i],
                                      strlen(account_names[i]), id) != LXP_OK ||
            lx_account_open(fx.runtime->accounts,
                            (const uint8_t *)account_names[i],
                            strlen(account_names[i]), id, treasury ? 2U : 1U,
                            LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
            lxp_ledger_bootstrap_balance(account, asset_id,
                treasury ? (lxp_u128){0U, 0U} :
                           (lxp_u128){0U, UINT64_C(1000000000)}, 0U) != LXP_OK)
            return 1;
    }
    if (lxp_state_store_require_account_root(&fx.store) != LXP_OK ||
        lxp_kernel_bind_module_runtime(&fx.kernel, LXP_MODULE_PROGRAMS,
                                       fx.runtime) != LXP_OK ||
        lxp_programs_bind_fee_transaction(&fx.kernel) != LXP_OK ||
        lxp_kernel_set_capabilities(&fx.kernel, NULL,
                                    lxp_kernel_canonical_ledger_apply) != LXP_OK ||
        lxp_state_root(&fx.kernel, fx.kernel.current_state_root) != LXP_OK)
        return 1;

    fx.scratch_bytes = (uint8_t *)malloc(OWNER_SCRATCH_BYTES);
    if (fx.scratch_bytes == NULL ||
        lxp_arena_init(&fx.scratch, fx.scratch_bytes, OWNER_SCRATCH_BYTES) !=
            LXP_OK ||
        pthread_mutex_init(&fx.owner.mutex, NULL) != 0 ||
        pthread_mutex_init(&fx.owner.receipt_mutex, NULL) != 0 ||
        pthread_mutex_init(&fx.owner.publication_mutex, NULL) != 0 ||
        pthread_mutex_init(&fx.owner.receipt_authority_mutex, NULL) != 0)
        return 1;
    fx.canonical_log.descriptor = -1;
    fx.history.log = &fx.canonical_log;
    fx.owner.kernel = &fx.kernel;
    fx.owner.identities = &fx.identities;
    fx.owner.programs_runtime = fx.runtime;
    fx.owner.network_id = NETWORK_ID;
    fx.owner.protocol_version = LXP_PROTOCOL_VERSION;
    fx.owner.history = &fx.history;
    fx.owner.receipt_authority = &fx.receipt_authority;
    fx.owner.scratch = &fx.scratch;
    fx.owner.feed_store.baseline_present = true;
    fx.owner.feed_store.baseline_next_sequence = 1U;
    fx.owner.attached = true;
    (void)memcpy(fx.receipt_authority.authorization.public_key,
                 sequencer_public, 32U);
    if (lxp_handover_sequencer_id(sequencer_public,
            fx.receipt_authority.authorization.sequencer_id) != LXP_OK)
        return 1;
    fx.receipt_authority.authorization.first_batch_number = 1U;
    fx.receipt_authority.authorization.last_batch_number = 1024U;
    fx.receipt_authority.authorization.authorized = 1U;
    hex_encode(sequencer_seed, 32U, key_hex);
    if (setenv("LAYERX_NODE_SEQUENCER_PRIVATE_KEY", key_hex, 1) != 0)
        return 1;
    (void)memset(&daemon_configuration, 0, sizeof(daemon_configuration));
    daemon_configuration.role = LXP_DAEMON_SEQUENCER;
    daemon_configuration.network_id = NETWORK_ID;
    daemon_configuration.start_sequence = 1U;
    daemon_configuration.serial_execution = true;
    if (lxp_daemon_start(&fx.daemon, &daemon_configuration, apply_none,
                         &fx.applied) != LXP_OK)
        return 1;
    fx.daemon_started = true;
    (void)strcpy(fx.socket_directory, "/tmp/lxp-capacity-socket-XXXXXX");
    (void)strcpy(fx.admission_directory, "/tmp/lxp-capacity-node-XXXXXX");
    (void)strcpy(fx.keeper_directory, "/tmp/lxp-capacity-keeper-XXXXXX");
    if (mkdtemp(fx.socket_directory) == NULL ||
        mkdtemp(fx.admission_directory) == NULL ||
        mkdtemp(fx.keeper_directory) == NULL ||
        chmod(fx.socket_directory, 0750) != 0 ||
        chmod(fx.admission_directory, 0700) != 0)
        return 1;
    written = snprintf(fx.socket_path, sizeof(fx.socket_path), "%s/lni.sock",
                       fx.socket_directory);
    if (written < 0 || (size_t)written >= sizeof(fx.socket_path)) return 1;
    return lni_serve(NULL);
}

static void remove_in(const char *directory, const char *name)
{
    char path[256];
    int written = snprintf(path, sizeof(path), "%s/%s", directory, name);
    if (written >= 0 && (size_t)written < sizeof(path)) (void)unlink(path);
}

static int fixture_destroy(void)
{
    int failed = lni_stop();
    if (fx.daemon_started && lxp_daemon_shutdown(&fx.daemon) != LXP_OK)
        failed = 1;
    remove_in(fx.socket_directory, "lni.sock");
    remove_in(fx.socket_directory, ".layerxd-lni.lock");
    remove_in(fx.admission_directory, ".layerxd-lni-admission.log");
    remove_in(fx.admission_directory, ".layerxd-lni-admission.tmp");
    remove_in(fx.admission_directory, ledger_name);
    remove_in(fx.admission_directory, ledger_temp_name);
    remove_in(fx.keeper_directory, "capacity-keeper.log");
    (void)rmdir(fx.socket_directory);
    (void)rmdir(fx.admission_directory);
    (void)rmdir(fx.keeper_directory);
    while (fx.kernel.blob_count != 0U)
        free(fx.kernel.blobs[--fx.kernel.blob_count].bytes);
    if (fx.runtime != NULL) {
        free((void *)fx.runtime->assets);
        lx_account_registry_release(fx.runtime->accounts);
        free(fx.runtime->accounts);
        free(fx.runtime);
    }
    free(fx.scratch_bytes);
    (void)lxp_state_store_destroy(&fx.store);
    return failed;
}

static int seed_open(void)
{
    return lxp_arena_init(&fx.seed_arena, fx.seed_arena_bytes,
                          sizeof(fx.seed_arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&fx.ctx, &fx.kernel, LXP_MODULE_PROGRAMS, 10U,
                            fx.kernel.epoch, fx.store.next_sequence,
                            UINT64_C(100000000), &fx.seed_arena,
                            true) != LXP_OK;
}

static int seed_close(lxp_result status)
{
    if (status != LXP_OK) {
        lxp_module_ctx_rollback(&fx.ctx);
        return 1;
    }
    return lxp_module_ctx_commit(&fx.ctx) != LXP_OK ||
        lxp_state_root(&fx.kernel, fx.kernel.current_state_root) != LXP_OK;
}

static lxp_result blob_stage(uint8_t key[32])
{
    uint8_t bytes[8];
    lxp_result status;
    (void)memcpy(bytes, "capblob", 4U);
    put_u32(bytes + 4U, ++fx.blob_serial);
    status = lxp_hash_sha256(bytes, sizeof(bytes), key);
    return status == LXP_OK ? lxp_ctx_blob_put(&fx.ctx, key, bytes,
                                               sizeof(bytes)) : status;
}

static int blobs_commit_until(size_t target)
{
    uint8_t key[32];
    while (fx.kernel.blob_count < target) {
        size_t batch = target - fx.kernel.blob_count;
        lxp_result status = LXP_OK;
        size_t i;
        if (batch > LXP_KERNEL_MAX_STAGED_BLOBS)
            batch = LXP_KERNEL_MAX_STAGED_BLOBS;
        if (seed_open() != 0) return 1;
        for (i = 0U; i < batch && status == LXP_OK; ++i)
            status = blob_stage(key);
        if (seed_close(status) != 0) return 1;
    }
    return fx.kernel.blob_count == target ? 0 : 1;
}

static int activity_prepare(program_activity *t, lxp_identity *identity,
                            const char *did, const uint8_t seed[32],
                            uint8_t program_fill)
{
    uint8_t digest[32];
    uint8_t encoded_bytes[2U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_byte_span encoded;
    (void)memset(t, 0, sizeof(*t));
    (void)memset(t->payload, program_fill, sizeof(t->payload));
    t->activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    t->activity.network_id = 1U;
    t->activity.activity_type = LX_PROGRAMS_REGISTRY;
    t->activity.actor_did = (lxp_byte_span){(const uint8_t *)did, strlen(did)};
    t->activity.authority = (lxp_byte_span){identity->primary_key, 32U};
    t->activity.account_sequence = identity->next_sequence;
    t->activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    ++fx.nonce;
    t->activity.idempotency_key[0] = 0xcaU;
    t->activity.idempotency_key[30] = (uint8_t)(fx.nonce >> 8U);
    t->activity.idempotency_key[31] = (uint8_t)fx.nonce;
    t->activity.fee_limit = (lxp_u128){0U, 100000000U};
    t->activity.payload = (lxp_byte_span){t->payload, sizeof(t->payload)};
    t->activity.signature = (lxp_byte_span){t->signature, 64U};
    return lxp_hash_payload(t->payload, sizeof(t->payload),
                            t->activity.payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(&t->activity, digest) != LXP_OK ||
        sign_raw(seed, digest, sizeof(digest), t->signature) != 0 ||
        lxp_arena_init(&arena, encoded_bytes, sizeof(encoded_bytes)) != LXP_OK ||
        lxp_activity_encode(&t->activity, &arena, &encoded) != LXP_OK ||
        lxp_activity_id(encoded.bytes, encoded.length, t->id) != LXP_OK;
}

static lxp_result activity_execute(program_activity *t, lxp_identity *identity)
{
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_kernel_execution execution;
    lxp_fee_params fees;
    lxp_arena arena;
    lxp_result status;
    if (lxp_activity_verify_signature(&t->activity) != LXP_OK ||
        lxp_authority_resolve_activity(&fx.kernel, identity, &t->activity,
                                       true, true, 10U, 100U,
                                       fx.store.next_sequence, &grant,
                                       &authority) != LXP_OK ||
        lxp_arena_init(&arena, fx.arena_bytes, sizeof(fx.arena_bytes)) !=
            LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memset(&fees, 0, sizeof(fees));
    fees.version = 1U;
    fees.base_fee = (lxp_u128){0U, 1U};
    fees.multiplier_basis_points = 10000U;
    (void)memset(&execution, 0, sizeof(execution));
    execution.network_id = 1U;
    execution.batch_number = 1U;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.epoch = fx.kernel.epoch;
    execution.global_sequence = fx.store.next_sequence;
    execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &fx.identities;
    execution.authority = &authority;
    execution.fee_parameters = &fees;
    execution.fee_balance = (lxp_u128){0U, UINT64_C(1000000000)};
    execution.gas_limit = UINT64_C(10000000);
    execution.arena = &arena;
    execution.sequencer_private_key = sequencer_seed;
    (void)memset(&fx.receipt, 0, sizeof(fx.receipt));
    status = lxp_kernel_execute_activity(&fx.kernel, &t->activity, &execution,
                                         &fx.receipt);
    if (status != LXP_OK) return status;
    if (memcmp(fx.receipt.activity_id, t->id, 32U) != 0)
        return LXP_FATAL_INVARIANT;
    return fx.receipt.result_code;
}

static int keeper_run(const char *const *extra, size_t extra_count,
                      char *output, int *exit_code)
{
    char network[16];
    char sequencer[65];
    char *argv[40];
    char *environment[] = {NULL};
    posix_spawn_file_actions_t actions;
    size_t argc = 0U;
    size_t length = 0U;
    size_t i;
    pid_t child;
    int pipe_descriptors[2];
    int status;
    if (extra_count + 10U > sizeof(argv) / sizeof(argv[0])) return 1;
    (void)snprintf(network, sizeof(network), "%u", (unsigned)NETWORK_ID);
    hex_encode(sequencer_public, 32U, sequencer);
    argv[argc++] = (char *)fx.keeper;
    argv[argc++] = (char *)extra[0];
    argv[argc++] = (char *)"--state";
    argv[argc++] = fx.keeper_directory;
    argv[argc++] = (char *)"--network-id";
    argv[argc++] = network;
    argv[argc++] = (char *)"--sequencer";
    argv[argc++] = sequencer;
    argv[argc++] = (char *)"--socket";
    argv[argc++] = fx.socket_path;
    for (i = 1U; i < extra_count; ++i) argv[argc++] = (char *)extra[i];
    argv[argc] = NULL;
    if (pipe2(pipe_descriptors, O_CLOEXEC) != 0) return 1;
    if (posix_spawn_file_actions_init(&actions) != 0 ||
        posix_spawn_file_actions_adddup2(&actions, pipe_descriptors[1], 1) != 0 ||
        posix_spawn(&child, fx.keeper, &actions, NULL, argv, environment) != 0) {
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

static int case_disabled_and_malformed(int descriptor)
{
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    uint8_t profile[LXP_CAPACITY_PROFILE_BYTES];
    put_u16(body, 1U);
    CHECK(refused(descriptor, OBSERVE, body, 2U, 4U,
                  LXP_ERR_MODULE_DISABLED) == 0, "observe_before_install");
    CHECK(refused(descriptor, OBSERVE, body, 3U, 1U,
                  LXP_ERR_MALFORMED_ENVELOPE) == 0, "observe_wrong_length");
    put_u16(body, 2U);
    CHECK(refused(descriptor, OBSERVE, body, 2U, 1U,
                  LXP_ERR_MALFORMED_ENVELOPE) == 0, "observe_wrong_format");
    put_u16(body, 1U);
    {
        reply answer;
        CHECK(call_minor(descriptor, 7U, OBSERVE, body, 2U, &answer) == 0 &&
              answer.tag == ERROR_RESPONSE && answer.refusal_class == 3U &&
              answer.refusal == LXP_ERR_VERSION_UNSUPPORTED,
              "observe_old_minor");
    }
    (void)memset(profile, 0, sizeof(profile));
    put_u16(profile, 1U);
    put_u16(profile + 2U, 1U);
    CHECK(refused(descriptor, INSTALL, profile, sizeof(profile), 1U,
                  LXP_ERR_NON_CANONICAL) == 0, "install_zero_profile");
    CHECK(ledger_absent(), "refusals_never_persist");
    return pass("disabled_and_malformed_refusals");
}

static void profile_encode(uint8_t out[LXP_CAPACITY_PROFILE_BYTES],
                           uint16_t version, uint8_t digest_fill,
                           uint32_t floor_blobs)
{
    put_u16(out, 1U);
    put_u16(out + 2U, version);
    (void)memset(out + 4U, digest_fill, 32U);
    put_u32(out + 36U, floor_blobs);
    put_u64(out + 40U, FLOOR_BYTES);
    put_u32(out + 48U, FLOOR_KV);
    put_u64(out + 52U, MAX_WORK_LIFETIME);
}

static int case_install(int descriptor)
{
    static const char *const install[] = {
        "install", "--profile-version", "1", "--profile-digest",
        "5151515151515151515151515151515151515151515151515151515151515151",
        "--floor-blobs", "64", "--floor-bytes", "8388608", "--floor-kv", "16",
        "--max-work-lifetime", "8"};
    char output[KEEPER_OUTPUT_BYTES];
    uint8_t profile[LXP_CAPACITY_PROFILE_BYTES];
    uint8_t *before = NULL;
    uint8_t *after = NULL;
    size_t before_length = 0U;
    size_t after_length = 0U;
    struct stat before_metadata;
    struct stat after_metadata;
    reply answer;
    int exit_code = -1;
    int same;
    CHECK(keeper_run(install, sizeof(install) / sizeof(install[0]), output,
                     &exit_code) == 0 && exit_code == 0 &&
          strstr(output, "profile version=1 ") != NULL, "keeper_install");
    CHECK(ledger_read(&before, &before_length, &before_metadata) == 0 &&
          (before_metadata.st_mode & 0777U) == 0600U, "ledger_persisted");
    profile_encode(profile, 1U, 0x51U, FLOOR_BLOBS);
    CHECK(call(descriptor, INSTALL, profile, sizeof(profile), &answer) == 0 &&
          answer.tag == INSTALL + 1U && answer.length == sizeof(profile) &&
          memcmp(answer.payload, profile, sizeof(profile)) == 0,
          "install_same_profile_idempotent");
    CHECK(ledger_read(&after, &after_length, &after_metadata) == 0,
          "ledger_reread");
    same = before_length == after_length &&
        memcmp(before, after, before_length) == 0 &&
        before_metadata.st_ino == after_metadata.st_ino;
    free(before);
    free(after);
    CHECK(same, "idempotent_install_does_not_rewrite");
    profile_encode(profile, 1U, 0x52U, FLOOR_BLOBS);
    CHECK(refused(descriptor, INSTALL, profile, sizeof(profile), 4U,
                  LXP_ERR_CONTEXT_MISMATCH) == 0, "install_conflict_refused");
    profile_encode(profile, 1U, 0x51U, LXP_KERNEL_MAX_BLOBS + 1U);
    CHECK(refused(descriptor, INSTALL, profile, sizeof(profile), 4U,
                  LXP_ERR_PARAMETER_BOUNDS) == 0, "install_floor_bound");
    return pass("profile_install_through_keeper");
}

static int case_reads_hold_nothing(int descriptor)
{
    uint8_t first_raw[LXP_CAPACITY_OBSERVATION_BYTES];
    uint8_t second_raw[LXP_CAPACITY_OBSERVATION_BYTES];
    uint8_t *before = NULL;
    uint8_t *after = NULL;
    size_t before_length = 0U;
    size_t after_length = 0U;
    struct stat before_metadata;
    struct stat after_metadata;
    observation first;
    observation second;
    int same;
    CHECK(ledger_read(&before, &before_length, &before_metadata) == 0,
          "ledger_before_reads");
    CHECK(observe(descriptor, &first, first_raw) == 0 &&
          observe(descriptor, &second, second_raw) == 0, "signed_observations");
    CHECK(ledger_read(&after, &after_length, &after_metadata) == 0,
          "ledger_after_reads");
    same = before_length == after_length &&
        memcmp(before, after, before_length) == 0 &&
        before_metadata.st_ino == after_metadata.st_ino &&
        before_metadata.st_mtim.tv_nsec == after_metadata.st_mtim.tv_nsec &&
        before_metadata.st_mtim.tv_sec == after_metadata.st_mtim.tv_sec;
    free(before);
    free(after);
    CHECK(same, "reads_never_write_the_ledger");
    CHECK(memcmp(first_raw, second_raw, sizeof(first_raw)) == 0,
          "reads_never_change_headroom");
    CHECK(first.profile_version == 1U && first.next_request_id == 1U &&
          first.active == 0U && first.next_sequence == fx.store.next_sequence &&
          memcmp(first.root, fx.kernel.current_state_root, 32U) == 0,
          "observation_bound_to_head");
    CHECK(first.committed[0] == fx.kernel.blob_count &&
          first.committed[2] == fx.kernel.module_kv_count &&
          first.floor[0] == FLOOR_BLOBS && first.floor[1] == FLOOR_BYTES &&
          first.floor[2] == FLOOR_KV &&
          first.available[0] == LXP_KERNEL_MAX_BLOBS - fx.kernel.blob_count -
              FLOOR_BLOBS &&
          first.available[2] == LXP_KERNEL_MAX_MODULE_KV -
              fx.kernel.module_kv_count - FLOOR_KV, "observation_headroom");
    return pass("reads_are_not_reservations");
}

static uint8_t work_body[LXP_CAPACITY_REQUEST_BYTES];
static uint64_t work_id;

static int case_work_reservation(int descriptor)
{
    observation head;
    observation held;
    cap_request request;
    cap_request other;
    reply answer;
    reply replay;
    CHECK(observe(descriptor, &head, NULL) == 0, "head");
    request_at(&request, LXP_CAPACITY_WORK, 0x11U, payer_did, &head);
    request.blobs = 1U;
    request.bytes = 64U;
    request.kv = 1U;
    CHECK(reserve(descriptor, &request, &answer, false) == 0, "reserve_work");
    work_id = get_u64(record_of(&answer) + REC_ID);
    request_encode(&request, work_body);
    CHECK(work_id == 1U && record_of(&answer)[REC_STATE] == LXP_CAPACITY_RESERVED &&
          get_u64(record_of(&answer) + REC_BOUND_SEQUENCE) == head.next_sequence &&
          memcmp(record_of(&answer) + REC_BOUND_ROOT, head.root, 32U) == 0 &&
          get_u64(record_of(&answer) + REC_EXPIRES) == head.next_sequence + 4U,
          "work_record_root_bound");
    CHECK(reserve(descriptor, &request, &replay, true) == 0 &&
          memcmp(record_of(&replay), record_of(&answer),
                 LXP_CAPACITY_RECORD_BYTES) == 0, "exact_replay_same_identity");
    CHECK(observe(descriptor, &held, NULL) == 0 && held.active == 1U &&
          held.next_request_id == 2U && held.work[0] == 1U &&
          held.work[1] == 64U && held.work[2] == 1U &&
          held.available[0] == head.available[0] - 1U, "work_counted");
    other = request;
    other.idempotency[0] ^= 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_DUPLICATE_ENTRY) == 0,
          "same_activity_refused");
    other = request;
    other.activity[0] ^= 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_DUPLICATE_ENTRY) == 0,
          "same_idempotency_refused");
    other = request;
    other.activity[0] ^= 2U;
    other.idempotency[0] ^= 2U;
    other.root[5] ^= 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_PROJECTION_STALE) == 0,
          "root_mismatch_refused");
    other.root[5] ^= 1U;
    other.sequence += 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_PROJECTION_STALE) == 0,
          "sequence_mismatch_refused");
    other.sequence -= 1U;
    other.lifetime = MAX_WORK_LIFETIME + 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_PARAMETER_BOUNDS) == 0,
          "lifetime_bound_refused");
    other.lifetime = 4U;
    other.blobs = LXP_KERNEL_MAX_BLOBS + 1U;
    CHECK(reserve_refused(descriptor, &other, 4U, LXP_ERR_ARENA_EXHAUSTED) == 0,
          "demand_beyond_limit_refused");
    other.blobs = 0U;
    other.bytes = 0U;
    other.kv = 0U;
    CHECK(reserve_refused(descriptor, &other, 1U, LXP_ERR_NON_CANONICAL) == 0,
          "zero_demand_refused");
    other.kv = 1U;
    other.supersedes = 1U;
    CHECK(reserve_refused(descriptor, &other, 1U, LXP_ERR_NON_CANONICAL) == 0,
          "work_cannot_supersede");
    CHECK(observe(descriptor, &held, NULL) == 0 && held.next_request_id == 2U &&
          held.active == 1U, "refusals_hold_nothing");
    return pass("work_reservation_serialized");
}

static int case_stale_after_commit(int descriptor)
{
    observation before;
    observation after;
    cap_request request;
    reply answer;
    CHECK(observe(descriptor, &before, NULL) == 0, "head");
    CHECK(blobs_commit_until(fx.kernel.blob_count + 1U) == 0, "seed_commit");
    CHECK(observe(descriptor, &after, NULL) == 0 &&
          after.next_sequence == before.next_sequence &&
          memcmp(after.root, before.root, 32U) != 0 &&
          after.committed[0] == before.committed[0] + 1U, "root_moved");
    request_at(&request, LXP_CAPACITY_WORK, 0x12U, payer_did, &before);
    request.kv = 1U;
    CHECK(reserve_refused(descriptor, &request, 4U,
                          LXP_ERR_PROJECTION_STALE) == 0, "old_root_refused");
    CHECK(call(descriptor, RESERVE, work_body, sizeof(work_body), &answer) == 0 &&
          answer.tag == RESERVE + 1U && answer.payload[0] == 1U &&
          get_u64(answer.payload + 1U + REC_ID) == work_id,
          "replay_survives_head_move");
    return pass("root_binding_exact");
}

static void *racer_run(void *context)
{
    racer *r = (racer *)context;
    (void)pthread_barrier_wait(r->barrier);
    r->failed = call(r->descriptor, RESERVE, r->body, sizeof(r->body),
                     &r->answer);
    return NULL;
}

static int case_race(void)
{
    connection links[2];
    racer racers[2];
    pthread_t threads[2];
    pthread_barrier_t barrier;
    observation head;
    observation after;
    cap_request request;
    reply cancelled;
    size_t winners = 0U;
    size_t losers = 0U;
    size_t winner = 0U;
    size_t i;
    CHECK(connection_open(&links[0]) == 0 && connection_open(&links[1]) == 0,
          "two_writers");
    CHECK(observe(links[0].descriptor, &head, NULL) == 0 &&
          head.available[0] > 1U, "head");
    CHECK(pthread_barrier_init(&barrier, NULL, 2U) == 0, "barrier");
    for (i = 0U; i < 2U; ++i) {
        request_at(&request, LXP_CAPACITY_WORK, (uint8_t)(0x21U + i),
                   payer_did, &head);
        request.blobs = (uint32_t)head.available[0];
        request.bytes = 1U;
        request_encode(&request, racers[i].body);
        racers[i].descriptor = links[i].descriptor;
        racers[i].barrier = &barrier;
        racers[i].failed = 1;
    }
    CHECK(pthread_create(&threads[0], NULL, racer_run, &racers[0]) == 0 &&
          pthread_create(&threads[1], NULL, racer_run, &racers[1]) == 0,
          "racers_started");
    CHECK(pthread_join(threads[0], NULL) == 0 &&
          pthread_join(threads[1], NULL) == 0, "racers_joined");
    (void)pthread_barrier_destroy(&barrier);
    for (i = 0U; i < 2U; ++i) {
        CHECK(racers[i].failed == 0, "racer_reply");
        if (racers[i].answer.tag == RESERVE + 1U) {
            ++winners;
            winner = i;
        } else if (racers[i].answer.tag == ERROR_RESPONSE &&
                   racers[i].answer.refusal_class == 4U &&
                   racers[i].answer.refusal == LXP_ERR_ARENA_EXHAUSTED) {
            ++losers;
        }
    }
    CHECK(winners == 1U && losers == 1U, "exactly_one_writer_wins");
    CHECK(get_u64(racers[winner].answer.payload + 1U + REC_ID) ==
          head.next_request_id, "winner_takes_next_identity");
    CHECK(observe(links[1].descriptor, &after, NULL) == 0 &&
          after.available[0] == 0U &&
          after.next_request_id == head.next_request_id + 1U,
          "headroom_consumed_once");
    CHECK(by_id(links[1].descriptor, CANCEL, head.next_request_id,
                &cancelled) == 0 && cancelled.tag == CANCEL + 1U &&
          cancelled.payload[REC_STATE] == LXP_CAPACITY_CANCELLED,
          "winner_cancelled");
    CHECK(by_id(links[0].descriptor, CANCEL, head.next_request_id,
                &cancelled) == 0 && cancelled.tag == CANCEL + 1U &&
          cancelled.payload[REC_STATE] == LXP_CAPACITY_CANCELLED,
          "cancel_idempotent");
    CHECK(observe(links[0].descriptor, &after, NULL) == 0 &&
          after.available[0] == head.available[0], "cancel_releases");
    CHECK(connection_close(&links[0]) == 0 && connection_close(&links[1]) == 0,
          "writers_closed");
    return pass("two_writers_serialized");
}

static int case_restart(void)
{
    connection link;
    observation before;
    observation after;
    lxp_capacity_ledger *ledger;
    uint8_t *bytes = NULL;
    size_t length = 0U;
    struct stat metadata;
    char path[256];
    reply answer;
    lxp_result status = LXP_OK;
    int descriptor;
    int decoded;
    CHECK(connection_open(&link) == 0 && observe(link.descriptor, &before,
                                                 NULL) == 0, "head");
    CHECK(connection_close(&link) == 0, "closed");
    CHECK(lni_stop() == 0, "node_stopped");
    CHECK(ledger_path(path, sizeof(path), ledger_temp_name) == 0, "temp_path");
    descriptor = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0600);
    CHECK(descriptor >= 0 && write_all(descriptor, (const uint8_t *)"torn", 4U) ==
          0 && close(descriptor) == 0, "torn_temp_left");
    CHECK(lni_serve(NULL) == 0, "node_restarted");
    CHECK(connection_open(&link) == 0 && observe(link.descriptor, &after,
                                                 NULL) == 0, "head_after");
    CHECK(after.next_request_id == before.next_request_id &&
          after.active == before.active &&
          memcmp(after.work, before.work, sizeof(after.work)) == 0,
          "ledger_recovered");
    CHECK(call(link.descriptor, RESERVE, work_body, sizeof(work_body),
               &answer) == 0 && answer.tag == RESERVE + 1U &&
          answer.payload[0] == 1U &&
          get_u64(answer.payload + 1U + REC_ID) == work_id,
          "identity_survives_restart");
    CHECK(by_id(link.descriptor, CANCEL, work_id, &answer) == 0 &&
          answer.tag == CANCEL + 1U &&
          answer.payload[REC_STATE] == LXP_CAPACITY_CANCELLED, "work_cancelled");
    CHECK(access(path, F_OK) != 0 && errno == ENOENT, "temp_consumed");
    CHECK(connection_close(&link) == 0, "closed_again");
    CHECK(ledger_read(&bytes, &length, &metadata) == 0, "ledger_read");
    ledger = (lxp_capacity_ledger *)calloc(1U, sizeof(*ledger));
    decoded = ledger != NULL &&
        lxp_capacity_ledger_decode(bytes, length, ledger) == LXP_OK &&
        ledger->next_request_id == before.next_request_id &&
        ledger->count == 2U && ledger->entries[0].request_id == work_id &&
        ledger->entries[0].state == LXP_CAPACITY_CANCELLED;
    free(ledger);
    if (!decoded) free(bytes);
    CHECK(decoded, "ledger_contents");
    CHECK(lni_stop() == 0, "node_stopped_again");
    bytes[length / 2U] ^= 0x01U;
    CHECK(ledger_write(bytes, length) == 0, "ledger_corrupted");
    CHECK(lni_serve(&status) != 0 && status == LXP_ERR_LOG_CORRUPT,
          "corrupt_ledger_fails_closed");
    bytes[length / 2U] ^= 0x01U;
    CHECK(ledger_write(bytes, length) == 0, "ledger_restored");
    free(bytes);
    CHECK(lni_serve(NULL) == 0, "node_restarted_clean");
    return pass("durable_identity_across_restart");
}

static int case_full_store(int descriptor)
{
    observation head;
    cap_request request;
    reply answer;
    uint8_t key[32];
    size_t committed_blobs;
    lxp_result status;
    CHECK(blobs_commit_until(LXP_KERNEL_MAX_BLOBS - FLOOR_BLOBS) == 0,
          "fill_to_floor");
    CHECK(observe(descriptor, &head, NULL) == 0 && head.available[0] == 0U,
          "no_work_headroom");
    request_at(&request, LXP_CAPACITY_WORK, 0x31U, payer_did, &head);
    request.blobs = 1U;
    request.bytes = 8U;
    CHECK(reserve_refused(descriptor, &request, 4U,
                          LXP_ERR_ARENA_EXHAUSTED) == 0, "work_refused_at_floor");
    request_at(&request, LXP_CAPACITY_OBLIGATION, 0x32U, debtor_did, &head);
    request.blobs = 1U;
    request.bytes = 8U;
    CHECK(reserve(descriptor, &request, &answer, false) == 0 &&
          record_of(&answer)[REC_KIND] == LXP_CAPACITY_OBLIGATION,
          "obligation_draws_on_floor");
    CHECK(blobs_commit_until(LXP_KERNEL_MAX_BLOBS) == 0, "fill_to_limit");
    CHECK(observe(descriptor, &head, NULL) == 0 && head.available[0] == 0U &&
          head.committed[0] == LXP_KERNEL_MAX_BLOBS, "full_live_store");
    request_at(&request, LXP_CAPACITY_OBLIGATION, 0x33U, debtor_did, &head);
    request.blobs = 1U;
    request.bytes = 8U;
    CHECK(reserve_refused(descriptor, &request, 4U,
                          LXP_ERR_ARENA_EXHAUSTED) == 0,
          "full_live_store_refuses_obligation");
    committed_blobs = fx.kernel.blob_count;
    CHECK(seed_open() == 0, "deletion_context");
    CHECK(lxp_ctx_blob_del(&fx.ctx, fx.kernel.blobs[0].key) == LXP_OK,
          "staged_deletion");
    {
        observation during;
        int observed = observe(descriptor, &during, NULL);
        status = blob_stage(key);
        if (observed != 0 || during.available[0] != 0U ||
            during.committed[0] != LXP_KERNEL_MAX_BLOBS || status != LXP_OK) {
            lxp_module_ctx_rollback(&fx.ctx);
            CHECK(false, "staged_deletion_not_headroom");
        }
        request_at(&request, LXP_CAPACITY_WORK, 0x34U, payer_did, &during);
        request.blobs = 1U;
        request.bytes = 8U;
        if (reserve_refused(descriptor, &request, 4U,
                            LXP_ERR_ARENA_EXHAUSTED) != 0) {
            lxp_module_ctx_rollback(&fx.ctx);
            CHECK(false, "staged_deletion_not_reservable");
        }
    }
    status = lxp_module_ctx_prepare_commit(&fx.ctx);
    lxp_module_ctx_rollback(&fx.ctx);
    CHECK(status == LXP_ERR_ARENA_EXHAUSTED, "native_prepare_refuses_addition");
    CHECK(fx.kernel.blob_count == committed_blobs, "nothing_deleted");
    return pass("full_live_store_refused");
}

static int canonical_digest(const program_activity *t, const char *did, uint8_t out[32])
{
    const uint8_t *bytes = NULL;
    size_t length = 0U;
    return lxp_idempotency_canonical_lookup(&fx.store, (const uint8_t *)did,
                                            strlen(did),
                                            t->activity.idempotency_key,
                                            &bytes, &length) !=
            LXP_ERR_IDEMPOTENT_REPLAY ||
        lxp_hash_sha256(bytes, length, out) != LXP_OK;
}

static int case_obligations(int descriptor)
{
    static program_activity failing;
    static program_activity paying;
    observation head;
    observation after;
    cap_request first;
    cap_request successor;
    cap_request duplicate;
    reply answer;
    uint8_t digest[32];
    uint64_t first_id;
    uint64_t second_id;
    lxp_result result;
    CHECK(activity_prepare(&failing, fx.debtor, debtor_did, debtor_seed, 0U) ==
          0, "failing_obligation_built");
    CHECK(observe(descriptor, &head, NULL) == 0, "head");
    request_at(&first, LXP_CAPACITY_OBLIGATION, 0U, debtor_did, &head);
    (void)memcpy(first.activity, failing.id, 32U);
    (void)memcpy(first.idempotency, failing.activity.idempotency_key, 32U);
    first.kv = 1U;
    CHECK(reserve(descriptor, &first, &answer, false) == 0, "obligation_reserved");
    first_id = get_u64(record_of(&answer) + REC_ID);
    CHECK(observe(descriptor, &after, NULL) == 0 && after.obligations[2] == 1U,
          "obligation_held_at_full_blob_store");
    CHECK(by_id(descriptor, CANCEL, first_id, &answer) == 0 &&
          answer.tag == ERROR_RESPONSE && answer.refusal_class == 4U &&
          answer.refusal == LXP_ERR_AUTH_SCOPE, "obligation_not_cancellable");
    CHECK(by_id(descriptor, RECONCILE, first_id, &answer) == 0 &&
          answer.tag == RECONCILE + 1U &&
          answer.payload[REC_STATE] == LXP_CAPACITY_RESERVED &&
          get_u64(answer.payload + REC_OUTCOME_SEQUENCE) == 0U,
          "pending_obligation_keeps_reserve");
    result = activity_execute(&failing, fx.debtor);
    CHECK(result == LXP_ERR_UNKNOWN_FIELD, "obligation_failed_committed");
    CHECK(canonical_digest(&failing, debtor_did, digest) == 0, "receipt_digest");
    CHECK(by_id(descriptor, RECONCILE, first_id, &answer) == 0 &&
          answer.tag == RECONCILE + 1U &&
          answer.payload[REC_STATE] == LXP_CAPACITY_RESERVED &&
          get_u64(answer.payload + REC_OUTCOME_SEQUENCE) ==
              fx.receipt.global_sequence &&
          (int32_t)get_u32(answer.payload + REC_OUTCOME_RESULT) ==
              LXP_ERR_UNKNOWN_FIELD &&
          memcmp(answer.payload + REC_OUTCOME_DIGEST, digest, 32U) == 0,
          "failed_obligation_still_reserved");
    CHECK(observe(descriptor, &head, NULL) == 0 && head.obligations[2] == 1U,
          "failed_obligation_counted");
    duplicate = first;
    duplicate.sequence = head.next_sequence;
    (void)memcpy(duplicate.root, head.root, 32U);
    CHECK(reserve_refused(descriptor, &duplicate, 4U,
                          LXP_ERR_DUPLICATE_ENTRY) == 0, "duplicate_obligation");
    CHECK(activity_prepare(&paying, fx.payer, payer_did, payer_seed, 0x77U) == 0,
          "successor_built");
    request_at(&successor, LXP_CAPACITY_OBLIGATION, 0U, payer_did, &head);
    (void)memcpy(successor.activity, paying.id, 32U);
    (void)memcpy(successor.idempotency, paying.activity.idempotency_key, 32U);
    successor.kv = 2U;
    successor.supersedes = first_id;
    CHECK(reserve_refused(descriptor, &successor, 4U,
                          LXP_ERR_CONDITION_UNMET) == 0, "unequal_successor");
    successor.kv = 1U;
    successor.supersedes = 999U;
    CHECK(reserve_refused(descriptor, &successor, 4U,
                          LXP_ERR_UNKNOWN_FIELD) == 0, "unknown_predecessor");
    successor.supersedes = first_id;
    CHECK(reserve(descriptor, &successor, &answer, false) == 0 &&
          get_u64(record_of(&answer) + REC_SUPERSEDES) == first_id,
          "successor_reserved");
    second_id = get_u64(record_of(&answer) + REC_ID);
    CHECK(by_id(descriptor, RECONCILE, first_id, &answer) == 0 &&
          answer.payload[REC_STATE] == LXP_CAPACITY_SUPERSEDED,
          "predecessor_superseded");
    CHECK(observe(descriptor, &after, NULL) == 0 && after.obligations[2] == 1U,
          "reserve_carried_not_doubled");
    result = activity_execute(&paying, fx.payer);
    CHECK(result == LXP_OK, "obligation_succeeded");
    CHECK(canonical_digest(&paying, payer_did, digest) == 0,
          "success_receipt_digest");
    CHECK(by_id(descriptor, RECONCILE, second_id, &answer) == 0 &&
          answer.payload[REC_STATE] == LXP_CAPACITY_RECONCILED &&
          (int32_t)get_u32(answer.payload + REC_OUTCOME_RESULT) == LXP_OK &&
          memcmp(answer.payload + REC_OUTCOME_DIGEST, digest, 32U) == 0,
          "success_reconciled");
    CHECK(observe(descriptor, &after, NULL) == 0 && after.obligations[2] == 0U,
          "reserve_released_on_receipt");
    return pass("obligations_reserved_until_receipt");
}

static const char *field(const char *line, const char *name, char *out,
                         size_t capacity)
{
    const char *start = strstr(line, name);
    size_t length = 0U;
    if (start == NULL) return NULL;
    start += strlen(name);
    while (start[length] != '\0' && start[length] != ' ' &&
           start[length] != '\n' && length + 1U < capacity) {
        out[length] = start[length];
        ++length;
    }
    out[length] = '\0';
    return out;
}

static int case_keeper_recovery(int descriptor)
{
    static const char *const stage[] = {
        "stage", "--kind", "work", "--activity",
        "4141414141414141414141414141414141414141414141414141414141414141",
        "--idempotency",
        "4242424242424242424242424242424242424242424242424242424242424242",
        "--actor", "did:lxp:capacity-payer", "--kv", "1", "--lifetime", "4"};
    static const char *const recover[] = {"recover"};
    static const char *const status_command[] = {"status"};
    static const char *const observe_command[] = {"observe"};
    char output[KEEPER_OUTPUT_BYTES];
    char text[2U * LXP_CAPACITY_REQUEST_BYTES + 2U];
    char expected[64];
    uint8_t body[LXP_CAPACITY_REQUEST_BYTES];
    reply answer;
    uint64_t id;
    int exit_code = -1;
    CHECK(keeper_run(observe_command, 1U, output, &exit_code) == 0 &&
          exit_code == 0 && strstr(output, "profile_match=1") != NULL,
          "keeper_observe_verified");
    CHECK(keeper_run(stage, sizeof(stage) / sizeof(stage[0]), output,
                     &exit_code) == 0 && exit_code == 0, "keeper_stage");
    CHECK(field(output, "request=", text, sizeof(text)) != NULL &&
          strlen(text) == 2U * LXP_CAPACITY_REQUEST_BYTES &&
          hex_decode(text, LXP_CAPACITY_REQUEST_BYTES, body) == 0,
          "staged_bytes");
    CHECK(call(descriptor, RESERVE, body, sizeof(body), &answer) == 0 &&
          answer.tag == RESERVE + 1U && answer.payload[0] == 0U,
          "lost_answer_reserved");
    id = get_u64(answer.payload + 1U + REC_ID);
    CHECK(keeper_run(status_command, 1U, output, &exit_code) == 0 &&
          exit_code == 0 && strstr(output, "unanswered digest=") != NULL,
          "keeper_knows_unanswered");
    CHECK(keeper_run(recover, 1U, output, &exit_code) == 0 && exit_code == 0,
          "keeper_recover");
    (void)snprintf(expected, sizeof(expected),
                   "reservation id=%llu kind=work state=reserved replayed=1",
                   (unsigned long long)id);
    CHECK(strstr(output, expected) != NULL &&
          strstr(output, "recovered resent=1 pending=0 abandoned=0") != NULL,
          "recover_returns_same_identity");
    {
        static const char *const cancel_prefix = "cancel";
        char id_text[24];
        const char *cancel[3];
        (void)snprintf(id_text, sizeof(id_text), "%llu",
                       (unsigned long long)id);
        cancel[0] = cancel_prefix;
        cancel[1] = "--request-id";
        cancel[2] = id_text;
        CHECK(keeper_run(cancel, 3U, output, &exit_code) == 0 &&
              exit_code == 0 && strstr(output, "state=cancelled") != NULL,
              "keeper_cancel");
    }
    CHECK(keeper_run(recover, 1U, output, &exit_code) == 0 && exit_code == 0 &&
          strstr(output, "recovered resent=0 pending=0 abandoned=0 "
                         "reconciled=0") != NULL, "recover_idempotent");
    CHECK(keeper_run(status_command, 1U, output, &exit_code) == 0 &&
          exit_code == 0 && strstr(output, "state=cancelled") != NULL &&
          strstr(output, "unanswered") == NULL, "keeper_state_durable");
    return pass("keeper_persists_and_recovers");
}

int main(int argument_count, char **arguments)
{
    connection link;
    int failed = 0;
    if (argument_count != 2) {
        (void)fprintf(stderr, "usage: %s <paxai-storage-keeper>\n",
                      argument_count > 0 ? arguments[0] : "test");
        return 2;
    }
    if (fixture_init(arguments[1]) != 0) {
        (void)fprintf(stderr, "BLOB_ADMISSION_FAIL fixture\n");
        (void)fixture_destroy();
        return 1;
    }
    if (connection_open(&link) != 0) failed = 1;
    if (failed == 0)
        failed = case_disabled_and_malformed(link.descriptor) != 0 ||
            case_install(link.descriptor) != 0 ||
            case_reads_hold_nothing(link.descriptor) != 0 ||
            case_work_reservation(link.descriptor) != 0 ||
            case_stale_after_commit(link.descriptor) != 0 ||
            case_race() != 0;
    if (connection_close(&link) != 0) failed = 1;
    if (failed == 0) failed = case_restart();
    if (failed == 0 && connection_open(&link) == 0) {
        failed = case_full_store(link.descriptor) != 0 ||
            case_obligations(link.descriptor) != 0 ||
            case_keeper_recovery(link.descriptor) != 0;
        if (connection_close(&link) != 0) failed = 1;
    } else if (failed == 0) {
        failed = 1;
    }
    if (failed == 0 && fx.applied != 0U) {
        (void)fprintf(stderr, "BLOB_ADMISSION_FAIL capacity entered admission\n");
        failed = 1;
    }
    if (fixture_destroy() != 0) failed = 1;
    if (failed != 0) return 1;
    (void)printf("PASSED %zu\n", passed);
    return 0;
}
