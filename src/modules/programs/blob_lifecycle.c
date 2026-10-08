#include "blob_lifecycle.h"

#include "storage.h"

#include "layerx/lxp_crypto.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_state_proof.h"

#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

enum {
    CONTROL_PROFILE_BYTES = 147,
    CONTROL_DIGEST = 147,
    CONTROL_LAST_CUTOFF = 179,
    CONTROL_RETIREMENTS = 187,
    CONTROL_BLOBS = 195,
    CONTROL_BYTES = 203,
    CONTROL_REPLAYS = 211,
    CONTROL_OUTCOME = 219,
    HEADER_NETWORK = 4,
    HEADER_PROFILE_VERSION = 8,
    HEADER_PROFILE_DIGEST = 10,
    HEADER_CONTROLLER = 42,
    HEADER_EXPECTED_SEQUENCE = 74,
    HEADER_EXPECTED_ROOT = 82,
    HEADER_CUTOFF = 114,
    HEADER_CUTOFF_HEADER = 122,
    HEADER_ARCHIVE_MANIFEST = 154,
    HEADER_ARCHIVE_BYTES = 186,
    HEADER_ARCHIVE_FIRST = 194,
    HEADER_ARCHIVE_LAST = 202,
    HEADER_ARCHIVE_ROOT = 210,
    HEADER_SIGNATURE_A = 242,
    HEADER_SIGNATURE_B = 306,
    CANDIDATE_NAMESPACE = 4,
    CANDIDATE_DIGEST = 37,
    CANDIDATE_SIZE = 69,
    CANDIDATE_ORIGIN_SEQUENCE = 73,
    CANDIDATE_ORIGIN_ROOT = 81,
    CANDIDATE_REPLAY_ID = 113,
    CANDIDATE_PROOF_LENGTH = 145,
    CANDIDATE_MANIFEST_LENGTH = 147,
    NAMESPACE_BYTES = 33,
    STORAGE_PREFIX_BYTES = 8,
    SHARED_HEAD_KEY_BYTES = 41,
    PRINCIPAL_HEAD_KEY_BYTES = 73,
    HEAD_BYTES = 38,
    MANIFEST_FIXED_BYTES = 6,
    MANIFEST_CELL_FIXED_BYTES = 38,
    PAXAI_KEY_BYTES = 14,
    PAXAI_MANIFEST_BYTES = 58,
    REPLAY_PREFIX_BYTES = 14,
    REPLAY_DOMAIN_BYTES = 29,
    REPLAY_RECORD_BYTES = 394,
    HEADER_FRAME_BYTES = 226,
    CANDIDATE_FRAME_BYTES = 117,
    SCAN_ENTRY_GAS = 64,
    STAGED_ENTRY_LIMIT = 4
};

static const uint8_t control_key[] = "progretire/v1";
static const uint8_t control_family[] = "progretire";
static const uint8_t control_magic[5] = {'L', 'X', 'R', 'T', '1'};
static const uint8_t storage_prefix[STORAGE_PREFIX_BYTES] = {
    'p', 'r', 'o', 'g', 's', 't', 'o', 'r'};
static const uint8_t replay_family[] = "progreplay";
static const uint8_t replay_prefix[] = "progreplay/v1/";
static const uint8_t paxai_state_key[] = "paxai/state/v1";
static const uint8_t frame_magic[4] = {'L', 'X', 'R', 'T'};
static const uint8_t profile_domain[] = "LXP/programs-retirement-profile/v1";
static const uint8_t candidates_domain[] =
    "LXP/programs-retirement-candidates/v1";
static const uint8_t certificate_domain[] =
    "LXP/programs-retirement-archive/v1";
static const uint8_t outcome_domain[] = "LXP/programs-retirement-outcome/v1";

typedef struct retirement_candidate {
    const uint8_t *fixed;
    uint8_t class_id;
    uint32_t size;
    uint64_t origin_sequence;
    const uint8_t *proof;
    uint16_t proof_length;
    const uint8_t *manifest;
    uint16_t manifest_length;
} retirement_candidate;

typedef struct retirement_request {
    const uint8_t *header;
    size_t length;
    uint8_t count;
    uint32_t network_id;
    uint16_t profile_version;
    uint64_t expected_sequence;
    uint64_t cutoff_sequence;
    uint64_t archive_first;
    uint64_t archive_last;
    retirement_candidate candidates[LXP_PROGRAMS_RETIREMENT_MAX_CANDIDATES];
    uint8_t candidate_set_digest[32];
    uint8_t certificate_digest[32];
} retirement_request;

typedef struct retirement_control {
    lxp_programs_retirement_profile profile;
    uint8_t record[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
} retirement_control;

typedef struct reference_scan {
    lxp_module_ctx *ctx;
    const retirement_request *request;
    bool referenced[LXP_PROGRAMS_RETIREMENT_MAX_CANDIDATES];
    uint64_t work;
} reference_scan;

static uint16_t read_u16(const uint8_t *p)
{
    return (uint16_t)(((uint16_t)p[0] << 8U) | p[1]);
}

static uint32_t read_u32(const uint8_t *p)
{
    return ((uint32_t)p[0] << 24U) | ((uint32_t)p[1] << 16U) |
           ((uint32_t)p[2] << 8U) | p[3];
}

static uint64_t read_u64(const uint8_t *p)
{
    return ((uint64_t)read_u32(p) << 32U) | read_u32(p + 4U);
}

static void write_u16(uint8_t *p, uint16_t v)
{
    p[0] = (uint8_t)(v >> 8U);
    p[1] = (uint8_t)v;
}

static void write_u32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)(v >> 24U);
    p[1] = (uint8_t)(v >> 16U);
    p[2] = (uint8_t)(v >> 8U);
    p[3] = (uint8_t)v;
}

static void write_u64(uint8_t *p, uint64_t v)
{
    write_u32(p, (uint32_t)(v >> 32U));
    write_u32(p + 4U, (uint32_t)v);
}

static bool has_prefix(const uint8_t *key, size_t key_length,
                       const uint8_t *prefix, size_t prefix_length)
{
    return key_length >= prefix_length &&
           memcmp(key, prefix, prefix_length) == 0;
}

static bool contains_digest(const uint8_t *bytes, size_t length,
                            const uint8_t digest[32])
{
    size_t offset;
    for (offset = 0U; length >= 32U && offset <= length - 32U; ++offset)
        if (memcmp(bytes + offset, digest, 32U) == 0) return true;
    return false;
}

static int key_compare(const uint8_t *a, size_t an, const uint8_t *b,
                       size_t bn)
{
    const int compared = memcmp(a, b, an < bn ? an : bn);
    if (compared != 0) return compared;
    return an < bn ? -1 : an > bn ? 1 : 0;
}

static const uint8_t *candidate_identity(const retirement_candidate *c)
{
    return c->fixed + (c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY ?
                       CANDIDATE_REPLAY_ID : CANDIDATE_DIGEST);
}

static int candidate_compare(const retirement_candidate *a,
                             const retirement_candidate *b)
{
    if (a->class_id != b->class_id) return a->class_id < b->class_id ? -1 : 1;
    return memcmp(candidate_identity(a), candidate_identity(b), 32U);
}

static lxp_result candidate_parse(const uint8_t *payload, size_t length,
                                  size_t *cursor, retirement_candidate *c)
{
    const uint8_t *fixed;
    if (length - *cursor < LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES)
        return LXP_ERR_TRUNCATED;
    fixed = payload + *cursor;
    *cursor += LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES;
    c->fixed = fixed;
    c->class_id = fixed[0];
    c->size = read_u32(fixed + CANDIDATE_SIZE);
    c->origin_sequence = read_u64(fixed + CANDIDATE_ORIGIN_SEQUENCE);
    c->proof_length = read_u16(fixed + CANDIDATE_PROOF_LENGTH);
    c->manifest_length = read_u16(fixed + CANDIDATE_MANIFEST_LENGTH);
    if (c->class_id < LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST ||
        c->class_id > LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY)
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (read_u16(fixed + 2U) != 0U ||
        lxp_ct_is_zero(fixed + CANDIDATE_DIGEST, 32U) || c->size == 0U ||
        c->size > LXP_KERNEL_MAX_BLOB_BYTES || c->origin_sequence == 0U ||
        lxp_ct_is_zero(fixed + CANDIDATE_ORIGIN_ROOT, 32U) ||
        c->proof_length == 0U)
        return LXP_ERR_NON_CANONICAL;
    if (c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY) {
        if (fixed[1] != 0U ||
            !lxp_ct_is_zero(fixed + CANDIDATE_NAMESPACE, NAMESPACE_BYTES) ||
            lxp_ct_is_zero(fixed + CANDIDATE_REPLAY_ID, 32U) ||
            c->manifest_length != 0U)
            return LXP_ERR_NON_CANONICAL;
    } else if (fixed[1] != NAMESPACE_BYTES ||
               lxp_ct_is_zero(fixed + CANDIDATE_NAMESPACE, 32U) ||
               fixed[CANDIDATE_NAMESPACE + 32U] != 1U ||
               !lxp_ct_is_zero(fixed + CANDIDATE_REPLAY_ID, 32U) ||
               c->manifest_length < MANIFEST_FIXED_BYTES) {
        return LXP_ERR_NON_CANONICAL;
    }
    if (length - *cursor < (size_t)c->proof_length + c->manifest_length)
        return LXP_ERR_TRUNCATED;
    c->proof = payload + *cursor;
    *cursor += c->proof_length;
    c->manifest = c->manifest_length == 0U ? NULL : payload + *cursor;
    *cursor += c->manifest_length;
    return LXP_OK;
}

static lxp_result certificate_digest(const retirement_request *request,
                                     uint8_t digest[32])
{
    static const size_t fields[][2] = {
        {HEADER_NETWORK, 4U}, {HEADER_PROFILE_DIGEST, 32U},
        {HEADER_ARCHIVE_MANIFEST, 32U}, {HEADER_ARCHIVE_BYTES, 8U},
        {HEADER_ARCHIVE_FIRST, 8U}, {HEADER_ARCHIVE_LAST, 8U},
        {HEADER_ARCHIVE_ROOT, 32U}, {HEADER_CUTOFF_HEADER, 32U}};
    lxp_hash_context hash;
    size_t index;
    lxp_result status;
    lxp_hash_init(&hash);
    status = lxp_hash_update(&hash, certificate_domain,
                             sizeof(certificate_domain));
    for (index = 0U; status == LXP_OK &&
                     index < sizeof(fields) / sizeof(fields[0]); ++index)
        status = lxp_hash_update(&hash, request->header + fields[index][0],
                                 fields[index][1]);
    if (status == LXP_OK)
        status = lxp_hash_update(&hash, request->candidate_set_digest, 32U);
    return status == LXP_OK ? lxp_hash_final(&hash, digest) : status;
}

static lxp_result request_parse(const uint8_t *payload, size_t length,
                                retirement_request *request)
{
    size_t cursor = LXP_PROGRAMS_RETIREMENT_HEADER_BYTES;
    lxp_hash_context hash;
    uint8_t i, j;
    lxp_result status;
    if (payload == NULL || request == NULL) return LXP_ERR_NON_CANONICAL;
    if (length > LXP_PROGRAMS_RETIREMENT_MAX_PAYLOAD_BYTES)
        return LXP_ERR_LENGTH_LIMIT;
    if (length < LXP_PROGRAMS_RETIREMENT_HEADER_BYTES) return LXP_ERR_TRUNCATED;
    if (payload[0] != LXP_PROGRAMS_RETIREMENT_REQUEST_VERSION)
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (payload[1] > LXP_PROGRAMS_RETIREMENT_MAX_CANDIDATES)
        return LXP_ERR_LENGTH_LIMIT;
    if (payload[1] == 0U || read_u16(payload + 2U) != 0U ||
        lxp_ct_is_zero(payload + HEADER_CONTROLLER, 32U) ||
        lxp_ct_is_zero(payload + HEADER_CUTOFF_HEADER, 32U) ||
        lxp_ct_is_zero(payload + HEADER_ARCHIVE_MANIFEST, 32U) ||
        read_u64(payload + HEADER_ARCHIVE_BYTES) == 0U ||
        lxp_ct_is_zero(payload + HEADER_ARCHIVE_ROOT, 32U))
        return LXP_ERR_NON_CANONICAL;
    (void)memset(request, 0, sizeof(*request));
    request->header = payload;
    request->length = length;
    request->count = payload[1];
    request->network_id = read_u32(payload + HEADER_NETWORK);
    request->profile_version = read_u16(payload + HEADER_PROFILE_VERSION);
    request->expected_sequence = read_u64(payload + HEADER_EXPECTED_SEQUENCE);
    request->cutoff_sequence = read_u64(payload + HEADER_CUTOFF);
    request->archive_first = read_u64(payload + HEADER_ARCHIVE_FIRST);
    request->archive_last = read_u64(payload + HEADER_ARCHIVE_LAST);
    lxp_hash_init(&hash);
    status = lxp_hash_update(&hash, candidates_domain,
                             sizeof(candidates_domain));
    for (i = 0U; status == LXP_OK && i < request->count; ++i) {
        retirement_candidate *c = &request->candidates[i];
        status = candidate_parse(payload, length, &cursor, c);
        if (status != LXP_OK) return status;
        if (i != 0U) {
            const int order = candidate_compare(&request->candidates[i - 1U], c);
            if (order == 0) return LXP_ERR_DUPLICATE_ENTRY;
            if (order > 0) return LXP_ERR_UNSORTED_SEQUENCE;
        }
        status = lxp_hash_update(&hash, c->fixed, CANDIDATE_PROOF_LENGTH);
    }
    if (status != LXP_OK) return status;
    if (cursor != length) return LXP_ERR_TRAILING_BYTES;
    for (i = 0U; i < request->count; ++i)
        for (j = (uint8_t)(i + 1U); j < request->count; ++j) {
            const retirement_candidate *a = &request->candidates[i];
            const retirement_candidate *b = &request->candidates[j];
            if (memcmp(a->fixed + CANDIDATE_DIGEST, b->fixed + CANDIDATE_DIGEST,
                       32U) == 0 &&
                (a->class_id != LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY ||
                 b->class_id != LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY))
                return LXP_ERR_DUPLICATE_ENTRY;
        }
    status = lxp_hash_final(&hash, request->candidate_set_digest);
    if (status != LXP_OK) return status;
    return certificate_digest(request, request->certificate_digest);
}

lxp_result lxp_programs_retirement_certificate_digest(
    const uint8_t *payload, size_t length, uint8_t digest[32])
{
    retirement_request request;
    lxp_result status;
    if (digest == NULL) return LXP_ERR_NON_CANONICAL;
    status = request_parse(payload, length, &request);
    if (status == LXP_OK)
        (void)memcpy(digest, request.certificate_digest, 32U);
    return status;
}

static lxp_result profile_encode(const lxp_programs_retirement_profile *profile,
                                 uint8_t record[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES])
{
    lxp_hash_context hash;
    lxp_result status;
    if (profile == NULL) return LXP_ERR_NON_CANONICAL;
    if (profile->version != LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION)
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (profile->network_id == 0U || profile->hot_horizon == 0U ||
        lxp_ct_is_zero(profile->controller, 32U) ||
        lxp_ct_is_zero(profile->paxai_program_id, 32U) ||
        !lxp_ed25519_pubkey_is_canonical(profile->archive_keys[0]) ||
        !lxp_ed25519_pubkey_is_canonical(profile->archive_keys[1]) ||
        memcmp(profile->archive_keys[0], profile->archive_keys[1], 32U) == 0)
        return LXP_ERR_NON_CANONICAL;
    (void)memset(record, 0, LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES);
    (void)memcpy(record, control_magic, sizeof(control_magic));
    write_u16(record + 5U, profile->version);
    write_u32(record + 7U, profile->network_id);
    (void)memcpy(record + 11U, profile->controller, 32U);
    (void)memcpy(record + 43U, profile->archive_keys[0], 32U);
    (void)memcpy(record + 75U, profile->archive_keys[1], 32U);
    (void)memcpy(record + 107U, profile->paxai_program_id, 32U);
    write_u64(record + 139U, profile->hot_horizon);
    lxp_hash_init(&hash);
    status = lxp_hash_update(&hash, profile_domain, sizeof(profile_domain));
    if (status == LXP_OK)
        status = lxp_hash_update(&hash, record, CONTROL_PROFILE_BYTES);
    return status == LXP_OK ? lxp_hash_final(&hash, record + CONTROL_DIGEST) :
                              status;
}

lxp_result lxp_programs_retirement_profile_digest(
    const lxp_programs_retirement_profile *profile, uint8_t digest[32])
{
    uint8_t record[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
    lxp_result status;
    if (digest == NULL) return LXP_ERR_NON_CANONICAL;
    status = profile_encode(profile, record);
    if (status == LXP_OK)
        (void)memcpy(digest, record + CONTROL_DIGEST, 32U);
    return status;
}

lxp_result lxp_programs_retirement_profile_stage(
    lxp_module_ctx *ctx, const lxp_programs_retirement_profile *profile)
{
    uint8_t record[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
    const uint8_t *existing;
    size_t existing_length;
    lxp_result status;
    if (ctx == NULL || ctx->module_id != LXP_MODULE_PROGRAMS)
        return LXP_ERR_NON_CANONICAL;
    status = profile_encode(profile, record);
    if (status != LXP_OK) return status;
    status = lxp_ctx_kv_get(ctx, control_key, sizeof(control_key) - 1U,
                            &existing, &existing_length);
    if (status == LXP_OK) return LXP_ERR_DUPLICATE_ENTRY;
    if (status != LXP_ERR_UNKNOWN_FIELD) return status;
    return lxp_ctx_kv_put(ctx, control_key, sizeof(control_key) - 1U, record,
                          sizeof(record));
}

static lxp_result control_load(lxp_module_ctx *ctx, retirement_control *control)
{
    uint8_t expected[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
    const uint8_t *record;
    size_t length;
    lxp_programs_retirement_profile *profile = &control->profile;
    lxp_result status = lxp_ctx_kv_get(ctx, control_key, sizeof(control_key) - 1U,
                                       &record, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) return LXP_ERR_MODULE_DISABLED;
    if (status != LXP_OK) return status;
    if (length != LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES ||
        memcmp(record, control_magic, sizeof(control_magic)) != 0)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(control->record, record, length);
    profile->version = read_u16(record + 5U);
    profile->network_id = read_u32(record + 7U);
    (void)memcpy(profile->controller, record + 11U, 32U);
    (void)memcpy(profile->archive_keys[0], record + 43U, 32U);
    (void)memcpy(profile->archive_keys[1], record + 75U, 32U);
    (void)memcpy(profile->paxai_program_id, record + 107U, 32U);
    profile->hot_horizon = read_u64(record + 139U);
    status = profile_encode(profile, expected);
    if (status != LXP_OK ||
        memcmp(expected, record, CONTROL_LAST_CUTOFF) != 0)
        return LXP_FATAL_INVARIANT;
    return LXP_OK;
}

static lxp_result admit(lxp_module_ctx *ctx, const lxp_activity *activity,
                        const lxp_authority_resolved *authority,
                        const retirement_request *request,
                        retirement_control *control)
{
    const lxp_programs_retirement_profile *profile = &control->profile;
    const lxp_state_journal *journal;
    const uint8_t *header;
    uint8_t i;
    lxp_result status;
    if (ctx == NULL || ctx->kernel == NULL || activity == NULL ||
        authority == NULL || request == NULL ||
        ctx->module_id != LXP_MODULE_PROGRAMS)
        return LXP_ERR_NON_CANONICAL;
    status = control_load(ctx, control);
    if (status != LXP_OK) return status;
    header = request->header;
    if (request->network_id != profile->network_id ||
        activity->network_id != profile->network_id)
        return LXP_ERR_WRONG_NETWORK;
    if (request->profile_version != profile->version)
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (lxp_ct_memcmp(header + HEADER_PROFILE_DIGEST,
                      control->record + CONTROL_DIGEST, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (authority->kind != LXP_AUTHORITY_OWNER ||
        lxp_ct_memcmp(authority->principal, profile->controller, 32U) != 0 ||
        lxp_ct_memcmp(header + HEADER_CONTROLLER, profile->controller, 32U) != 0)
        return LXP_ERR_AUTH_SCOPE;
    if (request->expected_sequence != ctx->global_sequence)
        return LXP_ERR_SEQUENCE_MISMATCH;
    if (lxp_ct_memcmp(header + HEADER_EXPECTED_ROOT,
                      ctx->kernel->current_state_root, 32U) != 0)
        return LXP_ERR_ROOT_MISMATCH;
    journal = ctx->kernel->journal;
    if (ctx->staged_count != 0U || ctx->staged_blob_count != 0U ||
        journal == NULL || !journal->open ||
        journal->global_sequence != ctx->global_sequence)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (request->cutoff_sequence >= ctx->global_sequence)
        return LXP_ERR_NOT_YET_VALID;
    if (request->cutoff_sequence <
        read_u64(control->record + CONTROL_LAST_CUTOFF))
        return LXP_ERR_SEQUENCE_MISMATCH;
    if (request->archive_last != request->cutoff_sequence ||
        request->archive_first > request->archive_last)
        return LXP_ERR_CONTEXT_MISMATCH;
    for (i = 0U; i < request->count; ++i) {
        const uint64_t origin = request->candidates[i].origin_sequence;
        if (origin < request->archive_first || origin > request->archive_last)
            return LXP_ERR_CONTEXT_MISMATCH;
        if (profile->hot_horizon > request->cutoff_sequence ||
            origin > request->cutoff_sequence - profile->hot_horizon)
            return LXP_ERR_NOT_YET_VALID;
    }
    if (lxp_ed25519_verify_raw(profile->archive_keys[0],
                               header + HEADER_SIGNATURE_A,
                               request->certificate_digest, 32U) != LXP_OK ||
        lxp_ed25519_verify_raw(profile->archive_keys[1],
                               header + HEADER_SIGNATURE_B,
                               request->certificate_digest, 32U) != LXP_OK)
        return LXP_ERR_ATTESTATION_THRESHOLD;
    return LXP_OK;
}

static lxp_result live_blob_check(lxp_module_ctx *ctx,
                                  const retirement_candidate *c)
{
    const uint8_t *bytes;
    size_t length;
    lxp_result status = lxp_ctx_blob_get(ctx, c->fixed + CANDIDATE_DIGEST,
                                         &bytes, &length);
    if (status != LXP_OK) return status;
    return length == c->size ? LXP_OK : LXP_ERR_NON_CANONICAL;
}

static lxp_result replay_provenance(lxp_module_ctx *ctx,
                                    const retirement_control *control,
                                    const retirement_candidate *c,
                                    const lxp_state_witness *witness)
{
    uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES], blob_key[32];
    const uint8_t *record;
    const uint8_t *body;
    size_t length;
    lxp_result status;
    lxp_programs_replay_record_key(c->fixed + CANDIDATE_REPLAY_ID, key);
    if (witness->key_length != sizeof(key) ||
        memcmp(witness->key, key, sizeof(key)) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_ctx_kv_get(ctx, key, sizeof(key), &record, &length);
    if (status != LXP_OK) return status;
    if (witness->value_length != length ||
        memcmp(witness->value, record, length) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_programs_replay_record_blob_key(
        (lxp_byte_span){record, length}, blob_key);
    if (status != LXP_OK) return status;
    body = record + REPLAY_DOMAIN_BYTES;
    if (read_u32(body + 2U) != control->profile.network_id)
        return LXP_ERR_WRONG_NETWORK;
    if (lxp_ct_memcmp(blob_key, c->fixed + CANDIDATE_DIGEST, 32U) != 0 ||
        lxp_ct_memcmp(body + 6U, c->fixed + CANDIDATE_REPLAY_ID, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (read_u64(body + 38U) != c->origin_sequence)
        return LXP_ERR_SEQUENCE_MISMATCH;
    return live_blob_check(ctx, c);
}

static lxp_result storage_provenance(lxp_module_ctx *ctx,
                                     const retirement_control *control,
                                     const retirement_candidate *c,
                                     const lxp_state_witness *witness)
{
    uint8_t key[SHARED_HEAD_KEY_BYTES], digest[32];
    const uint8_t *m = c->manifest;
    const uint8_t *head = witness->value;
    lxp_result status;
    if (lxp_ct_memcmp(c->fixed + CANDIDATE_NAMESPACE,
                      control->profile.paxai_program_id, 32U) != 0)
        return LXP_ERR_AUTH_SCOPE;
    (void)memcpy(key, storage_prefix, STORAGE_PREFIX_BYTES);
    (void)memcpy(key + STORAGE_PREFIX_BYTES, c->fixed + CANDIDATE_NAMESPACE,
                 NAMESPACE_BYTES);
    if (witness->key_length != sizeof(key) ||
        memcmp(witness->key, key, sizeof(key)) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (witness->value_length != HEAD_BYTES || read_u16(head) != 1U ||
        read_u32(head + 2U) != 1U)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_hash_sha256(m, c->manifest_length, digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, head + 6U, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (c->manifest_length != PAXAI_MANIFEST_BYTES || read_u16(m) != 1U ||
        read_u32(m + 2U) != 1U || read_u16(m + 6U) != PAXAI_KEY_BYTES ||
        memcmp(m + 8U, paxai_state_key, PAXAI_KEY_BYTES) != 0 ||
        read_u32(m + 54U) > LX_PROGRAMS_STORAGE_MAX_VALUE_BYTES)
        return LXP_ERR_NON_CANONICAL;
    if (c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST) {
        if (lxp_ct_memcmp(c->fixed + CANDIDATE_DIGEST, head + 6U, 32U) != 0 ||
            c->size != c->manifest_length)
            return LXP_ERR_CONTEXT_MISMATCH;
    } else if (read_u32(m + 54U) == 0U ||
               lxp_ct_memcmp(c->fixed + CANDIDATE_DIGEST, m + 22U, 32U) != 0 ||
               c->size != read_u32(m + 54U)) {
        return LXP_ERR_CONTEXT_MISMATCH;
    }
    return live_blob_check(ctx, c);
}

static lxp_result candidate_provenance(lxp_module_ctx *ctx,
                                       const retirement_control *control,
                                       const retirement_candidate *c,
                                       lxp_state_witness *witness)
{
    lxp_result status = lxp_state_proof_decode(c->proof, c->proof_length,
                                               witness);
    if (status != LXP_OK) return status;
    if (lxp_state_proof_verify(witness, c->fixed + CANDIDATE_ORIGIN_ROOT) !=
        LXP_OK)
        return LXP_ERR_ROOT_MISMATCH;
    if (witness->module_id != LXP_MODULE_PROGRAMS)
        return LXP_ERR_CONTEXT_MISMATCH;
    return c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY ?
        replay_provenance(ctx, control, c, witness) :
        storage_provenance(ctx, control, c, witness);
}

static void scan_mark(reference_scan *scan, const uint8_t digest[32])
{
    uint8_t i;
    for (i = 0U; i < scan->request->count; ++i)
        if (memcmp(scan->request->candidates[i].fixed + CANDIDATE_DIGEST,
                   digest, 32U) == 0)
            scan->referenced[i] = true;
}

static lxp_result scan_storage_head(reference_scan *scan, const uint8_t *key,
                                    size_t key_length, const uint8_t *value,
                                    size_t value_length)
{
    const uint8_t *manifest;
    const uint8_t *previous = NULL;
    size_t manifest_length, cursor = MANIFEST_FIXED_BYTES;
    uint16_t previous_length = 0U;
    uint32_t count, cell;
    uint8_t digest[32];
    lxp_result status;
    const bool shared = key_length == SHARED_HEAD_KEY_BYTES && key[40] == 1U;
    const bool principal = key_length == PRINCIPAL_HEAD_KEY_BYTES &&
        (key[40] == 0U || key[40] == 2U) && !lxp_ct_is_zero(key + 41U, 32U);
    if ((!shared && !principal) ||
        lxp_ct_is_zero(key + STORAGE_PREFIX_BYTES, 32U))
        return LXP_ERR_VERSION_UNSUPPORTED;
    if (value_length != HEAD_BYTES || read_u16(value) != 1U)
        return LXP_ERR_NON_CANONICAL;
    scan_mark(scan, value + 6U);
    status = lxp_ctx_blob_get(scan->ctx, value + 6U, &manifest,
                              &manifest_length);
    if (status == LXP_ERR_UNKNOWN_FIELD) return LXP_ERR_NON_CANONICAL;
    if (status != LXP_OK) return status;
    status = lxp_hash_sha256(manifest, manifest_length, digest);
    if (status != LXP_OK) return status;
    count = read_u32(value + 2U);
    if (memcmp(digest, value + 6U, 32U) != 0 ||
        manifest_length < MANIFEST_FIXED_BYTES || read_u16(manifest) != 1U ||
        read_u32(manifest + 2U) != count)
        return LXP_ERR_NON_CANONICAL;
    scan->work += manifest_length;
    for (cell = 0U; cell < count; ++cell) {
        const uint8_t *cell_key, *cell_digest, *bytes;
        uint16_t cell_key_length;
        uint32_t cell_value_length;
        size_t stored;
        if (manifest_length - cursor < 2U) return LXP_ERR_NON_CANONICAL;
        cell_key_length = read_u16(manifest + cursor);
        cursor += 2U;
        if (cell_key_length == 0U ||
            cell_key_length > LX_PROGRAMS_STORAGE_MAX_KEY_BYTES ||
            manifest_length - cursor <
                (size_t)cell_key_length + MANIFEST_CELL_FIXED_BYTES - 2U)
            return LXP_ERR_NON_CANONICAL;
        cell_key = manifest + cursor;
        cell_digest = cell_key + cell_key_length;
        cell_value_length = read_u32(cell_digest + 32U);
        if ((previous != NULL &&
             key_compare(previous, previous_length, cell_key,
                         cell_key_length) >= 0) ||
            cell_value_length > LX_PROGRAMS_STORAGE_MAX_VALUE_BYTES)
            return LXP_ERR_NON_CANONICAL;
        if (cell_value_length != 0U) {
            status = lxp_ctx_blob_get(scan->ctx, cell_digest, &bytes, &stored);
            if (status == LXP_ERR_UNKNOWN_FIELD) return LXP_ERR_NON_CANONICAL;
            if (status != LXP_OK) return status;
            if (stored != cell_value_length) return LXP_ERR_NON_CANONICAL;
        }
        scan_mark(scan, cell_digest);
        previous = cell_key;
        previous_length = cell_key_length;
        cursor += (size_t)cell_key_length + MANIFEST_CELL_FIXED_BYTES - 2U;
    }
    return cursor == manifest_length ? LXP_OK : LXP_ERR_NON_CANONICAL;
}

static lxp_result scan_replay_record(reference_scan *scan, const uint8_t *key,
                                     size_t key_length, const uint8_t *value,
                                     size_t value_length)
{
    uint8_t witness[32];
    uint8_t i;
    if (key_length != LXP_PROGRAMS_REPLAY_KEY_BYTES ||
        memcmp(key, replay_prefix, REPLAY_PREFIX_BYTES) != 0)
        return LXP_ERR_VERSION_UNSUPPORTED;
    for (i = 0U; i < scan->request->count; ++i) {
        const retirement_candidate *c = &scan->request->candidates[i];
        if (c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY &&
            memcmp(key + REPLAY_PREFIX_BYTES, c->fixed + CANDIDATE_REPLAY_ID,
                   32U) == 0)
            return LXP_OK;
    }
    if (lxp_programs_replay_record_blob_key(
            (lxp_byte_span){value, value_length}, witness) != LXP_OK ||
        memcmp(value + REPLAY_DOMAIN_BYTES + 6U, key + REPLAY_PREFIX_BYTES,
               32U) != 0)
        return LXP_ERR_NON_CANONICAL;
    scan_mark(scan, witness);
    return LXP_OK;
}

static lxp_result scan_visit(const uint8_t *key, size_t key_length,
                             const uint8_t *value, size_t value_length,
                             void *user)
{
    reference_scan *scan = (reference_scan *)user;
    uint8_t i;
    scan->work += SCAN_ENTRY_GAS;
    if (has_prefix(key, key_length, control_family, sizeof(control_family) - 1U))
        return key_length == sizeof(control_key) - 1U &&
               memcmp(key, control_key, key_length) == 0 ?
               LXP_OK : LXP_ERR_VERSION_UNSUPPORTED;
    if (has_prefix(key, key_length, storage_prefix, STORAGE_PREFIX_BYTES))
        return scan_storage_head(scan, key, key_length, value, value_length);
    if (has_prefix(key, key_length, replay_family, sizeof(replay_family) - 1U))
        return scan_replay_record(scan, key, key_length, value, value_length);
    for (i = 0U; i < scan->request->count; ++i) {
        const uint8_t *digest =
            scan->request->candidates[i].fixed + CANDIDATE_DIGEST;
        if (contains_digest(key, key_length, digest) ||
            contains_digest(value, value_length, digest))
            scan->referenced[i] = true;
    }
    scan->work += value_length;
    return LXP_OK;
}

static void programs_totals(const lxp_kernel *kernel, uint32_t *blobs,
                            uint32_t *entries)
{
    size_t i;
    *blobs = 0U;
    *entries = 0U;
    for (i = 0U; i < kernel->blob_count; ++i)
        if (kernel->blobs[i].module_id == LXP_MODULE_PROGRAMS) ++*blobs;
    for (i = 0U; i < kernel->module_kv_count; ++i)
        if (kernel->module_kv[i].module_id == LXP_MODULE_PROGRAMS) ++*entries;
}

lxp_result lxp_programs_retirement_decode(lxp_module_ctx *ctx,
    const uint8_t *payload, size_t length, void **decoded)
{
    void *allocation;
    lxp_result status;
    if (ctx == NULL || decoded == NULL) return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(ctx, sizeof(retirement_request),
                                 _Alignof(retirement_request), &allocation);
    if (status != LXP_OK) return status;
    status = request_parse(payload, length, (retirement_request *)allocation);
    if (status == LXP_OK) *decoded = allocation;
    return status;
}

lxp_result lxp_programs_retirement_validate(lxp_module_ctx *ctx,
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    const void *decoded)
{
    const retirement_request *request = (const retirement_request *)decoded;
    retirement_control control;
    lxp_result status = admit(ctx, activity, authority, request, &control);
    return status == LXP_OK ? lxp_ctx_charge_gas(ctx, request->length) : status;
}

lxp_result lxp_programs_retirement_execute(lxp_module_ctx *ctx,
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    const void *decoded, lxp_effect_buffer *effects)
{
    const retirement_request *request = (const retirement_request *)decoded;
    retirement_control control;
    reference_scan scan;
    lxp_state_witness *witness;
    uint8_t header_frame[HEADER_FRAME_BYTES];
    uint8_t frames[LXP_PROGRAMS_RETIREMENT_MAX_CANDIDATES][CANDIDATE_FRAME_BYTES];
    uint8_t record[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
    uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
    bool deleted[LXP_PROGRAMS_RETIREMENT_MAX_CANDIDATES] = {false};
    uint32_t blobs_before, entries_before;
    uint64_t retired_bytes = 0U, work = 0U;
    uint8_t blob_deletions = 0U, replay_retirements = 0U, i, j;
    lxp_hash_context hash;
    lxp_result status;
    (void)effects;
    status = admit(ctx, activity, authority, request, &control);
    if (status != LXP_OK) return status;
    witness = (lxp_state_witness *)malloc(sizeof(*witness));
    if (witness == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    for (i = 0U; status == LXP_OK && i < request->count; ++i) {
        status = candidate_provenance(ctx, &control, &request->candidates[i],
                                      witness);
        work += (uint64_t)request->candidates[i].proof_length +
                request->candidates[i].manifest_length;
    }
    free(witness);
    if (status != LXP_OK) return status;
    (void)memset(&scan, 0, sizeof(scan));
    scan.ctx = ctx;
    scan.request = request;
    status = lxp_ctx_kv_iter(ctx, NULL, 0U, scan_visit, &scan);
    if (status != LXP_OK) return status;
    status = lxp_ctx_charge_gas(ctx, work + scan.work);
    if (status != LXP_OK) return status;
    for (i = 0U; i < request->count; ++i)
        if (request->candidates[i].class_id !=
                LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY && scan.referenced[i])
            return LXP_ERR_CONDITION_UNMET;
    programs_totals(ctx->kernel, &blobs_before, &entries_before);
    for (i = 0U; i < request->count; ++i) {
        const retirement_candidate *c = &request->candidates[i];
        bool shared = false;
        if (c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY) {
            lxp_programs_replay_record_key(c->fixed + CANDIDATE_REPLAY_ID, key);
            status = lxp_ctx_kv_del(ctx, key, sizeof(key));
            if (status != LXP_OK) return status;
            ++replay_retirements;
            if (scan.referenced[i]) continue;
        }
        for (j = 0U; j < i; ++j)
            if (deleted[j] && memcmp(request->candidates[j].fixed + CANDIDATE_DIGEST,
                                     c->fixed + CANDIDATE_DIGEST, 32U) == 0)
                shared = true;
        if (shared) continue;
        status = lxp_ctx_blob_del(ctx, c->fixed + CANDIDATE_DIGEST);
        if (status != LXP_OK) return status;
        deleted[i] = true;
        ++blob_deletions;
        retired_bytes += c->size;
    }
    if (ctx->staged_blob_count >= STAGED_ENTRY_LIMIT ||
        ctx->staged_count >= STAGED_ENTRY_LIMIT)
        return LXP_ERR_ARENA_EXHAUSTED;
    (void)memcpy(header_frame, frame_magic, sizeof(frame_magic));
    header_frame[4] = 0U;
    header_frame[5] = (uint8_t)(request->count + 1U);
    header_frame[6] = 1U;
    (void)memcpy(header_frame + 7U, ctx->activity_id, 32U);
    (void)memcpy(header_frame + 39U, request->header + HEADER_EXPECTED_ROOT, 32U);
    (void)memcpy(header_frame + 71U, control.record + CONTROL_DIGEST, 32U);
    (void)memcpy(header_frame + 103U, request->candidate_set_digest, 32U);
    (void)memcpy(header_frame + 135U, request->certificate_digest, 32U);
    (void)memcpy(header_frame + 167U, request->header + HEADER_ARCHIVE_MANIFEST,
                 32U);
    header_frame[199] = request->count;
    header_frame[200] = blob_deletions;
    header_frame[201] = replay_retirements;
    write_u64(header_frame + 202U, retired_bytes);
    write_u32(header_frame + 210U, blobs_before);
    write_u32(header_frame + 214U, blobs_before - blob_deletions);
    write_u32(header_frame + 218U, entries_before);
    write_u32(header_frame + 222U, entries_before - replay_retirements);
    lxp_hash_init(&hash);
    status = lxp_hash_update(&hash, outcome_domain, sizeof(outcome_domain));
    if (status == LXP_OK)
        status = lxp_hash_update(&hash, header_frame, sizeof(header_frame));
    for (i = 0U; status == LXP_OK && i < request->count; ++i) {
        const retirement_candidate *c = &request->candidates[i];
        uint8_t *frame = frames[i];
        (void)memcpy(frame, frame_magic, sizeof(frame_magic));
        frame[4] = (uint8_t)(i + 1U);
        frame[5] = (uint8_t)(request->count + 1U);
        frame[6] = 1U;
        frame[7] = c->class_id;
        frame[8] = deleted[i] ? 1U : 0U;
        (void)memcpy(frame + 9U, c->fixed + CANDIDATE_DIGEST, 32U);
        write_u32(frame + 41U, c->size);
        write_u64(frame + 45U, c->origin_sequence);
        (void)memcpy(frame + 53U, c->fixed + CANDIDATE_ORIGIN_ROOT, 32U);
        (void)memcpy(frame + 85U, c->fixed + CANDIDATE_REPLAY_ID, 32U);
        status = lxp_hash_update(&hash, frame, CANDIDATE_FRAME_BYTES);
    }
    if (status != LXP_OK) return status;
    (void)memcpy(record, control.record, sizeof(record));
    status = lxp_hash_final(&hash, record + CONTROL_OUTCOME);
    if (status != LXP_OK) return status;
    if (read_u64(record + CONTROL_RETIREMENTS) == UINT64_MAX ||
        read_u64(record + CONTROL_BLOBS) > UINT64_MAX - blob_deletions ||
        read_u64(record + CONTROL_BYTES) > UINT64_MAX - retired_bytes ||
        read_u64(record + CONTROL_REPLAYS) > UINT64_MAX - replay_retirements)
        return LXP_ERR_OVERFLOW;
    write_u64(record + CONTROL_LAST_CUTOFF, request->cutoff_sequence);
    write_u64(record + CONTROL_RETIREMENTS,
              read_u64(record + CONTROL_RETIREMENTS) + 1U);
    write_u64(record + CONTROL_BLOBS,
              read_u64(record + CONTROL_BLOBS) + blob_deletions);
    write_u64(record + CONTROL_BYTES,
              read_u64(record + CONTROL_BYTES) + retired_bytes);
    write_u64(record + CONTROL_REPLAYS,
              read_u64(record + CONTROL_REPLAYS) + replay_retirements);
    status = lxp_ctx_kv_put(ctx, control_key, sizeof(control_key) - 1U, record,
                            sizeof(record));
    if (status == LXP_OK)
        status = lxp_ctx_emit_event(ctx, LXP_PROGRAMS_RETIREMENT_EVENT,
                                    header_frame, sizeof(header_frame));
    for (i = 0U; status == LXP_OK && i < request->count; ++i)
        status = lxp_ctx_emit_event(ctx, LXP_PROGRAMS_RETIREMENT_EVENT,
                                    frames[i], CANDIDATE_FRAME_BYTES);
    return status;
}
