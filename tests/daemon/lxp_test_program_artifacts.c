#define OPENSSL_API_COMPAT 0x10100000L
#define _POSIX_C_SOURCE 200809L
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_crypto.h"
#include "../../cmd/layerxd/lxp_daemon_deployment.h"
#include "../../src/modules/programs/storage.h"
#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static unsigned program_state_observer_calls;
static int program_state_store_observer(const lxp_kernel *kernel,
    const uint8_t program_id[32], const lxp_sequencer_authorization *authorization,
    const lxp_batch_header *header, const lxp_byte_span receipts[2],
    lxp_byte_span canonical_header, const uint8_t signature[64]);
#define LXP_TEST_PROGRAM_STATE_OBSERVER program_state_store_observer

static unsigned artifact_observer_calls;
static unsigned web_request_observer_calls;
static int artifact_store_observer(const lxp_receipt *executed);
static int web_request_store_observer(const lxp_receipt *executed);
int web_program_path_main(int argc, char **argv);
#define LXP_TEST_PROGRAM_ARTIFACT_OBSERVER artifact_store_observer
#define LXP_TEST_WEB_REQUEST_OBSERVER web_request_store_observer
#define LXP_TEST_WEB_PROGRAM_PATH_MAIN web_program_path_main
#include "../test_web_program_path.c"
#undef LXP_TEST_WEB_PROGRAM_PATH_MAIN
#undef LXP_TEST_WEB_REQUEST_OBSERVER
#undef LXP_TEST_PROGRAM_ARTIFACT_OBSERVER
#undef LXP_TEST_PROGRAM_STATE_OBSERVER

static void artifact_hex(const uint8_t *bytes, size_t length, char *text)
{
    static const char digits[] = "0123456789abcdef";
    size_t index;
    for (index = 0U; index < length; ++index) {
        text[index * 2U] = digits[bytes[index] >> 4U];
        text[index * 2U + 1U] = digits[bytes[index] & 15U];
    }
    text[length * 2U] = '\0';
}

static int artifact_store_observer(const lxp_receipt *executed)
{
    static const uint8_t secret[32] = {0x39U};
    static const uint8_t bearer[] = "artifact-fixture-owned-bearer";
    char directory[] = "/tmp/lxp-program-artifacts-XXXXXX";
    char path[256], corrupt_path[256], legacy_path[256], route[256];
    char activity_hex[65], digest_hex[65];
    uint8_t digest[32], signature[64];
    uint8_t *storage = NULL, *body = NULL;
    char *terminal_hex = NULL, *graph_hex = NULL, *expected = NULL;
    size_t expected_capacity, mark, public_length = 32U;
    lxp_receipt receipt = *executed;
    lxp_batch_header batch = {0};
    lxp_sequencer_authorization authorization = {0};
    lxp_merkle_proof proof = {0};
    lxp_arena arena;
    lxp_byte_span canonical_receipt, canonical_header, canonical_events;
    lxp_daemon_receipt_evidence evidence;
    lxp_daemon_receipt_authority_store store, reopened, corrupted, legacy;
    lxp_daemon_protocol_owner *owner = NULL;
    lxp_daemon_protocol_response response;
    lxp_log log = {.descriptor = -1}, corrupt_log = {.descriptor = -1};
    lxp_log legacy_log = {.descriptor = -1};
    lxp_log_record_header record;
    EVP_PKEY *key = NULL;
    uint64_t offset;
    bool mutex_ready = false, directory_ready = false;
    int result = 1;
#define REQUIRE(expression) do { if (!(expression)) { \
    (void)fprintf(stderr, "program artifact check failed at line %d\n", __LINE__); \
    goto done; } } while (0)
    REQUIRE(receipt.result_code == LXP_OK && receipt.program_outcome.present &&
            receipt.program_outcome.terminal_payload.length != 0U &&
            receipt.program_outcome.call_graph_payload.length != 0U);
    storage = malloc(16U * LXP_MAX_ACTIVITY_BYTES);
    owner = calloc(1U, sizeof(*owner));
    REQUIRE(storage != NULL && owner != NULL);
    REQUIRE(lxp_arena_init(&arena, storage, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, secret, sizeof(secret));
    REQUIRE(key != NULL && EVP_PKEY_get_raw_public_key(
                key, authorization.public_key, &public_length) == 1 && public_length == 32U);
    memcpy(authorization.sequencer_id, authorization.public_key, 32U);
    authorization.authorized = 1U;
    authorization.first_batch_number = 1U;
    authorization.last_batch_number = 1U;
    REQUIRE(lxp_receipt_sign(&receipt, secret, &arena) == LXP_OK);
    REQUIRE(lxp_receipt_encode(&receipt, true, &arena, &canonical_receipt) == LXP_OK);
    REQUIRE(lxp_receipt_digest(&receipt, &arena, digest) == LXP_OK);
    batch.protocol_version = receipt.protocol_version;
    batch.network_id = 42U;
    batch.epoch = 1U;
    batch.batch_number = 1U;
    batch.first_sequence = receipt.global_sequence;
    batch.last_sequence = receipt.global_sequence;
    batch.timestamp_ms = receipt.timestamp;
    memcpy(batch.previous_state_root, receipt.previous_state_root, 32U);
    memcpy(batch.resulting_state_root, receipt.resulting_state_root, 32U);
    memcpy(batch.activity_merkle_root, receipt.activity_root, 32U);
    memcpy(batch.sequencer_id, authorization.sequencer_id, 32U);
    REQUIRE(lxp_merkle_leaf_hash(canonical_receipt.bytes, canonical_receipt.length,
                                batch.receipt_merkle_root) == LXP_OK);
    REQUIRE(lxp_programs_project_receipt_events(&receipt, &arena, &canonical_events) == LXP_OK);
    REQUIRE(lxp_merkle_leaf_hash(canonical_events.bytes, canonical_events.length,
                                batch.event_merkle_root) == LXP_OK);
    REQUIRE(lxp_merkle_leaf_hash(NULL, 0U, batch.oracle_root) == LXP_OK);
    memcpy(batch.data_availability_root, batch.oracle_root, 32U);
    proof.leaf_count = 1U;
    REQUIRE(lxp_batch_sign(&batch, secret, &authorization, signature, &arena) == LXP_OK);
    REQUIRE(lxp_batch_header_encode(&batch, &arena, &canonical_header) == LXP_OK);
    REQUIRE(mkdtemp(directory) != NULL);
    directory_ready = true;
    REQUIRE(snprintf(path, sizeof(path), "%s/authority.log", directory) > 0);
    REQUIRE(snprintf(corrupt_path, sizeof(corrupt_path), "%s/corrupt.log", directory) > 0);
    REQUIRE(snprintf(legacy_path, sizeof(legacy_path), "%s/legacy.log", directory) > 0);
    REQUIRE(lxp_log_open_or_create(&log, path, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&store, &log, &authorization) == LXP_OK);
    {
        lxp_result append_status = lxp_daemon_receipt_authority_append_artifacts(&store,
            canonical_receipt.bytes, canonical_receipt.length,
            canonical_header.bytes, canonical_header.length, signature, &proof, &arena,
            receipt.program_outcome.terminal_payload,
            receipt.program_outcome.call_graph_payload);
        if (append_status != LXP_OK)
            (void)fprintf(stderr, "artifact append result=%d zero_batch=%u\n",
                (int)append_status, lxp_ct_is_zero(receipt.batch_id, 32U) ? 1U : 0U);
        REQUIRE(append_status == LXP_OK);
    }
    REQUIRE(lxp_log_close(&log) == LXP_OK);
    REQUIRE(lxp_log_open(&log, path) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&reopened, &log, &authorization) == LXP_OK);
    mark = lxp_arena_mark(&arena);
    REQUIRE(lxp_daemon_receipt_authority_lookup(&reopened, digest, &arena, &evidence) == LXP_OK);
    REQUIRE(evidence.format_version == 2U &&
        evidence.terminal_payload.length == receipt.program_outcome.terminal_payload.length &&
        evidence.call_graph.length == receipt.program_outcome.call_graph_payload.length &&
        memcmp(evidence.terminal_payload.bytes, receipt.program_outcome.terminal_payload.bytes,
               evidence.terminal_payload.length) == 0 &&
        memcmp(evidence.call_graph.bytes, receipt.program_outcome.call_graph_payload.bytes,
               evidence.call_graph.length) == 0);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(pthread_mutex_init(&owner->mutex, NULL) == 0);
    mutex_ready = true;
    owner->attached = true;
    owner->receipt_authority = &reopened;
    memcpy(owner->bearer_token, bearer, sizeof(bearer) - 1U);
    owner->bearer_token_length = sizeof(bearer) - 1U;
    artifact_hex(receipt.activity_id, 32U, activity_hex);
    artifact_hex(digest, 32U, digest_hex);
    REQUIRE(snprintf(route, sizeof(route),
        "/v1/programs/activities/%s/artifacts?receipt_digest=%s", activity_hex, digest_hex) > 0);
    terminal_hex = malloc(receipt.program_outcome.terminal_payload.length * 2U + 1U);
    graph_hex = malloc(receipt.program_outcome.call_graph_payload.length * 2U + 1U);
    expected_capacity = 256U + receipt.program_outcome.terminal_payload.length * 2U +
        receipt.program_outcome.call_graph_payload.length * 2U;
    expected = malloc(expected_capacity);
    REQUIRE(terminal_hex != NULL && graph_hex != NULL && expected != NULL);
    artifact_hex(receipt.program_outcome.terminal_payload.bytes,
                 receipt.program_outcome.terminal_payload.length, terminal_hex);
    artifact_hex(receipt.program_outcome.call_graph_payload.bytes,
                 receipt.program_outcome.call_graph_payload.length, graph_hex);
    REQUIRE(snprintf(expected, expected_capacity,
        "{\"activity_id\":\"%s\",\"receipt_digest\":\"%s\",\"terminal_payload\":\"%s\",\"call_graph\":\"%s\"}",
        activity_hex, digest_hex, terminal_hex, graph_hex) > 0);
    REQUIRE(lxp_daemon_protocol_route(owner, bearer, sizeof(bearer) - 1U,
                "GET", route, &arena, &response) == LXP_OK && response.status == 200U);
    REQUIRE(response.body.length == strlen(expected) &&
            memcmp(response.body.bytes, expected, response.body.length) == 0);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(lxp_daemon_protocol_route(owner, NULL, 0U,
                "GET", route, &arena, &response) == LXP_OK && response.status == 401U);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    route[24] = route[24] == '0' ? '1' : '0';
    REQUIRE(lxp_daemon_protocol_route(owner, bearer, sizeof(bearer) - 1U,
                "GET", route, &arena, &response) == LXP_OK && response.status == 503U);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(snprintf(route, sizeof(route),
        "/v1/programs/activities/%s/artifacts?receipt_digest=%s", activity_hex, digest_hex) > 0);
    REQUIRE(lxp_log_read(&log, 0U, &record, NULL, 0U) == LXP_ERR_LENGTH_LIMIT);
    body = malloc(record.body_length);
    REQUIRE(body != NULL && lxp_log_read(&log, 0U, &record, body, record.body_length) == LXP_OK);
    {
        const size_t changed_offsets[3] = {
            record.body_length - 1U,
            5U + 32U + 32U + 8U + 2U + canonical_header.length,
            5U};
        size_t attempt;
        for (attempt = 0U; attempt < 4U; ++attempt) {
            uint32_t body_length = record.body_length;
            if (attempt < 3U) body[changed_offsets[attempt]] ^= 1U;
            else --body_length;
            REQUIRE(lxp_log_open_or_create(&corrupt_log, corrupt_path,
                                          16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
            REQUIRE(lxp_log_append(&corrupt_log, LXP_LOG_STATE_DIFF, receipt.global_sequence,
                    body, body_length, &offset) == LXP_OK);
            REQUIRE(lxp_log_write_boundary(&corrupt_log) == LXP_OK);
            REQUIRE(lxp_daemon_receipt_authority_open(
                &corrupted, &corrupt_log, &authorization) != LXP_OK);
            REQUIRE(lxp_log_close(&corrupt_log) == LXP_OK);
            REQUIRE(unlink(corrupt_path) == 0);
            if (attempt < 3U) body[changed_offsets[attempt]] ^= 1U;
        }
    }
    REQUIRE(lxp_log_open_or_create(&legacy_log, legacy_path,
                                  16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&legacy, &legacy_log, &authorization) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_append(&legacy,
        canonical_receipt.bytes, canonical_receipt.length, canonical_header.bytes,
        canonical_header.length, signature, &proof, &arena) == LXP_OK);
    REQUIRE(lxp_log_close(&legacy_log) == LXP_OK);
    REQUIRE(lxp_log_open(&legacy_log, legacy_path) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&legacy, &legacy_log, &authorization) == LXP_OK);
    owner->receipt_authority = &legacy;
    REQUIRE(lxp_daemon_protocol_route(owner, bearer, sizeof(bearer) - 1U,
                "GET", route, &arena, &response) == LXP_OK && response.status == 503U);
    ++artifact_observer_calls;
    result = 0;
done:
    if (mutex_ready) (void)pthread_mutex_destroy(&owner->mutex);
    if (log.descriptor >= 0) (void)lxp_log_close(&log);
    if (corrupt_log.descriptor >= 0) (void)lxp_log_close(&corrupt_log);
    if (legacy_log.descriptor >= 0) (void)lxp_log_close(&legacy_log);
    if (directory_ready) {
        (void)unlink(path); (void)unlink(corrupt_path); (void)unlink(legacy_path);
        (void)rmdir(directory);
    }
    EVP_PKEY_free(key);
    free(expected); free(graph_hex); free(terminal_hex);
    free(body); free(owner); free(storage);
#undef REQUIRE
    return result;
}

static int web_request_event(lxp_byte_span list, const uint8_t **program,
                             lxp_byte_span *data)
{
    static const uint8_t domain[] = "LayerX/programs/events/v1";
    size_t cursor = sizeof(domain);
    size_t found = 0U;
    uint32_t count, index;
    if (list.bytes == NULL || list.length < sizeof(domain) + 4U ||
        memcmp(list.bytes, domain, sizeof(domain)) != 0)
        return 1;
    count = path_read_u32(list.bytes + cursor);
    cursor += 4U;
    for (index = 0U; index < count; ++index) {
        const uint8_t *event_program;
        const uint8_t *topic;
        uint32_t topic_length, data_length;
        if (list.length - cursor < 32U + 32U + 8U + 1U + 4U) return 1;
        event_program = list.bytes + cursor;
        cursor += 32U + 32U + 8U + 1U;
        topic_length = path_read_u32(list.bytes + cursor);
        cursor += 4U;
        if (list.length - cursor < (size_t)topic_length + 4U) return 1;
        topic = list.bytes + cursor;
        cursor += topic_length;
        data_length = path_read_u32(list.bytes + cursor);
        cursor += 4U;
        if (list.length - cursor < data_length) return 1;
        if (topic_length == LX_WEB_REQUEST_TOPIC_BYTES &&
            memcmp(topic, LX_WEB_REQUEST_TOPIC, LX_WEB_REQUEST_TOPIC_BYTES) == 0) {
            *program = event_program;
            *data = (lxp_byte_span){list.bytes + cursor, data_length};
            ++found;
        }
        cursor += data_length;
    }
    return cursor == list.length && found == 1U ? 0 : 1;
}

static int events_page(lxp_daemon_protocol_owner *owner, const uint8_t *bearer,
                       size_t bearer_length, const char *route, lxp_arena *arena,
                       uint16_t status, const char *expected)
{
    lxp_daemon_protocol_response response = {0};
    size_t mark = lxp_arena_mark(arena);
    int result = lxp_daemon_protocol_route(owner, bearer, bearer_length, "GET",
                                           route, arena, &response) == LXP_OK &&
                         response.status == status &&
                         (expected == NULL ||
                          (response.body.length == strlen(expected) &&
                           memcmp(response.body.bytes, expected,
                                  response.body.length) == 0)) ? 0 : 1;
    if (result != 0)
        (void)fprintf(stderr, "events route %s answered %u %.*s\n", route,
                      (unsigned)response.status, (int)response.body.length,
                      (const char *)response.body.bytes);
    (void)lxp_arena_reset(arena, mark);
    return result;
}

static int web_request_store_observer(const lxp_receipt *executed)
{
    static const uint8_t secret[32] = {0x3aU};
    static const uint8_t bearer[] = "web-request-fixture-owned-bearer";
    static const char other_topic_hex[] =
        "504158454552585f4f544845525f544f5049435f5631";
    char directory[] = "/tmp/lxp-program-events-XXXXXX";
    char path[256], plain_path[256], route[512], other_route[512];
    char topic_hex[2U * LX_WEB_REQUEST_TOPIC_BYTES + 1U];
    char program_hex[65], activity_hex[65], digest_hex[65];
    uint8_t digest[32], signature[64];
    uint8_t *storage = NULL, *tampered = NULL;
    char *data_hex = NULL, *expected = NULL, *empty = NULL;
    const uint8_t *event_program = NULL;
    lxp_byte_span event_data = {NULL, 0U};
    lxp_byte_span list = executed->program_outcome.event_envelope_payload;
    lxp_byte_span request_payload = {NULL, 0U};
    size_t expected_capacity, mark, public_length = 32U;
    uint64_t request_id = 0U, sequence, head;
    uint8_t request_kind = 0U;
    lxp_receipt receipt = *executed;
    lxp_batch_header batch = {0};
    lxp_sequencer_authorization authorization = {0};
    lxp_merkle_proof proof = {0};
    lxp_arena arena;
    lxp_byte_span canonical_receipt, canonical_header, canonical_events;
    lxp_daemon_receipt_evidence evidence;
    lxp_daemon_receipt_authority_store store, reopened, plain;
    lxp_daemon_protocol_owner *owner = NULL;
    lxp_log log = {.descriptor = -1}, plain_log = {.descriptor = -1};
    EVP_PKEY *key = NULL;
    bool mutex_ready = false, directory_ready = false;
    int result = 1;
#define REQUIRE(expression) do { if (!(expression)) { \
    (void)fprintf(stderr, "program events check failed at line %d\n", __LINE__); \
    goto done; } } while (0)
    REQUIRE(receipt.result_code == LXP_OK && receipt.program_outcome.present &&
            receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS &&
            list.length != 0U);
    REQUIRE(web_request_event(list, &event_program, &event_data) == 0);
    REQUIRE(lx_web_request_record_decode(event_data.bytes, event_data.length,
                                         &request_id, &request_kind,
                                         &request_payload) == LXP_OK);
    REQUIRE(request_id == path_request && request_kind == LX_WEB_KIND_FETCH &&
            request_payload.length == PATH_PAYLOAD_BYTES &&
            memcmp(request_payload.bytes, path_payload, PATH_PAYLOAD_BYTES) == 0);
    storage = malloc(16U * LXP_MAX_ACTIVITY_BYTES);
    owner = calloc(1U, sizeof(*owner));
    tampered = malloc(list.length);
    REQUIRE(storage != NULL && owner != NULL && tampered != NULL);
    REQUIRE(lxp_arena_init(&arena, storage, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, secret, sizeof(secret));
    REQUIRE(key != NULL && EVP_PKEY_get_raw_public_key(
                key, authorization.public_key, &public_length) == 1 && public_length == 32U);
    memcpy(authorization.sequencer_id, authorization.public_key, 32U);
    authorization.authorized = 1U;
    authorization.first_batch_number = 1U;
    authorization.last_batch_number = 1U;
    REQUIRE(lxp_receipt_sign(&receipt, secret, &arena) == LXP_OK);
    REQUIRE(lxp_receipt_encode(&receipt, true, &arena, &canonical_receipt) == LXP_OK);
    REQUIRE(lxp_receipt_digest(&receipt, &arena, digest) == LXP_OK);
    sequence = receipt.global_sequence;
    REQUIRE(sequence != 0U && sequence < UINT64_MAX);
    head = sequence + 1U;
    batch.protocol_version = receipt.protocol_version;
    batch.network_id = 42U;
    batch.epoch = 1U;
    batch.batch_number = 1U;
    batch.first_sequence = sequence;
    batch.last_sequence = sequence;
    batch.timestamp_ms = receipt.timestamp;
    memcpy(batch.previous_state_root, receipt.previous_state_root, 32U);
    memcpy(batch.resulting_state_root, receipt.resulting_state_root, 32U);
    memcpy(batch.activity_merkle_root, receipt.activity_root, 32U);
    memcpy(batch.sequencer_id, authorization.sequencer_id, 32U);
    REQUIRE(lxp_merkle_leaf_hash(canonical_receipt.bytes, canonical_receipt.length,
                                batch.receipt_merkle_root) == LXP_OK);
    REQUIRE(lxp_programs_project_receipt_events(&receipt, &arena, &canonical_events) == LXP_OK);
    REQUIRE(lxp_merkle_leaf_hash(canonical_events.bytes, canonical_events.length,
                                batch.event_merkle_root) == LXP_OK);
    REQUIRE(lxp_merkle_leaf_hash(NULL, 0U, batch.oracle_root) == LXP_OK);
    memcpy(batch.data_availability_root, batch.oracle_root, 32U);
    proof.leaf_count = 1U;
    REQUIRE(lxp_batch_sign(&batch, secret, &authorization, signature, &arena) == LXP_OK);
    REQUIRE(lxp_batch_header_encode(&batch, &arena, &canonical_header) == LXP_OK);
    REQUIRE(mkdtemp(directory) != NULL);
    directory_ready = true;
    REQUIRE(snprintf(path, sizeof(path), "%s/authority.log", directory) > 0);
    REQUIRE(snprintf(plain_path, sizeof(plain_path), "%s/plain.log", directory) > 0);
    REQUIRE(lxp_log_open_or_create(&log, path, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&store, &log, &authorization) == LXP_OK);
    memcpy(tampered, list.bytes, list.length);
    tampered[list.length - 1U] ^= 1U;
    REQUIRE(lxp_daemon_receipt_authority_append_event_list(&store,
        canonical_receipt.bytes, canonical_receipt.length,
        canonical_header.bytes, canonical_header.length, signature, &proof, &arena,
        receipt.program_outcome.terminal_payload,
        receipt.program_outcome.call_graph_payload,
        (lxp_byte_span){tampered, list.length}) != LXP_OK);
    REQUIRE(store.record_count == 0U && log.write_offset == 0U);
    REQUIRE(lxp_daemon_receipt_authority_append_event_list(&store,
        canonical_receipt.bytes, canonical_receipt.length,
        canonical_header.bytes, canonical_header.length, signature, &proof, &arena,
        receipt.program_outcome.terminal_payload,
        receipt.program_outcome.call_graph_payload, list) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_append_event_list(&store,
        canonical_receipt.bytes, canonical_receipt.length,
        canonical_header.bytes, canonical_header.length, signature, &proof, &arena,
        receipt.program_outcome.terminal_payload,
        receipt.program_outcome.call_graph_payload, list) == LXP_OK);
    REQUIRE(store.record_count == 1U);
    REQUIRE(lxp_log_close(&log) == LXP_OK);
    REQUIRE(lxp_log_open(&log, path) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&reopened, &log, &authorization) == LXP_OK);
    mark = lxp_arena_mark(&arena);
    REQUIRE(lxp_daemon_receipt_authority_lookup(&reopened, digest, &arena, &evidence) == LXP_OK);
    REQUIRE(evidence.format_version == 4U && evidence.global_sequence == sequence &&
            evidence.event_list.length == list.length &&
            memcmp(evidence.event_list.bytes, list.bytes, list.length) == 0 &&
            evidence.terminal_payload.length ==
                receipt.program_outcome.terminal_payload.length &&
            evidence.call_graph.length == receipt.program_outcome.call_graph_payload.length);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(pthread_mutex_init(&owner->mutex, NULL) == 0);
    mutex_ready = true;
    owner->attached = true;
    owner->receipt_authority = &reopened;
    memcpy(owner->bearer_token, bearer, sizeof(bearer) - 1U);
    owner->bearer_token_length = sizeof(bearer) - 1U;
    artifact_hex(receipt.activity_id, 32U, activity_hex);
    artifact_hex(digest, 32U, digest_hex);
    REQUIRE(snprintf(route, sizeof(route),
        "/v1/programs/activities/%s/artifacts?receipt_digest=%s", activity_hex, digest_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, NULL) == 0);
    artifact_hex((const uint8_t *)LX_WEB_REQUEST_TOPIC, LX_WEB_REQUEST_TOPIC_BYTES, topic_hex);
    artifact_hex(event_program, 32U, program_hex);
    data_hex = malloc(event_data.length * 2U + 1U);
    expected_capacity = 512U + event_data.length * 2U;
    expected = malloc(expected_capacity);
    empty = malloc(128U);
    REQUIRE(data_hex != NULL && expected != NULL && empty != NULL);
    artifact_hex(event_data.bytes, event_data.length, data_hex);
    REQUIRE(snprintf(expected, expected_capacity,
        "{\"events\":[{\"sequence\":%llu,\"program_id\":\"%s\",\"topic\":\"%s\","
        "\"data\":\"%s\"}],\"next_sequence\":%llu}",
        (unsigned long long)sequence, program_hex, topic_hex, data_hex,
        (unsigned long long)head) > 0);
    REQUIRE(snprintf(empty, 128U, "{\"events\":[],\"next_sequence\":%llu}",
                     (unsigned long long)head) > 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/0/256", topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, expected) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/%llu/1", topic_hex,
                     (unsigned long long)sequence) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, expected) == 0);
    REQUIRE(events_page(owner, NULL, 0U, route, &arena, 401U, NULL) == 0);
    REQUIRE(snprintf(other_route, sizeof(other_route), "/v1/programs/events/%s/0/256",
                     other_topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, other_route, &arena, 200U, empty) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/%llu/256", topic_hex,
                     (unsigned long long)head) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, empty) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/%llu/256", topic_hex,
                     (unsigned long long)(head + 7U)) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, empty) == 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U,
        "/v1/programs/events/504158454552585F5745425F524551554553545F5631/0/256",
        &arena, 503U, NULL) == 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U,
        "/v1/programs/events/504/0/256", &arena, 503U, NULL) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/0/0", topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 503U, NULL) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/0/257", topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 503U, NULL) == 0);
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/x/256", topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 503U, NULL) == 0);
    REQUIRE(lxp_log_open_or_create(&plain_log, plain_path,
                                  16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&plain, &plain_log, &authorization) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_append_artifacts(&plain,
        canonical_receipt.bytes, canonical_receipt.length,
        canonical_header.bytes, canonical_header.length, signature, &proof, &arena,
        receipt.program_outcome.terminal_payload,
        receipt.program_outcome.call_graph_payload) == LXP_OK);
    REQUIRE(lxp_log_close(&plain_log) == LXP_OK);
    REQUIRE(lxp_log_open(&plain_log, plain_path) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&plain, &plain_log, &authorization) == LXP_OK);
    mark = lxp_arena_mark(&arena);
    REQUIRE(lxp_daemon_receipt_authority_lookup(&plain, digest, &arena, &evidence) == LXP_OK &&
            evidence.format_version == 2U && evidence.event_list.length == 0U);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    owner->receipt_authority = &plain;
    REQUIRE(snprintf(route, sizeof(route), "/v1/programs/events/%s/0/256", topic_hex) > 0);
    REQUIRE(events_page(owner, bearer, sizeof(bearer) - 1U, route, &arena, 200U, empty) == 0);
    ++web_request_observer_calls;
    result = 0;
done:
    if (mutex_ready) (void)pthread_mutex_destroy(&owner->mutex);
    if (log.descriptor >= 0) (void)lxp_log_close(&log);
    if (plain_log.descriptor >= 0) (void)lxp_log_close(&plain_log);
    if (directory_ready) {
        (void)unlink(path); (void)unlink(plain_path);
        (void)rmdir(directory);
    }
    EVP_PKEY_free(key);
    free(empty); free(expected); free(data_hex);
    free(tampered); free(owner); free(storage);
#undef REQUIRE
    return result;
}


static int program_state_store_observer(const lxp_kernel *kernel,
    const uint8_t program_id[32], const lxp_sequencer_authorization *authorization,
    const lxp_batch_header *header, const lxp_byte_span receipts[2],
    lxp_byte_span canonical_header, const uint8_t signature[64])
{
    char directory[] = "/tmp/lxp-program-state-proof-XXXXXX";
    char path[256] = {0};
    uint8_t *storage = NULL;
    uint8_t leaves[2][32], merkle_root[32], digest[32];
    lxp_arena arena;
    lxp_merkle_proof proofs[2];
    lxp_log log = {.descriptor = -1};
    lxp_daemon_receipt_authority_store store, reopened;
    lxp_programs_occupancy_receipt receipt;
    lxp_daemon_receipt_evidence evidence;
    lxp_byte_span canonical_receipt = receipts[1];
    size_t mark;
    bool directory_ready = false;
    int result = 1;
#define REQUIRE(expression) do { if (!(expression)) { \
    (void)fprintf(stderr, "program state proof check failed at line %d\n", __LINE__); \
    goto done; } } while (0)
    storage = malloc(16U * LXP_MAX_ACTIVITY_BYTES);
    REQUIRE(storage != NULL);
    REQUIRE(lxp_arena_init(&arena, storage, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(header->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT);
    REQUIRE(lxp_batch_maintenance_occupancy_decode(canonical_receipt.bytes,
        canonical_receipt.length, &receipt) == LXP_OK);
    REQUIRE(receipt.global_sequence == header->last_sequence &&
        memcmp(receipt.resulting_state_root, kernel->current_state_root, 32U) == 0);
    for (size_t index = 0U; index < 2U; ++index)
        REQUIRE(lxp_merkle_leaf_hash(receipts[index].bytes,
            receipts[index].length, leaves[index]) == LXP_OK);
    for (size_t index = 0U; index < 2U; ++index) {
        REQUIRE(lxp_merkle_proof_generate((const uint8_t (*)[32])leaves, 2U,
            index, &arena, &proofs[index], merkle_root) == LXP_OK);
        REQUIRE(memcmp(merkle_root, header->receipt_merkle_root, 32U) == 0);
    }
    REQUIRE(mkdtemp(directory) != NULL);
    directory_ready = true;
    REQUIRE(snprintf(path, sizeof(path), "%s/authority.log", directory) > 0);
    REQUIRE(lxp_log_open_or_create(&log, path, 16U * LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&store, &log, authorization) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_append(&store,
        receipts[0].bytes, receipts[0].length, canonical_header.bytes,
        canonical_header.length, signature, &proofs[0], &arena) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_append_maintenance(&store,
        canonical_receipt.bytes, canonical_receipt.length, canonical_header.bytes,
        canonical_header.length, signature, &proofs[1], &arena) == LXP_OK);
    REQUIRE(lxp_log_close(&log) == LXP_OK);
    REQUIRE(lxp_log_open(&log, path) == LXP_OK);
    REQUIRE(lxp_daemon_receipt_authority_open(&reopened, &log, authorization) == LXP_OK);
    REQUIRE(lxp_hash_sha256(canonical_receipt.bytes, canonical_receipt.length, digest) == LXP_OK);
    mark = lxp_arena_mark(&arena);
    REQUIRE(lxp_daemon_receipt_authority_lookup(&reopened, digest, &arena, &evidence) == LXP_OK);
    REQUIRE(evidence.format_version == 3U && evidence.global_sequence == receipt.global_sequence &&
        evidence.canonical_receipt.length == canonical_receipt.length &&
        memcmp(evidence.canonical_receipt.bytes, canonical_receipt.bytes, canonical_receipt.length) == 0);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    static const uint8_t domain[] = "LayerX/programs/state-proof/v1";
    lxp_byte_span current;
    uint8_t other[32], root[32], programs_root[32];
    lxp_state_proof root_proof;
    size_t cursor, count;
    const uint8_t *keys[4], *values[4];
    size_t key_lengths[4], value_lengths[4];
    REQUIRE(lxp_daemon_program_state_encode(kernel, &reopened, header->network_id,
        program_id, receipt.global_sequence, digest, receipt.resulting_state_root,
        &arena, &current) == LXP_OK);
    REQUIRE(current.length > sizeof(domain) &&
        memcmp(current.bytes, domain, sizeof(domain)) == 0 && current.bytes[sizeof(domain)] == 1U);
    cursor = sizeof(domain) + 1U;
    for (count = 0U; count < 3U; ++count) {
        size_t length;
        REQUIRE(current.length - cursor >= 4U);
        length = path_read_u32(current.bytes + cursor);
        cursor += 4U;
        REQUIRE(length <= current.length - cursor);
        if (count == 0U) REQUIRE(length == canonical_receipt.length &&
            memcmp(current.bytes + cursor, canonical_receipt.bytes, length) == 0);
        if (count == 2U) REQUIRE(length == canonical_header.length &&
            memcmp(current.bytes + cursor, canonical_header.bytes, length) == 0);
        cursor += length;
    }
    REQUIRE(current.length - cursor >= 64U + 32U + 9U);
    REQUIRE(memcmp(current.bytes + cursor, signature, 64U) == 0);
    cursor += 64U;
    REQUIRE(lxp_state_subtree_root(kernel, 9U, programs_root) == LXP_OK);
    REQUIRE(memcmp(current.bytes + cursor, programs_root, 32U) == 0);
    cursor += 32U;
    REQUIRE(lxp_state_root_proof(kernel, 9U, root, &root_proof) == LXP_OK);
    REQUIRE(memcmp(root, receipt.resulting_state_root, 32U) == 0);
    REQUIRE(path_read_u32(current.bytes + cursor) == root_proof.leaf_index &&
        path_read_u32(current.bytes + cursor + 4U) == root_proof.leaf_count &&
        current.bytes[cursor + 8U] == root_proof.depth);
    cursor += 9U;
    REQUIRE(current.length - cursor >= 32U * root_proof.depth &&
        memcmp(current.bytes + cursor, root_proof.siblings, 32U * root_proof.depth) == 0);
    cursor += 32U * root_proof.depth;
    count = 0U;
    for (size_t field = 0U; field < 4U; ++field) {
        lxp_state_proof leaf_proof;
        if (field == 1U) {
            REQUIRE(current.length > cursor && current.bytes[cursor++] == 0U);
        }
        if (field == 1U || field == 2U) {
            REQUIRE(current.length > cursor && current.bytes[cursor] <= 1U);
            if (current.bytes[cursor++] == 0U) continue;
        }
        REQUIRE(current.length - cursor >= 4U);
        key_lengths[count] = path_read_u32(current.bytes + cursor); cursor += 4U;
        REQUIRE(key_lengths[count] <= current.length - cursor);
        keys[count] = current.bytes + cursor; cursor += key_lengths[count];
        REQUIRE(current.length - cursor >= 4U);
        value_lengths[count] = path_read_u32(current.bytes + cursor); cursor += 4U;
        REQUIRE(value_lengths[count] <= current.length - cursor);
        values[count] = current.bytes + cursor; cursor += value_lengths[count];
        REQUIRE(lxp_state_subtree_proof(kernel, 9U, keys[count], key_lengths[count], root, &leaf_proof) == LXP_OK);
        REQUIRE(memcmp(root, programs_root, 32U) == 0 && current.length - cursor >= 9U);
        REQUIRE(path_read_u32(current.bytes + cursor) == leaf_proof.leaf_index &&
            path_read_u32(current.bytes + cursor + 4U) == leaf_proof.leaf_count &&
            current.bytes[cursor + 8U] == leaf_proof.depth);
        cursor += 9U;
        REQUIRE(current.length - cursor >= 32U * leaf_proof.depth &&
            memcmp(current.bytes + cursor, leaf_proof.siblings, 32U * leaf_proof.depth) == 0);
        cursor += 32U * leaf_proof.depth;
        ++count;
    }
    REQUIRE(cursor == current.length && count >= 3U &&
        key_lengths[0] == 40U && memcmp(keys[0] + 8U, program_id, 32U) == 0 &&
        value_lengths[0] == 71U && key_lengths[count - 1U] == 42U &&
        memcmp(keys[count - 1U] + 10U, program_id, 32U) == 0 &&
        value_lengths[count - 1U] >= 134U &&
        memcmp(values[count - 1U], program_id, 32U) == 0);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    memcpy(other, receipt.resulting_state_root, 32U); other[0] ^= 1U;
    REQUIRE(lxp_daemon_program_state_encode(kernel, &reopened, header->network_id,
        program_id, receipt.global_sequence, digest, other, &arena, &current) == LXP_ERR_PROJECTION_STALE);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(lxp_daemon_program_state_encode(kernel, &reopened, header->network_id,
        program_id, receipt.global_sequence + 1U, digest, receipt.resulting_state_root,
        &arena, &current) == LXP_ERR_CONTEXT_MISMATCH);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    memcpy(other, program_id, 32U); other[0] ^= 1U;
    REQUIRE(lxp_daemon_program_state_encode(kernel, &reopened, header->network_id,
        other, receipt.global_sequence, digest, receipt.resulting_state_root,
        &arena, &current) == LXP_ERR_UNKNOWN_FIELD);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);
    REQUIRE(lxp_daemon_program_state_encode(kernel, &reopened, header->network_id + 1U,
        program_id, receipt.global_sequence, digest, receipt.resulting_state_root,
        &arena, &current) == LXP_ERR_CONTEXT_MISMATCH);
    REQUIRE(lxp_arena_reset(&arena, mark) == LXP_OK);

    ++program_state_observer_calls;
    result = 0;
done:
    if (log.descriptor >= 0) (void)lxp_log_close(&log);
    if (directory_ready) { (void)unlink(path); (void)rmdir(directory); }
    free(storage);
#undef REQUIRE
    return result;
}

enum {
    RESOLUTION_ARENA_BYTES = 8 * 1024 * 1024,
    RESOLUTION_WIDE_VALUE_BYTES = 300,
    RESOLUTION_INTERFACE_MAX_BYTES = 1024
};

typedef struct resolution_fixture {
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_arena arena;
    uint8_t *arena_bytes;
    bool ctx_open;
    lxp_authority_resolved authority;
    lxp_activity activity;
    uint8_t call[CALL_FIXED_BYTES + 128U];
    uint8_t program[2][32];
    uint8_t code_hash[2][32];
    uint8_t wasm[2][128];
    size_t wasm_length[2];
    uint8_t interface[2][RESOLUTION_INTERFACE_MAX_BYTES];
    size_t interface_length[2];
    uint8_t wide[RESOLUTION_WIDE_VALUE_BYTES];
    lxp_programs_storage_cell own[1];
    lxp_programs_storage_cell shared[2];
    lxp_programs_storage_cell foreign[1];
} resolution_fixture;

static int resolution_failed(int line)
{
    (void)fprintf(stderr, "artifact resolution check failed at line %d\n", line);
    return 1;
}

#define RESOLUTION_REQUIRE(expression) do { \
    if (!(expression)) return resolution_failed(__LINE__); } while (0)

static int resolution_ctx_open(resolution_fixture *f, uint64_t sequence)
{
    RESOLUTION_REQUIRE(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_module_ctx_init(&f->ctx, &f->kernel,
        LXP_MODULE_PROGRAMS, 1U, 0U, sequence, 1000000U, &f->arena,
        false) == LXP_OK);
    f->ctx_open = true;
    f->ctx.protocol_version = LXP_PROTOCOL_VERSION_LEGACY;
    f->ctx.batch_number = 1U;
    RESOLUTION_REQUIRE(lxp_effect_buffer_init(&f->effects) == LXP_OK &&
        lxp_module_ctx_bind_effects(&f->ctx, &f->effects) == LXP_OK);
    return 0;
}

static int resolution_ctx_commit(resolution_fixture *f)
{
    RESOLUTION_REQUIRE(lxp_module_ctx_commit(&f->ctx) == LXP_OK);
    f->ctx_open = false;
    return 0;
}

static void resolution_ctx_rollback(resolution_fixture *f)
{
    if (!f->ctx_open) return;
    lxp_module_ctx_rollback(&f->ctx);
    f->ctx_open = false;
}

static int resolution_deploy(resolution_fixture *f, size_t which)
{
    static uint8_t payload[DEPLOY_FIXED_BYTES + INTERFACE_MAX_FIXTURE_BYTES + 128U];
    const uint8_t entry[] = {0x41U, (uint8_t)(which + 1U), 0x0bU};
    const lxp_module_registration *registration;
    lxp_activity activity;
    lxp_result module_result = LXP_FATAL_INVARIANT;
    size_t length;
    (void)memset(f->program[which], 0x31 + (int)which, 32U);
    f->wasm_length[which] = candidate_module(f->wasm[which], entry, sizeof(entry));
    length = deploy_payload(payload, f->program[which], f->authority.principal,
                            f->wasm[which], f->wasm_length[which],
                            f->code_hash[which], LX_PROGRAMS_ABI_VERSION,
                            INTERFACE_CAPABILITIES_NONE);
    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = LX_PROGRAMS_DEPLOY;
    activity.payload = (lxp_byte_span){payload, length};
    if (resolution_ctx_open(f, 1U + which) != 0) return 1;
    RESOLUTION_REQUIRE(lxp_kernel_module_for_activity(&f->kernel,
        activity.activity_type, 0U, &registration) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_kernel_dispatch(registration, &f->ctx, &activity,
        &f->authority, &f->effects, &module_result) == LXP_OK &&
        module_result == LXP_OK);
    return resolution_ctx_commit(f);
}

static void resolution_namespace(const resolution_fixture *f, size_t which,
                                 uint16_t selector, uint8_t ns[65],
                                 uint16_t *ns_length)
{
    (void)memcpy(ns, f->program[which], 32U);
    ns[32] = (uint8_t)selector;
    if (selector == 0U) {
        (void)memcpy(ns + 33U, f->authority.principal, 32U);
        *ns_length = 65U;
    } else *ns_length = 33U;
}

static void resolution_storage_key(const resolution_fixture *f, size_t which,
                                   uint16_t selector, uint8_t key[73],
                                   size_t *key_length)
{
    uint16_t ns_length;
    (void)memcpy(key, "progstor", 8U);
    resolution_namespace(f, which, selector, key + 8U, &ns_length);
    *key_length = 8U + ns_length;
}

static int resolution_seed_storage(resolution_fixture *f)
{
    static const struct {
        size_t which;
        uint16_t selector;
    } namespaces[3] = {{0U, 0U}, {0U, 1U}, {1U, 0U}};
    const lxp_programs_storage_cell *cells[3] = {f->own, f->shared, f->foreign};
    const uint32_t counts[3] = {1U, 2U, 1U};
    uint8_t ns[65];
    uint16_t ns_length;
    size_t index;
    for (index = 0U; index < sizeof(f->wide); ++index)
        f->wide[index] = (uint8_t)(index * 7U + 3U);
    f->own[0] = (lxp_programs_storage_cell){
        (const uint8_t *)"own", 3U, (const uint8_t *)"value", 5U};
    f->shared[0] = (lxp_programs_storage_cell){
        (const uint8_t *)"alpha", 5U, (const uint8_t *)"one", 3U};
    f->shared[1] = (lxp_programs_storage_cell){
        (const uint8_t *)"beta", 4U, (const uint8_t *)"", 0U};
    f->foreign[0] = (lxp_programs_storage_cell){
        (const uint8_t *)"wide", 4U, f->wide, sizeof(f->wide)};
    for (index = 0U; index < 3U; ++index) {
        if (resolution_ctx_open(f, 3U + index) != 0) return 1;
        RESOLUTION_REQUIRE(lxp_module_ctx_set_mutable(&f->ctx, true) == LXP_OK);
        resolution_namespace(f, namespaces[index].which,
                             namespaces[index].selector, ns, &ns_length);
        RESOLUTION_REQUIRE(lxp_programs_storage_stage_final(&f->ctx, ns,
            ns_length, cells[index], counts[index]) == LXP_OK);
        if (resolution_ctx_commit(f) != 0) return 1;
    }
    return 0;
}

static int resolution_open_call(resolution_fixture *f, uint64_t sequence,
                                void **decoded, uint64_t *token)
{
    static const uint8_t did[] = "did:lxp:artifact-resolution";
    size_t length = call_payload(f->call, f->program[0]);
    fill_activity(&f->activity, LX_PROGRAMS_CALL, f->call, length, did,
                  sizeof(did) - 1U, f->authority.principal);
    if (resolution_ctx_open(f, sequence) != 0) return 1;
    f->ctx.call_admission.present = true;
    RESOLUTION_REQUIRE(lxp_programs_call_decode(&f->ctx, f->call, length,
                                                decoded) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_programs_call_validate(&f->ctx, &f->activity,
        &f->authority, *decoded) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_module_ctx_set_mutable(&f->ctx, true) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_programs_call_execute(&f->ctx, &f->activity,
        &f->authority, *decoded, &f->effects) == LXP_ERR_UNKNOWN_FIELD);
    *token = (uint64_t)(uintptr_t)*decoded;
    RESOLUTION_REQUIRE(lxp_ctx_activity_state(&f->ctx) == *decoded &&
                       layerx_programs_call_catalog_count(*token) == 2);
    return 0;
}

static int resolution_catalog_index(uint64_t token, const uint8_t program[32],
                                    uint32_t *index)
{
    uint32_t candidate, offset;
    for (candidate = 0U; candidate < 2U; ++candidate) {
        for (offset = 0U; offset < 32U; ++offset)
            if (layerx_programs_call_catalog_identity_byte(token, candidate, 0U,
                    offset) != (lxp_result)program[offset]) break;
        if (offset == 32U) { *index = candidate; return 0; }
    }
    return resolution_failed(__LINE__);
}

static lxp_result resolution_wasm_byte(uint64_t token, bool catalog,
                                       uint32_t program, uint32_t offset)
{
    return catalog ?
        layerx_programs_call_catalog_wasm_byte(token, program, offset) :
        layerx_programs_call_activity_byte(token, LX_PROGRAMS_ACTIVITY_BYTES_WASM,
                                           offset);
}

static int resolution_wasm_matches(uint64_t token, bool catalog,
                                   uint32_t program, const uint8_t *wasm,
                                   size_t length, uint32_t from)
{
    uint32_t offset;
    for (offset = from; offset < length; ++offset)
        RESOLUTION_REQUIRE(resolution_wasm_byte(token, catalog, program, offset) ==
                           (lxp_result)wasm[offset]);
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, catalog, program,
                                            (uint32_t)length) == LXP_ERR_TRUNCATED);
    return 0;
}

static int resolution_interface_matches(uint64_t token, uint32_t program,
                                        const uint8_t *encoding, size_t length)
{
    uint32_t offset;
    RESOLUTION_REQUIRE(layerx_programs_call_catalog_interface_length(token, program) ==
                       (lxp_result)length);
    for (offset = 0U; offset < length; ++offset)
        RESOLUTION_REQUIRE(layerx_programs_call_catalog_interface_byte(
            token, program, offset) == (lxp_result)encoding[offset]);
    RESOLUTION_REQUIRE(layerx_programs_call_catalog_interface_byte(
        token, program, (uint32_t)length) == LXP_ERR_TRUNCATED);
    return 0;
}

static lxp_result resolution_cell_count(uint64_t token, bool catalog,
                                        uint32_t program, uint16_t selector)
{
    return catalog ?
        layerx_programs_call_catalog_storage_cell_count(token, program, selector) :
        layerx_programs_call_storage_cell_count(token, selector);
}

static lxp_result resolution_cell_length(uint64_t token, bool catalog,
                                         uint32_t program, uint16_t selector,
                                         uint32_t index, uint16_t section)
{
    return catalog ?
        layerx_programs_call_catalog_storage_cell_length(token, program, selector,
                                                         index, section) :
        layerx_programs_call_storage_cell_length(token, selector, index, section);
}

static lxp_result resolution_cell_byte(uint64_t token, bool catalog,
                                       uint32_t program, uint16_t selector,
                                       uint32_t index, uint16_t section,
                                       uint32_t offset)
{
    return catalog ?
        layerx_programs_call_catalog_storage_cell_byte(token, program, selector,
                                                       index, section, offset) :
        layerx_programs_call_storage_cell_byte(token, selector, index, section,
                                               offset);
}

static int resolution_cells_match(uint64_t token, bool catalog, uint32_t program,
                                  uint16_t selector,
                                  const lxp_programs_storage_cell *cells,
                                  uint32_t count)
{
    uint32_t index, offset, length;
    uint16_t section;
    const uint8_t *bytes;
    RESOLUTION_REQUIRE(resolution_cell_count(token, catalog, program, selector) ==
                       (lxp_result)count);
    for (index = 0U; index < count; ++index) {
        for (section = 0U; section < 2U; ++section) {
            bytes = section == 0U ? cells[index].key : cells[index].value;
            length = section == 0U ? cells[index].key_length :
                                     cells[index].value_length;
            RESOLUTION_REQUIRE(resolution_cell_length(token, catalog, program,
                selector, index, section) == (lxp_result)length);
            for (offset = 0U; offset < length; ++offset)
                RESOLUTION_REQUIRE(resolution_cell_byte(token, catalog, program,
                    selector, index, section, offset) == (lxp_result)bytes[offset]);
            RESOLUTION_REQUIRE(resolution_cell_byte(token, catalog, program,
                selector, index, section, length) == LXP_ERR_TRUNCATED);
        }
        RESOLUTION_REQUIRE(resolution_cell_length(token, catalog, program,
            selector, index, 2U) == LXP_ERR_UNKNOWN_FIELD);
    }
    RESOLUTION_REQUIRE(resolution_cell_length(token, catalog, program, selector,
                                              count, 0U) == LXP_ERR_UNKNOWN_FIELD);
    return 0;
}

static int resolution_artifact_blob(resolution_fixture *f, size_t which,
                                    uint8_t **bytes)
{
    const uint8_t *blob;
    size_t length;
    RESOLUTION_REQUIRE(lxp_ctx_blob_get(&f->ctx, f->code_hash[which], &blob,
                                        &length) == LXP_OK &&
                       length == f->wasm_length[which]);
    *bytes = (uint8_t *)(uintptr_t)blob;
    return 0;
}

static int resolution_withdraw(resolution_fixture *f, size_t which)
{
    uint8_t key[73];
    size_t key_length;
    const uint8_t *wasm;
    size_t wasm_length;
    (void)memcpy(key, "progcode", 8U);
    (void)memcpy(key + 8U, f->program[which], 32U);
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, 40U) == LXP_OK);
    RESOLUTION_REQUIRE(lxp_programs_artifact_open(&f->ctx, f->program[which],
        f->code_hash[which], &wasm, &wasm_length) == LXP_ERR_UNKNOWN_FIELD);
    (void)memset(key, 0, 10U);
    (void)memcpy(key, "interface", 9U);
    (void)memcpy(key + 10U, f->program[which], 32U);
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, 42U) == LXP_OK);
    resolution_storage_key(f, which, 0U, key, &key_length);
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, key_length) == LXP_OK);
    resolution_storage_key(f, which, 1U, key, &key_length);
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, key_length) == LXP_OK);
    return 0;
}

static int resolution_capture_interfaces(resolution_fixture *f)
{
    uint8_t key[42] = "interface";
    const uint8_t *stored;
    size_t which, stored_length;
    for (which = 0U; which < 2U; ++which) {
        (void)memcpy(key + 10U, f->program[which], 32U);
        RESOLUTION_REQUIRE(lxp_ctx_kv_get(&f->ctx, key, sizeof(key), &stored,
                                          &stored_length) == LXP_OK &&
                           stored_length > 72U &&
                           stored_length - 72U <= sizeof(f->interface[which]));
        f->interface_length[which] = stored_length - 72U;
        (void)memcpy(f->interface[which], stored + 72U, f->interface_length[which]);
    }
    return 0;
}

static int resolution_single_walk(resolution_fixture *f)
{
    static const uint8_t settled_key[] = "new";
    static const uint8_t settled_value[] = "ok";
    const lxp_programs_storage_cell settled = {settled_key, 3U, settled_value, 2U};
    void *decoded;
    uint64_t token;
    uint32_t first, second, offset;
    uint8_t *blob;
    if (resolution_open_call(f, 10U, &decoded, &token) != 0 ||
        resolution_capture_interfaces(f) != 0 ||
        resolution_catalog_index(token, f->program[0], &first) != 0 ||
        resolution_catalog_index(token, f->program[1], &second) != 0 ||
        resolution_artifact_blob(f, 1U, &blob) != 0) return 1;
    RESOLUTION_REQUIRE(first != second);
    blob[f->wasm_length[1] - 1U] ^= 1U;
    RESOLUTION_REQUIRE(layerx_programs_call_catalog_wasm_byte(token, second, 0U) ==
                           LXP_FATAL_INVARIANT &&
                       layerx_programs_call_catalog_wasm_byte(token, second, 1U) ==
                           LXP_FATAL_INVARIANT);
    blob[f->wasm_length[1] - 1U] ^= 1U;
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, false, 0U, 0U) ==
                       (lxp_result)f->wasm[0][0]);
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, true, first, 0U) ==
                       (lxp_result)f->wasm[0][0]);
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, true, second, 0U) ==
                       (lxp_result)f->wasm[1][0]);
    RESOLUTION_REQUIRE(layerx_programs_call_catalog_interface_length(token, first) ==
                       (lxp_result)f->interface_length[0]);
    RESOLUTION_REQUIRE(layerx_programs_call_catalog_interface_length(token, second) ==
                       (lxp_result)f->interface_length[1]);
    RESOLUTION_REQUIRE(resolution_cell_count(token, false, 0U, 0U) == 1 &&
                       resolution_cell_count(token, false, 0U, 1U) == 2 &&
                       resolution_cell_count(token, true, first, 1U) == 2 &&
                       resolution_cell_count(token, true, second, 0U) == 1 &&
                       resolution_cell_count(token, true, second, 1U) == 0);
    if (resolution_withdraw(f, 0U) != 0 || resolution_withdraw(f, 1U) != 0 ||
        resolution_wasm_matches(token, false, 0U, f->wasm[0],
                                f->wasm_length[0], 1U) != 0 ||
        resolution_wasm_matches(token, true, first, f->wasm[0],
                                f->wasm_length[0], 1U) != 0 ||
        resolution_wasm_matches(token, true, second, f->wasm[1],
                                f->wasm_length[1], 1U) != 0 ||
        resolution_interface_matches(token, first, f->interface[0],
                                     f->interface_length[0]) != 0 ||
        resolution_interface_matches(token, second, f->interface[1],
                                     f->interface_length[1]) != 0 ||
        resolution_cells_match(token, false, 0U, 0U, f->own, 1U) != 0 ||
        resolution_cells_match(token, false, 0U, 1U, f->shared, 2U) != 0 ||
        resolution_cells_match(token, true, first, 1U, f->shared, 2U) != 0 ||
        resolution_cells_match(token, true, second, 0U, f->foreign, 1U) != 0)
        return 1;
    RESOLUTION_REQUIRE(layerx_programs_call_storage_final_authorize(token) == LXP_OK);
    RESOLUTION_REQUIRE(resolution_cell_count(token, false, 0U, 1U) == 0 &&
                       resolution_cell_count(token, true, first, 1U) == 0 &&
                       resolution_cell_count(token, true, second, 0U) == 0);
    RESOLUTION_REQUIRE(layerx_programs_call_storage_final_begin(token, 1U, 1U) == LXP_OK &&
                       layerx_programs_call_storage_final_cell(token, 1U, 0U, 3U, 2U) ==
                           LXP_OK);
    for (offset = 0U; offset < 3U; ++offset)
        RESOLUTION_REQUIRE(layerx_programs_call_storage_final_byte(token, 1U, 0U, 0U,
            offset, settled_key[offset]) == LXP_OK);
    for (offset = 0U; offset < 2U; ++offset)
        RESOLUTION_REQUIRE(layerx_programs_call_storage_final_byte(token, 1U, 0U, 1U,
            offset, settled_value[offset]) == LXP_OK);
    RESOLUTION_REQUIRE(layerx_programs_call_storage_final_apply(token, 1U) == LXP_OK);
    if (resolution_cells_match(token, false, 0U, 1U, &settled, 1U) != 0 ||
        resolution_cells_match(token, true, first, 1U, &settled, 1U) != 0 ||
        resolution_wasm_matches(token, true, first, f->wasm[0],
                                f->wasm_length[0], 0U) != 0) return 1;
    resolution_ctx_rollback(f);
    return 0;
}

static int resolution_per_call(resolution_fixture *f)
{
    uint8_t key[40], manifest[LX_PROGRAMS_ARTIFACT_MANIFEST_BYTES];
    const uint8_t *stored;
    size_t stored_length;
    void *decoded;
    uint64_t token;
    uint32_t first;
    uint8_t *blob;
    if (resolution_open_call(f, 11U, &decoded, &token) != 0 ||
        resolution_catalog_index(token, f->program[0], &first) != 0 ||
        resolution_artifact_blob(f, 0U, &blob) != 0) return 1;
    blob[0] ^= 1U;
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, false, 0U, 0U) ==
                           LXP_FATAL_INVARIANT &&
                       resolution_wasm_byte(token, false, 0U, 1U) ==
                           LXP_FATAL_INVARIANT &&
                       resolution_wasm_byte(token, true, first, 0U) ==
                           LXP_FATAL_INVARIANT);
    blob[0] ^= 1U;
    (void)memcpy(key, "progcode", 8U);
    (void)memcpy(key + 8U, f->program[0], 32U);
    RESOLUTION_REQUIRE(lxp_ctx_kv_get(&f->ctx, key, sizeof(key), &stored,
                                      &stored_length) == LXP_OK &&
                       stored_length == sizeof(manifest));
    (void)memcpy(manifest, stored, sizeof(manifest));
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, sizeof(key)) == LXP_OK);
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, false, 0U, 0U) ==
                           LXP_ERR_UNKNOWN_FIELD &&
                       resolution_wasm_byte(token, true, first, 0U) ==
                           LXP_ERR_UNKNOWN_FIELD);
    RESOLUTION_REQUIRE(lxp_ctx_kv_put(&f->ctx, key, sizeof(key), manifest,
                                      sizeof(manifest)) == LXP_OK);
    RESOLUTION_REQUIRE(resolution_wasm_byte(token, false, 0U, 0U) ==
                           (lxp_result)f->wasm[0][0] &&
                       resolution_wasm_byte(token, true, first, 0U) ==
                           (lxp_result)f->wasm[0][0]);
    RESOLUTION_REQUIRE(lxp_ctx_kv_del(&f->ctx, key, sizeof(key)) == LXP_OK);
    if (resolution_wasm_matches(token, false, 0U, f->wasm[0],
                                f->wasm_length[0], 1U) != 0 ||
        resolution_wasm_matches(token, true, first, f->wasm[0],
                                f->wasm_length[0], 1U) != 0) return 1;
    resolution_ctx_rollback(f);
    return 0;
}

static int artifact_single_resolution_case(void)
{
    resolution_fixture *f = calloc(1U, sizeof(*f));
    uint64_t parameters = 1U;
    bool store_ready = false;
    int result = 1;
    if (f == NULL) return resolution_failed(__LINE__);
    f->arena_bytes = malloc(RESOLUTION_ARENA_BYTES);
    (void)memset(f->authority.principal, 0x42, 32U);
    (void)memset(f->authority.authority_hash, 0x55, 32U);
    if (f->arena_bytes == NULL ||
        lxp_arena_init(&f->arena, f->arena_bytes, RESOLUTION_ARENA_BYTES) != LXP_OK ||
        lxp_state_store_init(&f->store, 0U) != LXP_OK) {
        result = resolution_failed(__LINE__);
        goto done;
    }
    store_ready = true;
    if (lxp_kernel_create(&f->kernel, &f->store, &f->journal, &parameters, 0U) !=
            LXP_OK ||
        install_metering_v1(&f->kernel) != LXP_OK ||
        lxp_kernel_register_module(&f->kernel, programs_module_registration()) !=
            LXP_OK) {
        result = resolution_failed(__LINE__);
        goto done;
    }
    result = resolution_deploy(f, 0U) != 0 || resolution_deploy(f, 1U) != 0 ||
             resolution_seed_storage(f) != 0 || resolution_single_walk(f) != 0 ||
             resolution_per_call(f) != 0 ? 1 : 0;
done:
    resolution_ctx_rollback(f);
    if (store_ready) {
        while (f->kernel.blob_count != 0U)
            free(f->kernel.blobs[--f->kernel.blob_count].bytes);
        if (lxp_state_store_destroy(&f->store) != LXP_OK) result = 1;
    }
    free(f->arena_bytes);
    free(f);
    return result;
}

#undef RESOLUTION_REQUIRE

int main(int argc, char **argv)
{
    if (argc != 2) {
        (void)fprintf(stderr, "usage: %s web-reader.wasm\n", argv[0]);
        return 2;
    }
    if (artifact_single_resolution_case() != 0) return 1;
    if (deploy_and_upgrade_persist_exact_artifacts() != 0) return 1;
    if (deploy_and_upgrade_persist_exact_artifacts_version(
            LXP_PROTOCOL_VERSION_STATE_COMMITMENT) != 0) return 1;
    post_upgrade_batch_regression = true;
    if (deploy_and_upgrade_persist_exact_artifacts_version(
            LXP_PROTOCOL_VERSION_STATE_COMMITMENT) != 0) return 1;
    post_upgrade_batch_regression = false;
    if (program_state_observer_calls != 4U) return 1;
    if (web_program_path_main(argc, argv) != 0) return 1;
    return artifact_observer_calls == 2U && web_request_observer_calls == 2U ? 0 : 1;
}
