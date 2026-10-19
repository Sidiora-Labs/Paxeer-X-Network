#include "layerx/lxp_codec.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>

int lxp_fuzz_codec_decode(const uint8_t *data, size_t size)
{
    lxp_codec_reader reader;
    uint64_t value = 0U;
    lxp_result result;
    if (lxp_codec_reader_init(&reader, data, size) != LXP_OK) return 1;
    result = lxp_codec_read_u64(&reader, &value);
    if (result == LXP_OK) result = lxp_codec_finish(&reader);
    return result == LXP_OK || result == LXP_ERR_TRUNCATED ||
           result == LXP_ERR_TRAILING_BYTES ? 0 : 1;
}

int lxp_fuzz_codec_roundtrip(const uint8_t *data, size_t size)
{
    uint8_t output[8];
    lxp_arena arena;
    lxp_codec_reader reader;
    lxp_codec_writer writer;
    uint64_t value;
    if (lxp_codec_reader_init(&reader, data, size) != LXP_OK) return 1;
    if (lxp_codec_read_u64(&reader, &value) != LXP_OK ||
        lxp_codec_finish(&reader) != LXP_OK) return 0;
    if (lxp_arena_init(&arena, output, sizeof(output)) != LXP_OK ||
        lxp_codec_writer_init(&writer, &arena, sizeof(output)) != LXP_OK ||
        lxp_codec_write_u64(&writer, value) != LXP_OK) return 1;
    return writer.length == size && memcmp(writer.bytes, data, size) == 0 ? 0 : 1;
}

static uint64_t next_value(uint64_t *state)
{
    *state ^= *state << 13U;
    *state ^= *state >> 7U;
    *state ^= *state << 17U;
    return *state;
}

int main(void)
{
    uint64_t state = UINT64_C(0x4c6179657258467a);
    uint8_t data[64];
    size_t iteration;
    FILE *seed = fopen("tests/fuzz/corpus/codec/seed.bin", "rb");
    if (seed == NULL) return 1;
    {
        size_t length = fread(data, 1U, sizeof(data), seed);
        if (ferror(seed) != 0 || fclose(seed) != 0 ||
            lxp_fuzz_codec_decode(data, length) != 0 ||
            lxp_fuzz_codec_roundtrip(data, length) != 0) return 1;
    }
    for (iteration = 0U; iteration < 20000U; ++iteration) {
        size_t length = (size_t)(next_value(&state) % (sizeof(data) + 1U));
        size_t i;
        for (i = 0U; i < length; ++i) data[i] = (uint8_t)next_value(&state);
        if (lxp_fuzz_codec_decode(data, length) != 0 ||
            lxp_fuzz_codec_roundtrip(data, length) != 0) return 1;
    }
    return 0;
}
