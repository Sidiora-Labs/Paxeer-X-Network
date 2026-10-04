#define _POSIX_C_SOURCE 200809L

#include "layerx/programs.h"

#include "layerx/lxp_genesis.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_governance.h"
#include "layerx/lxp_handover.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_module_ctx.h"

#include <openssl/evp.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

static int public_key_for(const uint8_t private_key[32], uint8_t public_key[32])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(
        EVP_PKEY_ED25519, NULL, private_key, 32U);
    size_t length = 32U;
    int ok = key != NULL && EVP_PKEY_get_raw_public_key(
        key, public_key, &length) == 1 && length == 32U;
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int sign_raw(const uint8_t private_key[32], const uint8_t *message,
                    size_t message_length, uint8_t signature[64])
{
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(
        EVP_PKEY_ED25519, NULL, private_key, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    size_t signature_length = 64U;
    int ok = key != NULL && context != NULL &&
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) == 1 &&
        EVP_DigestSign(context, signature, &signature_length,
                       message, message_length) == 1 &&
        signature_length == 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return ok ? 0 : 1;
}

static int migration_directory_fd = -1;
static uint8_t migration_governor_seed[32];
static lxp_genesis_manifest migration_genesis;
static lxp_u128 migration_funding = {0U, UINT64_C(1000000000)};
static uint32_t migration_u32(const uint8_t bytes[4]);

static int migration_write(const char *name, const void *bytes, size_t length)
{
    const uint8_t *source = bytes;
    int fd = openat(migration_directory_fd, name,
                    O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if (fd < 0) return 1;
    while (length != 0U) {
        ssize_t written = write(fd, source, length);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) { (void)close(fd); return 1; }
        source += (size_t)written; length -= (size_t)written;
    }
    if (fsync(fd) != 0) { (void)close(fd); return 1; }
    return close(fd) != 0;
}

static int project_metering_genesis_with(lxp_kernel *kernel,
                                        const uint8_t signer_private_key[32],
                                        int migration)
{
    static uint8_t arena_bytes[LXP_GENESIS_MAX_ENCODED_BYTES];
    static lxp_genesis_manifest manifest;
    lxp_arena arena;
    lxp_byte_span preimage;
    lx_programs_metering_schedule schedule;
    lx_programs_fee_genesis_parameters fees;
    uint8_t asset_id[32] = {1U};
    size_t index;
    (void)memset(&manifest, 0, sizeof(manifest));
    manifest.protocol_version = LXP_PROTOCOL_VERSION;
    manifest.network_id = 1U;
    manifest.genesis_timestamp_ms = 1U;
    manifest.parameter_count = 1U;
    manifest.parameters[0].module_id = LXP_MODULE_GOVERNANCE;
    (void)memcpy(manifest.parameters[0].key, "parameter-version", 17U);
    manifest.parameters[0].value[31] = 1U;
    if (migration) {
        manifest.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
        manifest.parameter_count = 2U;
        manifest.parameters[1].module_id = LXP_MODULE_GOVERNANCE;
        (void)memcpy(manifest.parameters[1].key, "handover-authority", 18U);
        if (public_key_for(migration_governor_seed, manifest.parameters[1].value) != 0)
            return 1;
    }
    manifest.guarantor_count = 1U;
    manifest.guarantors[0].guarantor_id[0] = 1U;
    (void)memset(manifest.guarantors[0].public_key, 1,
                 sizeof(manifest.guarantors[0].public_key));
    manifest.guarantors[0].bond = (lxp_u128){0U, 0U};
    if (public_key_for(signer_private_key, manifest.signer_public_key) != 0)
        return 1;
    (void)memset(&schedule, 0, sizeof(schedule));
    schedule.version = LXP_PROGRAM_METERING_SCHEDULE_VERSION_V1;
    for (index = 0U; index < 5U; ++index)
        schedule.coefficients[index] = 1U;
    schedule.coefficients[5] = 8U;
    schedule.coefficients[6] = 8U;
    schedule.coefficients[7] = 64U;
    schedule.coefficients[8] = 8U;
    schedule.activation_batch = 1U;
    schedule.authority_kind = LX_PROGRAMS_METERING_AUTHORITY_GENESIS;
    (void)memset(&fees, 0, sizeof(fees));
    fees.schedule = (lx_programs_fee_schedule){
        1U, 1U, 1U, 2U, 4U, 1U, 1U, 100U
    };
    (void)memcpy(fees.occupancy_asset_id, asset_id, 32U);
    fees.target_occupancy_byte_batches = 100U;
    fees.response_denominator = 1U;
    fees.maximum_change_numerator = 1U;
    fees.maximum_change_denominator = 10U;
    fees.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fees.maximum_fee_units_per_occupancy_byte_batch = 1000U;
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_genesis_fresh_empty_accounts(&manifest, asset_id) != LXP_OK ||
        lxp_hash_payload(manifest.signer_public_key, 32U,
                         schedule.authority_digest) != LXP_OK ||
        lxp_programs_metering_genesis_append(&manifest, &schedule) != LXP_OK ||
        lxp_programs_fee_genesis_append(&manifest, &fees) != LXP_OK ||
        lxp_genesis_state_root(&manifest, &arena,
                               manifest.genesis_state_root) != LXP_OK ||
        lxp_genesis_receipt_state_root(
            manifest.network_id, manifest.genesis_state_root,
            manifest.genesis_receipt_state_root) != LXP_OK ||
        lxp_genesis_encode(&manifest, false, &arena, &preimage) != LXP_OK ||
        sign_raw(signer_private_key, preimage.bytes, preimage.length,
                 manifest.signature) != 0 ||
        lxp_arena_reset(&arena, 0U) != LXP_OK ||
        lxp_programs_metering_genesis_project(&manifest, &arena, kernel) !=
            LXP_OK)
        return 1;
    if (migration) {
        if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
            lxp_programs_fee_genesis_project(&manifest, &arena, kernel) != LXP_OK ||
            lxp_handover_kernel_initialize(kernel, &manifest, NULL, NULL) != LXP_OK)
            return 1;
        migration_genesis = manifest;
    }
    if (migration && migration_directory_fd >= 0) {
        static int retained = 0;
        if (!retained) {
            if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
                lxp_genesis_verify_signature(&manifest, &arena) != LXP_OK ||
                lxp_arena_reset(&arena, 0U) != LXP_OK ||
                lxp_genesis_encode(&manifest, true, &arena, &preimage) != LXP_OK ||
                migration_write("signed-genesis.bin", preimage.bytes,
                                preimage.length) != 0) return 1;
            retained = 1;
        }
    }
    return 0;
}

static int project_metering_genesis(lxp_kernel *kernel)
{
    static const uint8_t signer_private_key[32] = {7U};
    return project_metering_genesis_with(kernel, signer_private_key, 0);
}

static void write_u16(uint8_t *bytes, uint16_t value)
{
    bytes[0] = (uint8_t)(value >> 8U);
    bytes[1] = (uint8_t)value;
}

static void write_u32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24U);
    bytes[1] = (uint8_t)(value >> 16U);
    bytes[2] = (uint8_t)(value >> 8U);
    bytes[3] = (uint8_t)value;
}

static size_t interface_payload(uint8_t *out, const uint8_t code_hash[32])
{
    static const uint8_t domain[] = "LayerX/program-interface/v1";
    size_t offset = 0U;
    (void)memcpy(out + offset, domain, sizeof(domain)); offset += sizeof(domain);
    (void)memcpy(out + offset, code_hash, 32U); offset += 32U;
    write_u16(out + offset, 1U); offset += 2U;
    write_u16(out + offset, 1U); offset += 2U;
    write_u16(out + offset, 11U); offset += 2U;
    (void)memcpy(out + offset, "layerx_call", 11U); offset += 11U;
    (void)memset(out + offset, 0, 4U); offset += 4U;
    out[offset++] = 1U; out[offset++] = 0x20U; write_u32(out + offset, 64U); offset += 4U;
    out[offset++] = 1U; out[offset++] = 0x20U; write_u32(out + offset, 64U); offset += 4U;
    write_u16(out + offset, 0U); offset += 2U;
    write_u16(out + offset, 0U); offset += 2U;
    write_u16(out + offset, 0U); offset += 2U;
    return offset;
}

static int interface_state_matches(const lxp_kernel *kernel,
                                   const uint8_t program_id[32],
                                   const uint8_t code_hash[32],
                                   uint32_t version)
{
    static const uint8_t prefix[] = "interface";
    size_t index;
    for (index = 0U; index < kernel->module_kv_count; ++index) {
        const lxp_module_kv_entry *item = &kernel->module_kv[index];
        const uint8_t *value = item->value;
        if (item->module_id != LXP_MODULE_PROGRAMS || item->key_length != 42U ||
            memcmp(item->key, prefix, sizeof(prefix)) != 0 ||
            memcmp(item->key + sizeof(prefix), program_id, 32U) != 0)
            continue;
        return item->value_length >= 134U &&
               memcmp(value, program_id, 32U) == 0 &&
               value[32] == (uint8_t)(version >> 24U) &&
               value[33] == (uint8_t)(version >> 16U) &&
               value[34] == (uint8_t)(version >> 8U) &&
               value[35] == (uint8_t)version &&
               memcmp(value + 100U, code_hash, 32U) == 0 ? 0 : 1;
    }
    return 1;
}

static int interface_state_absent(const lxp_kernel *kernel,
                                  const uint8_t program_id[32])
{
    static const uint8_t prefix[] = "interface";
    size_t index;
    for (index = 0U; index < kernel->module_kv_count; ++index) {
        const lxp_module_kv_entry *item = &kernel->module_kv[index];
        if (item->module_id == LXP_MODULE_PROGRAMS &&
            item->key_length == 42U &&
            memcmp(item->key, prefix, sizeof(prefix)) == 0 &&
            memcmp(item->key + sizeof(prefix), program_id, 32U) == 0)
            return 1;
    }
    return 0;
}

static int blob_matches(const lxp_kernel *kernel, const uint8_t hash[32],
                        const uint8_t *bytes, size_t length)
{
    size_t i;
    for (i = 0U; i < kernel->blob_count; ++i) {
        const lxp_module_blob *blob = &kernel->blobs[i];
        if (blob->module_id == LXP_MODULE_PROGRAMS &&
            memcmp(blob->key, hash, 32U) == 0)
            return blob->length == length &&
                   memcmp(blob->bytes, bytes, length) == 0 ? 0 : 1;
    }
    return 1;
}

static int dispatch(lxp_kernel *kernel, lxp_state_journal *journal,
                    lxp_authority_resolved *authority, uint16_t ordinal,
                    uint8_t *payload, size_t payload_length,
                    lxp_result expected)
{
    uint8_t arena_bytes[4096];
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity;
    const lxp_module_registration *registration;
    lxp_result module_result = LXP_OK;
    (void)journal;
    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity.payload = (lxp_byte_span){payload, payload_length};
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
        lxp_kernel_dispatch(registration, &ctx, &activity, authority,
                            &effects, &module_result) != LXP_OK ||
        module_result != expected)
        return 1;
    if (module_result == LXP_OK)
        return lxp_module_ctx_commit(&ctx) == LXP_OK ? 0 : 1;
    lxp_module_ctx_rollback(&ctx);
    return 0;
}

static int original_lifecycle(void)
{
    static const uint8_t wasm[] = {
        0,97,115,109,1,0,0,0,1,12,2,96,2,127,127,1,127,96,1,127,1,127,
        3,3,2,0,1,5,3,1,0,1,7,41,3,
        11,'l','a','y','e','r','x','_','c','a','l','l',0,0,
        14,'l','a','y','e','r','x','_','r','e','s','e','r','v','e',0,1,
        6,'m','e','m','o','r','y',2,0,10,11,2,4,0,65,0,11,4,0,65,0,11
    };
    static const uint8_t migration_wasm[] = {
        0,97,115,109,1,0,0,0,1,15,3,96,2,127,127,1,127,96,1,127,1,127,96,0,0,
        3,4,3,0,1,2,5,3,1,0,1,7,51,4,
        11,'l','a','y','e','r','x','_','c','a','l','l',0,0,
        14,'l','a','y','e','r','x','_','r','e','s','e','r','v','e',0,1,
        6,'m','e','m','o','r','y',2,0,7,'m','i','g','r','a','t','e',0,2,
        10,14,3,4,0,65,1,11,4,0,65,0,11,2,0,11
    };
    uint8_t deploy[512] = {0};
    uint8_t upgrade[512] = {0};
    uint8_t old_hash[32];
    uint8_t new_hash[32];
    uint8_t legacy_deploy[512] = {0};
    uint8_t legacy_upgrade[512] = {0};
    uint8_t legacy_program[32];
    uint8_t establish[512] = {0};
    uint8_t remove_interface[512] = {0};
    size_t establish_interface_length;
    size_t establish_length;
    size_t deploy_interface_length;
    size_t upgrade_interface_length;
    size_t deploy_length;
    size_t upgrade_length;
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_state_store sandbox_store;
    lxp_state_journal sandbox_journal;
    lxp_kernel sandbox_kernel;
    lxp_authority_resolved authority;
    uint64_t parameters = 1U;
    (void)memset(&authority, 0, sizeof(authority));
    (void)memset(authority.principal, 0x42, sizeof(authority.principal));
    if (lxp_hash_sha256(wasm, sizeof(wasm), old_hash) != LXP_OK ||
        lxp_hash_sha256(migration_wasm, sizeof(migration_wasm), new_hash) !=
            LXP_OK)
        return 1;
    (void)memset(deploy, 0x31, 32U);
    write_u16(deploy + 32U, 1U);
    deploy[34] = 1U;
    (void)memcpy(deploy + 36U, authority.principal, 32U);
    (void)memcpy(deploy + 68U, old_hash, 32U);
    write_u32(deploy + 100U, sizeof(wasm));
    deploy_interface_length = interface_payload(deploy + 108U, old_hash);
    write_u32(deploy + 104U, (uint32_t)deploy_interface_length);
    (void)memcpy(deploy + 108U + deploy_interface_length, wasm, sizeof(wasm));
    deploy_length = 108U + deploy_interface_length + sizeof(wasm);
    (void)memset(upgrade, 0x31, 32U);
    write_u16(upgrade + 32U, 1U);
    upgrade[34] = 1U;
    (void)memcpy(upgrade + 36U, old_hash, 32U);
    (void)memcpy(upgrade + 68U, new_hash, 32U);
    write_u16(upgrade + 100U, 7U);
    write_u32(upgrade + 102U, sizeof(migration_wasm));
    upgrade_interface_length = interface_payload(upgrade + 117U, new_hash);
    write_u32(upgrade + 106U, (uint32_t)upgrade_interface_length);
    (void)memcpy(upgrade + 110U, "migrate", 7U);
    (void)memcpy(upgrade + 117U + upgrade_interface_length, migration_wasm,
                 sizeof(migration_wasm));
    upgrade_length = 117U + upgrade_interface_length + sizeof(migration_wasm);
    if (lxp_state_store_init(&store, 0U) != LXP_OK ||
        lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
        project_metering_genesis(&kernel) != 0 ||
        lxp_kernel_register_module(&kernel, programs_module_registration()) != LXP_OK ||
        dispatch(&kernel, &journal, &authority, 1U, deploy, deploy_length,
                 LXP_OK) != 0 || kernel.blob_count != 1U ||
        blob_matches(&kernel, old_hash, wasm, sizeof(wasm)) != 0 ||
        interface_state_matches(&kernel, deploy, old_hash, 1U) != 0)
        return 1;
    (void)memset(legacy_program, 0x32, sizeof(legacy_program));
    (void)memcpy(legacy_deploy, legacy_program, 32U);
    write_u16(legacy_deploy + 32U, 1U);
    legacy_deploy[34] = 1U;
    (void)memcpy(legacy_deploy + 36U, authority.principal, 32U);
    (void)memcpy(legacy_deploy + 68U, old_hash, 32U);
    write_u32(legacy_deploy + 100U, sizeof(wasm));
    (void)memcpy(legacy_deploy + 104U, wasm, sizeof(wasm));
    (void)memcpy(legacy_upgrade, legacy_program, 32U);
    write_u16(legacy_upgrade + 32U, 1U);
    (void)memcpy(legacy_upgrade + 36U, old_hash, 32U);
    (void)memcpy(legacy_upgrade + 68U, new_hash, 32U);
    write_u16(legacy_upgrade + 100U, 0U);
    write_u32(legacy_upgrade + 102U, sizeof(migration_wasm));
    (void)memcpy(legacy_upgrade + 106U, migration_wasm,
                 sizeof(migration_wasm));
    if (dispatch(&kernel, &journal, &authority, 1U, legacy_deploy,
                 104U + sizeof(wasm), LXP_OK) != 0 ||
        interface_state_absent(&kernel, legacy_program) != 0 ||
        dispatch(&kernel, &journal, &authority, 2U, legacy_upgrade,
                 106U + sizeof(migration_wasm), LXP_OK) != 0 ||
        interface_state_absent(&kernel, legacy_program) != 0)
        return 1;
    (void)memcpy(establish, legacy_program, 32U);
    write_u16(establish + 32U, 1U);
    (void)memcpy(establish + 36U, new_hash, 32U);
    (void)memcpy(establish + 68U, old_hash, 32U);
    write_u16(establish + 100U, 0U);
    write_u32(establish + 102U, sizeof(wasm));
    establish_interface_length = interface_payload(establish + 110U,
                                                    old_hash);
    write_u32(establish + 106U, (uint32_t)establish_interface_length);
    (void)memcpy(establish + 110U + establish_interface_length,
                 wasm, sizeof(wasm));
    establish_length = 110U + establish_interface_length +
                       sizeof(wasm);
    if (dispatch(&kernel, &journal, &authority, 2U, establish,
                 establish_length, LXP_ERR_UNKNOWN_FIELD) != 0)
        return 1;
    establish[34] = 2U;
    if (dispatch(&kernel, &journal, &authority, 2U, establish,
                 establish_length, LXP_OK) != 0 ||
        interface_state_matches(&kernel, legacy_program, old_hash, 3U) != 0 ||
        dispatch(&kernel, &journal, &authority, 2U, legacy_upgrade,
                 106U + sizeof(migration_wasm),
                 LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    upgrade[110] = (uint8_t)'x';
    if (dispatch(&kernel, &journal, &authority, 2U, upgrade, upgrade_length,
                 LXP_ERR_UNKNOWN_ACTIVITY) != 0 || kernel.blob_count != 2U ||
        blob_matches(&kernel, old_hash, wasm, sizeof(wasm)) != 0)
        return 1;
    upgrade[110] = (uint8_t)'m';
    if (dispatch(&kernel, &journal, &authority, 2U, upgrade, upgrade_length,
                 LXP_OK) != 0 || kernel.blob_count != 2U ||
        blob_matches(&kernel, old_hash, wasm, sizeof(wasm)) != 0 ||
        blob_matches(&kernel, new_hash, migration_wasm,
                     sizeof(migration_wasm)) != 0 ||
        interface_state_matches(&kernel, upgrade, new_hash, 2U) != 0)
        return 1;
    upgrade[32] = 0U;
    upgrade[33] = 2U;
    if (dispatch(&kernel, &journal, &authority, 2U, upgrade, upgrade_length,
                 LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    upgrade[32] = 0U;
    upgrade[33] = 1U;
    (void)memcpy(remove_interface, deploy, 32U);
    write_u16(remove_interface + 32U, 1U);
    (void)memcpy(remove_interface + 36U, new_hash, 32U);
    (void)memcpy(remove_interface + 68U, old_hash, 32U);
    write_u16(remove_interface + 100U, 0U);
    write_u32(remove_interface + 102U, sizeof(wasm));
    write_u32(remove_interface + 106U, 0U);
    (void)memcpy(remove_interface + 110U, wasm, sizeof(wasm));
    if (dispatch(&kernel, &journal, &authority, 2U, remove_interface,
                 110U + sizeof(wasm), LXP_ERR_NON_CANONICAL) != 0 ||
        interface_state_matches(&kernel, upgrade, new_hash, 2U) != 0)
        return 1;
    remove_interface[34] = 2U;
    if (dispatch(&kernel, &journal, &authority, 2U, remove_interface,
                 110U + sizeof(wasm), LXP_OK) != 0 ||
        interface_state_absent(&kernel, deploy) != 0)
        return 1;
    (void)memset(deploy, 0x33, 32U);
    if (lxp_state_store_init(&sandbox_store, 0U) != LXP_OK ||
        lxp_kernel_create(&sandbox_kernel, &sandbox_store, &sandbox_journal,
                          &parameters, 0U) != LXP_OK ||
        project_metering_genesis(&sandbox_kernel) != 0 ||
        lxp_kernel_register_module(&sandbox_kernel,
                                   programs_module_registration_v3()) !=
            LXP_OK ||
        dispatch(&sandbox_kernel, &sandbox_journal, &authority, 1U, deploy,
                 deploy_length, LXP_OK) != 0 ||
        lxp_state_store_destroy(&sandbox_store) != LXP_OK)
        return 1;
    return lxp_state_store_destroy(&store) == LXP_OK ? 0 : 1;
}


enum { ADMISSION_BUFFER_BYTES = 2U * 1048576U };

typedef struct admission_bytes {
    uint8_t *bytes;
    size_t length;
} admission_bytes;

static void admission_byte(admission_bytes *out, uint8_t value)
{
    if (out->length >= ADMISSION_BUFFER_BYTES) abort();
    out->bytes[out->length++] = value;
}

static void admission_leb(admission_bytes *out, uint32_t value)
{
    do {
        uint8_t byte = (uint8_t)(value & 127U);
        value >>= 7U;
        admission_byte(out, (uint8_t)(byte | (value != 0U ? 128U : 0U)));
    } while (value != 0U);
}

static void admission_section(admission_bytes *out, uint8_t id,
                               const admission_bytes *section)
{
    admission_byte(out, id);
    admission_leb(out, (uint32_t)section->length);
    if (section->length > ADMISSION_BUFFER_BYTES - out->length) abort();
    (void)memcpy(out->bytes + out->length, section->bytes, section->length);
    out->length += section->length;
}

static size_t admission_functions(uint8_t *out, uint32_t count,
                                  uint32_t locals, uint32_t operands,
                                  int chain)
{
    static uint8_t section_storage[ADMISSION_BUFFER_BYTES];
    static uint8_t body_storage[ADMISSION_BUFFER_BYTES];
    static const uint8_t header[] = {0,97,115,109,1,0,0,0};
    admission_bytes module = {out, sizeof(header)};
    admission_bytes section = {section_storage, 0U};
    (void)memcpy(out, header, sizeof(header));
    admission_byte(&section, 1U);
    admission_byte(&section, 0x60U);
    admission_byte(&section, 0U);
    admission_byte(&section, 0U);
    admission_section(&module, 1U, &section);
    section.length = 0U;
    admission_leb(&section, count);
    for (uint32_t index = 0U; index < count; ++index)
        admission_byte(&section, 0U);
    admission_section(&module, 3U, &section);
    section.length = 0U;
    admission_leb(&section, count);
    for (uint32_t index = 0U; index < count; ++index) {
        admission_bytes body = {body_storage, 0U};
        admission_byte(&body, (uint8_t)(locals == 0U ? 0U : 1U));
        if (locals != 0U) {
            admission_leb(&body, locals);
            admission_byte(&body, 0x7fU);
        }
        for (uint32_t operand = 0U; operand < operands; ++operand) {
            admission_byte(&body, 0x41U);
            admission_byte(&body, 0U);
        }
        for (uint32_t operand = 0U; operand < operands; ++operand)
            admission_byte(&body, 0x1aU);
        if (chain && index + 1U < count) {
            admission_byte(&body, 0x10U);
            admission_leb(&body, index + 1U);
        }
        admission_byte(&body, 0x0bU);
        admission_leb(&section, (uint32_t)body.length);
        if (body.length > ADMISSION_BUFFER_BYTES - section.length) abort();
        (void)memcpy(section.bytes + section.length, body.bytes, body.length);
        section.length += body.length;
    }
    admission_section(&module, 10U, &section);
    return module.length;
}

static int admission_record_present(const lxp_kernel *kernel,
                                     const uint8_t program[32])
{
    static const uint8_t prefix[] = "program";
    for (size_t index = 0U; index < kernel->module_kv_count; ++index) {
        const lxp_module_kv_entry *item = &kernel->module_kv[index];
        if (item->module_id == LXP_MODULE_PROGRAMS &&
            item->key_length == sizeof(prefix) + 32U &&
            memcmp(item->key, prefix, sizeof(prefix)) == 0 &&
            memcmp(item->key + sizeof(prefix), program, 32U) == 0)
            return 1;
    }
    return 0;
}

static int admission_blob_present(const lxp_kernel *kernel,
                                   const uint8_t hash[32])
{
    for (size_t index = 0U; index < kernel->blob_count; ++index)
        if (kernel->blobs[index].module_id == LXP_MODULE_PROGRAMS &&
            memcmp(kernel->blobs[index].key, hash, 32U) == 0)
            return 1;
    return 0;
}

static int admission_case(lxp_kernel *kernel, lxp_authority_resolved *authority,
                           const char *name, int framed, uint16_t abi,
                           const uint8_t *wasm, size_t wasm_length,
                           int invalid_capability, lxp_result expected,
                           unsigned int serial, const char *phase)
{
    static uint8_t payload[ADMISSION_BUFFER_BYTES];
    uint8_t arena_bytes[4096];
    uint8_t hash[32], before_root[32], after_root[32];
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity = {0};
    const lxp_module_registration *registration;
    lxp_result result = LXP_OK;
    lxp_result status;
    size_t interface_length = 0U;
    size_t offset = framed ? 108U : 104U;
    size_t kv_before = kernel->module_kv_count;
    size_t blobs_before = kernel->blob_count;
    size_t blob_bytes_before = kernel->blob_total_bytes;
    (void)memset(payload, 0, 108U);
    payload[0] = 0xa5U;
    write_u32(payload + 28U, serial);
    write_u16(payload + 32U, abi);
    payload[34] = 1U;
    (void)memcpy(payload + 36U, authority->principal, 32U);
    if (lxp_hash_sha256(wasm, wasm_length, hash) != LXP_OK ||
        lxp_state_root(kernel, before_root) != LXP_OK)
        return 1;
    (void)memcpy(payload + 68U, hash, 32U);
    write_u32(payload + 100U, (uint32_t)wasm_length);
    if (framed) {
        interface_length = interface_payload(payload + offset, hash);
        write_u16(payload + offset + 60U, abi);
        if (abi == 3U || abi == 4U)
            payload[offset + 26U] = (uint8_t)('0' + abi);
        if (invalid_capability) {
            size_t capabilities = offset + interface_length - 6U;
            write_u16(payload + capabilities, 1U);
            (void)memmove(payload + capabilities + 3U,
                           payload + capabilities + 2U, 4U);
            payload[capabilities + 2U] = 0xffU;
            ++interface_length;
        }
        write_u32(payload + 104U, (uint32_t)interface_length);
        offset += interface_length;
    }
    if (wasm_length > sizeof(payload) - offset) return 1;
    (void)memcpy(payload + offset, wasm, wasm_length);
    activity.activity_type = LX_PROGRAMS_DEPLOY;
    activity.payload = (lxp_byte_span){payload, offset + wasm_length};
    {
        const char *directory = getenv("PAXEER_X_PROGRAM_VALIDATION_INPUTS");
        if (directory != NULL) {
            char path[4096];
            FILE *file;
            int written = snprintf(path, sizeof(path), "%s/%s.%s.%s.abi%u.bin",
                directory, phase, name, framed ? "framed" : "legacy", (unsigned)abi);
            if (written < 0 || (size_t)written >= sizeof(path)) return 1;
            file = fopen(path, "wb");
            if (file == NULL) return 1;
            if (fwrite(payload, 1U, activity.payload.length, file) != activity.payload.length) {
                (void)fclose(file);
                return 1;
            }
            if (fclose(file) != 0) return 1;
        }
    }
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U,
                                       &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 1U, 0U,
                            serial, 4000000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    ctx.batch_number = 1U;
    status = lxp_kernel_dispatch(registration, &ctx, &activity, authority,
                                 &effects, &result);
    if (status != LXP_OK || result != expected) {
        (void)fprintf(stderr, "Admission %s/%s/%s abi=%u status=%d result=%d expected=%d\n",
            name, framed ? "framed" : "legacy", phase, (unsigned)abi,
            (int)status, (int)result, (int)expected);
        lxp_module_ctx_rollback(&ctx);
        return 1;
    }
    if (expected != LXP_OK &&
        (ctx.staged_count != 0U || ctx.staged_blob_count != 0U ||
         ctx.staged_account_count != 0U || effects.count != 0U)) {
        lxp_module_ctx_rollback(&ctx);
        return 1;
    }
    if (expected == LXP_OK) {
        if (lxp_module_ctx_commit(&ctx) != LXP_OK) return 1;
    } else {
        lxp_module_ctx_rollback(&ctx);
        if (lxp_state_root(kernel, after_root) != LXP_OK ||
            memcmp(before_root, after_root, sizeof(before_root)) != 0 ||
            kernel->module_kv_count != kv_before ||
            kernel->blob_count != blobs_before ||
            kernel->blob_total_bytes != blob_bytes_before ||
            admission_record_present(kernel, payload) ||
            interface_state_absent(kernel, payload) != 0)
            return 1;
    }
    if (expected == LXP_OK) {
        int found = 0;
        for (size_t index = 0U; index < kernel->module_kv_count; ++index) {
            const lxp_module_kv_entry *item = &kernel->module_kv[index];
            if (item->module_id == LXP_MODULE_PROGRAMS &&
                item->key_length == 40U &&
                memcmp(item->key, "program", 8U) == 0 &&
                memcmp(item->key + 8U, payload, 32U) == 0) {
                found = item->value_length == 71U &&
                    item->value[65] == (uint8_t)(abi >> 8U) &&
                    item->value[66] == (uint8_t)abi &&
                    memcmp(item->value + 33U, hash, 32U) == 0;
            }
        }
        if (!found || !admission_blob_present(kernel, hash)) return 1;
    }
    if (printf("ALL_FORM_CASE {\"name\":\"%s\",\"form\":\"%s\",\"phase\":\"%s\",\"abi\":%u,\"result\":%d,\"expected\":%d,\"artifact_present\":%s,\"lifecycle_present\":%s,\"new_artifacts\":%zu,\"staged\":%zu,\"effects\":%zu,\"state_unchanged\":%s}\n",
        name, framed ? "framed" : "legacy", phase, (unsigned)abi,
        (int)result, (int)expected,
        admission_blob_present(kernel, hash) ? "true" : "false",
        admission_record_present(kernel, payload) ? "true" : "false",
        kernel->blob_count - blobs_before, ctx.staged_count + ctx.staged_blob_count +
            ctx.staged_account_count, (size_t)effects.count,
        expected != LXP_OK ? "true" : "false") < 0)
        return 1;
    return 0;
}

static int all_form_validation(void)
{
    static const uint8_t valid[] = {
        0,97,115,109,1,0,0,0,1,12,2,96,2,127,127,1,127,96,1,127,1,127,
        3,3,2,0,1,5,3,1,0,1,7,41,3,
        11,'l','a','y','e','r','x','_','c','a','l','l',0,0,
        14,'l','a','y','e','r','x','_','r','e','s','e','r','v','e',0,1,
        6,'m','e','m','o','r','y',2,0,10,11,2,4,0,65,0,11,4,0,65,0,11
    };
    static const uint8_t malformed[] = {0,97,115,109,1,0,0,0,10,2,1,4};
    static const uint8_t float_type[] = {0,97,115,109,1,0,0,0,1,5,1,96,0,1,125};
    static const uint8_t ambient[] = {
        0,97,115,109,1,0,0,0,1,4,1,96,0,0,2,13,1,
        3,'e','n','v',5,'c','l','o','c','k',0,0
    };
    static uint8_t generated[ADMISSION_BUFFER_BYTES];
    static uint8_t snapshot_storage[ADMISSION_BUFFER_BYTES];
    static lxp_kernel kernel, restored;
    lxp_state_store store, restored_store;
    lxp_state_journal journal, restored_journal;
    lxp_authority_resolved authority = {0};
    lxp_arena snapshot_arena;
    lxp_byte_span snapshot;
    lxp_snapshot_manifest_record manifest;
    uint8_t root[32];
    uint64_t parameters = 1U;
    unsigned int serial = 1U, cases = 0U;
    int failed = 0;
    (void)memset(authority.principal, 0x42, sizeof(authority.principal));
    if (lxp_state_store_init(&store, 1U) != LXP_OK ||
        lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
        project_metering_genesis(&kernel) != 0 ||
        lxp_kernel_register_module(&kernel, programs_module_registration_v4()) != LXP_OK)
        return 1;
    for (int phase = 0; phase < 2 && !failed; ++phase) {
        lxp_kernel *active = phase == 0 ? &kernel : &restored;
        const char *phase_name = phase == 0 ? "initial" : "snapshot_replay";
        for (int form = 0; form < 2 && !failed; ++form) {
            for (uint16_t abi = 1U; abi <= 4U && !failed; ++abi) {
                const struct {
                    const char *name;
                    const uint8_t *bytes;
                    size_t length;
                } invalid[] = {
                    {"malformed_body", malformed, sizeof(malformed)},
                    {"float_type", float_type, sizeof(float_type)},
                    {"ambient_import", ambient, sizeof(ambient)}
                };
                for (size_t index = 0U; index < sizeof(invalid) / sizeof(invalid[0]); ++index) {
                    failed = admission_case(active, &authority, invalid[index].name,
                        form, abi, invalid[index].bytes, invalid[index].length,
                        0, LXP_ERR_NON_CANONICAL, serial++, phase_name);
                    if (failed) break;
                    ++cases;
                }
            }
            if (failed) break;
            failed = admission_case(active, &authority, "unsupported_abi", form,
                5U, valid, sizeof(valid), 0, LXP_ERR_VERSION_UNSUPPORTED,
                serial++, phase_name);
            if (failed) break;
            ++cases;
            for (unsigned int bound = 0U; bound < 5U && !failed; ++bound) {
                static const char *names[] = {"module_bytes", "function_count",
                    "local_stack", "operand_stack", "call_depth"};
                size_t length;
                if (bound == 0U) {
                    admission_bytes module = {generated, sizeof(valid)};
                    (void)memcpy(generated, valid, sizeof(valid));
                    admission_byte(&module, 0U);
                    admission_leb(&module, 1048576U);
                    (void)memset(generated + module.length, 0, 1048576U);
                    length = module.length + 1048576U;
                } else {
                    length = admission_functions(generated,
                        bound == 1U ? 4097U : bound == 4U ? 513U : 1U,
                        bound == 2U ? 65537U : 0U,
                        bound == 3U ? 65537U : 0U, bound == 4U);
                }
                failed = admission_case(active, &authority, names[bound], form,
                    1U, generated, length, 0, LXP_ERR_NON_CANONICAL,
                    serial++, phase_name);
                if (!failed) ++cases;
            }
            if (failed) break;
            if (form) {
                failed = admission_case(active, &authority, "invalid_capability", form,
                    1U, valid, sizeof(valid), 1, LXP_ERR_NON_CANONICAL,
                    serial++, phase_name);
                if (failed) break;
                ++cases;
            }
            for (uint16_t abi = 1U; abi <= 4U && !failed; ++abi) {
                failed = admission_case(active, &authority, "valid", form, abi,
                    valid, sizeof(valid), 0, LXP_OK, serial++, phase_name);
                if (!failed) ++cases;
            }
        }
        if (phase == 0 && !failed) {
            if (lxp_arena_init(&snapshot_arena, snapshot_storage,
                               sizeof(snapshot_storage)) != LXP_OK ||
                lxp_state_root(&kernel, root) != LXP_OK ||
                lxp_snapshot_write(&kernel, 0U, &snapshot_arena, &snapshot) != LXP_OK ||
                lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length, 0U,
                    root, kernel.current_state_root, &manifest) != LXP_OK ||
                lxp_state_store_init(&restored_store, 1U) != LXP_OK ||
                lxp_kernel_create(&restored, &restored_store, &restored_journal,
                                  &parameters, 0U) != LXP_OK ||
                lxp_kernel_register_module(&restored,
                    programs_module_registration_v4()) != LXP_OK ||
                lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest,
                                  &restored) != LXP_OK ||
                lxp_snapshot_verify_root(&restored, &manifest) != LXP_OK ||
                restored.module_kv_count != kernel.module_kv_count ||
                restored.blob_count != kernel.blob_count)
                failed = 1;
        }
    }
    while (kernel.blob_count != 0U) free(kernel.blobs[--kernel.blob_count].bytes);
    while (restored.blob_count != 0U) free(restored.blobs[--restored.blob_count].bytes);
    if (lxp_state_store_destroy(&store) != LXP_OK) failed = 1;
    if (!failed && lxp_state_store_destroy(&restored_store) != LXP_OK) failed = 1;
    if (!failed && printf("ALL_FORM_VALIDATION cases=%u skipped=0\n", cases) < 0)
        failed = 1;
    return failed;
}

static void migration_name(admission_bytes *out, const char *name)
{
    admission_leb(out, (uint32_t)strlen(name));
    for (size_t i = 0U; i < strlen(name); ++i)
        admission_byte(out, (uint8_t)name[i]);
}

static size_t migration_module(uint8_t *out, uint16_t abi, unsigned int kind)
{
    static const uint8_t header[] = {0,97,115,109,1,0,0,0};
    uint8_t section_bytes[1024], body_bytes[256];
    admission_bytes module = {out, sizeof(header)};
    admission_bytes section = {section_bytes, 0U};
    admission_bytes body = {body_bytes, 0U};
    const char *names[] = {"storage_read", "context_read", "oracle_read", "web_read"};
    char namespace[16];
    unsigned int parameters = abi == 2U ? 3U : 4U;
    (void)memcpy(out, header, sizeof(header));
    admission_byte(&section, 4U);
    admission_byte(&section, 0x60U); admission_byte(&section, (uint8_t)parameters);
    for (unsigned int i = 0U; i < parameters; ++i) admission_byte(&section, 0x7fU);
    admission_byte(&section, 1U); admission_byte(&section, 0x7fU);
    admission_byte(&section, 0x60U); admission_byte(&section, 2U);
    admission_byte(&section, 0x7fU); admission_byte(&section, 0x7fU);
    admission_byte(&section, 1U); admission_byte(&section, 0x7fU);
    admission_byte(&section, 0x60U); admission_byte(&section, 1U);
    admission_byte(&section, 0x7fU); admission_byte(&section, 1U); admission_byte(&section, 0x7fU);
    admission_byte(&section, 0x60U); admission_byte(&section, 0U); admission_byte(&section, 0U);
    admission_section(&module, 1U, &section);
    section.length = 0U; admission_byte(&section, 1U);
    (void)snprintf(namespace, sizeof(namespace), "layerx_v%u", (unsigned)abi);
    migration_name(&section, namespace); migration_name(&section, names[abi - 1U]);
    admission_byte(&section, 0U); admission_byte(&section, 0U);
    admission_section(&module, 2U, &section);
    section.length = 0U;
    admission_byte(&section, 3U); admission_byte(&section, 1U);
    admission_byte(&section, 2U); admission_byte(&section, 3U);
    admission_section(&module, 3U, &section);
    section.length = 0U;
    admission_byte(&section, 1U); admission_byte(&section, 0U); admission_byte(&section, 1U);
    admission_section(&module, 5U, &section);
    section.length = 0U; admission_byte(&section, 4U);
    migration_name(&section, "layerx_call"); admission_byte(&section, 0U); admission_byte(&section, 1U);
    migration_name(&section, "layerx_reserve"); admission_byte(&section, 0U); admission_byte(&section, 2U);
    migration_name(&section, "memory"); admission_byte(&section, 2U); admission_byte(&section, 0U);
    migration_name(&section, "migrate"); admission_byte(&section, 0U); admission_byte(&section, 3U);
    admission_section(&module, 7U, &section);
    section.length = 0U; admission_byte(&section, 3U);
    for (unsigned int i = 0U; i < 2U; ++i) {
        admission_byte(&section, 4U); admission_byte(&section, 0U);
        admission_byte(&section, 0x41U); admission_byte(&section, 0U); admission_byte(&section, 0x0bU);
    }
    admission_byte(&body, 0U);
    if (kind == 1U) admission_byte(&body, 0U);
    else if (kind == 2U) {
        admission_byte(&body, 0x03U); admission_byte(&body, 0x40U);
        admission_byte(&body, 0x0cU); admission_byte(&body, 0U); admission_byte(&body, 0x0bU);
    } else {
        for (unsigned int i = 0U; i < parameters; ++i) {
            admission_byte(&body, 0x41U); admission_byte(&body, 0U);
        }
        admission_byte(&body, 0x10U); admission_byte(&body, 0U); admission_byte(&body, 0x1aU);
        if (kind == 4U) admission_byte(&body, 0x01U);
    }
    admission_byte(&body, 0x0bU);
    admission_leb(&section, (uint32_t)body.length);
    (void)memcpy(section.bytes + section.length, body.bytes, body.length);
    section.length += body.length;
    admission_section(&module, 10U, &section);
    return module.length;
}

static const lxp_module_kv_entry *migration_record(const lxp_kernel *kernel,
                                                   const uint8_t program[32])
{
    for (size_t i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *item = &kernel->module_kv[i];
        if (item->module_id == LXP_MODULE_PROGRAMS && item->key_length == 40U &&
            memcmp(item->key, "program", 8U) == 0 &&
            memcmp(item->key + 8U, program, 32U) == 0) return item;
    }
    return NULL;
}

static void migration_hex(const uint8_t bytes[32], char out[65])
{
    static const char hex[] = "0123456789abcdef";
    for (size_t i = 0U; i < 32U; ++i) {
        out[i * 2U] = hex[bytes[i] >> 4U]; out[i * 2U + 1U] = hex[bytes[i] & 15U];
    }
    out[64] = '\0';
}

static uint64_t migration_word(const uint8_t *bytes)
{
    uint64_t word = 0U;
    for (size_t i = 0U; i < 8U; ++i) word = (word << 8U) | bytes[i];
    return word;
}

static int migration_activity(lxp_kernel *kernel, lxp_activity *activity,
                               lxp_identity *identity, const uint8_t seed[32],
                               uint8_t signature[64], uint8_t *payload,
                               size_t length, uint16_t ordinal,
                               lxp_authority_grant *grant,
                               lxp_authority_resolved *authority)
{
    static const uint8_t owner_did[] = "did:lxp:native-migration-owner";
    static const uint8_t other_did[] = "did:lxp:native-migration-other";
    uint8_t digest[32];
    (void)memset(activity, 0, sizeof(*activity));
    activity->protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity->network_id = 1U;
    activity->activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity->actor_did = (lxp_byte_span){owner_did, sizeof(owner_did) - 1U};
    {
        uint8_t owner_id[32];
        if (lxp_did_id_derive(owner_did, sizeof(owner_did) - 1U, owner_id) != LXP_OK) return 1;
        if (memcmp(identity->did_id, owner_id, 32U) != 0)
            activity->actor_did = (lxp_byte_span){other_did, sizeof(other_did) - 1U};
    }
    activity->authority = (lxp_byte_span){identity->primary_key, 32U};
    activity->account_sequence = identity->next_sequence;
    activity->timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity->idempotency_key[0] = (uint8_t)ordinal;
    activity->fee_limit = (lxp_u128){0U, 100000000U};
    activity->payload = (lxp_byte_span){payload, length};
    activity->signature = (lxp_byte_span){signature, 64U};
    if (lxp_hash_payload(payload, length, activity->payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(activity, digest) != LXP_OK ||
        sign_raw(seed, digest, sizeof(digest), signature) != 0 ||
        lxp_activity_verify_signature(activity) != LXP_OK ||
        lxp_activity_check_envelope(activity, 1U) != LXP_OK ||
        !lxp_identity_key_valid(identity, identity->primary_key, 10U, 1U) ||
        lxp_authority_resolve_activity(kernel, identity, activity, true, true,
            10U, 100U, 1U, grant, authority) != LXP_OK) return 1;
    return 0;
}

static int migration_actor(lxp_kernel *kernel, lxp_activity *activity,
                            lxp_identity *identity, const uint8_t seed[32],
                            uint8_t signature[64], uint32_t type,
                            const uint8_t *payload, size_t length,
                            uint64_t limit, uint8_t nonce,
                            lxp_authority_grant *grant,
                            lxp_authority_resolved *authority)
{
    static const char *dids[] = {"did:lxp:native-migration-owner",
        "did:lxp:native-migration-other", "did:lxp:native-migration-governor"};
    uint8_t digest[32], did_id[32];
    const char *did = NULL;
    for (size_t i = 0U; i < sizeof(dids) / sizeof(dids[0]); ++i) {
        if (lxp_did_id_derive((const uint8_t *)dids[i], strlen(dids[i]), did_id) != LXP_OK)
            return 1;
        if (memcmp(identity->did_id, did_id, 32U) == 0) did = dids[i];
    }
    if (did == NULL) return 1;
    (void)memset(activity, 0, sizeof(*activity));
    activity->protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity->network_id = 1U;
    activity->activity_type = type;
    activity->actor_did = (lxp_byte_span){(const uint8_t *)did, strlen(did)};
    activity->authority = (lxp_byte_span){identity->primary_key, 32U};
    activity->account_sequence = identity->next_sequence;
    activity->timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    activity->idempotency_key[31] = nonce;
    activity->fee_limit = (lxp_u128){0U, limit};
    activity->payload = (lxp_byte_span){payload, length};
    activity->signature = (lxp_byte_span){signature, 64U};
    return lxp_hash_payload(payload, length, activity->payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(activity, digest) != LXP_OK ||
        sign_raw(seed, digest, sizeof(digest), signature) != 0 ||
        lxp_activity_verify_signature(activity) != LXP_OK ||
        lxp_authority_resolve_activity(kernel, identity, activity, true, true,
            10U, 100U, kernel->state->next_sequence, grant, authority) != LXP_OK;
}

static int migration_runtime(lxp_kernel *kernel)
{
    static const char *names[] = {"agent:did:lxp:native-migration-owner:main",
        "agent:did:lxp:native-migration-other:main",
        "agent:did:lxp:native-migration-governor:main", "system:fees"};
    uint8_t asset[32] = {1U}, id[32];
    lx_programs_transfer_runtime *runtime;
    lxp_transfer_asset_state *asset_state;
    if (kernel->module_runtime[LXP_MODULE_PROGRAMS] != NULL) return 0;
    runtime = calloc(1U, sizeof(*runtime));
    if (runtime == NULL) return 1;
    runtime->accounts = kernel->state->accounts;
    if (runtime->accounts == NULL) {
        runtime->accounts = calloc(1U, sizeof(*runtime->accounts));
        if (runtime->accounts == NULL || lx_account_registry_init(runtime->accounts) != LXP_OK ||
            lxp_state_store_bind_accounts(kernel->state, runtime->accounts) != LXP_OK)
            return 1;
    }
    asset_state = calloc(1U, sizeof(*asset_state));
    if (asset_state == NULL) return 1;
    (void)memcpy(asset_state->asset_id, asset, 32U);
    asset_state->registered = true;
    runtime->assets = asset_state;
    runtime->asset_count = 1U;
    (void)memcpy(runtime->occupancy_asset_id, asset, 32U);
    runtime->resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    runtime->occupancy_parameter_context = kernel;
    runtime->resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    runtime->metering_schedule_context = kernel;
    for (size_t i = 0U; i < sizeof(names) / sizeof(names[0]); ++i) {
        lx_account *account;
        size_t slot;
        if (lx_account_id_from_string((const uint8_t *)names[i], strlen(names[i]), id) != LXP_OK)
            return 1;
        if (lx_account_registry_index_lookup(runtime->accounts, id, &slot) == LXP_OK)
            continue;
        if (lx_account_open(runtime->accounts, (const uint8_t *)names[i], strlen(names[i]),
                id, i == 3U ? 2U : 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
            lxp_ledger_bootstrap_balance(account, asset,
                i == 3U ? (lxp_u128){0U, 0U} : migration_funding, 0U) != LXP_OK)
            return 1;
    }
    return lxp_state_store_require_account_root(kernel->state) != LXP_OK ||
        lxp_kernel_bind_module_runtime(kernel, LXP_MODULE_PROGRAMS, runtime) != LXP_OK ||
        lxp_programs_bind_fee_transaction(kernel) != LXP_OK ||
        lxp_kernel_set_capabilities(kernel, NULL, lxp_kernel_canonical_ledger_apply) != LXP_OK;
}

static void migration_execution(lxp_kernel *kernel, lxp_kernel_execution *execution,
                                 lxp_identity_store *identities,
                                 const lxp_authority_resolved *authority,
                                 const lxp_fee_params *fees, lxp_arena *arena,
                                 const uint8_t seed[32], uint32_t module_version)
{
    (void)memset(execution, 0, sizeof(*execution));
    execution->network_id = 1U;
    execution->batch_number = 1U;
    execution->batch_timestamp_ms = 10U;
    execution->maximum_timestamp_window = 100U;
    execution->epoch = kernel->epoch;
    execution->global_sequence = kernel->state->next_sequence;
    execution->recorded_module_version = module_version;
    execution->parameter_version = 1U;
    execution->signature_valid = true;
    execution->identities = identities;
    execution->authority = authority;
    execution->fee_parameters = fees;
    execution->fee_balance = (lxp_u128){0U, UINT64_C(1000000000)};
    execution->gas_limit = UINT64_C(1000000);
    execution->arena = arena;
    execution->sequencer_private_key = seed;
}

static int migration_activate(lxp_kernel *kernel, const uint8_t seed[32],
                                lxp_identity_store *identities,
                                lxp_identity *owner, lxp_identity *governor)
{
    static uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t proposal[81], intake[LXP_MAX_ACTIVITY_BYTES], signature[64], public_key[32];
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_receipt receipt, governance_proof;
    lxp_verified_receipt_index index;
    lxp_migration_profile profile = {0};
    lxp_fee_params fees = {0};
    lxp_arena arena;
    lxp_byte_span encoded;
    static const uint64_t limits[7] = {
        1000000U,16777216U,1048576U,1048576U,64U,1048576U,4096U};
    profile.version = 1U;
    profile.module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    profile.parameter_version = 1U;
    profile.activation_epoch = 1U;
    (void)memcpy(profile.limits, limits, sizeof(limits));
    fees.version = 1U;
    fees.multiplier_basis_points = 10000U;
    if (migration_runtime(kernel) != 0 ||
        lxp_kernel_register_module(kernel, lxp_governance_module_iface_for_handover(true)) != LXP_OK ||
        public_key_for(seed, public_key) != 0 ||
        lxp_programs_migration_proposal_encode(&profile, proposal) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_state_root(kernel, kernel->current_state_root) != LXP_OK ||
        migration_actor(kernel, &activity, governor, migration_governor_seed, signature,
            LXP_GOVERNANCE_MIGRATION_BUDGET, proposal, sizeof(proposal), 100000000U,
            0x71U, &grant, &authority) != 0) return 1;
    migration_execution(kernel, &execution, identities, &authority, &fees,
        &arena, seed, 1U);
    if (lxp_kernel_execute_activity(kernel, &activity, &execution, &receipt) != LXP_OK ||
        receipt.result_code != LXP_OK || receipt.effects.count != 1U ||
        receipt.effects.effects[0].kind != LXP_EFFECT_STATE ||
        receipt.effects.effects[0].body_length != sizeof(proposal) ||
        memcmp(receipt.effects.effects[0].body, proposal, sizeof(proposal)) != 0 ||
        lxp_receipt_encode(&receipt, true, &arena, &encoded) != LXP_OK ||
        encoded.length > sizeof(intake) - 85U) return 1;
    (void)memcpy(intake, proposal, sizeof(proposal));
    write_u32(intake + 81U, (uint32_t)encoded.length);
    (void)memcpy(intake + 85U, encoded.bytes, encoded.length);
    size_t intake_length = 85U + encoded.length;
    if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
        lxp_receipt_decode(intake + 85U, intake_length - 85U, true, &receipt) != LXP_OK ||
        lxp_verified_receipt_index_init(&index) != LXP_OK ||
        lxp_verified_receipt_index_add(&index, &receipt, public_key, &arena) != LXP_OK ||
        lxp_arena_reset(&arena, 0U) != LXP_OK ||
        migration_actor(kernel, &activity, owner, seed, signature,
            LX_PROGRAMS_FEE_GOVERNANCE, intake, intake_length, 100000000U,
            0x72U, &grant, &authority) != 0) return 1;
    migration_execution(kernel, &execution, identities, &authority, &fees,
        &arena, seed, LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION);
    execution.verified_receipts = &index;
    if (lxp_kernel_execute_activity(kernel, &activity, &execution, &receipt) != LXP_OK ||
        receipt.result_code != LXP_OK ||
        lxp_arena_reset(&arena, 0U) != LXP_OK)
        return 1;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_result module_result;
    const lxp_module_registration *registration;
    if (lxp_receipt_decode(intake + 85U, intake_length - 85U, true, &governance_proof) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 10U, 0U,
            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK ||
        lxp_programs_migration_profile_epoch_begin(&ctx, 0U) != LXP_OK ||
        ctx.staged_count != 0U ||
        lxp_programs_migration_profile_stage(&ctx, &profile,
            &governance_proof) != LXP_ERR_SEQUENCE_REUSED) return 1;
    lxp_module_ctx_rollback(&ctx);
    if (lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 10U, 2U,
            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK ||
        lxp_programs_migration_profile_activate(&ctx, 2U) != LXP_FATAL_REPLAY_DIVERGENCE ||
        ctx.staged_count != 0U) return 1;
    lxp_module_ctx_rollback(&ctx);
    if (migration_actor(kernel, &activity, governor, migration_governor_seed, signature,
            LXP_GOVERNANCE_MIGRATION_BUDGET, proposal, sizeof(proposal), 100000000U,
            0x73U, &grant, &authority) != 0 ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U, &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_GOVERNANCE, 10U, kernel->epoch,
            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK || lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT; ctx.identities = identities;
    if (lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
            &effects, &module_result) != LXP_OK || module_result != LXP_ERR_SEQUENCE_REUSED ||
        effects.count != 0U || ctx.staged_count != 0U) return 1;
    lxp_module_ctx_rollback(&ctx);
    if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
        lxp_kernel_epoch_transition(kernel, 1U, 10U, &arena) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 10U, 1U,
            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK ||
        lxp_programs_migration_profile_activate(&ctx, 1U) != LXP_ERR_UNKNOWN_FIELD ||
        lxp_programs_migration_profile_epoch_begin(&ctx, 1U) != LXP_OK ||
        ctx.staged_count != 0U) return 1;
    lxp_module_ctx_rollback(&ctx);
    return 0;
}

static int migration_deploy(lxp_kernel *kernel, const lxp_activity *activity,
                             const lxp_authority_resolved *authority)
{
    uint8_t arena_bytes[16384];
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    const lxp_module_registration *registration;
    lxp_result result = LXP_OK;
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity->activity_type, 0U, &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 1U, 0U,
                            1U, 1000000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK) return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    ctx.batch_number = 1U;
    if (lxp_kernel_dispatch(registration, &ctx, activity, authority, &effects, &result) != LXP_OK ||
        result != LXP_OK || effects.count != 1U || effects.effects[0].event_type != 1U) {
        lxp_module_ctx_rollback(&ctx); return 1;
    }
    return lxp_module_ctx_commit(&ctx) != LXP_OK;
}

static int migration_snapshot(lxp_kernel *kernel, lxp_kernel *restored,
                               lxp_state_store *store, lxp_state_journal *journal,
                               const char *name)
{
    static uint8_t storage[ADMISSION_BUFFER_BYTES];
    lxp_arena arena;
    lxp_byte_span encoded;
    lxp_snapshot_manifest_record manifest;
    uint8_t root[32];
    static uint64_t parameters = 1U;
    uint64_t sequence = kernel->state->next_sequence - 1U;
    if (lxp_arena_init(&arena, storage, sizeof(storage)) != LXP_OK ||
        lxp_state_root(kernel, root) != LXP_OK ||
        lxp_snapshot_write(kernel, sequence, &arena, &encoded) != LXP_OK ||
        migration_write(name, encoded.bytes, encoded.length) != 0 ||
        lxp_snapshot_manifest_build(encoded.bytes, encoded.length, sequence, root,
            kernel->current_state_root, &manifest) != LXP_OK ||
        lxp_state_store_init(store, 1U) != LXP_OK ||
        lxp_kernel_create(restored, store, journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(restored, programs_module_registration_v4()) != LXP_OK ||
        migration_runtime(restored) != 0 ||
        lxp_handover_kernel_initialize(restored, &migration_genesis, NULL, NULL) != LXP_OK ||
        lxp_snapshot_load(encoded.bytes, encoded.length, &manifest, restored) != LXP_OK ||
        lxp_snapshot_verify_root(restored, &manifest) != LXP_OK) return 1;
    return 0;
}

static int migration_attempt(lxp_kernel *kernel, lxp_identity_store *identities,
                              lxp_identity *identity,
                              const uint8_t seed[32], uint8_t *payload,
                              size_t length, uint16_t abi, const char *name,
                              const char *phase, lxp_result expected, FILE *results,
                              uint8_t result_root[32])
{
    uint8_t arena_bytes[16384], signature[64];
    static uint8_t receipt_bytes[2U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t before[32], activity_id[32], public_key[32], zero[32] = {0};
    char before_hex[65], after_hex[65], hash_hex[65], filename[128], row[2048];
    lxp_arena arena, receipt_arena;
    lxp_activity activity;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_receipt receipt = {0}, decoded_receipt;
    lxp_byte_span encoded;
    const lxp_module_registration *registration;
    const lxp_module_kv_entry *record;
    lxp_result result = LXP_OK, status;
    lxp_kernel_execution execution;
    lxp_fee_params fees = {0};
    size_t blobs_before = kernel->blob_count, staged;
    lx_programs_metering_schedule recorded_schedule;
    int success = expected == LXP_OK;
    fees.version = 1U;
    fees.base_fee = (lxp_u128){0U, 1U};
    fees.multiplier_basis_points = 10000U;
    if (migration_runtime(kernel) != 0 ||
        lxp_state_root(kernel, before) != LXP_OK ||
        lxp_programs_metering_schedule_current(kernel, 1U, &recorded_schedule) != LXP_OK ||
        migration_activity(kernel, &activity, identity, seed, signature, payload,
                           length, 2U, &grant, &authority) != 0 ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U, &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 10U, kernel->epoch,
                            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK) return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    ctx.batch_number = 1U;
    migration_execution(kernel, &execution, identities, &authority, &fees,
        &arena, seed, LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION);
    if (lxp_kernel_bind_migration_admission(&ctx, &activity, &execution) != LXP_OK)
        return 1;
    if (strcmp(name, "incompatible_schedule") == 0 || strcmp(name, "unknown_schedule") == 0) {
        void *decoded = NULL;
        lx_programs_metering_schedule schedule;
        status = lxp_programs_lifecycle_decode(&ctx, 2U, payload, length, &decoded);
        if (status != LXP_OK || lxp_programs_metering_schedule_current(kernel, 1U, &schedule) != LXP_OK) return 1;
        if (strcmp(name, "incompatible_schedule") == 0) schedule.coefficients[0] = 3U;
        else schedule.version = 2U;
        result = layerx_programs_migration_execute_activity((uint64_t)(uintptr_t)decoded,
            (uint32_t)(length - 113U), 7U, abi, schedule.version,
            schedule.coefficients[0], schedule.coefficients[1], schedule.coefficients[2],
            schedule.coefficients[3], schedule.coefficients[4], schedule.coefficients[5],
            schedule.coefficients[6], schedule.coefficients[7], schedule.coefficients[8],
            migration_word(payload + 68U), migration_word(payload + 76U),
            migration_word(payload + 84U), migration_word(payload + 92U));
    } else status = lxp_kernel_dispatch(registration, &ctx, &activity, &authority, &effects, &result);
    staged = ctx.staged_count + ctx.staged_blob_count + ctx.staged_account_count;
    if (status != LXP_OK || result != expected ||
        (!success && (staged != 0U || effects.count != 0U)) ||
        (success && (effects.count != 1U || effects.effects[0].kind != LXP_EFFECT_EVENT ||
                     effects.effects[0].event_type != 2U))) {
        (void)fprintf(stderr, "Migration %s/%s ABI%u status=%d result=%d expected=%d\n",
            name, phase, (unsigned)abi, (int)status, (int)result, (int)expected);
        lxp_module_ctx_rollback(&ctx); return 1;
    }
    if (success) { if (lxp_module_ctx_commit(&ctx) != LXP_OK) return 1; }
    else lxp_module_ctx_rollback(&ctx);
    record = migration_record(kernel, payload);
    if (record == NULL || record->value_length != 71U ||
        lxp_state_root(kernel, result_root) != LXP_OK ||
        (!success && (memcmp(before, result_root, 32U) != 0 || kernel->blob_count != blobs_before)) ||
        (success && (memcmp(record->value + 33U, payload + 68U, 32U) != 0 ||
                     kernel->blob_count != blobs_before + 1U))) return 1;
    {
        uint32_t version = ((uint32_t)record->value[67] << 24U) |
            ((uint32_t)record->value[68] << 16U) | ((uint32_t)record->value[69] << 8U) | record->value[70];
        uint16_t recorded_abi = (uint16_t)(((uint16_t)record->value[65] << 8U) | record->value[66]);
        uint32_t wanted_version = success || strcmp(phase, "restart") == 0 ? 2U : 1U;
        uint16_t wanted_abi = strcmp(name, "downgrade") == 0 && abi == 1U ? 2U : abi;
        if (version != wanted_version || recorded_abi != wanted_abi) return 1;
        if (lxp_arena_init(&receipt_arena, receipt_bytes, sizeof(receipt_bytes)) != LXP_OK ||
            lxp_activity_encode(&activity, &receipt_arena, &encoded) != LXP_OK ||
            lxp_activity_id(encoded.bytes, encoded.length, activity_id) != LXP_OK ||
            lxp_arena_reset(&receipt_arena, 0U) != LXP_OK ||
            lxp_receipt_build(&receipt, activity_id, 1U, before, result_root, zero, result,
                &effects, (lxp_u128){0U,0U}, zero, LXP_MODULE_PROGRAMS,
                LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION, 1U) != LXP_OK ||
            lxp_receipt_sign(&receipt, seed, &receipt_arena) != LXP_OK ||
            lxp_arena_reset(&receipt_arena, 0U) != LXP_OK ||
            lxp_receipt_encode(&receipt, true, &receipt_arena, &encoded) != LXP_OK ||
            lxp_receipt_decode(encoded.bytes, encoded.length, true, &decoded_receipt) != LXP_OK ||
            decoded_receipt.result_code != result || decoded_receipt.effects.count != effects.count ||
            memcmp(decoded_receipt.resulting_state_root, result_root, 32U) != 0 ||
            public_key_for(seed, public_key) != 0) return 1;
        (void)snprintf(filename, sizeof(filename), "receipt.abi%u.%s.%s.bin", (unsigned)abi, phase, name);
        if (migration_write(filename, encoded.bytes, encoded.length) != 0 ||
            lxp_arena_reset(&receipt_arena, 0U) != LXP_OK ||
            lxp_receipt_verify(&decoded_receipt, public_key, &receipt_arena) != LXP_OK) return 1;
        migration_hex(before, before_hex); migration_hex(result_root, after_hex);
        migration_hex(record->value + 33U, hash_hex);
        static const uint64_t expected_coefficients[9] = {1U,1U,1U,1U,1U,8U,8U,64U,8U};
        if (recorded_schedule.version != 1U ||
            memcmp(recorded_schedule.coefficients, expected_coefficients,
                   sizeof(expected_coefficients)) != 0) return 1;
        int count = snprintf(row, sizeof(row),
            "{\"name\":\"%s\",\"phase\":\"%s\",\"abi\":%u,\"result\":%d,\"expected\":%d,\"dispatch_status\":%d,\"recorded_abi\":%u,\"version\":%u,\"effects\":%zu,\"staged\":%zu,\"state_unchanged\":%s,\"artifact_count\":%zu,\"root_before\":\"%s\",\"root_after\":\"%s\",\"code_hash\":\"%s\",\"schedule_version\":1,\"schedule_coefficients\":[1,1,1,1,1,8,8,64,8]}",
            name, phase, (unsigned)abi, (int)result, (int)expected, (int)status,
            (unsigned)recorded_abi, (unsigned)version, effects.count, staged,
            memcmp(before, result_root, 32U) == 0 ? "true" : "false", kernel->blob_count,
            before_hex, after_hex, hash_hex);
        if (count < 0 || (size_t)count >= sizeof(row) ||
            fprintf(results, "%s\n", row) < 0 || printf("NATIVE_MIGRATION_CASE %s\n", row) < 0) return 1;
    }
    return 0;
}

static void migration_destroy(lxp_kernel *kernel, lxp_state_store *store)
{
    lx_programs_transfer_runtime *runtime = kernel->module_runtime[LXP_MODULE_PROGRAMS];
    while (kernel->blob_count != 0U) free(kernel->blobs[--kernel->blob_count].bytes);
    if (runtime != NULL) {
        free((void *)runtime->assets);
        lx_account_registry_release(runtime->accounts);
        free(runtime->accounts);
        free(runtime);
    }
    (void)lxp_state_store_destroy(store);
}

static lx_account *migration_account(lxp_kernel *kernel, const char *name)
{
    uint8_t id[32];
    size_t slot;
    lx_programs_transfer_runtime *runtime = kernel->module_runtime[LXP_MODULE_PROGRAMS];
    if (runtime == NULL || lx_account_id_from_string((const uint8_t *)name,
            strlen(name), id) != LXP_OK ||
        lx_account_registry_index_lookup(runtime->accounts, id, &slot) != LXP_OK)
        return NULL;
    return &runtime->accounts->accounts[slot];
}

static int migration_accounting_record(lxp_kernel *kernel,
    const uint8_t activity_id[32], uint8_t record[613])
{
    uint8_t storage[4096];
    lxp_arena arena;
    lxp_module_ctx ctx;
    if (lxp_arena_init(&arena, storage, sizeof(storage)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 10U, kernel->epoch,
            kernel->state->next_sequence, 1000000U, &arena, false) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    return lxp_programs_migration_accounting_read(&ctx, activity_id, record);
}

static int migration_accounting_row(FILE *results, const char *name, uint16_t abi,
    lxp_result status, lxp_result result, lxp_result expected, lxp_u128 fee,
    bool ledger_unchanged, bool program_unchanged, bool accounting_present,
    const uint8_t before[32], const uint8_t after[32])
{
    char before_hex[65], after_hex[65], row[1024];
    migration_hex(before, before_hex); migration_hex(after, after_hex);
    int count = snprintf(row, sizeof(row),
        "{\"name\":\"%s\",\"abi\":%u,\"status\":%d,\"result\":%d,\"expected\":%d,\"fee_hi\":%llu,\"fee_lo\":%llu,\"ledger_unchanged\":%s,\"program_unchanged\":%s,\"accounting_present\":%s,\"root_before\":\"%s\",\"root_after\":\"%s\"}",
        name, (unsigned)abi, (int)status, (int)result, (int)expected,
        (unsigned long long)fee.hi, (unsigned long long)fee.lo,
        ledger_unchanged ? "true" : "false", program_unchanged ? "true" : "false",
        accounting_present ? "true" : "false", before_hex, after_hex);
    return count < 0 || (size_t)count >= sizeof(row) ||
        fprintf(results, "%s\n", row) < 0 ||
        printf("%s %s\n", strcmp(name, "authenticated_legacy_replay") == 0 ?
            "NATIVE_MIGRATION_LEGACY_CASE" : "NATIVE_MIGRATION_ACCOUNTING_CASE", row) < 0;
}

static int migration_accounting(const uint8_t seed[32])
{
    static const char *names[] = {"combined_fee_once", "trap_zero_fee",
        "exhaustion_zero_fee", "signed_limit", "available_funds", "frozen_payer",
        "wrong_asset", "checked_overflow", "missing_profile"};
    static const lxp_result expected[] = {LXP_OK, LXP_ERR_NON_CANONICAL,
        LXP_ERR_GAS_EXHAUSTED, LXP_ERR_FEE_UNPAYABLE, LXP_ERR_FEE_UNPAYABLE,
        LXP_ERR_FEE_UNPAYABLE, LXP_ERR_ASSET_MISMATCH, LXP_ERR_OVERFLOW,
        LXP_ERR_VERSION_UNSUPPORTED};
    static lxp_kernel kernel, restored, restarted;
    static uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t old_wasm[512], wasm[512], deploy[1024], upgrade[1024], signature[64];
    uint8_t public_key[32], before[32], after[32], record[613], replay_record[613];
    lxp_state_store store, restored_store, restarted_store;
    lxp_state_journal journal, restored_journal, restarted_journal;
    lxp_identity_store identities, replay_identities;
    lxp_identity *owner, *other, *governor;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_activity activity;
    lxp_kernel_execution execution;
    lxp_receipt receipt, replay_receipt;
    lxp_fee_params fees;
    lxp_arena arena;
    lxp_byte_span encoded;
    uint64_t parameters = 1U;
    unsigned int cases = 0U;
    int output_fd = openat(migration_directory_fd, "accounting.jsonl",
        O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    FILE *results = output_fd < 0 ? NULL : fdopen(output_fd, "w");
    if (results == NULL) return 1;
    for (uint16_t abi = 1U; abi <= 4U; ++abi) {
        for (size_t index = 0U; index < sizeof(names) / sizeof(names[0]); ++index) {
            char filename[128];
            (void)memset(&identities, 0, sizeof(identities));
            (void)memset(&fees, 0, sizeof(fees));
            migration_funding = index == 7U ? (lxp_u128){UINT64_MAX, UINT64_MAX} :
                (lxp_u128){0U, UINT64_C(1000000000)};
            if (public_key_for(seed, public_key) != 0 ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-owner", 30U,
                    public_key, &owner) != LXP_OK ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-other", 30U,
                    public_key, &other) != LXP_OK ||
                public_key_for(migration_governor_seed, public_key) != 0 ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-governor", 33U,
                    public_key, &governor) != LXP_OK ||
                lxp_state_store_init(&store, 1U) != LXP_OK ||
                lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
                project_metering_genesis_with(&kernel, seed, 1) != 0 ||
                lxp_kernel_register_module(&kernel, programs_module_registration_v4()) != LXP_OK ||
                migration_runtime(&kernel) != 0) return 1;
            if (index != 8U && migration_activate(&kernel, seed, &identities, owner, governor) != 0)
                return 1;
            size_t old_length = migration_module(old_wasm, abi, 4U);
            size_t wasm_length = migration_module(wasm, abi, index == 1U ? 1U : index == 2U ? 2U : 0U);
            (void)memset(deploy, 0, sizeof(deploy));
            (void)memset(upgrade, 0, sizeof(upgrade));
            deploy[0] = (uint8_t)(0xa0U + abi); deploy[31] = (uint8_t)(index + 1U);
            write_u16(deploy + 32U, abi); deploy[34] = 1U;
            (void)memcpy(deploy + 36U, owner->did_id, 32U);
            if (lxp_hash_sha256(old_wasm, old_length, deploy + 68U) != LXP_OK) return 1;
            write_u32(deploy + 100U, (uint32_t)old_length);
            (void)memcpy(deploy + 104U, old_wasm, old_length);
            if (migration_activity(&kernel, &activity, owner, seed, signature, deploy,
                    104U + old_length, 1U, &grant, &authority) != 0 ||
                migration_deploy(&kernel, &activity, &authority) != 0) return 1;
            (void)memcpy(upgrade, deploy, 32U); write_u16(upgrade + 32U, abi);
            upgrade[34] = 1U; (void)memcpy(upgrade + 36U, deploy + 68U, 32U);
            if (lxp_hash_sha256(wasm, wasm_length, upgrade + 68U) != LXP_OK) return 1;
            write_u16(upgrade + 100U, 7U); write_u32(upgrade + 102U, (uint32_t)wasm_length);
            (void)memcpy(upgrade + 106U, "migrate", 7U);
            (void)memcpy(upgrade + 113U, wasm, wasm_length);
            lx_account *payer = migration_account(&kernel, "agent:did:lxp:native-migration-owner:main");
            lx_account *treasury = migration_account(&kernel, "system:fees");
            if (payer == NULL || treasury == NULL) return 1;
            if (index == 4U) payer->balance = (lxp_u128){0U, 100U};
            if (index == 5U) payer->frozen = true;
            if (index == 6U) {
                lx_programs_transfer_runtime *runtime = kernel.module_runtime[LXP_MODULE_PROGRAMS];
                runtime->occupancy_asset_id[0] = 2U;
            }
            lx_account payer_before = *payer, treasury_before = *treasury;
            size_t blob_count = kernel.blob_count;
            fees.version = 1U; fees.multiplier_basis_points = 10000U;
            fees.base_fee = index == 7U ? (lxp_u128){UINT64_MAX, UINT64_MAX} : (lxp_u128){0U, 1U};
            if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
                migration_actor(&kernel, &activity, owner, seed, signature,
                    LX_PROGRAMS_UPGRADE, upgrade, 113U + wasm_length,
                    index == 3U ? 100U : 100000000U, (uint8_t)(0x80U + index),
                    &grant, &authority) != 0) return 1;
            if (index == 7U) {
                uint8_t digest[32];
                activity.fee_limit = (lxp_u128){UINT64_MAX, UINT64_MAX};
                if (lxp_activity_signing_preimage(&activity, digest) != LXP_OK ||
                    sign_raw(seed, digest, sizeof(digest), signature) != 0) return 1;
            }
            migration_execution(&kernel, &execution, &identities, &authority, &fees,
                &arena, seed, LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION);
            execution.fee_balance = payer->balance;
            execution.recorded_fee_schedule_version = 1U;
            execution.recorded_metering_schedule_version = 1U;
            if (lxp_state_root(&kernel, before) != LXP_OK) return 1;
            (void)memcpy(kernel.current_state_root, before, 32U);
            if (index == 0U) {
                replay_identities = identities;
                (void)snprintf(filename, sizeof(filename), "accounting.abi%u.prestate.snapshot.bin", (unsigned)abi);
                if (migration_snapshot(&kernel, &restored, &restored_store,
                        &restored_journal, filename) != 0) return 1;
            }
            (void)memset(&receipt, 0, sizeof(receipt));
            lxp_result status = lxp_kernel_execute_activity(&kernel, &activity, &execution, &receipt);
            lxp_result result = status == LXP_OK ? receipt.result_code : status;
            lxp_u128 charged = status == LXP_OK ? receipt.fee_charged : (lxp_u128){0U, 0U};
            int found = migration_accounting_record(&kernel, receipt.activity_id, record);
            const lxp_module_kv_entry *program = migration_record(&kernel, deploy);
            if (program == NULL || lxp_state_root(&kernel, after) != LXP_OK || result != expected[index]) return 1;
            bool ledger_unchanged = lxp_u128_cmp(payer->balance, payer_before.balance) == 0 &&
                lxp_u128_cmp(treasury->balance, treasury_before.balance) == 0 &&
                payer->next_sequence == payer_before.next_sequence &&
                treasury->next_sequence == treasury_before.next_sequence;
            bool program_unchanged = memcmp(program->value + 33U, deploy + 68U, 32U) == 0 &&
                kernel.blob_count == blob_count;
            if (index == 0U) {
                lxp_u128 debit, credit, runtime_fee = {0U, 0U}, combined;
                static const uint64_t limits[7] = {1000000U,16777216U,1048576U,1048576U,64U,1048576U,4096U};
                static const uint64_t prices[7] = {1U,1U,2U,4U,1U,1U,100U};
                static const uint64_t coefficients[9] = {1U,1U,1U,1U,1U,8U,8U,64U,8U};
                if (status != LXP_OK || found != LXP_OK || memcmp(record, "LXMA1", 5U) != 0 ||
                    migration_u32(record + 5U) != 1U || record[9] != 0U || record[10] != 1U ||
                    record[11] != 0U || record[12] != abi ||
                    migration_u32(record + 13U) != LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION ||
                    migration_u32(record + 17U) != 1U || migration_u32(record + 21U) != 1U ||
                    migration_u32(record + 25U) != 1U ||
                    memcmp(record + 61U, receipt.activity_id, 32U) != 0 ||
                    memcmp(record + 93U, upgrade, 32U) != 0 ||
                    memcmp(record + 125U, upgrade + 36U, 32U) != 0 ||
                    memcmp(record + 157U, upgrade + 68U, 32U) != 0 ||
                    memcmp(record + 189U, payer->id, 32U) != 0 ||
                    memcmp(record + 221U, payer->asset_id, 32U) != 0 ||
                    lxp_ct_is_zero(record + 253U, 32U) ||
                    migration_word(record + 29U) != 1U ||
                    migration_word(record + 37U) != kernel.epoch ||
                    migration_word(record + 45U) != execution.batch_number ||
                    migration_word(record + 53U) != execution.global_sequence ||
                    migration_word(record + 517U) != 0U || migration_word(record + 525U) != 100000000U ||
                    migration_word(record + 533U) != payer_before.balance.hi ||
                    migration_word(record + 541U) != payer_before.balance.lo ||
                    migration_word(record + 565U) != 0U || migration_word(record + 573U) != 1U ||
                    lxp_u128_sub(payer_before.balance, payer->balance, &debit) != LXP_OK ||
                    lxp_u128_sub(treasury->balance, treasury_before.balance, &credit) != LXP_OK ||
                    lxp_u128_cmp(debit, charged) != 0 || lxp_u128_cmp(credit, charged) != 0 ||
                    payer->next_sequence != payer_before.next_sequence + 1U || program_unchanged)
                    return 1;
                for (size_t i = 0U; i < 7U; ++i)
                    if (migration_word(record + 285U + i * 8U) != limits[i] ||
                        migration_word(record + 341U + i * 8U) != prices[i]) return 1;
                for (size_t i = 0U; i < 9U; ++i)
                    if (migration_word(record + 397U + i * 8U) != coefficients[i]) return 1;
                for (size_t i = 0U; i < 6U; ++i) {
                    lxp_u256 product;
                    if (migration_word(record + 469U + i * 8U) > limits[i] ||
                        lxp_u128_mul((lxp_u128){0U, migration_word(record + 469U + i * 8U)},
                            (lxp_u128){0U, prices[i]}, &product) != LXP_OK ||
                        product.words[2] != 0U || product.words[3] != 0U ||
                        lxp_u128_add(runtime_fee, (lxp_u128){product.words[1], product.words[0]},
                            &runtime_fee) != LXP_OK) return 1;
                }
                if (lxp_u128_is_zero(runtime_fee) ||
                    lxp_u128_add(runtime_fee, (lxp_u128){0U,1U}, &combined) != LXP_OK ||
                    lxp_u128_cmp(combined, charged) != 0 ||
                    migration_word(record + 581U) != runtime_fee.hi ||
                    migration_word(record + 589U) != runtime_fee.lo ||
                    migration_word(record + 597U) != combined.hi ||
                    migration_word(record + 605U) != combined.lo ||
                    lxp_receipt_verify(&receipt, migration_genesis.signer_public_key, &arena) != LXP_OK ||
                    lxp_receipt_encode(&receipt, true, &arena, &encoded) != LXP_OK) return 1;
                (void)snprintf(filename, sizeof(filename), "accounting.abi%u.receipt.bin", (unsigned)abi);
                if (migration_write(filename, encoded.bytes, encoded.length) != 0) return 1;
                uint8_t *saved = malloc(encoded.length);
                if (saved == NULL) return 1;
                (void)memcpy(saved, encoded.bytes, encoded.length);
                if (lxp_receipt_decode(saved, encoded.length, true, &replay_receipt) != LXP_OK) return 1;
                (void)snprintf(filename, sizeof(filename), "accounting.abi%u.record.bin", (unsigned)abi);
                if (migration_write(filename, record, sizeof(record)) != 0 ||
                    migration_accounting_row(results, names[index], abi, status, result,
                        expected[index], charged, false, false, true, before, after) != 0)
                    return 1;
                ++cases;
                if (lxp_arena_reset(&arena, 0U) != LXP_OK) return 1;
                execution.identities = &replay_identities;
                execution.sequencer_private_key = NULL;
                execution.replay_receipt = &replay_receipt;
                execution.replay_public_key = migration_genesis.signer_public_key;
                lx_account rollback_payer = *migration_account(&restored, "agent:did:lxp:native-migration-owner:main");
                lx_account rollback_treasury = *migration_account(&restored, "system:fees");
                uint8_t rollback_root[32];
                if (lxp_state_root(&restored, rollback_root) != LXP_OK) return 1;
                lxp_receipt divergent = replay_receipt;
                divergent.resulting_state_root[0] ^= 1U;
                if (lxp_receipt_sign(&divergent, seed, &arena) != LXP_OK ||
                    lxp_arena_reset(&arena, 0U) != LXP_OK) return 1;
                execution.replay_receipt = &divergent;
                status = lxp_kernel_execute_activity(&restored, &activity, &execution, &receipt);
                lx_account *rollback_payer_after = migration_account(&restored, "agent:did:lxp:native-migration-owner:main");
                lx_account *rollback_treasury_after = migration_account(&restored, "system:fees");
                const lxp_module_kv_entry *rollback_program = migration_record(&restored, deploy);
                if (status != LXP_FATAL_REPLAY_DIVERGENCE ||
                    rollback_payer_after == NULL || rollback_treasury_after == NULL || rollback_program == NULL ||
                    lxp_u128_cmp(rollback_payer_after->balance, rollback_payer.balance) != 0 ||
                    lxp_u128_cmp(rollback_treasury_after->balance, rollback_treasury.balance) != 0 ||
                    rollback_payer_after->next_sequence != rollback_payer.next_sequence ||
                    rollback_treasury_after->next_sequence != rollback_treasury.next_sequence ||
                    memcmp(rollback_program->value + 33U, deploy + 68U, 32U) != 0 ||
                    migration_accounting_record(&restored, replay_receipt.activity_id, replay_record) == LXP_OK ||
                    lxp_state_root(&restored, before) != LXP_OK || memcmp(before, rollback_root, 32U) != 0 ||
                    migration_accounting_row(results, "downstream_rollback", abi, status, status,
                        LXP_FATAL_REPLAY_DIVERGENCE, (lxp_u128){0U,0U}, true, true, false,
                        rollback_root, before) != 0 || lxp_arena_reset(&arena, 0U) != LXP_OK) return 1;
                ++cases;
                execution.replay_receipt = &replay_receipt;
                status = lxp_kernel_execute_activity(&restored, &activity, &execution, &receipt);
                if (status != LXP_OK || receipt.result_code != LXP_OK ||
                    lxp_u128_cmp(receipt.fee_charged, charged) != 0 ||
                    migration_accounting_record(&restored, receipt.activity_id, replay_record) != LXP_OK ||
                    memcmp(record, replay_record, sizeof(record)) != 0 ||
                    lxp_state_root(&restored, before) != LXP_OK || memcmp(before, after, 32U) != 0 ||
                    migration_accounting_row(results, "recorded_profile_replay", abi, status,
                        receipt.result_code, LXP_OK, charged, false, false, true,
                        replay_receipt.previous_state_root, before) != 0) return 1;
                ++cases;
                (void)snprintf(filename, sizeof(filename), "accounting.abi%u.committed.snapshot.bin", (unsigned)abi);
                if (migration_snapshot(&kernel, &restarted, &restarted_store,
                        &restarted_journal, filename) != 0 ||
                    migration_accounting_record(&restarted, replay_receipt.activity_id, replay_record) != LXP_OK ||
                    memcmp(record, replay_record, sizeof(record)) != 0) return 1;
                lx_account *loaded_payer = migration_account(&restarted, "agent:did:lxp:native-migration-owner:main");
                lx_account *loaded_treasury = migration_account(&restarted, "system:fees");
                if (loaded_payer == NULL || loaded_treasury == NULL ||
                    lxp_u128_cmp(loaded_payer->balance, payer->balance) != 0 ||
                    lxp_u128_cmp(loaded_treasury->balance, treasury->balance) != 0 ||
                    loaded_payer->next_sequence != payer->next_sequence ||
                    migration_accounting_row(results, "restart_record_and_ledger", abi, LXP_OK,
                        LXP_OK, LXP_OK, (lxp_u128){0U,0U}, true, true, true, after, after) != 0)
                    return 1;
                ++cases;
                execution.identities = &identities;
                execution.global_sequence = restarted.state->next_sequence;
                execution.sequencer_private_key = seed;
                execution.replay_receipt = NULL; execution.replay_public_key = NULL;
                if (lxp_arena_reset(&arena, 0U) != LXP_OK) return 1;
                status = lxp_kernel_execute_activity(&restarted, &activity, &execution, &receipt);
                if (status != LXP_ERR_IDEMPOTENT_REPLAY ||
                    lxp_u128_cmp(loaded_payer->balance, payer->balance) != 0 ||
                    lxp_u128_cmp(loaded_treasury->balance, treasury->balance) != 0 ||
                    migration_accounting_record(&restarted, replay_receipt.activity_id, replay_record) != LXP_OK ||
                    memcmp(record, replay_record, sizeof(record)) != 0 ||
                    migration_accounting_row(results, "duplicate_no_second_fee", abi,
                        status, status, LXP_ERR_IDEMPOTENT_REPLAY, (lxp_u128){0U,0U},
                        true, true, true, after, after) != 0) return 1;
                ++cases;
                lxp_programs_occupancy_receipt occupancy;
                if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
                    lxp_programs_finalize_occupancy_batch_selected(&restarted,
                        LXP_PROTOCOL_VERSION_STATE_COMMITMENT, 1U, 1U, 10U,
                        restarted.state->next_sequence, 1U, &arena, &occupancy, &encoded) != LXP_OK ||
                    !lxp_u128_is_zero(occupancy.byte_batches) ||
                    !lxp_u128_is_zero(occupancy.fee_units)) return 1;
                (void)snprintf(filename, sizeof(filename), "accounting.abi%u.occupancy.bin", (unsigned)abi);
                if (migration_write(filename, encoded.bytes, encoded.length) != 0 ||
                    migration_accounting_record(&restarted, replay_receipt.activity_id, replay_record) != LXP_OK ||
                    memcmp(record, replay_record, sizeof(record)) != 0) return 1;
                lxp_module_ctx pricing_ctx;
                lx_programs_fee_schedule current_prices, old_prices;
                uint8_t current_asset[32], old_asset[32];
                if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
                    lxp_module_ctx_init(&pricing_ctx, &restarted, LXP_MODULE_PROGRAMS,
                        10U, restarted.epoch, restarted.state->next_sequence,
                        1000000U, &arena, false) != LXP_OK ||
                    lxp_programs_fee_schedule_current(&pricing_ctx, &current_prices, current_asset) != LXP_OK ||
                    lxp_programs_fee_schedule_at(&pricing_ctx, 1U, &old_prices, old_asset) != LXP_OK ||
                    current_prices.version != 2U || old_prices.version != 1U ||
                    current_prices.occupancy_byte_batch != 90U || old_prices.occupancy_byte_batch != 100U ||
                    memcmp(current_asset, old_asset, 32U) != 0 ||
                    lxp_u128_cmp(loaded_payer->balance, payer->balance) != 0 ||
                    lxp_u128_cmp(loaded_treasury->balance, treasury->balance) != 0 ||
                    lxp_state_root(&restarted, before) != LXP_OK ||
                    migration_accounting_row(results, "historical_record_not_repriced", abi,
                        LXP_OK, LXP_OK, LXP_OK, (lxp_u128){0U,0U}, true, true, true,
                        before, before) != 0) return 1;
                lxp_module_ctx_rollback(&pricing_ctx);
                ++cases;
                free(saved);
                migration_destroy(&restored, &restored_store);
                migration_destroy(&restarted, &restarted_store);
            } else {
                if (!ledger_unchanged || !program_unchanged || found == LXP_OK ||
                    !lxp_u128_is_zero(charged) ||
                    (status == LXP_OK && receipt.effects.count != 0U) ||
                    migration_accounting_row(results, names[index], abi, status, result,
                        expected[index], charged, ledger_unchanged, program_unchanged,
                        false, before, after) != 0) return 1;
                ++cases;
            }
            migration_destroy(&kernel, &store);
        }
    }
    migration_funding = (lxp_u128){0U, UINT64_C(1000000000)};
    if (fflush(results) != 0 || fsync(output_fd) != 0 || fclose(results) != 0) return 1;
    return printf("NATIVE_MIGRATION_ACCOUNTING_SUMMARY {\"cases\":%u,\"skipped\":0}\n", cases) < 0;
}

static uint32_t migration_u32(const uint8_t bytes[4])
{
    return ((uint32_t)bytes[0] << 24U) | ((uint32_t)bytes[1] << 16U) |
        ((uint32_t)bytes[2] << 8U) | bytes[3];
}

static int migration_legacy(void)
{
    const char *fd_text = getenv("PAXEER_X_NATIVE_MIGRATION_LEGACY_FIXTURE_FD");
    const char *source_revision = getenv("PAXEER_X_NATIVE_MIGRATION_LEGACY_SOURCE_REVISION");
    struct stat info;
    char *end;
    long input_fd;
    if (fd_text == NULL || source_revision == NULL ||
        strcmp(source_revision, "9e0e098bc429e6209a4e8a153001217fbcc84d6b") != 0)
        return 78;
    errno = 0; input_fd = strtol(fd_text, &end, 10);
    if (errno != 0 || *end != '\0' || input_fd < 0 || input_fd > 1048576L ||
        fstat((int)input_fd, &info) != 0 || !S_ISREG(info.st_mode) ||
        info.st_uid != geteuid() || (info.st_mode & 0777U) != 0600U ||
        info.st_nlink != 1 || info.st_size < 237 || info.st_size > 16777216)
        return 78;
    size_t length = (size_t)info.st_size;
    uint8_t *bytes = malloc(length);
    if (bytes == NULL || pread((int)input_fd, bytes, length, 0) != (ssize_t)length)
        return 78;
    if (memcmp(bytes, "LXLF1", 5U) != 0 || memcmp(bytes + 5U, source_revision, 40U) != 0 ||
        lxp_ct_is_zero(bytes + 45U, 32U) || lxp_ct_is_zero(bytes + 77U, 32U) ||
        lxp_ct_is_zero(bytes + 109U, 32U)) return 78;
    size_t sizes[4];
    size_t total = 237U;
    for (size_t i = 0U; i < 4U; ++i) {
        sizes[i] = migration_u32(bytes + 189U + i * 4U);
        if (sizes[i] == 0U || sizes[i] > length - total) return 78;
        total += sizes[i];
    }
    if (total != length) return 78;
    const uint8_t *genesis_bytes = bytes + 237U;
    const uint8_t *snapshot_bytes = genesis_bytes + sizes[0];
    const uint8_t *activity_bytes = snapshot_bytes + sizes[1];
    const uint8_t *receipt_bytes = activity_bytes + sizes[2];
    static lxp_genesis_manifest manifest;
    static lxp_kernel kernel;
    static uint8_t storage[8U * LXP_MAX_ACTIVITY_BYTES];
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_identity_store identities = {0};
    lxp_identity *identity;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_activity activity;
    lxp_receipt expected, actual;
    lxp_snapshot_manifest_record snapshot_manifest;
    lxp_kernel_execution execution;
    lxp_fee_params fees = {0};
    lxp_arena arena;
    uint64_t parameters = 1U;
    uint8_t root[32], record[613];
    if (lxp_arena_init(&arena, storage, sizeof(storage)) != LXP_OK ||
        lxp_genesis_parse(genesis_bytes, sizes[0], LXP_GENESIS_INPUT_MANIFEST, &manifest) != LXP_OK ||
        manifest.protocol_version != LXP_PROTOCOL_VERSION_STATE_COMMITMENT ||
        lxp_genesis_verify_signature(&manifest, &arena) != LXP_OK ||
        lxp_receipt_decode(receipt_bytes, sizes[3], true, &expected) != LXP_OK ||
        lxp_activity_decode(activity_bytes, sizes[2], &activity) != LXP_OK ||
        activity.activity_type != LX_PROGRAMS_UPGRADE || activity.payload.length < 113U ||
        activity.payload.bytes[32] != 0U || activity.payload.bytes[33] != 1U ||
        activity.payload.bytes[34] != 1U || activity.authority.length != 32U ||
        expected.result_code != LXP_OK || expected.fee_charged.hi != 0U || expected.fee_charged.lo != 1U ||
        lxp_activity_verify_signature(&activity) != LXP_OK ||
        lxp_receipt_verify(&expected, manifest.signer_public_key, &arena) != LXP_OK ||
        lxp_state_store_init(&store, 1U) != LXP_OK ||
        lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(&kernel, programs_module_registration_v4()) != LXP_OK ||
        migration_runtime(&kernel) != 0 ||
        lxp_handover_kernel_initialize(&kernel, &manifest, NULL, NULL) != LXP_OK ||
        expected.global_sequence == 0U ||
        lxp_snapshot_manifest_build(snapshot_bytes, sizes[1], expected.global_sequence - 1U,
            expected.previous_state_root, bytes + 205U, &snapshot_manifest) != LXP_OK ||
        lxp_snapshot_load(snapshot_bytes, sizes[1], &snapshot_manifest, &kernel) != LXP_OK ||
        lxp_snapshot_verify_root(&kernel, &snapshot_manifest) != LXP_OK ||
        lxp_identity_register(&identities, activity.actor_did.bytes, activity.actor_did.length,
            activity.authority.bytes, &identity) != LXP_OK) return 1;
    identity->next_sequence = activity.account_sequence;
    fees.version = 1U; fees.base_fee = (lxp_u128){0U, 1U}; fees.multiplier_basis_points = 10000U;
    if (lxp_authority_resolve_activity(&kernel, identity, &activity, true, true,
            migration_word(bytes + 165U), 100U, migration_word(bytes + 157U),
            &grant, &authority) != LXP_OK ||
        lxp_arena_reset(&arena, 0U) != LXP_OK) return 1;
    migration_execution(&kernel, &execution, &identities, &authority, &fees,
        &arena, NULL, migration_u32(bytes + 173U));
    execution.epoch = migration_word(bytes + 141U);
    execution.batch_number = migration_word(bytes + 149U);
    execution.global_sequence = migration_word(bytes + 157U);
    execution.batch_timestamp_ms = migration_word(bytes + 165U);
    execution.parameter_version = migration_u32(bytes + 177U);
    execution.recorded_fee_schedule_version = migration_u32(bytes + 181U);
    execution.recorded_metering_schedule_version = migration_u32(bytes + 185U);
    execution.replay_receipt = &expected; execution.replay_public_key = manifest.signer_public_key;
    (void)memcpy(execution.batch_id, expected.batch_id, 32U);
    if (execution.epoch != kernel.epoch || execution.global_sequence != kernel.state->next_sequence ||
        execution.recorded_module_version != expected.module_version ||
        execution.parameter_version != expected.parameter_version ||
        execution.batch_timestamp_ms != expected.timestamp) return 1;
    lxp_module_ctx ctx;
    bool preactivation = false;
    if (lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_PROGRAMS,
            execution.batch_timestamp_ms, execution.epoch, execution.global_sequence,
            execution.gas_limit, &arena, false) != LXP_OK ||
        lxp_programs_migration_preactivation(&ctx, &preactivation) != LXP_OK || !preactivation)
        return 1;
    lxp_result status = lxp_kernel_execute_activity(&kernel, &activity, &execution, &actual);
    if (status != LXP_OK || actual.result_code != LXP_OK ||
        lxp_u128_cmp(actual.fee_charged, expected.fee_charged) != 0 ||
        migration_accounting_record(&kernel, actual.activity_id, record) == LXP_OK ||
        lxp_state_root(&kernel, root) != LXP_OK ||
        memcmp(root, expected.resulting_state_root, 32U) != 0 ||
        migration_write("legacy.verified-input.bin", bytes, length) != 0) return 1;
    int result_fd = openat(migration_directory_fd, "legacy.jsonl",
        O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    FILE *results = result_fd < 0 ? NULL : fdopen(result_fd, "w");
    if (results == NULL || migration_accounting_row(results, "authenticated_legacy_replay", 1U,
            status, actual.result_code, LXP_OK, actual.fee_charged, false, false, false,
            expected.previous_state_root, root) != 0 || fflush(results) != 0 ||
        fsync(result_fd) != 0 || fclose(results) != 0) return 1;
    migration_destroy(&kernel, &store);
    free(bytes);
    return printf("NATIVE_MIGRATION_LEGACY_SUMMARY {\"cases\":1,\"skipped\":0}\n") < 0;
}

static int native_migration(void)
{
    static const char *names[] = {"valid_import", "unknown_abi", "downgrade", "wrong_code_hash",
        "wrong_prior_hash", "unauthorized_upgrader", "invalid_export", "trap", "resource_exhaustion",
        "incompatible_schedule", "unknown_schedule"};
    static const lxp_result expected[] = {LXP_OK, LXP_ERR_VERSION_UNSUPPORTED,
        LXP_ERR_VERSION_UNSUPPORTED, LXP_ERR_PAYLOAD_HASH_MISMATCH, LXP_ERR_CONTEXT_MISMATCH,
        LXP_ERR_AUTH_SCOPE, LXP_ERR_UNKNOWN_ACTIVITY, LXP_ERR_NON_CANONICAL,
        LXP_ERR_GAS_EXHAUSTED, LXP_ERR_NON_CANONICAL, LXP_ERR_NON_CANONICAL};
    static lxp_kernel kernel, restored, restarted;
    lxp_state_store store, restored_store, restarted_store;
    lxp_state_journal journal, restored_journal, restarted_journal;
    lxp_identity_store identities = {0};
    lxp_identity *owner, *other, *governor;
    lxp_authority_resolved authority;
    lxp_authority_grant grant;
    lxp_activity activity;
    uint8_t seed[32], public_key[32], signature[64], old_wasm[512], wasm[512];
    uint8_t deploy[1024], upgrade[1024], root[32], replay_root[32];
    uint64_t parameters = 1U;
    const char *fd_text = getenv("PAXEER_X_NATIVE_MIGRATION_AUTHORITY_FD");
    const char *governor_fd_text = getenv("PAXEER_X_NATIVE_MIGRATION_GOVERNOR_FD");
    const char *directory = getenv("PAXEER_X_NATIVE_MIGRATION_RUN");
    struct stat info;
    char *end;
    long key_fd;
    FILE *results;
    unsigned int cases = 0U;
    if (fd_text == NULL || governor_fd_text == NULL || directory == NULL) return 78;
    errno = 0; key_fd = strtol(fd_text, &end, 10);
    if (errno != 0 || *end != '\0' || key_fd < 0 || key_fd > 1048576L ||
        fstat((int)key_fd, &info) != 0 || !S_ISREG(info.st_mode) ||
        (info.st_mode & 0777U) != 0600U || info.st_uid != geteuid() || info.st_size != 32 ||
        pread((int)key_fd, seed, 32U, 0) != 32 || public_key_for(seed, public_key) != 0) return 78;
    errno = 0; key_fd = strtol(governor_fd_text, &end, 10);
    if (errno != 0 || *end != '\0' || key_fd < 0 || key_fd > 1048576L ||
        fstat((int)key_fd, &info) != 0 || !S_ISREG(info.st_mode) ||
        (info.st_mode & 0777U) != 0600U || info.st_uid != geteuid() || info.st_size != 32 ||
        pread((int)key_fd, migration_governor_seed, 32U, 0) != 32 ||
        memcmp(seed, migration_governor_seed, 32U) == 0) return 78;
    migration_directory_fd = open(directory, O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    if (migration_directory_fd < 0 || fstat(migration_directory_fd, &info) != 0 ||
        info.st_uid != geteuid() || (info.st_mode & 0777U) != 0700U) return 78;
    int results_fd = openat(migration_directory_fd, "results.jsonl", O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if (results_fd < 0 || (results = fdopen(results_fd, "w")) == NULL) return 1;
    for (uint16_t abi = 1U; abi <= 4U; ++abi) {
        for (size_t index = 0U; index < sizeof(names) / sizeof(names[0]); ++index) {
            (void)memset(&identities, 0, sizeof(identities));
            if (public_key_for(seed, public_key) != 0 ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-owner", 30U,
                    public_key, &owner) != LXP_OK ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-other", 30U,
                    public_key, &other) != LXP_OK ||
                public_key_for(migration_governor_seed, public_key) != 0 ||
                lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-governor", 33U,
                    public_key, &governor) != LXP_OK) return 1;
            uint16_t baseline_abi = index == 2U && abi == 1U ? 2U : abi;
            uint16_t requested_abi = index == 1U ? 5U : index == 2U ?
                (abi == 1U ? 1U : (uint16_t)(abi - 1U)) : abi;
            size_t old_length = migration_module(old_wasm, baseline_abi, 4U);
            size_t wasm_length = migration_module(wasm, abi, index == 7U ? 1U : index == 8U ? 2U : 0U);
            char snapshot_name[128];
            (void)memset(deploy, 0, sizeof(deploy)); (void)memset(upgrade, 0, sizeof(upgrade));
            deploy[0] = (uint8_t)(0x40U + abi); deploy[31] = (uint8_t)(index + 1U);
            write_u16(deploy + 32U, baseline_abi); deploy[34] = 1U;
            (void)memcpy(deploy + 36U, owner->did_id, 32U);
            if (lxp_hash_sha256(old_wasm, old_length, deploy + 68U) != LXP_OK) return 1;
            write_u32(deploy + 100U, (uint32_t)old_length);
            (void)memcpy(deploy + 104U, old_wasm, old_length);
            (void)memcpy(upgrade, deploy, 32U); write_u16(upgrade + 32U, requested_abi);
            upgrade[34] = 1U; (void)memcpy(upgrade + 36U, deploy + 68U, 32U);
            if (lxp_hash_sha256(wasm, wasm_length, upgrade + 68U) != LXP_OK) return 1;
            write_u16(upgrade + 100U, 7U); write_u32(upgrade + 102U, (uint32_t)wasm_length);
            (void)memcpy(upgrade + 106U, index == 6U ? "missing" : "migrate", 7U);
            (void)memcpy(upgrade + 113U, wasm, wasm_length);
            if (index == 3U) upgrade[68] ^= 1U;
            if (index == 4U) upgrade[36] ^= 1U;
            if (lxp_state_store_init(&store, 1U) != LXP_OK ||
                lxp_kernel_create(&kernel, &store, &journal, &parameters, 0U) != LXP_OK ||
                project_metering_genesis_with(&kernel, seed, 1) != 0 ||
                lxp_kernel_register_module(&kernel, programs_module_registration_v4()) != LXP_OK ||
                migration_activate(&kernel, seed, &identities, owner, governor) != 0 ||
                migration_activity(&kernel, &activity, owner, seed, signature, deploy,
                    104U + old_length, 1U, &grant, &authority) != 0 ||
                migration_deploy(&kernel, &activity, &authority) != 0) return 1;
            (void)snprintf(snapshot_name, sizeof(snapshot_name), "baseline.abi%u.%s.snapshot.bin", (unsigned)abi, names[index]);
            if (migration_snapshot(&kernel, &restored, &restored_store, &restored_journal, snapshot_name) != 0 ||
                migration_attempt(&kernel, &identities, index == 5U ? other : owner, seed, upgrade, 113U + wasm_length,
                    abi, names[index], "initial", expected[index], results, root) != 0 ||
                migration_attempt(&restored, &identities, index == 5U ? other : owner, seed, upgrade, 113U + wasm_length,
                    abi, names[index], "snapshot_replay", expected[index], results, replay_root) != 0 ||
                memcmp(root, replay_root, 32U) != 0) return 1;
            cases += 2U;
            if (index == 0U) {
                (void)snprintf(snapshot_name, sizeof(snapshot_name), "committed.abi%u.snapshot.bin", (unsigned)abi);
                if (migration_snapshot(&kernel, &restarted, &restarted_store, &restarted_journal, snapshot_name) != 0 ||
                    migration_attempt(&restarted, &identities, owner, seed, upgrade, 113U + wasm_length,
                        abi, "replay_upgrade", "restart", LXP_ERR_CONTEXT_MISMATCH, results, replay_root) != 0 ||
                    memcmp(root, replay_root, 32U) != 0) return 1;
                ++cases; migration_destroy(&restarted, &restarted_store);
            }
            migration_destroy(&kernel, &store); migration_destroy(&restored, &restored_store);
        }
    }
    if (migration_accounting(seed) != 0) return 1;
    int legacy_status = migration_legacy();
    if (legacy_status != 0) return legacy_status;
    (void)memset(seed, 0, sizeof(seed));
    (void)memset(migration_governor_seed, 0, sizeof(migration_governor_seed));
    if (fflush(results) != 0 || fsync(results_fd) != 0 || fclose(results) != 0 ||
        fsync(migration_directory_fd) != 0 || close(migration_directory_fd) != 0) return 1;
    return printf("NATIVE_MIGRATION_SUMMARY {\"cases\":%u,\"skipped\":0,\"authority_provisioned\":true,\"incompatible_schedule_refused\":true,\"snapshot_verified\":true}\n", cases) < 0;
}

int main(int argc, char **argv)
{
    if (argc == 2 && strcmp(argv[1], "--native-migration") == 0)
        return native_migration();
    if (argc == 2 && strcmp(argv[1], "--all-form-validation") == 0)
        return all_form_validation();
    if (argc != 1) return 2;
    return original_lifecycle();
}
