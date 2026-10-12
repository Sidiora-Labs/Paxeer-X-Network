#ifndef LAYERX_TESTS_MODULES_TEST_SERVICE_HELPERS_H
#define LAYERX_TESTS_MODULES_TEST_SERVICE_HELPERS_H

#include "layerx/lx_service.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_protocol.h"

#include <openssl/evp.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

enum { WORK_ARENA_BYTES = 16384, GAS_LIMIT = 100000 };

static lxp_state_store store_state;
static lxp_state_journal state_journal;
static lxp_kernel service_kernel;
static lxp_effect_buffer event_buffer;
static lxp_arena work_arena;
static uint8_t work_bytes[WORK_ARENA_BYTES];
static uint8_t activity_bytes[LXP_MAX_ACTIVITY_BYTES];
static uint64_t parameter_set = 1U;

/* Optional per-test audit of the effects a dispatch produced, run after the
 * module executed and before the context is committed or rolled back. */
static int (*effect_audit_hook)(void);

static inline void be16(uint8_t *bytes, uint16_t value)
{
    bytes[0] = (uint8_t)(value >> 8);
    bytes[1] = (uint8_t)value;
}

static inline void be32(uint8_t *bytes, uint32_t value)
{
    bytes[0] = (uint8_t)(value >> 24);
    bytes[1] = (uint8_t)(value >> 16);
    bytes[2] = (uint8_t)(value >> 8);
    bytes[3] = (uint8_t)value;
}

static inline void be64(uint8_t *bytes, uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bytes[i] = (uint8_t)(value >> ((7U - i) * 8U));
}

static inline void id32(uint8_t out[32], uint8_t marker)
{
    (void)memset(out, 0, 32U);
    out[0] = marker;
}

static inline size_t identifier_payload(uint8_t *out,
                                        const uint8_t identifier[32])
{
    out[0] = 0U;
    out[1] = (uint8_t)LX_SERVICE_RECORD_VERSION;
    (void)memcpy(out + LX_SERVICE_PAYLOAD_VERSION_BYTES, identifier, 32U);
    return (size_t)LX_SERVICE_IDENTIFIER_PAYLOAD_BYTES;
}

static inline int open_ctx(lxp_module_ctx *ctx, uint64_t timestamp,
                           uint64_t sequence)
{
    if (lxp_arena_init(&work_arena, work_bytes, sizeof(work_bytes)) !=
            LXP_OK ||
        lxp_effect_buffer_init(&event_buffer) != LXP_OK ||
        lxp_module_ctx_init(ctx, &service_kernel, LXP_MODULE_SERVICE,
                            timestamp, 0U, sequence, (uint64_t)GAS_LIMIT,
                            &work_arena, true) != LXP_OK)
        return 1;
    return lxp_module_ctx_bind_effects(ctx, &event_buffer) == LXP_OK ? 0 : 1;
}

static inline lxp_result dispatch_activity(
    const lxp_activity *activity, const lxp_authority_resolved *authority,
    uint64_t timestamp, uint64_t sequence, lxp_result *module_result)
{
    const lxp_module_registration *registration = NULL;
    lxp_arena encoding_arena;
    lxp_byte_span encoded;
    lxp_module_ctx ctx;
    lxp_result status;

    *module_result = LXP_FATAL_INVARIANT;
    status = lxp_kernel_module_for_activity(&service_kernel,
                                            activity->activity_type,
                                            0U, &registration);
    if (status != LXP_OK) return status;
    if (open_ctx(&ctx, timestamp, sequence) != 0) return LXP_FATAL_INVARIANT;
    ctx.protocol_version = activity->protocol_version;
    status = lxp_arena_init(&encoding_arena, activity_bytes,
                            sizeof(activity_bytes));
    if (status == LXP_OK)
        status = lxp_activity_encode(activity, &encoding_arena, &encoded);
    if (status == LXP_OK)
        status = lxp_activity_id(encoded.bytes, encoded.length,
                                 ctx.activity_id);
    if (status != LXP_OK) return status;
    status = lxp_kernel_dispatch(registration, &ctx, activity, authority,
                                 &event_buffer, module_result);
    if (status != LXP_OK) return status;
    if (lxp_ctx_idempotency_key(&ctx) != activity->idempotency_key)
        return LXP_FATAL_INVARIANT;
    if (effect_audit_hook != NULL && effect_audit_hook() != 0)
        return LXP_FATAL_INVARIANT;
    if (*module_result == LXP_OK) return lxp_module_ctx_commit(&ctx);
    lxp_module_ctx_rollback(&ctx);
    return LXP_OK;
}

static inline lxp_result dispatch(uint32_t activity_type,
                                  const uint8_t *payload, size_t length,
                                  const lxp_authority_resolved *authority,
                                  uint64_t timestamp, uint64_t sequence,
                                  uint8_t activity_marker,
                                  lxp_result *module_result)
{
    lxp_activity activity;
    lxp_result status;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION;
    activity.network_id = 7U;
    activity.activity_type = activity_type;
    activity.payload.bytes = payload;
    activity.payload.length = length;
    id32(activity.idempotency_key, activity_marker);
    status = lxp_hash_payload(payload, length, activity.payload_hash);
    if (status != LXP_OK) return status;
    return dispatch_activity(&activity, authority, timestamp, sequence,
                              module_result);
}

static inline lxp_result dispatch_signed_attest(
    const uint8_t *payload, size_t length,
    const lxp_authority_resolved *authority, const uint8_t seed[32],
    const uint8_t idempotency_key[32], uint64_t timestamp,
    uint64_t sequence, lxp_result *module_result)
{
    static const char hex[] = "0123456789abcdef";
    uint8_t did[sizeof("did:layerx:") - 1U + 64U];
    uint8_t public_key[32];
    uint8_t signature[64];
    uint8_t preimage[32];
    size_t public_length = sizeof(public_key);
    size_t signature_length = sizeof(signature);
    size_t index;
    lxp_activity activity;
    lxp_result status = LXP_ERR_BAD_SIGNATURE;
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                                 seed, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();

    *module_result = LXP_FATAL_INVARIANT;
    if (key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, public_key, &public_length) != 1 ||
        public_length != sizeof(public_key))
        goto finish;
    (void)memcpy(did, "did:layerx:", sizeof("did:layerx:") - 1U);
    for (index = 0U; index < sizeof(public_key); ++index) {
        did[sizeof("did:layerx:") - 1U + index * 2U] =
            (uint8_t)hex[public_key[index] >> 4U];
        did[sizeof("did:layerx:") + index * 2U] =
            (uint8_t)hex[public_key[index] & 15U];
    }
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION;
    activity.network_id = 7U;
    activity.activity_type = LX_SERVICE_TOOL_EXEC_ATTEST;
    activity.actor_did = (lxp_byte_span){did, sizeof(did)};
    activity.authority = (lxp_byte_span){public_key, sizeof(public_key)};
    activity.account_sequence = sequence;
    activity.timestamp_bound.not_before = timestamp;
    activity.timestamp_bound.not_after = timestamp;
    (void)memcpy(activity.idempotency_key, idempotency_key, 32U);
    activity.fee_limit = (lxp_u128){0U, (uint64_t)GAS_LIMIT};
    activity.payload = (lxp_byte_span){payload, length};
    status = lxp_hash_payload(payload, length, activity.payload_hash);
    if (status == LXP_OK)
        status = lxp_activity_signing_preimage(&activity, preimage);
    if (status != LXP_OK) goto finish;
    if (EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
        EVP_DigestSign(context, signature, &signature_length, preimage,
                       sizeof(preimage)) != 1 ||
        signature_length != sizeof(signature)) {
        status = LXP_ERR_BAD_SIGNATURE;
        goto finish;
    }
    activity.signature = (lxp_byte_span){signature, sizeof(signature)};
    status = lxp_activity_check_envelope(&activity, activity.network_id);
    if (status == LXP_OK) status = lxp_activity_verify_signature(&activity);
    if (status == LXP_OK)
        status = dispatch_activity(&activity, authority, timestamp,
                                    sequence, module_result);
finish:
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return status;
}

static inline int event_is(uint16_t event_type, const uint8_t primary[32],
                           const uint8_t secondary[32], uint8_t code,
                           uint64_t sequence)
{
    uint8_t body[LX_SERVICE_EVENT_BODY_BYTES];
    const lxp_effect *effect = &event_buffer.effects[0];
    if (event_buffer.count != 1U || effect->kind != LXP_EFFECT_EVENT ||
        effect->monetary || effect->module_id != LXP_MODULE_SERVICE ||
        effect->event_type != event_type ||
        effect->body_length != (uint16_t)LX_SERVICE_EVENT_BODY_BYTES)
        return 1;
    (void)memcpy(body, primary, 32U);
    (void)memcpy(body + 32U, secondary, 32U);
    body[64] = code;
    be64(body + 65U, sequence);
    return memcmp(effect->body, body, sizeof(body)) == 0 ? 0 : 1;
}

static inline int record_present(
    const uint8_t prefix[LX_SERVICE_KEY_PREFIX_BYTES],
    const uint8_t identifier[32], size_t expected)
{
    uint8_t key[LX_SERVICE_KEY_BYTES];
    const uint8_t *value = NULL;
    size_t length = 0U;
    lxp_module_ctx ctx;
    if (open_ctx(&ctx, 1U, 1U) != 0) return 1;
    (void)memcpy(key, prefix, (size_t)LX_SERVICE_KEY_PREFIX_BYTES);
    (void)memcpy(key + LX_SERVICE_KEY_PREFIX_BYTES, identifier, 32U);
    if (lxp_ctx_kv_get(&ctx, key, sizeof(key), &value, &length) != LXP_OK)
        return 1;
    return length == expected ? 0 : 1;
}

#endif
