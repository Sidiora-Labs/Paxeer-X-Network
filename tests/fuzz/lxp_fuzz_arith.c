#include "tests/arith/lxp_test_arith_property.h"

#include <stddef.h>
#include <stdint.h>
#include <string.h>

static uint64_t load_word(const uint8_t *bytes)
{
    uint64_t value;
    (void)memcpy(&value, bytes, sizeof(value));
    return value;
}

int lxp_fuzz_arith(const uint8_t *data, size_t size)
{
    lxp_u128 left;
    lxp_u128 right;
    uint32_t basis_points;
    if (data == NULL || size < 36U) return 0;
    left.lo = load_word(data);
    left.hi = load_word(data + 8U);
    right.lo = load_word(data + 16U);
    right.hi = load_word(data + 24U);
    (void)memcpy(&basis_points, data + 32U, sizeof(basis_points));
    return lxp_test_arith_property(left, right, basis_points) ? 0 : 1;
}
