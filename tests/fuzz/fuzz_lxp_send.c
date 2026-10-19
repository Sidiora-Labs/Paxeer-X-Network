#include "layerx/lxp_ledger.h"

#include <stddef.h>
#include <stdint.h>

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size)
{
    lxp_send send;
    (void)lxp_send_decode(data, size, &send);
    return 0;
}
