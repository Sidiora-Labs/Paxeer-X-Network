#define main program_admission_main
#include "lxp_test_program_admission.c"
#undef main
#include "layerx/lx_asset.h"
#include "layerx/lxp_ledger.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_merkle.h"
#include "layerx/lxp_bridge_credit.h"

static int fixture_signer(signer *key, const char *name)
{
    const char *directory = getenv("PAXEER_X_FIXTURE_KEYS");
    char path[PATH_MAX];
    REQUIRE(directory != NULL);
    REQUIRE(snprintf(path, sizeof(path), "%s/%s.seed", directory, name) > 0);
    FILE *file = fopen(path, "rb");
    REQUIRE(file != NULL && fread(key->private_key, 1U, 32U, file) == 32U);
    REQUIRE(fgetc(file) == EOF && fclose(file) == 0);
    EVP_PKEY *pkey = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, key->private_key, 32U);
    size_t length = 32U;
    REQUIRE(pkey != NULL && EVP_PKEY_get_raw_public_key(pkey, key->public_key, &length) == 1 && length == 32U);
    EVP_PKEY_free(pkey);
    return 0;
}

static void print_hex(const uint8_t *bytes, size_t length)
{
    for (size_t i = 0U; i < length; ++i) printf("%02x", bytes[i]);
}

static void actor_did(const signer *key, uint8_t did[76])
{
    static const char digits[] = "0123456789abcdef";
    memcpy(did, "did:layerx:", 11U);
    for (size_t i = 0U; i < 32U; ++i) {
        did[11U + i * 2U] = (uint8_t)digits[key->public_key[i] >> 4U];
        did[12U + i * 2U] = (uint8_t)digits[key->public_key[i] & 15U];
    }
    did[75] = 0U;
}

static int account_id(const uint8_t did[76], const uint8_t asset[32], uint8_t id[32])
{
    static const char digits[] = "0123456789abcdef";
    char hex[65], name[160];
    for (size_t i = 0U; i < 32U; ++i) {
        hex[i * 2U] = digits[asset[i] >> 4U];
        hex[i * 2U + 1U] = digits[asset[i] & 15U];
    }
    hex[64] = 0;
    int length = snprintf(name, sizeof(name), "agent:%s:asset:%s", did, hex);
    REQUIRE(length > 0 && (size_t)length < sizeof(name));
    REQUIRE(lx_account_id_from_string(
        (const uint8_t *)name, (size_t)length, id) == LXP_OK);
    return 0;
}

static int read_state(int descriptor, uint16_t tag, const uint8_t *query, size_t length)
{
    wire_envelope response;
    REQUIRE(send_request(descriptor, 5U, tag, tag, query, length) == 0);
    REQUIRE(receive_envelope(descriptor, &response) == 0);
    if (response.tag == 25U && response.payload_length == 5U)
        fprintf(stderr, "read tag=%u refusal=%d\n", tag, (int32_t)load_u32(response.payload + 1U));
    REQUIRE(response.tag == tag + 1U && response.correlation_id == tag);
    printf("read tag=%u payload=", response.tag);
    print_hex(response.payload, response.payload_length);
    printf("\n");
    release_envelope(&response);
    return 0;
}

static int submit_pay(int descriptor, const signer *key, uint64_t sequence,
    uint16_t ordinal, const uint8_t *payload, size_t payload_length, bool wait)
{
    uint8_t encoded[ACTIVITY_CAPACITY], id[32], query[34] = {1U};
    static uint8_t storage[2U * LXP_MAX_ACTIVITY_BYTES];
    size_t length;
    signer sequencer;
    lxp_arena arena;
    wire_envelope response;
    REQUIRE(build_activity(key, sequence, ((uint32_t)1U << 16U) | ordinal,
        0U, payload, payload_length, encoded, sizeof(encoded), &length) == 0);
    REQUIRE(lxp_activity_id(encoded, length, id) == LXP_OK);
    REQUIRE(send_request(descriptor, 5U, 3U, sequence + 1U, encoded, length) == 0);
    REQUIRE(expect_ack(descriptor, sequence + 1U, encoded, length, id) == 0);
    memcpy(query + 1U, id, 32U); query[33] = 1U;
    REQUIRE(fixture_signer(&sequencer, "sequencer") == 0);
    for (unsigned attempt = 0U; attempt < 200U; ++attempt) {
        REQUIRE(send_request(descriptor, 5U, 5U, sequence + 1U, query, wait ? 34U : 33U) == 0);
        REQUIRE(receive_envelope(descriptor, &response) == 0);
        REQUIRE(response.tag == 6U);
        if (response.payload_length != 0U) {
            lxp_receipt receipt;
            REQUIRE(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
            REQUIRE(lxp_receipt_decode(response.payload, response.payload_length, true, &receipt) == LXP_OK);
            REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
            REQUIRE(memcmp(receipt.activity_id, id, 32U) == 0);
            printf("receipt ordinal=%u sequence=%llu result=%d root=", ordinal,
                (unsigned long long)receipt.global_sequence, (int)receipt.result_code);
            print_hex(receipt.resulting_state_root, 32U); printf(" id=");
            print_hex(id, 32U); printf(" batch="); print_hex(receipt.batch_id, 32U);
            uint8_t receipt_digest[32];
            REQUIRE(lxp_receipt_digest(&receipt, &arena, receipt_digest) == LXP_OK);
            printf(" digest="); print_hex(receipt_digest, 32U); printf(" raw="); print_hex(response.payload, response.payload_length); printf("\n");
            REQUIRE(receipt.result_code == LXP_OK);
            release_envelope(&response);
            return 0;
        }
        release_envelope(&response);
        REQUIRE(!wait);
        const struct timespec delay = {0, 50000000};
        REQUIRE(nanosleep(&delay, NULL) == 0);
    }
    return 1;
}

static int evidence_file(const char *directory, const char *name, uint8_t *bytes, size_t capacity, size_t *length)
{
    char path[PATH_MAX];
    REQUIRE(snprintf(path, sizeof(path), "%s/%s", directory, name) > 0);
    FILE *file = fopen(path, "rb");
    REQUIRE(file != NULL);
    *length = fread(bytes, 1U, capacity, file);
    REQUIRE(!ferror(file) && fgetc(file) == EOF && fclose(file) == 0);
    return 0;
}

static int verify_replica(const char *directory)
{
    uint8_t header_bytes[354], signature[64], proof_bytes[1041], receipt_bytes[ACTIVITY_CAPACITY * 4U];
    uint8_t leaf[32];
    size_t header_length, signature_length, proof_length, receipt_length;
    static uint8_t scratch[2U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_batch_header header;
    lxp_receipt receipt;
    lxp_merkle_proof proof = {0};
    lxp_sequencer_authorization authorization = {0};
    signer sequencer;
    REQUIRE(evidence_file(directory, "header", header_bytes, sizeof(header_bytes), &header_length) == 0);
    REQUIRE(evidence_file(directory, "signature", signature, sizeof(signature), &signature_length) == 0);
    REQUIRE(evidence_file(directory, "proof", proof_bytes, sizeof(proof_bytes), &proof_length) == 0);
    REQUIRE(evidence_file(directory, "receipt", receipt_bytes, sizeof(receipt_bytes), &receipt_length) == 0);
    REQUIRE(fixture_signer(&sequencer, "sequencer") == 0);
    REQUIRE(lxp_arena_init(&arena, scratch, sizeof(scratch)) == LXP_OK);
    REQUIRE(lxp_receipt_decode(receipt_bytes, receipt_length, true, &receipt) == LXP_OK);
    REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
    REQUIRE(lxp_batch_header_decode(header_bytes, header_length, &header) == LXP_OK);
    REQUIRE(header.network_id == NETWORK_ID && header.protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT);
    REQUIRE(header.first_sequence <= receipt.global_sequence && receipt.global_sequence <= header.last_sequence);
    memcpy(authorization.public_key, sequencer.public_key, 32U);
    /* Bootstrap derives the sequencer identity from its lowercase public-key text. */
    char public_hex[65], identity_text[82];
    for (size_t i = 0; i < 32; ++i) (void)snprintf(public_hex + 2U * i, 3U, "%02x", sequencer.public_key[i]);
    REQUIRE(snprintf(identity_text, sizeof(identity_text), "layerx-sequencer:%s", public_hex) == 81);
    lxp_hash_context hash;
    lxp_hash_init(&hash);
    REQUIRE(lxp_hash_update(&hash, identity_text, 81U) == LXP_OK);
    REQUIRE(lxp_hash_final(&hash, authorization.sequencer_id) == LXP_OK);
    authorization.authorized = 1U;
    authorization.first_batch_number = 1U;
    authorization.last_batch_number = UINT64_C(1099511627776);
    REQUIRE(lxp_batch_verify_signature(&header, signature, signature_length, &authorization, &arena) == LXP_OK);
    REQUIRE(receipt.timestamp == header.timestamp_ms);
    REQUIRE(proof_length >= 17U && load_u16(proof_bytes) == 1U && load_u16(proof_bytes + 2U) == 0x4d50U);
    proof.leaf_index = load_u32(proof_bytes + 4U); proof.leaf_count = load_u32(proof_bytes + 8U); proof.depth = proof_bytes[12U];
    REQUIRE(proof.depth <= 32U && load_u32(proof_bytes + 13U) == (uint32_t)proof.depth * 32U);
    REQUIRE(proof_length == 17U + (size_t)proof.depth * 32U);
    memcpy(proof.siblings, proof_bytes + 17U, (size_t)proof.depth * 32U);
    REQUIRE(lxp_merkle_leaf_hash(receipt_bytes, receipt_length, leaf) == LXP_OK);
    REQUIRE(lxp_merkle_proof_verify(leaf, &proof, header.receipt_merkle_root) == LXP_OK);
    REQUIRE(proof.leaf_index == receipt.global_sequence - header.first_sequence);
    REQUIRE(proof.leaf_count == header.last_sequence - header.first_sequence + 1U);
    if (receipt.global_sequence == header.last_sequence)
        REQUIRE(memcmp(receipt.resulting_state_root, header.resulting_state_root, 32U) == 0);
    puts("authenticated replica signed header and receipt inclusion verified");
    return 0;
}


static int program_evidence_write(const char *directory, const char *name,
    const uint8_t *bytes, size_t length)
{
    char path[PATH_MAX];
    struct stat info;
    REQUIRE(stat(directory, &info) == 0 && S_ISDIR(info.st_mode));
    REQUIRE((info.st_mode & 077U) == 0U);
    int count = snprintf(path, sizeof(path), "%s/%s", directory, name);
    REQUIRE(count > 0 && (size_t)count < sizeof(path));
    int file = open(path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    REQUIRE(file >= 0);
    int status = descriptor_write_all(file, bytes, length);
    if (status == 0 && fsync(file) != 0) status = 1;
    if (close(file) != 0) status = 1;
    REQUIRE(status == 0);
    return 0;
}

static int program_payload_read(const char *path, uint8_t *bytes,
    size_t capacity, size_t *length)
{
    int file = open(path, O_RDONLY | O_NOFOLLOW);
    struct stat info;
    REQUIRE(file >= 0 && fstat(file, &info) == 0 && S_ISREG(info.st_mode));
    REQUIRE(info.st_size > 0 && (uint64_t)info.st_size <= capacity);
    *length = (size_t)info.st_size;
    size_t offset = 0U;
    while (offset < *length) {
        ssize_t count = read(file, bytes + offset, *length - offset);
        if (count < 0 && errno == EINTR) continue;
        REQUIRE(count > 0);
        offset += (size_t)count;
    }
    uint8_t tail;
    REQUIRE(read(file, &tail, 1U) == 0 && close(file) == 0);
    return 0;
}


static int program_envelope_refusal(char **argv)
{
    static uint8_t payload[2U * LXP_MAX_ACTIVITY_BYTES];
    static uint8_t scratch[LXP_MAX_ACTIVITY_BYTES];
    signer actor;
    uint8_t did[76], digest[32], preimage[32];
    size_t length;
    lxp_activity activity = {0};
    lxp_arena arena;
    lxp_byte_span encoded;
    REQUIRE(strcmp(argv[5], "wait") == 0 && strcmp(argv[7], "encoding:-105") == 0);
    REQUIRE(program_payload_read(argv[6], payload, sizeof(payload), &length) == 0);
    REQUIRE(length > LXP_MAX_PAYLOAD_BYTES);
    REQUIRE(fixture_signer(&actor, "treasury") == 0);
    actor_did(&actor, did);
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = NETWORK_ID;
    activity.activity_type = LX_PROGRAMS_DEPLOY;
    activity.actor_did = (lxp_byte_span){did, sizeof(did) - 1U};
    activity.authority = (lxp_byte_span){actor.public_key, sizeof(actor.public_key)};
    activity.payload = (lxp_byte_span){payload, length};
    REQUIRE(lxp_hash_payload(payload, length, activity.payload_hash) == LXP_OK &&
        lxp_hash_sha256(payload, length, digest) == LXP_OK);
    REQUIRE(lxp_arena_init(&arena, scratch, sizeof(scratch)) == LXP_OK);
    lxp_result status = lxp_activity_encode(&activity, &arena, &encoded);
    REQUIRE(status == LXP_ERR_MALFORMED_ENVELOPE);
    REQUIRE(lxp_activity_signing_preimage(&activity, preimage) == status);
    REQUIRE(program_evidence_write(argv[8], "payload.bin", payload, length) == 0);
    printf("{\"stage\":\"encoding\",\"result\":%d,\"payload_digest\":\"", (int)status);
    print_hex(digest, sizeof(digest));
    printf("\",\"payload_path\":\"payload.bin\"}\n");
    return 0;
}

static int program_fixture(int argc, char **argv)
{
    static uint8_t payload[LXP_MAX_ACTIVITY_BYTES];
    static uint8_t encoded[LXP_MAX_ACTIVITY_BYTES];
    static uint8_t scratch[2U * LXP_MAX_ACTIVITY_BYTES];
    signer actor, sequencer;
    uint8_t did[76], id[32], query[34] = {1U};
    size_t payload_length, encoded_length;
    struct sockaddr_un address = {0};
    wire_envelope response;
    lxp_activity activity;
    lxp_arena arena;
    lxp_byte_span canonical;
    char *end;
    uint64_t sequence;
    int expected;
    unsigned refusal_class = 0U;
    bool admission = false;
    bool replay = strcmp(argv[3], "program-replay") == 0;
    bool funding = strcmp(argv[3], "funding-credit") == 0;
    uint32_t activity_type = funding ? LXP_BRIDGE_CREDIT :
        strcmp(argv[3], "program-deploy") == 0 ? LX_PROGRAMS_DEPLOY : LX_PROGRAMS_CALL;
    REQUIRE(argc == 9 && (replay || funding || strcmp(argv[3], "program-deploy") == 0 ||
        strcmp(argv[3], "program-call") == 0));
    REQUIRE(strcmp(argv[5], "wait") == 0 && argv[4][0] >= '0' && argv[4][0] <= '9');
    errno = 0;
    sequence = strtoull(argv[4], &end, 10);
    REQUIRE(errno == 0 && *end == '\0' && sequence != UINT64_MAX);
    if (strncmp(argv[7], "admission:", 10U) == 0) {
        admission = true;
        REQUIRE(!replay && argv[7][10] >= '0' && argv[7][10] <= '9');
        errno = 0;
        unsigned long parsed_class = strtoul(argv[7] + 10U, &end, 10);
        REQUIRE(errno == 0 && *end == ':' && parsed_class > 0U &&
            parsed_class <= UINT8_MAX);
        refusal_class = (unsigned)parsed_class;
        const char *number = end + 1U;
        errno = 0;
        long result = strtol(number, &end, 10);
        REQUIRE(errno == 0 && end != number && *end == '\0' &&
            result >= INT32_MIN && result < 0);
        expected = (int)result;
    } else {
        const char *number = strncmp(argv[7], "receipt:", 8U) == 0 ?
            argv[7] + 8U : argv[7];
        errno = 0;
        long result = strtol(number, &end, 10);
        REQUIRE(errno == 0 && end != number && *end == '\0' &&
            result >= INT32_MIN && result <= 0);
        expected = (int)result;
    }
    REQUIRE(fixture_signer(&actor, "treasury") == 0 &&
        fixture_signer(&sequencer, "sequencer") == 0);
    actor_did(&actor, did);
    memcpy(REGISTERED_DID, did, sizeof(did));
    if (replay) {
        REQUIRE(program_payload_read(argv[6], encoded, sizeof(encoded),
            &encoded_length) == 0);
    } else {
        REQUIRE(program_payload_read(argv[6], payload, sizeof(payload),
            &payload_length) == 0);
        REQUIRE(build_activity(&actor, sequence, activity_type, 0U,
            payload, payload_length, encoded, sizeof(encoded), &encoded_length) == 0);
    }
    REQUIRE(lxp_activity_decode(encoded, encoded_length, &activity) == LXP_OK);
    if (!replay) {
        uint8_t preimage[32], signature[64];
        if (funding) {
            lxp_bridge_profile profile;
            lxp_bridge_credit credit;
            uint8_t name[LX_ACCOUNT_NAME_MAX], beneficiary[32];
            size_t profile_length;
            REQUIRE(program_payload_read(argv[2], profile.bytes, sizeof(profile.bytes),
                &profile_length) == 0 && profile_length == sizeof(profile.bytes));
            REQUIRE(lxp_bridge_credit_parse(payload, payload_length, &credit) == LXP_OK &&
                lxp_bridge_profile_validate(&profile) == LXP_OK && credit.proof_length >= 37U);
            REQUIRE(memcmp(actor.public_key, credit.bytes + 139U, 32U) == 0 &&
                load_u32(credit.bytes + 37U) == NETWORK_ID);
            memcpy(name, "agent:", 6U);
            memcpy(name + 6U, did, sizeof(did) - 1U);
            memcpy(name + 6U + sizeof(did) - 1U, ":main", 5U);
            REQUIRE(lx_account_id_from_string(name, 6U + sizeof(did) - 1U + 5U,
                beneficiary) == LXP_OK && memcmp(beneficiary, credit.bytes + 107U, 32U) == 0);
            uint64_t header_seconds = load_u64(credit.proof + 29U);
            REQUIRE(header_seconds <= UINT64_MAX / 1000U &&
                lxp_bridge_credit_verify(&profile, &credit, NETWORK_ID,
                    LXP_PROTOCOL_VERSION_STATE_COMMITMENT, NULL, header_seconds * 1000U,
                    activity.idempotency_key, NULL) == LXP_OK);
        } else {
            const char *fee = getenv("PAXEER_X_PROGRAM_FEE_LIMIT");
            REQUIRE(fee != NULL && fee[0] >= '0' && fee[0] <= '9');
            errno = 0;
            uint64_t fee_limit = strtoull(fee, &end, 10);
            REQUIRE(errno == 0 && *end == '\0');
            activity.fee_limit = (lxp_u128){0U, fee_limit};
        }
        REQUIRE(lxp_activity_signing_preimage(&activity, preimage) == LXP_OK &&
            sign_raw(&actor, preimage, sizeof(preimage), signature) == 0);
        activity.signature = (lxp_byte_span){signature, sizeof(signature)};
        REQUIRE(lxp_arena_init(&arena, scratch, sizeof(scratch)) == LXP_OK &&
            lxp_activity_encode(&activity, &arena, &canonical) == LXP_OK &&
            canonical.length <= sizeof(encoded));
        memcpy(encoded, canonical.bytes, canonical.length);
        encoded_length = canonical.length;
        REQUIRE(lxp_activity_decode(encoded, encoded_length, &activity) == LXP_OK);
    }
    REQUIRE(activity.protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT &&
        activity.network_id == NETWORK_ID && activity.account_sequence == sequence);
    REQUIRE(activity.activity_type == LX_PROGRAMS_DEPLOY || activity.activity_type == LX_PROGRAMS_CALL ||
        ((funding || replay) && activity.activity_type == LXP_BRIDGE_CREDIT));
    REQUIRE(activity.actor_did.length == sizeof(did) - 1U &&
        memcmp(activity.actor_did.bytes, did, sizeof(did) - 1U) == 0);
    REQUIRE(lxp_activity_verify_payload_hash(&activity) == LXP_OK &&
        lxp_activity_verify_signature(&activity) == LXP_OK &&
        lxp_activity_id(encoded, encoded_length, id) == LXP_OK);
    REQUIRE(program_evidence_write(argv[8], "activity.bin", encoded, encoded_length) == 0);
    REQUIRE(strlen(argv[1]) < sizeof(address.sun_path));
    address.sun_family = AF_UNIX;
    memcpy(address.sun_path, argv[1], strlen(argv[1]) + 1U);
    int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
    REQUIRE(descriptor >= 0 && connect(descriptor, (struct sockaddr *)&address, sizeof(address)) == 0);
    REQUIRE(send_request(descriptor, 0U, NODE_INFO_REQUEST, 0U, NULL, 0U) == 0);
    REQUIRE(receive_envelope(descriptor, &response) == 0);
    REQUIRE(response.major == LNI_MAJOR && response.minor == LNI_MINOR &&
        response.tag == NODE_INFO_RESPONSE && response.correlation_id == 0U);
    release_envelope(&response);
    if (!replay) {
        REQUIRE(send_request(descriptor, LNI_MINOR, SUBMIT_REQUEST, sequence + 1U,
            encoded, encoded_length) == 0);
        REQUIRE(receive_envelope(descriptor, &response) == 0);
        REQUIRE(response.major == LNI_MAJOR && response.minor == LNI_MINOR &&
            response.correlation_id == sequence + 1U);
        if (admission) {
            REQUIRE(response.tag == ERROR_RESPONSE && response.payload_length == 5U &&
                response.proof_length == 0U && response.payload[0] == refusal_class &&
                (int32_t)load_u32(response.payload + 1U) == expected);
            REQUIRE(program_evidence_write(argv[8], "refusal.bin", response.owned,
                response.owned_length) == 0);
            printf("{\"stage\":\"admission\",\"result\":%d,\"class\":%u,\"activity_id\":\"", expected, refusal_class);
            print_hex(id, sizeof(id));
            printf("\",\"activity_path\":\"activity.bin\",\"refusal_path\":\"refusal.bin\"}\n");
            release_envelope(&response);
            REQUIRE(close(descriptor) == 0);
            return 0;
        }
        REQUIRE(response.tag == SUBMIT_RESPONSE && response.payload_length == encoded_length &&
            memcmp(response.payload, encoded, encoded_length) == 0 &&
            response.proof_length == sizeof(id) && memcmp(response.proof, id, sizeof(id)) == 0);
        release_envelope(&response);
    }
    memcpy(query + 1U, id, sizeof(id));
    query[33] = 1U;
    REQUIRE(send_request(descriptor, LNI_MINOR, 5U, sequence + 1U, query, sizeof(query)) == 0);
    REQUIRE(receive_envelope(descriptor, &response) == 0);
    REQUIRE(response.major == LNI_MAJOR && response.minor == LNI_MINOR && response.tag == 6U &&
        response.correlation_id == sequence + 1U && response.payload_length != 0U);
    lxp_receipt receipt;
    REQUIRE(lxp_arena_init(&arena, scratch, sizeof(scratch)) == LXP_OK);
    REQUIRE(lxp_receipt_decode(response.payload, response.payload_length, true, &receipt) == LXP_OK);
    REQUIRE(receipt.protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT &&
        receipt.module_id == lxp_activity_module_id(activity.activity_type) && receipt.result_code == expected &&
        memcmp(receipt.activity_id, id, sizeof(id)) == 0);
    REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
    if (activity.activity_type == LX_PROGRAMS_CALL && expected == LXP_OK) {
        REQUIRE(activity.payload.length >= 34U && receipt.operation == 3U &&
            receipt.program_outcome.present &&
            receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS &&
            receipt.program_outcome.result_code == LXP_OK &&
            receipt.program_outcome.abi_version == load_u16(activity.payload.bytes + 32U) &&
            receipt.program_outcome.runtime_version != 0U &&
            receipt.program_outcome.metering_schedule_version != 0U &&
            lxp_program_outcome_validate_for_protocol(&receipt.program_outcome,
                receipt.protocol_version) == LXP_OK);
    }
    REQUIRE(expected == LXP_OK || receipt.effects.count == 0U);
    REQUIRE(lxp_arena_reset(&arena, 0U) == LXP_OK &&
        lxp_receipt_encode(&receipt, true, &arena, &canonical) == LXP_OK);
    REQUIRE(canonical.length == response.payload_length &&
        memcmp(canonical.bytes, response.payload, canonical.length) == 0);
    REQUIRE(program_evidence_write(argv[8], "receipt.bin", response.payload,
        response.payload_length) == 0);
    uint8_t receipt_digest[32];
    REQUIRE(lxp_arena_reset(&arena, 0U) == LXP_OK &&
        lxp_receipt_digest(&receipt, &arena, receipt_digest) == LXP_OK);
    printf("{\"stage\":\"receipt\",\"result\":%d,\"class\":0,\"activity_id\":\"", expected);
    print_hex(id, sizeof(id));
    printf("\",\"batch_id\":\"");
    print_hex(receipt.batch_id, sizeof(receipt.batch_id));
    printf("\",\"receipt_digest\":\"");
    print_hex(receipt_digest, sizeof(receipt_digest));
    printf("\",\"global_sequence\":%llu,\"module_id\":%u,\"module_version\":%u,\"activity_path\":\"activity.bin\",\"receipt_path\":\"receipt.bin\"}\n",
        (unsigned long long)receipt.global_sequence, (unsigned)receipt.module_id,
        (unsigned)receipt.module_version);
    release_envelope(&response);
    REQUIRE(close(descriptor) == 0);
    return 0;
}

int main(int argc, char **argv)
{
    if (argc == 9 && strcmp(argv[3], "program-envelope") == 0)
        return program_envelope_refusal(argv);
    if (argc == 9) return program_fixture(argc, argv);
    signer alice, bob;
    uint8_t alice_did[76], bob_did[76], issuer[32], salt[32], asset[32], from[32], to[32];
    uint8_t payload[1024] = {0U}, message[512], digest[32];
    size_t length = 0U, message_length;
    struct sockaddr_un address = {0};
    lxp_hash_context hash;
    lxp_payer_grant grant = {0};
    REQUIRE(argc == 6 && strlen(argv[1]) < sizeof(address.sun_path));
    if (strcmp(argv[3], "proof") == 0) return verify_replica(argv[4]);
    REQUIRE(fixture_signer(&alice, "treasury") == 0 && fixture_signer(&bob, "bob") == 0);
    actor_did(&alice, alice_did); actor_did(&bob, bob_did);
    REQUIRE(lxp_did_id_derive(alice_did, 75U, issuer) == LXP_OK);
    FILE *salt_file = fopen(argv[2], "rb");
    REQUIRE(salt_file != NULL && fread(salt, 1U, 32U, salt_file) == 32U);
    REQUIRE(fgetc(salt_file) == EOF && fclose(salt_file) == 0);
    lxp_hash_init(&hash);
    REQUIRE(lxp_hash_update(&hash, "LX:ASSET:v1", 11U) == LXP_OK);
    REQUIRE(lxp_hash_update(&hash, issuer, 32U) == LXP_OK);
    REQUIRE(lxp_hash_update(&hash, salt, 32U) == LXP_OK);
    REQUIRE(lxp_hash_final(&hash, asset) == LXP_OK);
    REQUIRE(account_id(alice_did, asset, from) == 0 && account_id(bob_did, asset, to) == 0);
    memcpy(REGISTERED_DID, alice_did, 76U);
    uint64_t sequence = strtoull(argv[4], NULL, 10);
    bool wait = strcmp(argv[5], "wait") == 0;
    address.sun_family = AF_UNIX;
    memcpy(address.sun_path, argv[1], strlen(argv[1]) + 1U);
    int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
    REQUIRE(descriptor >= 0 && connect(descriptor, (struct sockaddr *)&address, sizeof(address)) == 0);
    wire_envelope response;
    REQUIRE(send_request(descriptor, 0U, 1U, 0U, NULL, 0U) == 0);
    REQUIRE(receive_envelope(descriptor, &response) == 0 && response.tag == 2U && response.minor == 7U);
    release_envelope(&response);
    if (strcmp(argv[3], "ready") == 0) { REQUIRE(close(descriptor) == 0); return 0; }
    memcpy(grant.from, from, 32U); memcpy(grant.recipient, to, 32U); memcpy(grant.asset, asset, 32U);
    grant.per_draw_maximum.lo = 2U; grant.allowance.lo = 10U; grant.expiration = UINT64_MAX;
    grant.purpose_hash[0] = 1U; memcpy(grant.public_key, alice.public_key, 32U);
    REQUIRE(lxp_grant_authorization_message(&grant, message, sizeof(message), &message_length) == LXP_OK);
    REQUIRE(lxp_hash_authority(message, message_length, grant.grant_id) == LXP_OK);
    REQUIRE(lxp_hash_domain(LXP_DOMAIN_AUTHORITY_HASH, message, message_length, digest) == LXP_OK);
    REQUIRE(sign_raw(&alice, digest, 32U, grant.signature) == 0);
    if (strcmp(argv[3], "receipt") == 0) {
        uint8_t query[33] = {1U};
        REQUIRE(strlen(argv[4]) == 64U);
        for (size_t i = 0U; i < 32U; ++i) {
            unsigned value;
            REQUIRE(sscanf(argv[4] + 2U * i, "%2x", &value) == 1);
            query[i + 1U] = (uint8_t)value;
        }
        REQUIRE(send_request(descriptor, 5U, 5U, 1U, query, sizeof(query)) == 0);
        REQUIRE(receive_envelope(descriptor, &response) == 0 && response.tag == 6U && response.payload_length != 0U);
        lxp_receipt receipt;
        static uint8_t storage[2U * LXP_MAX_ACTIVITY_BYTES];
        lxp_arena arena;
        signer sequencer;
        REQUIRE(fixture_signer(&sequencer, "sequencer") == 0);
        REQUIRE(lxp_arena_init(&arena, storage, sizeof(storage)) == LXP_OK);
        REQUIRE(lxp_receipt_decode(response.payload, response.payload_length, true, &receipt) == LXP_OK);
        REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
        REQUIRE(memcmp(receipt.activity_id, query + 1U, 32U) == 0);
        print_hex(response.payload, response.payload_length); printf("\n");
        release_envelope(&response);
    } else if (strcmp(argv[3], "read") == 0) {
        uint8_t list[3] = {0U, 1U, 1U}, get[35] = {0U, 1U, 2U};
        uint8_t fee[30] = {0U, 1U}, accounts[37] = {0U, 1U, 3U};
        memcpy(get + 3U, asset, 32U);
        store_u32(fee + 2U, LX_ASSET_SEND); store_u64(fee + 6U, 400U);
        memcpy(accounts + 3U, issuer, 32U); accounts[35] = 1U; accounts[36] = 1U;
        REQUIRE(read_state(descriptor, 32U, list, sizeof(list)) == 0);
        REQUIRE(read_state(descriptor, 32U, get, sizeof(get)) == 0);
        REQUIRE(read_state(descriptor, 34U, fee, sizeof(fee)) == 0);
        REQUIRE(read_state(descriptor, 7U, accounts, sizeof(accounts)) == 0);
        REQUIRE(lxp_did_id_derive(bob_did, 75U, accounts + 3U) == LXP_OK);
        REQUIRE(read_state(descriptor, 7U, accounts, sizeof(accounts)) == 0);
    } else if (strcmp(argv[3], "register") == 0) {
        store_u16(payload, 1U); memcpy(payload + 2U, asset, 32U); memcpy(payload + 34U, salt, 32U);
        length = 66U; payload[length++] = 3U; memcpy(payload + length, "TOK", 3U); length += 3U;
        payload[length++] = 5U; memcpy(payload + length, "Token", 5U); length += 5U;
        payload[length++] = 6U;
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, 10000U}, payload + length) == LXP_OK); length += 16U;
        payload[length++] = 1U; payload[length++] = 0U;
        REQUIRE(submit_pay(descriptor, &alice, sequence, 1U, payload, length, wait) == 0);
    } else if (strcmp(argv[3], "open") == 0 || strcmp(argv[3], "open-bob") == 0) {
        bool second = strcmp(argv[3], "open-bob") == 0;
        if (second) memcpy(REGISTERED_DID, bob_did, 76U);
        store_u16(payload, 1U); memcpy(payload + 2U, asset, 32U);
        REQUIRE(submit_pay(descriptor, second ? &bob : &alice, sequence, 4U, payload, 34U, wait) == 0);
    } else if (strcmp(argv[3], "mint") == 0 || strcmp(argv[3], "burn") == 0) {
        bool mint = strcmp(argv[3], "mint") == 0;
        store_u16(payload, 1U); memcpy(payload + 2U, asset, 32U); memcpy(payload + 34U, from, 32U);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, mint ? 9000U : 100U}, payload + 66U) == LXP_OK);
        REQUIRE(submit_pay(descriptor, &alice, sequence, mint ? 10U : 11U, payload, 82U, wait) == 0);
    } else if (strcmp(argv[3], "grant-issue") == 0) {
        REQUIRE(lxp_payer_grant_encode(&grant, payload, sizeof(payload), &length) == LXP_OK);
        REQUIRE(submit_pay(descriptor, &alice, sequence, 7U, payload, length, wait) == 0);
    } else if (strcmp(argv[3], "grant-revoke") == 0) {
        store_u16(payload, 1U); memcpy(payload + 2U, grant.grant_id, 32U); store_u64(payload + 34U, sequence);
        REQUIRE(submit_pay(descriptor, &alice, sequence, 8U, payload, 42U, wait) == 0);
    } else {
        REQUIRE(strcmp(argv[3], "sends") == 0 || strcmp(argv[3], "send-one") == 0);
        for (unsigned i = 0U; i < (strcmp(argv[3], "send-one") == 0 ? 1U : 20U); ++i) {
            lxp_send send = {0};
            memcpy(send.from, from, 32U); memcpy(send.to, to, 32U); memcpy(send.asset, asset, 32U);
            send.amount.lo = 1U; send.sequence = sequence - 5U + i; send.expires_at = UINT64_MAX;
            store_u64(send.idempotency_key, sequence + i); send.idempotency_key[31] = 0xa5U;
            uint8_t material[144];
            send.authorization.kind = LXP_AUTH_OWNER;
            send.authorization.network_id = NETWORK_ID;
            send.authorization.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
            memcpy(send.authorization.controller, from, 32U);
            memcpy(material, from, 32U); memcpy(material + 32U, to, 32U);
            memcpy(material + 64U, asset, 32U);
            REQUIRE(lxp_u128_to_be(send.amount, material + 96U) == LXP_OK);
            memcpy(material + 112U, send.idempotency_key, 32U);
            REQUIRE(lxp_hash_context_value(material, sizeof(material), send.context_hash) == LXP_OK);
            memcpy(send.authorization.signed_context_hash, send.context_hash, 32U);
            memcpy(send.authorization.public_key, alice.public_key, 32U);
            REQUIRE(lxp_send_authorization_message(&send, message, sizeof(message), &message_length) == LXP_OK);
            REQUIRE(lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message, message_length, digest) == LXP_OK);
            REQUIRE(sign_raw(&alice, digest, 32U, send.authorization.signature) == 0);
            REQUIRE(lxp_send_encode(&send, payload, sizeof(payload), &length) == LXP_OK);
            REQUIRE(submit_pay(descriptor, &alice, sequence + i, 5U, payload, length, wait) == 0);
        }
    }
    REQUIRE(close(descriptor) == 0);
    return 0;
}
