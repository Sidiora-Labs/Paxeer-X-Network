#define _POSIX_C_SOURCE 200809L
#include "lxp_daemon_finality_authority.h"
#include "../../cmd/layerx-guarantor/producer.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_paxeer.h"
#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

#define FAIL() do { (void)fprintf(stderr, "fixture failure at line %d\n", __LINE__); return 1; } while (0)

static uint8_t memory[512U * 1024U];
static lxp_daemon_evidence_store store;
static lxp_guarantor_cert certificate;
static lxp_guarantor_set bonded_set;
static lxp_finalisation_requirements requirements;
static lxp_daemon_settlement_registration_evidence registration;
static bool recording_mode;

static int log_bootstrap(void)
{
    char directory[] = "/tmp/lxp-bootstrap-log-XXXXXX";
    char path[128];
    const uint8_t body[] = {1U, 2U, 3U};
    uint8_t readback[3];
    lxp_log_record_header header;
    lxp_log log;
    struct stat metadata;
    uint64_t offset;
    unsigned mode;
    if (mkdtemp(directory) == NULL ||
        snprintf(path, sizeof(path), "%s/log", directory) < 0) FAIL();
    for (mode = 0U; mode < 2U; ++mode) {
        if (mode == 1U) {
            int fd = open(path, O_CREAT | O_EXCL | O_RDWR, 0600);
            if (fd < 0 || close(fd) != 0) FAIL();
        }
        if (lxp_log_open_or_create(&log, path, 4096U) != LXP_OK ||
            !log.has_durable_marker || log.capacity == 0U ||
            lxp_log_append(&log, LXP_LOG_ACTIVITY, 1U, body, sizeof(body), &offset) != LXP_OK ||
            lxp_log_append(&log, LXP_LOG_RECEIPT, 1U, body, sizeof(body), NULL) != LXP_OK ||
            lxp_log_sync(&log) != LXP_OK || lxp_log_close(&log) != LXP_OK ||
            lxp_log_open_or_create(&log, path, 16384U) != LXP_OK ||
            fstat(log.descriptor, &metadata) != 0 || metadata.st_size != 4096 ||
            lxp_log_recover(&log, NULL, NULL) != LXP_OK ||
            lxp_log_read(&log, offset, &header, readback, sizeof(readback)) != LXP_OK ||
            memcmp(body, readback, sizeof(body)) != 0 ||
            lxp_log_close(&log) != LXP_OK || unlink(path) != 0)
            FAIL();
    }
    return rmdir(directory) == 0 ? 0 : 1;
}

static int decode(const char *text, uint8_t *out, size_t length)
{
    size_t i;
    if (text == NULL) FAIL();
    if (strncmp(text, "0x", 2U) == 0) text += 2U;
    if (strlen(text) != length * 2U) FAIL();
    for (i = 0U; i < length; ++i) {
        unsigned value;
        if (sscanf(text + i * 2U, "%2x", &value) != 1) FAIL();
        out[i] = (uint8_t)value;
    }
    return 0;
}

static void hex(const uint8_t *bytes, size_t length)
{
    size_t i;
    (void)printf("0x");
    for (i = 0U; i < length; ++i) (void)printf("%02x", bytes[i]);
}

static int fixture(lxp_daemon_finality_authority *authority)
{
    static const char *const public_keys[2] = {
        "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
        "02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5"
    };
    static const uint8_t proof[] = {'P', 'R', 'O', 'O', 'F'};
    lxp_checkpoint_certificate checkpoint = {0};
    lxp_guarantor_attestation attestations[2];
    lxp_batch_header *header = &checkpoint.header;
    lxp_arena arena;
    size_t i;
    if (lxp_arena_init(&arena, memory, sizeof(memory)) != LXP_OK ||
        lxp_daemon_finality_authority_init(authority, &store) != LXP_OK)
        FAIL();
    header->protocol_version = 2U;
    header->network_id = 42U;
    header->epoch = 1U;
    header->batch_number = 1U;
    header->first_sequence = 1U;
    header->last_sequence = 1000000U;
    header->previous_state_root[0] = 0x11U;
    header->resulting_state_root[0] = 0x22U;
    header->activity_merkle_root[0] = 0x33U;
    header->receipt_merkle_root[0] = 0x44U;
    header->event_merkle_root[0] = 0x55U;
    header->data_availability_root[0] = 0x66U;
    header->oracle_root[0] = 0x77U;
    header->sequencer_id[0] = 0x88U;
    header->timestamp_ms = 1000000U;
    checkpoint.validity_proof = (lxp_byte_span){proof, sizeof(proof)};
    if (getenv("LAYERX_TEST_DA_HEADER_FILE") != NULL) {
        uint8_t canonical[LXP_BATCH_HEADER_ENCODED_SIZE];
        FILE *input = fopen(getenv("LAYERX_TEST_DA_HEADER_FILE"), "rb");
        if (input == NULL || fread(canonical, 1U, sizeof(canonical), input) != sizeof(canonical) ||
            fgetc(input) != EOF || fclose(input) != 0 ||
            lxp_batch_header_decode(canonical, sizeof(canonical), header) != LXP_OK) FAIL();
        checkpoint.validity_proof = (lxp_byte_span){NULL, 0U};
    }
    store.network_id = header->network_id;
    store.initialized = true;
    store.registry.finalisation.settlement_anchor[0] = 0x11U;
    if (lxp_guarantor_set_init(&bonded_set) != LXP_OK) FAIL();
    for (i = 0U; i < 2U; ++i) {
        lxp_guarantor_ctx guarantor = {0};
        lxp_guarantor_bond_state bond = {0};
        guarantor.guarantor_id[31] = (uint8_t)(i + 1U);
        guarantor.paxeer_private_key[31] = (uint8_t)(i + 1U);
        if (decode(public_keys[i], guarantor.paxeer_public_key, 33U) != 0)
            FAIL();
        guarantor.protocol_version = header->protocol_version;
        guarantor.network_id = header->network_id;
        guarantor.paxeer_chain_id = authority->paxeer_chain_id;
        (void)memcpy(guarantor.paxeer_settlement_contract,
                     authority->settlement_contract, 20U);
        guarantor.ready_to_sign = true;
        guarantor.possesses_availability = true;
        guarantor.bond_view.bonded = true;
        (void)memcpy(bond.guarantor_id, guarantor.guarantor_id, 32U);
        (void)memcpy(bond.public_key, guarantor.paxeer_public_key, 33U);
        bond.joined_epoch = 1U;
        bond.active = true;
        if (lxp_guarantor_set_apply(&bonded_set, i * 2U + 1U, true, &bond) != LXP_OK)
            FAIL();
        bond.bond_amount = (lxp_u128){0U, 1000U};
        if (lxp_guarantor_set_apply(&bonded_set, i * 2U + 2U, true, &bond) != LXP_OK ||
            lxp_guarantor_attest(&guarantor, &checkpoint, true, true,
                header->timestamp_ms + 1000U, &arena, &attestations[i]) != LXP_OK)
            FAIL();
    }
    if (getenv("LAYERX_TEST_DA_BONDED_SET_VERSION") != NULL) {
        const char *text = getenv("LAYERX_TEST_DA_BONDED_SET_VERSION");
        uint64_t version = 0U;
        if (getenv("LAYERX_TEST_DA_HEADER_FILE") == NULL || text[0] < '1' || text[0] > '9')
            FAIL();
        for (i = 0U; text[i] != '\0'; ++i) {
            unsigned digit;
            if (text[i] < '0' || text[i] > '9') FAIL();
            digit = (unsigned)(text[i] - '0');
            if (version > (UINT64_MAX - digit) / 10U) FAIL();
            version = version * 10U + digit;
        }
        if (version < bonded_set.version) FAIL();
        bonded_set.version = version;
    }
    if (lxp_guarantor_cert_assemble(&checkpoint, attestations, 2U, 2U,
                                    &certificate) != LXP_OK) FAIL();
    requirements.checkpoint_epoch = header->epoch;
    requirements.challenge_window_end_ms = header->timestamp_ms + 100U;
    requirements.checkpoint_deadline_ms = header->timestamp_ms + 2000U;
    requirements.now_ms = header->timestamp_ms + 1500U;
    requirements.threshold = 2U;
    requirements.minimum_bond = (lxp_u128){0U, 500U};
    requirements.availability_challenges_answered = true;
    registration.paxeer_chain_id = authority->paxeer_chain_id;
    (void)memcpy(registration.settlement_contract,
                 authority->settlement_contract, 20U);
    registration.observed_at_ms = header->timestamp_ms + 1500U;
    return lxp_checkpoint_certificate_hash(&checkpoint, &arena,
        registration.checkpoint_id) == LXP_OK ? 0 : 1;
}

static void prepare(void)
{
    const lxp_batch_header *h = &certificate.checkpoint.header;
    const uint8_t *roots[9] = {h->previous_state_root, h->resulting_state_root,
        h->activity_merkle_root, h->receipt_merkle_root, h->event_merkle_root,
        h->data_availability_root, h->oracle_root, h->sequencer_id,
        registration.checkpoint_id};
    size_t i;
    (void)printf("{\"header\":\"(%u,%u,%" PRIu64 ",%" PRIu64 ",%" PRIu64 ",%" PRIu64,
        (unsigned)h->protocol_version, h->network_id, h->epoch, h->batch_number,
        h->first_sequence, h->last_sequence);
    for (i = 0U; i < 7U; ++i) { (void)printf(","); hex(roots[i], 32U); }
    (void)printf(",%" PRIu64 ",", h->timestamp_ms); hex(roots[7], 32U);
    (void)printf(")\",\"checkpoint_id\":\""); hex(roots[8], 32U);
    (void)printf("\",\"attestations\":\"[");
    for (i = 0U; i < certificate.attestation_count; ++i) {
        const lxp_guarantor_attestation *a = &certificate.attestations[i];
        (void)printf("%s(%u,%u,%" PRIu64 ",", i == 0U ? "" : ",",
            (unsigned)a->protocol_version, a->network_id, a->paxeer_chain_id);
        hex(a->paxeer_settlement_contract, 20U);
        (void)printf(",%" PRIu64 ",", a->epoch); hex(a->checkpoint_id, 32U);
        (void)printf(","); hex(a->checkpoint_hash, 32U);
        (void)printf(","); hex(a->guarantor_id, 32U);
        (void)printf(",%" PRIu64 ",", a->batch_number); hex(a->data_availability_root, 32U);
        (void)printf(",true,true,%u,%" PRIu64 ",", (unsigned)a->availability_class_mask, a->attested_at_ms); hex(a->signer, 20U);
        (void)printf(","); hex(a->signature, 32U);
        (void)printf(","); hex(a->signature + 32U, 32U);
        (void)printf(",%u)", (unsigned)a->signature_v);
    }
    (void)printf("]\"}\n");
}

static int attestations_file(const char *directory, bool writing)
{
    for (size_t i = 0U; i < certificate.attestation_count; ++i) {
        uint8_t canonical[GP_ATTESTATION_BYTES], original[GP_ATTESTATION_BYTES];
        lxp_guarantor_attestation retained;
        struct stat metadata;
        char path[1024];
        FILE *file;
        int length = snprintf(path, sizeof(path), "%s/guarantor-%zu.attestation", directory, i + 1U);
        if (length < 0 || (size_t)length >= sizeof(path) ||
            gp_attestation_encode(&certificate.attestations[i], canonical) != LXP_OK) FAIL();
        file = fopen(path, writing ? "wbx" : "rb");
        if (file == NULL) FAIL();
        if (writing) {
            if (fwrite(canonical, 1U, sizeof(canonical), file) != sizeof(canonical) ||
                fclose(file) != 0) FAIL();
        } else {
            if (fstat(fileno(file), &metadata) != 0 || !S_ISREG(metadata.st_mode) ||
                fread(original, 1U, sizeof(original), file) != sizeof(original) ||
                fgetc(file) != EOF || fclose(file) != 0 ||
                memcmp(original, canonical, 209U) != 0 ||
                gp_attestation_decode(original, sizeof(original), &retained) != LXP_OK ||
                lxp_guarantor_attestation_verify(&retained, bonded_set.records[i].public_key) != LXP_OK)
                FAIL();
            certificate.attestations[i] = retained;
        }
    }
    return 0;
}

static int read_file(const char *path, uint8_t **out, size_t *length, size_t limit)
{
    struct stat metadata;
    FILE *input = fopen(path, "rb");
    if (input == NULL || fstat(fileno(input), &metadata) != 0 || !S_ISREG(metadata.st_mode) ||
        metadata.st_size <= 0 || (uint64_t)metadata.st_size > limit) { if (input != NULL) (void)fclose(input); FAIL(); }
    *length = (size_t)metadata.st_size;
    *out = malloc(*length);
    if (*out == NULL || fread(*out, 1U, *length, input) != *length || fgetc(input) != EOF || fclose(input) != 0) FAIL();
    return 0;
}

static int records(const char *path)
{
    lxp_log log;
    lxp_log_record_header header;
    uint64_t offset = 0U, valid_end = 0U, last = 0U, next = 0U;
    uint8_t *body = malloc(LXP_DAEMON_FINALITY_REGISTER_MAX_BYTES + 65536U);
    bool first = true;
    if (body == NULL || lxp_log_open_readonly(&log, path) != LXP_OK) FAIL();
    if (lxp_log_scan_tail(&log, &valid_end, &last, &next) != LXP_OK && valid_end == 0U) FAIL();
    (void)printf("{\"valid_end\":%" PRIu64 ",\"records\":[", valid_end);
    while (offset < valid_end) {
        if (lxp_log_read(&log, offset, &header, body, LXP_DAEMON_FINALITY_REGISTER_MAX_BYTES + 65536U) != LXP_OK) FAIL();
        (void)printf("%s{\"offset\":%" PRIu64 ",\"kind\":%u,\"evidence_kind\":%d,\"length\":%" PRIu32 ",\"sequence\":%" PRIu64 "}",
            first ? "" : ",", offset, (unsigned)header.record_kind,
            header.body_length >= 6U ? (int)body[5] : -1, header.body_length, header.global_sequence);
        first = false;
        offset += LXP_LOG_HEADER_BYTES + header.body_length;
    }
    (void)printf("]}\n");
    free(body);
    return lxp_log_close(&log) == LXP_OK && offset == valid_end ? 0 : 1;
}

static struct {
    lxp_guarantor_cert certificate;
    lxp_guarantor_set bonded_set;
    lxp_finalisation_requirements requirements;
    lxp_daemon_settlement_registration_evidence registration;
    bool seen;
} actual;

static lxp_result capture_history(void *context, const lxp_guarantor_cert *candidate,
    const lxp_guarantor_set *set, const lxp_finalisation_requirements *required,
    const lxp_daemon_settlement_registration_evidence *registered)
{
    actual.certificate = *candidate;
    actual.bonded_set = *set;
    actual.requirements = *required;
    actual.registration = *registered;
    actual.seen = true;
    return lxp_finality_authority_verify_history(context, candidate, set, required, registered);
}

static int refused(lxp_daemon_finality_authority *authority, const char *name, lxp_result expected)
{
    lxp_finalisation_state before = store.registry.finalisation;
    lxp_result status = lxp_finality_authority_verify_history(authority, &certificate, &bonded_set,
        &requirements, &registration);
    if (status == LXP_OK || (expected != LXP_OK && status != expected) ||
        memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) {
        (void)fprintf(stderr, "%s: unexpected status %d or mutated frontier\n", name, (int)status);
        FAIL();
    }
    (void)printf("%s refused status=%d\n", name, (int)status);
    certificate = actual.certificate;
    bonded_set = actual.bonded_set;
    requirements = actual.requirements;
    registration = actual.registration;
    return 0;
}

static int recorded_contents_refused(lxp_daemon_finality_authority *authority,
    uint32_t network_id, lxp_byte_span payload, lxp_byte_span proof,
    lxp_byte_span header, const uint8_t checkpoint_id[32], lxp_arena *arena,
    const char *name)
{
    lxp_finalisation_state before = store.registry.finalisation;
    size_t mark = lxp_arena_mark(arena);
    lxp_result status = lxp_daemon_finality_contents_verify(network_id, payload,
        proof, header, checkpoint_id, lxp_finality_authority_verify_history,
        authority, arena);
    if (lxp_arena_reset(arena, mark) != LXP_OK || status == LXP_OK ||
        memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) {
        (void)fprintf(stderr, "%s: unexpected status %d or mutated frontier\n",
            name, (int)status);
        FAIL();
    }
    (void)printf("%s refused status=%d\n", name, (int)status);
    return 0;
}

/* Actual producer payloads: the guarantor's %020batch.checkpoint/.finality
 * files and its <checkpoint-id>.header go through the production finality
 * decoder and verifier against the live anchor. */
static int actual_payload(const char *state, const char *batch, const char *checkpoint_hex, bool admit)
{
    lxp_daemon_finality_authority authority;
    lxp_finalisation_state before;
    lxp_batch_header header;
    uint8_t checkpoint_id[32], *payload, *proof, *saved_header, *arena_memory;
    size_t payload_length, proof_length, header_length;
    char path[4096];
    lxp_arena arena;
    lxp_result status;
    lxp_result fresh_status;
    uint8_t payload_digest[32], proof_digest[32], header_digest[32];
    int failed = 0;
    if (decode(checkpoint_hex, checkpoint_id, 32U) != 0 ||
        snprintf(path, sizeof(path), "%s/%020llu.checkpoint", state, strtoull(batch, NULL, 10)) >= (int)sizeof(path) ||
        read_file(path, &payload, &payload_length, LXP_DAEMON_FINALITY_REGISTER_MAX_BYTES) != 0 ||
        snprintf(path, sizeof(path), "%s/%020llu.finality", state, strtoull(batch, NULL, 10)) >= (int)sizeof(path) ||
        read_file(path, &proof, &proof_length, LXP_DAEMON_FINALITY_REGISTER_MAX_BYTES) != 0 ||
        snprintf(path, sizeof(path), "%s/%s.header", state, checkpoint_hex + (strncmp(checkpoint_hex, "0x", 2U) == 0 ? 2U : 0U)) >= (int)sizeof(path) ||
        read_file(path, &saved_header, &header_length, LXP_BATCH_HEADER_ENCODED_SIZE + 64U) != 0 ||
        header_length != LXP_BATCH_HEADER_ENCODED_SIZE + 64U ||
        lxp_batch_header_decode(saved_header, LXP_BATCH_HEADER_ENCODED_SIZE, &header) != LXP_OK) FAIL();
    status = lxp_finality_authority_bind(&authority, &store);
    if (status != LXP_OK) { (void)fprintf(stderr, "finality authority bind refused: %d\n", (int)status); FAIL(); }
    /* The trusted predecessor anchor is the settlement root the checkpoint extends. */
    (void)memcpy(store.registry.finalisation.settlement_anchor, header.previous_state_root, 32U);
    arena_memory = malloc(4U * LXP_MAX_VALIDITY_PROOF_BYTES);
    if (arena_memory == NULL || lxp_arena_init(&arena, arena_memory, 4U * LXP_MAX_VALIDITY_PROOF_BYTES) != LXP_OK) FAIL();
    before = store.registry.finalisation;
    status = lxp_daemon_finality_contents_verify(header.network_id, (lxp_byte_span){payload, payload_length},
        (lxp_byte_span){proof, proof_length}, (lxp_byte_span){saved_header, LXP_BATCH_HEADER_ENCODED_SIZE},
        checkpoint_id, capture_history, &authority, &arena);
    if (status != LXP_OK || !actual.seen || memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) {
        (void)fprintf(stderr, "historical recovery refused: %d\n", (int)status);
        FAIL();
    }
    if (recording_mode &&
        (lxp_hash_sha256(payload, payload_length, payload_digest) != LXP_OK ||
         lxp_hash_sha256(proof, proof_length, proof_digest) != LXP_OK ||
         lxp_hash_sha256(saved_header, header_length, header_digest) != LXP_OK)) FAIL();
    if (lxp_ct_is_zero(actual.registration.observed_block_hash, 32U) || proof[0] != 0U || proof[1] != 2U) FAIL();
    (void)printf("historical recovery passed set_version=%" PRIu64 " block=%" PRIu64 " guarantors=%zu threshold=%u\n",
        actual.bonded_set.version, actual.registration.observed_block_number,
        actual.certificate.attestation_count, (unsigned)actual.certificate.threshold);
    status = lxp_daemon_finality_contents_verify(header.network_id, (lxp_byte_span){payload, payload_length},
        (lxp_byte_span){proof, proof_length}, (lxp_byte_span){saved_header, LXP_BATCH_HEADER_ENCODED_SIZE},
        checkpoint_id, lxp_finality_authority_verify, &authority, &arena);
    fresh_status = status;
    if ((admit && status != LXP_OK) || (!admit && status == LXP_OK) ||
        memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) {
        (void)fprintf(stderr, "fresh admission: unexpected status %d\n", (int)status);
        FAIL();
    }
    (void)printf("fresh admission %s status=%d\n", admit ? "passed" : "refused", (int)status);
    proof[1] = 1U;
    status = lxp_daemon_finality_contents_verify(header.network_id,
        (lxp_byte_span){payload, payload_length}, (lxp_byte_span){proof, proof_length - 32U},
        (lxp_byte_span){saved_header, LXP_BATCH_HEADER_ENCODED_SIZE}, checkpoint_id,
        lxp_finality_authority_verify_history, &authority, &arena);
    if (status != LXP_OK || memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) FAIL();
    (void)printf("legacy historical recovery passed\n");
    status = lxp_daemon_finality_contents_verify(header.network_id,
        (lxp_byte_span){payload, payload_length}, (lxp_byte_span){proof, proof_length - 32U},
        (lxp_byte_span){saved_header, LXP_BATCH_HEADER_ENCODED_SIZE}, checkpoint_id,
        lxp_finality_authority_verify, &authority, &arena);
    proof[1] = 2U;
    if (status == LXP_OK || memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) FAIL();
    (void)printf("legacy fresh admission refused status=%d\n", (int)status);
    for (unsigned boundary = 0U; boundary < 3U; ++boundary) {
        const char *name = boundary == 0U ? "unknown proof version" :
            boundary == 1U ? "truncated historical context" : "zero historical block hash";
        uint8_t saved_hash[32];
        size_t length = proof_length;
        (void)memcpy(saved_hash, proof + proof_length - 32U, 32U);
        if (boundary == 0U) proof[1] = 3U;
        else if (boundary == 1U) --length;
        else (void)memset(proof + proof_length - 32U, 0, 32U);
        status = lxp_daemon_finality_contents_verify(header.network_id,
            (lxp_byte_span){payload, payload_length}, (lxp_byte_span){proof, length},
            (lxp_byte_span){saved_header, LXP_BATCH_HEADER_ENCODED_SIZE}, checkpoint_id,
            lxp_finality_authority_verify_history, &authority, &arena);
        proof[1] = 2U;
        (void)memcpy(proof + proof_length - 32U, saved_hash, 32U);
        if (status == LXP_OK || memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) FAIL();
        (void)printf("%s refused status=%d\n", name, (int)status);
    }
    certificate = actual.certificate;
    bonded_set = actual.bonded_set;
    requirements = actual.requirements;
    registration = actual.registration;
    if (recording_mode) {
        lxp_byte_span payload_span = {payload, payload_length};
        lxp_byte_span proof_span = {proof, proof_length};
        lxp_byte_span header_span = {saved_header, LXP_BATCH_HEADER_ENCODED_SIZE};
        uint8_t original_byte;
        if (payload_length < 2U || proof_length < 3U) FAIL();
        failed |= recorded_contents_refused(&authority, header.network_id,
            (lxp_byte_span){payload, payload_length - 1U}, proof_span,
            header_span, checkpoint_id, &arena, "truncated checkpoint");
        failed |= recorded_contents_refused(&authority, header.network_id,
            payload_span, (lxp_byte_span){proof, proof_length - 1U},
            header_span, checkpoint_id, &arena, "truncated proof");
        original_byte = payload[0]; payload[0] ^= 0x80U;
        failed |= recorded_contents_refused(&authority, header.network_id,
            payload_span, proof_span, header_span, checkpoint_id, &arena,
            "corrupted checkpoint");
        payload[0] = original_byte;
        original_byte = proof[0]; proof[0] ^= 0x80U;
        failed |= recorded_contents_refused(&authority, header.network_id,
            payload_span, proof_span, header_span, checkpoint_id, &arena,
            "corrupted proof");
        proof[0] = original_byte;
        failed |= recorded_contents_refused(&authority, header.network_id ^ 1U,
            payload_span, proof_span, header_span, checkpoint_id, &arena,
            "wrong payload network");
        certificate.attestations[0].guarantor_id[0] ^= 1U;
        failed |= refused(&authority, "unknown signer", LXP_OK);
    }
    registration.observed_block_hash[0] ^= 1U;
    failed |= refused(&authority, "wrong block hash", LXP_ERR_CONTEXT_MISMATCH);
    ++registration.observed_block_number;
    failed |= refused(&authority, "wrong block", LXP_OK);
    --registration.observed_block_number;
    failed |= refused(&authority, "earlier block", LXP_OK);
    ++bonded_set.version;
    failed |= refused(&authority, "wrong set version", LXP_ERR_CONTEXT_MISMATCH);
    --bonded_set.version;
    failed |= refused(&authority, "older set version", LXP_ERR_CONTEXT_MISMATCH);
    bonded_set.records[0].public_key[32] ^= 1U;
    failed |= refused(&authority, "wrong membership proof", LXP_OK);
    certificate.attestation_count = 1U;
    failed |= refused(&authority, "short signer list", LXP_OK);
    ++registration.paxeer_chain_id;
    failed |= refused(&authority, "wrong chain", LXP_ERR_CONTEXT_MISMATCH);
    registration.settlement_contract[19] ^= 1U;
    failed |= refused(&authority, "wrong anchor domain", LXP_ERR_CONTEXT_MISMATCH);
    certificate.threshold = 1U;
    requirements.threshold = 1U;
    failed |= refused(&authority, "wrong certificate threshold", LXP_ERR_CONTEXT_MISMATCH);
    certificate.attestations[0].signature[0] ^= 1U;
    failed |= refused(&authority, "wrong certificate signature", LXP_OK);
    registration.transaction_id[0] ^= 1U;
    failed |= refused(&authority, "wrong receipt", LXP_OK);
    certificate.checkpoint.header.resulting_state_root[0] ^= 1U;
    failed |= refused(&authority, "wrong checkpoint root", LXP_OK);
    registration.checkpoint_id[0] ^= 1U;
    failed |= refused(&authority, "wrong checkpoint id", LXP_OK);
    authority.rpc_port = 1U;
    failed |= refused(&authority, "unavailable history", LXP_ERR_IO);
    if (recording_mode && failed == 0) {
        if (memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) FAIL();
        (void)printf("FINALITY_RECORDINGS_CASES {\"version\":1,\"batch\":%" PRIu64
            ",\"network_id\":%u,\"paxeer_chain_id\":%" PRIu64 ",\"observed_block\":%" PRIu64
            ",\"bonded_set_version\":%" PRIu64 ",\"historical_status\":0,\"fresh_status\":%d,"
            "\"frontier_unchanged\":true,\"cases\":{\"finalized-history\":true,\"short-signer-list\":true,"
            "\"unknown-signer\":true,\"altered-checkpoint\":true,\"altered-receipt\":true,\"wrong-domain\":true,"
            "\"truncated-proof\":true,\"corrupted-proof\":true,\"truncated-checkpoint\":true,"
            "\"corrupted-checkpoint\":true,\"wrong-network\":true},\"checkpoint_id\":\"",
            header.batch_number, header.network_id, authority.paxeer_chain_id, actual.registration.observed_block_number,
            actual.bonded_set.version, (int)fresh_status);
        hex(checkpoint_id, 32U);
        (void)printf("\",\"observed_block_hash\":\""); hex(actual.registration.observed_block_hash, 32U);
        (void)printf("\",\"payload_sha256\":\""); hex(payload_digest, 32U);
        (void)printf("\",\"proof_sha256\":\""); hex(proof_digest, 32U);
        (void)printf("\",\"header_sha256\":\""); hex(header_digest, 32U);
        (void)printf("\",\"settlement_anchor\":\""); hex(before.settlement_anchor, 32U);
        (void)printf("\",\"settlement_contract\":\""); hex(authority.settlement_contract, 20U);
        (void)printf("\"}\n");
    }
    free(arena_memory); free(saved_header); free(proof); free(payload);
    return failed;
}

static int check(lxp_daemon_finality_authority *authority, const char *name,
                  bool success, bool unavailable)
{
    lxp_finalisation_state before = store.registry.finalisation;
    lxp_result status = lxp_daemon_finality_authority_verify(authority,
        &certificate, &bonded_set, &requirements, &registration);
    if ((success && status != LXP_OK) || (!success && status == LXP_OK) ||
        (unavailable && status != LXP_ERR_IO) ||
        memcmp(&before, &store.registry.finalisation, sizeof(before)) != 0) {
        (void)fprintf(stderr, "%s: unexpected status %d or mutated store\n", name, (int)status);
        FAIL();
    }
    (void)printf("%s passed\n", name);
    return 0;
}

int main(int argc, char **argv)
{
    lxp_daemon_finality_authority authority;
    lxp_daemon_settlement_registration_evidence original;
    int failed = 0;
    if (argc == 2 && strcmp(argv[1], "bind") == 0) {
        lxp_daemon_finality_authority unbound;
        lxp_result status = lxp_finality_authority_bind(&authority, &store);
        if (status != LXP_OK) {
            (void)fprintf(stderr, "finality authority bind refused: %d\n", (int)status);
            return 3;
        }
        (void)memset(&unbound, 0, sizeof(unbound));
        if (authority.store != &store || authority.threshold == 0U || authority.rpc_port == 0U ||
            authority.finalized_exists != (authority.finalized_batch != 0U) ||
            (authority.finalized_exists && authority.finalized_guarantor_count < authority.threshold) ||
            lxp_ct_memcmp(authority.settlement_contract, lxp_paxeer_anchor_address, 20U) != 0 ||
            certificate.attestation_count != 0U ||
            lxp_finality_authority_verify(&authority, &certificate, &bonded_set, &requirements, &registration) != LXP_ERR_ATTESTATION_THRESHOLD ||
            lxp_finality_authority_verify(&unbound, &certificate, &bonded_set, &requirements, &registration) != LXP_ERR_NON_CANONICAL) FAIL();
        (void)printf("{\"chain_id\":%" PRIu64 ",\"threshold\":%" PRIu32 ",\"finalized_exists\":%s,\"finalized_batch\":%" PRIu64 ",\"finalized_guarantors\":%zu}\n",
            authority.paxeer_chain_id, authority.threshold, authority.finalized_exists ? "true" : "false",
            authority.finalized_batch, authority.finalized_guarantor_count);
        return 0;
    }
    if (argc == 3 && strcmp(argv[1], "records") == 0) return records(argv[2]);
    if (argc == 6 && strcmp(argv[1], "recorded") == 0 &&
        (strcmp(argv[5], "admit") == 0 || strcmp(argv[5], "refuse") == 0)) {
        recording_mode = true;
        return actual_payload(argv[2], argv[3], argv[4], strcmp(argv[5], "admit") == 0);
    }
    if (argc == 6 && strcmp(argv[1], "actual") == 0 &&
        (strcmp(argv[5], "admit") == 0 || strcmp(argv[5], "refuse") == 0))
        return actual_payload(argv[2], argv[3], argv[4], strcmp(argv[5], "admit") == 0);
    if (log_bootstrap() != 0 || fixture(&authority) != 0) FAIL();
    if (argc == 2 && strcmp(argv[1], "prepare") == 0) { prepare(); return 0; }
    if (argc == 3 && strcmp(argv[1], "prepare") == 0) {
        if (attestations_file(argv[2], true) != 0) FAIL();
        prepare();
        return 0;
    }
    if (argc == 6 && strcmp(argv[1], "emit") == 0) {
        lxp_arena arena;
        lxp_byte_span payload, proof;
        FILE *output;
        char path[1024];
        if (attestations_file(argv[5], false) != 0 ||
            decode(argv[2], registration.transaction_id, 32U) != 0) FAIL();
        registration.observed_block_number = strtoull(argv[3], NULL, 10);
        registration.observed_at_ms = strtoull(argv[4], NULL, 10);
        if (lxp_arena_init(&arena, memory, sizeof(memory)) != LXP_OK ||
            lxp_daemon_finality_evidence_encode(&certificate, &bonded_set, &requirements,
                0U, &registration, &arena, &payload, &proof) != LXP_OK) FAIL();
        for (size_t i = 0U; i < 2U; ++i) {
            lxp_byte_span bytes = i == 0U ? payload : proof;
            int length = snprintf(path, sizeof(path), "%s/%s", argv[5],
                i == 0U ? "checkpoint.bin" : "finality.bin");
            if (length < 0 || (size_t)length >= sizeof(path)) FAIL();
            output = fopen(path, "wb");
            if (output == NULL || fwrite(bytes.bytes, 1U, bytes.length, output) != bytes.length ||
                fclose(output) != 0) FAIL();
        }
        return 0;
    }
    if (argc == 5 && strcmp(argv[1], "recover") == 0) {
        lxp_finalisation_state before = store.registry.finalisation;
        lxp_result history, admission;
        if (attestations_file(argv[4], false) != 0 ||
            decode(argv[2], registration.transaction_id, 32U) != 0) FAIL();
        registration.observed_block_number = strtoull(argv[3], NULL, 10);
        history = lxp_finality_authority_verify_history(&authority, &certificate, &bonded_set, &requirements, &registration);
        authority.threshold = (uint32_t)certificate.threshold;
        admission = lxp_finality_authority_verify(&authority, &certificate, &bonded_set, &requirements, &registration);
        (void)printf("{\"history\":%d,\"admission\":%d,\"frontier_unchanged\":%s}\n", (int)history, (int)admission,
            memcmp(&before, &store.registry.finalisation, sizeof(before)) == 0 ? "true" : "false");
        return 0;
    }
    if (argc != 6 || strcmp(argv[1], "verify") != 0 ||
        decode(argv[2], registration.transaction_id, 32U) != 0) FAIL();
    registration.observed_block_number = strtoull(argv[3], NULL, 10);
    original = registration;
    failed |= check(&authority, "registered checkpoint", true, false);
    ++registration.paxeer_chain_id;
    failed |= check(&authority, "wrong chain", false, false);
    registration = original;
    registration.settlement_contract[0] ^= 1U;
    failed |= check(&authority, "wrong settlement", false, false);
    registration = original;
    registration.checkpoint_id[0] ^= 1U;
    failed |= check(&authority, "wrong checkpoint", false, false);
    registration = original;
    ++registration.observed_block_number;
    failed |= check(&authority, "wrong block", false, false);
    registration = original;
    certificate.attestations[0].signature[0] ^= 1U;
    failed |= check(&authority, "invalid signature", false, false);
    certificate.attestations[0].signature[0] ^= 1U;
    authority.checkpoint_registry[0] ^= 1U;
    failed |= check(&authority, "wrong registry", false, false);
    authority.checkpoint_registry[0] ^= 1U;
    if (decode(argv[4], registration.transaction_id, 32U) != 0) FAIL();
    registration.observed_block_number = strtoull(argv[5], NULL, 10);
    failed |= check(&authority, "reverted transaction", false, false);
    registration = original;
    authority.rpc_port = 1U;
    failed |= check(&authority, "unreachable chain", false, true);
    return failed;
}
