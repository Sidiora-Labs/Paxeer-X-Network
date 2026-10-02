#define main program_admission_main
#include "lxp_test_program_admission.c"
#undef main
#include "layerx/lx_asset.h"
#include "layerx/lxp_ledger.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_merkle.h"

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

#include "layerx/lx_escrow.h"
#include "layerx/lxp_bridge_credit.h"
#include "layerx/lxp_module.h"
#include "layerx/lxp_state_proof.h"
#include "../../src/modules/escrow/lx_escrow_internal.h"

static int escrow_read(int fd, uint16_t module, const uint8_t *key, size_t key_length, const char *label)
{
    uint8_t query[512] = {0U, 1U, 4U};
    wire_envelope response;
    store_u16(query + 3U, module); store_u16(query + 5U, (uint16_t)key_length);
    memcpy(query + 7U, key, key_length); query[7U + key_length] = 1U; query[8U + key_length] = 3U;
    REQUIRE(send_request(fd, 5U, 7U, 91U, query, key_length + 9U) == 0);
    REQUIRE(receive_envelope(fd, &response) == 0);
    REQUIRE(response.tag == 8U && response.proof_length > 8U);
    const uint8_t *p = response.proof;
    REQUIRE(load_u16(p) == 1U && p[2] == 4U && p[3] == 1U);
    size_t length = load_u32(p + 4U);
    REQUIRE(length < response.proof_length - 8U);
    lxp_state_witness *witness = calloc(1U, sizeof(*witness));
    REQUIRE(witness != NULL && lxp_state_proof_decode(p + 8U, length, witness) == LXP_OK);
    REQUIRE(witness->key_length == key_length && memcmp(witness->key, key, key_length) == 0);
    REQUIRE(witness->value_length == response.payload_length && memcmp(witness->value, response.payload, response.payload_length) == 0);
    size_t offset = 8U + length;
    REQUIRE(response.proof_length - offset >= 86U + LXP_BATCH_HEADER_ENCODED_SIZE + 65U);
    p += offset;
    lxp_sequencer_authorization authorization = {0};
    signer sequencer;
    REQUIRE(fixture_signer(&sequencer, "sequencer") == 0);
    REQUIRE(load_u16(p) == 1U && memcmp(p + 34U, sequencer.public_key, 32U) == 0);
    memcpy(authorization.sequencer_id, p + 2U, 32U);
    memcpy(authorization.public_key, sequencer.public_key, 32U);
    authorization.first_batch_number = load_u64(p + 66U);
    authorization.last_batch_number = load_u64(p + 74U); authorization.authorized = 1U;
    REQUIRE(load_u32(p + 82U) == LXP_BATCH_HEADER_ENCODED_SIZE);
    lxp_batch_header header;
    static uint8_t memory[2U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    REQUIRE(lxp_arena_init(&arena, memory, sizeof(memory)) == LXP_OK);
    REQUIRE(lxp_batch_header_decode(p + 86U, LXP_BATCH_HEADER_ENCODED_SIZE, &header) == LXP_OK);
    REQUIRE(header.network_id == NETWORK_ID && header.protocol_version == 3U);
    REQUIRE(lxp_batch_verify_signature(&header, p + 86U + LXP_BATCH_HEADER_ENCODED_SIZE, 64U, &authorization, &arena) == LXP_OK);
    REQUIRE(lxp_state_proof_verify(witness, header.resulting_state_root) == LXP_OK);
    printf("state label=%s timestamp=%llu root=", label, (unsigned long long)header.timestamp_ms);
    print_hex(header.resulting_state_root, 32U); printf(" raw="); print_hex(response.payload, response.payload_length); puts("");
    free(witness); release_envelope(&response); return 0;
}

static int build_escrow_activity(const signer *key, uint64_t account_sequence, uint32_t activity_type,
    const uint8_t *payload, size_t payload_length, uint8_t *output, size_t capacity, size_t *length)
{
    lxp_activity activity;
    lxp_arena arena;
    lxp_byte_span encoded;
    uint8_t preimage[32], signature[64];
    struct timespec now;
    memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = NETWORK_ID;
    activity.activity_type = activity_type;
    activity.actor_did = (lxp_byte_span){REGISTERED_DID, sizeof(REGISTERED_DID) - 1U};
    activity.authority = (lxp_byte_span){key->public_key, 32U};
    activity.account_sequence = account_sequence;
    REQUIRE(clock_gettime(CLOCK_REALTIME, &now) == 0);
    activity.timestamp_bound.not_before = (uint64_t)now.tv_sec * 1000U;
    activity.timestamp_bound.not_after = activity.timestamp_bound.not_before + 300000U;
    /* The outer Activity key is fresh per account sequence; the inner escrow
     * key travels in the payload and is chosen by the caller. */
    for (size_t index = 0U; index < 8U; ++index)
        activity.idempotency_key[index] = (uint8_t)(account_sequence >> ((7U - index) * 8U));
    activity.idempotency_key[30] = 0xe5U; activity.idempotency_key[31] = 0xa5U;
    activity.fee_limit = (lxp_u128){0U, activity_type == LXP_BRIDGE_CREDIT ? 0U : 10000U};
    if (activity_type == LXP_BRIDGE_CREDIT) {
        lxp_bridge_credit credit;
        lxp_bridge_profile profile;
        const char *profile_path = getenv("ESCROW_CREDIT_PROFILE");
        REQUIRE(profile_path != NULL);
        FILE *file = fopen(profile_path, "rb");
        REQUIRE(file != NULL && fread(profile.bytes, 1U, sizeof(profile.bytes), file) == sizeof(profile.bytes));
        REQUIRE(fgetc(file) == EOF && fclose(file) == 0);
        REQUIRE(lxp_bridge_credit_parse(payload, payload_length, &credit) == LXP_OK);
        uint64_t seconds = load_u64(credit.proof + 29U);
        REQUIRE(seconds <= UINT64_MAX / 1000U);
        REQUIRE(lxp_bridge_credit_verify(&profile, &credit, NETWORK_ID, 3U, NULL,
            seconds * 1000U, activity.idempotency_key, NULL) == LXP_OK);
    }
    activity.payload = (lxp_byte_span){payload, payload_length};
    REQUIRE(lxp_hash_payload(payload, payload_length, activity.payload_hash) == LXP_OK);
    REQUIRE(lxp_activity_signing_preimage(&activity, preimage) == LXP_OK);
    REQUIRE(sign_raw(key, preimage, sizeof(preimage), signature) == 0);
    activity.signature = (lxp_byte_span){signature, sizeof(signature)};
    uint8_t *storage = malloc(LXP_MAX_ACTIVITY_BYTES);
    REQUIRE(storage != NULL);
    REQUIRE(lxp_arena_init(&arena, storage, LXP_MAX_ACTIVITY_BYTES) == LXP_OK);
    REQUIRE(lxp_activity_encode(&activity, &arena, &encoded) == LXP_OK && encoded.length <= capacity);
    memcpy(output, encoded.bytes, encoded.length);
    *length = encoded.length;
    free(storage);
    return 0;
}

static void print_u128(lxp_u128 value)
{
    printf("%016llx%016llx", (unsigned long long)value.hi, (unsigned long long)value.lo);
}

static int escrow_submit(int fd, const signer *actor, uint64_t sequence, uint32_t type,
    const uint8_t *payload, size_t payload_length, const char *path, bool replay, int expected)
{
    static uint8_t encoded[LXP_MAX_ACTIVITY_BYTES];
    uint8_t id[32], query[33] = {1U};
    size_t length;
    FILE *file;
    if (replay) {
        file = fopen(path, "rb"); REQUIRE(file != NULL);
        length = fread(encoded, 1U, sizeof(encoded), file);
        REQUIRE(length > 0U && fgetc(file) == EOF && fclose(file) == 0);
    } else {
        REQUIRE(build_escrow_activity(actor, sequence, type, payload, payload_length, encoded, sizeof(encoded), &length) == 0);
        file = fopen(path, "wx"); REQUIRE(file != NULL);
        REQUIRE(fwrite(encoded, 1U, length, file) == length && fclose(file) == 0);
    }
    REQUIRE(lxp_activity_id(encoded, length, id) == LXP_OK);
    REQUIRE(send_request(fd, 5U, 3U, sequence + 1U, encoded, length) == 0);
    REQUIRE(expect_ack(fd, sequence + 1U, encoded, length, id) == 0);
    memcpy(query + 1U, id, 32U);
    signer sequencer; REQUIRE(fixture_signer(&sequencer, "sequencer") == 0);
    for (unsigned attempt = 0; attempt < 400U; ++attempt) {
        wire_envelope response;
        REQUIRE(send_request(fd, 5U, 5U, sequence + 1U, query, sizeof(query)) == 0);
        REQUIRE(receive_envelope(fd, &response) == 0 && response.tag == 6U);
        if (response.payload_length) {
            static lxp_receipt receipt;
            lxp_arena arena;
            static uint8_t memory[2U * LXP_MAX_ACTIVITY_BYTES];
            REQUIRE(lxp_arena_init(&arena, memory, sizeof(memory)) == LXP_OK);
            REQUIRE(lxp_receipt_decode(response.payload, response.payload_length, true, &receipt) == LXP_OK);
            REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
            REQUIRE(memcmp(receipt.activity_id, id, 32U) == 0);
            printf("receipt ordinal=%u sequence=%llu result=%d root=", type & 65535U, (unsigned long long)receipt.global_sequence, (int)receipt.result_code);
            print_hex(receipt.resulting_state_root, 32U); printf(" id="); print_hex(id, 32U);
            printf(" batch="); print_hex(receipt.batch_id, 32U);
            uint8_t digest[32]; REQUIRE(lxp_receipt_digest(&receipt, &arena, digest) == LXP_OK);
            printf(" digest="); print_hex(digest, 32U);
            printf(" fee="); print_u128(receipt.fee_charged);
            printf(" module=%u operation=%u amount=", receipt.module_id, receipt.operation); print_u128(receipt.amount);
            printf(" from="); print_hex(receipt.from, 32U); printf(" to="); print_hex(receipt.to, 32U);
            printf(" tsr="); print_hex(receipt.transfer_set_root, 32U);
            printf(" events=");
            size_t events = 0U;
            for (size_t i = 0U; i < receipt.effects.count; ++i) {
                const lxp_effect *effect = &receipt.effects.effects[i];
                if (effect->kind != LXP_EFFECT_EVENT || effect->module_id != LXP_MODULE_ESCROW) continue;
                printf("%s%u", events++ ? "." : "", effect->event_type);
            }
            if (events == 0U) printf("none");
            printf(" event_bytes=");
            for (size_t i = 0U; i < receipt.effects.count; ++i) {
                const lxp_effect *effect = &receipt.effects.effects[i];
                if (effect->kind != LXP_EFFECT_EVENT || effect->module_id != LXP_MODULE_ESCROW) continue;
                REQUIRE(effect->body_length == LX_ESCROW_EVENT_BYTES);
                print_hex(effect->body, effect->body_length);
            }
            if (events == 0U) printf("none");
            printf(" raw="); print_hex(response.payload, response.payload_length); puts("");
            REQUIRE(receipt.result_code == expected);
            release_envelope(&response); return 0;
        }
        release_envelope(&response);
        const struct timespec delay = {0, 50000000}; REQUIRE(nanosleep(&delay, NULL) == 0);
    }
    return 1;
}

static int named_account(const uint8_t did[76], const char *suffix, uint8_t id[32])
{
    char name[256];
    int n = snprintf(name, sizeof(name), "agent:%s:%s", (const char *)did, suffix);
    REQUIRE(n > 0 && (size_t)n < sizeof(name));
    REQUIRE(lx_account_id_from_string((const uint8_t *)name, (size_t)n, id) == LXP_OK);
    return 0;
}

static int parse_hex(const char *text, uint8_t *out, size_t length)
{
    REQUIRE(text != NULL && strlen(text) == length * 2U);
    for (size_t i = 0U; i < length; ++i) {
        unsigned value;
        REQUIRE(sscanf(text + 2U * i, "%2x", &value) == 1);
        out[i] = (uint8_t)value;
    }
    return 0;
}

static const char *require_env(const char *name)
{
    const char *value = getenv(name);
    if (value == NULL || value[0] == 0) {
        fprintf(stderr, "missing %s\n", name);
        exit(1);
    }
    return value;
}

static void inner_key(const char *label, uint8_t key[32])
{
    memset(key, 0, 32U);
    key[0] = 0x4bU; key[1] = (uint8_t)strtoul(label, NULL, 10); key[31] = 0xa5U;
}

int main(int argc, char **argv)
{
    signer alice, bob;
    uint8_t alice_did[76], bob_did[76], salt[32], alice_main[32], bob_main[32];
    uint8_t hold[3][32], custody[3][32];
    struct sockaddr_un address = {.sun_family = AF_UNIX};
    REQUIRE(argc == 6 && strcmp(argv[5], "poll") == 0 && strlen(argv[1]) < sizeof(address.sun_path));
    if (strcmp(argv[3], "proof") == 0) return verify_replica(argv[4]);
    REQUIRE(fixture_signer(&alice, "treasury") == 0 && fixture_signer(&bob, "bob") == 0);
    actor_did(&alice, alice_did); actor_did(&bob, bob_did);
    FILE *salt_file = fopen(argv[2], "rb");
    REQUIRE(salt_file != NULL && fread(salt, 1U, 32U, salt_file) == 32U);
    REQUIRE(fgetc(salt_file) == EOF && fclose(salt_file) == 0);
    REQUIRE(named_account(alice_did, "main", alice_main) == 0 && named_account(bob_did, "main", bob_main) == 0);
    for (size_t h = 0U; h < 3U; ++h) {
        char suffix[80] = "escrow:";
        hold[h][0] = 0xe5U; hold[h][1] = (uint8_t)('a' + h); memcpy(hold[h] + 2U, salt, 30U);
        for (size_t i = 0U; i < 32U; ++i) snprintf(suffix + 7U + 2U * i, 3U, "%02x", hold[h][i]);
        REQUIRE(named_account(alice_did, suffix, custody[h]) == 0);
    }
    strcpy(address.sun_path, argv[1]);
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    REQUIRE(fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0);
    REQUIRE(handshake(fd) == 0);
    if (strcmp(argv[3], "ready") == 0) { REQUIRE(close(fd) == 0); return 0; }
    if (strcmp(argv[3], "receipt") == 0) {
        uint8_t query[33] = {1U};
        wire_envelope response;
        REQUIRE(parse_hex(argv[4], query + 1U, 32U) == 0);
        REQUIRE(send_request(fd, 5U, 5U, 1U, query, sizeof(query)) == 0);
        REQUIRE(receive_envelope(fd, &response) == 0 && response.tag == 6U && response.payload_length != 0U);
        static lxp_receipt receipt;
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
        REQUIRE(close(fd) == 0); return 0;
    }
    if (strcmp(argv[3], "read") == 0) {
        char labels[1024];
        const char *list = require_env("ESCROW_READ");
        REQUIRE(strlen(list) < sizeof(labels));
        strcpy(labels, list);
        for (char *save = NULL, *label = strtok_r(labels, ",", &save); label != NULL; label = strtok_r(NULL, ",", &save)) {
            uint8_t key[64] = {4U};
            if (strcmp(label, "alice") == 0 || strcmp(label, "bob") == 0) {
                memcpy(key + 1U, label[0] == 'a' ? alice_main : bob_main, 32U);
                REQUIRE(escrow_read(fd, 0U, key, 33U, label) == 0);
            } else if (strncmp(label, "acct-", 5U) == 0 && label[5] >= 'a' && label[5] <= 'c' && label[6] == 0) {
                memcpy(key + 1U, custody[label[5] - 'a'], 32U);
                REQUIRE(escrow_read(fd, 0U, key, 33U, label) == 0);
            } else if (strncmp(label, "hold-", 5U) == 0 && label[5] >= 'a' && label[5] <= 'c' && label[6] == 0) {
                REQUIRE(lx_escrow_hold_key(hold[label[5] - 'a'], key) == LXP_OK);
                REQUIRE(escrow_read(fd, LXP_MODULE_ESCROW, key, LX_ESCROW_HOLD_KEY_BYTES, label) == 0);
            } else {
                uint8_t idempotency[32];
                REQUIRE(strncmp(label, "result-", 7U) == 0);
                inner_key(label + 7U, idempotency);
                REQUIRE(lx_escrow_result_key(idempotency, key) == LXP_OK);
                REQUIRE(escrow_read(fd, LXP_MODULE_ESCROW, key, LX_ESCROW_RESULT_KEY_BYTES, label) == 0);
            }
        }
        REQUIRE(close(fd) == 0); return 0;
    }
    static uint8_t payload[LXP_MAX_PAYLOAD_BYTES];
    size_t length = 0U;
    uint32_t type = 0U;
    bool replay = strcmp(argv[3], "replay") == 0;
    bool as_bob = strcmp(require_env("ESCROW_ACTOR"), "bob") == 0;
    REQUIRE(as_bob || strcmp(getenv("ESCROW_ACTOR"), "alice") == 0);
    const char *hold_label = getenv("ESCROW_HOLD");
    size_t h = hold_label != NULL && hold_label[0] >= 'a' && hold_label[0] <= 'c' ? (size_t)(hold_label[0] - 'a') : 0U;
    uint64_t amount = getenv("ESCROW_AMOUNT") != NULL ? strtoull(getenv("ESCROW_AMOUNT"), NULL, 10) : 0U;
    uint8_t key[32];
    inner_key(getenv("ESCROW_KEY") != NULL ? getenv("ESCROW_KEY") : "0", key);
    if (replay) {
        type = 0U;
    } else if (strcmp(argv[3], "credit") == 0) {
        type = LXP_BRIDGE_CREDIT;
        FILE *credit_file = fopen(require_env("ESCROW_CREDIT_FILE"), "rb");
        REQUIRE(credit_file != NULL);
        length = fread(payload, 1U, sizeof(payload), credit_file);
        REQUIRE(length > 363U && fgetc(credit_file) == EOF && fclose(credit_file) == 0);
    } else if (strcmp(argv[3], "open") == 0) {
        struct timespec now;
        REQUIRE(clock_gettime(CLOCK_REALTIME, &now) == 0);
        uint64_t start = (uint64_t)now.tv_sec * 1000U;
        type = LX_ESCROW_OPEN; length = LX_ESCROW_OPEN_PAYLOAD_BYTES;
        memcpy(payload, hold[h], 32U); memcpy(payload + 32U, alice_main, 32U);
        memcpy(payload + 64U, custody[h], 32U); memcpy(payload + 96U, bob_main, 32U);
        memcpy(payload + 128U, alice_main, 32U);
        REQUIRE(parse_hex(require_env("ESCROW_ASSET"), payload + 160U, 32U) == 0);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, amount}, payload + 192U) == LXP_OK);
        store_u64(payload + 208U, 0U);
        store_u64(payload + 216U, start + 3600000U);
        memset(payload + 224U, 0x7eU, 32U); memcpy(payload + 256U, hold[h], 32U); payload[256] = 0x52U;
    } else if (strcmp(argv[3], "capture") == 0 || strcmp(argv[3], "partial") == 0) {
        type = argv[3][0] == 'c' ? LX_ESCROW_CAPTURE : LX_ESCROW_PARTIAL_CAPTURE;
        length = LX_ESCROW_CAPTURE_PAYLOAD_BYTES;
        memcpy(payload, hold[h], 32U);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, amount}, payload + 32U) == LXP_OK);
        memcpy(payload + 48U, key, 32U);
    } else if (strcmp(argv[3], "release") == 0 || strcmp(argv[3], "timeout") == 0) {
        type = argv[3][0] == 'r' ? LX_ESCROW_RELEASE : LX_ESCROW_TIMEOUT;
        length = LX_ESCROW_RELEASE_PAYLOAD_BYTES;
        memcpy(payload, hold[h], 32U); memcpy(payload + 32U, key, 32U);
    } else if (strcmp(argv[3], "dispute") == 0) {
        type = LX_ESCROW_DISPUTE_OPEN; length = LX_ESCROW_DISPUTE_OPEN_PAYLOAD_BYTES;
        memcpy(payload, hold[h], 32U);
    } else {
        REQUIRE(strcmp(argv[3], "resolve") == 0);
        uint32_t bps = (uint32_t)strtoul(require_env("ESCROW_BPS"), NULL, 10);
        type = LX_ESCROW_DISPUTE_RESOLVE; length = LX_ESCROW_DISPUTE_RESOLVE_PAYLOAD_BYTES;
        memcpy(payload, hold[h], 32U);
        payload[32] = (uint8_t)(bps >> 24U); payload[33] = (uint8_t)(bps >> 16U);
        payload[34] = (uint8_t)(bps >> 8U); payload[35] = (uint8_t)bps;
        memcpy(payload + 36U, key, 32U);
    }
    memcpy(REGISTERED_DID, as_bob ? bob_did : alice_did, 76U);
    REQUIRE(escrow_submit(fd, as_bob ? &bob : &alice, strtoull(argv[4], NULL, 10), type, payload, length,
        require_env("ESCROW_WIRE"), replay, atoi(require_env("ESCROW_EXPECT"))) == 0);
    REQUIRE(close(fd) == 0);
    return 0;
}
