#define main program_admission_main
#include "lxp_test_program_admission.c"
#undef main
#include "layerx/lx_asset.h"
#include "layerx/lx_stream.h"
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


#include "layerx/lx_perps.h"
#include "layerx/lx_oracle.h"
#include "layerx/lxp_bridge_credit.h"
#include "layerx/lxp_state_proof.h"

static int oracle_read(int fd, uint16_t module, const uint8_t *key, size_t key_length, const char *label)
{
    uint8_t query[512] = {0U, 1U, 4U};
    wire_envelope response;
    store_u16(query + 3U, module); store_u16(query + 5U, (uint16_t)key_length);
    memcpy(query + 7U, key, key_length); query[7U + key_length] = 1U; query[8U + key_length] = 3U;
    REQUIRE(send_request(fd, 5U, 7U, 91U, query, key_length + 9U) == 0);
    REQUIRE(receive_envelope(fd, &response) == 0);
    REQUIRE(response.tag == 8U && response.correlation_id == 91U && response.proof_length > 8U);
    const uint8_t *p = response.proof;
    REQUIRE(load_u16(p) == 1U && p[2] == 4U && p[3] == 1U);
    size_t length = load_u32(p + 4U);
    REQUIRE(length < response.proof_length - 8U);
    lxp_state_witness *witness = calloc(1U, sizeof(*witness));
    REQUIRE(witness != NULL && lxp_state_proof_decode(p + 8U, length, witness) == LXP_OK);
    REQUIRE(witness->module_id == module && witness->key_length == key_length && memcmp(witness->key, key, key_length) == 0);
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

static int oracle_preparation(int fd, const uint8_t did[76])
{
    uint8_t request[79] = {0U, 1U, 0U, 75U};
    wire_envelope response;
    memcpy(request + 4U, did, 75U);
    REQUIRE(send_request(fd, 5U, 26U, 93U, request, sizeof(request)) == 0);
    REQUIRE(receive_envelope(fd, &response) == 0);
    REQUIRE(response.tag == 27U && response.correlation_id == 93U &&
        response.payload_length >= 157U && response.proof_length == 0U);
    const uint8_t *value = response.payload;
    REQUIRE(load_u16(value) == 1U && load_u16(value + 2U) == 75U &&
        memcmp(value + 4U, did, 75U) == 0 && load_u32(value + 79U) == NETWORK_ID);
    printf("preparation actor_sequence=%llu timestamp=%llu head=%llu root=",
        (unsigned long long)load_u64(value + 83U),
        (unsigned long long)load_u64(value + 91U),
        (unsigned long long)load_u64(value + 99U));
    print_hex(value + 107U, 32U);
    puts("");
    release_envelope(&response);
    return 0;
}

static int build_oracle_activity(const signer *key, uint64_t account_sequence,
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
        FILE *file = fopen(getenv("ORACLE_CREDIT_PROFILE"), "rb");
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

static int submit_wire(int fd, const uint8_t *encoded, size_t length,
    const char *path, int expected, bool ingress, bool save)
{
    uint8_t id[32], query[33] = {1U};
    wire_envelope response;
    if (save) {
        FILE *file = fopen(path, "wx"); REQUIRE(file != NULL);
        REQUIRE(fwrite(encoded, 1U, length, file) == length && fclose(file) == 0);
    }
    REQUIRE(lxp_activity_id(encoded, length, id) == LXP_OK);
    REQUIRE(send_request(fd, 5U, 3U, 91U, encoded, length) == 0);
    REQUIRE(receive_envelope(fd, &response) == 0);
    if (ingress) {
        REQUIRE(response.tag == 25U && response.payload_length == 5U);
        int code = (int32_t)load_u32(response.payload + 1U);
        printf("refusal result=%d\n", code);
        REQUIRE(code == expected); release_envelope(&response); return 0;
    }
    if (response.tag == 25U && response.payload_length == 5U)
        fprintf(stderr,"unexpected ingress refusal=%d\n",(int32_t)load_u32(response.payload+1U));
    REQUIRE(response.tag == 4U && response.proof_length == 32U &&
        memcmp(response.proof,id,32U)==0 && response.payload_length==length &&
        memcmp(response.payload,encoded,length)==0);
    release_envelope(&response);
    memcpy(query + 1U,id,32U);
    signer sequencer; REQUIRE(fixture_signer(&sequencer,"sequencer")==0);
    for(unsigned attempt=0;attempt<400U;++attempt) {
        REQUIRE(send_request(fd,5U,5U,92U,query,sizeof(query))==0);
        REQUIRE(receive_envelope(fd,&response)==0 && response.tag==6U);
        if(response.payload_length) {
            lxp_receipt receipt; lxp_arena arena; static uint8_t memory[2U*LXP_MAX_ACTIVITY_BYTES];
            REQUIRE(lxp_arena_init(&arena,memory,sizeof(memory))==LXP_OK);
            REQUIRE(lxp_receipt_decode(response.payload,response.payload_length,true,&receipt)==LXP_OK);
            REQUIRE(lxp_receipt_verify(&receipt,sequencer.public_key,&arena)==LXP_OK);
            REQUIRE(memcmp(receipt.activity_id,id,32U)==0);
            printf("receipt ordinal=3 sequence=%llu result=%d module=%u root=",(unsigned long long)receipt.global_sequence,(int)receipt.result_code,receipt.module_version);
            print_hex(receipt.resulting_state_root,32U);printf(" id=");print_hex(id,32U);
            printf(" batch=");print_hex(receipt.batch_id,32U);
            uint8_t digest[32];REQUIRE(lxp_receipt_digest(&receipt,&arena,digest)==LXP_OK);
            REQUIRE(receipt.fee_charged.hi==0U);printf(" fee=%llu",(unsigned long long)receipt.fee_charged.lo);printf(" digest=");print_hex(digest,32U);printf(" raw=");print_hex(response.payload,response.payload_length);puts("");
            REQUIRE(receipt.result_code==expected);release_envelope(&response);return 0;
        }
        release_envelope(&response);const struct timespec delay={0,50000000};REQUIRE(nanosleep(&delay,NULL)==0);
    }
    return 1;
}

typedef struct oracle_input {
    lx_oracle_observation observation;
    bool emitted;
    int fd;
    const char *path;
} oracle_input;
static lxp_result poll_owned_observation(void *opaque,lx_oracle_observation *observation,bool *available)
{
    oracle_input *input=opaque;*available=!input->emitted;
    if(*available){*observation=input->observation;input->emitted=true;}return LXP_OK;
}
static lxp_result submit_owned_observation(void *opaque,const uint8_t *bytes,size_t length)
{
    oracle_input *input=opaque;
    return submit_wire(input->fd,bytes,length,input->path,LXP_OK,false,true)==0?LXP_OK:LXP_ERR_IO;
}
static int named_account(const char *name,uint8_t id[32])
{
    REQUIRE(lx_account_id_from_string((const uint8_t *)name,strlen(name),id)==LXP_OK);return 0;
}
static int market_account(const uint8_t market[32],const char *prefix,const char *suffix,uint8_t id[32])
{
    char hex[65],name[128];for(size_t i=0;i<32U;++i)snprintf(hex+2U*i,3U,"%02x",market[i]);
    REQUIRE(snprintf(name,sizeof(name),"%s%s%s",prefix,hex,suffix)>0);return named_account(name,id);
}
static int resign(lxp_activity *activity,const signer *key,uint8_t *out,size_t *length)
{
    uint8_t digest[32],signature[64];lxp_arena arena; lxp_byte_span encoded;
    static uint8_t memory[2U*LXP_MAX_ACTIVITY_BYTES];
    REQUIRE(lxp_hash_payload(activity->payload.bytes,activity->payload.length,activity->payload_hash)==LXP_OK);
    REQUIRE(lxp_activity_signing_preimage(activity,digest)==LXP_OK && sign_raw(key,digest,32U,signature)==0);
    activity->signature=(lxp_byte_span){signature,64U};
    REQUIRE(lxp_arena_init(&arena,memory,sizeof(memory))==LXP_OK && lxp_activity_encode(activity,&arena,&encoded)==LXP_OK);
    memcpy(out,encoded.bytes,encoded.length);*length=encoded.length;return 0;
}
int main(int argc,char **argv)
{
    REQUIRE(argc==8);
    const char *operation=argv[3];uint64_t sequence=strtoull(argv[4],NULL,10);
    int expected=atoi(argv[7]);bool ingress=strncmp(operation,"ingress-",8U)==0;
    signer owner,bob;uint8_t did[76],bob_did[76],asset[32],source[32],insurance[32],market_id[32]={0x44U,0x04U};
    REQUIRE(fixture_signer(&owner,"treasury")==0 && fixture_signer(&bob,"bob")==0);
    actor_did(&owner,did);actor_did(&bob,bob_did);memcpy(REGISTERED_DID,did,76U);
    const char *asset_hex=getenv("ORACLE_ASSET");REQUIRE(asset_hex!=NULL && strlen(asset_hex)==64U);
    for(size_t i=0;i<32U;++i){unsigned byte;REQUIRE(sscanf(asset_hex+2U*i,"%2x",&byte)==1);asset[i]=(uint8_t)byte;}
    char name[LX_ACCOUNT_NAME_MAX+1U];REQUIRE(snprintf(name,sizeof(name),"agent:%s:main",did)>0 && named_account(name,source)==0);
    uint8_t stream_id[32]={0x44U,0x04U,0x01U},stream_account[32];char stream_hex[65];
    for(size_t i=0;i<32U;++i)snprintf(stream_hex+2U*i,3U,"%02x",stream_id[i]);
    int stream_name_length=snprintf(name,sizeof(name),"agent:%s:stream:%s",did,stream_hex);
    REQUIRE(stream_name_length>0 && (size_t)stream_name_length<sizeof(name) && named_account(name,stream_account)==0);
    REQUIRE(named_account("system:insurance",insurance)==0);
    struct sockaddr_un address={.sun_family=AF_UNIX};REQUIRE(strlen(argv[1])<sizeof(address.sun_path));strcpy(address.sun_path,argv[1]);
    int fd=socket(AF_UNIX,SOCK_STREAM,0);REQUIRE(fd>=0 && connect(fd,(struct sockaddr *)&address,sizeof(address))==0 && handshake(fd)==0);
    if(strcmp(operation,"read-bob")==0 || strcmp(operation,"read-bob-state")==0){REQUIRE(snprintf(name,sizeof(name),"agent:%s:main",bob_did)>0 && named_account(name,source)==0);}
    if(strncmp(operation,"read",4U)==0){
        uint8_t key[39];memcpy(key,"oracle:",7U);memcpy(key+7U,market_id,32U);
        if(strcmp(operation,"read-state")==0 || strcmp(operation,"read-bob-state")==0)REQUIRE(oracle_read(fd,LXP_MODULE_PERPS,key,sizeof(key),"oracle")==0);
        uint8_t account[33]={4U};memcpy(account+1U,source,32U);REQUIRE(oracle_read(fd,0U,account,sizeof(account),"owner")==0);
        memcpy(account+1U,insurance,32U);REQUIRE(oracle_read(fd,0U,account,sizeof(account),"insurance")==0);
        if(strcmp(operation,"read-insurance-stream")==0){memcpy(account+1U,stream_account,32U);REQUIRE(oracle_read(fd,0U,account,sizeof(account),"stream")==0);}
        uint8_t fees[32];REQUIRE(named_account("system:fees",fees)==0);
        memcpy(account+1U,fees,32U);REQUIRE(oracle_read(fd,0U,account,sizeof(account),"fees")==0);
        REQUIRE(oracle_read(fd,0U,(const uint8_t *)"sequence",8U,"global_sequence")==0);
        REQUIRE(oracle_preparation(fd,strncmp(operation,"read-bob",8U)==0?bob_did:did)==0);
        REQUIRE(close(fd)==0);return 0;
    }
    static uint8_t payload[LXP_MAX_PAYLOAD_BYTES],wire[LXP_MAX_ACTIVITY_BYTES];size_t length=0U,wire_length=0U;
    uint32_t type=LX_PERPS_ORACLE_PUSH;
    if(strcmp(operation,"replay")==0){FILE *f=fopen(argv[6],"rb");REQUIRE(f!=NULL);wire_length=fread(wire,1U,sizeof(wire),f);REQUIRE(wire_length>0U && fgetc(f)==EOF && fclose(f)==0);REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,false,false)==0);}
    else if(strcmp(operation,"credit")==0 || strcmp(operation,"credit-bob")==0){
        signer *credit_actor=&owner;if(strcmp(operation,"credit-bob")==0){credit_actor=&bob;memcpy(REGISTERED_DID,bob_did,76U);}
        FILE *f=fopen(getenv("ORACLE_CREDIT_FILE"),"rb");REQUIRE(f!=NULL);length=fread(payload,1U,sizeof(payload),f);REQUIRE(length>363U && fgetc(f)==EOF && fclose(f)==0);
        REQUIRE(build_oracle_activity(credit_actor,sequence,LXP_BRIDGE_CREDIT,0U,payload,length,wire,sizeof(wire),&wire_length)==0);
        REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,false,true)==0);
    } else if(strcmp(operation,"insurance-open")==0){
        lx_stream_open_payload open={0};struct timespec now;
        REQUIRE(clock_gettime(CLOCK_REALTIME,&now)==0 && now.tv_sec>1);
        uint64_t start=(uint64_t)now.tv_sec*1000U-1000U;
        memcpy(open.record.stream_id,stream_id,32U);memcpy(open.record.stream_account,stream_account,32U);
        memcpy(open.record.recipient,insurance,32U);memcpy(open.record.asset_id,asset,32U);
        open.record.mode=LX_STREAM_MODE_TIME;open.record.rate=(lxp_u128){0U,10000U};open.record.rate_unit=1U;
        open.record.start_timestamp=start;open.record.end_timestamp=start+1U;
        open.record.total_cap=open.initial_funding=(lxp_u128){0U,10000U};
        REQUIRE(lx_stream_open_encode(&open,payload,sizeof(payload),&length)==LXP_OK);
        REQUIRE(build_oracle_activity(&owner,sequence,LX_STREAM_OPEN,0U,payload,length,wire,sizeof(wire),&wire_length)==0);
        REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,false,true)==0);
    } else if(strcmp(operation,"insurance")==0){
        lx_stream_keyed_payload settle={0};memcpy(settle.stream_id,stream_id,32U);
        memcpy(settle.idempotency_key,stream_id,32U);settle.idempotency_key[31]=0xa5U;
        REQUIRE(lx_stream_keyed_encode(&settle,payload,sizeof(payload),&length)==LXP_OK);
        REQUIRE(build_oracle_activity(&owner,sequence,LX_STREAM_SETTLE,0U,payload,length,wire,sizeof(wire),&wire_length)==0);
        REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,false,true)==0);
    } else if(strcmp(operation,"market")==0){
        lx_perps_market market={0};memcpy(market.market_id,market_id,32U);memcpy(market.quote_asset,asset,32U);
        REQUIRE(lxp_did_id_derive(did,75U,market.administrator)==LXP_OK);
        REQUIRE(market_account(market_id,"system:liquidity:","",market.liquidity_account_id)==0 && market_account(market_id,"system:funding:",":long",market.long_funding_account_id)==0 && market_account(market_id,"system:funding:",":short",market.short_funding_account_id)==0);
        memcpy(market.insurance_account_id,insurance,32U);market.contract_size=market.tick_size=market.lot_size=market.price_scale=(lxp_u128){0U,1U};
        market.initial_margin_ratio_bps=1000U;market.maintenance_margin_ratio_bps=500U;market.liquidation_fee_bps=10U;market.liquidator_share_bps=6000U;market.maximum_funding_rate_bps=1000U;
        market.maximum_deviation_basis_points=1000U;market.funding_interval_ms=1000U;market.maximum_oracle_staleness_ms=60000U;market.minimum_price=(lxp_u128){0U,50U};market.maximum_price=(lxp_u128){0U,200U};
        market.permitted_oracle_key_count=1U;memcpy(market.permitted_oracle_keys[0],owner.public_key,32U);market.parameter_version=1U;
        REQUIRE(lx_perps_market_encode(&market,payload)==LXP_OK);
        REQUIRE(build_oracle_activity(&owner,sequence,LX_PERPS_MARKET_CREATE,0U,payload,LX_PERPS_MARKET_BYTES,wire,sizeof(wire),&wire_length)==0);
        REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,false,true)==0);
    } else {
        struct timespec now;REQUIRE(clock_gettime(CLOCK_REALTIME,&now)==0);uint64_t timestamp=(uint64_t)now.tv_sec*1000U;
        lx_oracle_observation observation={0};memcpy(observation.market_id,market_id,32U);observation.observation_sequence=strtoull(argv[5],NULL,10);observation.price.lo=100U;observation.observed_at=timestamp;observation.source_identifier=1U;
        lx_oracle_adapter_config config={0};memcpy(config.oracle_private_key,owner.private_key,32U);config.network_id=NETWORK_ID;config.protocol_version=3U;config.transport_version=1U;config.actor_did=did;config.actor_did_length=75U;config.next_account_sequence=sequence;config.fee_limit.lo=10000U;config.maximum_observations=1U;config.not_before=timestamp;config.not_after=timestamp+300000U;
        if(strcmp(operation,"oracle")==0){
            printf("observation sequence=%llu price=100 time=%llu source=1 key=",(unsigned long long)observation.observation_sequence,(unsigned long long)timestamp);print_hex(owner.public_key,32U);puts("");
            oracle_input input={.observation=observation,.fd=fd,.path=argv[6]};config.poll_context=config.submit_context=&input;config.poll_crossverse=poll_owned_observation;config.submit_activity=submit_owned_observation;
            size_t submitted=0U;REQUIRE(lx_oracle_adapter_run(&config,&submitted)==LXP_OK && submitted==1U && config.next_account_sequence==sequence+1U);
        } else {
            if(strcmp(operation,"stale")==0)observation.observed_at=timestamp-60001U;
            if(strcmp(operation,"future")==0)observation.observed_at=timestamp+60000U;
            if(strcmp(operation,"bounds-low")==0)observation.price.lo=49U;
            if(strcmp(operation,"bounds-high")==0)observation.price.lo=201U;
            if(strcmp(operation,"deviation")==0)observation.price.lo=111U;
            if(strcmp(operation,"zero-price")==0)observation.price.lo=0U;
            if(strcmp(operation,"zero-sequence")==0)observation.observation_sequence=0U;
            if(strcmp(operation,"zero-time")==0)observation.observed_at=0U;
            if(strcmp(operation,"zero-source")==0)observation.source_identifier=0U;
            bool zero=strncmp(operation,"zero-",5U)==0;
            lx_oracle_observation canonical=observation;
            if(zero){canonical.price.lo=100U;canonical.observation_sequence=2U;canonical.observed_at=timestamp;canonical.source_identifier=1U;}
            REQUIRE(lx_oracle_observation_sign(&canonical,owner.private_key)==LXP_OK);
            REQUIRE(lx_oracle_transport_encode(&canonical,payload)==LXP_OK);length=LX_ORACLE_TRANSPORT_BYTES;
            if(strcmp(operation,"bad-inner")==0)payload[73U]^=1U;
            if(strcmp(operation,"key-mismatch")==0){REQUIRE(lx_oracle_observation_sign(&canonical,bob.private_key)==LXP_OK);memcpy(payload+73U,canonical.signature,64U);}
            if(strcmp(operation,"disallowed-key")==0){REQUIRE(lx_oracle_observation_sign(&canonical,bob.private_key)==LXP_OK);REQUIRE(lx_oracle_transport_encode(&canonical,payload)==LXP_OK);}
            if(strcmp(operation,"version-zero")==0)payload[0]=0U;
            if(strcmp(operation,"version-unknown")==0)payload[0]=2U;
            if(strcmp(operation,"truncated")==0)--length;
            if(strcmp(operation,"overlong")==0)payload[length++]=0U;
            if(strcmp(operation,"legacy72")==0 || strcmp(operation,"ingress-legacy-oracle")==0){memmove(payload,payload+1U,72U);length=72U;}
            if(strcmp(operation,"zero-market")==0)memset(payload+1U,0,32U);
            if(zero){if(strcmp(operation,"zero-price")==0)memset(payload+41U,0,16U);if(strcmp(operation,"zero-sequence")==0)memset(payload+33U,0,8U);if(strcmp(operation,"zero-time")==0)memset(payload+57U,0,8U);if(strcmp(operation,"zero-source")==0)memset(payload+65U,0,8U);}
            signer *actor=strcmp(operation,"disallowed-key")==0?&bob:&owner;
            if(actor==&bob)memcpy(REGISTERED_DID,bob_did,76U);
            REQUIRE(build_oracle_activity(actor,sequence,type,0U,payload,length,wire,sizeof(wire),&wire_length)==0);
            lxp_activity activity;REQUIRE(lxp_activity_decode(wire,wire_length,&activity)==LXP_OK);
            if(strcmp(operation,"ingress-network")==0)activity.network_id=NETWORK_ID+1U;
            if(strcmp(operation,"ingress-protocol")==0)activity.protocol_version=2U;
            if(strcmp(operation,"ingress-actor")==0)activity.actor_did=(lxp_byte_span){(const uint8_t *)"did:layerx:unknown",18U};
            if(strcmp(operation,"ingress-expired")==0){activity.timestamp_bound.not_before=timestamp-600000U;activity.timestamp_bound.not_after=timestamp-300001U;}
            if(strcmp(operation,"fee")==0)activity.fee_limit.lo=0U;
            if(strcmp(operation,"fee-low-positive")==0)activity.fee_limit.lo=1U;
            if(strcmp(operation,"fee-unpayable")==0)activity.fee_limit=(lxp_u128){1U,0U};
            REQUIRE(resign(&activity,actor,wire,&wire_length)==0);
            if(strcmp(operation,"ingress-outer")==0)wire[wire_length-1U]^=1U;
            if(strcmp(operation,"ingress-legacy-oracle")==0){
                REQUIRE(lxp_activity_decode(wire,wire_length,&activity)==LXP_OK && activity.signature.length==64U);
                memcpy((uint8_t *)activity.signature.bytes,canonical.signature,64U);
            }
            REQUIRE(submit_wire(fd,wire,wire_length,argv[6],expected,ingress,true)==0);
        }
    }
    REQUIRE(close(fd)==0);return 0;
}
