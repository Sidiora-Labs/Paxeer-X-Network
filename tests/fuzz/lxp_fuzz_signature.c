#include "layerx/lxp_crypto.h"

#include <string.h>

int lxp_fuzz_signature(const uint8_t public_key[32],
                       const uint8_t signature[64],
                       const void *message, size_t message_length)
{
    uint8_t mutated[64];
    size_t i;
    for (i = 0U; i < sizeof(mutated); ++i) {
        (void)memcpy(mutated, signature, sizeof(mutated));
        mutated[i] ^= (uint8_t)(1U << (i & 7U));
        if (lxp_ed25519_verify_raw(public_key, mutated, message,
                                   message_length) == LXP_OK) return 1;
    }
    return 0;
}
