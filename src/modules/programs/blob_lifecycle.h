#ifndef LAYERX_PROGRAMS_BLOB_LIFECYCLE_H
#define LAYERX_PROGRAMS_BLOB_LIFECYCLE_H

#include "layerx/programs.h"

lxp_result lxp_programs_retirement_decode(lxp_module_ctx *ctx,
    const uint8_t *payload, size_t length, void **decoded);
lxp_result lxp_programs_retirement_validate(lxp_module_ctx *ctx,
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    const void *decoded);
lxp_result lxp_programs_retirement_execute(lxp_module_ctx *ctx,
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    const void *decoded, lxp_effect_buffer *effects);

#endif
