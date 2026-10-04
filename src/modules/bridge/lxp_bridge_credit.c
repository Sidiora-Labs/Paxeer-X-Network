#include "layerx/lxp_bridge_credit.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"

#include <string.h>

const uint8_t lxp_bridge_profile_key[32] = "custody-credit-profile/v1";
const uint8_t lxp_bridge_light_trust_key[32] = "paxeer-light-trust/v1";

static uint64_t read_u64(const uint8_t *bytes)
{
    uint64_t value = 0U;
    for (size_t index = 0U; index < 8U; ++index)
        value = (value << 8U) | bytes[index];
    return value;
}

static const uint8_t module_domain[] = "LX:CUSTODY:MODULE:v1";
static const uint8_t custody_store[] = "layerxcustody";

static size_t chain_id_length(const lxp_bridge_profile *profile)
{
    const uint8_t *text = profile->bytes + 169U;
    size_t length = 0U;
    while (length < LXP_BRIDGE_LIGHT_MAX_CHAIN_ID && text[length] != 0U) {
        if (text[length] < 0x21U || text[length] > 0x7eU) return 0U;
        ++length;
    }
    return lxp_ct_is_zero(text + length, LXP_BRIDGE_LIGHT_MAX_CHAIN_ID - length) ? length : 0U;
}

lxp_result lxp_bridge_profile_validate(const lxp_bridge_profile *profile)
{
    uint8_t reserve[32];
    uint8_t module[32];
    uint8_t identity[sizeof(module_domain) - 1U + sizeof(custody_store) - 1U + 20U];
    static const uint8_t name[] = "system:paxeer-reserve";
    if (profile == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memcpy(identity, module_domain, sizeof(module_domain) - 1U);
    (void)memcpy(identity + sizeof(module_domain) - 1U, custody_store, sizeof(custody_store) - 1U);
    (void)memcpy(identity + sizeof(identity) - 20U, profile->bytes + 13U, 20U);
    if (lxp_hash_sha256(identity, sizeof(identity), module) != LXP_OK ||
        lxp_ct_memcmp(module, profile->bytes + 33U, 32U) != 0 ||
        memcmp(profile->bytes, "LXBC3", 5U) != 0 ||
        read_u64(profile->bytes + 5U) != 125U ||
        lxp_ct_is_zero(profile->bytes + 13U, 20U) ||
        lxp_ct_is_zero(profile->bytes + 33U, 32U) ||
        lxp_ct_is_zero(profile->bytes + 65U, 32U) ||
        lxp_ct_is_zero(profile->bytes + 97U, 32U) ||
        read_u64(profile->bytes + 161U) == 0U ||
        read_u64(profile->bytes + 161U) >= (uint64_t)INT64_MAX ||
        chain_id_length(profile) == 0U ||
        lxp_ct_is_zero(profile->bytes + 201U, 4U) ||
        profile->bytes[205] != 0U || profile->bytes[206] != 3U ||
        read_u64(profile->bytes + 207U) == 0U ||
        read_u64(profile->bytes + 207U) > UINT32_MAX ||
        read_u64(profile->bytes + 215U) == 0U ||
        read_u64(profile->bytes + 215U) > UINT64_C(253402300799) ||
        lx_account_id_from_string(name, sizeof(name) - 1U, reserve) != LXP_OK ||
        lxp_ct_memcmp(reserve, profile->bytes + 129U, 32U) != 0)
        return LXP_ERR_NON_CANONICAL;
    return LXP_OK;
}


static const char *const custody_symbols[LXP_BRIDGE_REGISTRY_COUNT] = {
    "PAX", "SID", "USDC", "USDL"
};
static const char *const custody_reserves[LXP_BRIDGE_REGISTRY_COUNT] = {
    "system:paxeer-reserve:pax", "system:paxeer-reserve:sid",
    "system:paxeer-reserve:usdc", "system:paxeer-reserve:usdl"
};

static lxp_result registry_asset(size_t index, uint8_t asset[32])
{
    uint8_t text[25] = "layerx-asset:125:";
    size_t length;
    if (index >= LXP_BRIDGE_REGISTRY_COUNT || asset == NULL) return LXP_ERR_NON_CANONICAL;
    length = strlen(custody_symbols[index]);
    (void)memcpy(text + 16U, custody_symbols[index], length);
    return lxp_hash_sha256(text, 16U + length, asset);
}

lxp_result lxp_bridge_registry_key(uint8_t key[32])
{
    static const uint8_t domain[] = "LX:CUSTODY:REGISTRY:v1";
    return key == NULL ? LXP_ERR_NON_CANONICAL : lxp_hash_sha256(domain, sizeof(domain) - 1U, key);
}

lxp_result lxp_bridge_profile_key_asset(const uint8_t asset_id[32], uint8_t key[32])
{
    uint8_t input[sizeof("LX:CUSTODY:PROFILE:v2") - 1U + 32U] = "LX:CUSTODY:PROFILE:v2";
    if (asset_id == NULL || key == NULL) return LXP_ERR_NON_CANONICAL;
    (void)memcpy(input + sizeof(input) - 32U, asset_id, 32U);
    return lxp_hash_sha256(input, sizeof(input), key);
}

lxp_result lxp_bridge_light_trust_key_asset(const lxp_bridge_profile *profile, uint8_t key[32])
{
    uint8_t input[sizeof("LX:CUSTODY:TRUST:v2") - 1U + 32U] = "LX:CUSTODY:TRUST:v2";
    if (profile == NULL || key == NULL) return LXP_ERR_NON_CANONICAL;
    if (memcmp(profile->bytes, "LXBC3", 5U) == 0) {
        if (lxp_bridge_profile_validate(profile) != LXP_OK) return LXP_ERR_NON_CANONICAL;
        (void)memcpy(key, lxp_bridge_light_trust_key, 32U);
        return LXP_OK;
    }
    if (lxp_bridge_profile_validate_asset(profile) != LXP_OK) return LXP_ERR_NON_CANONICAL;
    (void)memcpy(input + sizeof(input) - 32U, profile->bytes + 97U, 32U);
    return lxp_hash_sha256(input, sizeof(input), key);
}

lxp_result lxp_bridge_reserve_name(const uint8_t asset_id[32], uint8_t *name,
                                  size_t capacity, size_t *length)
{
    uint8_t asset[32];
    if (asset_id == NULL || name == NULL || length == NULL) return LXP_ERR_NON_CANONICAL;
    for (size_t index = 0U; index < LXP_BRIDGE_REGISTRY_COUNT; ++index) {
        if (registry_asset(index, asset) != LXP_OK) return LXP_ERR_NON_CANONICAL;
        if (memcmp(asset, asset_id, 32U) == 0) {
            *length = strlen(custody_reserves[index]);
            if (capacity < *length) return LXP_ERR_NON_CANONICAL;
            (void)memcpy(name, custody_reserves[index], *length);
            return LXP_OK;
        }
    }
    return LXP_ERR_ASSET_MISMATCH;
}

lxp_result lxp_bridge_profile_validate_asset(const lxp_bridge_profile *profile)
{
    lxp_bridge_profile legacy;
    uint8_t name[LX_ACCOUNT_NAME_MAX], reserve[32];
    size_t length;
    static const uint8_t legacy_name[] = "system:paxeer-reserve";
    if (profile == NULL || memcmp(profile->bytes, "LXBC4", 5U) != 0 ||
        lxp_bridge_reserve_name(profile->bytes + 97U, name, sizeof(name), &length) != LXP_OK ||
        lx_account_id_from_string(name, length, reserve) != LXP_OK ||
        memcmp(reserve, profile->bytes + 129U, 32U) != 0)
        return LXP_ERR_NON_CANONICAL;
    legacy = *profile;
    (void)memcpy(legacy.bytes, "LXBC3", 5U);
    if (lx_account_id_from_string(legacy_name, sizeof(legacy_name) - 1U, legacy.bytes + 129U) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    return lxp_bridge_profile_validate(&legacy);
}

lxp_result lxp_bridge_profile_beneficiary(const lxp_bridge_profile *profile,
                                         const uint8_t *did, size_t did_length,
                                         uint8_t *name, size_t capacity,
                                         size_t *name_length, uint8_t beneficiary[32])
{
    static const uint8_t digits[] = "0123456789abcdef";
    bool asset_profile = profile != NULL && memcmp(profile->bytes, "LXBC4", 5U) == 0;
    size_t suffix = asset_profile ? 71U : 5U;
    if (did == NULL || name == NULL || name_length == NULL || beneficiary == NULL ||
        did_length == 0U || capacity < 6U + suffix || did_length > capacity - 6U - suffix ||
        (asset_profile ? lxp_bridge_profile_validate_asset(profile) : lxp_bridge_profile_validate(profile)) != LXP_OK)
        return LXP_ERR_ACCOUNT_ID_MISMATCH;
    (void)memcpy(name, "agent:", 6U);
    (void)memcpy(name + 6U, did, did_length);
    size_t at = 6U + did_length;
    if (asset_profile) {
        (void)memcpy(name + at, ":asset:", 7U); at += 7U;
        for (size_t i = 0U; i < 32U; ++i) {
            name[at + 2U * i] = digits[profile->bytes[97U + i] >> 4U];
            name[at + 2U * i + 1U] = digits[profile->bytes[97U + i] & 15U];
        }
        at += 64U;
    } else { (void)memcpy(name + at, ":main", 5U); at += 5U; }
    *name_length = at;
    return lx_account_id_from_string(name, at, beneficiary);
}

static lxp_result registry_profiles_validate(const lxp_bridge_profile profiles[LXP_BRIDGE_REGISTRY_COUNT])
{
    uint8_t asset[32];
    if (profiles == NULL) return LXP_ERR_NON_CANONICAL;
    for (size_t index = 0U; index < LXP_BRIDGE_REGISTRY_COUNT; ++index) {
        if (registry_asset(index, asset) != LXP_OK ||
            memcmp(asset, profiles[index].bytes + 97U, 32U) != 0 ||
            lxp_bridge_profile_validate_asset(&profiles[index]) != LXP_OK ||
            memcmp(profiles[0].bytes + 5U, profiles[index].bytes + 5U, 92U) != 0 ||
            memcmp(profiles[0].bytes + 161U, profiles[index].bytes + 161U, 62U) != 0)
            return LXP_ERR_NON_CANONICAL;
    }
    return LXP_OK;
}

lxp_result lxp_bridge_registry_decode(const uint8_t *bytes, size_t length,
                                      lxp_bridge_profile profiles[LXP_BRIDGE_REGISTRY_COUNT])
{
    if (bytes == NULL || profiles == NULL || length != LXP_BRIDGE_REGISTRY_BYTES ||
        memcmp(bytes, "LXBR1", 5U) != 0) return LXP_ERR_NON_CANONICAL;
    for (size_t index = 0U; index < LXP_BRIDGE_REGISTRY_COUNT; ++index) {
        size_t offset = 5U + index * (1U + LXP_BRIDGE_PROFILE_BYTES);
        if (bytes[offset] != index + 1U) return LXP_ERR_NON_CANONICAL;
        (void)memcpy(profiles[index].bytes, bytes + offset + 1U, LXP_BRIDGE_PROFILE_BYTES);
    }
    return registry_profiles_validate(profiles);
}

lxp_result lxp_bridge_registry_append(lxp_genesis_manifest *manifest,
                                      const lxp_bridge_profile profiles[LXP_BRIDGE_REGISTRY_COUNT])
{
    uint8_t keys[LXP_BRIDGE_REGISTRY_COUNT + 1U][32];
    if (manifest == NULL || manifest->protocol_version != 3U ||
        registry_profiles_validate(profiles) != LXP_OK ||
        manifest->module_value_count > LXP_GENESIS_MAX_MODULE_VALUES - 5U ||
        lxp_bridge_registry_key(keys[0]) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    for (size_t index = 0U; index < LXP_BRIDGE_REGISTRY_COUNT; ++index) {
        if (lxp_bridge_profile_key_asset(profiles[index].bytes + 97U, keys[index + 1U]) != LXP_OK ||
            (((uint32_t)profiles[index].bytes[201] << 24U) | ((uint32_t)profiles[index].bytes[202] << 16U) |
             ((uint32_t)profiles[index].bytes[203] << 8U) | profiles[index].bytes[204]) != manifest->network_id)
            return LXP_ERR_NON_CANONICAL;
    }
    for (size_t index = 0U; index < manifest->module_value_count; ++index)
        if (manifest->module_values[index].module_id == LXP_MODULE_BRIDGE)
            for (size_t key = 0U; key < LXP_BRIDGE_REGISTRY_COUNT + 1U; ++key)
                if (memcmp(manifest->module_values[index].key, keys[key], 32U) == 0)
                    return LXP_ERR_SEQUENCE_REUSED;
    for (size_t key = 0U; key < LXP_BRIDGE_REGISTRY_COUNT + 1U; ++key) {
        size_t position = 0U;
        while (position < manifest->module_value_count &&
               (manifest->module_values[position].module_id < LXP_MODULE_BRIDGE ||
                (manifest->module_values[position].module_id == LXP_MODULE_BRIDGE &&
                 memcmp(manifest->module_values[position].key, keys[key], 32U) < 0))) ++position;
        (void)memmove(&manifest->module_values[position + 1U], &manifest->module_values[position],
                       (manifest->module_value_count - position) * sizeof(manifest->module_values[0]));
        lxp_genesis_module_value *entry = &manifest->module_values[position];
        (void)memset(entry, 0, sizeof(*entry));
        entry->module_id = LXP_MODULE_BRIDGE;
        (void)memcpy(entry->key, keys[key], 32U);
        entry->value_length = key == 0U ? 5U : LXP_BRIDGE_PROFILE_BYTES;
        (void)memcpy(entry->value, key == 0U ? (const uint8_t *)"LXBR1" : profiles[key - 1U].bytes, entry->value_length);
        ++manifest->module_value_count;
    }
    return LXP_OK;
}

lxp_result lxp_bridge_registry_profile(const lxp_genesis_manifest *manifest,
                                       const uint8_t asset_id[32], lxp_bridge_profile *profile, bool *present)
{
    uint8_t marker[32], key[32];
    lxp_bridge_profile profiles[LXP_BRIDGE_REGISTRY_COUNT];
    size_t marker_count = 0U;
    if (manifest == NULL || asset_id == NULL || profile == NULL || present == NULL ||
        manifest->module_value_count > LXP_GENESIS_MAX_MODULE_VALUES ||
        lxp_bridge_registry_key(marker) != LXP_OK) return LXP_ERR_NON_CANONICAL;
    *present = false;
    for (size_t i = 0U; i < manifest->module_value_count; ++i) {
        const lxp_genesis_module_value *entry = &manifest->module_values[i];
        if (entry->module_id == LXP_MODULE_BRIDGE && memcmp(entry->key, marker, 32U) == 0) {
            if (++marker_count != 1U || entry->value_length != 5U || memcmp(entry->value, "LXBR1", 5U) != 0)
                return LXP_ERR_NON_CANONICAL;
        }
    }
    for (size_t slot = 0U; slot < LXP_BRIDGE_REGISTRY_COUNT; ++slot) {
        uint8_t asset[32]; size_t count = 0U;
        if (registry_asset(slot, asset) != LXP_OK || lxp_bridge_profile_key_asset(asset, key) != LXP_OK)
            return LXP_ERR_NON_CANONICAL;
        for (size_t i = 0U; i < manifest->module_value_count; ++i) {
            const lxp_genesis_module_value *entry = &manifest->module_values[i];
            if (entry->module_id == LXP_MODULE_BRIDGE && memcmp(entry->key, key, 32U) == 0) {
                if (++count != 1U || marker_count == 0U || entry->value_length != LXP_BRIDGE_PROFILE_BYTES)
                    return LXP_ERR_NON_CANONICAL;
                (void)memcpy(profiles[slot].bytes, entry->value, LXP_BRIDGE_PROFILE_BYTES);
            }
        }
        if (marker_count != 0U && count != 1U) return LXP_ERR_NON_CANONICAL;
    }
    if (marker_count == 0U) return LXP_OK;
    for (size_t i = 0U; i < manifest->module_value_count; ++i) {
        const lxp_genesis_module_value *entry = &manifest->module_values[i];
        if (entry->module_id != LXP_MODULE_BRIDGE) continue;
        bool known = memcmp(entry->key, marker, 32U) == 0 ||
                     memcmp(entry->key, lxp_bridge_profile_key, 32U) == 0;
        for (size_t slot = 0U; !known && slot < LXP_BRIDGE_REGISTRY_COUNT; ++slot) {
            if (lxp_bridge_profile_key_asset(profiles[slot].bytes + 97U, key) != LXP_OK)
                return LXP_ERR_NON_CANONICAL;
            known = memcmp(entry->key, key, 32U) == 0;
        }
        if (!known) return LXP_ERR_NON_CANONICAL;
    }
    if (manifest->protocol_version != 3U || registry_profiles_validate(profiles) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    for (size_t slot = 0U; slot < LXP_BRIDGE_REGISTRY_COUNT; ++slot) {
        const uint8_t *bytes = profiles[slot].bytes;
        if ((((uint32_t)bytes[201] << 24U) | ((uint32_t)bytes[202] << 16U) |
             ((uint32_t)bytes[203] << 8U) | bytes[204]) != manifest->network_id)
            return LXP_ERR_NON_CANONICAL;
        if (memcmp(bytes + 97U, asset_id, 32U) == 0) { *profile = profiles[slot]; *present = true; }
    }
    return *present ? LXP_OK : LXP_ERR_ASSET_MISMATCH;
}

lxp_result lxp_bridge_profile_load_asset(lxp_module_ctx *ctx, const uint8_t asset_id[32], lxp_bridge_profile *profile)
{
    uint8_t key[32], asset[32]; const uint8_t *stored; size_t length;
    lxp_bridge_profile profiles[LXP_BRIDGE_REGISTRY_COUNT];
    if (ctx == NULL || asset_id == NULL || profile == NULL || lxp_bridge_registry_key(key) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    lxp_result status = lxp_ctx_kv_get(ctx, key, 32U, &stored, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) {
        status = lxp_ctx_kv_get(ctx, lxp_bridge_profile_key, 32U, &stored, &length);
        if (status != LXP_OK || length != LXP_BRIDGE_PROFILE_BYTES) return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
        (void)memcpy(profile->bytes, stored, length);
        return lxp_bridge_profile_validate(profile) == LXP_OK &&
               memcmp(profile->bytes + 97U, asset_id, 32U) == 0 ? LXP_OK : LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    }
    if (status != LXP_OK || length != 5U || memcmp(stored, "LXBR1", 5U) != 0)
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    for (size_t slot = 0U; slot < LXP_BRIDGE_REGISTRY_COUNT; ++slot) {
        if (registry_asset(slot, asset) != LXP_OK || lxp_bridge_profile_key_asset(asset, key) != LXP_OK)
            return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
        status = lxp_ctx_kv_get(ctx, key, 32U, &stored, &length);
        if (status != LXP_OK || length != LXP_BRIDGE_PROFILE_BYTES) return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
        (void)memcpy(profiles[slot].bytes, stored, length);
    }
    if (registry_profiles_validate(profiles) != LXP_OK) return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    for (size_t slot = 0U; slot < LXP_BRIDGE_REGISTRY_COUNT; ++slot)
        if (memcmp(profiles[slot].bytes + 97U, asset_id, 32U) == 0) { *profile = profiles[slot]; return LXP_OK; }
    return LXP_ERR_ASSET_MISMATCH;
}

void lxp_bridge_profile_trust(const lxp_bridge_profile *profile,
                              lxp_bridge_light_trust *trust)
{
    (void)memset(trust, 0, sizeof(*trust));
    trust->height = read_u64(profile->bytes + 161U);
    (void)memcpy(trust->next_validators_hash, profile->bytes + 65U, 32U);
    trust->time_seconds = (int64_t)read_u64(profile->bytes + 215U);
}

lxp_result lxp_bridge_light_trust_load(lxp_module_ctx *ctx,
                                       const lxp_bridge_profile *profile,
                                       lxp_bridge_light_trust *trust)
{
    const uint8_t *stored;
    size_t length;
    lxp_result status;
    if (ctx == NULL || trust == NULL || lxp_bridge_profile_validate(profile) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_kv_get(ctx, lxp_bridge_light_trust_key, 32U, &stored, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) {
        lxp_bridge_profile_trust(profile, trust);
        return LXP_OK;
    }
    if (status != LXP_OK) return status;
    status = lxp_bridge_light_trust_decode(stored, length, trust);
    if (status == LXP_OK &&
        (trust->height <= read_u64(profile->bytes + 161U) ||
         trust->time_seconds < (int64_t)read_u64(profile->bytes + 215U) ||
         (trust->time_seconds == (int64_t)read_u64(profile->bytes + 215U) &&
          trust->time_nanos == 0U)))
        status = LXP_ERR_NON_CANONICAL;
    return status;
}

lxp_result lxp_bridge_light_trust_load_asset(lxp_module_ctx *ctx,
                                            const lxp_bridge_profile *profile,
                                            lxp_bridge_light_trust *trust)
{
    uint8_t key[32]; const uint8_t *stored; size_t length;
    if (profile != NULL && memcmp(profile->bytes, "LXBC3", 5U) == 0)
        return lxp_bridge_light_trust_load(ctx, profile, trust);
    if (ctx == NULL || trust == NULL || lxp_bridge_light_trust_key_asset(profile, key) != LXP_OK)
        return LXP_ERR_NON_CANONICAL;
    lxp_result status = lxp_ctx_kv_get(ctx, key, 32U, &stored, &length);
    if (status == LXP_ERR_UNKNOWN_FIELD) { lxp_bridge_profile_trust(profile, trust); return LXP_OK; }
    if (status != LXP_OK) return status;
    status = lxp_bridge_light_trust_decode(stored, length, trust);
    if (status == LXP_OK &&
        (trust->height <= read_u64(profile->bytes + 161U) ||
         trust->time_seconds < (int64_t)read_u64(profile->bytes + 215U) ||
         (trust->time_seconds == (int64_t)read_u64(profile->bytes + 215U) &&
          trust->time_nanos == 0U)))
        status = LXP_ERR_NON_CANONICAL;
    return status;
}

lxp_result lxp_bridge_credit_parse(const uint8_t *payload, size_t length,
                                   lxp_bridge_credit *credit)
{
    if (payload == NULL || credit == NULL || length < LXP_BRIDGE_CREDIT_MIN_PAYLOAD_BYTES ||
        length > LXP_MAX_PAYLOAD_BYTES)
        return LXP_ERR_NON_CANONICAL;
    (void)memcpy(credit->bytes, payload, sizeof(credit->bytes));
    credit->proof = payload + sizeof(credit->bytes);
    credit->proof_length = length - sizeof(credit->bytes);
    return LXP_OK;
}

bool lxp_bridge_credit_matches(const lxp_bridge_credit *credit,
                               const uint8_t *payload, size_t length)
{
    return credit != NULL && payload != NULL && credit->proof != NULL &&
        length >= LXP_BRIDGE_CREDIT_MIN_PAYLOAD_BYTES &&
        length - sizeof(credit->bytes) == credit->proof_length &&
        memcmp(payload, credit->bytes, sizeof(credit->bytes)) == 0 &&
        memcmp(payload + sizeof(credit->bytes), credit->proof, credit->proof_length) == 0;
}

bool lxp_bridge_credit_owner_bound(const uint8_t *did, size_t did_length,
                                   const uint8_t owner_key[32])
{
    static const char digits[] = "0123456789abcdef";
    uint8_t expected[75] = "did:layerx:";
    if (did == NULL || owner_key == NULL || did_length != sizeof(expected)) return false;
    for (size_t index = 0U; index < 32U; ++index) {
        expected[11U + index * 2U] = (uint8_t)digits[owner_key[index] >> 4U];
        expected[12U + index * 2U] = (uint8_t)digits[owner_key[index] & 15U];
    }
    return lxp_ct_memcmp(did, expected, sizeof(expected)) == 0;
}

lxp_result lxp_bridge_genesis_profile(const lxp_genesis_manifest *manifest,
                                     lxp_bridge_profile *profile, bool *present)
{
    if (manifest == NULL || profile == NULL || present == NULL ||
        manifest->module_value_count > LXP_GENESIS_MAX_MODULE_VALUES)
        return LXP_ERR_NON_CANONICAL;
    *present = false;
    (void)memset(profile, 0, sizeof(*profile));
    for (size_t index = 0U; index < manifest->module_value_count; ++index) {
        const lxp_genesis_module_value *entry = &manifest->module_values[index];
        if (entry->module_id != LXP_MODULE_BRIDGE ||
            memcmp(entry->key, lxp_bridge_profile_key, 32U) != 0) continue;
        if (*present || manifest->protocol_version != 3U ||
            entry->value_length != sizeof(profile->bytes))
            return LXP_ERR_NON_CANONICAL;
        (void)memcpy(profile->bytes, entry->value, sizeof(profile->bytes));
        if (lxp_bridge_profile_validate(profile) != LXP_OK)
            return LXP_ERR_NON_CANONICAL;
        if ((((uint32_t)profile->bytes[201] << 24U) |
             ((uint32_t)profile->bytes[202] << 16U) |
             ((uint32_t)profile->bytes[203] << 8U) | profile->bytes[204]) != manifest->network_id)
            return LXP_ERR_NON_CANONICAL;
        *present = true;
    }
    return LXP_OK;
}

lxp_result lxp_bridge_genesis_append(lxp_genesis_manifest *manifest,
                                    const lxp_bridge_profile *profile)
{
    size_t position = 0U;
    if (manifest == NULL || manifest->protocol_version != 3U ||
        lxp_bridge_profile_validate(profile) != LXP_OK ||
        manifest->module_value_count >= LXP_GENESIS_MAX_MODULE_VALUES)
        return LXP_ERR_NON_CANONICAL;
    for (size_t index = 0U; index < manifest->module_value_count; ++index)
        if (manifest->module_values[index].module_id == LXP_MODULE_BRIDGE)
            return LXP_ERR_SEQUENCE_REUSED;
    while (position < manifest->module_value_count &&
           manifest->module_values[position].module_id < LXP_MODULE_BRIDGE)
        ++position;
    (void)memmove(&manifest->module_values[position + 1U],
                  &manifest->module_values[position],
                  (manifest->module_value_count - position) *
                      sizeof(manifest->module_values[0]));
    (void)memset(&manifest->module_values[position], 0,
                 sizeof(manifest->module_values[0]));
    manifest->module_values[position].module_id = LXP_MODULE_BRIDGE;
    (void)memcpy(manifest->module_values[position].key,
                 lxp_bridge_profile_key, 32U);
    (void)memcpy(manifest->module_values[position].value,
                 profile->bytes, sizeof(profile->bytes));
    manifest->module_values[position].value_length = sizeof(profile->bytes);
    ++manifest->module_value_count;
    return LXP_OK;
}

static lxp_result credit_verify(const lxp_bridge_profile *profile,
                                    const lxp_bridge_credit *credit,
                                    uint32_t network_id,
                                    uint16_t protocol_version,
                                    const lxp_bridge_light_trust *trusted,
                                    uint64_t now_ms,
                                    uint8_t nullifier[32],
                                    lxp_bridge_light_trust *advanced)
{
    static const uint8_t deposit_domain[] = "LXP/Paxeer/custody-deposit/v1";
    static const uint8_t nullifier_domain[] = "LX:DEPOSIT:NULLIFIER:v1";
    uint8_t deposit[320] = {0};
    uint8_t digest[32];
    uint8_t nullifier_input[sizeof(nullifier_domain) - 1U + 32U];
    lxp_bridge_light_trust seeded;
    lxp_bridge_light_result proven;
    lxp_bridge_light_deposit record;
    const uint8_t *bytes;
    uint32_t network;
    uint64_t state_height;
    lxp_result status;
    if (credit == NULL || nullifier == NULL || protocol_version != 3U ||
        network_id == 0U || profile == NULL ||
        (lxp_bridge_profile_validate(profile) != LXP_OK &&
         lxp_bridge_profile_validate_asset(profile) != LXP_OK) ||
        credit->proof == NULL || credit->proof_length == 0U ||
        credit->proof_length > LXP_MAX_PAYLOAD_BYTES - sizeof(credit->bytes))
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    bytes = credit->bytes;
    network = ((uint32_t)bytes[37] << 24U) | ((uint32_t)bytes[38] << 16U) |
              ((uint32_t)bytes[39] << 8U) | bytes[40];
    state_height = read_u64(bytes + 215U);
    status = lxp_hash_sha256(profile->bytes, sizeof(profile->bytes), digest);
    if (status != LXP_OK) return status;
    if (memcmp(bytes, "LXDC3", 5U) != 0 || network != network_id ||
        memcmp(bytes + 37U, profile->bytes + 201U, 6U) != 0 ||
        bytes[41] != 0U || bytes[42] != 3U ||
        lxp_ct_memcmp(bytes + 5U, digest, 32U) != 0 ||
        lxp_ct_memcmp(bytes + 75U, profile->bytes + 97U, 32U) != 0 ||
        lxp_ct_is_zero(bytes + 107U, 32U) ||
        !lxp_ed25519_pubkey_is_canonical(bytes + 139U) ||
        lxp_ct_is_zero(bytes + 171U, 20U) ||
        lxp_ct_is_zero(bytes + 191U, 16U) || read_u64(bytes + 207U) == 0U ||
        state_height == 0U || state_height >= (uint64_t)INT64_MAX - 1U ||
        read_u64(bytes + 287U) != state_height + 1U ||
        bytes[359] != 0U || bytes[360] != 0U || bytes[361] != 0U ||
        bytes[362] != LXP_BRIDGE_LIGHT_PROOF_KIND)
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    status = lxp_hash_sha256(credit->proof, credit->proof_length, digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, bytes + 327U, 32U) != 0)
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    deposit[30] = 1U;
    (void)memcpy(deposit + 56U, profile->bytes + 5U, 8U);
    (void)memcpy(deposit + 76U, profile->bytes + 13U, 20U);
    (void)memcpy(deposit + 108U, bytes + 171U, 20U);
    (void)memcpy(deposit + 128U, bytes + 75U, 32U);
    (void)memcpy(deposit + 160U, bytes + 107U, 32U);
    (void)memcpy(deposit + 208U, bytes + 191U, 16U);
    (void)memcpy(deposit + 248U, bytes + 207U, 8U);
    deposit[287] = (uint8_t)(sizeof(deposit_domain) - 1U);
    (void)memcpy(deposit + 288U, deposit_domain, sizeof(deposit_domain) - 1U);
    status = lxp_hash_sha256(deposit, sizeof(deposit), digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, bytes + 43U, 32U) != 0)
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    if (trusted == NULL) {
        lxp_bridge_profile_trust(profile, &seeded);
        trusted = &seeded;
    } else if (trusted->height < read_u64(profile->bytes + 161U)) {
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    }
    status = lxp_bridge_light_verify(profile->bytes + 169U, chain_id_length(profile),
                                     custody_store, sizeof(custody_store) - 1U, trusted,
                                     read_u64(profile->bytes + 207U), now_ms / 1000U,
                                     credit->proof, credit->proof_length, &proven);
    if (status != LXP_OK) return status;
    if (proven.height != state_height + 1U ||
        lxp_ct_memcmp(proven.header_hash, bytes + 223U, 32U) != 0 ||
        lxp_ct_memcmp(proven.app_hash, bytes + 255U, 32U) != 0 ||
        lxp_ct_memcmp(proven.validators_hash, bytes + 295U, 32U) != 0 ||
        proven.key_length != 33U || proven.key[0] != 0x20U ||
        lxp_ct_memcmp(proven.key + 1U, bytes + 43U, 32U) != 0 ||
        lxp_bridge_light_deposit_decode(proven.value, proven.value_length, &record) != LXP_OK ||
        lxp_ct_memcmp(record.deposit_id, bytes + 43U, 32U) != 0 ||
        lxp_ct_memcmp(record.asset_id, bytes + 75U, 32U) != 0 ||
        lxp_ct_memcmp(record.beneficiary, bytes + 107U, 32U) != 0 ||
        lxp_ct_memcmp(record.depositor, bytes + 171U, 20U) != 0 ||
        lxp_ct_memcmp(record.amount, bytes + 191U, 16U) != 0 ||
        record.nonce != read_u64(bytes + 207U) || record.height > state_height)
        return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    if (advanced != NULL) *advanced = proven.advanced;
    (void)memcpy(nullifier_input, nullifier_domain, sizeof(nullifier_domain) - 1U);
    (void)memcpy(nullifier_input + sizeof(nullifier_domain) - 1U, bytes + 43U, 32U);
    return lxp_hash_sha256(nullifier_input, sizeof(nullifier_input), nullifier);
}

lxp_result lxp_bridge_credit_verify(const lxp_bridge_profile *profile,
                                    const lxp_bridge_credit *credit, uint32_t network_id,
                                    uint16_t protocol_version, const lxp_bridge_light_trust *trusted,
                                    uint64_t now_ms, uint8_t nullifier[32], lxp_bridge_light_trust *advanced)
{
    if (lxp_bridge_profile_validate(profile) != LXP_OK) return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    return credit_verify(profile, credit, network_id, protocol_version, trusted, now_ms, nullifier, advanced);
}

lxp_result lxp_bridge_credit_verify_asset(const lxp_bridge_profile *profile,
                                         const lxp_bridge_credit *credit, uint32_t network_id,
                                         uint16_t protocol_version, const lxp_bridge_light_trust *trusted,
                                         uint64_t now_ms, uint8_t nullifier[32], lxp_bridge_light_trust *advanced)
{
    if (profile != NULL && memcmp(profile->bytes, "LXBC3", 5U) == 0)
        return lxp_bridge_credit_verify(profile, credit, network_id, protocol_version, trusted, now_ms, nullifier, advanced);
    if (lxp_bridge_profile_validate_asset(profile) != LXP_OK) return LXP_ERR_DEPOSIT_PROOF_NOT_FINAL;
    return credit_verify(profile, credit, network_id, protocol_version, trusted, now_ms, nullifier, advanced);
}

static lxp_result genesis(lxp_module_ctx *ctx, const uint8_t *bytes, size_t length)
{
    return ctx == NULL || (bytes == NULL && length != 0U) ?
        LXP_ERR_NON_CANONICAL : lxp_ctx_charge_gas(ctx, length);
}

static lxp_result decode(lxp_module_ctx *ctx, uint16_t ordinal,
                          const uint8_t *bytes, size_t length, void **decoded)
{
    void *memory = NULL;
    lxp_result status;
    if (ctx == NULL || ordinal != 1U || bytes == NULL || decoded == NULL ||
        length < LXP_BRIDGE_CREDIT_MIN_PAYLOAD_BYTES || length > LXP_MAX_PAYLOAD_BYTES)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_ctx_arena_alloc(ctx, sizeof(lxp_bridge_credit),
                                 _Alignof(lxp_bridge_credit), &memory);
    if (status != LXP_OK) return status;
    status = lxp_bridge_credit_parse(bytes, length, memory);
    if (status != LXP_OK) return status;
    *decoded = memory;
    return LXP_OK;
}

static lxp_result validate_credit(lxp_module_ctx *ctx, const lxp_activity *activity,
                                  const lxp_authority_resolved *authority,
                                  const void *decoded)
{
    if (ctx == NULL || activity == NULL || authority == NULL || decoded == NULL ||
        activity->activity_type != LXP_BRIDGE_CREDIT ||
        activity->protocol_version != 3U || authority->kind != LXP_AUTHORITY_OWNER)
        return LXP_ERR_UNAUTHORIZED_DEBIT;
    return lxp_ctx_charge_gas(ctx, activity->payload.length);
}

static lxp_result execute(lxp_module_ctx *ctx, const lxp_activity *activity,
                          const lxp_authority_resolved *authority,
                          const void *decoded, lxp_effect_buffer *effects)
{
    (void)effects;
    return lxp_ctx_bridge_credit(ctx, activity, authority, decoded);
}

static lxp_result epoch(lxp_module_ctx *ctx, uint64_t number, uint64_t timestamp)
{
    (void)number;
    (void)timestamp;
    return ctx == NULL ? LXP_ERR_NON_CANONICAL : LXP_OK;
}

static lxp_result root(lxp_module_ctx *ctx, uint8_t digest[32])
{
    return ctx == NULL ? LXP_ERR_NON_CANONICAL :
        lxp_state_subtree_root(ctx->kernel, LXP_MODULE_BRIDGE, digest);
}

const lxp_module_iface *lxp_bridge_module_iface(void)
{
    static const uint32_t types[] = {LXP_BRIDGE_CREDIT};
    static const lxp_module_iface iface = {
        LXP_MODULE_BRIDGE, 1U, "bridge", types, 1U, genesis, decode,
        validate_credit, execute, epoch, epoch, root, NULL
    };
    return &iface;
}
