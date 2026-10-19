#include "layerx/lxp_fee.h"

#include <limits.h>
#include <stddef.h>
#include <stdint.h>

static uint64_t take_u64(const uint8_t *data, size_t size, size_t *offset)
{
    uint64_t value = 0U;
    size_t i;
    for (i = 0U; i < 8U; ++i) {
        value <<= 8U;
        if (*offset < size) value |= data[(*offset)++];
    }
    return value;
}

static int64_t signed_delta(uint64_t bits)
{
    if (bits <= (uint64_t)INT64_MAX) return (int64_t)bits;
    return -(int64_t)(~bits) - 1;
}

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size)
{
    lxp_meter_ctx meter;
    size_t offset = 0U;
    uint64_t execution_ceiling;
    uint64_t storage_ceiling;
    lxp_u128 storage_rate;
    lxp_result status;
    if (data == NULL && size != 0U) return 0;
    execution_ceiling = take_u64(data, size, &offset);
    storage_ceiling = take_u64(data, size, &offset);
    storage_rate.hi = take_u64(data, size, &offset);
    storage_rate.lo = take_u64(data, size, &offset);
    if (lxp_meter_init(&meter, execution_ceiling, storage_ceiling,
                       storage_rate, (lxp_u128){UINT64_MAX, UINT64_MAX},
                       1U, true) != LXP_OK)
        return 1;
    while (offset < size) {
        uint8_t operation = data[offset++];
        uint64_t value = take_u64(data, size, &offset);
        uint64_t execution_before = meter.execution_units;
        uint64_t storage_before = meter.net_storage_bytes;
        lxp_u128 fee_before = meter.storage_fee;
        bool exhausted_before = meter.exhausted;
        if ((operation & 1U) == 0U)
            status = lxp_meter_charge_exec(&meter, value);
        else
            status = lxp_meter_charge_storage(&meter, signed_delta(value));
        if (status != LXP_OK && status != LXP_ERR_GAS_EXHAUSTED &&
            status != LXP_ERR_OVERFLOW)
            return 1;
        if (status == LXP_ERR_OVERFLOW &&
            (meter.execution_units != execution_before ||
             meter.net_storage_bytes != storage_before ||
             lxp_u128_cmp(meter.storage_fee, fee_before) != 0 ||
             meter.exhausted != exhausted_before))
            return 1;
        if (status == LXP_ERR_GAS_EXHAUSTED && !meter.exhausted)
            return 1;
        if (meter.exhausted && lxp_meter_exhausted(&meter) !=
            LXP_ERR_GAS_EXHAUSTED)
            return 1;
    }
    return 0;
}
