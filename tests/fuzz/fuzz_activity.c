#include "layerx/lxp_fuzz.h"

#include "layerx/lxp_admission.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_protocol.h"
#include "layerx/lxp_qualification.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum {
    FUZZ_CORPUS_HEADER_BYTES = 252,
    FUZZ_CORPUS_TYPE_BYTES = LXP_QUAL_ACTIVITY_TYPE_COUNT * 4,
    FUZZ_CORPUS_FIXED_RECORD_BYTES = 8 + 1 + 4 + 32 +
                                    LXP_QUAL_RECEIPT_BYTES +
                                    LXP_QUAL_EVENT_BYTES,
    FUZZ_CORPUS_BATCH_RECORD_BYTES = LXP_QUAL_BATCH_HEADER_BYTES + 32
};

static uint32_t load_u32(const uint8_t in[4])
{
    return ((uint32_t)in[0] << 24U) | ((uint32_t)in[1] << 16U) |
           ((uint32_t)in[2] << 8U) | (uint32_t)in[3];
}

static uint64_t load_u64(const uint8_t in[8])
{
    uint64_t value = 0U;
    size_t i;
    for (i = 0U; i < 8U; ++i) value = (value << 8U) | in[i];
    return value;
}

static lxp_result canonical_roundtrip(const uint8_t *data, size_t size,
                                      const lxp_activity *activity)
{
    uint8_t *storage = malloc(LXP_MAX_ACTIVITY_BYTES);
    lxp_arena arena;
    lxp_byte_span encoded;
    lxp_result status;
    if (storage == NULL) return LXP_ERR_IO;
    status = lxp_arena_init(&arena, storage, LXP_MAX_ACTIVITY_BYTES);
    if (status == LXP_OK)
        status = lxp_activity_encode(activity, &arena, &encoded);
    if (status == LXP_OK &&
        (encoded.length != size || memcmp(encoded.bytes, data, size) != 0))
        status = LXP_FATAL_REPLAY_DIVERGENCE;
    lxp_secure_zero(storage, LXP_MAX_ACTIVITY_BYTES);
    free(storage);
    return status;
}

lxp_result lxp_fuzz_activity_decode(const uint8_t *data, size_t size,
                                    lxp_result *decode_result)
{
    uint8_t state[32];
    uint8_t state_before[32];
    lxp_activity first;
    lxp_activity second;
    lxp_admission_context context;
    lxp_admission_result admission;
    lxp_result first_status;
    lxp_result second_status;
    size_t i;
    if (decode_result == NULL || (data == NULL && size != 0U))
        return LXP_ERR_NON_CANONICAL;
    for (i = 0U; i < sizeof(state); ++i) state[i] = (uint8_t)(0x80U + i);
    (void)memcpy(state_before, state, sizeof(state));
    (void)memset(&first, 0xa5, sizeof(first));
    (void)memset(&second, 0x5a, sizeof(second));
    first_status = lxp_activity_decode(data, size, &first);
    second_status = lxp_activity_decode(data, size, &second);
    if (first_status != second_status) return LXP_FATAL_REPLAY_DIVERGENCE;
    *decode_result = first_status;
    if (first_status != LXP_OK) {
        if (first_status != LXP_ERR_MALFORMED_ENVELOPE ||
            !lxp_ct_is_zero(&first, sizeof(first)) ||
            !lxp_ct_is_zero(&second, sizeof(second)))
            return LXP_FATAL_REPLAY_DIVERGENCE;
        return memcmp(state, state_before, sizeof(state)) == 0 ? LXP_OK :
               LXP_FATAL_REPLAY_DIVERGENCE;
    }
    if (memcmp(&first, &second, sizeof(first)) != 0)
        return LXP_FATAL_REPLAY_DIVERGENCE;
    if (canonical_roundtrip(data, size, &first) != LXP_OK)
        return LXP_FATAL_REPLAY_DIVERGENCE;
    (void)memset(&context, 0, sizeof(context));
    context.network_id = first.network_id;
    context.batch_timestamp = first.timestamp_bound.not_before;
    context.maximum_timestamp_window = UINT64_MAX;
    context.next_account_sequence = first.account_sequence;
    context.signature_valid = false;
    context.fee_limit_spendable = true;
    admission = lxp_admit_activity(&first, &context);
    if (admission.result_code == LXP_OK || admission.assign_global_sequence ||
        admission.consume_account_sequence || admission.charge_fee ||
        memcmp(state, state_before, sizeof(state)) != 0)
        return LXP_FATAL_REPLAY_DIVERGENCE;
    return LXP_OK;
}

static lxp_result write_seed(const char *directory, size_t index,
                             const uint8_t *bytes, size_t length)
{
    char path[4096];
    FILE *file;
    int path_length;
    path_length = snprintf(path, sizeof(path), "%s/activity-%02zu.bin",
                           directory, index);
    if (path_length < 0 || (size_t)path_length >= sizeof(path))
        return LXP_ERR_LENGTH_LIMIT;
    file = fopen(path, "wb");
    if (file == NULL) return LXP_ERR_IO;
    if (fwrite(bytes, 1U, length, file) != length || fclose(file) != 0)
        return LXP_ERR_IO;
    return LXP_OK;
}

lxp_result lxp_fuzz_corpus_seed(const char *qualification_corpus,
                                const char *seed_directory)
{
    uint8_t header[FUZZ_CORPUS_HEADER_BYTES];
    uint8_t types[FUZZ_CORPUS_TYPE_BYTES];
    uint32_t seen[LXP_QUAL_ACTIVITY_TYPE_COUNT];
    FILE *corpus;
    size_t index;
    lxp_result status = LXP_OK;
    if (qualification_corpus == NULL || seed_directory == NULL)
        return LXP_ERR_NON_CANONICAL;
    corpus = fopen(qualification_corpus, "rb");
    if (corpus == NULL) return LXP_ERR_IO;
    if (fread(header, 1U, sizeof(header), corpus) != sizeof(header) ||
        memcmp(header, "LXPQRP01", 8U) != 0 || load_u32(header + 8U) != 1U ||
        load_u64(header + 12U) < LXP_QUAL_ACTIVITY_TYPE_COUNT ||
        load_u32(header + 24U) != LXP_QUAL_ACTIVITY_TYPE_COUNT ||
        fread(types, 1U, sizeof(types), corpus) != sizeof(types))
        status = LXP_ERR_NON_CANONICAL;
    (void)memset(seen, 0, sizeof(seen));
    for (index = 0U; status == LXP_OK &&
         index < LXP_QUAL_ACTIVITY_TYPE_COUNT; ++index) {
        uint8_t record_prefix[13];
        uint8_t *activity;
        uint32_t length;
        uint8_t boundary;
        lxp_activity decoded;
        lxp_result decode_status;
        size_t prior;
        if (fread(record_prefix, 1U, sizeof(record_prefix), corpus) !=
            sizeof(record_prefix)) {
            status = LXP_ERR_IO;
            break;
        }
        length = load_u32(record_prefix + 9U);
        boundary = record_prefix[8];
        if (load_u64(record_prefix) != index + 1U || boundary > 1U ||
            length == 0U || length > LXP_MAX_ACTIVITY_BYTES) {
            status = LXP_ERR_NON_CANONICAL;
            break;
        }
        activity = malloc(length);
        if (activity == NULL || fread(activity, 1U, length, corpus) != length) {
            free(activity);
            status = LXP_ERR_IO;
            break;
        }
        status = lxp_fuzz_activity_decode(activity, length, &decode_status);
        if (status == LXP_OK && decode_status != LXP_OK)
            status = LXP_FATAL_REPLAY_DIVERGENCE;
        if (status == LXP_OK)
            status = lxp_activity_decode(activity, length, &decoded);
        for (prior = 0U; status == LXP_OK && prior < index; ++prior)
            if (seen[prior] == decoded.activity_type)
                status = LXP_FATAL_REPLAY_DIVERGENCE;
        if (status == LXP_OK) seen[index] = decoded.activity_type;
        if (status == LXP_OK)
            status = write_seed(seed_directory, index, activity, length);
        free(activity);
        if (status == LXP_OK) {
            long skip = (long)(32U + LXP_QUAL_RECEIPT_BYTES +
                               LXP_QUAL_EVENT_BYTES +
                               (boundary != 0U ?
                                FUZZ_CORPUS_BATCH_RECORD_BYTES : 0U));
            if (fseek(corpus, skip, SEEK_CUR) != 0) status = LXP_ERR_IO;
        }
    }
    if (fclose(corpus) != 0 && status == LXP_OK) status = LXP_ERR_IO;
    return status;
}
