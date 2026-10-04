#define main spend_reference_main
int spend_reference_main(int argc, char **argv);
#include "test_call_activity.c"
#undef main
#include "layerx/lxp_batch_identity.h"
#include <sys/stat.h>
#include <fcntl.h>
#include <errno.h>

#define SPEND_CHECK(c) do { if (!(c)) { fprintf(stderr, "Spend composition line %d\n", __LINE__); return 1; } } while (0)

typedef struct spend_fixture {
    lxp_kernel kernel;
    lxp_state_store state;
    lxp_state_journal journal;
    lx_account_registry accounts;
    lxp_identity_store identities;
    lxp_identity *identity;
    lx_account *actor, *payee, *source;
    lxp_transfer_asset_state asset;
    lx_programs_transfer_runtime runtime;
    lxp_fee_params fees;
    lxp_arena arena;
    uint8_t storage[8U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t key[32], owner[32], child[32], descendant[32], source_id[32];
    uint8_t signature[64];
    lxp_activity activity;
    lxp_authority_grant grant;
    lxp_authority_resolved authority;
    lxp_kernel_execution execution;
    lxp_receipt receipt;
    uint64_t parameters;
} spend_fixture;

static const uint8_t spend_seed[32] = {0x57U};
static const uint8_t spend_did[] = "did:lxp:composition-spend";
static const uint8_t account_seed[] = "composition/vault";
static const char *guest_directory;
static const char *result_directory;

static void spend_i32(uint8_t *bytes, size_t *length, uint32_t value)
{
    bytes[(*length)++] = 0x41U;
    do {
        uint8_t byte = (uint8_t)(value & 127U);
        value >>= 7U;
        if (value != 0U || (byte & 64U) != 0U) byte |= 128U;
        bytes[(*length)++] = byte;
        if (value == 0U && (byte & 128U) != 0U) { bytes[(*length)++] = 0U; break; }
    } while (value != 0U);
}

static size_t spend_guest(uint8_t *out, const spend_fixture *f,
    const uint8_t *requested, size_t requested_length, unsigned int visits, bool transfer, unsigned int variant, const uint8_t target[32])
{
    static const uint8_t header[] = {0U,97U,115U,109U,1U,0U,0U,0U};
    static const uint8_t types[] = {4U,
        0x60U,10U,0x7eU,0x7eU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7fU,
        0x60U,8U,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7eU,
        0x60U,1U,0x7fU,1U,0x7fU,0x60U,2U,0x7fU,0x7fU,1U,0x7fU};
    static const uint8_t functions[] = {2U,2U,3U};
    static const uint8_t memory[] = {1U,1U,1U,1U};
    uint8_t section[4096], body[2048], escalated[512];
    size_t escalated_length = program_spend_capabilities(escalated,f->owner,account_seed,
        sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,21U);
    size_t cursor = 0U, length = 0U, body_length = 0U;
    append_bytes(out, &cursor, header, sizeof(header));
    append_section(out, &cursor, 1U, types, sizeof(types));
    section[length++] = 2U;
    append_name(section, &length, "layerx_v2"); append_name(section, &length, "transfer_program_402");
    section[length++] = 0U; section[length++] = 0U;
    append_name(section, &length, "layerx_v2"); append_name(section, &length, "program_call_response");
    section[length++] = 0U; section[length++] = 1U;
    append_section(out, &cursor, 2U, section, length);
    append_section(out, &cursor, 3U, functions, sizeof(functions));
    append_section(out, &cursor, 5U, memory, sizeof(memory));
    length = 0U; section[length++] = 3U;
    append_name(section, &length, "layerx_reserve"); section[length++] = 0U; section[length++] = 2U;
    append_name(section, &length, "layerx_call"); section[length++] = 0U; section[length++] = 3U;
    append_name(section, &length, "memory"); section[length++] = 2U; section[length++] = 0U;
    append_section(out, &cursor, 7U, section, length);
    body[body_length++] = 0U;
    if (transfer) {
        body[body_length++] = 0x42U; body[body_length++] = 0U;
        body[body_length++] = 0x42U; body[body_length++] = 7U;
        spend_i32(body, &body_length, 0U); spend_i32(body, &body_length, sizeof(account_seed) - 1U);
        for (uint32_t offset = 128U; offset <= 192U; offset += 32U) {
            spend_i32(body, &body_length, offset); spend_i32(body, &body_length, 32U);
        }
        body[body_length++] = 0x10U; body[body_length++] = 0U; body[body_length++] = 0x1aU;
    }
    for (unsigned int visit = 0U; visit < visits; ++visit) {
        bool last = visit + 1U == visits && (variant == 6U || variant == 7U);
        const uint32_t args[] = {last && variant == 6U ? 288U : 256U,32U,0U,0U,
            last ? 1024U : 512U,last ? (uint32_t)escalated_length : (uint32_t)requested_length,1536U,0U};
        for (size_t i = 0U; i < sizeof(args) / sizeof(args[0]); ++i)
            spend_i32(body, &body_length, args[i]);
        body[body_length++] = 0x10U; body[body_length++] = 1U; body[body_length++] = 0x1aU;
    }
    spend_i32(body, &body_length, 0U); body[body_length++] = 0x0bU;
    length = 0U; section[length++] = 2U;
    section[length++] = 4U; section[length++] = 0U; section[length++] = 0x41U;
    section[length++] = 0U; section[length++] = 0x0bU;
    append_u32_leb(section, &length, (uint32_t)body_length); append_bytes(section, &length, body, body_length);
    append_section(out, &cursor, 10U, section, length);
    length = 0U; section[length++] = 8U;
    const uint8_t *segments[] = {account_seed,f->source_id,f->asset.asset_id,f->payee->id,target,requested,f->descendant,escalated};
    const uint32_t offsets[] = {0U,128U,160U,192U,256U,512U,288U,1024U};
    const size_t lengths[] = {sizeof(account_seed)-1U,32U,32U,32U,32U,requested_length,32U,escalated_length};
    for (size_t i = 0U; i < 8U; ++i) {
        section[length++] = 0U; spend_i32(section, &length, offsets[i]); section[length++] = 0x0bU;
        append_u32_leb(section, &length, (uint32_t)lengths[i]);
        append_bytes(section, &length, segments[i], lengths[i]);
    }
    append_section(out, &cursor, 11U, section, length);
    return cursor;
}

static int spend_sign(spend_fixture *f, uint32_t type, const uint8_t *payload,
    size_t length, uint8_t marker)
{
    uint8_t digest[32];
    SPEND_CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    fill_activity(&f->activity, type, payload, length, spend_did,
        sizeof(spend_did) - 1U, f->key);
    f->activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    f->activity.account_sequence = f->identity->next_sequence;
    f->activity.idempotency_key[31] = marker;
    f->activity.fee_limit = (lxp_u128){0U,67108864U};
    f->activity.signature = (lxp_byte_span){f->signature,64U};
    SPEND_CHECK(lxp_hash_payload(payload, length, f->activity.payload_hash) == LXP_OK);
    SPEND_CHECK(lxp_activity_signing_preimage(&f->activity, digest) == LXP_OK);
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519,NULL,spend_seed,32U);
    EVP_MD_CTX *signer = EVP_MD_CTX_new();
    size_t signature_length = 64U;
    SPEND_CHECK(key != NULL && signer != NULL &&
        EVP_DigestSignInit(signer,NULL,NULL,NULL,key) == 1 &&
        EVP_DigestSign(signer,f->signature,&signature_length,digest,32U) == 1);
    EVP_MD_CTX_free(signer); EVP_PKEY_free(key);
    SPEND_CHECK(signature_length == 64U && lxp_activity_verify_signature(&f->activity) == LXP_OK);
    SPEND_CHECK(lxp_authority_resolve_activity(&f->kernel,f->identity,&f->activity,
        lxp_identity_key_valid(f->identity,f->key,10U,f->state.next_sequence),true,10U,100U,f->state.next_sequence,&f->grant,&f->authority) == LXP_OK);
    (void)memset(&f->execution,0,sizeof(f->execution));
    f->execution.network_id = 7U; f->execution.batch_number = 1U;
    f->execution.batch_timestamp_ms = 10U; f->execution.maximum_timestamp_window = 100U;
    f->execution.global_sequence = f->state.next_sequence;
    f->execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    f->execution.parameter_version = 1U; f->execution.signature_valid = true;
    f->execution.identities = &f->identities; f->execution.authority = &f->authority;
    f->execution.fee_parameters = &f->fees; f->execution.fee_balance = f->actor->balance;
    f->execution.gas_limit = 1000000U; f->execution.arena = &f->arena;
    f->execution.sequencer_private_key = executed_sequencer_seed;
    return 0;
}

static int spend_init(spend_fixture *f)
{
    static const char *names[] = {"agent:did:lxp:composition-spend:main",
        "agent:did:lxp:composition-payee:main","system:fees"};
    lx_account *account;
    uint8_t id[32];
    lxp_genesis_manifest *manifest = calloc(1U,sizeof(*manifest));
    lx_programs_metering_schedule metering = {0};
    lxp_byte_span genesis_preimage;
    SPEND_CHECK(manifest != NULL && lxp_arena_init(&f->arena,f->storage,sizeof(f->storage)) == LXP_OK);
    lx_programs_fee_genesis_parameters prices = {0};
    SPEND_CHECK(executed_public_key(spend_seed,f->key) == 0);
    f->owner[0] = 0x61U; f->child[0] = 0x62U; f->descendant[0] = 0x63U; f->asset.asset_id[0] = 9U;
    f->asset.registered = true; f->parameters = 1U;
    SPEND_CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    for (size_t i = 0U; i < 3U; ++i) {
        SPEND_CHECK(lx_account_id_from_string((const uint8_t *)names[i],strlen(names[i]),id) == LXP_OK);
        SPEND_CHECK(lx_account_open(&f->accounts,(const uint8_t *)names[i],strlen(names[i]),id,
            i == 2U ? 2U : 1U,LX_ACCOUNT_OPEN_GENESIS,NULL,&account) == LXP_OK);
        SPEND_CHECK(lxp_ledger_bootstrap_balance(account,f->asset.asset_id,
            (lxp_u128){0U,i == 0U ? UINT64_C(1000000000) : 0U},0U) == LXP_OK);
        if (i == 0U) f->actor = account;
        if (i == 1U) f->payee = account;
    }
    SPEND_CHECK(lxp_state_store_init(&f->state,1U) == LXP_OK &&
        lxp_state_store_bind_accounts(&f->state,&f->accounts) == LXP_OK &&
        lxp_kernel_create(&f->kernel,&f->state,&f->journal,&f->parameters,0U) == LXP_OK &&
        lxp_kernel_register_module(&f->kernel,programs_module_registration_v4()) == LXP_OK);
    prices.schedule = (lx_programs_fee_schedule){1U,1U,1U,2U,4U,1U,1U,1U};
    (void)memcpy(prices.occupancy_asset_id,f->asset.asset_id,32U);
    prices.target_occupancy_byte_batches = 3U; prices.response_denominator = 1U;
    prices.maximum_change_numerator = 1U; prices.maximum_change_denominator = 1U;
    prices.minimum_fee_units_per_occupancy_byte_batch = 1U; prices.maximum_fee_units_per_occupancy_byte_batch = 10U;
    manifest->protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    manifest->network_id = 7U; manifest->genesis_timestamp_ms = 1U;
    manifest->parameter_count = 1U;
    manifest->parameters[0].module_id = LXP_MODULE_GOVERNANCE;
    memcpy(manifest->parameters[0].key,"parameter-version",17U);
    manifest->parameters[0].value[31] = 1U;
    manifest->guarantor_count = 1U; manifest->guarantors[0].guarantor_id[0] = 1U;
    memcpy(manifest->guarantors[0].public_key,f->key,32U);
    memcpy(manifest->signer_public_key,f->key,32U);
    metering.version = LXP_PROGRAM_METERING_SCHEDULE_VERSION_V1;
    for (size_t i = 0U; i < 5U; ++i) metering.coefficients[i] = 1U;
    metering.coefficients[5] = 8U; metering.coefficients[6] = 8U;
    metering.coefficients[7] = 64U; metering.coefficients[8] = 8U;
    metering.activation_batch = 1U; metering.authority_kind = LX_PROGRAMS_METERING_AUTHORITY_GENESIS;
    SPEND_CHECK(lxp_hash_payload(f->key,32U,metering.authority_digest) == LXP_OK &&
        lxp_genesis_fresh_empty_accounts(manifest,f->asset.asset_id) == LXP_OK &&
        lxp_programs_metering_genesis_append(manifest,&metering) == LXP_OK &&
        lxp_programs_fee_genesis_append(manifest,&prices) == LXP_OK &&
        lxp_genesis_state_root(manifest,&f->arena,manifest->genesis_state_root) == LXP_OK &&
        lxp_genesis_receipt_state_root(manifest->network_id,manifest->genesis_state_root,
            manifest->genesis_receipt_state_root) == LXP_OK &&
        lxp_genesis_encode(manifest,false,&f->arena,&genesis_preimage) == LXP_OK);
    EVP_PKEY *genesis_key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519,NULL,spend_seed,32U);
    EVP_MD_CTX *genesis_signer = EVP_MD_CTX_new();
    size_t signature_length = 64U;
    SPEND_CHECK(genesis_key != NULL && genesis_signer != NULL &&
        EVP_DigestSignInit(genesis_signer,NULL,NULL,NULL,genesis_key) == 1 &&
        EVP_DigestSign(genesis_signer,manifest->signature,&signature_length,
            genesis_preimage.bytes,genesis_preimage.length) == 1 && signature_length == 64U);
    EVP_MD_CTX_free(genesis_signer); EVP_PKEY_free(genesis_key);
    SPEND_CHECK(lxp_arena_reset(&f->arena,0U) == LXP_OK &&
        lxp_genesis_verify_signature(manifest,&f->arena) == LXP_OK &&
        lxp_arena_reset(&f->arena,0U) == LXP_OK &&
        lxp_programs_metering_genesis_project(manifest,&f->arena,&f->kernel) == LXP_OK &&
        lxp_arena_reset(&f->arena,0U) == LXP_OK &&
        lxp_programs_fee_genesis_project(manifest,&f->arena,&f->kernel) == LXP_OK);
    free(manifest);
    f->runtime.accounts = &f->accounts; f->runtime.assets = &f->asset; f->runtime.asset_count = 1U;
    f->runtime.resolve_occupancy_parameters = lxp_programs_fee_governance_resolve_runtime;
    f->runtime.occupancy_parameter_context = &f->kernel;
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    (void)memcpy(f->runtime.occupancy_asset_id,f->asset.asset_id,32U);
    SPEND_CHECK(lxp_kernel_bind_module_runtime(&f->kernel,LXP_MODULE_PROGRAMS,&f->runtime) == LXP_OK &&
        lxp_programs_bind_fee_transaction(&f->kernel) == LXP_OK &&
        lxp_kernel_set_capabilities(&f->kernel,NULL,lxp_kernel_canonical_ledger_apply) == LXP_OK &&
        lxp_identity_register(&f->identities,spend_did,sizeof(spend_did)-1U,f->key,&f->identity) == LXP_OK &&
        lxp_arena_init(&f->arena,f->storage,sizeof(f->storage)) == LXP_OK);
    f->fees.version = 1U; f->fees.multiplier_basis_points = 10000U;
    SPEND_CHECK(lxp_programs_account_derive(f->owner,account_seed,sizeof(account_seed)-1U,f->source_id) == LXP_OK);
    return 0;
}

static int spend_deploy(spend_fixture *f, const uint8_t program[32],
    const uint8_t *wasm, size_t length, uint8_t marker)
{
    uint8_t payload[8192], hash[32];
    size_t size = program_spend_deploy_payload(payload,program,f->identity->did_id,wasm,length,hash);
    SPEND_CHECK(lxp_state_root(&f->kernel,f->kernel.current_state_root) == LXP_OK &&
        spend_sign(f,LX_PROGRAMS_DEPLOY,payload,size,marker) == 0 &&
        lxp_kernel_execute_activity(&f->kernel,&f->activity,&f->execution,&f->receipt) == LXP_OK &&
        f->receipt.result_code == LXP_OK);
    return 0;
}

static void spend_guests(const spend_fixture *f, unsigned int variant,
    uint8_t guest[8192], uint8_t child[8192], uint8_t descendant[8192], size_t lengths[3])
{
    uint8_t requested[512], grant[512], widening[512];
    size_t requested_length = program_spend_capabilities(requested,f->owner,account_seed,
        sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,variant == 1U ? 21U : 10U);
    if (variant == 2U) requested[3U+32U+2U+sizeof(account_seed)-1U+32U] ^= 1U;
    if (variant == 3U) requested[3U+32U+2U+sizeof(account_seed)-1U+64U] ^= 1U;
    if (variant == 5U) {
        memcpy(grant,requested,requested_length);
        write_u16(requested,2U); requested[2] = 4U;
        memcpy(requested+3U,f->descendant,32U);
        memcpy(requested+35U,grant+2U,requested_length-2U);
        requested_length += 33U;
    }
    unsigned int visits = variant == 4U || variant == 7U ? 8U : variant == 6U ? 2U : 1U;
    lengths[0] = spend_guest(guest,f,requested,requested_length,visits,true,variant,f->child);
    if (variant == 5U) {
        size_t widening_length = program_spend_capabilities(widening,f->owner,account_seed,
            sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,21U);
        lengths[1] = spend_guest(child,f,widening,widening_length,1U,false,0U,f->descendant);
    } else {
        lengths[1] = spend_guest(child,f,requested,requested_length,0U,false,0U,f->child);
    }
    lengths[2] = spend_guest(descendant,f,requested,requested_length,0U,false,0U,f->child);
}

static int spend_case(unsigned int variant)
{
    spend_fixture *f = calloc(1U,sizeof(*f));
    uint8_t grants[512], spend_grant[512], guest[8192], child[8192], descendant[8192], call[2048];
    SPEND_CHECK(f != NULL && spend_init(f) == 0);
    size_t guest_lengths[3];
    spend_guests(f,variant,guest,child,descendant,guest_lengths);
    char guest_path[4096];
    uint8_t saved[8192];
    const uint8_t *expected_bytes[] = {guest,child,descendant};
    const size_t *expected_lengths = guest_lengths;
    for (unsigned int module = 0U; module < 3U; ++module) {
        int count = snprintf(guest_path,sizeof(guest_path),"%s/case%u.%s.wasm",guest_directory,
            variant,module == 0U ? "owner" : module == 1U ? "child" : "descendant");
        SPEND_CHECK(count > 0 && (size_t)count < sizeof(guest_path));
        FILE *input = fopen(guest_path,"rb");
        SPEND_CHECK(input != NULL && fread(saved,1U,sizeof(saved),input) == expected_lengths[module] &&
            memcmp(saved,expected_bytes[module],expected_lengths[module]) == 0 && fgetc(input) == EOF && fclose(input) == 0);
    }
    SPEND_CHECK(spend_deploy(f,f->descendant,descendant,guest_lengths[2],1U) == 0 &&
        spend_deploy(f,f->child,child,guest_lengths[1],2U) == 0 &&
        spend_deploy(f,f->owner,guest,guest_lengths[0],3U) == 0);
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    bool created = false;
    SPEND_CHECK(lxp_arena_reset(&f->arena,0U) == LXP_OK &&
        lxp_state_journal_open(&f->state,f->state.next_sequence,&f->journal) == LXP_OK &&
        lxp_module_ctx_init(&ctx,&f->kernel,LXP_MODULE_PROGRAMS,10U,0U,
            f->state.next_sequence,100000U,&f->arena,true) == LXP_OK);
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    SPEND_CHECK(lxp_effect_buffer_init(&effects) == LXP_OK && lxp_module_ctx_bind_effects(&ctx,&effects) == LXP_OK &&
        lxp_programs_account_register(&ctx,f->owner,account_seed,sizeof(account_seed)-1U,
            f->asset.asset_id,&f->source,&created) == LXP_OK && created &&
        lxp_module_ctx_prepare_commit(&ctx) == LXP_OK && lxp_state_journal_commit(&f->journal) == LXP_OK &&
        lxp_module_ctx_commit(&ctx) == LXP_OK);
    f->source = program_spend_account(&f->accounts,f->source_id);
    SPEND_CHECK(f->source != NULL && lxp_ledger_bootstrap_balance(f->source,f->asset.asset_id,(lxp_u128){0U,40U},0U) == LXP_OK);
    size_t length = program_spend_capabilities(spend_grant,f->owner,account_seed,sizeof(account_seed)-1U,
        f->source_id,f->asset.asset_id,f->payee->id,20U);
    write_u16(grants,3U); grants[2] = 4U; (void)memcpy(grants+3U,f->child,32U);
    grants[35U] = 4U; (void)memcpy(grants+36U,f->descendant,32U);
    (void)memcpy(grants+68U,spend_grant+2U,length-2U);
    size_t call_length = call_payload_with_capabilities(call,f->owner,grants,68U+length-2U);
    write_u16(call+32U,LX_PROGRAMS_ACCOUNT_ABI_VERSION);
    lxp_u128 actor_before = f->actor->balance;
    uint64_t source_sequence = f->source->next_sequence;
    SPEND_CHECK(lxp_state_root(&f->kernel,f->kernel.current_state_root) == LXP_OK &&
        spend_sign(f,LX_PROGRAMS_CALL,call,call_length,4U) == 0 &&
        lxp_kernel_execute_activity(&f->kernel,&f->activity,&f->execution,&f->receipt) == LXP_OK);
    bool refused = (variant >= 1U && variant <= 3U) || variant >= 5U;
    SPEND_CHECK(f->receipt.result_code == (refused ? LXP_ERR_PROGRAM_REFUSED : LXP_OK));
    SPEND_CHECK(f->source->balance.lo == (refused ? 40U : 33U) && f->payee->balance.lo == (refused ? 0U : 7U));
    lxp_u128 actor_after;
    SPEND_CHECK(lxp_u128_sub(actor_before,f->receipt.fee_charged,&actor_after) == LXP_OK &&
        lxp_u128_cmp(actor_after,f->actor->balance) == 0 &&
        f->source->next_sequence == source_sequence);
    SPEND_CHECK(f->receipt.program_outcome.present &&
        (refused ? lxp_ct_is_zero(f->receipt.program_outcome.transfer_root,32U) :
            !lxp_ct_is_zero(f->receipt.program_outcome.transfer_root,32U)));
    uint8_t public_key[32];
    SPEND_CHECK(executed_public_key(executed_sequencer_seed,public_key) == 0 &&
        lxp_receipt_verify(&f->receipt,public_key,&f->arena) == LXP_OK);
    SPEND_CHECK(f->receipt.program_outcome.terminal_kind == (refused ? LXP_PROGRAM_TERMINAL_FAILURE : LXP_PROGRAM_TERMINAL_SUCCESS));
    char terminal_path[4096];
    int path_length = snprintf(terminal_path,sizeof(terminal_path),"%s/case%u.terminal.bin",result_directory,variant);
    SPEND_CHECK(path_length > 0 && (size_t)path_length < sizeof(terminal_path));
    int output = open(terminal_path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);
    lxp_byte_span terminal = f->receipt.program_outcome.terminal_payload;
    SPEND_CHECK(output >= 0 && terminal.bytes != NULL && terminal.length != 0U &&
        write(output,terminal.bytes,terminal.length) == (ssize_t)terminal.length && fsync(output) == 0 && close(output) == 0);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    SPEND_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    lx_account_registry_release(&f->accounts); free(f);
    return 0;
}


#define SPEND_PROPOSAL_PREFIX_BYTES (140U + sizeof(account_seed) - 1U)
#define SPEND_PROPOSAL_BYTES (156U + sizeof(account_seed) - 1U)

static size_t spend_proposal_encode(uint8_t *out, const spend_fixture *f, uint64_t amount)
{
    (void)memcpy(out,"LXPPRP01",8U);
    write_u16(out+8U,1U);
    (void)memcpy(out+10U,f->owner,32U);
    write_u16(out+42U,(uint16_t)(sizeof(account_seed)-1U));
    (void)memcpy(out+44U,account_seed,sizeof(account_seed)-1U);
    (void)memcpy(out+44U+sizeof(account_seed)-1U,f->source_id,32U);
    (void)memcpy(out+76U+sizeof(account_seed)-1U,f->asset.asset_id,32U);
    (void)memcpy(out+108U+sizeof(account_seed)-1U,f->payee->id,32U);
    write_u64(out+SPEND_PROPOSAL_PREFIX_BYTES,0U);
    write_u64(out+SPEND_PROPOSAL_PREFIX_BYTES+8U,amount);
    return SPEND_PROPOSAL_BYTES;
}

static void spend_proposal_i64(uint8_t *body, size_t *length, uint64_t value)
{
    body[(*length)++] = 0x42U;
    do {
        uint8_t byte = (uint8_t)(value & 127U);
        value >>= 7U;
        if (value != 0U || (byte & 64U) != 0U) byte |= 128U;
        body[(*length)++] = byte;
        if (value == 0U && (byte & 128U) != 0U) { body[(*length)++] = 0U; break; }
    } while (value != 0U);
}

static void spend_proposal_exact_response(uint8_t *body, size_t *length)
{
    spend_proposal_i64(body,length,SPEND_PROPOSAL_BYTES);
    body[(*length)++] = 0x52U;
    body[(*length)++] = 0x04U; body[(*length)++] = 0x40U;
    body[(*length)++] = 0x00U; body[(*length)++] = 0x0bU;
}

static void spend_proposal_read_amount(uint8_t *body, size_t *length, uint32_t offset)
{
    spend_proposal_i64(body,length,0U);
    for (uint32_t i = 0U; i < 8U; ++i) {
        spend_proposal_i64(body,length,8U); body[(*length)++] = 0x86U;
        spend_i32(body,length,offset+i);
        body[(*length)++] = 0x31U; body[(*length)++] = 0U; body[(*length)++] = 0U;
        body[(*length)++] = 0x84U;
    }
}

static size_t spend_proposal_guest(uint8_t *out, const spend_fixture *f,
    const uint8_t *requested, size_t requested_length, unsigned int role,
    unsigned int visits, uint64_t amount, const uint8_t target[32])
{
    static const uint8_t header[] = {0U,97U,115U,109U,1U,0U,0U,0U};
    static const uint8_t types[] = {5U,
        0x60U,10U,0x7eU,0x7eU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7fU,
        0x60U,8U,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,0x7fU,1U,0x7eU,
        0x60U,1U,0x7fU,1U,0x7fU,0x60U,2U,0x7fU,0x7fU,1U,0x7fU,
        0x60U,3U,0x7fU,0x7fU,0x7fU,1U,0x7fU};
    static const uint8_t functions[] = {2U,2U,3U};
    static const uint8_t memory[] = {1U,1U,1U,1U};
    uint8_t section[16384], body[8192], proposal[SPEND_PROPOSAL_BYTES], alternate[512];
    size_t cursor=0U,length=0U,body_length=0U;
    size_t proposal_length=spend_proposal_encode(proposal,f,amount);
    size_t alternate_length=program_spend_capabilities(alternate,f->owner,account_seed,
        sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,14U);
    append_bytes(out,&cursor,header,sizeof(header));
    append_section(out,&cursor,1U,types,sizeof(types));
    section[length++]=3U;
    append_name(section,&length,"layerx_v2"); append_name(section,&length,"transfer_program_402");
    section[length++]=0U; section[length++]=0U;
    append_name(section,&length,"layerx_v2"); append_name(section,&length,"program_call_response");
    section[length++]=0U; section[length++]=1U;
    append_name(section,&length,"layerx_v2"); append_name(section,&length,"response_write");
    section[length++]=0U; section[length++]=4U;
    append_section(out,&cursor,2U,section,length);
    append_section(out,&cursor,3U,functions,sizeof(functions));
    append_section(out,&cursor,5U,memory,sizeof(memory));
    length=0U; section[length++]=3U;
    append_name(section,&length,"layerx_reserve"); section[length++]=0U; section[length++]=3U;
    append_name(section,&length,"layerx_call"); section[length++]=0U; section[length++]=4U;
    append_name(section,&length,"memory"); section[length++]=2U; section[length++]=0U;
    append_section(out,&cursor,7U,section,length);
    body[body_length++]=1U; body[body_length++]=1U; body[body_length++]=0x7fU;
    for (unsigned int visit=0U; visit<visits; ++visit) {
        const uint32_t args[]={visit==1U && role==0U && visits==2U ? 288U : 256U,
            32U,0U,0U,visit==1U && role==0U && visits==2U && amount==7U ? 1536U : 512U,
            visit==1U && role==0U && visits==2U && amount==7U ? (uint32_t)alternate_length : (uint32_t)requested_length,
            2048U,SPEND_PROPOSAL_BYTES};
        for (size_t i=0U;i<sizeof(args)/sizeof(args[0]);++i) spend_i32(body,&body_length,args[i]);
        body[body_length++]=0x10U; body[body_length++]=1U;
        spend_proposal_exact_response(body,&body_length);
        if (role!=1U) {
            spend_i32(body,&body_length,0U); body[body_length++]=0x21U; body[body_length++]=2U;
            body[body_length++]=0x02U; body[body_length++]=0x40U;
            body[body_length++]=0x03U; body[body_length++]=0x40U;
            body[body_length++]=0x20U; body[body_length++]=2U;
            spend_i32(body,&body_length,SPEND_PROPOSAL_PREFIX_BYTES);
            body[body_length++]=0x4fU; body[body_length++]=0x0dU; body[body_length++]=1U;
            spend_i32(body,&body_length,2048U); body[body_length++]=0x20U; body[body_length++]=2U;
            body[body_length++]=0x6aU; body[body_length++]=0x2dU; body[body_length++]=0U; body[body_length++]=0U;
            spend_i32(body,&body_length,1024U); body[body_length++]=0x20U; body[body_length++]=2U;
            body[body_length++]=0x6aU; body[body_length++]=0x2dU; body[body_length++]=0U; body[body_length++]=0U;
            body[body_length++]=0x47U; body[body_length++]=0x04U; body[body_length++]=0x40U;
            body[body_length++]=0U; body[body_length++]=0x0bU;
            body[body_length++]=0x20U; body[body_length++]=2U; spend_i32(body,&body_length,1U);
            body[body_length++]=0x6aU; body[body_length++]=0x21U; body[body_length++]=2U;
            body[body_length++]=0x0cU; body[body_length++]=0U;
            body[body_length++]=0x0bU; body[body_length++]=0x0bU;
            spend_proposal_read_amount(body,&body_length,2048U+SPEND_PROPOSAL_PREFIX_BYTES);
            body[body_length++]=0x50U; body[body_length++]=0x45U;
            body[body_length++]=0x04U; body[body_length++]=0x40U;
            body[body_length++]=0U; body[body_length++]=0x0bU;
            spend_proposal_read_amount(body,&body_length,2048U+SPEND_PROPOSAL_PREFIX_BYTES+8U);
            spend_proposal_i64(body,&body_length,role==2U ? 7U :
                visit==1U && visits==2U && amount==7U ? 14U : 10U);
            body[body_length++]=0x56U;
            body[body_length++]=0x04U; body[body_length++]=0x40U;
            body[body_length++]=0U; body[body_length++]=0x0bU;
            if (role==0U) {
            spend_proposal_read_amount(body,&body_length,2048U+SPEND_PROPOSAL_PREFIX_BYTES);
            spend_proposal_read_amount(body,&body_length,2048U+SPEND_PROPOSAL_PREFIX_BYTES+8U);
            const uint32_t transfer_args[]={2048U+44U,(uint32_t)(sizeof(account_seed)-1U),
                2048U+44U+(uint32_t)(sizeof(account_seed)-1U),32U,
                2048U+76U+(uint32_t)(sizeof(account_seed)-1U),32U,
                2048U+108U+(uint32_t)(sizeof(account_seed)-1U),32U};
            for (size_t i=0U;i<sizeof(transfer_args)/sizeof(transfer_args[0]);++i)
                spend_i32(body,&body_length,transfer_args[i]);
            body[body_length++]=0x10U; body[body_length++]=0U; body[body_length++]=0x1aU;
            }
        }
    }
    if (role!=0U) {
        spend_i32(body,&body_length,0U); spend_i32(body,&body_length,role==2U ? 2048U : 1024U);
        spend_i32(body,&body_length,SPEND_PROPOSAL_BYTES);
        body[body_length++]=0x10U; body[body_length++]=2U;
        body[body_length++]=0x04U; body[body_length++]=0x40U;
        body[body_length++]=0U; body[body_length++]=0x0bU;
    }
    spend_i32(body,&body_length,0U); body[body_length++]=0x0bU;
    length=0U; section[length++]=2U;
    section[length++]=4U; section[length++]=0U; section[length++]=0x41U; section[length++]=0U; section[length++]=0x0bU;
    append_u32_leb(section,&length,(uint32_t)body_length); append_bytes(section,&length,body,body_length);
    append_section(out,&cursor,10U,section,length);
    length=0U; section[length++]=5U;
    const uint8_t *segments[]={target,requested,proposal,f->descendant,alternate};
    const uint32_t offsets[]={256U,512U,1024U,288U,1536U};
    const size_t lengths[]={32U,requested_length,proposal_length,32U,alternate_length};
    for(size_t i=0U;i<5U;++i){
        section[length++]=0U; spend_i32(section,&length,offsets[i]); section[length++]=0x0bU;
        append_u32_leb(section,&length,(uint32_t)lengths[i]); append_bytes(section,&length,segments[i],lengths[i]);
    }
    append_section(out,&cursor,11U,section,length);
    return cursor;
}

static void spend_proposal_guests(const spend_fixture *f,unsigned int variant,
    uint8_t guest[8192],uint8_t child[8192],uint8_t descendant[8192],size_t lengths[3])
{
    uint8_t requested[512],leaf_grant[512],nested[512];
    uint64_t amount=variant==11U ? 1U : variant==10U ? 4U : 7U;
    size_t requested_length=program_spend_capabilities(requested,f->owner,account_seed,
        sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,10U);
    size_t leaf_length=program_spend_capabilities(leaf_grant,f->owner,account_seed,
        sizeof(account_seed)-1U,f->source_id,f->asset.asset_id,f->payee->id,7U);
    if(variant==9U){
        write_u16(nested,2U); nested[2]=4U; memcpy(nested+3U,f->descendant,32U);
        memcpy(nested+35U,requested+2U,requested_length-2U);
        memcpy(requested,nested,requested_length+33U); requested_length+=33U;
    }
    unsigned int visits=variant==11U ? 8U : variant==10U || variant==12U ? 2U : 1U;
    lengths[0]=spend_proposal_guest(guest,f,requested,requested_length,0U,visits,amount,f->child);
    lengths[1]=spend_proposal_guest(child,f,leaf_grant,leaf_length,variant==9U ? 2U : 1U,
        variant==9U ? 1U : 0U,amount,f->descendant);
    lengths[2]=spend_proposal_guest(descendant,f,leaf_grant,leaf_length,1U,0U,
        variant==10U ? 3U : variant==12U ? 14U : amount,f->child);
}

static int spend_proposal_case(unsigned int variant)
{
    spend_fixture *f=calloc(1U,sizeof(*f));
    uint8_t grants[512],spend_grant[512],guest[8192],child[8192],descendant[8192],call[2048];
    SPEND_CHECK(f!=NULL && spend_init(f)==0);
    size_t lengths[3]; spend_proposal_guests(f,variant,guest,child,descendant,lengths);
    const uint8_t *modules[]={guest,child,descendant};
    for(unsigned int module=0U;module<3U;++module){
        uint8_t saved[8192]; char path[4096];
        int size=snprintf(path,sizeof(path),"%s/case%u.%s.wasm",guest_directory,variant,
            module==0U ? "owner" : module==1U ? "child" : "descendant");
        SPEND_CHECK(size>0 && (size_t)size<sizeof(path));
        FILE *input=fopen(path,"rb");
        SPEND_CHECK(input!=NULL && fread(saved,1U,sizeof(saved),input)==lengths[module] &&
            memcmp(saved,modules[module],lengths[module])==0 && fgetc(input)==EOF && fclose(input)==0);
    }
    SPEND_CHECK(spend_deploy(f,f->descendant,descendant,lengths[2],1U)==0 &&
        spend_deploy(f,f->child,child,lengths[1],2U)==0 && spend_deploy(f,f->owner,guest,lengths[0],3U)==0);
    lxp_module_ctx ctx; lxp_effect_buffer effects; bool created=false;
    SPEND_CHECK(lxp_arena_reset(&f->arena,0U)==LXP_OK &&
        lxp_state_journal_open(&f->state,f->state.next_sequence,&f->journal)==LXP_OK &&
        lxp_module_ctx_init(&ctx,&f->kernel,LXP_MODULE_PROGRAMS,10U,0U,f->state.next_sequence,
            100000U,&f->arena,true)==LXP_OK);
    ctx.protocol_version=LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    SPEND_CHECK(lxp_effect_buffer_init(&effects)==LXP_OK && lxp_module_ctx_bind_effects(&ctx,&effects)==LXP_OK &&
        lxp_programs_account_register(&ctx,f->owner,account_seed,sizeof(account_seed)-1U,
            f->asset.asset_id,&f->source,&created)==LXP_OK && created &&
        lxp_module_ctx_prepare_commit(&ctx)==LXP_OK && lxp_state_journal_commit(&f->journal)==LXP_OK &&
        lxp_module_ctx_commit(&ctx)==LXP_OK);
    f->source=program_spend_account(&f->accounts,f->source_id);
    SPEND_CHECK(f->source!=NULL && lxp_ledger_bootstrap_balance(f->source,f->asset.asset_id,(lxp_u128){0U,40U},0U)==LXP_OK);
    size_t grant_length=program_spend_capabilities(spend_grant,f->owner,account_seed,sizeof(account_seed)-1U,
        f->source_id,f->asset.asset_id,f->payee->id,20U);
    write_u16(grants,3U); grants[2U]=4U; memcpy(grants+3U,f->child,32U);
    grants[35U]=4U; memcpy(grants+36U,f->descendant,32U);
    memcpy(grants+68U,spend_grant+2U,grant_length-2U);
    size_t call_length=call_payload_with_capabilities(call,f->owner,grants,68U+grant_length-2U);
    write_u16(call+32U,LX_PROGRAMS_ACCOUNT_ABI_VERSION);
    lxp_u128 actor_before=f->actor->balance;
    uint64_t source_sequence=f->source->next_sequence;
    uint64_t identity_sequence=f->identity->next_sequence;
    uint64_t global_sequence=f->state.next_sequence;
    SPEND_CHECK(lxp_state_root(&f->kernel,f->kernel.current_state_root)==LXP_OK &&
        spend_sign(f,LX_PROGRAMS_CALL,call,call_length,4U)==0 &&
        lxp_kernel_execute_activity(&f->kernel,&f->activity,&f->execution,&f->receipt)==LXP_OK);
    bool refused=variant==12U;
    uint64_t transferred=variant==11U ? 8U : 7U;
    SPEND_CHECK(f->receipt.result_code==(refused ? LXP_ERR_PROGRAM_REFUSED : LXP_OK) &&
        f->source->balance.lo==(refused ? 40U : 40U-transferred) &&
        f->payee->balance.lo==(refused ? 0U : transferred));
    lxp_u128 actor_after;
    SPEND_CHECK(lxp_u128_sub(actor_before,f->receipt.fee_charged,&actor_after)==LXP_OK &&
        lxp_u128_cmp(actor_after,f->actor->balance)==0 && !lxp_u128_is_zero(f->receipt.fee_charged) &&
        f->source->next_sequence==source_sequence && f->identity->next_sequence==identity_sequence+1U &&
        f->state.next_sequence==global_sequence+1U);
    SPEND_CHECK(f->receipt.program_outcome.present &&
        f->receipt.program_outcome.terminal_kind==(refused ? LXP_PROGRAM_TERMINAL_FAILURE : LXP_PROGRAM_TERMINAL_SUCCESS) &&
        (refused ? lxp_ct_is_zero(f->receipt.program_outcome.transfer_root,32U) :
            !lxp_ct_is_zero(f->receipt.program_outcome.transfer_root,32U)));
    if(refused) SPEND_CHECK(f->receipt.effects.count==0U &&
        f->receipt.program_outcome.result_code==LXP_ERR_PROGRAM_REFUSED);
    uint8_t public_key[32];
    SPEND_CHECK(executed_public_key(executed_sequencer_seed,public_key)==0 &&
        lxp_receipt_verify(&f->receipt,public_key,&f->arena)==LXP_OK);
    char path[4096]; int size=snprintf(path,sizeof(path),"%s/case%u.terminal.bin",result_directory,variant);
    SPEND_CHECK(size>0 && (size_t)size<sizeof(path));
    int fd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);
    lxp_byte_span terminal=f->receipt.program_outcome.terminal_payload;
    SPEND_CHECK(fd>=0 && terminal.bytes!=NULL && terminal.length!=0U &&
        write(fd,terminal.bytes,terminal.length)==(ssize_t)terminal.length && fsync(fd)==0 && close(fd)==0);
    while(f->kernel.blob_count!=0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    SPEND_CHECK(lxp_state_store_destroy(&f->state)==LXP_OK);
    lx_account_registry_release(&f->accounts); free(f); return 0;
}

int main(int argc, char **argv)
{
    static const char *names[] = {"owner_transfer_and_narrowed_edge","amount_escalation_atomic",
        "asset_escalation_atomic","destination_escalation_atomic","repeated_visits_owner_fee_once",
        "depth_escalation_atomic","fanout_escalation_atomic","repeated_late_escalation_atomic",
        "owner_consumes_child_proposal","depth_forwarded_owner_proposal","fanout_owner_proposals",
        "repeated_owner_proposals","late_owner_proposal_atomic"};
    if (argc == 3 && strcmp(argv[1],"--emit-wasm") == 0) {
        spend_fixture *f = calloc(1U,sizeof(*f));
        SPEND_CHECK(f != NULL && spend_init(f) == 0);
        uint8_t guest[8192], child[8192], descendant[8192];
        for (unsigned int variant = 0U; variant < 13U; ++variant) {
            size_t lengths[3];
            if (variant < 8U) spend_guests(f,variant,guest,child,descendant,lengths);
            else spend_proposal_guests(f,variant,guest,child,descendant,lengths);
            const uint8_t *modules[] = {guest,child,descendant};
            for (unsigned int module = 0U; module < 3U; ++module) {
                char path[4096];
                int count = snprintf(path,sizeof(path),"%s/case%u.%s.wasm",argv[2],variant,
                    module == 0U ? "owner" : module == 1U ? "child" : "descendant");
                SPEND_CHECK(count > 0 && (size_t)count < sizeof(path));
                int fd = open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);
                SPEND_CHECK(fd >= 0 && write(fd,modules[module],lengths[module]) == (ssize_t)lengths[module] &&
                    fsync(fd) == 0 && close(fd) == 0);
            }
        }
        char path[4096];
        int count = snprintf(path,sizeof(path),"%s/payee.bin",argv[2]);
        SPEND_CHECK(count > 0 && (size_t)count < sizeof(path));
        int fd = open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);
        SPEND_CHECK(fd >= 0 && write(fd,f->payee->id,32U) == 32 && fsync(fd) == 0 && close(fd) == 0);
        SPEND_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
        lx_account_registry_release(&f->accounts); free(f);
        return 0;
    }
    guest_directory = getenv("PAXEER_X_SPEND_GUESTS");
    result_directory = getenv("PAXEER_X_SPEND_RESULTS");
    if (argc != 1 || argv == NULL || guest_directory == NULL || result_directory == NULL) return 78;
    struct stat result_info;
    SPEND_CHECK(stat(result_directory,&result_info) == 0 && S_ISDIR(result_info.st_mode) &&
        result_info.st_uid == geteuid() && (result_info.st_mode & 0777U) == 0700U);
    for (unsigned int i = 0U; i < sizeof(names)/sizeof(names[0]); ++i) {
        if ((i < 8U ? spend_case(i) : spend_proposal_case(i)) != 0) return 1;
        if (printf("CASE %s ok\n",names[i]) < 0) return 1;
    }
    return printf("PASSED 13\n") < 0;
}
