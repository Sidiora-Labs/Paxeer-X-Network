#define _POSIX_C_SOURCE 200809L

#include "layerx/programs.h"

#include "layerx/lxp_genesis.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_identity.h"

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
        admission_byte(&body, locals == 0U ? 0U : 1U);
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
    activity->fee_limit = (lxp_u128){0U, 1000000U};
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
    if (lxp_arena_init(&arena, storage, sizeof(storage)) != LXP_OK ||
        lxp_state_root(kernel, root) != LXP_OK ||
        lxp_snapshot_write(kernel, 0U, &arena, &encoded) != LXP_OK ||
        migration_write(name, encoded.bytes, encoded.length) != 0 ||
        lxp_snapshot_manifest_build(encoded.bytes, encoded.length, 0U, root,
            kernel->current_state_root, &manifest) != LXP_OK ||
        lxp_state_store_init(store, 1U) != LXP_OK ||
        lxp_kernel_create(restored, store, journal, &parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(restored, programs_module_registration_v4()) != LXP_OK ||
        lxp_snapshot_load(encoded.bytes, encoded.length, &manifest, restored) != LXP_OK ||
        lxp_snapshot_verify_root(restored, &manifest) != LXP_OK) return 1;
    return 0;
}

static int migration_attempt(lxp_kernel *kernel, lxp_identity *identity,
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
    static lx_account_registry accounts;
    static lx_programs_transfer_runtime runtime;
    size_t blobs_before = kernel->blob_count, staged;
    lx_programs_metering_schedule recorded_schedule;
    int success = expected == LXP_OK;
    if (lx_account_registry_init(&accounts) != LXP_OK) return 1;
    (void)memset(&runtime, 0, sizeof(runtime));
    runtime.accounts = &accounts;
    if (lxp_kernel_bind_module_runtime(kernel, LXP_MODULE_PROGRAMS, &runtime) != LXP_OK ||
        lxp_state_root(kernel, before) != LXP_OK ||
        lxp_programs_metering_schedule_current(kernel, 1U, &recorded_schedule) != LXP_OK ||
        migration_activity(kernel, &activity, identity, seed, signature, payload,
                           length, 2U, &grant, &authority) != 0 ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U, &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 1U, 0U,
                            1U, 1000000U, &arena, false) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK) return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    ctx.batch_number = 1U;
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
    while (kernel->blob_count != 0U) free(kernel->blobs[--kernel->blob_count].bytes);
    (void)lxp_state_store_destroy(store);
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
    lxp_identity *owner, *other;
    lxp_authority_resolved authority;
    lxp_authority_grant grant;
    lxp_activity activity;
    uint8_t seed[32], public_key[32], signature[64], old_wasm[512], wasm[512];
    uint8_t deploy[1024], upgrade[1024], root[32], replay_root[32];
    uint64_t parameters = 1U;
    const char *fd_text = getenv("PAXEER_X_NATIVE_MIGRATION_AUTHORITY_FD");
    const char *directory = getenv("PAXEER_X_NATIVE_MIGRATION_RUN");
    struct stat info;
    char *end;
    long key_fd;
    FILE *results;
    unsigned int cases = 0U;
    if (fd_text == NULL || directory == NULL) return 78;
    errno = 0; key_fd = strtol(fd_text, &end, 10);
    if (errno != 0 || *end != '\0' || key_fd < 0 || key_fd > 1048576L ||
        fstat((int)key_fd, &info) != 0 || !S_ISREG(info.st_mode) ||
        (info.st_mode & 0777U) != 0600U || info.st_uid != geteuid() || info.st_size != 32 ||
        pread((int)key_fd, seed, 32U, 0) != 32 || public_key_for(seed, public_key) != 0) return 78;
    migration_directory_fd = open(directory, O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    if (migration_directory_fd < 0 || fstat(migration_directory_fd, &info) != 0 ||
        info.st_uid != geteuid() || (info.st_mode & 0777U) != 0700U) return 78;
    int results_fd = openat(migration_directory_fd, "results.jsonl", O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if (results_fd < 0 || (results = fdopen(results_fd, "w")) == NULL) return 1;
    if (lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-owner", 30U,
            public_key, &owner) != LXP_OK ||
        lxp_identity_register(&identities, (const uint8_t *)"did:lxp:native-migration-other", 30U,
            public_key, &other) != LXP_OK) return 1;
    for (uint16_t abi = 1U; abi <= 4U; ++abi) {
        for (size_t index = 0U; index < sizeof(names) / sizeof(names[0]); ++index) {
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
                migration_activity(&kernel, &activity, owner, seed, signature, deploy,
                    104U + old_length, 1U, &grant, &authority) != 0 ||
                migration_deploy(&kernel, &activity, &authority) != 0) return 1;
            (void)snprintf(snapshot_name, sizeof(snapshot_name), "baseline.abi%u.%s.snapshot.bin", (unsigned)abi, names[index]);
            if (migration_snapshot(&kernel, &restored, &restored_store, &restored_journal, snapshot_name) != 0 ||
                migration_attempt(&kernel, index == 5U ? other : owner, seed, upgrade, 113U + wasm_length,
                    abi, names[index], "initial", expected[index], results, root) != 0 ||
                migration_attempt(&restored, index == 5U ? other : owner, seed, upgrade, 113U + wasm_length,
                    abi, names[index], "snapshot_replay", expected[index], results, replay_root) != 0 ||
                memcmp(root, replay_root, 32U) != 0) return 1;
            cases += 2U;
            if (index == 0U) {
                (void)snprintf(snapshot_name, sizeof(snapshot_name), "committed.abi%u.snapshot.bin", (unsigned)abi);
                if (migration_snapshot(&kernel, &restarted, &restarted_store, &restarted_journal, snapshot_name) != 0 ||
                    migration_attempt(&restarted, owner, seed, upgrade, 113U + wasm_length,
                        abi, "replay_upgrade", "restart", LXP_ERR_CONTEXT_MISMATCH, results, replay_root) != 0 ||
                    memcmp(root, replay_root, 32U) != 0) return 1;
                ++cases; migration_destroy(&restarted, &restarted_store);
            }
            migration_destroy(&kernel, &store); migration_destroy(&restored, &restored_store);
        }
    }
    (void)memset(seed, 0, sizeof(seed));
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
