#include "layerx/lxp_activity.h"

#include <stdlib.h>
#include <string.h>

int lxp_fuzz_activity(const uint8_t *data, size_t size)
{
    lxp_activity activity;
    lxp_result status;
    if (size > LXP_MAX_ACTIVITY_BYTES) return 0;
    status = lxp_activity_decode(data, size, &activity);
    if (status == LXP_OK) {
        uint8_t *storage = malloc(LXP_MAX_ACTIVITY_BYTES);
        lxp_arena arena;
        lxp_byte_span encoded;
        int mismatch;
        if (storage == NULL ||
            lxp_arena_init(&arena, storage, LXP_MAX_ACTIVITY_BYTES) != LXP_OK ||
            lxp_activity_encode(&activity, &arena, &encoded) != LXP_OK) {
            free(storage);
            return 1;
        }
        mismatch = encoded.length != size ||
                   memcmp(encoded.bytes, data, size) != 0;
        free(storage);
        return mismatch;
    }
    return status == LXP_ERR_MALFORMED_ENVELOPE ||
           status == LXP_ERR_LENGTH_LIMIT ? 0 : 1;
}
