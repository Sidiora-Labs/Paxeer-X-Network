#define main metered_fixture_reference_main
int metered_fixture_reference_main(int argc, char **argv);
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
                                     &f->parameters, 0U) == LXP_OK);
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


#include "balance_read_guests.h"
#include "layerx/lxp_history.h"
#include <pthread.h>

typedef struct balance_fixture {
    metered_fixture f;
    lx_account *sight[33];
    uint8_t programs[5][32];
    uint8_t wasm[5][65536];
    size_t wasm_length[5];
    lxp_verified_receipt_index index;
    lxp_arena proof_arena;
    uint8_t proof_storage[4U * LXP_MAX_ACTIVITY_BYTES];
    uint8_t digest[32];
    uint8_t source[32];
    lxp_log feed_log, canonical_log;
    lxp_history history;
    lx_programs_state_feed_store feed;
    pthread_mutex_t feed_mutex;
    char directory[128];
} balance_fixture;

static void balance_le32(uint8_t *p, uint32_t n) {
    for (unsigned i=0; i<4; ++i) p[i]=(uint8_t)(n>>(8U*i));
}
static uint32_t balance_be32(const uint8_t *p) {
    return ((uint32_t)p[0]<<24)|((uint32_t)p[1]<<16)|((uint32_t)p[2]<<8)|p[3];
}
static uint64_t balance_be64(const uint8_t *p) {
    uint64_t v=0; for(unsigned i=0;i<8;++i) v=(v<<8)|p[i]; return v;
}
static int32_t balance_status(const uint8_t *p) {
    return (int32_t)((uint32_t)p[0]|((uint32_t)p[1]<<8)|((uint32_t)p[2]<<16)|((uint32_t)p[3]<<24));
}
static int balance_guests(balance_fixture *b,const char *directory,bool emit) {
    for(unsigned i=0;i<5;++i) {
        char path[4096]; uint8_t expected[65536]; FILE *f;
        size_t length=balance_guest_generate(expected,sizeof(expected),i);
        METERED_CHECK(length>0);
        int n=snprintf(path,sizeof(path),"%s/balance-%u.wasm",directory,i);
        METERED_CHECK(n>0&&(size_t)n<sizeof(path));
        f=fopen(path,emit?"wb":"rb"); METERED_CHECK(f!=NULL);
        if(emit) METERED_CHECK(fwrite(expected,1,length,f)==length);
        else { b->wasm_length[i]=fread(b->wasm[i],1,sizeof(b->wasm[i]),f);
            METERED_CHECK(feof(f)&&!ferror(f)&&b->wasm_length[i]==length&&memcmp(expected,b->wasm[i],length)==0); }
        METERED_CHECK(fclose(f)==0);
        b->programs[i][0]=0x61; b->programs[i][31]=(uint8_t)(i+1);
    } return 0;
}
static int balance_receipt(balance_fixture *b) {
    uint8_t key[32];
    METERED_CHECK(lxp_arena_reset(&b->proof_arena,0)==LXP_OK);
    METERED_CHECK(executed_public_key(executed_sequencer_seed,key)==0);
    METERED_CHECK(lxp_receipt_verify(&b->f.receipt,key,&b->proof_arena)==LXP_OK);
    METERED_CHECK(lxp_verified_receipt_index_add(&b->index,&b->f.receipt,key,&b->proof_arena)==LXP_OK);
    METERED_CHECK(lxp_receipt_digest(&b->f.receipt,&b->proof_arena,b->digest)==LXP_OK);
    METERED_CHECK(memcmp(b->f.receipt.resulting_state_root,b->f.kernel.current_state_root,32)==0);
    return 0;
}
static int balance_execute(balance_fixture *b,uint32_t type,const uint8_t *p,size_t n) {
    METERED_CHECK(metered_activity(&b->f,type,p,n,0x55,false)==0);
    b->f.execution.verified_receipts=&b->index;
    lxp_result rc=lxp_kernel_execute_activity(&b->f.kernel,&b->f.activity,&b->f.execution,&b->f.receipt);
    if(rc!=LXP_OK) fprintf(stderr,"native execute type=%u rc=%d\n",type,(int)rc);
    METERED_CHECK(rc==LXP_OK); return 0;
}
static int balance_init(balance_fixture *b) {
    METERED_CHECK(cache_fixture_init(&b->f)==0);
    METERED_CHECK(lxp_arena_init(&b->proof_arena,b->proof_storage,sizeof(b->proof_storage))==LXP_OK);
    METERED_CHECK(lxp_verified_receipt_index_init(&b->index)==LXP_OK);
    for(unsigned i=0;i<33;++i) {
        char name[96]; (void)snprintf(name,sizeof(name),"agent:did:lxp:balance-%02u:main",i);
        METERED_CHECK(metered_account(&b->f,name,i==0?0:1000U+i,&b->sight[i])==0);
    }
    for(unsigned i=0;i<33;++i) for(unsigned j=i+1;j<33;++j)
        if(memcmp(b->sight[i]->id,b->sight[j]->id,32)>0) {lx_account *a=b->sight[i];b->sight[i]=b->sight[j];b->sight[j]=a;}
    METERED_CHECK(lxp_state_root(&b->f.kernel,b->f.kernel.current_state_root)==LXP_OK);
    {
        char path[192],database[192];
        strcpy(b->directory,"/tmp/lxp-balance-read-XXXXXX");
        METERED_CHECK(mkdtemp(b->directory)!=NULL&&pthread_mutex_init(&b->feed_mutex,NULL)==0);
        METERED_CHECK(snprintf(path,sizeof(path),"%s/feed.log",b->directory)>0);
        METERED_CHECK(lxp_log_open_or_create(&b->feed_log,path,LXP_MAX_BATCH_BODY_BYTES)==LXP_OK);
        METERED_CHECK(snprintf(path,sizeof(path),"%s/canonical.log",b->directory)>0);
        METERED_CHECK(lxp_log_open_or_create(&b->canonical_log,path,LXP_MAX_BATCH_BODY_BYTES)==LXP_OK);
        METERED_CHECK(snprintf(database,sizeof(database),"%s/history.db",b->directory)>0);
        METERED_CHECK(lxp_history_open(&b->history,&b->canonical_log,database,"migrations/0007_history_index.sql")==LXP_OK);
        METERED_CHECK(lxp_programs_state_feed_store_open(&b->feed,&b->feed_log,&b->canonical_log,&b->history,&b->proof_arena,&b->feed_mutex)==LXP_OK);
        METERED_CHECK(lxp_programs_state_feed_store_anchor(&b->feed,b->f.state.next_sequence,b->f.kernel.current_state_root)==LXP_OK);
        b->f.runtime.state_feed=&b->feed.feed;
        METERED_CHECK(lxp_programs_bind_state_feed(&b->f.kernel,b->f.runtime.state_feed)==LXP_OK);
        METERED_CHECK(lxp_programs_state_feed_store_recover(&b->feed,&b->f.kernel)==LXP_OK);
    }
    for(unsigned i=0;i<5;++i) {
        uint8_t payload[66000],hash[32]; size_t n=program_spend_deploy_payload(payload,b->programs[i],b->f.identity->did_id,b->wasm[i],b->wasm_length[i],hash);
        METERED_CHECK(balance_execute(b,LX_PROGRAMS_DEPLOY,payload,n)==0);
        METERED_CHECK(b->f.receipt.result_code==LXP_OK);
        METERED_CHECK(balance_receipt(b)==0);
    }
    uint8_t registration[74]; memcpy(registration,b->programs[0],32);memcpy(registration+32,"LXPA1",5);
    memcpy(registration+37,b->f.asset.asset_id,32);write_u32(registration+69,1);registration[73]='s';
    METERED_CHECK(balance_execute(b,LX_PROGRAMS_ACCOUNT,registration,sizeof(registration))==0);
    METERED_CHECK(b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(lxp_programs_account_derive(b->programs[0],registration+73,1,b->source)==LXP_OK);
    METERED_CHECK(balance_receipt(b)==0);
    return 0;
}
static size_t balance_caps(balance_fixture *b,uint8_t *out,unsigned count,const uint8_t *digest) {
    write_u16(out,(uint16_t)count);size_t n=2;
    for(unsigned i=0;i<count;++i) {out[n++]=10;memcpy(out+n,b->sight[i]->id,32);n+=32;memcpy(out+n,b->f.asset.asset_id,32);n+=32;memcpy(out+n,digest,32);n+=32;}
    return n;
}
static void balance_query(uint8_t out[88],const uint8_t account[32],const uint8_t asset[32],unsigned index) {
    memset(out,0,88);balance_le32(out,1048U+88U*index);balance_le32(out+4,32);
    balance_le32(out+8,1080U+88U*index);balance_le32(out+12,32);
    balance_le32(out+16,32772U+20U*index);balance_le32(out+20,16);
    memcpy(out+24,account,32);memcpy(out+56,asset,32);
}
static int balance_response(balance_fixture *b,const uint8_t **response,size_t *length) {
    const uint8_t *p=b->f.receipt.program_outcome.terminal_payload.bytes;size_t n=b->f.receipt.program_outcome.terminal_payload.length;
    static const char *const wrappers[]={"LXP/programs/terminal-applied-legs/v1","LXP/program-execution-with-transfer-authority/v2","LXP/program-execution-with-occupancy/v1"};
    for(unsigned i=0;i<3;++i){size_t d=strlen(wrappers[i])+1;if(n>=d&&memcmp(p,wrappers[i],d)==0){METERED_CHECK(n>=d+4);size_t inner=balance_be32(p+d);METERED_CHECK(inner<=n-d-4);p+=d+4;n=inner;}}
    static const uint8_t domain[]="LXP/program-execution/v4";
    METERED_CHECK(n>sizeof(domain)+18&&memcmp(p,domain,sizeof(domain))==0);
    size_t at=sizeof(domain)+10;uint64_t values=balance_be64(p+at);at+=8;
    METERED_CHECK(values<65);
    for(uint64_t i=0;i<values;++i){METERED_CHECK(at<n);unsigned tag=p[at++];METERED_CHECK(tag==1||tag==2);at+=tag==1?4:8;METERED_CHECK(at<=n);}
    at+=60;METERED_CHECK(at<n);unsigned trace=p[at++];METERED_CHECK(trace<=1);
    if(trace){METERED_CHECK(at+8<=n);uint64_t z=balance_be64(p+at);at+=8;METERED_CHECK(z<=n-at);at+=(size_t)z;}
    METERED_CHECK(at+35<=n);at+=32;METERED_CHECK(p[at]==0&&p[at+1]==2);at+=2;
    METERED_CHECK(p[at++]==0&&at+12<=n);METERED_CHECK(balance_be32(p+at)==0);at+=4;
    uint64_t z=balance_be64(p+at);at+=8;METERED_CHECK(z<=n-at);*response=p+at;*length=(size_t)z;at+=(size_t)z;
    METERED_CHECK(at+8<=n);z=balance_be64(p+at);at+=8;METERED_CHECK(z==n-at);
    return 0;
}
static int balance_call(balance_fixture *b,unsigned guest,const uint8_t *caps,size_t capn,const uint8_t *input,size_t inputn,uint64_t read_budget,bool index) {
    uint8_t payload[32768];static const uint8_t access[]="LayerX/programs/access-declaration/v1\0";
    size_t n=call_payload_with_data(payload,b->programs[guest],caps,capn,access,sizeof(access),input,inputn);write_u16(payload+32,2);write_u32(payload+46,guest==1U?640U:guest==2U?60U:guest==3U?28U:20U);write_u64(payload+66,read_budget);
    METERED_CHECK(metered_activity(&b->f,LX_PROGRAMS_CALL,payload,n,0x56,false)==0);
    b->f.execution.verified_receipts=index?&b->index:NULL;
    lxp_result rc=lxp_kernel_execute_activity(&b->f.kernel,&b->f.activity,&b->f.execution,&b->f.receipt);
    if(rc!=LXP_OK)fprintf(stderr,"balance call rc=%d receipt=%d\n",(int)rc,(int)b->f.receipt.result_code);
    if(b->f.receipt.result_code!=LXP_OK) {
        const lxp_program_outcome *outcome=&b->f.receipt.program_outcome;
        fprintf(stderr,"balance receipt result=%d kind=%u cpu=%llu read=%llu terminal=",(int)b->f.receipt.result_code,(unsigned)outcome->terminal_kind,(unsigned long long)outcome->cpu_fuel,(unsigned long long)outcome->storage_read_bytes);
        for(size_t i=0;i<outcome->terminal_payload.length;++i)fprintf(stderr,"%02x",outcome->terminal_payload.bytes[i]);
        fputc('\n',stderr);
    }
    METERED_CHECK(rc==LXP_OK);return 0;
}
static int balance_proof_check(balance_fixture *b,const uint8_t account[32],uint64_t expected) {
    lxp_module_ctx ctx;lx_programs_balance_view view;uint8_t root[32];
    METERED_CHECK(lxp_arena_reset(&b->proof_arena,0)==LXP_OK);
    METERED_CHECK(lxp_module_ctx_init(&ctx,&b->f.kernel,LXP_MODULE_PROGRAMS,10,0,b->f.state.next_sequence,1000000,&b->proof_arena,false)==LXP_OK);
    ctx.protocol_version=LXP_PROTOCOL_VERSION_STATE_COMMITMENT;ctx.verified_receipts=&b->index;
    METERED_CHECK(lxp_programs_balance_read(&ctx,account,b->f.asset.asset_id,b->digest,&view)==LXP_OK);
    METERED_CHECK(view.balance.hi==0&&view.balance.lo==expected&&memcmp(view.account.id,account,32)==0);
    METERED_CHECK(memcmp(view.receipt_digest,b->digest,32)==0&&view.observed_sequence==b->f.state.next_sequence-1&&view.observed_at==10);
    METERED_CHECK(memcmp(view.state_root,b->f.kernel.current_state_root,32)==0);
    METERED_CHECK(lx_account_registry_root(&b->f.accounts,root)==LXP_OK&&memcmp(root,view.account_root,32)==0);
    ctx.staged_account_count=1;METERED_CHECK(lxp_programs_balance_read(&ctx,account,b->f.asset.asset_id,b->digest,&view)==LXP_ERR_NON_CANONICAL);ctx.staged_account_count=0;
    ctx.staged_count=1;METERED_CHECK(lxp_programs_balance_read(&ctx,account,b->f.asset.asset_id,b->digest,&view)==LXP_ERR_NON_CANONICAL);ctx.staged_count=0;
    ctx.transfer_applied=true;METERED_CHECK(lxp_programs_balance_read(&ctx,account,b->f.asset.asset_id,b->digest,&view)==LXP_ERR_NON_CANONICAL);ctx.transfer_applied=false;
    lxp_module_ctx_rollback(&ctx);return 0;
}
static int balance_read_check(balance_fixture *b,const uint8_t *caps,size_t capn,uint8_t query[88],int32_t status,uint64_t expected,uint64_t read_budget,bool index) {
    const uint8_t *response;size_t length;
    if(status==16)METERED_CHECK(balance_proof_check(b,query+24,expected)==0);
    METERED_CHECK(balance_call(b,0,caps,capn,query,88,read_budget,index)==0);
    METERED_CHECK(b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==20);
    if(balance_status(response)!=status)fprintf(stderr,"balance status actual=%d expected=%d\n",balance_status(response),status);
    METERED_CHECK(balance_status(response)==status);
    if(status==16)METERED_CHECK(balance_be64(response+4)==0&&balance_be64(response+12)==expected);
    else for(unsigned i=4;i<20;++i)METERED_CHECK(response[i]==0xa5);
    METERED_CHECK(balance_receipt(b)==0);return 0;
}

static size_t balance_one_cap(uint8_t *out,const uint8_t account[32],const uint8_t asset[32],const uint8_t digest[32]) {
    write_u16(out,1);out[2]=10;memcpy(out+3,account,32);memcpy(out+35,asset,32);memcpy(out+67,digest,32);return 99;
}
static int balance_refresh(balance_fixture *b) {
    uint8_t caps[2]={0};const uint8_t *response;size_t length;
    METERED_CHECK(balance_call(b,4,caps,2,NULL,0,1048576,true)==0);
    METERED_CHECK(b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==20);
    METERED_CHECK(balance_receipt(b)==0);return 0;
}
static int balance_malformed(balance_fixture *b,const uint8_t *caps,size_t n,unsigned abi) {
    uint8_t payload[16384],query[88];static const uint8_t access[]="LayerX/programs/access-declaration/v1\0";
    balance_query(query,b->sight[0]->id,b->f.asset.asset_id,0);
    size_t z=call_payload_with_data(payload,b->programs[0],caps,n,access,sizeof(access),query,sizeof(query));write_u16(payload+32,(uint16_t)abi);
    METERED_CHECK(metered_activity(&b->f,LX_PROGRAMS_CALL,payload,z,0x57,false)==0);b->f.execution.verified_receipts=&b->index;
    lxp_result rc=lxp_kernel_execute_activity(&b->f.kernel,&b->f.activity,&b->f.execution,&b->f.receipt);
    METERED_CHECK(rc!=LXP_OK||b->f.receipt.result_code!=LXP_OK);
    METERED_CHECK(balance_refresh(b)==0);return 0;
}
static int balance_access_denial(balance_fixture *b) {
    static const uint8_t domain[]="LayerX/programs/access-declaration/v1";
    static const uint8_t set[]="LayerX/programs/access-set/v1";
    uint8_t access[128],payload[2048],caps[99],query[88];size_t at=0;
    append_bytes(access,&at,domain,sizeof(domain));access[at++]=1;
    write_u32(access+at,(uint32_t)sizeof(set)+6U);at+=4;
    append_bytes(access,&at,set,sizeof(set));memset(access+at,0,6);at+=6;
    balance_one_cap(caps,b->sight[0]->id,b->f.asset.asset_id,b->digest);
    balance_query(query,b->sight[0]->id,b->f.asset.asset_id,0);
    size_t n=call_payload_with_data(payload,b->programs[0],caps,sizeof(caps),access,at,query,sizeof(query));write_u16(payload+32,2);write_u32(payload+46,20);
    METERED_CHECK(balance_execute(b,LX_PROGRAMS_CALL,payload,n)==0&&b->f.receipt.result_code==LXP_OK);
    const uint8_t *response;size_t length;
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==20&&balance_status(response)==-1);
    METERED_CHECK(b->f.receipt.program_outcome.storage_read_bytes==16);
    METERED_CHECK(balance_receipt(b)==0);return 0;
}
static int balance_cases(balance_fixture *b) {
    uint8_t caps[8192],query[88],unknown[32]={0x99},old_digest[32];size_t n;const uint8_t *response;size_t length;
    unsigned zero=0,funded=0;for(unsigned i=0;i<33;++i){if(b->sight[i]->balance.lo==0)zero=i;else funded=i;}
    /* The source account is registered by a signed native activity. */
    uint8_t registration[74];memcpy(registration,b->programs[2],32);memcpy(registration+32,"LXPA1",5);memcpy(registration+37,b->f.asset.asset_id,32);write_u32(registration+69,1);registration[73]='s';
    METERED_CHECK(balance_execute(b,LX_PROGRAMS_ACCOUNT,registration,sizeof(registration))==0&&b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(lxp_programs_account_derive(b->programs[2],registration+73,1,b->source)==LXP_OK);
    METERED_CHECK(balance_receipt(b)==0);
    uint8_t spending[128]={0};spending[0]='s';memcpy(spending+32,b->source,32);memcpy(spending+64,b->f.asset.asset_id,32);memcpy(spending+96,b->source,32);
    write_u16(caps,1);caps[2]=5;memcpy(caps+3,b->f.asset.asset_id,32);memcpy(caps+35,b->source,32);memset(caps+67,0,16);write_u64(caps+75,2);
    METERED_CHECK(balance_call(b,2,caps,83,spending,sizeof(spending),1048576,true)==0&&b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==60);
    METERED_CHECK(balance_status(response)==0&&balance_status(response+20)==-1&&balance_status(response+40)==0);
    METERED_CHECK(balance_receipt(b)==0);
    lx_account *source=program_spend_account(&b->f.accounts,b->source);METERED_CHECK(source!=NULL&&source->balance.hi==0&&source->balance.lo==2);
    n=balance_one_cap(caps,b->source,b->f.asset.asset_id,b->digest);balance_query(query,b->source,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,16,2,1048576,true)==0);
    puts("BALANCE_READ_CASE BAL-01 ok");

    n=balance_one_cap(caps,b->sight[zero]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[zero]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,16,0,1048576,true)==0);
    n=balance_one_cap(caps,unknown,b->f.asset.asset_id,b->digest);balance_query(query,unknown,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,-7,0,1048576,true)==0);
    write_u16(caps,0);METERED_CHECK(balance_read_check(b,caps,2,query,-1,0,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[zero]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,-1,0,1048576,true)==0);
    puts("BALANCE_READ_CASE BAL-02 ok");

    memcpy(old_digest,b->digest,32);METERED_CHECK(balance_refresh(b)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,old_digest);balance_query(query,b->sight[funded]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,-5,0,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,unknown);
    METERED_CHECK(balance_read_check(b,caps,n,query,-5,0,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);
    METERED_CHECK(balance_read_check(b,caps,n,query,-5,0,1048576,false)==0);
    n=balance_one_cap(caps,unknown,b->f.asset.asset_id,unknown);balance_query(query,unknown,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,-5,0,1048576,true)==0);
    lxp_receipt altered=b->f.receipt;altered.resulting_state_root[0]^=1;uint8_t key[32];
    METERED_CHECK(executed_public_key(executed_sequencer_seed,key)==0);
    METERED_CHECK(lxp_verified_receipt_index_add(&b->index,&altered,key,&b->proof_arena)!=LXP_OK);
    METERED_CHECK(lxp_verified_receipt_index_add(&b->index,&b->f.receipt,b->f.owner_key,&b->proof_arena)!=LXP_OK);
    {
        uint8_t rejected_registration[74];
        lxp_verified_receipt_facts verified;
        size_t account_count = b->f.accounts.count;
        memcpy(rejected_registration, registration, sizeof(rejected_registration));
        memcpy(rejected_registration + 37U, unknown, 32U);
        METERED_CHECK(balance_execute(b, LX_PROGRAMS_ACCOUNT,
            rejected_registration, sizeof(rejected_registration)) == 0);
        METERED_CHECK(b->f.receipt.result_code == LXP_ERR_ASSET_MISMATCH);
        METERED_CHECK(b->f.accounts.count == account_count &&
            source->balance.hi == 0U && source->balance.lo == 2U);
        METERED_CHECK(balance_receipt(b) == 0);
        METERED_CHECK(lxp_verified_receipt_index_lookup(
            &b->index, b->digest, &verified) == LXP_OK);
        METERED_CHECK(verified.result_code == LXP_ERR_ASSET_MISMATCH &&
            verified.global_sequence == b->f.state.next_sequence - 1U &&
            memcmp(verified.resulting_state_root,
                b->f.kernel.current_state_root, 32U) == 0);
        n = balance_one_cap(caps, b->sight[funded]->id,
            b->f.asset.asset_id, b->digest);
        balance_query(query, b->sight[funded]->id, b->f.asset.asset_id, 0U);
        METERED_CHECK(balance_read_check(b, caps, n, query,
            -5, 0U, 1048576U, true) == 0);
    }
    puts("BALANCE_READ_CASE BAL-03 ok");

    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[funded]->id,unknown,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,-1,0,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,unknown,b->digest);
    METERED_CHECK(balance_read_check(b,caps,n,query,-7,0,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);memcpy(caps+n,caps+2,97);write_u16(caps,2);
    METERED_CHECK(balance_malformed(b,caps,n+97,2)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);memcpy(caps+n,caps+2,97);caps[n+96]^=1;write_u16(caps,2);
    METERED_CHECK(balance_malformed(b,caps,n+97,2)==0);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);
    METERED_CHECK(balance_malformed(b,caps,n-1,2)==0);caps[n]=0;METERED_CHECK(balance_malformed(b,caps,n+1,2)==0);
    METERED_CHECK(balance_access_denial(b)==0);
    puts("BALANCE_READ_CASE BAL-04 ok");

    uint8_t queries[32*88];n=balance_caps(b,caps,32,b->digest);
    for(unsigned i=0;i<32;++i)balance_query(queries+88*i,b->sight[i]->id,b->f.asset.asset_id,i);
    METERED_CHECK(balance_call(b,1,caps,n,queries,sizeof(queries),1048576,true)==0&&b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==640);
    for(unsigned i=0;i<32;++i){METERED_CHECK(balance_status(response+20*i)==16);METERED_CHECK(balance_be64(response+20*i+4)==0&&balance_be64(response+20*i+12)==b->sight[i]->balance.lo);}
    METERED_CHECK(b->f.receipt.program_outcome.storage_read_bytes==512);METERED_CHECK(balance_receipt(b)==0);
    n=balance_caps(b,caps,33,b->digest);METERED_CHECK(balance_malformed(b,caps,n,2)==0);
    puts("BALANCE_READ_CASE BAL-05 ok");

    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[funded]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,16,b->sight[funded]->balance.lo,16,true)==0);
    METERED_CHECK(b->f.receipt.program_outcome.storage_read_bytes==16);
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);
    METERED_CHECK(balance_call(b,0,caps,n,query,sizeof(query),15,true)==0);
    METERED_CHECK(b->f.receipt.program_outcome.terminal_kind==LXP_PROGRAM_TERMINAL_RESOURCE);
    METERED_CHECK(balance_refresh(b)==0);
    const unsigned fields[]={0,4,4,8,12,12,16,20,20};
    const uint32_t values[]={UINT32_MAX,31,33,UINT32_MAX,31,33,65530,15,UINT32_MAX};
    for(unsigned i=0;i<sizeof(fields)/sizeof(fields[0]);++i){n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[funded]->id,b->f.asset.asset_id,0);balance_le32(query+fields[i],values[i]);METERED_CHECK(balance_read_check(b,caps,n,query,-3,0,1048576,true)==0);METERED_CHECK(b->f.receipt.program_outcome.storage_read_bytes==(fields[i]==16?16U:0U));}
    puts("BALANCE_READ_CASE BAL-06 ok");

    n=balance_caps(b,caps,32,b->digest);uint64_t balances[33];for(unsigned i=0;i<33;++i)balances[i]=b->sight[i]->balance.lo;
    memcpy(spending+96,b->sight[zero]->id,32);lxp_u128 actor_before=b->f.actor->balance;
    METERED_CHECK(balance_call(b,2,caps,n,spending,sizeof(spending),1048576,true)==0&&b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==60);
    for(unsigned i=0;i<3;++i)METERED_CHECK(balance_status(response+20*i)==-1);
    for(unsigned i=0;i<33;++i)METERED_CHECK(balances[i]==b->sight[i]->balance.lo);
    METERED_CHECK(source->balance.lo==2&&source->balance.hi==0);
    METERED_CHECK(actor_before.hi==0&&b->f.actor->balance.hi==0&&actor_before.lo-b->f.actor->balance.lo==b->f.receipt.fee_charged.lo);
    METERED_CHECK(balance_receipt(b)==0);
    uint8_t nested[512]={0};memcpy(nested,b->programs[0],32);balance_le32(nested+32,88);balance_le32(nested+36,99);
    balance_query(nested+40,b->sight[funded]->id,b->f.asset.asset_id,0);
    balance_one_cap(nested+128,b->sight[funded]->id,b->f.asset.asset_id,b->digest);
    write_u16(caps,2);caps[2]=4;memcpy(caps+3,b->programs[0],32);memcpy(caps+35,nested+130,97);
    METERED_CHECK(balance_call(b,3,caps,132,nested,227,1048576,true)==0&&b->f.receipt.result_code==LXP_OK);
    METERED_CHECK(balance_response(b,&response,&length)==0&&length==28);
    METERED_CHECK(balance_status(response+8)==16&&balance_be64(response+20)==b->sight[funded]->balance.lo);
    METERED_CHECK(balance_receipt(b)==0);
    balance_le32(nested+36,83);
    write_u16(nested+128,1);nested[130]=5;memcpy(nested+131,b->f.asset.asset_id,32);memcpy(nested+163,b->sight[zero]->id,32);memset(nested+195,0,16);write_u64(nested+203,1);
    write_u16(caps,2);caps[2]=4;memcpy(caps+3,b->programs[0],32);uint8_t childcap[99];balance_one_cap(childcap,b->sight[funded]->id,b->f.asset.asset_id,b->digest);
    memcpy(caps+35,childcap+2,97);
    METERED_CHECK(balance_call(b,3,caps,132,nested,211,1048576,true)==0);
    METERED_CHECK(b->f.receipt.result_code!=LXP_OK);
    for(unsigned i=0;i<33;++i)METERED_CHECK(balances[i]==b->sight[i]->balance.lo);
    METERED_CHECK(source->balance.lo==2);
    METERED_CHECK(balance_refresh(b)==0);
    puts("BALANCE_READ_CASE BAL-07 ok");

    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[funded]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,16,b->sight[funded]->balance.lo,1048576,true)==0);
    n=balance_one_cap(caps,b->sight[zero]->id,b->f.asset.asset_id,b->digest);balance_query(query,b->sight[zero]->id,b->f.asset.asset_id,0);
    METERED_CHECK(balance_read_check(b,caps,n,query,16,0,1048576,true)==0);
    puts("BALANCE_READ_CASE BAL-08 ok");
    n=balance_one_cap(caps,b->sight[funded]->id,b->f.asset.asset_id,b->digest);METERED_CHECK(balance_malformed(b,caps,n,1)==0);
    puts("BALANCE_READ_CASE BAL-09 ok");
    puts("BALANCE_READ_COMPLETE cases=9 skipped=0");return 0;
}
int main(int argc,char **argv) {
    if(argc!=3||(strcmp(argv[1],"--emit-guests")!=0&&strcmp(argv[1],"--guest-dir")!=0))return 2;
    balance_fixture *b=calloc(1,sizeof(*b));if(b==NULL)return 1;bool emit=strcmp(argv[1],"--emit-guests")==0;
    int status=balance_guests(b,argv[2],emit);if(!status&&!emit)status=balance_init(b);if(!status&&!emit)status=balance_cases(b);free(b);return status;
}
