#include "layerx/lx_web.h"

#include "layerx/lxp_crypto.h"

#include <openssl/evp.h>
#include <stdlib.h>
#include <string.h>

static void put_u32(uint8_t bytes[4], uint32_t value)
{
    size_t i;
    for (i = 0U; i < 4U; ++i)
        bytes[i] = (uint8_t)(value >> ((3U - i) * 8U));
}

static void put_u64(uint8_t bytes[8], uint64_t value)
{
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bytes[i] = (uint8_t)(value >> ((7U - i) * 8U));
}

lxp_result lx_web_observation_encode(const lx_web_observation *observation,
                                     uint8_t *bytes, size_t capacity,
                                     size_t *length)
{
    size_t required;
    size_t offset;
    size_t i;
    if (observation == NULL || bytes == NULL || length == NULL ||
        observation->origin != LX_WEB_ORIGIN_PROGRAM ||
        observation->network_id == 0U ||
        lxp_ct_is_zero(observation->program_id, 32U) ||
        (observation->kind != LX_WEB_KIND_FETCH &&
         observation->kind != LX_WEB_KIND_SEARCH) ||
        observation->response_length > LX_WEB_MAX_RESPONSE_BYTES ||
        observation->full_length < observation->response_length ||
        observation->signature_count == 0U ||
        observation->signature_count > LX_WEB_MAX_ATTESTORS)
        return LXP_ERR_NON_CANONICAL;
    required = LX_WEB_OBSERVATION_HEADER_BYTES +
               (size_t)observation->response_length + 1U +
               observation->signature_count * LX_WEB_SIGNATURE_BYTES;
    if (capacity < required) return LXP_ERR_LENGTH_LIMIT;
    bytes[0] = observation->origin;
    (void)memset(bytes + 1U, 0, 28U);
    put_u32(bytes + 29U, observation->network_id);
    (void)memcpy(bytes + 33U, observation->program_id, 32U);
    put_u64(bytes + 65U, observation->request_id);
    bytes[73] = observation->kind;
    (void)memcpy(bytes + 74U, observation->payload_hash, 32U);
    (void)memcpy(bytes + 106U, observation->content_digest, 32U);
    put_u32(bytes + 138U, observation->full_length);
    put_u32(bytes + 142U, observation->response_length);
    offset = LX_WEB_OBSERVATION_HEADER_BYTES;
    if (observation->response_length != 0U)
        (void)memcpy(bytes + offset, observation->response,
                     observation->response_length);
    offset += observation->response_length;
    bytes[offset++] = (uint8_t)observation->signature_count;
    for (i = 0U; i < observation->signature_count; ++i) {
        (void)memcpy(bytes + offset, observation->signatures[i],
                     LX_WEB_SIGNATURE_BYTES);
        offset += LX_WEB_SIGNATURE_BYTES;
    }
    *length = offset;
    return LXP_OK;
}

static lxp_result submitter_sign(lxp_activity *activity,
                                 const uint8_t private_key[32],
                                 uint8_t public_key[32],
                                 uint8_t signature[64])
{
    uint8_t preimage[32];
    size_t public_length = 32U;
    size_t signature_length = 64U;
    EVP_PKEY *key;
    EVP_MD_CTX *context;
    lxp_result status = LXP_OK;
    key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                       private_key, 32U);
    context = EVP_MD_CTX_new();
    if (key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, public_key, &public_length) != 1 ||
        public_length != 32U)
        status = LXP_ERR_BAD_SIGNATURE;
    if (status == LXP_OK) {
        activity->authority.bytes = public_key;
        activity->authority.length = 32U;
        status = lxp_activity_signing_preimage(activity, preimage);
    }
    if (status == LXP_OK &&
        (EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
         EVP_DigestSign(context, signature, &signature_length, preimage,
                        sizeof(preimage)) != 1 ||
         signature_length != 64U))
        status = LXP_ERR_BAD_SIGNATURE;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    lxp_secure_zero(preimage, sizeof(preimage));
    return status;
}

lxp_result lx_web_activity_encode(const lx_web_observation *observation,
                                  const uint8_t submitter_private_key[32],
                                  uint32_t network_id,
                                  const uint8_t *actor_did,
                                  size_t actor_did_length,
                                  uint64_t account_sequence,
                                  lxp_u128 fee_limit,
                                  lxp_timestamp_bound timestamp_bound,
                                  lxp_arena *arena, lxp_byte_span *encoded)
{
    lxp_activity activity;
    uint8_t payload[LX_WEB_OBSERVATION_MAX_BYTES];
    uint8_t public_key[32];
    uint8_t signature[64];
    size_t payload_length;
    lxp_result status;
    if (observation == NULL || submitter_private_key == NULL ||
        actor_did == NULL || actor_did_length == 0U ||
        actor_did_length > LXP_MAX_DID_LENGTH || arena == NULL ||
        encoded == NULL || lxp_u128_is_zero(fee_limit) ||
        network_id == 0U || observation->network_id != network_id ||
        timestamp_bound.not_before > timestamp_bound.not_after)
        return LXP_ERR_NON_CANONICAL;
    status = lx_web_observation_encode(observation, payload,
                                       sizeof(payload), &payload_length);
    if (status != LXP_OK) return status;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION;
    activity.network_id = network_id;
    activity.activity_type = LX_WEB_OBSERVATION_ACTIVITY;
    activity.actor_did.bytes = actor_did;
    activity.actor_did.length = actor_did_length;
    activity.account_sequence = account_sequence;
    activity.timestamp_bound = timestamp_bound;
    status = lxp_hash_context_value(payload, payload_length,
                                    activity.idempotency_key);
    if (status == LXP_OK)
        status = lxp_hash_payload(payload, payload_length,
                                  activity.payload_hash);
    if (status != LXP_OK) return status;
    activity.fee_limit = fee_limit;
    activity.payload.bytes = payload;
    activity.payload.length = payload_length;
    status = submitter_sign(&activity, submitter_private_key, public_key,
                            signature);
    if (status != LXP_OK) return status;
    activity.signature.bytes = signature;
    activity.signature.length = 64U;
    return lxp_activity_encode(&activity, arena, encoded);
}

lxp_result lx_web_adapter_run(lx_web_adapter_config *config,
                              size_t *submitted)
{
    size_t count = 0U;
    if (config == NULL || submitted == NULL ||
        config->poll_observations == NULL ||
        config->submit_activity == NULL || config->actor_did == NULL ||
        config->actor_did_length == 0U || config->maximum_observations == 0U)
        return LXP_ERR_NON_CANONICAL;
    while (count < config->maximum_observations) {
        lx_web_observation observation;
        uint8_t arena_bytes[LXP_MAX_ACTIVITY_BYTES];
        lxp_arena arena;
        lxp_byte_span activity;
        bool available = false;
        lxp_result status;
        (void)memset(&observation, 0, sizeof(observation));
        status = config->poll_observations(config->poll_context,
                                           &observation, &available);
        if (status != LXP_OK) return status;
        if (!available) break;
        status = lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes));
        if (status == LXP_OK)
            status = lx_web_activity_encode(
                &observation, config->submitter_private_key,
                config->network_id, config->actor_did,
                config->actor_did_length, config->next_account_sequence,
                config->fee_limit, config->timestamp_bound, &arena,
                &activity);
        if (status == LXP_OK)
            status = config->submit_activity(config->submit_context,
                                              activity.bytes,
                                              activity.length);
        if (status != LXP_OK) return status;
        ++config->next_account_sequence;
        ++count;
    }
    *submitted = count;
    return LXP_OK;
}

lxp_result lx_web_submission_prepare(const lx_web_adapter_config *config,
                                     const lx_web_observation *observation,
                                     uint64_t account_sequence,
                                     lx_web_submission *submission)
{
    uint8_t arena_bytes[LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_byte_span activity;
    lxp_activity decoded;
    lxp_result status;
    if (config == NULL || observation == NULL || submission == NULL ||
        config->actor_did == NULL || config->actor_did_length == 0U)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes));
    if (status == LXP_OK)
        status = lx_web_activity_encode(
            observation, config->submitter_private_key, config->network_id,
            config->actor_did, config->actor_did_length, account_sequence,
            config->fee_limit, config->timestamp_bound, &arena, &activity);
    if (status != LXP_OK) return status;
    if (activity.length > sizeof(submission->activity))
        return LXP_ERR_LENGTH_LIMIT;
    status = lxp_activity_decode(activity.bytes, activity.length, &decoded);
    if (status != LXP_OK) return status;
    (void)memset(submission, 0, sizeof(*submission));
    (void)memcpy(submission->program_id, observation->program_id, 32U);
    submission->request_id = observation->request_id;
    (void)memcpy(submission->payload_hash, observation->payload_hash, 32U);
    submission->account_sequence = account_sequence;
    submission->not_after = decoded.timestamp_bound.not_after;
    (void)memcpy(submission->idempotency_key, decoded.idempotency_key, 32U);
    status = lxp_activity_id(activity.bytes, activity.length,
                             submission->activity_id);
    if (status != LXP_OK) return status;
    (void)memcpy(submission->activity, activity.bytes, activity.length);
    submission->activity_length = activity.length;
    submission->state = LX_WEB_SUBMISSION_SUBMITTING;
    return LXP_OK;
}

lxp_result lx_web_submission_check(const lx_web_submission *submission,
                                   uint32_t network_id)
{
    lxp_activity activity;
    lx_web_observation observation;
    uint8_t identifier[32];
    lxp_result status;
    if (submission == NULL || network_id == 0U ||
        submission->activity_length == 0U ||
        submission->activity_length > sizeof(submission->activity) ||
        submission->state < LX_WEB_SUBMISSION_SUBMITTING ||
        submission->state > LX_WEB_SUBMISSION_REJECTED)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_activity_decode(submission->activity,
                                 submission->activity_length, &activity);
    if (status != LXP_OK) return status;
    if (activity.activity_type != LX_WEB_OBSERVATION_ACTIVITY ||
        activity.network_id != network_id ||
        activity.account_sequence != submission->account_sequence ||
        activity.timestamp_bound.not_after != submission->not_after ||
        memcmp(activity.idempotency_key, submission->idempotency_key,
               32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_activity_verify_payload_hash(&activity);
    if (status != LXP_OK) return status;
    status = lxp_activity_id(submission->activity,
                             submission->activity_length, identifier);
    if (status != LXP_OK) return status;
    if (memcmp(identifier, submission->activity_id, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lx_web_observation_decode(activity.payload.bytes,
                                       activity.payload.length,
                                       &observation);
    if (status != LXP_OK) return status;
    if (observation.network_id != network_id ||
        memcmp(observation.program_id, submission->program_id, 32U) != 0 ||
        observation.request_id != submission->request_id ||
        memcmp(observation.payload_hash, submission->payload_hash, 32U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    return LXP_OK;
}

static lxp_result submission_send(lx_web_adapter_config *config,
                                  lx_web_submission *submission)
{
    lxp_result status = config->submit_activity(
        config->submit_context, submission->activity,
        submission->activity_length);
    if (status != LXP_OK) return status;
    submission->state = LX_WEB_SUBMISSION_UNKNOWN;
    return config->record_submission(config->record_context, submission);
}

static bool durable_config_valid(const lx_web_adapter_config *config)
{
    return config != NULL && config->submit_activity != NULL &&
           config->record_submission != NULL &&
           config->lookup_receipt != NULL && config->network_id != 0U &&
           config->actor_did != NULL && config->actor_did_length != 0U;
}

static lxp_result submission_resign(lx_web_adapter_config *config,
                                    lx_web_submission *submission)
{
    lxp_activity activity;
    lx_web_observation *observation;
    lxp_result status;
    observation = malloc(sizeof(*observation));
    if (observation == NULL) return LXP_ERR_LENGTH_LIMIT;
    status = lxp_activity_decode(submission->activity,
                                 submission->activity_length, &activity);
    if (status == LXP_OK)
        status = lx_web_observation_decode(activity.payload.bytes,
                                           activity.payload.length,
                                           observation);
    if (status == LXP_OK)
        status = lx_web_submission_prepare(config, observation,
                                           config->next_account_sequence,
                                           submission);
    free(observation);
    if (status == LXP_OK)
        status = config->record_submission(config->record_context,
                                           submission);
    if (status != LXP_OK) return status;
    ++config->next_account_sequence;
    return submission_send(config, submission);
}

lxp_result lx_web_submission_recover(lx_web_adapter_config *config,
                                     lx_web_submission *submission,
                                     uint64_t now)
{
    uint8_t outcome = LX_WEB_RECEIPT_PENDING;
    int32_t rejection = 0;
    lxp_result status;
    if (!durable_config_valid(config) || submission == NULL)
        return LXP_ERR_NON_CANONICAL;
    status = lx_web_submission_check(submission, config->network_id);
    if (status != LXP_OK) return status;
    if (submission->state != LX_WEB_SUBMISSION_SUBMITTING &&
        submission->state != LX_WEB_SUBMISSION_UNKNOWN)
        return LXP_OK;
    status = config->lookup_receipt(config->receipt_context,
                                    submission->activity_id,
                                    submission->idempotency_key, &outcome,
                                    &rejection);
    if (status != LXP_OK) return status;
    switch (outcome) {
    case LX_WEB_RECEIPT_PENDING:
        if (submission->state == LX_WEB_SUBMISSION_UNKNOWN) return LXP_OK;
        submission->state = LX_WEB_SUBMISSION_UNKNOWN;
        return config->record_submission(config->record_context,
                                         submission);
    case LX_WEB_RECEIPT_NOT_FOUND:
        if (now <= submission->not_after)
            return submission_send(config, submission);
        return submission_resign(config, submission);
    case LX_WEB_RECEIPT_REJECTED:
        if (rejection == 0) return LXP_ERR_NON_CANONICAL;
        submission->state = LX_WEB_SUBMISSION_REJECTED;
        submission->rejection = rejection;
        return config->record_submission(config->record_context,
                                         submission);
    case LX_WEB_RECEIPT_COMPLETED:
        if (rejection != 0) return LXP_ERR_NON_CANONICAL;
        submission->state = LX_WEB_SUBMISSION_COMPLETED;
        return config->record_submission(config->record_context,
                                         submission);
    default:
        return LXP_ERR_NON_CANONICAL;
    }
}

lxp_result lx_web_adapter_run_durable(lx_web_adapter_config *config,
                                      size_t *submitted)
{
    size_t count = 0U;
    if (!durable_config_valid(config) || submitted == NULL ||
        config->poll_observations == NULL || config->actor_did == NULL ||
        config->actor_did_length == 0U || config->maximum_observations == 0U)
        return LXP_ERR_NON_CANONICAL;
    while (count < config->maximum_observations) {
        lx_web_observation observation;
        lx_web_submission *submission;
        bool available = false;
        lxp_result status;
        (void)memset(&observation, 0, sizeof(observation));
        status = config->poll_observations(config->poll_context,
                                           &observation, &available);
        if (status != LXP_OK) return status;
        if (!available) break;
        submission = malloc(sizeof(*submission));
        if (submission == NULL) return LXP_ERR_LENGTH_LIMIT;
        status = lx_web_submission_prepare(config, &observation,
                                           config->next_account_sequence,
                                           submission);
        if (status == LXP_OK)
            status = config->record_submission(config->record_context,
                                               submission);
        if (status == LXP_OK) ++config->next_account_sequence;
        if (status == LXP_OK) status = submission_send(config, submission);
        free(submission);
        if (status != LXP_OK) return status;
        ++count;
    }
    *submitted = count;
    return LXP_OK;
}
