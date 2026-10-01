#!/usr/bin/env python3
"""Interface-bearing native Deploy/Upgrade qualification across guest ABIs 1-4.

The driver is compiled against the candidate kernel library and the candidate
programs runtime static library, dispatches real lifecycle activities through
the registered programs module, restarts from a verified snapshot and checks
the persisted interface and lifecycle records byte for byte.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
KERNEL_LIB = ROOT / 'build' / 'liblayerx.a'
RUNTIME_LIB = ROOT / 'programs' / 'target' / 'debug' / 'liblayerx_programs_sandbox.a'
SOURCES = [
    'programs/crates/layerx-programs-runtime/src/ffi_interface.rs',
    'programs/crates/layerx-programs-runtime/src/abi_policy.rs',
    'programs/crates/layerx-programs-runtime/src/engine.rs',
    'programs/crates/layerx-programs-runtime/src/ffi_call.rs',
]
KERNEL_SOURCES = ['src/modules/programs/deploy.c']
CLI_SOURCES = [
    'programs/sdk/rust/src/bindgen.rs',
    'programs/sdk/rust/src/abi_policy.rs',
    'programs/crates/layerx-programs-registry/src/interface.rs',
    'platform/cli/src/programs.rs',
]
DOMAIN = b'LayerX/program-interface/v1\0'
AUTHORITY = bytes([0x42]) * 32

DRIVER = r'''
#include "layerx/programs.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_batch.h"
#include "layerx/lxp_genesis.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static lxp_state_store stores[4];
static lxp_state_journal journals[4];
static lxp_kernel kernels[4];
static uint8_t payload[65536];
static uint8_t expected_bytes[4096];
static uint8_t snapshot_storage[1U << 20];
static uint8_t arena_bytes[1U << 18];

static const struct { const char *name; lxp_result value; } results[] = {
    {"OK", LXP_OK},
    {"NON_CANONICAL", LXP_ERR_NON_CANONICAL},
    {"VERSION_UNSUPPORTED", LXP_ERR_VERSION_UNSUPPORTED},
    {"CONTEXT_MISMATCH", LXP_ERR_CONTEXT_MISMATCH},
    {"UNKNOWN_FIELD", LXP_ERR_UNKNOWN_FIELD},
    {"PAYLOAD_HASH_MISMATCH", LXP_ERR_PAYLOAD_HASH_MISMATCH},
};

static int result_named(const char *name, lxp_result *value)
{
    size_t i;
    for (i = 0U; i < sizeof(results) / sizeof(results[0]); ++i)
        if (strcmp(results[i].name, name) == 0) {
            *value = results[i].value;
            return 0;
        }
    return 1;
}

static long hex_decode(const char *text, uint8_t *out, size_t capacity)
{
    size_t length = strlen(text), i;
    if (length % 2U != 0U || length / 2U > capacity) return -1;
    for (i = 0U; i < length / 2U; ++i) {
        unsigned int byte;
        if (sscanf(text + 2U * i, "%2x", &byte) != 1) return -1;
        out[i] = (uint8_t)byte;
    }
    return (long)(length / 2U);
}

static uint32_t read_u32(const uint8_t *b)
{
    return ((uint32_t)b[0] << 24U) | ((uint32_t)b[1] << 16U) |
           ((uint32_t)b[2] << 8U) | (uint32_t)b[3];
}

static const lxp_module_kv_entry *find_kv(const lxp_kernel *kernel,
                                          const char *prefix, size_t prefix_length,
                                          const uint8_t program_id[32])
{
    size_t i;
    for (i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *item = &kernel->module_kv[i];
        if (item->module_id == LXP_MODULE_PROGRAMS &&
            item->key_length == prefix_length + 32U &&
            memcmp(item->key, prefix, prefix_length) == 0 &&
            memcmp(item->key + prefix_length, program_id, 32U) == 0)
            return item;
    }
    return NULL;
}

static int create_kernel(int slot)
{
    uint64_t parameters = 1U;
    return lxp_state_store_init(&stores[slot], 2U) != LXP_OK ||
           lxp_kernel_create(&kernels[slot], &stores[slot], &journals[slot],
                             &parameters, 0U) != LXP_OK ||
           lxp_kernel_register_module(&kernels[slot],
                                      programs_module_registration()) != LXP_OK;
}

static int dispatch(int slot, uint16_t ordinal, size_t length,
                    lxp_result expected, lxp_result *observed)
{
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity;
    lxp_authority_resolved authority;
    const lxp_module_registration *registration;
    lxp_kernel *kernel = &kernels[slot];
    *observed = LXP_OK;
    (void)memset(&authority, 0, sizeof(authority));
    (void)memset(authority.principal, 0x42, sizeof(authority.principal));
    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity.payload = (lxp_byte_span){payload, length};
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U,
                                       &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 1U, 0U,
                            ordinal, 1000000U, &arena, false) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    ctx.batch_number = 1U;
    if (lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK ||
        lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
                            &effects, observed) != LXP_OK)
        return 1;
    if (*observed == LXP_OK) {
        if (lxp_module_ctx_commit(&ctx) != LXP_OK) return 1;
    } else {
        lxp_module_ctx_rollback(&ctx);
    }
    return *observed == expected ? 0 : 1;
}

/* Slots 2 and 3 run the production execution path: genesis-materialized
 * metering and fee governance, genesis accounts, a registered identity and the
 * bound programs runtime, driven through lxp_kernel_execute_activity. */
static lx_account_registry boot_accounts[2];
static lx_account *boot_actor[2];
static lxp_identity_store boot_identities[2];
static lxp_identity *boot_identity[2];
static lx_programs_transfer_runtime boot_runtime[2];
static lxp_transfer_asset_state boot_asset[2];
static lxp_authority_resolved boot_authority;
static lxp_fee_params boot_fees;
static uint64_t boot_parameters[4] = {1U, 1U, 1U, 1U};
static uint32_t boot_idempotency;
static uint8_t boot_key[32] = {1U};
static const uint8_t boot_did[] = "did:lxp:programs-native-interfaces";
static const uint8_t boot_actor_name[] = "agent:did:lxp:programs-native-interfaces:main";
static const uint8_t boot_treasury_name[] = "system:fees";
static const uint8_t boot_fee_asset[32] = {9U};
static uint8_t execution_arena[2U * LXP_MAX_ACTIVITY_BYTES + 4096U];

static int bootstrap(int slot)
{
    static const uint64_t coefficients[LX_PROGRAMS_METERING_COEFFICIENTS] = {
        1U, 1U, 1U, 1U, 1U, 8U, 8U, 64U, 8U};
    const int index = slot - 2;
    uint8_t actor_id[32], treasury_id[32];
    lx_account *treasury;
    lxp_genesis_manifest manifest;
    lx_programs_metering_schedule metering;
    lx_programs_fee_genesis_parameters fee_genesis;
    if (index < 0 || index > 1) return 20;
    (void)memset(&manifest, 0, sizeof(manifest));
    (void)memset(&metering, 0, sizeof(metering));
    (void)memset(&fee_genesis, 0, sizeof(fee_genesis));
    (void)memcpy(manifest.signer_public_key, boot_key, 32U);
    metering.version = 1U;
    (void)memcpy(metering.coefficients, coefficients, sizeof(coefficients));
    metering.activation_batch = 1U;
    metering.authority_kind = LX_PROGRAMS_METERING_AUTHORITY_GENESIS;
    fee_genesis.schedule = (lx_programs_fee_schedule){1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U};
    (void)memcpy(fee_genesis.occupancy_asset_id, boot_fee_asset, 32U);
    fee_genesis.target_occupancy_byte_batches = 3U;
    fee_genesis.response_denominator = 1U;
    fee_genesis.maximum_change_numerator = 1U;
    fee_genesis.maximum_change_denominator = 1U;
    fee_genesis.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fee_genesis.maximum_fee_units_per_occupancy_byte_batch = 10U;
    (void)memset(&boot_asset[index], 0, sizeof(boot_asset[index]));
    (void)memcpy(boot_asset[index].asset_id, boot_fee_asset, 32U);
    boot_asset[index].registered = true;
    (void)memset(&boot_runtime[index], 0, sizeof(boot_runtime[index]));
    boot_runtime[index].accounts = &boot_accounts[index];
    boot_runtime[index].assets = &boot_asset[index];
    boot_runtime[index].asset_count = 1U;
    boot_runtime[index].fee_schedule = fee_genesis.schedule;
    boot_runtime[index].resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    boot_runtime[index].metering_schedule_context = &kernels[slot];
    (void)memcpy(boot_runtime[index].occupancy_asset_id, boot_fee_asset, 32U);
    boot_runtime[index].resolve_occupancy_parameters =
        lxp_programs_fee_governance_resolve_runtime;
    boot_runtime[index].occupancy_parameter_context = &kernels[slot];
    if (lx_account_registry_init(&boot_accounts[index]) != LXP_OK ||
        lx_account_id_from_string(boot_actor_name, sizeof(boot_actor_name) - 1U,
                                  actor_id) != LXP_OK ||
        lx_account_id_from_string(boot_treasury_name, sizeof(boot_treasury_name) - 1U,
                                  treasury_id) != LXP_OK ||
        lx_account_open(&boot_accounts[index], boot_actor_name,
                        sizeof(boot_actor_name) - 1U, actor_id, 1U,
                        LX_ACCOUNT_OPEN_GENESIS, NULL, &boot_actor[index]) != LXP_OK ||
        lx_account_open(&boot_accounts[index], boot_treasury_name,
                        sizeof(boot_treasury_name) - 1U, treasury_id, 2U,
                        LX_ACCOUNT_OPEN_GENESIS, NULL, &treasury) != LXP_OK ||
        lxp_ledger_bootstrap_balance(boot_actor[index], boot_fee_asset,
                                     (lxp_u128){0U, UINT64_MAX}, 1U) != LXP_OK ||
        lxp_ledger_bootstrap_balance(treasury, boot_fee_asset,
                                     (lxp_u128){0U, 0U}, 0U) != LXP_OK)
        return 21;
    if (lxp_hash_payload(boot_key, 32U, metering.authority_digest) != LXP_OK ||
        lxp_programs_metering_genesis_append(&manifest, &metering) != LXP_OK ||
        lxp_programs_fee_genesis_append(&manifest, &fee_genesis) != LXP_OK)
        return 22;
    if (lxp_state_store_init(&stores[slot], 1U) != LXP_OK ||
        lxp_kernel_create(&kernels[slot], &stores[slot], &journals[slot],
                          &boot_parameters[slot], 0U) != LXP_OK)
        return 23;
    if (lxp_programs_metering_genesis_materialize(&manifest, &kernels[slot]) != LXP_OK)
        return 24;
    if (lxp_kernel_register_module(&kernels[slot], programs_module_registration_v4()) != LXP_OK)
        return 25;
    if (lxp_programs_fee_genesis_materialize(&manifest, &kernels[slot]) != LXP_OK)
        return 26;
    if (lxp_kernel_bind_module_runtime(&kernels[slot], LXP_MODULE_PROGRAMS,
                                       &boot_runtime[index]) != LXP_OK ||
        lxp_programs_bind_fee_transaction(&kernels[slot]) != LXP_OK)
        return 27;
    if (index == 0) {
        (void)memset(&boot_authority, 0, sizeof(boot_authority));
        (void)memcpy(boot_authority.principal, actor_id, 32U);
        (void)memset(boot_authority.authority_hash, 0x55, 32U);
        boot_fees.version = 1U;
        boot_fees.multiplier_basis_points = 10000U;
        if (lxp_identity_register(&boot_identities[0], boot_did, sizeof(boot_did) - 1U,
                                  boot_key, &boot_identity[0]) != LXP_OK)
            return 28;
    } else {
        boot_identities[1] = boot_identities[0];
        boot_identity[1] = &boot_identities[1].identities[
            boot_identity[0] - boot_identities[0].identities];
    }
    return lxp_state_root(&kernels[slot], kernels[slot].current_state_root) == LXP_OK ? 0 : 29;
}

static void write_u32_be(uint8_t *out, uint32_t value)
{
    out[0] = (uint8_t)(value >> 24U);
    out[1] = (uint8_t)(value >> 16U);
    out[2] = (uint8_t)(value >> 8U);
    out[3] = (uint8_t)value;
}

static void write_u64_be(uint8_t *out, uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i) out[i] = (uint8_t)(value >> ((7U - i) * 8U));
}

/* Executes one Programs activity through the production kernel entry point.
 * A Deploy names its authority as the executing principal. */
static int execute(int slot, uint16_t ordinal, size_t length, lxp_result expected,
                   int expected_abi, char *detail, size_t detail_size)
{
    const int index = slot - 2;
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_receipt receipt;
    lxp_arena arena;
    lxp_byte_span encoded;
    lxp_batch_roots roots;
    uint8_t preimage[88];
    lxp_result status, observed;
    lxp_kernel *kernel = &kernels[slot];
    if (index < 0 || index > 1 || boot_identity[index] == NULL) return 1;
    if (ordinal == 1U && length >= 68U)
        (void)memcpy(payload + 36U, boot_authority.principal, 32U);
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION;
    activity.network_id = 7U;
    activity.activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity.actor_did = (lxp_byte_span){boot_did, sizeof(boot_did) - 1U};
    activity.authority = (lxp_byte_span){boot_key, 32U};
    activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    write_u32_be(activity.idempotency_key + 28U, ++boot_idempotency);
    activity.account_sequence = boot_identity[index]->next_sequence;
    activity.fee_limit = (lxp_u128){0U, UINT64_MAX};
    activity.payload = (lxp_byte_span){payload, length};
    if (lxp_hash_payload(payload, length, activity.payload_hash) != LXP_OK) return 1;
    (void)memset(&execution, 0, sizeof(execution));
    execution.network_id = 7U;
    execution.epoch = kernel->epoch;
    execution.batch_number = 1U;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.global_sequence = stores[slot].next_sequence;
    execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &boot_identities[index];
    execution.authority = &boot_authority;
    execution.fee_parameters = &boot_fees;
    execution.fee_balance = boot_actor[index]->balance;
    execution.gas_limit = 1000000U;
    execution.arena = &arena;
    if (lxp_arena_init(&arena, execution_arena, sizeof(execution_arena)) != LXP_OK ||
        lxp_activity_encode(&activity, &arena, &encoded) != LXP_OK ||
        lxp_batch_roots_compute(
            &(lxp_batch_root_inputs){&encoded, 1U, NULL, 0U, NULL, 0U, NULL, 0U, NULL, 0U},
            &arena, &roots) != LXP_OK)
        return 1;
    (void)memcpy(preimage, kernel->current_state_root, 32U);
    (void)memcpy(preimage + 32U, roots.activity_merkle_root, 32U);
    write_u64_be(preimage + 64U, execution.global_sequence);
    write_u64_be(preimage + 72U, execution.global_sequence);
    write_u64_be(preimage + 80U, execution.batch_number);
    if (lxp_hash_context_value(preimage, sizeof(preimage), execution.batch_id) != LXP_OK ||
        lxp_arena_reset(&arena, 0U) != LXP_OK)
        return 1;
    (void)memcpy(execution.activity_root, roots.activity_merkle_root, 32U);
    (void)memset(&receipt, 0, sizeof(receipt));
    status = lxp_kernel_execute_activity(kernel, &activity, &execution, &receipt);
    observed = status != LXP_OK ? status : receipt.result_code;
    (void)snprintf(detail, detail_size,
                   "status=%d receipt=%d outcome=%d outcome_abi=%u outcome_result=%d",
                   (int)status, (int)receipt.result_code,
                   (int)receipt.program_outcome.present,
                   (unsigned)receipt.program_outcome.abi_version,
                   (int)receipt.program_outcome.result_code);
    if (observed != expected) return 1;
    if (expected_abi <= 0) return 0;
    return !(receipt.program_outcome.present &&
             receipt.program_outcome.abi_version == (uint16_t)expected_abi &&
             receipt.program_outcome.result_code == LXP_OK);
}

static int restart(int from, int to)
{
    lxp_arena arena;
    lxp_byte_span snapshot;
    lxp_snapshot_manifest_record manifest;
    uint8_t original_root[32];
    uint8_t restored_root[32];
    size_t i;
    lxp_result status;
    if (to >= 2 ? bootstrap(to) != 0 : create_kernel(to) != 0) return 10;
    if (lxp_arena_init(&arena, snapshot_storage, sizeof(snapshot_storage)) != LXP_OK)
        return 11;
    status = lxp_state_root(&kernels[from], original_root);
    if (status != LXP_OK) return 1000 + (int)-status;
    (void)memcpy(kernels[from].current_state_root, original_root, 32U);
    status = lxp_snapshot_write(&kernels[from], stores[from].next_sequence - 1U, &arena,
                                &snapshot);
    if (status != LXP_OK) return 2000 + (int)-status;
    status = lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length,
                                         stores[from].next_sequence - 1U, original_root,
                                         kernels[from].current_state_root, &manifest);
    if (status != LXP_OK) return 3000 + (int)-status;
    status = lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest, &kernels[to]);
    if (status != LXP_OK) return 4000 + (int)-status;
    if (lxp_state_root(&kernels[to], restored_root) != LXP_OK ||
        memcmp(original_root, restored_root, 32U) != 0)
        return 12;
    status = lxp_snapshot_verify_root(&kernels[to], &manifest);
    if (status != LXP_OK) return 5000 + (int)-status;
    if (kernels[to].module_kv_count != kernels[from].module_kv_count) return 13;
    for (i = 0U; i < kernels[from].module_kv_count; ++i) {
        const lxp_module_kv_entry *a = &kernels[from].module_kv[i];
        size_t j;
        int matched = 0;
        for (j = 0U; j < kernels[to].module_kv_count && !matched; ++j) {
            const lxp_module_kv_entry *b = &kernels[to].module_kv[j];
            matched = b->module_id == a->module_id &&
                b->key_length == a->key_length &&
                memcmp(b->key, a->key, a->key_length) == 0 &&
                b->value_length == a->value_length &&
                memcmp(b->value, a->value, a->value_length) == 0;
        }
        if (!matched) return 14;
    }
    if (kernels[to].blob_count != kernels[from].blob_count) return 15;
    for (i = 0U; i < kernels[from].blob_count; ++i) {
        const lxp_module_blob *a = &kernels[from].blobs[i];
        const lxp_module_blob *b = &kernels[to].blobs[i];
        if (a->module_id != b->module_id || a->length != b->length ||
            memcmp(a->key, b->key, 32U) != 0 || memcmp(a->bytes, b->bytes, a->length) != 0)
            return 16;
    }
    return 0;
}

int main(void)
{
    static char line[200000];
    while (fgets(line, sizeof(line), stdin) != NULL) {
        char label[256], command[32], a[131072], b[64], c[64], d[131072];
        int fields;
        int failed = 1;
        char detail[4096] = "";
        line[strcspn(line, "\n")] = '\0';
        a[0] = b[0] = c[0] = d[0] = '\0';
        fields = sscanf(line, "%255s %31s %131071s %63s %63s %131071s",
                        label, command, a, b, c, d);
        if (fields < 2) {
            printf("fail malformed-command\n");
            return 2;
        }
        if (strcmp(command, "new") == 0) {
            failed = create_kernel(atoi(a));
        } else if (strcmp(command, "dispatch") == 0) {
            /* dispatch <slot> <ordinal> <expected> <payload-hex> */
            lxp_result expected, observed = LXP_OK;
            long length = hex_decode(d, payload, sizeof(payload));
            if (length > 0 && result_named(c, &expected) == 0) {
                failed = dispatch(atoi(a), (uint16_t)atoi(b), (size_t)length,
                                  expected, &observed);
                (void)snprintf(detail, sizeof(detail), "observed=%d", (int)observed);
            }
        } else if (strcmp(command, "iface") == 0) {
            /* iface <slot> <program-hex> <version> <encoding-hex|absent> */
            uint8_t program_id[32];
            const lxp_module_kv_entry *item;
            if (hex_decode(a, program_id, 32U) == 32) {
                item = find_kv(&kernels[atoi(b)], "interface", 10U, program_id);
                if (strcmp(d, "absent") == 0) {
                    failed = item != NULL;
                } else {
                    long length = hex_decode(d, expected_bytes, sizeof(expected_bytes));
                    uint8_t digest[32];
                    failed = item == NULL || length <= 0 ||
                        item->value_length != 72U + (size_t)length ||
                        memcmp(item->value, program_id, 32U) != 0 ||
                        read_u32(item->value + 32U) != (uint32_t)atoi(c) ||
                        read_u32(item->value + 68U) != (uint32_t)length ||
                        memcmp(item->value + 72U, expected_bytes, (size_t)length) != 0 ||
                        lxp_hash_sha256(expected_bytes, (size_t)length, digest) != LXP_OK ||
                        memcmp(item->value + 36U, digest, 32U) != 0;
                }
            }
        } else if (strcmp(command, "life") == 0) {
            /* life <slot> <program-hex> <abi> <version> <code-hash-hex> */
            uint8_t program_id[32], hash[32];
            char hash_hex[65];
            const lxp_module_kv_entry *item;
            (void)snprintf(hash_hex, sizeof(hash_hex), "%.64s", d);
            if (strlen(d) > 64U && hex_decode(a, program_id, 32U) == 32 &&
                hex_decode(hash_hex, hash, 32U) == 32) {
                item = find_kv(&kernels[atoi(b)], "program", 8U, program_id);
                failed = item == NULL || item->value_length != 71U ||
                    memcmp(item->value + 33U, hash, 32U) != 0 ||
                    (((unsigned)item->value[65] << 8U) | item->value[66]) !=
                        (unsigned)atoi(c) ||
                    read_u32(item->value + 67U) != (uint32_t)strtoul(d + 64, NULL, 10);
            }
        } else if (strcmp(command, "boot") == 0) {
            failed = bootstrap(atoi(a));
            (void)snprintf(detail, sizeof(detail), "step=%d", failed);
        } else if (strcmp(command, "execute") == 0) {
            /* execute <slot> <ordinal> <expected>[/<outcome-abi>] <payload-hex> */
            lxp_result expected;
            char *outcome = strchr(c, '/');
            int expected_abi = 0;
            long length = hex_decode(d, payload, sizeof(payload));
            if (outcome != NULL) {
                *outcome = '\0';
                expected_abi = atoi(outcome + 1);
            }
            if (length > 0 && result_named(c, &expected) == 0)
                failed = execute(atoi(a), (uint16_t)atoi(b), (size_t)length, expected,
                                 expected_abi, detail, sizeof(detail));
        } else if (strcmp(command, "ifacehex") == 0) {
            /* ifacehex <program-hex> <slot>: persisted interface, digest, lifecycle hash and ABI */
            uint8_t program_id[32];
            const lxp_module_kv_entry *item, *life;
            if (hex_decode(a, program_id, 32U) == 32) {
                item = find_kv(&kernels[atoi(b)], "interface", 10U, program_id);
                life = find_kv(&kernels[atoi(b)], "program", 8U, program_id);
                if (item != NULL && life != NULL && item->value_length > 72U &&
                    item->value_length - 72U == read_u32(item->value + 68U) &&
                    life->value_length == 71U &&
                    2U * item->value_length + 256U < sizeof(detail)) {
                    size_t i, cursor = 0U;
                    cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor,
                                               "abi=%u interface=",
                                               ((unsigned)life->value[65] << 8U) | life->value[66]);
                    for (i = 72U; i < item->value_length; ++i)
                        cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor,
                                                   "%02x", item->value[i]);
                    cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor, " digest=");
                    for (i = 36U; i < 68U; ++i)
                        cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor,
                                                   "%02x", item->value[i]);
                    cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor, " code=");
                    for (i = 33U; i < 65U; ++i)
                        cursor += (size_t)snprintf(detail + cursor, sizeof(detail) - cursor,
                                                   "%02x", life->value[i]);
                    failed = 0;
                }
            }
        } else if (strcmp(command, "restart") == 0) {
            failed = restart(atoi(a), atoi(b));
            (void)snprintf(detail, sizeof(detail), "step=%d", failed);
        }
        printf("%s %s %s\n", failed ? "fail" : "ok", label, detail);
        fflush(stdout);
    }
    return 0;
}
'''


def fail(message):
    print('programs_native_interfaces: FAILED: ' + message, file=sys.stderr)
    sys.exit(1)


def leb(value):
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        out.append(byte | (0x80 if value else 0))
        if not value:
            return bytes(out)


def name(text):
    return leb(len(text)) + text.encode()


def section(identifier, body):
    return bytes([identifier]) + leb(len(body)) + body


def vector(items):
    return leb(len(items)) + b''.join(items)


def module(result, import_module=None, import_name=None, distinct=0):
    """A guest exporting layerx_call/layerx_reserve/memory, optionally calling one host import."""
    types = [b'\x60\x02\x7f\x7f\x01\x7f', b'\x60\x01\x7f\x01\x7f',
             b'\x60\x04\x7f\x7f\x7f\x7f\x01\x7f']
    imported = 1 if import_module else 0
    out = b'\0asm\x01\0\0\0' + section(1, vector(types))
    if imported:
        out += section(2, vector([name(import_module) + name(import_name) + b'\x00\x02']))
    out += section(3, vector([b'\x00', b'\x01']))
    out += section(5, vector([b'\x00\x01']))
    out += section(7, vector([
        name('layerx_call') + b'\x00' + leb(imported),
        name('layerx_reserve') + b'\x00' + leb(imported + 1),
        name('memory') + b'\x02\x00']))
    if imported:
        call_body = b'\x00' + b'\x41\x00' * 4 + b'\x10\x00' + b'\x1a' + b'\x41' + leb(result) + b'\x0b'
    else:
        call_body = b'\x00\x41' + leb(result) + b'\x0b'
    if distinct:
        # Distinct code with identical behavior: leading i32.const/drop pairs.
        call_body = call_body[:1] + b'\x41\x00\x1a' * distinct + call_body[1:]
    reserve_body = b'\x00\x41\x00\x0b'
    out += section(10, vector([leb(len(call_body)) + call_body,
                               leb(len(reserve_body)) + reserve_body]))
    return out


def u16(value):
    return value.to_bytes(2, 'big')


def u32(value):
    return value.to_bytes(4, 'big')


def interface(code_hash, abi, calldata_bytes=64, capabilities=b'', capability_count=0):
    out = DOMAIN + code_hash + u16(abi) + u16(1)
    out += u16(11) + b'layerx_call' + b'\0\0\0\0'
    out += b'\x01\x20' + u32(calldata_bytes)
    out += b'\x01\x20' + u32(64)
    out += u16(capability_count) + capabilities
    out += u16(0) + u16(0)
    return out


def sha(data):
    return hashlib.sha256(data).digest()


def deploy(program, abi, wasm, encoding, code_hash=None):
    code_hash = code_hash or sha(wasm)
    return (program + u16(abi) + b'\x01\x00' + AUTHORITY + code_hash +
            u32(len(wasm)) + u32(len(encoding)) + encoding + wasm)


def upgrade(program, abi, old_hash, wasm, encoding, breaking=False):
    return (program + u16(abi) + bytes([2 if breaking else 0]) + b'\x00' + old_hash +
            sha(wasm) + u16(0) + u32(len(wasm)) + u32(len(encoding)) + encoding + wasm)


class Plan:
    def __init__(self):
        self.lines = []
        self.cases = []

    def add(self, case, command, *args):
        label = 'c%03d' % len(self.lines)
        self.lines.append(' '.join([label, command] + [str(a) for a in args]))
        self.cases.append((label, case))

    def dispatch(self, case, slot, ordinal, expected, body):
        self.add(case, 'dispatch', slot, ordinal, expected, body.hex())

    def execute(self, case, slot, ordinal, expected, body):
        self.add(case, 'execute', slot, ordinal, expected, body.hex())

    def iface(self, case, slot, program, version, encoding):
        self.add(case, 'iface', program.hex(), slot, version,
                 encoding.hex() if encoding else 'absent')

    def life(self, case, slot, program, abi, version, code_hash):
        self.add(case, 'life', program.hex(), slot, abi, code_hash.hex() + str(version))


def build_plan():
    plan = Plan()
    plan.add('kernel', 'new', 0)
    storage = (b'layerx_v1', 'storage_read')
    web = (b'layerx_v4', 'web_read')
    oracle = (b'layerx_v3', 'oracle_read')
    for abi in (1, 2, 3, 4):
        program = bytes([0x10 + abi]) * 32
        plain = module(abi)
        reading = module(16 + abi, storage[0].decode(), storage[1])
        tag = 'abi%d' % abi
        # Refusals first: nothing may be persisted by a refused deploy.
        other = module(40 + abi)
        plan.dispatch(tag + ':deploy-wrong-interface-hash', 0, 1, 'CONTEXT_MISMATCH',
                      deploy(program, abi, plain, interface(sha(other), abi)))
        wrong_abi = 1 + abi % 4
        plan.dispatch(tag + ':deploy-interface-abi-mismatch', 0, 1, 'CONTEXT_MISMATCH',
                      deploy(program, abi, plain, interface(sha(plain), wrong_abi)))
        plan.dispatch(tag + ':deploy-unknown-interface-abi', 0, 1, 'VERSION_UNSUPPORTED',
                      deploy(program, abi, plain, interface(sha(plain), 5)))
        plan.dispatch(tag + ':deploy-undeclared-capability', 0, 1, 'NON_CANONICAL',
                      deploy(program, abi, reading, interface(sha(reading), abi)))
        plan.dispatch(tag + ':deploy-overdeclared-capability', 0, 1, 'NON_CANONICAL',
                      deploy(program, abi, plain,
                             interface(sha(plain), abi, capabilities=b'\x00', capability_count=1)))
        if abi < 4:
            forbidden = module(50 + abi, web[0].decode(), web[1])
            plan.dispatch(tag + ':deploy-unsupported-import', 0, 1, 'NON_CANONICAL',
                          deploy(program, abi, forbidden, interface(sha(forbidden), abi)))
        if abi < 3:
            forbidden = module(54 + abi, oracle[0].decode(), oracle[1])
            plan.dispatch(tag + ':deploy-unsupported-oracle-import', 0, 1, 'NON_CANONICAL',
                          deploy(program, abi, forbidden, interface(sha(forbidden), abi)))
        plan.iface(tag + ':refusals-persist-nothing', 0, program, 0, None)
        # Valid interface-bearing deploy that declares exactly the imported capability.
        deployed = interface(sha(reading), abi, capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':deploy-accepted', 0, 1, 'OK', deploy(program, abi, reading, deployed))
        plan.iface(tag + ':deploy-persisted-interface', 0, program, 1, deployed)
        plan.life(tag + ':deploy-lifecycle-abi', 0, program, abi, 1, sha(reading))
        # Capability widening is refused without the explicit breaking authorization.
        widened = module(60 + abi, storage[0].decode(), 'storage_write')
        widened_iface = interface(sha(widened), abi, capabilities=b'\x01', capability_count=1)
        plan.dispatch(tag + ':upgrade-capability-widening-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(reading), widened, widened_iface))
        # Schema narrowing is a breaking change and needs the explicit flag.
        rebuilt = module(70 + abi, storage[0].decode(), storage[1])
        narrowed = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':upgrade-breaking-schema-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(reading), rebuilt, narrowed))
        plan.iface(tag + ':refused-upgrade-keeps-interface', 0, program, 1, deployed)
        plan.dispatch(tag + ':upgrade-unknown-abi', 0, 2, 'VERSION_UNSUPPORTED',
                      upgrade(program, 5, sha(reading), rebuilt, narrowed))
        plan.dispatch(tag + ':upgrade-wrong-interface-hash', 0, 2, 'CONTEXT_MISMATCH',
                      upgrade(program, abi, sha(reading), rebuilt,
                              interface(sha(reading), abi, capabilities=b'\x00',
                                        capability_count=1)))
        # Compatible upgrade: wider calldata, capability narrowed to none.
        compatible = module(80 + abi)
        compatible_iface = interface(sha(compatible), abi, calldata_bytes=96)
        plan.dispatch(tag + ':upgrade-compatible-narrowing', 0, 2, 'OK',
                      upgrade(program, abi, sha(reading), compatible, compatible_iface))
        plan.iface(tag + ':upgrade-persisted-interface', 0, program, 2, compatible_iface)
        plan.life(tag + ':upgrade-lifecycle-abi', 0, program, abi, 2, sha(compatible))
        # The narrowed contract is now binding: re-adding the capability is widening.
        plan.dispatch(tag + ':upgrade-rewidening-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(compatible), rebuilt,
                              interface(sha(rebuilt), abi, calldata_bytes=96,
                                        capabilities=b'\x00', capability_count=1)))
        breaking = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':upgrade-breaking-authorized', 0, 2, 'OK',
                      upgrade(program, abi, sha(compatible), rebuilt, breaking, breaking=True))
        plan.iface(tag + ':breaking-persisted-interface', 0, program, 3, breaking)
        plan.life(tag + ':breaking-lifecycle-abi', 0, program, abi, 3, sha(rebuilt))
        if abi > 1:
            plan.dispatch(tag + ':upgrade-downgrade-refused', 0, 2, 'VERSION_UNSUPPORTED',
                          upgrade(program, abi - 1, sha(rebuilt), compatible,
                                  interface(sha(compatible), abi - 1, calldata_bytes=96)))
    # Compatible cross-ABI upgrades under the central monotonic policy.
    program = bytes([0x30]) * 32
    current = module(90)
    encoding = interface(sha(current), 1)
    plan.dispatch('cross:deploy-abi1', 0, 1, 'OK', deploy(program, 1, current, encoding))
    for version, abi in enumerate((2, 3, 4), start=2):
        following = module(90 + abi)
        following_iface = interface(sha(following), abi)
        plan.dispatch('cross:upgrade-to-abi%d' % abi, 0, 2, 'OK',
                      upgrade(program, abi, sha(current), following, following_iface))
        plan.iface('cross:persisted-abi%d' % abi, 0, program, version, following_iface)
        plan.life('cross:lifecycle-abi%d' % abi, 0, program, abi, version, sha(following))
        current = following
    # Restart from a verified snapshot and keep consuming the persisted contracts.
    plan.add('restart:snapshot-restore', 'restart', 0, 1)
    for abi in (1, 2, 3, 4):
        program = bytes([0x10 + abi]) * 32
        rebuilt = module(70 + abi, 'layerx_v1', 'storage_read')
        breaking = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.iface('restart:abi%d-interface' % abi, 1, program, 3, breaking)
        plan.life('restart:abi%d-lifecycle' % abi, 1, program, abi, 3, sha(rebuilt))
        widened = module(60 + abi, 'layerx_v1', 'storage_write')
        plan.dispatch('restart:abi%d-widening-refused' % abi, 1, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(rebuilt), widened,
                              interface(sha(widened), abi, calldata_bytes=32,
                                        capabilities=b'\x01', capability_count=1)))
        after = module(100 + abi)
        after_iface = interface(sha(after), abi, calldata_bytes=64)
        plan.dispatch('restart:abi%d-compatible-upgrade' % abi, 1, 2, 'OK',
                      upgrade(program, abi, sha(rebuilt), after, after_iface))
        plan.iface('restart:abi%d-upgraded-interface' % abi, 1, program, 4, after_iface)
        plan.life('restart:abi%d-upgraded-lifecycle' % abi, 1, program, abi, 4, sha(after))
    return plan


ACCESS_ABSENT = b'LayerX/programs/access-declaration/v1\0\0'
CALL_BUDGET = (1000000, 16777216, 1048576, 1048576, 64, 1048576, 4096)


def call(program, abi, capabilities=b'\0\0'):
    entry = b'layerx_call'
    return (program + u16(abi) + u16(len(entry)) + u32(0) + u16(len(capabilities)) +
            u32(len(ACCESS_ABSENT)) + u32(16) +
            b''.join(v.to_bytes(8, 'big') for v in CALL_BUDGET) +
            entry + capabilities + ACCESS_ABSENT)


def add_call_plan(plan):
    """Deploy -> Call through lxp_kernel_execute_activity on a genesis-bootstrapped kernel."""
    typed = []
    plan.add('call:bootstrap-production-genesis', 'boot', 2)
    for abi in (1, 2, 3, 4):
        tag = 'call:abi%d' % abi
        program = bytes([0x50 + abi]) * 32
        first = module(0, distinct=abi)
        first_iface = interface(sha(first), abi)
        plan.execute(tag + ':deploy', 2, 1, 'OK', deploy(program, abi, first, first_iface))
        plan.iface(tag + ':deploy-persisted-interface', 2, program, 1, first_iface)
        plan.life(tag + ':deploy-lifecycle-abi', 2, program, abi, 1, sha(first))
        plan.execute(tag + ':call-after-deploy', 2, 3, 'OK/%d' % abi, call(program, abi))
        plan.execute(tag + ':call-abi-mismatch-refused', 2, 3, 'VERSION_UNSUPPORTED',
                     call(program, 1 + abi % 4))
        plan.execute(tag + ':call-unknown-abi-refused', 2, 3, 'VERSION_UNSUPPORTED',
                     call(program, 5))
        compatible = module(0, distinct=10 + abi)
        compatible_iface = interface(sha(compatible), abi, calldata_bytes=96)
        plan.execute(tag + ':upgrade-compatible', 2, 2, 'OK',
                     upgrade(program, abi, sha(first), compatible, compatible_iface))
        plan.execute(tag + ':call-after-compatible-upgrade', 2, 3, 'OK/%d' % abi,
                     call(program, abi))
        reading = module(0, 'layerx_v1', 'storage_read')
        plan.execute(tag + ':upgrade-capability-widening-refused', 2, 2, 'NON_CANONICAL',
                     upgrade(program, abi, sha(compatible), reading,
                             interface(sha(reading), abi, calldata_bytes=96,
                                       capabilities=b'\x00', capability_count=1)))
        plan.execute(tag + ':call-after-refused-upgrade', 2, 3, 'OK/%d' % abi,
                     call(program, abi))
        plan.iface(tag + ':upgrade-persisted-interface', 2, program, 2, compatible_iface)
        plan.life(tag + ':upgrade-lifecycle-abi', 2, program, abi, 2, sha(compatible))
    program = bytes([0x5f]) * 32
    current = module(0, distinct=30)
    plan.execute('call:cross:deploy-abi1', 2, 1, 'OK',
                 deploy(program, 1, current, interface(sha(current), 1)))
    plan.execute('call:cross:call-abi1', 2, 3, 'OK/1', call(program, 1))
    for version, abi in enumerate((2, 3, 4), start=2):
        following = module(0, distinct=30 + abi)
        plan.execute('call:cross:upgrade-to-abi%d' % abi, 2, 2, 'OK',
                     upgrade(program, abi, sha(current), following,
                             interface(sha(following), abi)))
        plan.execute('call:cross:call-abi%d' % abi, 2, 3, 'OK/%d' % abi, call(program, abi))
        plan.execute('call:cross:call-prior-abi%d-refused' % (abi - 1), 2, 3,
                     'VERSION_UNSUPPORTED', call(program, abi - 1))
        current = following
    plan.add('call:restart-snapshot-restore', 'restart', 2, 3)
    for abi in (1, 2, 3, 4):
        tag = 'call:restart:abi%d' % abi
        program = bytes([0x50 + abi]) * 32
        compatible = module(0, distinct=10 + abi)
        compatible_iface = interface(sha(compatible), abi, calldata_bytes=96)
        plan.iface(tag + ':persisted-interface', 3, program, 2, compatible_iface)
        plan.life(tag + ':lifecycle-abi', 3, program, abi, 2, sha(compatible))
        plan.execute(tag + ':call-after-restart', 3, 3, 'OK/%d' % abi, call(program, abi))
        plan.add(tag + ':stored-interface-read', 'ifacehex', program.hex(), 3)
        typed.append((abi, plan.cases[-1][0], compatible_iface, sha(compatible)))
    plan.execute('call:restart:cross-call-abi4', 3, 3, 'OK/4', call(bytes([0x5f]) * 32, 4))
    return typed


def typed_client(cli, evidence, stamp, typed, observed):
    """Feed the exact authenticated stored bytes to the shipped typed-binding generator."""
    results = []
    root = evidence / ('typed-bindings-%s' % stamp)
    root.mkdir(mode=0o700)
    for abi, label, expected_iface, code_hash in typed:
        status, detail = observed.get(label, ('missing', ''))
        fields = dict(part.split('=', 1) for part in detail.split() if '=' in part)
        stored = bytes.fromhex(fields.get('interface', ''))
        digest = fields.get('digest', '')
        code = fields.get('code', '')
        consistent = (status == 'ok' and fields.get('abi') == str(abi) and
                      stored == expected_iface and digest == sha(stored).hex() and
                      code == code_hash.hex())
        results.append({'case': 'typed:abi%d:stored-bytes-authenticated' % abi,
                        'status': 'ok' if consistent else 'fail',
                        'detail': 'abi=%s digest=%s' % (fields.get('abi'), digest)})
        path = root / ('abi%d.interface' % abi)
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as stream:
            stream.write(stored)
        for variant, bound_digest, want in (
                ('bindings-generated', digest, True),
                ('stale-digest-refused',
                 (bytes([digest and int(digest[:2], 16) ^ 1 or 0]).hex() + digest[2:]), False)):
            output = root / ('abi%d-%s' % (abi, variant))
            run = subprocess.run([str(cli), 'program', 'bindings', '--interface', str(path),
                                  '--digest', bound_digest, '--code-hash', code,
                                  '--output', str(output)],
                                 capture_output=True, text=True, timeout=120)
            generated = all((output / name).is_file()
                            for name in ('client.rs', 'client.ts', 'guest.rs', 'bindings.json'))
            passed = consistent and ((run.returncode == 0 and generated) if want
                                     else run.returncode != 0)
            results.append({'case': 'typed:abi%d:%s' % (abi, variant),
                            'status': 'ok' if passed else 'fail',
                            'detail': 'exit=%d %s' % (run.returncode,
                                                      (run.stderr or run.stdout).strip()[-300:])})
    return results


def private_dir(variable):
    value = os.environ.get(variable)
    if not value:
        fail('missing configuration %s' % variable)
    path = Path(value)
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.stat().st_mode & 0o077 or ROOT in path.resolve().parents:
        fail('%s must be a private directory outside the source tree' % variable)
    return path


def main():
    evidence = private_dir('PAXEER_X_EVIDENCE_DIR')
    manifest = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    if not manifest or not Path(manifest).is_file():
        fail('missing candidate manifest PAXEER_X_CANDIDATE_MANIFEST')
    revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, check=True,
                              capture_output=True, text=True).stdout.strip()
    target = os.environ.get('CARGO_TARGET_DIR')
    if not target:
        fail('missing configuration CARGO_TARGET_DIR (location of the shipped layerx CLI)')
    cli = Path(target) / 'debug' / 'layerx'
    for artifact, sources in ((KERNEL_LIB, KERNEL_SOURCES), (RUNTIME_LIB, SOURCES),
                              (cli, CLI_SOURCES)):
        if not artifact.is_file():
            fail('missing candidate artifact %s' % artifact)
        built = artifact.stat().st_mtime
        stale = [s for s in sources if (ROOT / s).stat().st_mtime > built]
        if stale:
            fail('candidate artifact %s is older than %s' % (artifact, stale))
    plan = build_plan()
    typed = add_call_plan(plan)
    with tempfile.TemporaryDirectory(prefix='programs-native-interfaces-') as scratch:
        source = Path(scratch) / 'driver.c'
        binary = Path(scratch) / 'driver'
        source.write_text(DRIVER)
        compiler = os.environ.get('CC', 'cc')
        command = [compiler, '-std=c17', '-O1', '-Wall', '-Wextra', '-Werror',
                   '-I' + str(ROOT / 'include'), '-I' + str(ROOT / 'build' / 'generated'),
                   str(source), str(KERNEL_LIB), str(RUNTIME_LIB),
                   '-lcrypto', '-pthread', '-ldl', '-lm', '-o', str(binary)]
        compiled = subprocess.run(command, capture_output=True, text=True)
        if compiled.returncode != 0:
            fail('driver compilation failed:\n' + compiled.stderr[-4000:])
        run = subprocess.run([str(binary)], input='\n'.join(plan.lines) + '\n',
                             capture_output=True, text=True, timeout=1500)
    observed = {}
    for line in run.stdout.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] in ('ok', 'fail'):
            observed[parts[1]] = (parts[0], ' '.join(parts[2:]))
    results = []
    for label, case in plan.cases:
        status, detail = observed.get(label, ('missing', ''))
        results.append({'case': case, 'status': status, 'detail': detail})
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    results.extend(typed_client(cli, evidence, stamp, typed, observed))
    passed = sum(1 for r in results if r['status'] == 'ok')
    record = evidence / ('programs-native-interfaces-%s.json' % stamp)
    payload = {
        'schema': 'paxeer-x.qualification.programs-native-interfaces.v1',
        'revision': revision,
        'command': 'timeout 30m python3 tools/qualification/paxeer-x/programs_native_interfaces.py',
        'driver_exit_code': run.returncode,
        'cases': results,
        'artifacts': {str(a.relative_to(ROOT)): hashlib.sha256(a.read_bytes()).hexdigest()
                      for a in (KERNEL_LIB, RUNTIME_LIB)} | {
                          str(cli): hashlib.sha256(cli.read_bytes()).hexdigest()},
        'stderr_tail': run.stderr[-2000:],
    }
    descriptor = os.open(record, os.O_WRONLY | os.O_CREAT | os.O_EXCL, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(payload, stream, indent=2)
    for result in results:
        print('%-5s %s %s' % (result['status'], result['case'], result['detail']))
    print('revision=%s evidence=%s' % (revision, record))
    print('cases=%d passed=%d skipped=0' % (len(results), passed))
    if run.returncode != 0 or passed != len(results):
        fail('%d of %d cases did not pass (driver exit %d)' %
             (len(results) - passed, len(results), run.returncode))
    print('programs_native_interfaces: PASSED')


if __name__ == '__main__':
    main()
