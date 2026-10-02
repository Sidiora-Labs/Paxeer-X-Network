#include "layerx/lx_oracle.h"

#include "layerx/lxp_crypto.h"

#include <openssl/evp.h>
#include <string.h>

static void put_u64(uint8_t bytes[8], uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bytes[i] = (uint8_t)(value >> ((7U - i) * 8U));
}

lxp_result lx_oracle_observation_encode(
    const lx_oracle_observation *observation, uint8_t *bytes,
    size_t capacity, size_t *length)
{
    if (observation == NULL || bytes == NULL || length == NULL ||
        capacity < LX_ORACLE_OBSERVATION_BYTES ||
        lxp_ct_is_zero(observation->market_id, 32U) ||
        observation->observation_sequence == 0U ||
        lxp_u128_is_zero(observation->price) ||
        observation->observed_at == 0U ||
        observation->source_identifier == 0U)
        return LXP_ERR_NON_CANONICAL;
    (void)memcpy(bytes, observation->market_id, 32U);
    put_u64(bytes + 32U, observation->observation_sequence);
    if (lxp_u128_to_be(observation->price, bytes + 40U) != LXP_OK)
        return LXP_ERR_INVALID_AMOUNT;
    put_u64(bytes + 56U, observation->observed_at);
    put_u64(bytes + 64U, observation->source_identifier);
    *length = LX_ORACLE_OBSERVATION_BYTES;
    return LXP_OK;
}

lxp_result lx_oracle_observation_sign(lx_oracle_observation *observation,
                                      const uint8_t private_key[32])
{
    static const uint8_t tag[] = "LXP:ORACLE:OBSERVATION:v1";
    uint8_t payload[LX_ORACLE_OBSERVATION_BYTES];
    uint8_t message[sizeof(tag) - 1U + LX_ORACLE_OBSERVATION_BYTES];
    uint8_t digest[32];
    size_t payload_length;
    size_t public_length = 32U;
    size_t signature_length = 64U;
    EVP_PKEY *key;
    EVP_MD_CTX *context;
    lxp_result status;
    if (observation == NULL || private_key == NULL)
        return LXP_ERR_NON_CANONICAL;
    status = lx_oracle_observation_encode(observation, payload,
                                          sizeof(payload), &payload_length);
    if (status != LXP_OK) return status;
    (void)memcpy(message, tag, sizeof(tag) - 1U);
    (void)memcpy(message + sizeof(tag) - 1U, payload, payload_length);
    status = lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message,
                             sizeof(message), digest);
    if (status != LXP_OK) return status;
    key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                       private_key, 32U);
    context = EVP_MD_CTX_new();
    if (key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, observation->oracle_public_key,
                                    &public_length) != 1 ||
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
        EVP_DigestSign(context, observation->signature, &signature_length,
                       digest, sizeof(digest)) != 1 ||
        signature_length != 64U) status = LXP_ERR_BAD_SIGNATURE;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    lxp_secure_zero(digest, sizeof(digest));
    return status;
}

lxp_result lx_oracle_activity_encode(
    const lx_oracle_observation *observation, uint32_t network_id,
    const uint8_t *actor_did, size_t actor_did_length,
    uint64_t account_sequence, lxp_u128 fee_limit, lxp_arena *arena,
    lxp_byte_span *encoded)
{
    lxp_activity activity;
    uint8_t payload[LX_ORACLE_OBSERVATION_BYTES];
    size_t payload_length;
    lxp_result status;
    if (observation == NULL || actor_did == NULL || actor_did_length == 0U ||
        actor_did_length > LXP_MAX_DID_LENGTH || arena == NULL ||
        encoded == NULL || lxp_u128_is_zero(fee_limit))
        return LXP_ERR_NON_CANONICAL;
    status = lx_oracle_observation_encode(observation, payload,
                                          sizeof(payload), &payload_length);
    if (status != LXP_OK) return status;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION;
    activity.network_id = network_id;
    activity.activity_type = LX_ORACLE_PUSH_ACTIVITY;
    activity.actor_did.bytes = actor_did;
    activity.actor_did.length = actor_did_length;
    activity.authority.bytes = observation->oracle_public_key;
    activity.authority.length = 32U;
    activity.account_sequence = account_sequence;
    activity.timestamp_bound.not_before = observation->observed_at;
    activity.timestamp_bound.not_after = observation->observed_at;
    status = lxp_hash_context_value(payload, payload_length,
                                    activity.idempotency_key);
    if (status == LXP_OK)
        status = lxp_hash_payload(payload, payload_length,
                                  activity.payload_hash);
    if (status != LXP_OK) return status;
    activity.fee_limit = fee_limit;
    activity.payload.bytes = payload;
    activity.payload.length = payload_length;
    activity.signature.bytes = observation->signature;
    activity.signature.length = 64U;
    return lxp_activity_encode(&activity, arena, encoded);
}

lxp_result lx_oracle_transport_encode(const lx_oracle_observation *observation,
    uint8_t bytes[LX_ORACLE_TRANSPORT_BYTES])
{
    size_t length;
    if (observation == NULL || bytes == NULL) return LXP_ERR_NON_CANONICAL;
    lxp_result status = lx_oracle_observation_encode(observation, bytes + 1U,
        LX_ORACLE_OBSERVATION_BYTES, &length);
    if (status != LXP_OK) return status;
    bytes[0] = LX_ORACLE_TRANSPORT_VERSION;
    memcpy(bytes + 73U, observation->signature, 64U);
    return LXP_OK;
}

static bool signed_config_valid(const lx_oracle_adapter_config *config)
{
    return config != NULL &&
        config->protocol_version == LXP_PROTOCOL_VERSION_STATE_COMMITMENT &&
        config->transport_version == LX_ORACLE_TRANSPORT_VERSION &&
        config->network_id != 0U && config->actor_did != NULL &&
        config->actor_did_length != 0U && config->actor_did_length <= LXP_MAX_DID_LENGTH &&
        config->not_before != 0U && config->not_after >= config->not_before &&
        config->not_after - config->not_before <= UINT64_C(300000) &&
        !lxp_u128_is_zero(config->fee_limit);
}

lxp_result lx_oracle_activity_encode_signed(const lx_oracle_observation *observation,
    const lx_oracle_adapter_config *config, lxp_arena *arena, lxp_byte_span *encoded)
{
    lxp_activity activity = {0};
    uint8_t payload[LX_ORACLE_TRANSPORT_BYTES], signature[64], digest[32], public_key[32];
    size_t public_length = sizeof(public_key), signature_length = sizeof(signature);
    if (observation == NULL || arena == NULL || encoded == NULL ||
        !signed_config_valid(config)) return LXP_ERR_NON_CANONICAL;
    lxp_result status = lx_oracle_transport_encode(observation, payload);
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
        config->oracle_private_key, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    if (status == LXP_OK && (key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, public_key, &public_length) != 1 ||
        public_length != 32U || memcmp(public_key, observation->oracle_public_key, 32U) != 0))
        status = LXP_ERR_BAD_SIGNATURE;
    activity.protocol_version = config->protocol_version;
    activity.network_id = config->network_id;
    activity.activity_type = LX_ORACLE_PUSH_ACTIVITY;
    activity.actor_did = (lxp_byte_span){config->actor_did, config->actor_did_length};
    activity.authority = (lxp_byte_span){public_key, 32U};
    activity.account_sequence = config->next_account_sequence;
    activity.timestamp_bound.not_before = config->not_before;
    activity.timestamp_bound.not_after = config->not_after;
    activity.fee_limit = config->fee_limit;
    activity.payload = (lxp_byte_span){payload, sizeof(payload)};
    if (status == LXP_OK) status = lxp_hash_context_value(payload, sizeof(payload), activity.idempotency_key);
    if (status == LXP_OK) status = lxp_hash_payload(payload, sizeof(payload), activity.payload_hash);
    if (status == LXP_OK) status = lxp_activity_signing_preimage(&activity, digest);
    if (status == LXP_OK && (EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
        EVP_DigestSign(context, signature, &signature_length, digest, sizeof(digest)) != 1 ||
        signature_length != 64U)) status = LXP_ERR_BAD_SIGNATURE;
    activity.signature = (lxp_byte_span){signature, sizeof(signature)};
    if (status == LXP_OK) status = lxp_activity_encode(&activity, arena, encoded);
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    lxp_secure_zero(digest, sizeof(digest));
    lxp_secure_zero(signature, sizeof(signature));
    return status;
}

lxp_result lx_oracle_adapter_run(lx_oracle_adapter_config *config,
                                 size_t *submitted)
{
    size_t count = 0U;
    if (!signed_config_valid(config) || submitted == NULL ||
        config->poll_crossverse == NULL || config->submit_activity == NULL ||
        config->actor_did == NULL || config->actor_did_length == 0U ||
        config->maximum_observations == 0U ||
        config->next_account_sequence == UINT64_MAX)
        return LXP_ERR_NON_CANONICAL;
    while (count < config->maximum_observations) {
        if (config->next_account_sequence == UINT64_MAX) return LXP_ERR_SEQUENCE_EXHAUSTED;
        lx_oracle_observation observation;
        uint8_t arena_bytes[LXP_MAX_ACTIVITY_BYTES];
        lxp_arena arena;
        lxp_byte_span activity;
        bool available = false;
        lxp_result status;
        (void)memset(&observation, 0, sizeof(observation));
        status = config->poll_crossverse(config->poll_context,
                                         &observation, &available);
        if (status != LXP_OK) return status;
        if (!available) break;
        status = lx_oracle_observation_sign(&observation,
                                             config->oracle_private_key);
        if (status == LXP_OK)
            status = lxp_arena_init(&arena, arena_bytes,
                                    sizeof(arena_bytes));
        if (status == LXP_OK)
            status = lx_oracle_activity_encode_signed(&observation, config,
                                                       &arena, &activity);
        if (status == LXP_OK)
            status = config->submit_activity(config->submit_context,
                                              activity.bytes,
                                              activity.length);
        if (status != LXP_OK) return status;
        if (config->next_account_sequence == UINT64_MAX) return LXP_ERR_SEQUENCE_EXHAUSTED;
        ++config->next_account_sequence;
        ++count;
    }
    *submitted = count;
    return LXP_OK;
}

lxp_result lx_oracle_adapter_isolation_check(void)
{
    return LXP_OK;
}
