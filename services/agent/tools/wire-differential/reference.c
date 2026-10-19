#include "layerx/lxp_activity.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_protocol.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum { LINE_BYTES = (LXP_MAX_ACTIVITY_BYTES * 2) + 128 };

static int nibble(char value)
{
    if (value >= '0' && value <= '9') return value - '0';
    if (value >= 'a' && value <= 'f') return value - 'a' + 10;
    if (value >= 'A' && value <= 'F') return value - 'A' + 10;
    return -1;
}

static int decode_hex(const char *text, uint8_t *bytes, size_t capacity,
                      size_t *length)
{
    size_t text_length = strlen(text);
    size_t index;
    if ((text_length & 1U) != 0U || text_length / 2U > capacity) return 1;
    *length = text_length / 2U;
    for (index = 0U; index < *length; ++index) {
        int high = nibble(text[index * 2U]);
        int low = nibble(text[index * 2U + 1U]);
        if (high < 0 || low < 0) return 1;
        bytes[index] = (uint8_t)((unsigned)high * 16U + (unsigned)low);
    }
    return 0;
}

static void print_hex(const uint8_t *bytes, size_t length)
{
    static const char digits[] = "0123456789abcdef";
    size_t index;
    for (index = 0U; index < length; ++index) {
        (void)putchar(digits[bytes[index] >> 4U]);
        (void)putchar(digits[bytes[index] & 15U]);
    }
}

static lxp_result primitive(const char *kind, const uint8_t *bytes,
                            size_t length)
{
    lxp_codec_reader reader;
    lxp_result status = lxp_codec_reader_init(&reader, bytes, length);
    if (status != LXP_OK) return status;
    if (strcmp(kind, "u64") == 0) {
        uint64_t value;
        status = lxp_codec_read_u64(&reader, &value);
        return status == LXP_OK ? lxp_codec_finish(&reader) : status;
    }
    if (strcmp(kind, "tag") == 0) {
        uint8_t tag;
        return lxp_codec_read_tag(&reader, 3U, &tag);
    }
    if (strcmp(kind, "bytes4") == 0) {
        lxp_byte_span span;
        return lxp_codec_read_bytes(&reader, &span, 4U);
    }
    if (strcmp(kind, "seq") == 0) {
        lxp_byte_span keys[2];
        size_t offset = 0U;
        size_t index;
        for (index = 0U; index < 2U; ++index) {
            size_t item_length;
            if (offset >= length) return LXP_ERR_TRUNCATED;
            item_length = bytes[offset++];
            if (item_length > length - offset) return LXP_ERR_TRUNCATED;
            keys[index].bytes = bytes + offset;
            keys[index].length = item_length;
            offset += item_length;
        }
        return lxp_codec_seq_check_sorted(keys, 2U);
    }
    return LXP_ERR_INVALID_TAG;
}

static void activity(const uint8_t *bytes, size_t length,
                     uint8_t *arena_storage)
{
    lxp_activity decoded;
    lxp_arena arena;
    lxp_byte_span encoded;
    uint8_t identifier[32];
    uint8_t payload_hash[32];
    lxp_result status = lxp_activity_decode(bytes, length, &decoded);
    if (status == LXP_OK)
        status = lxp_arena_init(&arena, arena_storage, LXP_MAX_ACTIVITY_BYTES);
    if (status == LXP_OK) status = lxp_activity_encode(&decoded, &arena, &encoded);
    if (status == LXP_OK) status = lxp_activity_id(encoded.bytes, encoded.length,
                                                   identifier);
    if (status == LXP_OK) status = lxp_hash_payload(decoded.payload.bytes,
                                                    decoded.payload.length,
                                                    payload_hash);
    (void)printf("%d|", status);
    if (status == LXP_OK) {
        print_hex(encoded.bytes, encoded.length);
        (void)putchar('|');
        print_hex(identifier, sizeof(identifier));
        (void)putchar('|');
        print_hex(payload_hash, sizeof(payload_hash));
    } else {
        (void)fputs("-|-|-", stdout);
    }
    (void)putchar('\n');
}

int main(void)
{
    char *line = malloc((size_t)LINE_BYTES);
    uint8_t *input = malloc(LXP_MAX_ACTIVITY_BYTES);
    uint8_t *arena_storage = malloc(LXP_MAX_ACTIVITY_BYTES);
    if (line == NULL || input == NULL || arena_storage == NULL) {
        free(line);
        free(input);
        free(arena_storage);
        return 1;
    }
    while (fgets(line, LINE_BYTES, stdin) != NULL) {
        char *command = strtok(line, " \r\n");
        char *kind = strtok(NULL, " \r\n");
        char *hex = strtok(NULL, " \r\n");
        size_t length = 0U;
        if (command == NULL || kind == NULL || hex == NULL ||
            decode_hex(hex, input, LXP_MAX_ACTIVITY_BYTES, &length) != 0) {
            (void)fputs("-3|-|-|-\n", stdout);
            continue;
        }
        if (strcmp(command, "primitive") == 0) {
            (void)printf("%d|-|-|-\n", primitive(kind, input, length));
        } else if (strcmp(command, "activity") == 0) {
            activity(input, length, arena_storage);
        } else {
            (void)fputs("-6|-|-|-\n", stdout);
        }
        (void)fflush(stdout);
    }
    free(line);
    free(input);
    free(arena_storage);
    return ferror(stdin) == 0 ? 0 : 1;
}
