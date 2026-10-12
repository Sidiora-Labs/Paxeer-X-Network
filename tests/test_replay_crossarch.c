#include "layerx/lxp_activity.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_replay_fixture.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const uint32_t expected_activity_types[] = {
    UINT32_C(0x00010001), UINT32_C(0x00010002), UINT32_C(0x00010003),
    UINT32_C(0x00010004), UINT32_C(0x00010005), UINT32_C(0x00010006),
    UINT32_C(0x00010007), UINT32_C(0x00010008),
    UINT32_C(0x00020001), UINT32_C(0x00020002), UINT32_C(0x00020003),
    UINT32_C(0x00020004), UINT32_C(0x00020005), UINT32_C(0x00020006),
    UINT32_C(0x00020007),
    UINT32_C(0x00030001), UINT32_C(0x00030002), UINT32_C(0x00030003),
    UINT32_C(0x00030004), UINT32_C(0x00030005), UINT32_C(0x00030006),
    UINT32_C(0x00030007), UINT32_C(0x00030008), UINT32_C(0x00030009),
    UINT32_C(0x0003000a), UINT32_C(0x0003000b),
    UINT32_C(0x00040001), UINT32_C(0x00040002), UINT32_C(0x00040003),
    UINT32_C(0x00040004), UINT32_C(0x00040005), UINT32_C(0x00040006),
    UINT32_C(0x00040007), UINT32_C(0x00040008),
    UINT32_C(0x00050001), UINT32_C(0x00050002), UINT32_C(0x00050003),
    UINT32_C(0x00050004), UINT32_C(0x00050005), UINT32_C(0x00050006),
    UINT32_C(0x00050007), UINT32_C(0x00050008), UINT32_C(0x00050009),
    UINT32_C(0x0005000a), UINT32_C(0x0005000b), UINT32_C(0x0005000c),
    UINT32_C(0x0005000d),
    UINT32_C(0x00060001), UINT32_C(0x00060002), UINT32_C(0x00060003),
    UINT32_C(0x00060004), UINT32_C(0x00060005), UINT32_C(0x00060006),
    UINT32_C(0x00060007), UINT32_C(0x00060008), UINT32_C(0x00060009),
    UINT32_C(0x0006000a), UINT32_C(0x0006000b)
};

enum {
    ORIGINAL_RECORD_COUNT = 53,
    ORIGINAL_RECORD_BYTES = 25228,
    FIXTURE_HEADER_BYTES = 80
};

static const uint8_t original_records_digest[32] = {
    0x91, 0x98, 0x24, 0x82, 0x17, 0x24, 0x9c, 0x45,
    0x78, 0x59, 0xe4, 0x59, 0x54, 0x50, 0x06, 0xf5,
    0x72, 0x2a, 0x10, 0xde, 0x99, 0x31, 0xd8, 0x8c,
    0x21, 0xc1, 0x43, 0x06, 0x75, 0x78, 0x64, 0x85
};

static const uint8_t original_replay_digest[32] = {
    0x56, 0xbe, 0x4b, 0xa5, 0xac, 0xd8, 0x5b, 0x64,
    0x85, 0x0c, 0x39, 0x3b, 0xc1, 0xf2, 0x7c, 0xeb,
    0x8c, 0x32, 0xe8, 0x87, 0xa3, 0xa9, 0x5c, 0x22,
    0x52, 0x35, 0x98, 0x9d, 0xb0, 0xb6, 0xf1, 0x72
};

static const uint8_t original_terminal_root[32] = {
    0x3e, 0x83, 0x4c, 0x23, 0xf2, 0x11, 0x57, 0x7b,
    0x42, 0x2d, 0x8a, 0xe9, 0xc2, 0x18, 0xfc, 0x8c,
    0x32, 0xd2, 0x56, 0x43, 0xf1, 0xf9, 0x65, 0xea,
    0xc2, 0x74, 0xb0, 0x77, 0x69, 0xdb, 0x09, 0x3c
};

static int check_original_record_bytes(void)
{
    uint8_t records[ORIGINAL_RECORD_BYTES];
    uint8_t digest[32];
    size_t length;
    FILE *file = fopen("tests/vectors/replay_corpus.lxb", "rb");
    if (file == NULL) return 1;
    if (fseek(file, FIXTURE_HEADER_BYTES, SEEK_SET) != 0) {
        (void)fclose(file);
        return 1;
    }
    length = fread(records, 1U, sizeof(records), file);
    if (fclose(file) != 0 || length != sizeof(records)) return 1;
    return lxp_hash_sha256(records, sizeof(records), digest) != LXP_OK ||
        memcmp(digest, original_records_digest, sizeof(digest)) != 0;
}

static int check_activity_types(const lxp_replay_fixture *fixture)
{
    static const uint32_t original_maxima[] = {8U, 7U, 7U, 7U, 13U, 11U};
    size_t record_index = 0U;
    size_t i;
    unsigned int phase;
    for (phase = 0U; phase < 2U; ++phase) {
        for (i = 0U; i < sizeof(expected_activity_types) /
                          sizeof(expected_activity_types[0]); ++i) {
            uint32_t activity_type = expected_activity_types[i];
            uint32_t module = activity_type >> 16U;
            uint32_t ordinal = activity_type & UINT32_C(0xffff);
            int original = module != 0U &&
                module <= sizeof(original_maxima) / sizeof(original_maxima[0]) &&
                ordinal != 0U && ordinal <= original_maxima[module - 1U];
            lxp_activity activity;
            if (i != 0U && activity_type <= expected_activity_types[i - 1U])
                return 1;
            if (original != (phase == 0U)) continue;
            if (record_index >= fixture->record_count ||
                lxp_activity_decode(
                    fixture->records[record_index].canonical_activity.bytes,
                    fixture->records[record_index].canonical_activity.length,
                    &activity) != LXP_OK ||
                activity.activity_type != activity_type) return 1;
            ++record_index;
        }
        if (phase == 0U && record_index != ORIGINAL_RECORD_COUNT) return 1;
    }
    return record_index != fixture->record_count;
}

static void print_digest(const uint8_t digest[32])
{
    static const char hex[] = "0123456789abcdef";
    size_t i;
    for (i = 0U; i < 32U; ++i) {
        (void)putchar(hex[digest[i] >> 4U]);
        (void)putchar(hex[digest[i] & 15U]);
    }
    (void)putchar('\n');
}

int main(void)
{
    uint8_t *storage = malloc(2U * 1024U * 1024U);
    lxp_arena arena;
    lxp_replay_fixture fixture;
    lxp_replay_fixture original;
    uint8_t digest[32];
    uint8_t terminal[32];
    uint64_t divergent = 0U;
    size_t i;
    lxp_result status;
    if (storage == NULL ||
        lxp_arena_init(&arena, storage, 2U * 1024U * 1024U) != LXP_OK ||
        lxp_replay_fixture_load("tests/vectors/replay_corpus.lxb", &arena,
                                &fixture) != LXP_OK ||
        fixture.record_count != sizeof(expected_activity_types) /
                                sizeof(expected_activity_types[0])) {
        free(storage);
        return 1;
    }
    if (check_activity_types(&fixture) != 0 ||
        check_original_record_bytes() != 0) {
        free(storage);
        return 1;
    }
    original = fixture;
    original.record_count = ORIGINAL_RECORD_COUNT;
    status = lxp_replay_digest(&original, digest, terminal, &divergent);
    if (status != LXP_OK || divergent != 0U ||
        memcmp(digest, original_replay_digest, 32U) != 0 ||
        memcmp(terminal, original_terminal_root, 32U) != 0) {
        free(storage);
        return 1;
    }
    status = lxp_replay_digest(&fixture, digest, terminal, &divergent);
    if (status != LXP_OK || divergent != 0U ||
        memcmp(digest, fixture.expected_digest, 32U) != 0 ||
        memcmp(terminal, fixture.expected_terminal_root, 32U) != 0) {
        (void)fprintf(stderr, "first divergent global sequence: %llu\n",
                      (unsigned long long)divergent);
        free(storage);
        return 1;
    }
    ((uint8_t *)fixture.records[17].expected_receipt.bytes)[0] ^= 1U;
    status = lxp_replay_digest(&fixture, digest, terminal, &divergent);
    ((uint8_t *)fixture.records[17].expected_receipt.bytes)[0] ^= 1U;
    if (status != LXP_FATAL_REPLAY_DIVERGENCE || divergent != 18U ||
        lxp_replay_crossarch_case("tests/vectors/replay_corpus.lxb", &arena,
                                  digest, &divergent) != LXP_OK ||
        divergent != 0U) {
        (void)fprintf(stderr, "first divergent global sequence: %llu\n",
                      (unsigned long long)divergent);
        free(storage);
        return 1;
    }
    for (i = ORIGINAL_RECORD_COUNT; i < fixture.record_count; ++i) {
        ((uint8_t *)fixture.records[i].expected_receipt.bytes)[0] ^= 1U;
        status = lxp_replay_digest(&fixture, digest, terminal, &divergent);
        ((uint8_t *)fixture.records[i].expected_receipt.bytes)[0] ^= 1U;
        if (status != LXP_FATAL_REPLAY_DIVERGENCE || divergent != i + 1U) {
            (void)fprintf(stderr, "first divergent global sequence: %llu\n",
                          (unsigned long long)divergent);
            free(storage);
            return 1;
        }
    }
    status = lxp_replay_digest(&fixture, digest, terminal, &divergent);
    if (status != LXP_OK || divergent != 0U ||
        memcmp(digest, fixture.expected_digest, 32U) != 0 ||
        memcmp(terminal, fixture.expected_terminal_root, 32U) != 0) {
        free(storage);
        return 1;
    }
    print_digest(digest);
    free(storage);
    return 0;
}
