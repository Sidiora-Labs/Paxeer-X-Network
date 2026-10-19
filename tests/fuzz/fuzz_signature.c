#include "layerx/lxp_fuzz.h"

#include "layerx/lxp_crypto.h"

#include <string.h>

static lxp_result must_reject(const lxp_activity *activity)
{
    return lxp_activity_verify_signature(activity) == LXP_ERR_BAD_SIGNATURE ?
           LXP_OK : LXP_FATAL_INVARIANT;
}

lxp_result lxp_fuzz_signature_mutate(const lxp_activity *signed_activity)
{
    uint8_t signature[64];
    uint8_t invalid_key[32];
    uint8_t other_domain[32];
    uint8_t preimage[32];
    uint8_t changed_payload[LXP_MAX_PAYLOAD_BYTES];
    lxp_activity mutated;
    size_t byte;
    unsigned int bit;
    lxp_result status;
    if (signed_activity == NULL || signed_activity->authority.length != 32U ||
        signed_activity->signature.length != 64U ||
        signed_activity->payload.length > sizeof(changed_payload) ||
        lxp_activity_verify_signature(signed_activity) != LXP_OK)
        return LXP_ERR_BAD_SIGNATURE;
    status = lxp_activity_signing_preimage(signed_activity, preimage);
    if (status == LXP_OK)
        status = lxp_hash_domain(LXP_DOMAIN_ACTIVITY_ID, preimage,
                                 sizeof(preimage), other_domain);
    if (status == LXP_OK &&
        lxp_ed25519_verify_raw(signed_activity->authority.bytes,
                               signed_activity->signature.bytes,
                               other_domain, sizeof(other_domain)) !=
            LXP_ERR_BAD_SIGNATURE)
        status = LXP_FATAL_INVARIANT;
    for (byte = 0U; status == LXP_OK && byte < sizeof(signature); ++byte) {
        for (bit = 0U; bit < 8U; ++bit) {
            (void)memcpy(signature, signed_activity->signature.bytes,
                         sizeof(signature));
            signature[byte] ^= (uint8_t)(1U << bit);
            mutated = *signed_activity;
            mutated.signature = (lxp_byte_span){ signature,
                                                 sizeof(signature) };
            status = must_reject(&mutated);
            if (status != LXP_OK) break;
        }
    }
    if (status == LXP_OK) {
        static const size_t invalid_lengths[] = { 0U, 1U, 32U, 63U, 65U, 128U };
        size_t i;
        for (i = 0U; i < sizeof(invalid_lengths) /
             sizeof(invalid_lengths[0]); ++i) {
            mutated = *signed_activity;
            mutated.signature = (lxp_byte_span){ signed_activity->signature.bytes,
                                                 invalid_lengths[i] };
            status = must_reject(&mutated);
            if (status != LXP_OK) break;
        }
    }
    if (status == LXP_OK) {
        (void)memset(signature, 0, sizeof(signature));
        mutated = *signed_activity;
        mutated.signature = (lxp_byte_span){ signature, sizeof(signature) };
        status = must_reject(&mutated);
    }
    if (status == LXP_OK) {
        (void)memcpy(signature, signed_activity->signature.bytes,
                     sizeof(signature));
        (void)memset(signature + 32U, 0xff, 32U);
        mutated = *signed_activity;
        mutated.signature = (lxp_byte_span){ signature, sizeof(signature) };
        status = must_reject(&mutated);
    }
    if (status == LXP_OK) {
        (void)memset(invalid_key, 0, sizeof(invalid_key));
        mutated = *signed_activity;
        mutated.authority = (lxp_byte_span){ invalid_key, sizeof(invalid_key) };
        status = must_reject(&mutated);
    }
    if (status == LXP_OK) {
        (void)memset(invalid_key, 0xff, sizeof(invalid_key));
        mutated = *signed_activity;
        mutated.authority = (lxp_byte_span){ invalid_key, sizeof(invalid_key) };
        status = must_reject(&mutated);
    }
    if (status == LXP_OK) {
        mutated = *signed_activity;
        mutated.network_id ^= UINT32_C(0x01000000);
        status = must_reject(&mutated);
    }
    if (status == LXP_OK && signed_activity->payload.length != 0U) {
        (void)memcpy(changed_payload, signed_activity->payload.bytes,
                     signed_activity->payload.length);
        changed_payload[signed_activity->payload.length / 2U] ^= 1U;
        mutated = *signed_activity;
        mutated.payload = (lxp_byte_span){ changed_payload,
                                          signed_activity->payload.length };
        status = must_reject(&mutated);
    }
    lxp_secure_zero(signature, sizeof(signature));
    lxp_secure_zero(preimage, sizeof(preimage));
    lxp_secure_zero(other_domain, sizeof(other_domain));
    lxp_secure_zero(changed_payload, sizeof(changed_payload));
    return status;
}
