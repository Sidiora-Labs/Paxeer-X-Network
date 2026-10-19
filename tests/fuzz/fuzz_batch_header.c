#include "layerx/lxp_batch.h"

#include <stddef.h>
#include <stdint.h>

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size)
{
    lxp_batch_header header;
    (void)lxp_batch_header_decode(data, size, &header);
    return 0;
}
