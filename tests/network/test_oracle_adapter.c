#include "layerx/lx_oracle.h"
#include "layerx/lxp_crypto.h"

#include <string.h>

int main(void)
{
    static const uint8_t seed[32] = { 12U };
    static const uint8_t actor[] = "did:key:oracle-one";
    static const uint8_t tag[] = "LXP:ORACLE:OBSERVATION:v1";
    lx_oracle_observation observation;
    lxp_activity decoded;
    lxp_arena arena;
    lxp_byte_span encoded;
    static uint8_t arena_bytes[LXP_MAX_ACTIVITY_BYTES];
    uint8_t payload[LX_ORACLE_OBSERVATION_BYTES];
    uint8_t message[sizeof(tag) - 1U + LX_ORACLE_OBSERVATION_BYTES];
    size_t payload_length;

    (void)memset(&observation, 0, sizeof(observation));
    observation.market_id[0] = 1U;
    observation.observation_sequence = UINT64_C(0x0102030405060708);
    observation.price = (lxp_u128){ 2U, 3U };
    observation.observed_at = 1000U;
    observation.source_identifier = 42U;
    if (lx_oracle_observation_sign(&observation, seed) != LXP_OK ||
        lx_oracle_observation_encode(&observation, payload, sizeof(payload),
                                     &payload_length) != LXP_OK ||
        payload_length != LX_ORACLE_OBSERVATION_BYTES ||
        payload[32] != 1U || payload[39] != 8U ||
        payload[40] != 0U || payload[47] != 2U || payload[55] != 3U)
        return 1;
    (void)memcpy(message, tag, sizeof(tag) - 1U);
    (void)memcpy(message + sizeof(tag) - 1U, payload, payload_length);
    if (lxp_ed25519_verify(observation.oracle_public_key,
                           observation.signature,
                           LXP_DOMAIN_SIGNATURE_PREIMAGE,
                           message, sizeof(message)) != LXP_OK ||
        lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lx_oracle_activity_encode(&observation, 9U, actor,
                                  sizeof(actor) - 1U, 7U,
                                  (lxp_u128){ 0U, 100U }, &arena,
                                  &encoded) != LXP_OK ||
        lxp_activity_decode(encoded.bytes, encoded.length, &decoded) != LXP_OK ||
        decoded.protocol_version != LXP_PROTOCOL_VERSION ||
        decoded.network_id != 9U ||
        decoded.activity_type != LX_ORACLE_PUSH_ACTIVITY ||
        decoded.account_sequence != 7U ||
        decoded.timestamp_bound.not_before != 1000U ||
        decoded.timestamp_bound.not_after != 1000U ||
        decoded.payload.length != LX_ORACLE_OBSERVATION_BYTES ||
        memcmp(decoded.payload.bytes, payload, payload_length) != 0 ||
        memcmp(decoded.signature.bytes, observation.signature, 64U) != 0 ||
        lxp_activity_check_envelope(&decoded, 9U) != LXP_OK ||
        lx_oracle_adapter_isolation_check() != LXP_OK)
        return 1;
    lx_oracle_adapter_config config = {0};
    memcpy(config.oracle_private_key, seed, 32U);
    config.network_id = 9U; config.protocol_version = 3U; config.transport_version = 1U;
    config.actor_did = actor; config.actor_did_length = sizeof(actor) - 1U;
    config.next_account_sequence = 7U; config.fee_limit.lo = 100U;
    config.not_before = 1000U; config.not_after = 2000U;
    if (lxp_arena_reset(&arena, 0U) != LXP_OK ||
        lx_oracle_activity_encode_signed(&observation, &config, &arena, &encoded) != LXP_OK ||
        lxp_activity_decode(encoded.bytes, encoded.length, &decoded) != LXP_OK ||
        decoded.protocol_version != 3U || decoded.payload.length != 137U ||
        memcmp(decoded.payload.bytes + 1U, payload, 72U) != 0 ||
        memcmp(decoded.payload.bytes + 73U, observation.signature, 64U) != 0 ||
        memcmp(decoded.signature.bytes, observation.signature, 64U) == 0 ||
        lxp_activity_verify_signature(&decoded) != LXP_OK) return 1;
    return 0;
}
