#include "layerx/lxp_gateway.h"

#include <stddef.h>
#include <stdint.h>

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size)
{
    lxp_payment_requirement requirement;
    uint8_t canonical[LXP_PAYMENT_REQUIREMENT_ENCODED_SIZE];
    size_t canonical_length = 0U;
    lxp_result status = lxp_gateway_translate(
        data, size, &requirement, canonical, &canonical_length);
    if (status == LXP_OK &&
        canonical_length != LXP_PAYMENT_REQUIREMENT_ENCODED_SIZE)
        __builtin_trap();
    return 0;
}
