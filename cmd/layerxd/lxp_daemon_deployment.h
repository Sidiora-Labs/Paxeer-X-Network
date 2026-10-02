#ifndef LAYERX_DAEMON_DEPLOYMENT_H
#define LAYERX_DAEMON_DEPLOYMENT_H
#include "layerx/lxp_daemon.h"

lxp_result lxp_daemon_deployment_encode(
    const lxp_kernel *kernel, const lxp_daemon_activity_evidence *evidence,
    const lxp_daemon_receipt_authority_store *receipts,
    uint32_t network_id, lxp_arena *arena, lxp_byte_span *encoded);
lxp_result lxp_daemon_program_state_encode(
    const lxp_kernel *kernel,
    const lxp_daemon_receipt_authority_store *receipts,
    uint32_t network_id, const uint8_t program_id[32],
    uint64_t sequence, const uint8_t receipt_digest[32],
    const uint8_t expected_root[32], lxp_arena *arena, lxp_byte_span *encoded);
#endif
