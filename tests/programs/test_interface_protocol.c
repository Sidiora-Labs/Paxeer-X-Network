#define main interface_fixture_reference_main
int interface_fixture_reference_main(int argc, char **argv);
#include "test_call_activity.c"
#undef main
#include "layerx/lxp_batch_identity.h"
#include "../../src/modules/programs/storage.h"

#define METERED_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "metered call check failed at line %d\n", __LINE__); \
    return 1; } } while (0)

typedef struct metered_fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lx_account_registry accounts;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    lxp_arena arena;
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_identity *identity;
    lx_account *actor;
    lx_account *payee;
    lx_account *source;
    uint64_t parameters;
    uint8_t owner_key[32];
    uint8_t delegate_key[32];
    uint8_t program[32];
    uint8_t source_id[32];
    uint8_t grant_id[32];
    uint8_t call[1024];
    size_t call_length;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_transfer_allowance allowance;
    lxp_kernel_execution execution;
    lxp_activity activity;
    lxp_receipt receipt;
    uint8_t signature[64];
    lxp_u128 signed_fee_limit;
    unsigned recipient_mode;
} metered_fixture;

static const uint8_t metered_owner_seed[32] = {0x33U};
static const uint8_t metered_delegate_seed[32] = {0x34U};
static const uint8_t metered_did[] = "did:lxp:metered-call";
static int metered_account(metered_fixture *f, const char *name,
                            uint64_t balance, lx_account **account)
{
    uint8_t id[32];
    METERED_CHECK(lx_account_id_from_string((const uint8_t *)name,
                                            strlen(name), id) == LXP_OK);
    METERED_CHECK(lx_account_open(&f->accounts, (const uint8_t *)name,
                                   strlen(name), id, 1U,
                                   LX_ACCOUNT_OPEN_GENESIS, NULL, account) ==
                  LXP_OK);
    METERED_CHECK(lxp_ledger_bootstrap_balance(*account, f->asset.asset_id,
                                              (lxp_u128){0U, balance}, 0U) ==
                  LXP_OK);
    return 0;
}

static int metered_activity(metered_fixture *f, uint32_t type,
                             const uint8_t *payload, size_t length,
                             uint8_t marker, bool delegated)
{
    const uint8_t *key = delegated ? f->delegate_key : f->owner_key;
    const uint8_t *seed = delegated ? metered_delegate_seed : metered_owner_seed;
    uint8_t digest[32];
    lxp_byte_span encoded;
    EVP_PKEY *signer_key;
    EVP_MD_CTX *signer_context;
    size_t signature_length = sizeof(f->signature);
    METERED_CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    fill_activity(&f->activity, type, payload, length, metered_did,
                   sizeof(metered_did) - 1U, key);
    f->activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    f->activity.account_sequence = f->identity->next_sequence;
    write_u64(f->activity.idempotency_key + 23U, f->state.next_sequence);
    f->activity.idempotency_key[31] = marker;
    f->activity.fee_limit = lxp_u128_is_zero(f->signed_fee_limit) ?
        (lxp_u128){0U, 67108864U} : f->signed_fee_limit;
    f->activity.signature = (lxp_byte_span){f->signature, sizeof(f->signature)};
    METERED_CHECK(lxp_hash_payload(payload, length, f->activity.payload_hash) ==
                  LXP_OK);
    METERED_CHECK(lxp_activity_signing_preimage(&f->activity, digest) == LXP_OK);
    signer_key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed, 32U);
    signer_context = EVP_MD_CTX_new();
    METERED_CHECK(signer_key != NULL && signer_context != NULL);
    METERED_CHECK(EVP_DigestSignInit(signer_context, NULL, NULL, NULL,
                                     signer_key) == 1);
    METERED_CHECK(EVP_DigestSign(signer_context, f->signature, &signature_length,
                                digest, sizeof(digest)) == 1);
    EVP_MD_CTX_free(signer_context);
    EVP_PKEY_free(signer_key);
    METERED_CHECK(signature_length == sizeof(f->signature));
    METERED_CHECK(lxp_activity_verify_signature(&f->activity) == LXP_OK);
    METERED_CHECK(lxp_authority_resolve_activity(&f->kernel, f->identity,
        &f->activity, !delegated, true, 10U, 100U, f->state.next_sequence,
        &f->grant, &f->authority) == LXP_OK);
    lxp_authority_allowance_bind(&f->grant, &f->authority, &f->allowance);
    (void)memset(&f->execution, 0, sizeof(f->execution));
    f->execution.network_id = 7U;
    f->execution.epoch = f->kernel.epoch;
    f->execution.batch_number = 1U;
    f->execution.batch_timestamp_ms = 10U;
    f->execution.maximum_timestamp_window = 100U;
    f->execution.global_sequence = f->state.next_sequence;
    f->execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    f->execution.recorded_metering_schedule_version = 1U;
    f->execution.recorded_fee_schedule_version = 1U;
    f->execution.parameter_version = 1U;
    f->execution.signature_valid = true;
    f->execution.sequencer_private_key = executed_sequencer_seed;
    f->execution.identities = &f->identities;
    f->execution.authority = &f->authority;
    f->execution.allowance = &f->allowance;
    f->execution.fee_parameters = &f->fees;
    f->execution.fee_balance = f->actor->balance;
    f->execution.gas_limit = 1000000U;
    f->execution.arena = &f->arena;
    METERED_CHECK(lxp_activity_encode(&f->activity, &f->arena, &encoded) == LXP_OK);
    METERED_CHECK(lxp_activity_id(encoded.bytes, encoded.length, digest) == LXP_OK);
    METERED_CHECK(lxp_batch_identity_activity(f->kernel.current_state_root,
        digest, f->state.next_sequence, 1U, f->execution.batch_id) == LXP_OK);
    return 0;
}

static int cache_fixture_init(metered_fixture *f)
{
    lx_account *treasury;
    lxp_genesis_manifest manifest = {0};
    lx_programs_fee_genesis_parameters fees = {0};
    METERED_CHECK(executed_public_key(metered_owner_seed, f->owner_key) == 0);
    METERED_CHECK(executed_public_key(metered_delegate_seed, f->delegate_key) == 0);
    f->asset.asset_id[0] = 9U;
    f->asset.registered = true;
    f->program[0] = 0x47U;
    METERED_CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    METERED_CHECK(metered_account(f, "agent:did:lxp:metered-call:main",
                                   UINT64_C(1000000000), &f->actor) == 0);
    METERED_CHECK(metered_account(f, "agent:did:lxp:metered-payee:main", 0U,
                                   &f->payee) == 0);
    METERED_CHECK(metered_account(f, "system:fees", 0U, &treasury) == 0);
    if (f->recipient_mode == 1U) f->payee = f->actor;
    if (f->recipient_mode == 2U) f->payee = treasury;
    f->actor->has_authority_key = true;
    (void)memcpy(f->actor->authority_key, f->owner_key, 32U);
    METERED_CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    METERED_CHECK(lxp_state_store_bind_accounts(&f->state, &f->accounts) == LXP_OK);
    f->parameters = 1U;
    METERED_CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal,
                                     &f->parameters, 1U) == LXP_OK);
    METERED_CHECK(install_metering_v1(&f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_register_module(&f->kernel,
                    programs_module_registration_v4()) == LXP_OK);
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->asset;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){
        1U, 1U, 1U, 2U, 4U, 1U, 1U, 1U
    };
    (void)memcpy(f->runtime.occupancy_asset_id, f->asset.asset_id, 32U);
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    (void)memcpy(manifest.signer_public_key, f->owner_key, 32U);
    fees.schedule = f->runtime.fee_schedule;
    (void)memcpy(fees.occupancy_asset_id, f->asset.asset_id, 32U);
    fees.target_occupancy_byte_batches = 3U;
    fees.response_denominator = 1U;
    fees.maximum_change_numerator = 1U;
    fees.maximum_change_denominator = 1U;
    fees.minimum_fee_units_per_occupancy_byte_batch = 1U;
    fees.maximum_fee_units_per_occupancy_byte_batch = 10U;
    METERED_CHECK(lxp_programs_fee_genesis_append(&manifest, &fees) == LXP_OK);
    METERED_CHECK(lxp_programs_fee_genesis_materialize(&manifest, &f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS,
                                                  &f->runtime) == LXP_OK);
    METERED_CHECK(lxp_programs_bind_fee_transaction(&f->kernel) == LXP_OK);
    METERED_CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL,
                    lxp_kernel_canonical_ledger_apply) == LXP_OK);
    METERED_CHECK(lxp_identity_register(&f->identities, metered_did,
        sizeof(metered_did) - 1U, f->owner_key, &f->identity) == LXP_OK);
    f->identity->revocation_sequence = 1U;
    f->fees.version = 1U;
    f->fees.multiplier_basis_points = 10000U;
    METERED_CHECK(lxp_arena_init(&f->arena, f->arena_bytes,
                                 sizeof(f->arena_bytes)) == LXP_OK);
    METERED_CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    return 0;
}

#include "layerx/lxp_merkle.h"
#include <errno.h>
#include <sys/stat.h>

static int interface_hex(FILE *file, const char *name, const uint8_t *bytes, size_t length)
{
    METERED_CHECK(fprintf(file, "%s=", name) > 0);
    for (size_t i = 0; i < length; ++i)
        METERED_CHECK(fprintf(file, "%02x", bytes[i]) == 2);
    METERED_CHECK(fputc('\n', file) != EOF);
    return 0;
}

static int interface_proof(FILE *file, const char *name, uint32_t index,
                            uint32_t count, uint8_t depth,
                            const uint8_t siblings[][32])
{
    METERED_CHECK(fprintf(file, "%s.leaf_index=%u\n%s.leaf_count=%u\n%s.siblings=",
        name, index, name, count, name) > 0);
    for (uint8_t i = 0; i < depth; ++i) {
        if (i != 0) METERED_CHECK(fputc(',', file) != EOF);
        for (size_t j = 0; j < 32; ++j)
            METERED_CHECK(fprintf(file, "%02x", siblings[i][j]) == 2);
    }
    METERED_CHECK(fputc('\n', file) != EOF);
    return 0;
}

static int interface_leaf(FILE *file, const char *name, const metered_fixture *f,
                           const uint8_t *key, size_t key_length,
                           const uint8_t *value, size_t value_length,
                           const uint8_t programs_root[32])
{
    lxp_state_proof proof;
    uint8_t root[32];
    char field[128];
    METERED_CHECK(lxp_state_subtree_proof(&f->kernel, LXP_MODULE_PROGRAMS,
        key, key_length, root, &proof) == LXP_OK);
    METERED_CHECK(memcmp(root, programs_root, 32) == 0);
    METERED_CHECK(snprintf(field, sizeof(field), "%s.key", name) > 0);
    METERED_CHECK(interface_hex(file, field, key, key_length) == 0);
    METERED_CHECK(snprintf(field, sizeof(field), "%s.value", name) > 0);
    METERED_CHECK(interface_hex(file, field, value, value_length) == 0);
    METERED_CHECK(snprintf(field, sizeof(field), "%s.proof", name) > 0);
    return interface_proof(file, field, proof.leaf_index, proof.leaf_count,
        proof.depth, (const uint8_t (*)[32])proof.siblings);
}

static int interface_compare(const uint8_t *a, size_t an, const uint8_t *b, size_t bn)
{
    int result = memcmp(a, b, an < bn ? an : bn);
    return result != 0 ? result : an < bn ? -1 : an > bn ? 1 : 0;
}

static int interface_export(const char *output, const char *name, metered_fixture *f)
{
    static uint8_t memory[4U * LXP_MAX_ACTIVITY_BYTES];
    lxp_arena arena;
    lxp_byte_span activity, receipt, header_bytes, events;
    lxp_batch_roots roots;
    lxp_batch_header header = {0};
    lxp_sequencer_authorization auth = {0};
    lxp_state_proof root_proof;
    lxp_merkle_proof activity_proof, receipt_proof;
    uint8_t programs_root[32], state_root[32], signature[64], leaf_hash[1][32], root[32];
    uint8_t status_key[43], lower_key[129], upper_key[129];
    uint8_t activity_identifier[32], execution_identifier[32];
    size_t lower_length = 0, upper_length = 0;
    const uint8_t *lower_value = NULL, *upper_value = NULL;
    size_t lower_value_length = 0, upper_value_length = 0;
    const lxp_module_kv_entry *record = NULL, *interface = NULL;
    char path[4096];
    FILE *file;
    METERED_CHECK(lxp_arena_init(&arena, memory, sizeof(memory)) == LXP_OK);
    METERED_CHECK(lxp_activity_verify_signature(&f->activity) == LXP_OK);
    METERED_CHECK(lxp_activity_encode(&f->activity, &arena, &activity) == LXP_OK);
    METERED_CHECK(lxp_activity_id(activity.bytes, activity.length, activity_identifier) == LXP_OK);
    METERED_CHECK(memcmp(activity_identifier, f->receipt.activity_id, 32) == 0);
    METERED_CHECK(lxp_batch_identity_activity(f->receipt.previous_state_root,
        activity_identifier, f->receipt.global_sequence, f->execution.batch_number,
        execution_identifier) == LXP_OK);
    METERED_CHECK(memcmp(execution_identifier, f->receipt.batch_id, 32) == 0);
    METERED_CHECK(lxp_receipt_encode(&f->receipt, true, &arena, &receipt) == LXP_OK);
    METERED_CHECK(executed_public_key(executed_sequencer_seed, auth.public_key) == 0);
    METERED_CHECK(lxp_receipt_verify(&f->receipt, auth.public_key, &arena) == LXP_OK);
    METERED_CHECK(lxp_programs_project_receipt_events(&f->receipt, &arena, &events) == LXP_OK);
    METERED_CHECK(lxp_batch_roots_compute(&(lxp_batch_root_inputs){&activity, 1,
        &receipt, 1, &events, 1, NULL, 0, NULL, 0}, &arena, &roots) == LXP_OK);
    METERED_CHECK(lxp_merkle_leaf_hash(activity.bytes, activity.length, leaf_hash[0]) == LXP_OK);
    METERED_CHECK(lxp_merkle_proof_generate((const uint8_t (*)[32])leaf_hash, 1, 0,
        &arena, &activity_proof, root) == LXP_OK);
    METERED_CHECK(memcmp(root, roots.activity_merkle_root, 32) == 0);
    METERED_CHECK(lxp_merkle_leaf_hash(receipt.bytes, receipt.length, leaf_hash[0]) == LXP_OK);
    METERED_CHECK(lxp_merkle_proof_generate((const uint8_t (*)[32])leaf_hash, 1, 0,
        &arena, &receipt_proof, root) == LXP_OK);
    METERED_CHECK(memcmp(root, roots.receipt_merkle_root, 32) == 0);
    METERED_CHECK(lxp_state_subtree_root(&f->kernel, LXP_MODULE_PROGRAMS, programs_root) == LXP_OK);
    METERED_CHECK(lxp_state_root_proof(&f->kernel, LXP_MODULE_PROGRAMS, state_root, &root_proof) == LXP_OK);
    METERED_CHECK(memcmp(state_root, f->receipt.resulting_state_root, 32) == 0);
    header.protocol_version = f->activity.protocol_version;
    header.network_id = f->execution.network_id;
    header.epoch = f->execution.epoch;
    header.batch_number = f->execution.batch_number;
    header.first_sequence = f->execution.global_sequence;
    header.last_sequence = f->execution.global_sequence;
    header.timestamp_ms = f->execution.batch_timestamp_ms;
    memcpy(header.previous_state_root, f->receipt.previous_state_root, 32);
    memcpy(header.resulting_state_root, state_root, 32);
    memcpy(header.activity_merkle_root, roots.activity_merkle_root, 32);
    memcpy(header.receipt_merkle_root, roots.receipt_merkle_root, 32);
    memcpy(header.event_merkle_root, roots.event_merkle_root, 32);
    memcpy(header.oracle_root, roots.oracle_root, 32);
    memcpy(header.data_availability_root, roots.data_availability_root, 32);
    memcpy(auth.sequencer_id, auth.public_key, 32);
    memcpy(header.sequencer_id, auth.public_key, 32);
    auth.authorized = 1;
    auth.first_batch_number = 1;
    auth.last_batch_number = 100;
    METERED_CHECK(lxp_batch_sign(&header, executed_sequencer_seed, &auth, signature, &arena) == LXP_OK);
    METERED_CHECK(lxp_batch_verify_signature(&header, signature, sizeof(signature), &auth, &arena) == LXP_OK);
    METERED_CHECK(lxp_batch_header_encode(&header, &arena, &header_bytes) == LXP_OK);
    METERED_CHECK(snprintf(path, sizeof(path), "%s/%s", output, name) > 0);
    METERED_CHECK(mkdir(path, 0700) == 0 || errno == EEXIST);
    METERED_CHECK(snprintf(path, sizeof(path), "%s/%s/evidence.kvx", output, name) > 0);
    file = fopen(path, "wx");
    METERED_CHECK(file != NULL && fchmod(fileno(file), 0600) == 0);
    METERED_CHECK(fprintf(file, "protocol_version=3\nnetwork_id=7\nepoch=1\nnow_ms=11\nfirst_batch=1\nlast_batch=100\nresult_code=%d\n", f->receipt.result_code) > 0);
    METERED_CHECK(interface_hex(file, "activity", activity.bytes, activity.length) == 0);
    METERED_CHECK(interface_hex(file, "receipt", receipt.bytes, receipt.length) == 0);
    METERED_CHECK(interface_hex(file, "header", header_bytes.bytes, header_bytes.length) == 0);
    METERED_CHECK(interface_hex(file, "header_signature", signature, sizeof(signature)) == 0);
    METERED_CHECK(interface_hex(file, "sequencer_key", auth.public_key, 32) == 0);
    METERED_CHECK(interface_hex(file, "sequencer_id", header.sequencer_id, 32) == 0);
    METERED_CHECK(interface_hex(file, "program_id", f->program, 32) == 0);
    METERED_CHECK(interface_hex(file, "programs_root", programs_root, 32) == 0);
    METERED_CHECK(interface_proof(file, "programs_root_proof", root_proof.leaf_index,
        root_proof.leaf_count, root_proof.depth, (const uint8_t (*)[32])root_proof.siblings) == 0);
    METERED_CHECK(interface_proof(file, "activity_proof", activity_proof.leaf_index,
        activity_proof.leaf_count, activity_proof.depth, (const uint8_t (*)[32])activity_proof.siblings) == 0);
    METERED_CHECK(interface_proof(file, "receipt_proof", receipt_proof.leaf_index,
        receipt_proof.leaf_count, receipt_proof.depth, (const uint8_t (*)[32])receipt_proof.siblings) == 0);
    memcpy(status_key, "wind-down\0s", 11);
    memcpy(status_key + 11, f->program, 32);
    for (size_t i = 0; i < f->kernel.module_kv_count + f->kernel.blob_count; ++i) {
        const uint8_t *key, *value;
        size_t key_length, value_length;
        uint8_t blob_key[129] = {0};
        if (i < f->kernel.module_kv_count) {
            const lxp_module_kv_entry *entry = &f->kernel.module_kv[i];
            if (entry->module_id != LXP_MODULE_PROGRAMS) continue;
            key = entry->key; key_length = entry->key_length;
            value = entry->value; value_length = entry->value_length;
            if (key_length == 40 && memcmp(key, "program\0", 8) == 0 && memcmp(key + 8, f->program, 32) == 0) record = entry;
            if (key_length == 42 && memcmp(key, "interface\0", 10) == 0 && memcmp(key + 10, f->program, 32) == 0) interface = entry;
        } else {
            const lxp_module_blob *blob = &f->kernel.blobs[i - f->kernel.module_kv_count];
            if (blob->module_id != LXP_MODULE_PROGRAMS) continue;
            blob_key[0] = 255; memcpy(blob_key + 97, blob->key, 32);
            key = blob_key; key_length = sizeof(blob_key);
            value = blob->bytes; value_length = blob->length;
        }
        int order = interface_compare(key, key_length, status_key, sizeof(status_key));
        METERED_CHECK(order != 0);
        if (order < 0 && (lower_length == 0 || interface_compare(key, key_length, lower_key, lower_length) > 0)) {
            memcpy(lower_key, key, key_length); lower_length = key_length;
            lower_value = value; lower_value_length = value_length;
        }
        if (order > 0 && (upper_length == 0 || interface_compare(key, key_length, upper_key, upper_length) < 0)) {
            memcpy(upper_key, key, key_length); upper_length = key_length;
            upper_value = value; upper_value_length = value_length;
        }
    }
    METERED_CHECK(record != NULL && interface != NULL);
    METERED_CHECK(interface_leaf(file, "program_record", f, record->key, record->key_length,
        record->value, record->value_length, programs_root) == 0);
    METERED_CHECK(interface_leaf(file, "interface_record", f, interface->key, interface->key_length,
        interface->value, interface->value_length, programs_root) == 0);
    if (lower_length != 0) {
        METERED_CHECK(interface_leaf(file, "lifecycle_lower", f, lower_key, lower_length,
            lower_value, lower_value_length, programs_root) == 0);
    } else METERED_CHECK(fprintf(file, "lifecycle_lower=absent\n") > 0);
    if (upper_length != 0) {
        METERED_CHECK(interface_leaf(file, "lifecycle_upper", f, upper_key, upper_length,
            upper_value, upper_value_length, programs_root) == 0);
    } else METERED_CHECK(fprintf(file, "lifecycle_upper=absent\n") > 0);
    METERED_CHECK(fflush(file) == 0 && fsync(fileno(file)) == 0 && fclose(file) == 0);
    METERED_CHECK(printf("INTERFACE_CASE name=%s\n", name) > 0);
    return 0;
}

static int interface_read_file(const char *directory, const char *name, const char *file,
                                uint8_t *bytes, size_t capacity, size_t *length)
{
    char path[4096];
    FILE *input;
    METERED_CHECK(snprintf(path, sizeof(path), "%s/%s/%s", directory, name, file) > 0);
    input = fopen(path, "rb");
    METERED_CHECK(input != NULL);
    *length = fread(bytes, 1, capacity, input);
    METERED_CHECK(*length != 0 && *length < capacity && feof(input) && !ferror(input));
    METERED_CHECK(fclose(input) == 0);
    return 0;
}

static int interface_transition(metered_fixture *f, const char *inputs, const char *input_name,
                                 const char *output, const char *case_name,
                                 bool upgrade, bool breaking, lxp_result expected)
{
    uint8_t module[65536], description[953], payload[67000], hash[32], prior_hash[32];
    uint8_t before[32], after[32], activity_identifier[32];
    size_t module_length, description_length, length;
    lxp_byte_span canonical;
    lxp_batch_roots roots;
    uint16_t abi;
    METERED_CHECK(interface_read_file(inputs, input_name, "module.wasm", module, sizeof(module), &module_length) == 0);
    METERED_CHECK(interface_read_file(inputs, input_name, "interface.bin", description, sizeof(description), &description_length) == 0);
    METERED_CHECK(description_length >= 62);
    abi = (uint16_t)(((uint16_t)description[60] << 8) | description[61]);
    METERED_CHECK(lxp_hash_sha256(module, module_length, hash) == LXP_OK);
    METERED_CHECK(memcmp(description + 28, hash, 32) == 0);
    METERED_CHECK(lxp_state_subtree_root(&f->kernel, LXP_MODULE_PROGRAMS, before) == LXP_OK);
    memset(payload, 0, sizeof(payload));
    memcpy(payload, f->program, 32); write_u16(payload + 32, abi);
    if (upgrade) {
        bool found = false;
        for (size_t i = 0; i < f->kernel.module_kv_count; ++i) {
            const lxp_module_kv_entry *entry = &f->kernel.module_kv[i];
            if (entry->module_id == LXP_MODULE_PROGRAMS && entry->key_length == 40 &&
                memcmp(entry->key, "program\0", 8) == 0 && memcmp(entry->key + 8, f->program, 32) == 0) {
                METERED_CHECK(entry->value_length == 71);
                memcpy(prior_hash, entry->value + 33, 32); found = true;
            }
        }
        METERED_CHECK(found);
        payload[34] = breaking ? 2 : 0;
        memcpy(payload + 36, prior_hash, 32); memcpy(payload + 68, hash, 32);
        write_u32(payload + 102, (uint32_t)module_length);
        write_u32(payload + 106, (uint32_t)description_length);
        length = 110;
    } else {
        payload[34] = 1;
        memcpy(payload + 36, f->identity->did_id, 32); memcpy(payload + 68, hash, 32);
        write_u32(payload + 100, (uint32_t)module_length);
        write_u32(payload + 104, (uint32_t)description_length);
        length = 108;
    }
    memcpy(payload + length, description, description_length); length += description_length;
    memcpy(payload + length, module, module_length); length += module_length;
    METERED_CHECK(metered_activity(f, upgrade ? LX_PROGRAMS_UPGRADE : LX_PROGRAMS_DEPLOY,
        payload, length, (uint8_t)f->state.next_sequence, false) == 0);
    f->execution.batch_number = f->state.next_sequence;
    METERED_CHECK(lxp_activity_encode(&f->activity, &f->arena, &canonical) == LXP_OK);
    METERED_CHECK(lxp_batch_roots_compute(&(lxp_batch_root_inputs){&canonical, 1,
        NULL, 0, NULL, 0, NULL, 0, NULL, 0}, &f->arena, &roots) == LXP_OK);
    METERED_CHECK(lxp_activity_id(canonical.bytes, canonical.length, activity_identifier) == LXP_OK);
    METERED_CHECK(lxp_batch_identity_activity(f->kernel.current_state_root,
        activity_identifier, f->execution.global_sequence, f->execution.batch_number,
        f->execution.batch_id) == LXP_OK);
    memcpy(f->execution.activity_root, roots.activity_merkle_root, 32);
    METERED_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity, &f->execution, &f->receipt) == LXP_OK);
    if (f->receipt.result_code != expected)
        fprintf(stderr, "interface case=%s result=%d expected=%d\n", case_name, f->receipt.result_code, expected);
    METERED_CHECK(f->receipt.result_code == expected);
    METERED_CHECK(lxp_state_subtree_root(&f->kernel, LXP_MODULE_PROGRAMS, after) == LXP_OK);
    if (expected != LXP_OK) METERED_CHECK(memcmp(before, after, 32) == 0);
    return interface_export(output, case_name, f);
}

int main(int argc, char **argv)
{
    const char *inputs = NULL, *output = NULL;
    static const char *cases[] = {"abi1", "abi2", "abi2-dynamic", "abi3", "abi3-dynamic", "abi4", "abi4-dynamic"};
    for (int i = 1; i < argc; i += 2) {
        METERED_CHECK(i + 1 < argc);
        if (strcmp(argv[i], "--input") == 0) inputs = argv[i + 1];
        else if (strcmp(argv[i], "--output") == 0) output = argv[i + 1];
        else return 2;
    }
    METERED_CHECK(output != NULL);
    if (inputs == NULL) inputs = output;
    METERED_CHECK(mkdir(output, 0700) == 0 || errno == EEXIST);
    for (size_t i = 0; i < sizeof(cases) / sizeof(cases[0]); ++i) {
        metered_fixture *f = calloc(1, sizeof(*f));
        METERED_CHECK(f != NULL && cache_fixture_init(f) == 0);
        f->program[31] = (uint8_t)(i + 1);
        METERED_CHECK(interface_transition(f, inputs, cases[i], output, cases[i], false, false, LXP_OK) == 0);
        if (i == 1) {
            METERED_CHECK(interface_transition(f, inputs, "abi2-widening", output,
                "upgrade-widening", true, false, LXP_OK) == 0);
            METERED_CHECK(interface_transition(f, inputs, "abi2-narrowing", output,
                "upgrade-narrowing", true, false, LXP_ERR_NON_CANONICAL) == 0);
            METERED_CHECK(interface_transition(f, inputs, "abi2-narrowing", output,
                "upgrade-breaking", true, true, LXP_OK) == 0);
            METERED_CHECK(interface_transition(f, inputs, "abi1", output,
                "upgrade-downgrade", true, true, LXP_ERR_VERSION_UNSUPPORTED) == 0);
        }
        while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
        METERED_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
        free(f);
    }
    METERED_CHECK(printf("INTERFACE_NATIVE tests=11 skipped=0\n") > 0);
    return 0;
}
