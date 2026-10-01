#define _POSIX_C_SOURCE 200809L

#include "layerx/lx_stream.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"
#include "layerx/lxp_state.h"

#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define CHECK(x) do { \
    if (!(x)) { (void)fprintf(stderr, "line %d\n", __LINE__); return 1; } \
} while (0)

static int sign_attestation(lx_stream_meter_attestation *attestation,
                            const uint8_t seed[32])
{
    uint8_t message[128];
    uint8_t digest[32];
    size_t message_length;
    size_t public_length = 32U;
    size_t signature_length = 64U;
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL,
                                                  seed, 32U);
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    int failed = key == NULL || context == NULL ||
        EVP_PKEY_get_raw_public_key(key, attestation->authority_key,
                                    &public_length) != 1 ||
        lx_stream_meter_attestation_bytes(attestation, message,
                                          sizeof(message),
                                          &message_length) != LXP_OK ||
        lxp_hash_domain(LXP_DOMAIN_SIGNATURE_PREIMAGE, message,
                        message_length, digest) != LXP_OK ||
        EVP_DigestSignInit(context, NULL, NULL, NULL, key) != 1 ||
        EVP_DigestSign(context, attestation->signature, &signature_length,
                       digest, sizeof(digest)) != 1 || signature_length != 64U;
    EVP_MD_CTX_free(context);
    EVP_PKEY_free(key);
    return failed;
}

static void metered_record(lx_stream_record *record, lxp_u128 cap)
{
    (void)memset(record, 0, sizeof(*record));
    record->stream_id[0] = 3U;
    record->payer[0] = 1U;
    record->stream_account[0] = 2U;
    record->recipient[0] = 4U;
    record->asset_id[0] = 5U;
    record->mode = LX_STREAM_MODE_METERED;
    record->rate = (lxp_u128){ 0U, 3U };
    record->rate_unit = 2U;
    record->start_timestamp = 100U;
    record->last_accrual_timestamp = 100U;
    record->total_cap = cap;
    record->meter_authorities[0][0] = 7U;
    record->meter_authority_count = 1U;
}

static int attested_meter(void)
{
    static const uint8_t seed[32] = { 1U };
    static const uint8_t other_seed[32] = { 2U };
    lx_stream_record record;
    lx_stream_meter_attestation attestation;
    lxp_u128 accrued;
    uint8_t authorized[32];

    (void)memset(&record, 0, sizeof(record));
    (void)memset(&attestation, 0, sizeof(attestation));
    record.stream_id[0] = 3U;
    record.mode = LX_STREAM_MODE_METERED;
    record.rate = (lxp_u128){ 0U, 3U };
    record.rate_unit = 2U;
    record.total_cap = (lxp_u128){ 0U, 1000U };
    (void)memcpy(attestation.stream_id, record.stream_id, 32U);
    attestation.cumulative_reading = 3U;
    CHECK(sign_attestation(&attestation, seed) == 0);
    (void)memcpy(authorized, attestation.authority_key, 32U);
    (void)memcpy(record.meter_authorities[0], authorized, 32U);
    record.meter_authority_count = 1U;
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) == LXP_OK);
    CHECK(accrued.lo == 4U && record.remainder_carry.lo == 1U &&
          record.cumulative_meter == 3U);
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) == LXP_OK);
    CHECK(lxp_u128_is_zero(accrued) && record.accrued_total.lo == 4U);

    attestation.cumulative_reading = 2U;
    CHECK(sign_attestation(&attestation, seed) == 0);
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_METER_REGRESSION);
    CHECK(record.accrued_total.lo == 4U && record.cumulative_meter == 3U);

    attestation.cumulative_reading = 4U;
    CHECK(sign_attestation(&attestation, other_seed) == 0);
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_UNAUTHORIZED_METER);
    CHECK(record.accrued_total.lo == 4U);

    /* An authorized key with a mutilated signature is still refused. */
    CHECK(sign_attestation(&attestation, seed) == 0);
    attestation.signature[0] = (uint8_t)(attestation.signature[0] ^ 1U);
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_UNAUTHORIZED_METER);
    CHECK(record.cumulative_meter == 3U);

    /* A signature bound to one stream cannot be replayed onto another. */
    CHECK(sign_attestation(&attestation, seed) == 0);
    attestation.stream_id[1] = 9U;
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_UNAUTHORIZED_METER);
    (void)memcpy(attestation.stream_id, record.stream_id, 32U);

    (void)memcpy(attestation.authority_key, authorized, 32U);
    record.rate = (lxp_u128){ UINT64_MAX, UINT64_MAX };
    record.rate_unit = 1U;
    attestation.cumulative_reading = UINT64_MAX;
    CHECK(sign_attestation(&attestation, seed) == 0);
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_ACCRUAL_OVERFLOW);
    CHECK(record.cumulative_meter == 3U);
    record.meter_authority_count = LX_STREAM_MAX_METER_AUTHORITIES + 1U;
    CHECK(lx_stream_meter_execute(&record, &attestation, &accrued) ==
          LXP_ERR_UNAUTHORIZED_METER);
    CHECK(lx_stream_meter_authority_check(NULL, &attestation) ==
          LXP_ERR_UNAUTHORIZED_METER);
    CHECK(lx_stream_meter_authority_check(&record, NULL) ==
          LXP_ERR_UNAUTHORIZED_METER);
    return 0;
}

static int metered_boundaries(void)
{
    lx_stream_record record;
    lxp_u128 accrued;

    metered_record(&record, (lxp_u128){ 0U, 5U });
    CHECK(lx_stream_metered_accrue(&record, 4U, &accrued) == LXP_OK);
    CHECK(accrued.lo == 5U && record.accrued_total.lo == 5U &&
          record.cumulative_meter == 4U &&
          lxp_u128_is_zero(record.remainder_carry));
    /* At the cap the reading is not consumed, so no usage is lost. */
    CHECK(lx_stream_metered_accrue(&record, 8U, &accrued) == LXP_OK);
    CHECK(lxp_u128_is_zero(accrued) && record.cumulative_meter == 4U &&
          record.accrued_total.lo == 5U);

    metered_record(&record, (lxp_u128){ 0U, 100U });
    record.paused = true;
    CHECK(lx_stream_metered_accrue(&record, 6U, &accrued) == LXP_OK);
    CHECK(lxp_u128_is_zero(accrued) && record.cumulative_meter == 6U &&
          lxp_u128_is_zero(record.accrued_total));
    record.paused = false;
    record.underfunded = true;
    CHECK(lx_stream_metered_accrue(&record, 10U, &accrued) == LXP_OK);
    CHECK(lxp_u128_is_zero(accrued) && record.cumulative_meter == 10U &&
          lxp_u128_is_zero(record.accrued_total));
    record.underfunded = false;
    CHECK(lx_stream_metered_accrue(&record, 12U, &accrued) == LXP_OK);
    CHECK(accrued.lo == 3U && record.accrued_total.lo == 3U &&
          record.cumulative_meter == 12U);

    metered_record(&record, (lxp_u128){ 0U, 100U });
    record.closed = true;
    CHECK(lx_stream_metered_accrue(&record, 6U, &accrued) == LXP_OK);
    CHECK(lxp_u128_is_zero(accrued) && record.cumulative_meter == 0U);

    metered_record(&record, (lxp_u128){ 0U, 100U });
    record.mode = LX_STREAM_MODE_TIME;
    CHECK(lx_stream_metered_accrue(&record, 6U, &accrued) ==
          LXP_ERR_NON_CANONICAL);
    metered_record(&record, (lxp_u128){ 0U, 100U });
    record.rate_unit = 0U;
    CHECK(lx_stream_metered_accrue(&record, 6U, &accrued) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_metered_accrue(NULL, 6U, &accrued) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_metered_accrue(&record, 6U, NULL) ==
          LXP_ERR_NON_CANONICAL);
    return 0;
}

static int attestation_preimage(void)
{
    static const uint8_t tag[] = "LXP:STREAM:METER:v1";
    lx_stream_meter_attestation attestation;
    uint8_t message[128];
    size_t length = 0U;
    size_t i;

    (void)memset(&attestation, 0, sizeof(attestation));
    attestation.stream_id[0] = 3U;
    attestation.authority_key[0] = 7U;
    attestation.cumulative_reading = 0x0102030405060708ULL;
    CHECK(lx_stream_meter_attestation_bytes(&attestation, message,
                                            sizeof(message), &length) ==
          LXP_OK);
    CHECK(length == sizeof(tag) - 1U + 72U);
    CHECK(memcmp(message, tag, sizeof(tag) - 1U) == 0);
    CHECK(memcmp(message + sizeof(tag) - 1U, attestation.stream_id, 32U) == 0);
    for (i = 0U; i < 8U; ++i)
        CHECK(message[sizeof(tag) - 1U + 32U + i] == (uint8_t)(i + 1U));
    CHECK(memcmp(message + sizeof(tag) - 1U + 40U,
                 attestation.authority_key, 32U) == 0);
    CHECK(lx_stream_meter_attestation_bytes(&attestation, message,
                                            length - 1U, &length) ==
          LXP_ERR_LENGTH_LIMIT);
    return 0;
}

static int meter_payload(void)
{
    lx_stream_meter_attestation attestation;
    lx_stream_meter_attestation decoded;
    uint8_t bytes[LX_STREAM_METER_PAYLOAD_BYTES];
    size_t length = 0U;
    size_t i;

    (void)memset(&attestation, 0, sizeof(attestation));
    attestation.stream_id[0] = 3U;
    attestation.authority_key[0] = 7U;
    attestation.cumulative_reading = 0x0102030405060708ULL;
    for (i = 0U; i < 64U; ++i) attestation.signature[i] = (uint8_t)(i + 1U);
    CHECK(lx_stream_meter_encode(&attestation, bytes, sizeof(bytes),
                                 &length) == LXP_OK);
    CHECK(length == (size_t)LX_STREAM_METER_PAYLOAD_BYTES);
    CHECK(bytes[0] == 0U && bytes[1] == (uint8_t)LX_STREAM_PAYLOAD_VERSION);
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) == LXP_OK);
    CHECK(memcmp(&attestation, &decoded, sizeof(attestation)) == 0);
    CHECK(lx_stream_meter_encode(&attestation, bytes, length - 1U,
                                 &length) == LXP_ERR_LENGTH_LIMIT);

    CHECK(lx_stream_meter_encode(&attestation, bytes, sizeof(bytes),
                                 &length) == LXP_OK);
    CHECK(lx_stream_meter_decode(bytes, length - 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_meter_decode(bytes, length + 1U, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    bytes[1] = (uint8_t)(LX_STREAM_PAYLOAD_VERSION + 1U);
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) ==
          LXP_ERR_VERSION_UNSUPPORTED);
    bytes[1] = (uint8_t)LX_STREAM_PAYLOAD_VERSION;
    bytes[0] = 1U;
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) ==
          LXP_ERR_VERSION_UNSUPPORTED);
    bytes[0] = 0U;
    (void)memset(bytes + 2U, 0, 32U);
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_meter_encode(&attestation, bytes, sizeof(bytes),
                                 &length) == LXP_OK);
    (void)memset(bytes + 42U, 0, 32U);
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_meter_encode(&attestation, bytes, sizeof(bytes),
                                 &length) == LXP_OK);
    (void)memset(bytes + 74U, 0, 64U);
    CHECK(lx_stream_meter_decode(bytes, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_meter_decode(NULL, length, &decoded) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_stream_meter_encode(&attestation, bytes, sizeof(bytes), NULL) ==
          LXP_ERR_NON_CANONICAL);
    return 0;
}

/* Routed METER, SETTLE, PAUSE and CLOSE activities through the kernel on a
 * primary, a replica fed the same activities, and a node restored from a
 * durable snapshot store. The batch timestamp is the only clock. */
typedef struct meter_node {
    lx_account_registry accounts;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_arena arena;
    uint8_t arena_bytes[131072];
} meter_node;

enum {
    STREAM_S = 0x31,
    STREAM_W = 0x32,
    STREAM_P = 0x33,
    STREAM_O = 0x34
};

static const char payer_name[] = "agent:did:key:payer:main";
static const char provider_name[] = "agent:did:key:provider:main";
static const char stream_s_name[] = "agent:did:key:payer:stream:s1";
static const char stream_w_name[] = "agent:did:key:payer:stream:s2";
static const char stream_p_name[] = "agent:did:key:payer:stream:s3";
static const char stream_o_name[] = "agent:did:key:payer:stream:s4";
static const uint8_t meter_seed[32] = { 21U };
static const uint8_t stranger_seed[32] = { 22U };
static meter_node primary;
static meter_node replica;
static meter_node restored;
static lx_asset_record meter_asset;
static lxp_transfer_asset_state meter_asset_state;
static lx_stream_runtime meter_runtime;
static uint8_t meter_key[32];
static uint64_t meter_parameters = 1U;
static uint64_t next_sequence = 1U;
static uint64_t snapshot_sequence;

static int name_id(const char *name, uint8_t id[32])
{
    return lx_account_id_from_string((const uint8_t *)name, strlen(name),
                                     id) == LXP_OK ? 0 : 1;
}

static lx_account *node_account(meter_node *node, const char *name)
{
    uint8_t id[32];
    lx_account *account = NULL;
    if (name_id(name, id) != 0 ||
        lx_account_lookup(node->kernel.state->accounts,
                          (const uint8_t *)name, strlen(name), id,
                          &account) != LXP_OK)
        return NULL;
    return account;
}

static uint64_t node_balance(meter_node *node, const char *name)
{
    lx_account *account = node_account(node, name);
    return account == NULL ? UINT64_MAX : account->balance.lo;
}

static int node_kernel(meter_node *node)
{
    CHECK(lxp_kernel_create(&node->kernel, &node->state, &node->journal,
                            &meter_parameters, 0U) == LXP_OK);
    CHECK(lxp_kernel_register_module(&node->kernel,
                                     lx_stream_module_iface()) == LXP_OK);
    CHECK(lxp_kernel_set_capabilities(&node->kernel, NULL,
                                      lxp_kernel_canonical_ledger_apply) ==
          LXP_OK);
    CHECK(lxp_kernel_bind_module_runtime(&node->kernel, LXP_MODULE_STREAM,
                                         &meter_runtime) == LXP_OK);
    return 0;
}

static int node_open_account(meter_node *node, const char *name)
{
    uint8_t id[32];
    lx_account *account;
    CHECK(name_id(name, id) == 0);
    CHECK(lx_account_open(&node->accounts, (const uint8_t *)name,
                          strlen(name), id, 1U, LX_ACCOUNT_OPEN_CREDIT, NULL,
                          &account) == LXP_OK);
    return 0;
}

static int node_genesis(meter_node *node)
{
    lx_account *payer;
    CHECK(lx_account_registry_init(&node->accounts) == LXP_OK);
    if (node_open_account(node, payer_name) != 0 ||
        node_open_account(node, provider_name) != 0 ||
        node_open_account(node, stream_s_name) != 0 ||
        node_open_account(node, stream_w_name) != 0 ||
        node_open_account(node, stream_p_name) != 0 ||
        node_open_account(node, stream_o_name) != 0)
        return 1;
    CHECK(lxp_state_store_init(&node->state, 1U) == LXP_OK);
    CHECK(lxp_state_store_bind_accounts(&node->state, &node->accounts) ==
          LXP_OK);
    CHECK(lxp_state_store_require_account_root(&node->state) == LXP_OK);
    if (node_kernel(node) != 0) return 1;
    payer = node_account(node, payer_name);
    CHECK(payer != NULL);
    CHECK(lxp_ledger_bootstrap_balance(payer, meter_asset.asset_id,
                                       (lxp_u128){ 0U, 100U }, 0U) == LXP_OK);
    CHECK(node_account(node, stream_s_name)->kind ==
          LX_ACCOUNT_AGENT_STREAM);
    return 0;
}

static lxp_result node_run(meter_node *node, uint32_t activity_type,
                           const uint8_t *payload, size_t length,
                           const char *principal, uint64_t timestamp,
                           uint64_t sequence, lxp_result *module_result)
{
    const lxp_module_registration *registration;
    lxp_activity activity;
    lxp_authority_resolved authority;
    lxp_result status;
    status = lxp_kernel_module_for_activity(&node->kernel, activity_type, 0U,
                                            &registration);
    if (status != LXP_OK) return status;
    (void)memset(&activity, 0, sizeof(activity));
    (void)memset(&authority, 0, sizeof(authority));
    activity.activity_type = activity_type;
    activity.payload.bytes = payload;
    activity.payload.length = length;
    authority.kind = LXP_AUTHORITY_OWNER;
    if (name_id(principal, authority.principal) != 0)
        return LXP_ERR_NON_CANONICAL;
    status = lxp_arena_init(&node->arena, node->arena_bytes,
                            sizeof(node->arena_bytes));
    if (status == LXP_OK) status = lxp_effect_buffer_init(&node->effects);
    if (status == LXP_OK)
        status = lxp_module_ctx_init(&node->ctx, &node->kernel,
                                     LXP_MODULE_STREAM, timestamp, 0U,
                                     sequence, 1000000U, &node->arena, true);
    if (status == LXP_OK)
        status = lxp_module_ctx_bind_effects(&node->ctx, &node->effects);
    if (status != LXP_OK) return status;
    *module_result = LXP_OK;
    status = lxp_kernel_dispatch(registration, &node->ctx, &activity,
                                 &authority, &node->effects, module_result);
    if (status == LXP_OK && *module_result == LXP_OK)
        status = lxp_module_ctx_commit(&node->ctx);
    return status;
}

static int node_record(meter_node *node, uint8_t marker,
                       lx_stream_record *record,
                       uint8_t bytes[LX_STREAM_RECORD_BYTES])
{
    uint8_t id[32] = { 0U };
    id[0] = marker;
    CHECK(lxp_arena_init(&node->arena, node->arena_bytes,
                         sizeof(node->arena_bytes)) == LXP_OK);
    CHECK(lxp_module_ctx_init(&node->ctx, &node->kernel, LXP_MODULE_STREAM,
                              0U, 0U, 0U, 1000000U, &node->arena, false) ==
          LXP_OK);
    CHECK(lx_stream_load(&node->ctx, id, record) == LXP_OK);
    CHECK(lx_stream_record_encode(record, bytes) == LXP_OK);
    return 0;
}

/* Runs one activity on every node and requires the same module result. */
static int step(meter_node *const *nodes, size_t count,
                uint32_t activity_type, const uint8_t *payload,
                size_t length, const char *principal, uint64_t timestamp,
                lxp_result expected)
{
    lxp_result module_result;
    size_t i;
    uint64_t sequence = next_sequence++;
    for (i = 0U; i < count; ++i) {
        module_result = LXP_OK;
        CHECK(node_run(nodes[i], activity_type, payload, length, principal,
                       timestamp, sequence, &module_result) == LXP_OK);
        if (module_result != expected)
            (void)fprintf(stderr, "node %zu activity %08x at %llu: %s\n", i,
                          (unsigned)activity_type,
                          (unsigned long long)timestamp,
                          lxp_result_name(module_result));
        CHECK(module_result == expected);
    }
    return 0;
}

static size_t open_payload(uint8_t marker, const char *custody,
                           lxp_u128 rate, uint64_t end, lxp_u128 cap,
                           uint64_t funding, uint8_t *bytes, size_t capacity)
{
    lx_stream_open_payload payload;
    size_t length = 0U;
    (void)memset(&payload, 0, sizeof(payload));
    payload.record.stream_id[0] = marker;
    if (name_id(custody, payload.record.stream_account) != 0 ||
        name_id(provider_name, payload.record.recipient) != 0)
        return 0U;
    (void)memcpy(payload.record.asset_id, meter_asset.asset_id, 32U);
    payload.record.mode = LX_STREAM_MODE_METERED;
    payload.record.rate = rate;
    payload.record.rate_unit = 1U;
    payload.record.start_timestamp = 1000U;
    payload.record.end_timestamp = end;
    payload.record.total_cap = cap;
    (void)memcpy(payload.record.meter_authorities[0], meter_key, 32U);
    payload.record.meter_authority_count = 1U;
    payload.initial_funding = (lxp_u128){ 0U, funding };
    return lx_stream_open_encode(&payload, bytes, capacity, &length) ==
        LXP_OK ? length : 0U;
}

static size_t meter_reading(uint8_t marker, uint64_t reading,
                            const uint8_t seed[32],
                            uint8_t bytes[LX_STREAM_METER_PAYLOAD_BYTES])
{
    lx_stream_meter_attestation attestation;
    size_t length = 0U;
    (void)memset(&attestation, 0, sizeof(attestation));
    attestation.stream_id[0] = marker;
    attestation.cumulative_reading = reading;
    if (sign_attestation(&attestation, seed) != 0 ||
        lx_stream_meter_encode(&attestation, bytes,
                               LX_STREAM_METER_PAYLOAD_BYTES,
                               &length) != LXP_OK)
        return 0U;
    return length;
}

static size_t keyed(uint8_t marker, uint8_t key,
                    uint8_t bytes[LX_STREAM_KEYED_PAYLOAD_BYTES])
{
    lx_stream_keyed_payload payload;
    size_t length = 0U;
    (void)memset(&payload, 0, sizeof(payload));
    payload.stream_id[0] = marker;
    payload.idempotency_key[0] = key;
    return lx_stream_keyed_encode(&payload, bytes,
                                  LX_STREAM_KEYED_PAYLOAD_BYTES, &length) ==
        LXP_OK ? length : 0U;
}

static int meter_step(meter_node *const *nodes, size_t count, uint8_t marker,
                      uint64_t reading, const uint8_t seed[32],
                      uint64_t timestamp, lxp_result expected)
{
    uint8_t bytes[LX_STREAM_METER_PAYLOAD_BYTES];
    size_t length = meter_reading(marker, reading, seed, bytes);
    CHECK(length == (size_t)LX_STREAM_METER_PAYLOAD_BYTES);
    return step(nodes, count, LX_STREAM_METER, bytes, length, payer_name,
                timestamp, expected);
}

static int keyed_step(meter_node *const *nodes, size_t count,
                      uint32_t activity_type, uint8_t marker, uint8_t key,
                      const char *principal, uint64_t timestamp,
                      lxp_result expected)
{
    uint8_t bytes[LX_STREAM_KEYED_PAYLOAD_BYTES];
    size_t length = keyed(marker, key, bytes);
    CHECK(length == (size_t)LX_STREAM_KEYED_PAYLOAD_BYTES);
    return step(nodes, count, activity_type, bytes, length, principal,
                timestamp, expected);
}

/* A refused METER leaves the record byte for byte and every balance as it
 * was on every node. */
static int refused_meter(meter_node *const *nodes, size_t count,
                         uint8_t marker, uint64_t reading,
                         const uint8_t seed[32], uint64_t timestamp,
                         lxp_result expected, const char *custody)
{
    lx_stream_record record;
    uint8_t before[3][LX_STREAM_RECORD_BYTES];
    uint8_t after[LX_STREAM_RECORD_BYTES];
    uint64_t balances[3][3];
    size_t i;
    CHECK(count <= 3U);
    for (i = 0U; i < count; ++i) {
        if (node_record(nodes[i], marker, &record, before[i]) != 0) return 1;
        balances[i][0] = node_balance(nodes[i], payer_name);
        balances[i][1] = node_balance(nodes[i], provider_name);
        balances[i][2] = node_balance(nodes[i], custody);
    }
    if (meter_step(nodes, count, marker, reading, seed, timestamp,
                   expected) != 0)
        return 1;
    for (i = 0U; i < count; ++i) {
        CHECK(nodes[i]->effects.count == 0U);
        if (node_record(nodes[i], marker, &record, after) != 0) return 1;
        CHECK(memcmp(before[i], after, sizeof(after)) == 0);
        CHECK(balances[i][0] == node_balance(nodes[i], payer_name));
        CHECK(balances[i][1] == node_balance(nodes[i], provider_name));
        CHECK(balances[i][2] == node_balance(nodes[i], custody));
    }
    return 0;
}

static int effect_amount(const meter_node *node, size_t offset,
                         uint64_t expected)
{
    uint8_t amount[16];
    CHECK(node->effects.count >= 1U);
    CHECK(lxp_u128_to_be((lxp_u128){ 0U, expected }, amount) == LXP_OK);
    CHECK(memcmp(node->effects.effects[node->effects.count - 1U].body +
                 offset, amount, 16U) == 0);
    return 0;
}

static int expect_stream(meter_node *const *nodes, size_t count,
                         uint8_t marker, uint64_t meter, uint64_t accrued,
                         uint64_t settled)
{
    lx_stream_record record;
    uint8_t bytes[LX_STREAM_RECORD_BYTES];
    size_t i;
    for (i = 0U; i < count; ++i) {
        if (node_record(nodes[i], marker, &record, bytes) != 0) return 1;
        CHECK(record.cumulative_meter == meter);
        CHECK(record.accrued_total.hi == 0U &&
              record.accrued_total.lo == accrued);
        CHECK(record.settled_total.hi == 0U &&
              record.settled_total.lo == settled);
    }
    return 0;
}

static int dispatch_fixture(void)
{
    lx_stream_meter_attestation probe;
    meter_node *nodes[2];
    uint8_t bytes[LX_STREAM_OPEN_PAYLOAD_MAX];
    size_t length;
    const lxp_u128 max = { UINT64_MAX, UINT64_MAX };

    (void)memset(&probe, 0, sizeof(probe));
    probe.stream_id[0] = STREAM_S;
    CHECK(sign_attestation(&probe, meter_seed) == 0);
    (void)memcpy(meter_key, probe.authority_key, 32U);
    (void)memset(&meter_asset, 0, sizeof(meter_asset));
    meter_asset.asset_id[0] = 6U;
    CHECK(lx_asset_transfer_state(&meter_asset, &meter_asset_state) ==
          LXP_OK);
    meter_runtime.assets = &meter_asset_state;
    meter_runtime.asset_count = 1U;
    if (node_genesis(&primary) != 0 || node_genesis(&replica) != 0) return 1;
    nodes[0] = &primary;
    nodes[1] = &replica;

    /* S: start 1000, end 2000, cap 30 of which 5 stays unused. */
    length = open_payload(STREAM_S, stream_s_name, (lxp_u128){ 0U, 1U },
                          2000U, (lxp_u128){ 0U, 30U }, 40U, bytes,
                          sizeof(bytes));
    CHECK(length != 0U);
    if (step(nodes, 2U, LX_STREAM_OPEN, bytes, length, payer_name, 500U,
             LXP_OK) != 0) return 1;
    length = open_payload(STREAM_W, stream_w_name, (lxp_u128){ 0U, 1U },
                          9000U, (lxp_u128){ 0U, 12U }, 20U, bytes,
                          sizeof(bytes));
    CHECK(length != 0U);
    if (step(nodes, 2U, LX_STREAM_OPEN, bytes, length, payer_name, 500U,
             LXP_OK) != 0) return 1;
    length = open_payload(STREAM_P, stream_p_name, (lxp_u128){ 0U, 1U },
                          9000U, (lxp_u128){ 0U, 50U }, 10U, bytes,
                          sizeof(bytes));
    CHECK(length != 0U);
    if (step(nodes, 2U, LX_STREAM_OPEN, bytes, length, payer_name, 500U,
             LXP_OK) != 0) return 1;
    length = open_payload(STREAM_O, stream_o_name, max, 9000U, max, 10U,
                          bytes, sizeof(bytes));
    CHECK(length != 0U);
    if (step(nodes, 2U, LX_STREAM_OPEN, bytes, length, payer_name, 500U,
             LXP_OK) != 0) return 1;
    CHECK(node_balance(&primary, payer_name) == 20U);
    CHECK(node_balance(&replica, payer_name) == 20U);
    return 0;
}

static int dispatched_window(void)
{
    meter_node *nodes[2];
    nodes[0] = &primary;
    nodes[1] = &replica;

    /* Before start nothing accrues and nothing moves. */
    if (refused_meter(nodes, 2U, STREAM_S, 5U, meter_seed, 999U,
                      LXP_ERR_NON_MONOTONIC_TIME, stream_s_name) != 0)
        return 1;

    /* end - 1: accepted, and settled to the recipient. */
    if (meter_step(nodes, 2U, STREAM_S, 10U, meter_seed, 1999U,
                   LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 40U, 10U) != 0) return 1;
    if (expect_stream(nodes, 2U, STREAM_S, 10U, 10U, 0U) != 0) return 1;
    if (keyed_step(nodes, 2U, LX_STREAM_SETTLE, STREAM_S, 0x51U,
                   provider_name, 1999U, LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 32U, 10U) != 0) return 1;
    CHECK(node_balance(&primary, provider_name) == 10U);

    /* end: the inclusive boundary still accrues, more than once. */
    if (meter_step(nodes, 2U, STREAM_S, 20U, meter_seed, 2000U,
                   LXP_OK) != 0) return 1;
    if (keyed_step(nodes, 2U, LX_STREAM_SETTLE, STREAM_S, 0x52U,
                   provider_name, 2000U, LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 32U, 10U) != 0) return 1;
    if (meter_step(nodes, 2U, STREAM_S, 25U, meter_seed, 2000U,
                   LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 40U, 5U) != 0) return 1;
    if (effect_amount(&primary, 56U, 25U) != 0) return 1;
    if (expect_stream(nodes, 2U, STREAM_S, 25U, 25U, 20U) != 0) return 1;

    /* end + 1: refused with the cap still unused, reading and totals kept. */
    if (refused_meter(nodes, 2U, STREAM_S, 40U, meter_seed, 2001U,
                      LXP_ERR_EXPIRED, stream_s_name) != 0)
        return 1;
    /* Value accrued inside the lifetime is still paid after end. */
    if (keyed_step(nodes, 2U, LX_STREAM_SETTLE, STREAM_S, 0x53U,
                   provider_name, 2001U, LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 32U, 5U) != 0) return 1;
    CHECK(node_balance(&primary, provider_name) == 25U);
    CHECK(node_balance(&primary, stream_s_name) == 15U);

    /* start=1000/end=2000 at batch time 3000: a fresh activity, the same
     * reading and a higher one are all refused; a stranger stays refused as
     * a stranger. */
    if (refused_meter(nodes, 2U, STREAM_S, 40U, meter_seed, 3000U,
                      LXP_ERR_EXPIRED, stream_s_name) != 0 ||
        refused_meter(nodes, 2U, STREAM_S, 25U, meter_seed, 3000U,
                      LXP_ERR_EXPIRED, stream_s_name) != 0 ||
        refused_meter(nodes, 2U, STREAM_S, 26U, meter_seed, 3000U,
                      LXP_ERR_EXPIRED, stream_s_name) != 0 ||
        refused_meter(nodes, 2U, STREAM_S, 40U, stranger_seed, 3000U,
                      LXP_ERR_UNAUTHORIZED_METER, stream_s_name) != 0)
        return 1;

    /* Nothing is left to pay, and a replayed settlement pays nothing new. */
    if (keyed_step(nodes, 2U, LX_STREAM_SETTLE, STREAM_S, 0x54U,
                   provider_name, 3000U, LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 32U, 0U) != 0) return 1;
    if (keyed_step(nodes, 2U, LX_STREAM_SETTLE, STREAM_S, 0x53U,
                   provider_name, 3000U, LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 32U, 5U) != 0) return 1;
    CHECK(node_balance(&primary, provider_name) == 25U);
    CHECK(node_balance(&replica, provider_name) == 25U);
    CHECK(node_balance(&primary, stream_s_name) == 15U);
    return expect_stream(nodes, 2U, STREAM_S, 25U, 25U, 25U);
}

static int dispatched_refusals(void)
{
    meter_node *nodes[2];
    uint8_t bytes[LX_STREAM_ID_PAYLOAD_BYTES];
    lx_stream_id_payload id;
    size_t length = 0U;
    nodes[0] = &primary;
    nodes[1] = &replica;

    if (meter_step(nodes, 2U, STREAM_W, 4U, meter_seed, 3100U, LXP_OK) != 0)
        return 1;
    if (refused_meter(nodes, 2U, STREAM_W, 6U, stranger_seed, 3110U,
                      LXP_ERR_UNAUTHORIZED_METER, stream_w_name) != 0 ||
        refused_meter(nodes, 2U, STREAM_W, 3U, meter_seed, 3120U,
                      LXP_ERR_METER_REGRESSION, stream_w_name) != 0)
        return 1;
    /* The cap clips the accrual, then an exhausted cap takes nothing. */
    if (meter_step(nodes, 2U, STREAM_W, 20U, meter_seed, 3130U,
                   LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 40U, 8U) != 0) return 1;
    if (expect_stream(nodes, 2U, STREAM_W, 20U, 12U, 0U) != 0) return 1;
    if (meter_step(nodes, 2U, STREAM_W, 30U, meter_seed, 3140U,
                   LXP_OK) != 0) return 1;
    if (effect_amount(&primary, 40U, 0U) != 0) return 1;
    if (expect_stream(nodes, 2U, STREAM_W, 20U, 12U, 0U) != 0) return 1;

    if (refused_meter(nodes, 2U, STREAM_O, UINT64_MAX, meter_seed, 3200U,
                      LXP_ERR_ACCRUAL_OVERFLOW, stream_o_name) != 0)
        return 1;

    /* Paused usage is absorbed without accruing; closed refuses. */
    (void)memset(&id, 0, sizeof(id));
    id.stream_id[0] = STREAM_P;
    CHECK(lx_stream_id_encode(&id, bytes, sizeof(bytes), &length) == LXP_OK);
    if (step(nodes, 2U, LX_STREAM_PAUSE, bytes, length, payer_name, 3300U,
             LXP_OK) != 0) return 1;
    if (meter_step(nodes, 2U, STREAM_P, 7U, meter_seed, 3400U, LXP_OK) != 0)
        return 1;
    if (effect_amount(&primary, 40U, 0U) != 0) return 1;
    if (expect_stream(nodes, 2U, STREAM_P, 7U, 0U, 0U) != 0) return 1;
    if (keyed_step(nodes, 2U, LX_STREAM_CLOSE, STREAM_P, 0x61U, payer_name,
                   3500U, LXP_OK) != 0) return 1;
    CHECK(node_balance(&primary, payer_name) == 30U);
    CHECK(node_balance(&primary, stream_p_name) == 0U);
    return refused_meter(nodes, 2U, STREAM_P, 9U, meter_seed, 3600U,
                         LXP_ERR_STREAM_CLOSED, stream_p_name);
}

static int same_roots(meter_node *const *nodes, size_t count)
{
    uint8_t first[32];
    uint8_t root[32];
    uint8_t first_bytes[LX_STREAM_RECORD_BYTES];
    uint8_t bytes[LX_STREAM_RECORD_BYTES];
    lx_stream_record record;
    size_t i;
    CHECK(lxp_state_root(&nodes[0]->kernel, first) == LXP_OK);
    if (node_record(nodes[0], STREAM_S, &record, first_bytes) != 0) return 1;
    for (i = 1U; i < count; ++i) {
        CHECK(lxp_state_root(&nodes[i]->kernel, root) == LXP_OK);
        CHECK(memcmp(first, root, 32U) == 0);
        if (node_record(nodes[i], STREAM_S, &record, bytes) != 0) return 1;
        CHECK(memcmp(first_bytes, bytes, sizeof(bytes)) == 0);
        CHECK(node_balance(nodes[i], payer_name) ==
              node_balance(nodes[0], payer_name));
        CHECK(node_balance(nodes[i], provider_name) ==
              node_balance(nodes[0], provider_name));
        CHECK(node_balance(nodes[i], stream_s_name) ==
              node_balance(nodes[0], stream_s_name));
    }
    return 0;
}

static int restart_and_replica(const char *directory)
{
    static uint8_t snapshot_storage[4194304];
    static uint8_t read_storage[4194304];
    meter_node *nodes[3];
    lxp_snapshot_manifest_record manifest;
    lxp_snapshot_manifest_record stored_manifest;
    lxp_byte_span snapshot;
    lxp_byte_span stored;
    lxp_arena snapshot_arena;
    lxp_arena read_arena;
    uint8_t root[32];
    char path[4096];
    uint64_t sequence;
    const uint64_t later = 1000000000U;
    nodes[0] = &primary;
    nodes[1] = &replica;
    nodes[2] = &restored;

    if (same_roots(nodes, 2U) != 0) return 1;
    CHECK(primary.state.next_sequence != 0U);
    sequence = primary.state.next_sequence - 1U;
    snapshot_sequence = sequence;
    CHECK(lxp_state_root(&primary.kernel, root) == LXP_OK);
    CHECK(lxp_arena_init(&snapshot_arena, snapshot_storage,
                         sizeof(snapshot_storage)) == LXP_OK);
    CHECK(lxp_snapshot_write(&primary.kernel, sequence, &snapshot_arena,
                             &snapshot) == LXP_OK);
    CHECK(lxp_snapshot_manifest(snapshot.bytes, snapshot.length, sequence,
                                root, root, &manifest) == LXP_OK);
    CHECK(lxp_snapshot_store_write(directory, &manifest, snapshot.bytes,
                                   snapshot.length) == LXP_OK);
    CHECK(snprintf(path, sizeof(path), "%s/%020llu.lxs", directory,
                   (unsigned long long)sequence) > 0);

    /* A fresh process image: empty registry, durable store, no clock. */
    CHECK(lx_account_registry_init(&restored.accounts) == LXP_OK);
    CHECK(lxp_state_store_init(&restored.state, 0U) == LXP_OK);
    CHECK(lxp_state_store_bind_accounts(&restored.state,
                                        &restored.accounts) == LXP_OK);
    CHECK(lxp_state_store_require_account_root(&restored.state) == LXP_OK);
    if (node_kernel(&restored) != 0) return 1;
    CHECK(lxp_arena_init(&read_arena, read_storage, sizeof(read_storage)) ==
          LXP_OK);
    CHECK(lxp_snapshot_store_read(path, &read_arena, &stored_manifest,
                                  &stored) == LXP_OK);
    CHECK(lxp_snapshot_load(stored.bytes, stored.length, &stored_manifest,
                            &restored.kernel) == LXP_OK);
    CHECK(lxp_snapshot_verify_root(&restored.kernel, &stored_manifest) ==
          LXP_OK);
    if (same_roots(nodes, 3U) != 0) return 1;
    if (expect_stream(nodes, 3U, STREAM_S, 25U, 25U, 25U) != 0) return 1;

    /* Long after the restart the expired stream still takes no usage. */
    if (refused_meter(nodes, 3U, STREAM_S, 60U, meter_seed, later,
                      LXP_ERR_EXPIRED, stream_s_name) != 0)
        return 1;
    if (keyed_step(nodes, 3U, LX_STREAM_SETTLE, STREAM_S, 0x55U,
                   provider_name, later, LXP_OK) != 0) return 1;
    if (effect_amount(&restored, 32U, 0U) != 0) return 1;
    if (keyed_step(nodes, 3U, LX_STREAM_SETTLE, STREAM_S, 0x53U,
                   provider_name, later, LXP_OK) != 0) return 1;
    if (effect_amount(&restored, 32U, 5U) != 0) return 1;
    CHECK(node_balance(&restored, provider_name) == 25U);

    /* Closing refunds exactly the residual the lifetime left unspent. */
    if (keyed_step(nodes, 3U, LX_STREAM_CLOSE, STREAM_S, 0x56U, payer_name,
                   later, LXP_OK) != 0) return 1;
    if (effect_amount(&restored, 32U, 0U) != 0 ||
        effect_amount(&restored, 48U, 15U) != 0) return 1;
    CHECK(node_balance(&restored, payer_name) == 45U);
    CHECK(node_balance(&restored, stream_s_name) == 0U);
    if (refused_meter(nodes, 3U, STREAM_S, 60U, meter_seed, later + 1U,
                      LXP_ERR_STREAM_CLOSED, stream_s_name) != 0)
        return 1;
    return same_roots(nodes, 3U);
}

#define RUN_CASE(name, call) do { \
    if ((call) != 0) { \
        (void)fprintf(stderr, "case %s failed\n", name); \
        return 1; \
    } \
    (void)printf("case %s ok\n", name); \
} while (0)

int main(int argc, char **argv)
{
    char scratch[] = "/tmp/lx-stream-meter-XXXXXX";
    const char *directory = argc > 1 ? argv[1] : mkdtemp(scratch);
    if (directory == NULL) return 1;
    RUN_CASE("attested_meter", attested_meter());
    RUN_CASE("metered_boundaries", metered_boundaries());
    RUN_CASE("attestation_preimage", attestation_preimage());
    RUN_CASE("meter_payload", meter_payload());
    RUN_CASE("dispatch_fixture", dispatch_fixture());
    RUN_CASE("dispatched_window", dispatched_window());
    RUN_CASE("dispatched_refusals", dispatched_refusals());
    RUN_CASE("restart_and_replica", restart_and_replica(directory));
    if (argc <= 1) {
        char path[128];
        (void)snprintf(path, sizeof(path), "%s/%020llu.lxs", directory,
                       (unsigned long long)snapshot_sequence);
        (void)unlink(path);
        (void)rmdir(directory);
    }
    return 0;
}
