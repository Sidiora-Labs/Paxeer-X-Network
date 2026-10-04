#define main language_reference_main
int language_reference_main(int argc, char **argv);
#include "test_call_activity.c"
#undef main
#include "../../src/modules/programs/storage.h"
#include "layerx/lx_oracle.h"
#include "layerx/lx_perps.h"
#include "layerx/lx_web.h"

static const uint8_t language_web_text[] = "Paxeer X Network";
enum {
    LANGUAGE_WEB_RECORD_BYTES = LX_WEB_ANSWER_HEADER_BYTES +
        sizeof(language_web_text) - 1U
};

#define LANG_CHECK(condition) do { if (!(condition)) { \
    (void)fprintf(stderr, "language ABI fixture line %d\n", __LINE__); \
    return 1; } } while (0)
enum { ORACLE_READ_SEQUENCE = 9, ORACLE_READ_PRICE = 123456,
    ORACLE_READ_OBSERVED_AT = 500, ORACLE_READ_SOURCE = 7 };

static void oracle_read_market(lx_perps_market *market,
                               const uint8_t market_id[32], bool halted)
{
    (void)memset(market, 0, sizeof(*market));
    (void)memcpy(market->market_id, market_id, 32U);
    (void)memset(market->quote_asset, 0x44, 32U);
    (void)memset(market->administrator, 0x55, 32U);
    (void)memset(market->liquidity_account_id, 0x56, 32U);
    (void)memset(market->long_funding_account_id, 0x57, 32U);
    (void)memset(market->short_funding_account_id, 0x58, 32U);
    (void)memset(market->insurance_account_id, 0x59, 32U);
    market->contract_size = (lxp_u128){0U, 1U};
    market->tick_size = (lxp_u128){0U, 1U};
    market->lot_size = (lxp_u128){0U, 1U};
    market->price_scale = (lxp_u128){0U, 1U};
    market->initial_margin_ratio_bps = 200U;
    market->maintenance_margin_ratio_bps = 100U;
    market->liquidation_fee_bps = 10U;
    market->liquidator_share_bps = 10U;
    market->maximum_funding_rate_bps = 100U;
    market->maximum_deviation_basis_points = 1000U;
    market->funding_interval_ms = 1000U;
    market->maximum_oracle_staleness_ms = 1000U;
    market->minimum_price = (lxp_u128){0U, 1U};
    market->maximum_price = (lxp_u128){0U, 1000000U};
    market->permitted_oracle_key_count = 2U;
    (void)memset(market->permitted_oracle_keys[0], 0x61, 32U);
    (void)memset(market->permitted_oracle_keys[1], 0x62, 32U);
    market->parameter_version = 1U;
    market->halted = halted;
}

static void oracle_read_observation(lx_perps_oracle_state *state,
                                    const uint8_t market_id[32])
{
    (void)memset(state, 0, sizeof(*state));
    (void)memcpy(state->market_id, market_id, 32U);
    state->observation_sequence = ORACLE_READ_SEQUENCE;
    state->price = (lxp_u128){0U, ORACLE_READ_PRICE};
    state->observed_at = ORACLE_READ_OBSERVED_AT;
    state->source_identifier = ORACLE_READ_SOURCE;
    (void)memset(state->oracle_public_key, 0x61, 32U);
}

static int oracle_read_expected_record(const lx_perps_market *market,
                                       const lx_perps_oracle_state *state,
                                       uint8_t record[LX_ORACLE_COMMITTED_BYTES])
{
    static const uint8_t domain[] = "LXP:ORACLE:SOURCE-SET:v1";
    uint8_t preimage[sizeof(domain) - 1U + 1U + LX_ORACLE_MAX_KEYS * 32U];
    uint8_t price[16];
    size_t length = 0U;
    size_t index;
    (void)memcpy(preimage, domain, sizeof(domain) - 1U);
    length += sizeof(domain) - 1U;
    preimage[length++] = market->permitted_oracle_key_count;
    (void)memcpy(preimage + length, market->permitted_oracle_keys[0],
                 (size_t)market->permitted_oracle_key_count * 32U);
    length += (size_t)market->permitted_oracle_key_count * 32U;
    LANG_CHECK(lxp_u128_to_be(state->price, price) == LXP_OK);
    for (index = 0U; index < 16U; ++index) record[index] = price[15U - index];
    for (index = 0U; index < 8U; ++index) {
        record[16U + index] = (uint8_t)(state->observed_at >> (8U * index));
        record[24U + index] =
            (uint8_t)(state->observation_sequence >> (8U * index));
    }
    LANG_CHECK(lxp_hash_sha256(preimage, length, record + 32U) == LXP_OK);
    return 0;
}


typedef struct language_fixture {
    lxp_state_store state;
    lxp_state_journal journal;
    lxp_kernel kernel;
    lxp_identity_store identities;
    lxp_identity *identity;
    lxp_authority_resolved authority;
    lxp_authority_scope scope;
    lxp_kernel_execution execution;
    lxp_fee_params fees;
    lx_account_registry accounts;
    lx_account *actor;
    lx_account *treasury;
    lxp_transfer_asset_state fee_asset_state;
    lx_programs_transfer_runtime runtime;
    lxp_arena arena;
    uint8_t arena_bytes[4U * LXP_MAX_ACTIVITY_BYTES + 65536U];
    uint8_t payload[LXP_MAX_ACTIVITY_BYTES];
    uint8_t primary_key[32], actor_id[32], treasury_id[32];
    uint8_t program_id[32], callee_id[32], account_id[32], fee_asset[32];
    uint64_t parameters;
    bool state_ready;
} language_fixture;

static const uint8_t language_did[] = "did:lxp:programs-language-abi";

static int language_setup(language_fixture *f)
{
    static const uint8_t actor_name[] = "agent:did:lxp:programs-language-abi:main";
    static const uint8_t treasury_name[] = "system:fees";
    static const uint8_t seed[32] = {0x33U}, grant[32] = {0};
    f->parameters = 1U;
    f->fee_asset[0] = 9U;
    memset(f->program_id, 0x71, 32U);
    memset(f->callee_id, 0x72, 32U);
    LANG_CHECK(executed_public_key(seed, f->primary_key) == 0);
    LANG_CHECK(lx_account_registry_init(&f->accounts) == LXP_OK);
    LANG_CHECK(lx_account_id_from_string(actor_name, sizeof(actor_name)-1U, f->actor_id) == LXP_OK);
    LANG_CHECK(lx_account_id_from_string(treasury_name, sizeof(treasury_name)-1U, f->treasury_id) == LXP_OK);
    LANG_CHECK(lx_account_open(&f->accounts, actor_name, sizeof(actor_name)-1U,
        f->actor_id, 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->actor) == LXP_OK);
    LANG_CHECK(lx_account_open(&f->accounts, treasury_name, sizeof(treasury_name)-1U,
        f->treasury_id, 2U, LX_ACCOUNT_OPEN_GENESIS, NULL, &f->treasury) == LXP_OK);
    LANG_CHECK(lxp_ledger_bootstrap_balance(f->actor, f->fee_asset,
        (lxp_u128){0U, UINT64_MAX}, 1U) == LXP_OK);
    LANG_CHECK(lxp_ledger_bootstrap_balance(f->treasury, f->fee_asset,
        (lxp_u128){0U, 0U}, 0U) == LXP_OK);
    LANG_CHECK(lxp_did_id_derive(language_did, sizeof(language_did)-1U,
        f->authority.principal) == LXP_OK);
    memcpy(f->authority.actor, f->authority.principal, 32U);
    memcpy(f->authority.verified_key, f->primary_key, 32U);
    f->authority.kind = LXP_AUTHORITY_OWNER;
    f->scope.module_mask = UINT64_C(1) << LXP_MODULE_PROGRAMS;
    f->scope.activity_ordinal_min = 1U; f->scope.activity_ordinal_max = 7U;
    f->scope.maximum_per_activity = (lxp_u128){UINT64_MAX, UINT64_MAX};
    f->scope.maximum_total = f->scope.maximum_per_activity;
    f->scope.maximum_per_period = f->scope.maximum_per_activity;
    f->authority.scope = &f->scope;
    LANG_CHECK(lxp_authority_hash(f->authority.kind, grant, f->primary_key,
        f->authority.authority_hash) == LXP_OK);
    memcpy(f->fee_asset_state.asset_id, f->fee_asset, 32U);
    f->fee_asset_state.registered = true;
    f->runtime.accounts = &f->accounts;
    f->runtime.assets = &f->fee_asset_state;
    f->runtime.asset_count = 1U;
    f->runtime.fee_schedule = (lx_programs_fee_schedule){1U,1U,1U,2U,4U,1U,1U,1U};
    f->runtime.resolve_metering_schedule = lxp_programs_metering_resolve_runtime;
    f->runtime.metering_schedule_context = &f->kernel;
    memcpy(f->runtime.occupancy_asset_id, f->fee_asset, 32U);
    f->runtime.resolve_occupancy_parameters = occupancy_parameters;
    f->runtime.occupancy_parameter_context = &f->runtime;
    f->fees.version = 1U; f->fees.multiplier_basis_points = 10000U;
    LANG_CHECK(lxp_state_store_init(&f->state, 1U) == LXP_OK);
    f->state_ready = true;
    LANG_CHECK(lxp_identity_register(&f->identities, language_did,
        sizeof(language_did)-1U, f->primary_key, &f->identity) == LXP_OK);
    LANG_CHECK(lxp_kernel_create(&f->kernel, &f->state, &f->journal, &f->parameters, 0U) == LXP_OK);
    LANG_CHECK(install_metering_v1(&f->kernel) == LXP_OK);
    LANG_CHECK(lxp_kernel_register_module(&f->kernel, programs_module_registration_v4()) == LXP_OK);
    LANG_CHECK(lxp_kernel_register_module(&f->kernel, lx_perps_module_iface()) == LXP_OK);
    LANG_CHECK(lxp_kernel_bind_module_runtime(&f->kernel, LXP_MODULE_PROGRAMS, &f->runtime) == LXP_OK);
    LANG_CHECK(lxp_programs_bind_fee_transaction(&f->kernel) == LXP_OK);
    LANG_CHECK(lxp_kernel_set_capabilities(&f->kernel, NULL, lxp_kernel_canonical_ledger_apply) == LXP_OK);
    LANG_CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK);
    LANG_CHECK(lxp_arena_init(&f->arena, f->arena_bytes, sizeof(f->arena_bytes)) == LXP_OK);
    f->execution.network_id = 7U; f->execution.batch_number = 1U;
    f->execution.batch_timestamp_ms = 10U; f->execution.maximum_timestamp_window = 100U;
    f->execution.recorded_module_version = LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION;
    f->execution.parameter_version = 1U; f->execution.signature_valid = true;
    f->execution.identities = &f->identities; f->execution.authority = &f->authority;
    f->execution.fee_parameters = &f->fees; f->execution.gas_limit = 1000000U;
    f->execution.arena = &f->arena;
    f->execution.sequencer_private_key = executed_sequencer_seed;
    LANG_CHECK(lxp_programs_account_derive(f->program_id, (const uint8_t *)"fixture", 7U,
        f->account_id) == LXP_OK);
    return 0;
}

static int language_execute(language_fixture *f, uint32_t type, size_t length,
    uint8_t marker, lxp_receipt *receipt)
{
    lxp_activity activity;
    uint8_t key[32];
    uint64_t identity_sequence = f->identity->next_sequence;
    fill_activity(&activity, type, f->payload, length, language_did,
        sizeof(language_did)-1U, f->primary_key);
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.account_sequence = identity_sequence;
    activity.idempotency_key[31] = marker;
    activity.fee_limit = (lxp_u128){0U, 67108864U};
    f->execution.fee_balance = f->actor->balance;
    f->execution.global_sequence = f->state.next_sequence;
    LANG_CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK);
    LANG_CHECK(execute_artifact_fixture_activity(&f->kernel, &activity,
        &f->execution, receipt) == LXP_OK);
    LANG_CHECK(f->identity->next_sequence == identity_sequence + 1U);
    LANG_CHECK(executed_public_key(executed_sequencer_seed, key) == 0);
    LANG_CHECK(lxp_receipt_verify(receipt, key, &f->arena) == LXP_OK);
    if (type == LX_PROGRAMS_CALL) {
        LANG_CHECK(receipt->program_outcome.present);
        LANG_CHECK(receipt->program_outcome.abi_version == 4U);
        LANG_CHECK(lxp_program_outcome_validate_for_protocol(&receipt->program_outcome,
            LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == LXP_OK);
    }
    return 0;
}

static int language_deploy(language_fixture *f, const char *path)
{
    uint8_t *wasm = malloc(LXP_MAX_ACTIVITY_BYTES);
    uint8_t code_hash[32];
    lxp_receipt receipt;
    FILE *stream;
    long end;
    size_t length, index;
    LANG_CHECK(wasm != NULL);
    stream = fopen(path, "rb");
    if (stream == NULL) { free(wasm); return 1; }
    if (fseek(stream, 0L, SEEK_END) != 0 || (end = ftell(stream)) <= 0L ||
        (unsigned long)end > LXP_MAX_ACTIVITY_BYTES - 104U || fseek(stream, 0L, SEEK_SET) != 0) {
        fclose(stream); free(wasm); return 1;
    }
    length = (size_t)end;
    if (fread(wasm, 1U, length, stream) != length || fclose(stream) != 0) { free(wasm); return 1; }
    for (index = 0U; index < 2U; ++index) {
        size_t payload_length = program_spend_deploy_payload(f->payload,
            index == 0U ? f->program_id : f->callee_id, f->authority.principal,
            wasm, length, code_hash);
        write_u16(f->payload + 32U, LX_PROGRAMS_GUEST_ABI_V4_VERSION);
        if (language_execute(f, LX_PROGRAMS_DEPLOY, payload_length,
            (uint8_t)(0x40U + index), &receipt) != 0 || receipt.result_code != LXP_OK) {
            free(wasm); return 1;
        }
    }
    free(wasm);
    return 0;
}

static int language_inputs(language_fixture *f, uint8_t expected_oracle[64],
    uint8_t expected_web[LANGUAGE_WEB_RECORD_BYTES])
{
    uint8_t market_id[32], bytes[65536];
    lx_perps_market market;
    lx_perps_oracle_state oracle;
    lx_web_observation web;
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    size_t index;
    memset(market_id, 0x11, 32U);
    oracle_read_market(&market, market_id, false);
    oracle_read_observation(&oracle, market_id);
    LANG_CHECK(oracle_read_expected_record(&market, &oracle, expected_oracle) == 0);
    LANG_CHECK(lxp_arena_init(&arena, bytes, sizeof(bytes)) == LXP_OK);
    LANG_CHECK(lxp_state_journal_open(&f->state, f->state.next_sequence, &f->journal) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_init(&ctx, &f->kernel, LXP_MODULE_PERPS,
        10U,0U,f->state.next_sequence,100000U,&arena,true) == LXP_OK);
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    LANG_CHECK(lxp_effect_buffer_init(&effects) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_bind_effects(&ctx, &effects) == LXP_OK);
    LANG_CHECK(lx_perps_market_put(&ctx, &market) == LXP_OK);
    LANG_CHECK(lx_perps_oracle_state_put(&ctx, &oracle) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_prepare_commit(&ctx) == LXP_OK);
    LANG_CHECK(lxp_state_journal_commit(&f->journal) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    LANG_CHECK(lxp_arena_reset(&arena,0U) == LXP_OK);
    LANG_CHECK(lxp_state_journal_open(&f->state, f->state.next_sequence, &f->journal) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_init(&ctx, &f->kernel, LXP_MODULE_PROGRAMS,
        10U,0U,f->state.next_sequence,100000U,&arena,true) == LXP_OK);
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    LANG_CHECK(lxp_effect_buffer_init(&effects) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_bind_effects(&ctx, &effects) == LXP_OK);
    memset(&web,0,sizeof(web));
    web.origin = LX_WEB_ORIGIN_PROGRAM; web.network_id = 7U;
    memcpy(web.program_id,f->program_id,32U);
    web.request_id = UINT64_C(0x0102030405060708); web.kind = LX_WEB_KIND_FETCH;
    memset(web.content_digest,0x5a,32U); web.full_length = 5000U;
    web.response_length = sizeof(language_web_text)-1U;
    memcpy(web.response,language_web_text,sizeof(language_web_text)-1U);
    LANG_CHECK(lx_web_committed_put(&ctx,&web) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_prepare_commit(&ctx) == LXP_OK);
    LANG_CHECK(lxp_state_journal_commit(&f->journal) == LXP_OK);
    LANG_CHECK(lxp_module_ctx_commit(&ctx) == LXP_OK);
    LANG_CHECK(lxp_state_root(&f->kernel,f->kernel.current_state_root) == LXP_OK);
    memcpy(expected_web,web.content_digest,32U);
    for(index=0U;index<4U;++index) {
        expected_web[32U+index]=(uint8_t)(web.full_length>>(8U*index));
        expected_web[36U+index]=(uint8_t)(web.response_length>>(8U*index));
    }
    memcpy(expected_web+LX_WEB_ANSWER_HEADER_BYTES,language_web_text,
        sizeof(language_web_text)-1U);
    return 0;
}

typedef struct language_reader { const uint8_t *bytes; size_t length, offset; } language_reader;
static int language_take(language_reader *r, size_t length, const uint8_t **bytes)
{
    if (r->offset > r->length || length > r->length-r->offset) return 1;
    *bytes = r->bytes+r->offset; r->offset += length; return 0;
}
static uint32_t language_u32(const uint8_t *bytes)
{ return ((uint32_t)bytes[0]<<24U)|((uint32_t)bytes[1]<<16U)|((uint32_t)bytes[2]<<8U)|bytes[3]; }
static uint64_t language_u64(const uint8_t *bytes)
{ uint64_t value=0U; size_t index; for(index=0U;index<8U;++index)value=(value<<8U)|bytes[index]; return value; }
static int language_blob(language_reader *r, unsigned int width, lxp_byte_span *span)
{
    const uint8_t *length_bytes;
    uint64_t length;
    if (language_take(r,width,&length_bytes)!=0) return 1;
    length=width==4U?language_u32(length_bytes):language_u64(length_bytes);
    if(length>LXP_MAX_ACTIVITY_BYTES) return 1;
    span->length=(size_t)length;
    return language_take(r,span->length,&span->bytes);
}
static bool language_domain(lxp_byte_span span,const char *domain)
{
    size_t length=strlen(domain)+1U;
    return span.length>=length && memcmp(span.bytes,domain,length)==0;
}
static int language_terminal(const lxp_receipt *receipt,lxp_byte_span *response,
    lxp_byte_span *refusal,lxp_byte_span *legs)
{
    static const char *const wrappers[]={
        "LXP/programs/terminal-applied-legs/v1",
        "LXP/program-execution-with-transfer-authority/v2",
        "LXP/program-execution-with-occupancy/v1",
        "LXP/programs/execution-authority/v1",
        "LXP/programs/execution-occupancy/v1"};
    lxp_byte_span detail=receipt->program_outcome.terminal_payload;
    size_t depth,index;
    *response=(lxp_byte_span){NULL,0U}; *refusal=*response; *legs=*response;
    for(depth=0U;depth<5U;++depth) {
        bool found=false;
        for(index=0U;index<sizeof(wrappers)/sizeof(wrappers[0]);++index) {
            if(language_domain(detail,wrappers[index])) {
                language_reader r={detail.bytes,detail.length,strlen(wrappers[index])+1U};
                lxp_byte_span inner,attachment;
                const uint8_t *suffix;
                LANG_CHECK(language_blob(&r,4U,&inner)==0);
                LANG_CHECK(language_blob(&r,4U,&attachment)==0);
                if(index==1U || index==3U) LANG_CHECK(language_take(&r,32U,&suffix)==0);
                LANG_CHECK(r.offset==r.length);
                if(index==0U) {
                    LANG_CHECK(attachment.length%115U==0U);
                    *legs=attachment;
                }
                detail=inner; found=true; break;
            }
        }
        if(!found) break;
    }
    if(language_domain(detail,"LXP/programs/failure-detail/v1")) {
        language_reader r={detail.bytes,detail.length,sizeof("LXP/programs/failure-detail/v1")};
        const uint8_t *tag;
        lxp_byte_span payload;
        LANG_CHECK(receipt->result_code==LXP_ERR_PROGRAM_REFUSED);
        LANG_CHECK(language_take(&r,1U,&tag)==0 && tag[0]>=1U && tag[0]<=4U);
        LANG_CHECK(language_blob(&r,4U,&payload)==0 && r.offset==r.length);
        if(tag[0]==1U) {
            LANG_CHECK(payload.length>=40U && !lxp_ct_is_zero(payload.bytes,32U));
            LANG_CHECK(language_u32(payload.bytes+36U)==payload.length-40U);
            *refusal=(lxp_byte_span){payload.bytes+32U,payload.length-32U};
        } else *refusal=detail;
        return 0;
    }
    LANG_CHECK(language_domain(detail,"LXP/program-execution/v4"));
    {
        language_reader r={detail.bytes,detail.length,sizeof("LXP/program-execution/v4")};
        const uint8_t *bytes;
        uint64_t count;
        lxp_byte_span graph,trace;
        LANG_CHECK(language_take(&r,10U,&bytes)==0);
        LANG_CHECK(language_take(&r,8U,&bytes)==0);
        count=language_u64(bytes); LANG_CHECK(count<=64U);
        for(index=0U;index<count;++index) {
            LANG_CHECK(language_take(&r,1U,&bytes)==0 && (bytes[0]==1U || bytes[0]==2U));
            LANG_CHECK(language_take(&r,bytes[0]==1U?4U:8U,&bytes)==0);
        }
        LANG_CHECK(language_take(&r,64U,&bytes)==0);
        LANG_CHECK(language_take(&r,1U,&bytes)==0 && bytes[0]<=1U);
        if(bytes[0]==1U) LANG_CHECK(language_blob(&r,8U,&trace)==0);
        LANG_CHECK(language_take(&r,34U,&bytes)==0 && bytes[32U]==0U && bytes[33U]==4U);
        LANG_CHECK(language_take(&r,1U,&bytes)==0 && bytes[0]==0U);
        LANG_CHECK(language_take(&r,4U,&bytes)==0 && language_u32(bytes)==0U);
        LANG_CHECK(language_blob(&r,8U,response)==0);
        LANG_CHECK(language_blob(&r,8U,&graph)==0 && r.offset==r.length);
        LANG_CHECK(graph.length==receipt->program_outcome.call_graph_payload.length &&
            memcmp(graph.bytes,receipt->program_outcome.call_graph_payload.bytes,graph.length)==0);
        LANG_CHECK(receipt->result_code==LXP_OK);
    }
    return 0;
}

typedef struct language_effects { uint8_t bytes[32768]; size_t length; uint32_t count; } language_effects;
static lxp_result language_cell(void *context,const uint8_t *key,uint16_t key_length,
    const uint8_t *value,uint32_t value_length)
{
    language_effects *effects=context;
    size_t length=6U+(size_t)key_length+value_length;
    if(length>sizeof(effects->bytes)-effects->length) return LXP_ERR_LENGTH_LIMIT;
    write_u16(effects->bytes+effects->length,key_length); effects->length+=2U;
    memcpy(effects->bytes+effects->length,key,key_length); effects->length+=key_length;
    write_u32(effects->bytes+effects->length,value_length); effects->length+=4U;
    memcpy(effects->bytes+effects->length,value,value_length); effects->length+=value_length;
    ++effects->count; return LXP_OK;
}
static int language_storage(language_fixture *f,language_effects *effects)
{
    lxp_module_ctx ctx;
    uint8_t namespace_bytes[65];
    size_t selector;
    effects->length=0U;
    LANG_CHECK(lxp_module_ctx_init(&ctx,&f->kernel,LXP_MODULE_PROGRAMS,10U,0U,
        f->state.next_sequence,100000U,&f->arena,false)==LXP_OK);
    memcpy(namespace_bytes,f->program_id,32U);
    for(selector=0U;selector<2U;++selector) {
        size_t count_offset;
        namespace_bytes[32U]=(uint8_t)selector;
        if(selector==0U) memcpy(namespace_bytes+33U,f->authority.principal,32U);
        effects->bytes[effects->length++]=(uint8_t)selector;
        count_offset=effects->length; effects->length+=4U; effects->count=0U;
        LANG_CHECK(lxp_programs_storage_import(&ctx,namespace_bytes,selector==0U?65U:33U,
            language_cell,effects)==LXP_OK);
        write_u32(effects->bytes+count_offset,effects->count);
    }
    return 0;
}
static int language_hex(const char *field,lxp_byte_span span,bool comma)
{
    LANG_CHECK(printf("\"%s\":\"",field)>=0);
    LANG_CHECK(lifecycle_vector_hex(span.bytes,span.length)==0);
    LANG_CHECK(printf(comma?"\",":"\"")>=0); return 0;
}
static size_t language_caps(uint8_t out[128],uint8_t operation,const language_fixture *f)
{
    if(operation==0U) { write_u16(out,3U);out[2]=1U;out[3]=2U;out[4]=3U;return 5U; }
    if(operation==4U) { write_u16(out,1U);out[2]=4U;memcpy(out+3U,f->callee_id,32U);return 35U; }
    if(operation==5U) {
        write_u16(out,1U);out[2]=5U;memcpy(out+3U,f->fee_asset,32U);
        memcpy(out+35U,f->account_id,32U);memset(out+67U,0,16U);out[82U]=1U;return 83U;
    }
    write_u16(out,0U);return 2U;
}

static int language_cases(const char *path)
{
    static const char *const names[]={"basic","oracle","web","refusal","call","account","denied"};
    static const uint8_t operations[]={0U,1U,2U,3U,4U,5U,7U};
    static const uint8_t access[]="LayerX/programs/access-declaration/v1\0";
    static const uint8_t binding[]="binding";
    language_fixture *f=calloc(1U,sizeof(*f));
    language_effects *before=malloc(sizeof(*before)),*after=malloc(sizeof(*after));
    uint8_t expected_oracle[64],expected_web[LANGUAGE_WEB_RECORD_BYTES];
    size_t index;
    int result=1;
    if(f==NULL || before==NULL || after==NULL) goto finished;
    if(language_setup(f)!=0 || language_deploy(f,path)!=0 ||
        language_inputs(f,expected_oracle,expected_web)!=0) goto finished;
    if(printf("{\"cases\":{")<0) goto finished;
    for(index=0U;index<sizeof(operations);++index) {
        uint8_t calldata[33],caps[128],canonical_effects[65536];
        lxp_receipt receipt;
        lxp_byte_span response,refusal,legs,effects;
        size_t cap_length,payload_length,effect_length=0U;
        uint8_t operation=operations[index];
        size_t calldata_length=operation==5U?33U:1U;
        calldata[0]=operation;
        if(operation==5U) memcpy(calldata+1U,f->account_id,32U);
        if(lxp_arena_reset(&f->arena,0U)!=LXP_OK || language_storage(f,before)!=0) goto finished;
        cap_length=language_caps(caps,operation,f);
        payload_length=call_payload_with_data(f->payload,f->program_id,caps,cap_length,
            access,sizeof(access),calldata,calldata_length);
        write_u16(f->payload+32U,4U);write_u32(f->payload+46U,4096U);
        if(language_execute(f,LX_PROGRAMS_CALL,payload_length,(uint8_t)(0x50U+index),&receipt)!=0 ||
            language_terminal(&receipt,&response,&refusal,&legs)!=0 || language_storage(f,after)!=0) goto finished;
        if(operation==3U || operation==7U) {
            if(receipt.result_code!=LXP_ERR_PROGRAM_REFUSED || response.length!=0U || refusal.length==0U ||
                before->length!=after->length || memcmp(before->bytes,after->bytes,before->length)!=0 ||
                legs.length!=0U) goto finished;
            if(operation==3U && (refusal.length!=10U || language_u32(refusal.bytes)!=1U ||
                language_u32(refusal.bytes+4U)!=2U || memcmp(refusal.bytes+8U,"no",2U)!=0)) goto finished;
        } else {
            if(receipt.result_code!=LXP_OK || refusal.length!=0U) goto finished;
            if((operation==0U || operation==4U) &&
                (response.length!=sizeof(binding)-1U || memcmp(response.bytes,binding,sizeof(binding)-1U)!=0)) goto finished;
            if(operation==0U && (after->length!=26U || after->bytes[0]!=0U ||
                language_u32(after->bytes+1U)!=1U || after->bytes[5U]!=0U || after->bytes[6U]!=3U ||
                memcmp(after->bytes+7U,"key",3U)!=0 || language_u32(after->bytes+10U)!=7U ||
                memcmp(after->bytes+14U,binding,7U)!=0 || after->bytes[21U]!=1U ||
                language_u32(after->bytes+22U)!=0U || receipt.program_outcome.event_envelope_payload.length==0U))
                goto finished;
            if(operation==1U && (response.length!=64U || memcmp(response.bytes,expected_oracle,64U)!=0)) goto finished;
            if(operation==2U && (response.length!=sizeof(expected_web) ||
                memcmp(response.bytes,expected_web,sizeof(expected_web))!=0)) goto finished;
            if(operation==5U) {
                size_t account_index;
                lx_account *account;
                if(lx_account_registry_index_lookup(&f->accounts,f->account_id,&account_index)!=LXP_OK)
                    goto finished;
                account=&f->accounts.accounts[account_index];
                if(account->balance.hi!=0U || account->balance.lo!=1U || legs.length!=115U)
                    goto finished;
            }
        }
        if(before->length!=after->length || memcmp(before->bytes,after->bytes,before->length)!=0) {
            memcpy(canonical_effects,after->bytes,after->length);effect_length=after->length;
        }
        if(legs.length>sizeof(canonical_effects)-effect_length) goto finished;
        if(legs.length!=0U) memcpy(canonical_effects+effect_length,legs.bytes,legs.length);
        effect_length+=legs.length;effects=(lxp_byte_span){canonical_effects,effect_length};
        if(printf("%s\"%s\":{\"result_code\":%d,",index==0U?"":",",names[index],(int)receipt.result_code)<0 ||
            language_hex("response_hex",response,true)!=0 || language_hex("refusal_hex",refusal,true)!=0 ||
            language_hex("effects_hex",effects,true)!=0 ||
            language_hex("events_hex",receipt.program_outcome.event_envelope_payload,true)!=0 ||
            language_hex("calldata_hex",(lxp_byte_span){calldata,calldata_length},true)!=0 ||
            language_hex("terminal_hex",receipt.program_outcome.terminal_payload,false)!=0 || printf("}")<0) goto finished;
    }
    if(printf("}}")<0) goto finished;
    result=0;
finished:
    if(f!=NULL && f->state_ready) (void)lxp_state_store_destroy(&f->state);
    free(after);free(before);free(f);return result;
}
int main(int argc,char **argv)
{
    static const char *const languages[]={"c","rust","assemblyscript"};
    size_t index;
    if(argc!=4) return 1;
    if(printf("{\"schema\":\"layerx.program-language-abi.v1\",\"languages\":{")<0) return 1;
    for(index=0U;index<3U;++index) {
        if(printf("%s\"%s\":",index==0U?"":",",languages[index])<0 || language_cases(argv[index+1U])!=0) return 1;
    }
    return printf("}}\n")<0 || fflush(stdout)!=0?1:0;
}
