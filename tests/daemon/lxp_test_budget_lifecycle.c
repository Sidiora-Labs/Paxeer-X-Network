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


#include "layerx/lx_budget.h"
#include "layerx/lxp_bridge_credit.h"
#include "layerx/lxp_state_proof.h"

static int budget_read(int fd, uint16_t module, const uint8_t *key, size_t key_length, const char *label)
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

static int build_budget_activity(const signer *key, uint64_t account_sequence,
                          uint32_t activity_type, uint64_t timestamp, const uint8_t *payload, size_t payload_length, uint8_t *output,
                          size_t capacity, size_t *length)
{
    uint8_t *arena_storage;
    lxp_activity activity;
    lxp_arena arena;
    lxp_byte_span encoded;
    uint8_t preimage[32];
    uint8_t signature[64];
    size_t index;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = NETWORK_ID;
    activity.activity_type = activity_type;
    activity.actor_did = (lxp_byte_span){
        REGISTERED_DID, sizeof(REGISTERED_DID) - 1U};
    activity.authority = (lxp_byte_span){key->public_key, 32U};
    activity.account_sequence = account_sequence;
    {
        struct timespec now;
        if (clock_gettime(CLOCK_REALTIME, &now) != 0) return 1;
        activity.timestamp_bound.not_before = timestamp != 0U ? timestamp : (uint64_t)now.tv_sec * 1000U;
        activity.timestamp_bound.not_after = activity.timestamp_bound.not_before + 300000U;
    }
    for (index = 0U; index < 8U; ++index)
        activity.idempotency_key[index] =
            (uint8_t)(account_sequence >> ((7U - index) * 8U));
    activity.idempotency_key[31] = 0xa5U;
    activity.fee_limit = (lxp_u128){0U, activity_type == LXP_BRIDGE_CREDIT ? 0U : 10000U};
    if (activity_type == LXP_BRIDGE_CREDIT) {
        lxp_bridge_credit credit;
        lxp_bridge_profile profile;
        FILE *file = fopen(getenv("BUDGET_CREDIT_PROFILE"), "rb");
        REQUIRE(file != NULL && fread(profile.bytes, 1U, sizeof(profile.bytes), file) == sizeof(profile.bytes));
        REQUIRE(fgetc(file) == EOF && fclose(file) == 0);
        REQUIRE(lxp_bridge_credit_parse(payload, payload_length, &credit) == LXP_OK);
        uint64_t seconds = load_u64(credit.proof + 29U);
        REQUIRE(seconds <= UINT64_MAX / 1000U);
        REQUIRE(lxp_bridge_credit_verify(&profile, &credit, NETWORK_ID, 3U, NULL,
            seconds * 1000U, activity.idempotency_key, NULL) == LXP_OK);
    }
    activity.payload = (lxp_byte_span){payload, payload_length};
    if (lxp_hash_payload(payload, payload_length, activity.payload_hash) !=
            LXP_OK ||
        lxp_activity_signing_preimage(&activity, preimage) != LXP_OK ||
        sign_raw(key, preimage, sizeof(preimage), signature) != 0)
        return 1;
    activity.signature = (lxp_byte_span){signature, sizeof(signature)};
    arena_storage = (uint8_t *)malloc(LXP_MAX_ACTIVITY_BYTES);
    if (arena_storage == NULL) return 1;
    if (lxp_arena_init(&arena, arena_storage, LXP_MAX_ACTIVITY_BYTES) !=
            LXP_OK ||
        lxp_activity_encode(&activity, &arena, &encoded) != LXP_OK ||
        encoded.length > capacity) {
        free(arena_storage);
        return 1;
    }
    (void)memcpy(output, encoded.bytes, encoded.length);
    *length = encoded.length;
    free(arena_storage);
    return 0;
}

static int budget_submit(int fd, const signer *actor, uint64_t sequence, uint32_t type,
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
        REQUIRE(build_budget_activity(actor, sequence, type, 0U, payload, payload_length, encoded, sizeof(encoded), &length) == 0);
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
            lxp_receipt receipt; lxp_arena arena;
            static uint8_t memory[2U * LXP_MAX_ACTIVITY_BYTES];
            REQUIRE(lxp_arena_init(&arena, memory, sizeof(memory)) == LXP_OK);
            REQUIRE(lxp_receipt_decode(response.payload, response.payload_length, true, &receipt) == LXP_OK);
            REQUIRE(lxp_receipt_verify(&receipt, sequencer.public_key, &arena) == LXP_OK);
            REQUIRE(memcmp(receipt.activity_id, id, 32U) == 0);
            printf("receipt ordinal=%u sequence=%llu result=%d root=", type & 65535U, (unsigned long long)receipt.global_sequence, (int)receipt.result_code);
            print_hex(receipt.resulting_state_root, 32U); printf(" id="); print_hex(id, 32U);
            printf(" batch="); print_hex(receipt.batch_id, 32U);
            uint8_t digest[32]; REQUIRE(lxp_receipt_digest(&receipt, &arena, digest) == LXP_OK);
            printf(" digest="); print_hex(digest, 32U); printf(" raw="); print_hex(response.payload, response.payload_length); puts("");
            REQUIRE(receipt.result_code == expected);
            release_envelope(&response); return 0;
        }
        release_envelope(&response);
        const struct timespec delay = {0, 50000000}; REQUIRE(nanosleep(&delay, NULL) == 0);
    }
    return 1;
}

int main(int argc, char **argv)
{
    REQUIRE(argc == 8);
    signer owner, bob;
    uint8_t did[76], bob_did[76], issuer[32], asset[32], salt[32], source[32], recipient[32], budget[32], object[32] = {0x0dU, 0xefU, 1U};
    REQUIRE(fixture_signer(&owner, "treasury") == 0 && fixture_signer(&bob, "bob") == 0);
    actor_did(&owner, did); actor_did(&bob, bob_did);
    REQUIRE(lxp_did_id_derive(did, 75U, issuer) == LXP_OK);
    FILE *file = fopen(argv[2], "rb"); REQUIRE(file != NULL && fread(salt, 1U, 32U, file) == 32U && fclose(file) == 0);
    lxp_hash_context hash; lxp_hash_init(&hash);
    REQUIRE(lxp_hash_update(&hash, "LX:ASSET:v1", 11U) == LXP_OK && lxp_hash_update(&hash, issuer, 32U) == LXP_OK && lxp_hash_update(&hash, salt, 32U) == LXP_OK && lxp_hash_final(&hash, asset) == LXP_OK);
    REQUIRE(account_id(did, asset, source) == 0 && account_id(bob_did, asset, recipient) == 0);
    if (strstr(argv[3], "close") != NULL) object[31] = 2U;
    char name[256], hex[65]; for (size_t i=0;i<32;i++) snprintf(hex+i*2,3,"%02x",object[i]);
    int n=snprintf(name,sizeof(name),"agent:%s:budget:%s",did,hex);
    REQUIRE(n>0 && (size_t)n<sizeof(name) && lx_account_id_from_string((uint8_t *)name,(size_t)n,budget)==LXP_OK);
    struct sockaddr_un address = {.sun_family = AF_UNIX}; REQUIRE(strlen(argv[1]) < sizeof(address.sun_path)); strcpy(address.sun_path,argv[1]);
    int fd=socket(AF_UNIX,SOCK_STREAM,0); REQUIRE(fd>=0 && connect(fd,(struct sockaddr *)&address,sizeof(address))==0);
    REQUIRE(handshake(fd)==0);
    if (strncmp(argv[3],"read",4)==0) {
        uint8_t key[LX_BUDGET_STATE_KEY_BYTES]; REQUIRE(lx_budget_state_key(object,key)==LXP_OK);
        REQUIRE(budget_read(fd,LXP_MODULE_BUDGET,key,sizeof(key),"budget")==0);
        uint8_t account[33]={4U};
        memcpy(account+1,source,32); REQUIRE(budget_read(fd,0,account,sizeof(account),"owner")==0);
        memcpy(account+1,recipient,32); REQUIRE(budget_read(fd,0,account,sizeof(account),"recipient")==0);
        memcpy(account+1,budget,32); REQUIRE(budget_read(fd,0,account,sizeof(account),"custody")==0);
        REQUIRE(close(fd)==0); return 0;
    }
    uint64_t sequence=strtoull(argv[4],NULL,10), amount=strtoull(argv[5],NULL,10);
    static uint8_t payload[LXP_MAX_PAYLOAD_BYTES]={0U,1U}; memcpy(payload+2,object,32);
    uint32_t type=LX_BUDGET_DEFUND; size_t length=LX_BUDGET_DEFUND_PAYLOAD_BYTES;
    if (strncmp(argv[3], "credit-", 7U) == 0) {
        type = LXP_BRIDGE_CREDIT;
        FILE *credit_file = fopen(getenv("BUDGET_CREDIT_FILE"), "rb");
        REQUIRE(credit_file != NULL);
        length = fread(payload, 1U, sizeof(payload), credit_file);
        REQUIRE(length > 363U && fgetc(credit_file) == EOF && fclose(credit_file) == 0);
    } else if (strncmp(argv[3],"create",6)==0) {
        type=LX_BUDGET_CREATE; length=LX_BUDGET_CREATE_V2_PAYLOAD_BYTES; payload[1]=2U;
        memcpy(payload+34,budget,32); memcpy(payload+66,asset,32); payload[98]=0x55U;
        REQUIRE(lxp_u128_to_be((lxp_u128){0,200},payload+130)==LXP_OK);
        REQUIRE(lxp_u128_to_be((lxp_u128){0,50},payload+146)==LXP_OK);
        REQUIRE(lxp_u128_to_be((lxp_u128){0,amount},payload+162)==LXP_OK);
        struct timespec now; REQUIRE(clock_gettime(CLOCK_REALTIME,&now)==0);
        uint64_t start=(uint64_t)now.tv_sec*1000U;
        store_u64(payload+178,30000U); store_u64(payload+186,start); store_u64(payload+194,start+3600000U);
        store_u64(payload+202,1U); payload[210]=LX_BUDGET_ROLLOVER_CAPPED;
        memcpy(payload+211,source,32); store_u64(payload+243,strtoull(getenv("BUDGET_SOURCE_SEQUENCE"),NULL,10));
    } else if (strcmp(argv[3],"spend")==0) {
        type=LX_BUDGET_SPEND; length=LX_BUDGET_SPEND_PAYLOAD_BYTES; memcpy(payload+34,recipient,32);
        REQUIRE(lxp_u128_to_be((lxp_u128){0,amount},payload+66)==LXP_OK);
    } else if (strstr(argv[3],"revoke")!=NULL || strcmp(argv[3],"close")==0) {
        type=strcmp(argv[3],"close")==0?LX_BUDGET_CLOSE:LX_BUDGET_REVOKE;
        length=LX_BUDGET_REVOKE_PAYLOAD_BYTES; store_u64(payload+34,amount);
    } else {
        REQUIRE(lxp_u128_to_be((lxp_u128){strcmp(argv[3],"overflow")==0?UINT64_MAX:0U,amount},payload+34)==LXP_OK);
    }
    bool unauthorized=strncmp(argv[3],"unauthorized",12)==0 || strcmp(argv[3],"credit-bob")==0;
    memcpy(REGISTERED_DID,unauthorized?bob_did:did,76);
    REQUIRE(budget_submit(fd,unauthorized?&bob:&owner,sequence,type,payload,length,argv[6],strcmp(argv[3],"replay")==0,atoi(argv[7]))==0);
    REQUIRE(close(fd)==0); return 0;
}
