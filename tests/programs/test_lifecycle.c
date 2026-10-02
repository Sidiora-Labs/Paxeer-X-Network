#include "layerx/programs.h"

#include "layerx/lxp_genesis.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"

#include <openssl/evp.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>

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

static int project_metering_genesis(lxp_kernel *kernel)
{
    static const uint8_t signer_private_key[32] = {7U};
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
    return 0;
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

int main(int argc, char **argv)
{
    if (argc == 2 && strcmp(argv[1], "--all-form-validation") == 0)
        return all_form_validation();
    if (argc != 1) return 2;
    return original_lifecycle();
}
