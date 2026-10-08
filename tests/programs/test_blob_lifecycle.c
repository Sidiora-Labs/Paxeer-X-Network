#include "layerx/programs.h"

#include "layerx/lxp_crypto.h"
#include "layerx/lxp_fee.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_identity.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_module_ctx.h"
#include "layerx/lxp_state_proof.h"

#include "../../src/modules/programs/storage.h"

#include <openssl/evp.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum {
    PROOF_CAPACITY = 4096,
    PAYLOAD_CAPACITY = LXP_PROGRAMS_RETIREMENT_MAX_PAYLOAD_BYTES + 512,
    MANIFEST_BYTES = 58,
    MAX_ACCOUNTS = 8,
    MAX_TRACE = 96
};

static const char controller_did[] = "did:lxp:retirement-controller";
static const char intruder_did[] = "did:lxp:retirement-intruder";
static const char *account_names[] = {
    "agent:did:lxp:retirement-controller:main",
    "agent:did:lxp:retirement-intruder:main", "system:fees"};
static const uint8_t paxai_key[] = "paxai/state/v1";
static const uint8_t mirror_key[] = "mirror/state";

typedef struct proof_bytes {
    uint8_t bytes[PROOF_CAPACITY];
    uint16_t length;
} proof_bytes;

typedef struct candidate {
    uint8_t class_id;
    uint8_t ns[33];
    bool has_ns;
    uint8_t digest[32];
    uint32_t size;
    uint64_t origin;
    uint8_t origin_root[32];
    uint8_t replay_id[32];
    const proof_bytes *proof;
    const uint8_t *manifest;
    uint16_t manifest_length;
} candidate;

typedef struct request_params {
    uint32_t network;
    uint16_t profile_version;
    uint8_t profile_digest[32];
    uint8_t controller[32];
    uint64_t cutoff;
    uint64_t first;
    uint64_t last;
    const uint8_t *seed_a;
    const uint8_t *seed_b;
} request_params;

typedef struct programs_view {
    uint32_t blobs;
    uint32_t entries;
    uint8_t programs_digest[32];
    uint8_t others_digest[32];
    uint8_t control[LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES];
} programs_view;

typedef struct trace {
    uint8_t roots[MAX_TRACE][32];
    lxp_result results[MAX_TRACE];
    size_t count;
    uint8_t outcome[32];
    uint8_t final_root[32];
} trace;

typedef struct fixture {
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_identity_store identities;
    lxp_identity *controller;
    lxp_identity *intruder;
    lxp_module_ctx ctx;
    lxp_arena seed_arena;
    uint8_t seed_arena_bytes[65536];
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_receipt receipt;
    uint8_t payload[PAYLOAD_CAPACITY];
    uint64_t parameters;
    uint16_t nonce;
    uint8_t profile_digest[32];
    uint8_t ns_paxai[33], ns_shared_b[33], ns_principal_b[65];
    uint8_t root_a[32], root_b[32], root_c[32];
    uint64_t origin_a, origin_b;
    proof_bytes head_a, head_b, head_c, head_shared_b;
    proof_bytes replay_proof[4];
    uint8_t manifest_a[MANIFEST_BYTES], manifest_b[MANIFEST_BYTES];
    uint8_t manifest_c[MANIFEST_BYTES], manifest_shared_b[MANIFEST_BYTES];
    uint8_t replay_ids[4][32];
    uint8_t witness_digest[4][32];
    uint32_t witness_size[4];
    bool verbose;
    trace *trace;
} fixture;

static fixture fixtures[2];
static trace traces[2];
static lxp_state_witness witness;
static uint8_t controller_seed[32], intruder_seed[32];
static uint8_t archive_seed_a[32], archive_seed_b[32], stranger_seed[32];
static const uint8_t asset_id[32] = {1U};

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

static void put_u16(uint8_t *p, uint16_t v)
{
    p[0] = (uint8_t)(v >> 8U);
    p[1] = (uint8_t)v;
}

static void put_u32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)(v >> 24U);
    p[1] = (uint8_t)(v >> 16U);
    p[2] = (uint8_t)(v >> 8U);
    p[3] = (uint8_t)v;
}

static void put_u64(uint8_t *p, uint64_t v)
{
    put_u32(p, (uint32_t)(v >> 32U));
    put_u32(p + 4U, (uint32_t)v);
}

static uint32_t get_u32(const uint8_t *p)
{
    return ((uint32_t)p[0] << 24U) | ((uint32_t)p[1] << 16U) |
           ((uint32_t)p[2] << 8U) | p[3];
}

static uint64_t get_u64(const uint8_t *p)
{
    return ((uint64_t)get_u32(p) << 32U) | get_u32(p + 4U);
}

static void fill(uint8_t *bytes, size_t length, uint8_t seed)
{
    size_t i;
    for (i = 0U; i < length; ++i) bytes[i] = (uint8_t)(seed + i * 7U);
}

static int fail(const char *name, const char *what, lxp_result got,
                lxp_result expected)
{
    (void)fprintf(stderr, "BLOB_LIFECYCLE_FAIL %s %s got=%d expected=%d\n",
                  name, what, (int)got, (int)expected);
    return 1;
}

static int pass(const fixture *fx, const char *name, lxp_result result)
{
    if (fx->verbose)
        (void)printf("BLOB_LIFECYCLE_CASE %s result=%d\n", name, (int)result);
    return 0;
}

static const lxp_module_kv_entry *find_kv(const lxp_kernel *kernel,
                                          const uint8_t *key, size_t length)
{
    size_t i;
    for (i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[i];
        if (entry->module_id == LXP_MODULE_PROGRAMS &&
            entry->key_length == length && memcmp(entry->key, key, length) == 0)
            return entry;
    }
    return NULL;
}

static const lxp_module_blob *find_blob(const lxp_kernel *kernel,
                                        const uint8_t digest[32])
{
    size_t i;
    for (i = 0U; i < kernel->blob_count; ++i)
        if (kernel->blobs[i].module_id == LXP_MODULE_PROGRAMS &&
            memcmp(kernel->blobs[i].key, digest, 32U) == 0)
            return &kernel->blobs[i];
    return NULL;
}

static int view_take(const lxp_kernel *kernel, programs_view *view)
{
    static const uint8_t control_key[] = "progretire/v1";
    lxp_hash_context programs, others;
    const lxp_module_kv_entry *control;
    size_t i;
    lxp_result status = LXP_OK;
    (void)memset(view, 0, sizeof(*view));
    lxp_hash_init(&programs);
    lxp_hash_init(&others);
    for (i = 0U; status == LXP_OK && i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *entry = &kernel->module_kv[i];
        lxp_hash_context *target =
            entry->module_id == LXP_MODULE_PROGRAMS ? &programs : &others;
        uint8_t header[8];
        put_u16(header, entry->module_id);
        put_u16(header + 2U, entry->key_length);
        put_u32(header + 4U, entry->value_length);
        if (entry->module_id == LXP_MODULE_PROGRAMS) ++view->entries;
        status = lxp_hash_update(target, header, sizeof(header));
        if (status == LXP_OK)
            status = lxp_hash_update(target, entry->key, entry->key_length);
        if (status == LXP_OK)
            status = lxp_hash_update(target, entry->value, entry->value_length);
    }
    for (i = 0U; status == LXP_OK && i < kernel->blob_count; ++i) {
        const lxp_module_blob *blob = &kernel->blobs[i];
        lxp_hash_context *target =
            blob->module_id == LXP_MODULE_PROGRAMS ? &programs : &others;
        if (blob->module_id == LXP_MODULE_PROGRAMS) ++view->blobs;
        status = lxp_hash_update(target, blob->key, 32U);
        if (status == LXP_OK && blob->length != 0U)
            status = lxp_hash_update(target, blob->bytes, blob->length);
    }
    if (status == LXP_OK) status = lxp_hash_final(&programs, view->programs_digest);
    if (status == LXP_OK) status = lxp_hash_final(&others, view->others_digest);
    control = find_kv(kernel, control_key, sizeof(control_key) - 1U);
    if (control != NULL) {
        if (control->value_length != LXP_PROGRAMS_RETIREMENT_CONTROL_BYTES)
            return 1;
        (void)memcpy(view->control, control->value, control->value_length);
    }
    return status == LXP_OK ? 0 : 1;
}

static int fixture_init(fixture *fx, bool verbose, trace *out)
{
    static const char *dids[] = {controller_did, intruder_did};
    const uint8_t *seeds[] = {controller_seed, intruder_seed};
    lxp_identity **identities[] = {&fx->controller, &fx->intruder};
    lx_programs_transfer_runtime *runtime;
    lxp_transfer_asset_state *asset_state;
    uint8_t public_key[32], id[32];
    size_t i;
    (void)memset(fx, 0, sizeof(*fx));
    (void)memset(out, 0, sizeof(*out));
    fx->verbose = verbose;
    fx->trace = out;
    fx->parameters = 1U;
    for (i = 0U; i < 2U; ++i)
        if (public_key_for(seeds[i], public_key) != 0 ||
            lxp_identity_register(&fx->identities, (const uint8_t *)dids[i],
                                  strlen(dids[i]), public_key,
                                  identities[i]) != LXP_OK)
            return 1;
    if (lxp_state_store_init(&fx->store, 1U) != LXP_OK ||
        lxp_kernel_create(&fx->kernel, &fx->store, &fx->journal,
                          &fx->parameters, 0U) != LXP_OK ||
        lxp_kernel_register_module(
            &fx->kernel, programs_module_registration_v4_storage_retirement()) !=
            LXP_OK)
        return 1;
    runtime = calloc(1U, sizeof(*runtime));
    asset_state = calloc(1U, sizeof(*asset_state));
    if (runtime == NULL || asset_state == NULL) {
        free(runtime);
        free(asset_state);
        return 1;
    }
    runtime->accounts = calloc(1U, sizeof(*runtime->accounts));
    (void)memcpy(asset_state->asset_id, asset_id, 32U);
    asset_state->registered = true;
    runtime->assets = asset_state;
    runtime->asset_count = 1U;
    (void)memcpy(runtime->occupancy_asset_id, asset_id, 32U);
    if (runtime->accounts == NULL ||
        lx_account_registry_init(runtime->accounts) != LXP_OK ||
        lxp_state_store_bind_accounts(&fx->store, runtime->accounts) != LXP_OK)
        return 1;
    for (i = 0U; i < sizeof(account_names) / sizeof(account_names[0]); ++i) {
        lx_account *account;
        const bool treasury = i == 2U;
        if (lx_account_id_from_string((const uint8_t *)account_names[i],
                                      strlen(account_names[i]), id) != LXP_OK ||
            lx_account_open(runtime->accounts, (const uint8_t *)account_names[i],
                            strlen(account_names[i]), id, treasury ? 2U : 1U,
                            LX_ACCOUNT_OPEN_GENESIS, NULL, &account) != LXP_OK ||
            lxp_ledger_bootstrap_balance(account, asset_id,
                treasury ? (lxp_u128){0U, 0U} :
                           (lxp_u128){0U, UINT64_C(1000000000)}, 0U) != LXP_OK)
            return 1;
    }
    if (lxp_state_store_require_account_root(&fx->store) != LXP_OK ||
        lxp_kernel_bind_module_runtime(&fx->kernel, LXP_MODULE_PROGRAMS,
                                       runtime) != LXP_OK ||
        lxp_programs_bind_fee_transaction(&fx->kernel) != LXP_OK ||
        lxp_kernel_set_capabilities(&fx->kernel, NULL,
                                    lxp_kernel_canonical_ledger_apply) != LXP_OK ||
        lxp_state_root(&fx->kernel, fx->kernel.current_state_root) != LXP_OK)
        return 1;
    return 0;
}

static void fixture_destroy(fixture *fx)
{
    lx_programs_transfer_runtime *runtime =
        fx->kernel.module_runtime[LXP_MODULE_PROGRAMS];
    while (fx->kernel.blob_count != 0U)
        free(fx->kernel.blobs[--fx->kernel.blob_count].bytes);
    if (runtime != NULL) {
        free((void *)runtime->assets);
        lx_account_registry_release(runtime->accounts);
        free(runtime->accounts);
        free(runtime);
    }
    (void)lxp_state_store_destroy(&fx->store);
}

static int seed_open(fixture *fx)
{
    return lxp_arena_init(&fx->seed_arena, fx->seed_arena_bytes,
                          sizeof(fx->seed_arena_bytes)) != LXP_OK ||
        lxp_module_ctx_init(&fx->ctx, &fx->kernel, LXP_MODULE_PROGRAMS, 10U,
                            fx->kernel.epoch, fx->store.next_sequence,
                            UINT64_C(100000000), &fx->seed_arena,
                            true) != LXP_OK;
}

static int seed_close(fixture *fx, lxp_result status)
{
    if (status != LXP_OK) {
        lxp_module_ctx_rollback(&fx->ctx);
        return 1;
    }
    return lxp_module_ctx_commit(&fx->ctx) != LXP_OK ||
        lxp_state_root(&fx->kernel, fx->kernel.current_state_root) != LXP_OK;
}

static int seed_namespace(fixture *fx, const uint8_t *ns, uint16_t ns_length,
                          const uint8_t *key, uint16_t key_length,
                          const uint8_t *value, uint32_t value_length)
{
    lxp_programs_storage_cell cell = {key, key_length, value, value_length};
    if (seed_open(fx) != 0) return 1;
    return seed_close(fx, lxp_programs_storage_stage_final(&fx->ctx, ns,
                                                           ns_length, &cell, 1U));
}

static int seed_kv(fixture *fx, const uint8_t *key, size_t key_length,
                   const uint8_t *value, size_t value_length)
{
    if (seed_open(fx) != 0) return 1;
    return seed_close(fx, value == NULL ?
        lxp_ctx_kv_del(&fx->ctx, key, key_length) :
        lxp_ctx_kv_put(&fx->ctx, key, key_length, value, value_length));
}

static int seed_replay(fixture *fx, size_t index, const uint8_t *bytes,
                       uint32_t length)
{
    lxp_programs_replay_capture capture;
    (void)memset(&capture, 0, sizeof(capture));
    if (seed_open(fx) != 0) return 1;
    fill(fx->replay_ids[index], 32U, (uint8_t)(0x30U + index));
    capture.network_id = 1U;
    capture.sequence = fx->ctx.global_sequence;
    (void)memcpy(capture.activity_id, fx->replay_ids[index], 32U);
    (void)memcpy(capture.previous_root, fx->kernel.current_state_root, 32U);
    (void)memcpy(capture.program_id, fx->ns_paxai, 32U);
    fill(capture.code_hash, 32U, 0x51U);
    fill(capture.input_digest, 32U, (uint8_t)(0x61U + index));
    capture.runtime_version = 1U;
    capture.abi_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    capture.fee_version = 1U;
    capture.metering_version = 1U;
    capture.max_boundaries = 2U;
    capture.max_bytes = 512U;
    capture.terminal_status = 1U;
    capture.boundary_count = 1U;
    fill(capture.authority_root, 32U, 0x71U);
    fill(capture.host_root, 32U, 0x72U);
    fill(capture.boundary_root, 32U, 0x73U);
    fill(capture.witness_digest, 32U, 0x74U);
    capture.bytes = (uint8_t *)(uintptr_t)bytes;
    capture.length = length;
    (void)memcpy(fx->ctx.activity_id, fx->replay_ids[index], 32U);
    fx->ctx.call_admission.present = true;
    fx->ctx.call_admission.fee_schedule_version = 1U;
    fx->ctx.call_admission.metering_schedule_version = 1U;
    if (lxp_hash_sha256(bytes, length, fx->witness_digest[index]) != LXP_OK)
        return 1;
    fx->witness_size[index] = length;
    return seed_close(fx, lxp_programs_replay_capture_stage(&fx->ctx, &capture,
                                                            LXP_OK));
}

static int prove(fixture *fx, const uint8_t *key, size_t key_length,
                 const uint8_t root[32], proof_bytes *out)
{
    size_t length = 0U;
    if (lxp_state_proof_build(&fx->kernel, LXP_MODULE_PROGRAMS,
                              (lxp_byte_span){key, key_length},
                              &witness) != LXP_OK ||
        lxp_state_proof_verify(&witness, root) != LXP_OK ||
        lxp_state_proof_encode(&witness, out->bytes, sizeof(out->bytes),
                               &length) != LXP_OK ||
        length > UINT16_MAX)
        return 1;
    out->length = (uint16_t)length;
    return 0;
}

static int prove_head(fixture *fx, const uint8_t ns[33], const uint8_t root[32],
                      proof_bytes *out, uint8_t manifest[MANIFEST_BYTES])
{
    uint8_t key[41];
    const lxp_module_kv_entry *head;
    const lxp_module_blob *blob;
    (void)memcpy(key, "progstor", 8U);
    (void)memcpy(key + 8U, ns, 33U);
    head = find_kv(&fx->kernel, key, sizeof(key));
    if (head == NULL || head->value_length != 38U) return 1;
    blob = find_blob(&fx->kernel, head->value + 6U);
    if (blob == NULL || blob->length != MANIFEST_BYTES) return 1;
    (void)memcpy(manifest, blob->bytes, MANIFEST_BYTES);
    return prove(fx, key, sizeof(key), root, out);
}

static void storage_candidate(candidate *c, uint8_t class_id,
                              const uint8_t ns[33], const proof_bytes *proof,
                              uint64_t origin, const uint8_t root[32],
                              const uint8_t *manifest)
{
    (void)memset(c, 0, sizeof(*c));
    c->class_id = class_id;
    c->has_ns = true;
    (void)memcpy(c->ns, ns, 33U);
    if (class_id == LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST) {
        (void)lxp_hash_sha256(manifest, MANIFEST_BYTES, c->digest);
        c->size = MANIFEST_BYTES;
    } else {
        (void)memcpy(c->digest, manifest + 22U, 32U);
        c->size = get_u32(manifest + 54U);
    }
    c->origin = origin;
    (void)memcpy(c->origin_root, root, 32U);
    c->proof = proof;
    c->manifest = manifest;
    c->manifest_length = MANIFEST_BYTES;
}

static void replay_candidate(const fixture *fx, candidate *c, size_t index)
{
    (void)memset(c, 0, sizeof(*c));
    c->class_id = LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY;
    (void)memcpy(c->digest, fx->witness_digest[index], 32U);
    c->size = fx->witness_size[index];
    c->origin = fx->origin_a;
    (void)memcpy(c->origin_root, fx->root_a, 32U);
    (void)memcpy(c->replay_id, fx->replay_ids[index], 32U);
    c->proof = &fx->replay_proof[index];
}

static const uint8_t *identity_of(const candidate *c)
{
    return c->class_id == LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY ?
        c->replay_id : c->digest;
}

static void sort_candidates(candidate *c, size_t count)
{
    size_t i, j;
    for (i = 1U; i < count; ++i)
        for (j = i; j != 0U; --j) {
            candidate swap;
            if (c[j - 1U].class_id < c[j].class_id ||
                (c[j - 1U].class_id == c[j].class_id &&
                 memcmp(identity_of(&c[j - 1U]), identity_of(&c[j]), 32U) < 0))
                break;
            swap = c[j - 1U];
            c[j - 1U] = c[j];
            c[j] = swap;
        }
}

static void default_params(const fixture *fx, request_params *p)
{
    (void)memset(p, 0, sizeof(*p));
    p->network = 1U;
    p->profile_version = LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION;
    (void)memcpy(p->profile_digest, fx->profile_digest, 32U);
    (void)memcpy(p->controller, fx->controller->did_id, 32U);
    p->cutoff = fx->store.next_sequence - 1U;
    p->first = 1U;
    p->last = p->cutoff;
    p->seed_a = archive_seed_a;
    p->seed_b = archive_seed_b;
}

static size_t encode(fixture *fx, const request_params *p, const candidate *c,
                     uint8_t count, size_t count_present)
{
    uint8_t *out = fx->payload;
    uint8_t digest[32];
    size_t cursor = LXP_PROGRAMS_RETIREMENT_HEADER_BYTES, i;
    (void)memset(out, 0, sizeof(fx->payload));
    out[0] = LXP_PROGRAMS_RETIREMENT_REQUEST_VERSION;
    out[1] = count;
    put_u32(out + 4U, p->network);
    put_u16(out + 8U, p->profile_version);
    (void)memcpy(out + 10U, p->profile_digest, 32U);
    (void)memcpy(out + 42U, p->controller, 32U);
    put_u64(out + 74U, fx->store.next_sequence);
    (void)memcpy(out + 82U, fx->kernel.current_state_root, 32U);
    put_u64(out + 114U, p->cutoff);
    fill(out + 122U, 32U, 0xC1U);
    fill(out + 154U, 32U, 0xD1U);
    put_u64(out + 186U, UINT64_C(1048576));
    put_u64(out + 194U, p->first);
    put_u64(out + 202U, p->last);
    (void)memcpy(out + 210U, fx->root_c, 32U);
    for (i = 0U; i < count_present; ++i) {
        uint8_t *fixed = out + cursor;
        fixed[0] = c[i].class_id;
        if (c[i].has_ns) {
            fixed[1] = 33U;
            (void)memcpy(fixed + 4U, c[i].ns, 33U);
        }
        (void)memcpy(fixed + 37U, c[i].digest, 32U);
        put_u32(fixed + 69U, c[i].size);
        put_u64(fixed + 73U, c[i].origin);
        (void)memcpy(fixed + 81U, c[i].origin_root, 32U);
        (void)memcpy(fixed + 113U, c[i].replay_id, 32U);
        put_u16(fixed + 145U, c[i].proof->length);
        put_u16(fixed + 147U, c[i].manifest_length);
        cursor += LXP_PROGRAMS_RETIREMENT_CANDIDATE_BYTES;
        (void)memcpy(out + cursor, c[i].proof->bytes, c[i].proof->length);
        cursor += c[i].proof->length;
        if (c[i].manifest_length != 0U)
            (void)memcpy(out + cursor, c[i].manifest, c[i].manifest_length);
        cursor += c[i].manifest_length;
    }
    if (lxp_programs_retirement_certificate_digest(out, cursor, digest) == LXP_OK &&
        (sign_raw(p->seed_a, digest, 32U, out + 242U) != 0 ||
         sign_raw(p->seed_b, digest, 32U, out + 306U) != 0))
        return 0U;
    return cursor;
}

static lxp_result execute(fixture *fx, lxp_identity *identity, const char *did,
                          const uint8_t seed[32], size_t length)
{
    lxp_activity activity;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_kernel_execution execution;
    lxp_fee_params fees;
    lxp_arena arena;
    uint8_t signature[64], digest[32];
    lxp_result status;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.network_id = 1U;
    activity.activity_type = LX_PROGRAMS_STORAGE_RETIREMENT;
    activity.actor_did = (lxp_byte_span){(const uint8_t *)did, strlen(did)};
    activity.authority = (lxp_byte_span){identity->primary_key, 32U};
    activity.account_sequence = identity->next_sequence;
    activity.timestamp_bound = (lxp_timestamp_bound){1U, 100U};
    ++fx->nonce;
    activity.idempotency_key[30] = (uint8_t)(fx->nonce >> 8U);
    activity.idempotency_key[31] = (uint8_t)fx->nonce;
    activity.fee_limit = (lxp_u128){0U, 100000000U};
    activity.payload = (lxp_byte_span){fx->payload, length};
    activity.signature = (lxp_byte_span){signature, 64U};
    if (lxp_hash_payload(fx->payload, length, activity.payload_hash) != LXP_OK ||
        lxp_activity_signing_preimage(&activity, digest) != LXP_OK ||
        sign_raw(seed, digest, sizeof(digest), signature) != 0 ||
        lxp_activity_verify_signature(&activity) != LXP_OK ||
        lxp_authority_resolve_activity(&fx->kernel, identity, &activity, true,
                                       true, 10U, 100U, fx->store.next_sequence,
                                       &grant, &authority) != LXP_OK ||
        lxp_arena_init(&arena, fx->arena_bytes, sizeof(fx->arena_bytes)) !=
            LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memset(&fees, 0, sizeof(fees));
    fees.version = 1U;
    fees.base_fee = (lxp_u128){0U, 1U};
    fees.multiplier_basis_points = 10000U;
    (void)memset(&execution, 0, sizeof(execution));
    execution.network_id = 1U;
    execution.batch_number = 1U;
    execution.batch_timestamp_ms = 10U;
    execution.maximum_timestamp_window = 100U;
    execution.epoch = fx->kernel.epoch;
    execution.global_sequence = fx->store.next_sequence;
    execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    execution.parameter_version = 1U;
    execution.signature_valid = true;
    execution.identities = &fx->identities;
    execution.authority = &authority;
    execution.fee_parameters = &fees;
    execution.fee_balance = (lxp_u128){0U, UINT64_C(1000000000)};
    execution.gas_limit = UINT64_C(10000000);
    execution.arena = &arena;
    execution.sequencer_private_key = controller_seed;
    (void)memset(&fx->receipt, 0, sizeof(fx->receipt));
    status = lxp_kernel_execute_activity(&fx->kernel, &activity, &execution,
                                         &fx->receipt);
    if (status != LXP_OK) return status;
    if (fx->trace->count == MAX_TRACE) return LXP_FATAL_INVARIANT;
    (void)memcpy(fx->trace->roots[fx->trace->count],
                 fx->receipt.resulting_state_root, 32U);
    fx->trace->results[fx->trace->count] = fx->receipt.result_code;
    ++fx->trace->count;
    return fx->receipt.result_code;
}

static int refuse_as(fixture *fx, const char *name, lxp_identity *identity,
                     const char *did, const uint8_t seed[32], size_t length,
                     lxp_result expected)
{
    programs_view before, after;
    uint8_t root[32];
    uint64_t sequence = fx->store.next_sequence;
    lxp_result result;
    if (length == 0U) return fail(name, "encode", LXP_OK, expected);
    if (view_take(&fx->kernel, &before) != 0)
        return fail(name, "view", LXP_OK, expected);
    result = execute(fx, identity, did, seed, length);
    if (result != expected) return fail(name, "result", result, expected);
    if (view_take(&fx->kernel, &after) != 0 ||
        memcmp(&before, &after, sizeof(before)) != 0)
        return fail(name, "programs-state-changed", result, expected);
    if (fx->store.next_sequence != sequence + 1U ||
        lxp_state_root(&fx->kernel, root) != LXP_OK ||
        memcmp(root, fx->kernel.current_state_root, 32U) != 0 ||
        memcmp(root, fx->receipt.resulting_state_root, 32U) != 0 ||
        fx->receipt.effects.count != 0U)
        return fail(name, "receipt", result, expected);
    return pass(fx, name, result);
}

static int refuse(fixture *fx, const char *name, size_t length,
                  lxp_result expected)
{
    return refuse_as(fx, name, fx->controller, controller_did, controller_seed,
                     length, expected);
}

static int advance(fixture *fx, size_t steps)
{
    request_params p;
    size_t i;
    for (i = 0U; i < steps; ++i) {
        default_params(fx, &p);
        if (refuse(fx, "advance_truncated",
                   encode(fx, &p, NULL, 1U, 0U), LXP_ERR_TRUNCATED) != 0)
            return 1;
    }
    return 0;
}

typedef struct account_view {
    lx_account accounts[MAX_ACCOUNTS];
    size_t count;
} account_view;

static int accounts_take(const fixture *fx, account_view *view)
{
    const lx_programs_transfer_runtime *runtime =
        fx->kernel.module_runtime[LXP_MODULE_PROGRAMS];
    if (runtime->accounts->count > MAX_ACCOUNTS) return 1;
    view->count = runtime->accounts->count;
    (void)memcpy(view->accounts, runtime->accounts->accounts,
                 view->count * sizeof(lx_account));
    return 0;
}

static int accounts_untouched(const fixture *fx, const account_view *before,
                              lxp_u128 fee)
{
    account_view after;
    uint8_t payer[32], treasury[32];
    size_t i;
    if (accounts_take(fx, &after) != 0 || after.count != before->count ||
        lx_account_id_from_string((const uint8_t *)account_names[0],
                                  strlen(account_names[0]), payer) != LXP_OK ||
        lx_account_id_from_string((const uint8_t *)account_names[2],
                                  strlen(account_names[2]), treasury) != LXP_OK)
        return 1;
    for (i = 0U; i < after.count; ++i) {
        const lx_account *a = &before->accounts[i];
        const lx_account *b = &after.accounts[i];
        lxp_u128 expected = a->balance;
        if (memcmp(a->id, b->id, 32U) != 0 ||
            memcmp(a->asset_id, b->asset_id, 32U) != 0 ||
            a->has_asset != b->has_asset || a->frozen != b->frozen ||
            a->has_open_reference != b->has_open_reference ||
            a->kind != b->kind)
            return 1;
        if (memcmp(a->id, payer, 32U) == 0 &&
            lxp_u128_sub(a->balance, fee, &expected) != LXP_OK)
            return 1;
        if (memcmp(a->id, treasury, 32U) == 0 &&
            lxp_u128_add(a->balance, fee, &expected) != LXP_OK)
            return 1;
        if (lxp_u128_cmp(b->balance, expected) != 0) return 1;
    }
    return 0;
}

static int retire(fixture *fx, const char *name, size_t length, uint8_t count,
                  uint8_t deletions, uint8_t replays, const uint8_t *deleted,
                  const candidate *c)
{
    programs_view before, after;
    account_view accounts;
    uint8_t root_before[32], root[32];
    const lxp_effect *header;
    uint64_t retirements;
    lxp_result result;
    size_t i;
    if (length == 0U) return fail(name, "encode", LXP_OK, LXP_OK);
    if (view_take(&fx->kernel, &before) != 0 || accounts_take(fx, &accounts) != 0)
        return fail(name, "view", LXP_OK, LXP_OK);
    (void)memcpy(root_before, fx->kernel.current_state_root, 32U);
    result = execute(fx, fx->controller, controller_did, controller_seed, length);
    if (result != LXP_OK) return fail(name, "result", result, LXP_OK);
    if (view_take(&fx->kernel, &after) != 0 ||
        lxp_state_root(&fx->kernel, root) != LXP_OK ||
        memcmp(root, fx->kernel.current_state_root, 32U) != 0 ||
        memcmp(root, fx->receipt.resulting_state_root, 32U) != 0 ||
        memcmp(root, root_before, 32U) == 0)
        return fail(name, "root", result, LXP_OK);
    if (after.blobs + deletions != before.blobs ||
        after.entries + replays != before.entries ||
        memcmp(after.others_digest, before.others_digest, 32U) != 0)
        return fail(name, "bounded-state", result, LXP_OK);
    retirements = get_u64(before.control + 187U);
    if (get_u64(after.control + 179U) != get_u64(fx->payload + 114U) ||
        get_u64(after.control + 187U) != retirements + 1U ||
        get_u64(after.control + 195U) !=
            get_u64(before.control + 195U) + deletions ||
        get_u64(after.control + 211U) !=
            get_u64(before.control + 211U) + replays ||
        memcmp(after.control, before.control, 179U) != 0)
        return fail(name, "control", result, LXP_OK);
    if (fx->receipt.effects.count != (size_t)count + 1U)
        return fail(name, "events", (lxp_result)fx->receipt.effects.count, LXP_OK);
    for (i = 0U; i <= count; ++i)
        if (fx->receipt.effects.effects[i].kind != LXP_EFFECT_EVENT ||
            fx->receipt.effects.effects[i].event_type !=
                LXP_PROGRAMS_RETIREMENT_EVENT)
            return fail(name, "event-kind", result, LXP_OK);
    header = &fx->receipt.effects.effects[0];
    if (header->body_length != 226U || header->body[199] != count ||
        header->body[200] != deletions || header->body[201] != replays ||
        memcmp(header->body + 39U, root_before, 32U) != 0 ||
        get_u32(header->body + 210U) != before.blobs ||
        get_u32(header->body + 214U) != after.blobs ||
        get_u32(header->body + 218U) != before.entries ||
        get_u32(header->body + 222U) != after.entries)
        return fail(name, "header-frame", result, LXP_OK);
    for (i = 0U; i < count; ++i) {
        const lxp_effect *frame = &fx->receipt.effects.effects[i + 1U];
        const bool gone = find_blob(&fx->kernel, c[i].digest) == NULL;
        if (frame->body_length != 117U || frame->body[7] != c[i].class_id ||
            frame->body[8] != deleted[i] ||
            memcmp(frame->body + 9U, c[i].digest, 32U) != 0 ||
            gone != (deleted[i] != 0U))
            return fail(name, "candidate-frame", result, LXP_OK);
    }
    if (accounts_untouched(fx, &accounts, fx->receipt.fee_charged) != 0)
        return fail(name, "ledger", result, LXP_OK);
    (void)memcpy(fx->trace->outcome, after.control + 219U, 32U);
    return pass(fx, name, result);
}

static int seed_world(fixture *fx)
{
    static const uint8_t control_key[] = "progretire/v1";
    lxp_programs_retirement_profile profile;
    uint8_t value_a[40], value_b[40], value_c[40], value_shared[40];
    uint8_t witness_a[96], witness_c[80], witness_d[72];
    uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
    size_t i;
    fill(fx->ns_paxai, 32U, 0xA1U);
    fx->ns_paxai[32] = 1U;
    fill(fx->ns_shared_b, 32U, 0xB1U);
    fx->ns_shared_b[32] = 1U;
    (void)memcpy(fx->ns_principal_b, fx->ns_shared_b, 32U);
    fx->ns_principal_b[32] = 0U;
    (void)memcpy(fx->ns_principal_b + 33U, fx->controller->did_id, 32U);
    fill(value_a, sizeof(value_a), 0x11U);
    fill(value_b, sizeof(value_b), 0x22U);
    fill(value_c, sizeof(value_c), 0x33U);
    fill(value_shared, sizeof(value_shared), 0x44U);
    fill(witness_a, sizeof(witness_a), 0x55U);
    fill(witness_c, sizeof(witness_c), 0x66U);
    fill(witness_d, sizeof(witness_d), 0x77U);
    (void)memset(&profile, 0, sizeof(profile));
    profile.version = LXP_PROGRAMS_RETIREMENT_PROFILE_VERSION;
    profile.network_id = 1U;
    (void)memcpy(profile.controller, fx->controller->did_id, 32U);
    (void)memcpy(profile.paxai_program_id, fx->ns_paxai, 32U);
    profile.hot_horizon = 1U;
    if (public_key_for(archive_seed_a, profile.archive_keys[0]) != 0 ||
        public_key_for(archive_seed_b, profile.archive_keys[1]) != 0 ||
        lxp_programs_retirement_profile_digest(&profile, fx->profile_digest) !=
            LXP_OK ||
        seed_open(fx) != 0 ||
        seed_close(fx, lxp_programs_retirement_profile_stage(&fx->ctx,
                                                             &profile)) != 0 ||
        find_kv(&fx->kernel, control_key, sizeof(control_key) - 1U) == NULL)
        return 1;
    if (seed_open(fx) != 0) return 1;
    if (lxp_programs_retirement_profile_stage(&fx->ctx, &profile) !=
        LXP_ERR_DUPLICATE_ENTRY)
        return 1;
    lxp_module_ctx_rollback(&fx->ctx);
    if (seed_namespace(fx, fx->ns_paxai, 33U, paxai_key, 14U, value_a,
                       sizeof(value_a)) != 0 ||
        seed_replay(fx, 0U, witness_a, sizeof(witness_a)) != 0 ||
        seed_replay(fx, 1U, witness_a, sizeof(witness_a)) != 0 ||
        seed_replay(fx, 2U, witness_c, sizeof(witness_c)) != 0 ||
        seed_replay(fx, 3U, witness_d, sizeof(witness_d)) != 0)
        return 1;
    fx->origin_a = fx->store.next_sequence;
    (void)memcpy(fx->root_a, fx->kernel.current_state_root, 32U);
    if (prove_head(fx, fx->ns_paxai, fx->root_a, &fx->head_a,
                   fx->manifest_a) != 0)
        return 1;
    for (i = 0U; i < 4U; ++i) {
        lxp_programs_replay_record_key(fx->replay_ids[i], key);
        if (prove(fx, key, sizeof(key), fx->root_a, &fx->replay_proof[i]) != 0)
            return 1;
    }
    if (advance(fx, 2U) != 0 ||
        seed_namespace(fx, fx->ns_paxai, 33U, paxai_key, 14U, value_b,
                       sizeof(value_b)) != 0 ||
        seed_namespace(fx, fx->ns_principal_b, 65U, mirror_key,
                       sizeof(mirror_key) - 1U, value_b, sizeof(value_b)) != 0 ||
        seed_namespace(fx, fx->ns_shared_b, 33U, paxai_key, 14U, value_shared,
                       sizeof(value_shared)) != 0)
        return 1;
    fx->origin_b = fx->store.next_sequence;
    (void)memcpy(fx->root_b, fx->kernel.current_state_root, 32U);
    if (prove_head(fx, fx->ns_paxai, fx->root_b, &fx->head_b,
                   fx->manifest_b) != 0 ||
        prove_head(fx, fx->ns_shared_b, fx->root_b, &fx->head_shared_b,
                   fx->manifest_shared_b) != 0 ||
        seed_namespace(fx, fx->ns_paxai, 33U, paxai_key, 14U, value_c,
                       sizeof(value_c)) != 0)
        return 1;
    (void)memcpy(fx->root_c, fx->kernel.current_state_root, 32U);
    if (prove_head(fx, fx->ns_paxai, fx->root_c, &fx->head_c,
                   fx->manifest_c) != 0)
        return 1;
    return advance(fx, 2U);
}

static int absent_profile(fixture *fx)
{
    request_params p;
    candidate c;
    proof_bytes proof;
    (void)memset(&c, 0, sizeof(c));
    (void)memset(&proof, 0, sizeof(proof));
    proof.length = 1U;
    proof.bytes[0] = 1U;
    c.class_id = LXP_PROGRAMS_RETIREMENT_CLASS_REPLAY;
    fill(c.digest, 32U, 0x01U);
    fill(c.replay_id, 32U, 0x02U);
    fill(c.origin_root, 32U, 0x03U);
    c.size = 1U;
    c.origin = 1U;
    c.proof = &proof;
    fill(fx->root_c, 32U, 0x04U);
    default_params(fx, &p);
    fill(p.profile_digest, 32U, 0x05U);
    p.cutoff = 1U;
    p.last = 1U;
    return refuse(fx, "profile_absent_refuses", encode(fx, &p, &c, 1U, 1U),
                  LXP_ERR_MODULE_DISABLED);
}

static int decode_refusals(fixture *fx)
{
    request_params p;
    candidate c[4];
    size_t length;
    default_params(fx, &p);
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[1], LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[2], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_b, fx->origin_b, fx->root_b, fx->manifest_b);
    replay_candidate(fx, &c[3], 2U);
    if (refuse(fx, "empty_request_refuses", encode(fx, &p, c, 0U, 0U),
               LXP_ERR_NON_CANONICAL) != 0 ||
        refuse(fx, "four_candidates_refuse", encode(fx, &p, c, 4U, 4U),
               LXP_ERR_LENGTH_LIMIT) != 0)
        return 1;
    length = encode(fx, &p, c, 1U, 1U);
    if (refuse(fx, "truncated_candidate_refuses", length - 1U,
               LXP_ERR_TRUNCATED) != 0)
        return 1;
    length = encode(fx, &p, c, 1U, 1U);
    fx->payload[length] = 0U;
    if (refuse(fx, "trailing_bytes_refuse", length + 1U,
               LXP_ERR_TRAILING_BYTES) != 0)
        return 1;
    c[3].class_id = 4U;
    if (refuse(fx, "unknown_class_refuses", encode(fx, &p, &c[3], 1U, 1U),
               LXP_ERR_VERSION_UNSUPPORTED) != 0)
        return 1;
    replay_candidate(fx, &c[3], 2U);
    length = encode(fx, &p, c, 1U, 1U);
    fx->payload[LXP_PROGRAMS_RETIREMENT_HEADER_BYTES + 2U] = 1U;
    if (refuse(fx, "reserved_bytes_refuse", length,
               LXP_ERR_NON_CANONICAL) != 0)
        return 1;
    length = encode(fx, &p, c, 1U, 1U);
    fx->payload[LXP_PROGRAMS_RETIREMENT_HEADER_BYTES + 4U + 32U] = 0U;
    if (refuse(fx, "principal_form_candidate_refuses", length,
               LXP_ERR_NON_CANONICAL) != 0)
        return 1;
    {
        candidate twice[2] = {c[0], c[0]};
        if (refuse(fx, "duplicate_candidate_refuses",
                   encode(fx, &p, twice, 2U, 2U),
                   LXP_ERR_DUPLICATE_ENTRY) != 0)
            return 1;
    }
    {
        candidate pair[2] = {c[0], c[2]};
        sort_candidates(pair, 2U);
        candidate swapped[2] = {pair[1], pair[0]};
        if (refuse(fx, "unsorted_candidates_refuse",
                   encode(fx, &p, swapped, 2U, 2U),
                   LXP_ERR_UNSORTED_SEQUENCE) != 0)
            return 1;
    }
    {
        candidate conflict[2] = {c[0], c[1]};
        (void)memcpy(conflict[1].digest, conflict[0].digest, 32U);
        if (refuse(fx, "conflicting_candidates_refuse",
                   encode(fx, &p, conflict, 2U, 2U),
                   LXP_ERR_DUPLICATE_ENTRY) != 0)
            return 1;
    }
    return 0;
}

static int admission_refusals(fixture *fx)
{
    request_params p;
    candidate c[2];
    size_t length;
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[1], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_b, fx->origin_b, fx->root_b, fx->manifest_b);
    default_params(fx, &p);
    p.network = 2U;
    if (refuse(fx, "wrong_network_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_WRONG_NETWORK) != 0)
        return 1;
    default_params(fx, &p);
    p.profile_version = 2U;
    if (refuse(fx, "wrong_profile_version_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_VERSION_UNSUPPORTED) != 0)
        return 1;
    default_params(fx, &p);
    p.profile_digest[0] ^= 1U;
    if (refuse(fx, "wrong_profile_digest_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    default_params(fx, &p);
    if (refuse_as(fx, "wrong_controller_signer_refuses", fx->intruder,
                  intruder_did, intruder_seed, encode(fx, &p, c, 1U, 1U),
                  LXP_ERR_AUTH_SCOPE) != 0)
        return 1;
    (void)memcpy(p.controller, fx->intruder->did_id, 32U);
    if (refuse(fx, "wrong_controller_field_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_AUTH_SCOPE) != 0)
        return 1;
    default_params(fx, &p);
    length = encode(fx, &p, c, 1U, 1U);
    put_u64(fx->payload + 74U, fx->store.next_sequence + 1U);
    if (refuse(fx, "expected_sequence_race_refuses", length,
               LXP_ERR_SEQUENCE_MISMATCH) != 0)
        return 1;
    {
        uint8_t stale[32];
        (void)memcpy(stale, fx->kernel.current_state_root, 32U);
        if (advance(fx, 1U) != 0) return 1;
        length = encode(fx, &p, c, 1U, 1U);
        (void)memcpy(fx->payload + 82U, stale, 32U);
        if (refuse(fx, "expected_root_race_refuses", length,
                   LXP_ERR_ROOT_MISMATCH) != 0)
            return 1;
    }
    default_params(fx, &p);
    p.cutoff = fx->store.next_sequence;
    p.last = p.cutoff;
    if (refuse(fx, "unfinalized_cutoff_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_NOT_YET_VALID) != 0)
        return 1;
    default_params(fx, &p);
    p.cutoff = fx->origin_b;
    p.last = p.cutoff;
    if (refuse(fx, "hot_horizon_refuses", encode(fx, &p, &c[1], 1U, 1U),
               LXP_ERR_NOT_YET_VALID) != 0)
        return 1;
    default_params(fx, &p);
    p.last = p.cutoff - 1U;
    if (refuse(fx, "archive_cutoff_mismatch_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    default_params(fx, &p);
    p.first = fx->origin_a + 1U;
    if (refuse(fx, "origin_outside_archive_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    default_params(fx, &p);
    p.seed_b = stranger_seed;
    if (refuse(fx, "wrong_archive_key_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_ATTESTATION_THRESHOLD) != 0)
        return 1;
    default_params(fx, &p);
    length = encode(fx, &p, c, 1U, 1U);
    (void)memset(fx->payload + 306U, 0, 64U);
    if (refuse(fx, "single_signature_refuses", length,
               LXP_ERR_ATTESTATION_THRESHOLD) != 0)
        return 1;
    default_params(fx, &p);
    length = encode(fx, &p, c, 1U, 1U);
    put_u64(fx->payload + 186U, UINT64_C(1048577));
    if (refuse(fx, "tampered_certificate_refuses", length,
               LXP_ERR_ATTESTATION_THRESHOLD) != 0)
        return 1;
    return 0;
}

static int provenance_refusals(fixture *fx)
{
    request_params p;
    candidate c;
    proof_bytes tampered;
    default_params(fx, &p);
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_b, fx->manifest_a);
    if (refuse(fx, "wrong_origin_root_refuses", encode(fx, &p, &c, 1U, 1U),
               LXP_ERR_ROOT_MISMATCH) != 0)
        return 1;
    tampered = fx->head_a;
    tampered.bytes[tampered.length - 1U] ^= 1U;
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &tampered, fx->origin_a, fx->root_a, fx->manifest_a);
    if (refuse(fx, "tampered_proof_refuses", encode(fx, &p, &c, 1U, 1U),
               LXP_ERR_ROOT_MISMATCH) != 0)
        return 1;
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_b);
    if (refuse(fx, "manifest_not_proven_refuses", encode(fx, &p, &c, 1U, 1U),
               LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    ++c.size;
    if (refuse(fx, "wrong_value_size_refuses", encode(fx, &p, &c, 1U, 1U),
               LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_shared_b,
                      &fx->head_shared_b, fx->origin_b, fx->root_b,
                      fx->manifest_shared_b);
    if (refuse(fx, "non_paxai_namespace_refuses", encode(fx, &p, &c, 1U, 1U),
               LXP_ERR_AUTH_SCOPE) != 0)
        return 1;
    replay_candidate(fx, &c, 2U);
    c.origin = fx->origin_a + 1U;
    if (refuse(fx, "replay_wrong_origin_sequence_refuses",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_SEQUENCE_MISMATCH) != 0)
        return 1;
    replay_candidate(fx, &c, 2U);
    c.proof = &fx->replay_proof[3];
    if (refuse(fx, "replay_proof_for_other_record_refuses",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_CONTEXT_MISMATCH) != 0)
        return 1;
    replay_candidate(fx, &c, 2U);
    (void)memcpy(c.digest, fx->witness_digest[3], 32U);
    return refuse(fx, "replay_wrong_witness_refuses",
                  encode(fx, &p, &c, 1U, 1U), LXP_ERR_CONTEXT_MISMATCH);
}

static int protection_refusals(fixture *fx)
{
    request_params p;
    candidate c[2];
    default_params(fx, &p);
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_c, fx->origin_b, fx->root_c, fx->manifest_c);
    if (refuse(fx, "current_shared_head_manifest_protected",
               encode(fx, &p, c, 1U, 1U), LXP_ERR_CONDITION_UNMET) != 0)
        return 1;
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_c, fx->origin_b, fx->root_c, fx->manifest_c);
    if (refuse(fx, "current_shared_head_value_protected",
               encode(fx, &p, c, 1U, 1U), LXP_ERR_CONDITION_UNMET) != 0)
        return 1;
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_b, fx->origin_b, fx->root_b, fx->manifest_b);
    if (refuse(fx, "cross_namespace_dedup_protected",
               encode(fx, &p, c, 1U, 1U), LXP_ERR_CONDITION_UNMET) != 0)
        return 1;
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[1], LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_b, fx->origin_b, fx->root_b, fx->manifest_b);
    sort_candidates(c, 2U);
    if (refuse(fx, "mixed_request_with_live_reference_refuses_whole",
               encode(fx, &p, c, 2U, 2U), LXP_ERR_CONDITION_UNMET) != 0)
        return 1;
    return 0;
}

static int unknown_reference_refusals(fixture *fx)
{
    static const uint8_t unknown_control[] = "progretire/v2";
    request_params p;
    candidate c;
    uint8_t key[73], value[38];
    default_params(fx, &p);
    storage_candidate(&c, LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    (void)memcpy(key, "progstor", 8U);
    fill(key + 8U, 32U, 0xC7U);
    key[40] = 7U;
    (void)memset(value, 0, sizeof(value));
    put_u16(value, 1U);
    if (seed_kv(fx, key, 41U, value, sizeof(value)) != 0 ||
        refuse(fx, "unknown_head_form_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_VERSION_UNSUPPORTED) != 0 ||
        seed_kv(fx, key, 41U, NULL, 0U) != 0)
        return 1;
    key[40] = 1U;
    put_u32(value + 2U, 1U);
    fill(value + 6U, 32U, 0xE3U);
    if (seed_kv(fx, key, 41U, value, sizeof(value)) != 0 ||
        refuse(fx, "dangling_manifest_reference_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_NON_CANONICAL) != 0 ||
        seed_kv(fx, key, 41U, NULL, 0U) != 0)
        return 1;
    key[40] = 0U;
    (void)memset(key + 41U, 0, 32U);
    (void)memcpy(value + 6U, c.digest, 32U);
    if (seed_kv(fx, key, 73U, value, sizeof(value)) != 0 ||
        refuse(fx, "zero_principal_head_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_VERSION_UNSUPPORTED) != 0 ||
        seed_kv(fx, key, 73U, NULL, 0U) != 0)
        return 1;
    (void)memcpy(key, "progreplay/v2/", 14U);
    fill(key + 14U, 32U, 0xF1U);
    if (seed_kv(fx, key, 46U, value, sizeof(value)) != 0 ||
        refuse(fx, "unknown_replay_format_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_VERSION_UNSUPPORTED) != 0 ||
        seed_kv(fx, key, 46U, NULL, 0U) != 0)
        return 1;
    (void)memcpy(key, "progreplay/v1/", 14U);
    if (seed_kv(fx, key, 46U, value, sizeof(value)) != 0 ||
        refuse(fx, "malformed_replay_record_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_NON_CANONICAL) != 0 ||
        seed_kv(fx, key, 46U, NULL, 0U) != 0)
        return 1;
    if (seed_kv(fx, unknown_control, sizeof(unknown_control) - 1U, value,
                sizeof(value)) != 0 ||
        refuse(fx, "unknown_control_record_fails_closed",
               encode(fx, &p, &c, 1U, 1U), LXP_ERR_VERSION_UNSUPPORTED) != 0 ||
        seed_kv(fx, unknown_control, sizeof(unknown_control) - 1U, NULL, 0U) != 0)
        return 1;
    {
        static const uint8_t foreign_key[] = "progother/digest-ref";
        if (seed_kv(fx, foreign_key, sizeof(foreign_key) - 1U, c.digest, 32U) != 0 ||
            refuse(fx, "opaque_live_reference_protected",
                   encode(fx, &p, &c, 1U, 1U), LXP_ERR_CONDITION_UNMET) != 0 ||
            seed_kv(fx, foreign_key, sizeof(foreign_key) - 1U, NULL, 0U) != 0)
            return 1;
    }
    return 0;
}

static int proofs_survive(fixture *fx, const char *name)
{
    uint8_t root[32];
    size_t i;
    if (lxp_state_proof_decode(fx->head_a.bytes, fx->head_a.length, &witness) !=
            LXP_OK ||
        lxp_state_proof_verify(&witness, fx->root_a) != LXP_OK ||
        lxp_state_root(&fx->kernel, root) != LXP_OK ||
        lxp_state_proof_verify(&witness, root) == LXP_OK)
        return fail(name, "head-proof", LXP_OK, LXP_OK);
    for (i = 0U; i < 4U; ++i)
        if (lxp_state_proof_decode(fx->replay_proof[i].bytes,
                                   fx->replay_proof[i].length, &witness) !=
                LXP_OK ||
            lxp_state_proof_verify(&witness, fx->root_a) != LXP_OK)
            return fail(name, "replay-proof", LXP_OK, LXP_OK);
    return pass(fx, name, LXP_OK);
}

static int retirements(fixture *fx)
{
    request_params p;
    candidate c[3];
    uint8_t deleted[3];
    uint64_t first_cutoff;
    size_t i;
    default_params(fx, &p);
    first_cutoff = p.cutoff;
    storage_candidate(&c[0], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[1], LXP_PROGRAMS_RETIREMENT_CLASS_VALUE, fx->ns_paxai,
                      &fx->head_a, fx->origin_a, fx->root_a, fx->manifest_a);
    storage_candidate(&c[2], LXP_PROGRAMS_RETIREMENT_CLASS_MANIFEST, fx->ns_paxai,
                      &fx->head_b, fx->origin_b, fx->root_b, fx->manifest_b);
    sort_candidates(c, 3U);
    (void)memset(deleted, 1, sizeof(deleted));
    if (find_blob(&fx->kernel, c[0].digest) == NULL ||
        find_blob(&fx->kernel, c[1].digest) == NULL ||
        find_blob(&fx->kernel, c[2].digest) == NULL ||
        retire(fx, "superseded_manifests_and_value_retire",
               encode(fx, &p, c, 3U, 3U), 3U, 3U, 0U, deleted, c) != 0)
        return 1;
    for (i = 0U; i < 3U; ++i)
        if (find_blob(&fx->kernel, c[i].digest) != NULL)
            return fail("superseded_manifests_and_value_retire", "blob-live",
                        LXP_OK, LXP_OK);
    {
        uint8_t value_b[32], manifest_c[32];
        (void)memcpy(value_b, fx->manifest_b + 22U, 32U);
        (void)lxp_hash_sha256(fx->manifest_c, MANIFEST_BYTES, manifest_c);
        if (find_blob(&fx->kernel, value_b) == NULL ||
            find_blob(&fx->kernel, manifest_c) == NULL ||
            find_blob(&fx->kernel, fx->manifest_c + 22U) == NULL ||
            find_blob(&fx->kernel, fx->manifest_shared_b + 22U) == NULL)
            return fail("live_blobs_kept", "blob-missing", LXP_OK, LXP_OK);
        if (pass(fx, "live_blobs_kept", LXP_OK) != 0) return 1;
    }
    if (proofs_survive(fx, "historical_proofs_valid_after_retirement") != 0)
        return 1;
    default_params(fx, &p);
    if (refuse(fx, "already_retired_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_UNKNOWN_FIELD) != 0)
        return 1;
    replay_candidate(fx, &c[0], 0U);
    default_params(fx, &p);
    p.cutoff = first_cutoff - 1U;
    p.last = p.cutoff;
    if (refuse(fx, "cutoff_regression_refuses", encode(fx, &p, c, 1U, 1U),
               LXP_ERR_SEQUENCE_MISMATCH) != 0)
        return 1;
    default_params(fx, &p);
    deleted[0] = 0U;
    if (retire(fx, "replay_record_retires_shared_witness_kept",
               encode(fx, &p, c, 1U, 1U), 1U, 0U, 1U, deleted, c) != 0)
        return 1;
    {
        uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
        lxp_programs_replay_record_key(fx->replay_ids[0], key);
        if (find_kv(&fx->kernel, key, sizeof(key)) != NULL ||
            find_blob(&fx->kernel, fx->witness_digest[0]) == NULL)
            return fail("replay_record_retires_shared_witness_kept", "state",
                        LXP_OK, LXP_OK);
    }
    for (i = 0U; i < 3U; ++i) replay_candidate(fx, &c[i], i + 1U);
    sort_candidates(c, 3U);
    (void)memset(deleted, 1, sizeof(deleted));
    default_params(fx, &p);
    if (retire(fx, "paired_replay_and_witness_retire_at_staging_bound",
               encode(fx, &p, c, 3U, 3U), 3U, 3U, 3U, deleted, c) != 0)
        return 1;
    for (i = 1U; i < 4U; ++i) {
        uint8_t key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
        lxp_programs_replay_record_key(fx->replay_ids[i], key);
        if (find_kv(&fx->kernel, key, sizeof(key)) != NULL ||
            find_blob(&fx->kernel, fx->witness_digest[i]) != NULL)
            return fail("paired_replay_and_witness_retire_at_staging_bound",
                        "state", LXP_OK, LXP_OK);
    }
    return proofs_survive(fx, "replay_proofs_valid_after_retirement");
}

static int scenario(fixture *fx, bool verbose, trace *out)
{
    int status;
    if (fixture_init(fx, verbose, out) != 0) {
        fixture_destroy(fx);
        return fail("fixture", "init", LXP_OK, LXP_OK);
    }
    status = absent_profile(fx) != 0 || seed_world(fx) != 0 ||
        decode_refusals(fx) != 0 || admission_refusals(fx) != 0 ||
        provenance_refusals(fx) != 0 || protection_refusals(fx) != 0 ||
        unknown_reference_refusals(fx) != 0 || retirements(fx) != 0;
    if (status == 0 && lxp_state_root(&fx->kernel, out->final_root) != LXP_OK)
        status = 1;
    fixture_destroy(fx);
    return status;
}

int main(void)
{
    size_t i;
    fill(controller_seed, 32U, 0x81U);
    fill(intruder_seed, 32U, 0x91U);
    fill(archive_seed_a, 32U, 0xA9U);
    fill(archive_seed_b, 32U, 0xB9U);
    fill(stranger_seed, 32U, 0xC9U);
    if (scenario(&fixtures[0], true, &traces[0]) != 0) return 1;
    if (scenario(&fixtures[1], false, &traces[1]) != 0) return 1;
    if (traces[0].count != traces[1].count ||
        memcmp(traces[0].outcome, traces[1].outcome, 32U) != 0 ||
        memcmp(traces[0].final_root, traces[1].final_root, 32U) != 0)
        return fail("deterministic_replay", "trace", LXP_OK, LXP_OK);
    for (i = 0U; i < traces[0].count; ++i)
        if (memcmp(traces[0].roots[i], traces[1].roots[i], 32U) != 0 ||
            traces[0].results[i] != traces[1].results[i])
            return fail("deterministic_replay", "root", traces[0].results[i],
                        traces[1].results[i]);
    (void)printf("BLOB_LIFECYCLE_CASE deterministic_replay result=0 "
                 "activities=%zu\n", traces[0].count);
    (void)printf("BLOB_LIFECYCLE_PASS\n");
    return 0;
}
