#define _POSIX_C_SOURCE 200809L
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_state_proof.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
struct maintenance_fixture;
static int replay_authority_resolve(struct maintenance_fixture *, const lxp_activity *, lxp_kernel_execution *, size_t);
static const lxp_receipt *market_receipts(const lxp_kernel_prepared_batch *);
#define lxp_kernel_prepared_batch_receipts market_receipts
#include "replay-authority-fixture-base.inc"
#undef lxp_kernel_prepared_batch_receipts
static lxp_authority_grant replay_authority_grants[64];
static lxp_authority_resolved replay_authorities[64];

static int replay_authority_resolve(maintenance_fixture *f,
    const lxp_activity *activity, lxp_kernel_execution *execution, size_t index)
{
    const lxp_module_registration *registration = NULL;
    lxp_authority_envelope envelope;
    lxp_authority_grant *grant;
    lxp_authority_resolved *authority;
    bool owner_key_valid;
    CHECK(f != NULL && activity != NULL && execution != NULL && index < 64U);
    CHECK(lxp_activity_verify_signature(activity) == LXP_OK);
    CHECK(lxp_governance_identity_refresh(&f->kernel, f->identity) == LXP_OK);
    CHECK(lxp_kernel_module_for_activity(&f->kernel, activity->activity_type,
        execution->epoch, &registration) == LXP_OK && registration != NULL);
    execution->recorded_module_version = registration->abi_version;
    grant = &replay_authority_grants[index];
    authority = &replay_authorities[index];
    owner_key_valid = lxp_identity_key_valid(f->identity, activity->authority.bytes,
        execution->batch_timestamp_ms, execution->global_sequence);
    CHECK(owner_key_valid);
    CHECK(lxp_authority_resolve_activity(&f->kernel, f->identity, activity,
        owner_key_valid, true, execution->batch_timestamp_ms,
        execution->maximum_timestamp_window, execution->global_sequence,
        grant, authority) == LXP_OK);
    CHECK(lxp_authority_envelope_declare(&f->kernel, f->kernel.epoch,
        &envelope) == LXP_OK);
    CHECK(authority->kind == LXP_AUTHORITY_OWNER && authority->scope == &grant->scope);
    CHECK(!lxp_ct_is_zero(authority->grant_id, 32U));
    CHECK(authority->scope->module_mask == envelope.module_mask &&
        authority->scope->activity_ordinal_min == envelope.activity_ordinal_min &&
        authority->scope->activity_ordinal_max == envelope.activity_ordinal_max);
    CHECK(lxp_u128_is_zero(authority->scope->maximum_per_activity) &&
        lxp_u128_is_zero(authority->scope->maximum_total) &&
        lxp_u128_is_zero(authority->scope->maximum_per_period) &&
        lxp_u128_is_zero(authority->scope->spent_total) &&
        lxp_u128_is_zero(authority->scope->spent_this_period));
    CHECK(memcmp(authority->actor, f->identity->did_id, 32U) == 0 &&
        memcmp(authority->principal, f->identity->did_id, 32U) == 0 &&
        memcmp(authority->verified_key, f->identity->primary_key, 32U) == 0);
    execution->authority = authority;
    return 0;
}


static uint32_t market_u32(const uint8_t *p) { return ((uint32_t)p[0]<<24U)|((uint32_t)p[1]<<16U)|((uint32_t)p[2]<<8U)|p[3]; }
static const char *market_directory;
static uint8_t market_program[32] = {0x61U};
static const char *market_names[] = {"integer", "trap", "incorrect"};
static const uint8_t tenant_seed[32] = {0x56U};
static const uint8_t tenant_did[] = "did:lxp:market-tenant";
static lxp_identity *market_identities[2];
static lx_account *market_accounts[2];
static uint8_t market_public_keys[2][32], market_commitments[3][32];
static uint8_t market_destinations[6][32];
static uint64_t market_batch;
static int market_capture, market_capture_error;
static const char *market_capture_name, *market_capture_role;
static FILE *market_manifest;

static int market_hex(FILE *stream, const uint8_t *bytes, size_t length)
{
    for (size_t i=0U;i<length;++i) CHECK(fprintf(stream,"%02x",bytes[i])>0);
    return 0;
}
static int market_read(const char *name, uint8_t **bytes, size_t *length)
{
    FILE *file=fopen(name,"rb"); long size;
    CHECK(file!=NULL && fseek(file,0,SEEK_END)==0);
    size=ftell(file); CHECK(size>0 && (uint64_t)size<LXP_MAX_ACTIVITY_BYTES);
    CHECK(fseek(file,0,SEEK_SET)==0);
    *bytes=malloc((size_t)size);CHECK(*bytes!=NULL);
    *length=fread(*bytes,1,(size_t)size,file);
    CHECK(*length==(size_t)size && !ferror(file) && fclose(file)==0);
    return 0;
}
static const lxp_module_blob *market_blob(const lxp_kernel *kernel, const uint8_t hash[32])
{
    for(size_t i=0;i<kernel->blob_count;++i)
        if(kernel->blobs[i].module_id==9U && !kernel->blobs[i].deleted && memcmp(kernel->blobs[i].key,hash,32U)==0) return &kernel->blobs[i];
    return NULL;
}
static int market_namespace(const lxp_kernel *kernel, const lxp_receipt *receipt)
{
    uint8_t key[41],root[32],*encoded=malloc(LXP_STATE_WITNESS_MAX_BYTES);
    lxp_state_witness *proof=calloc(1,sizeof(*proof));
    const lxp_module_blob *manifest;
    char name[160]; size_t length,cursor=6U;
    CHECK(proof!=NULL && encoded!=NULL);
    memcpy(key,"progstor",8U);memcpy(key+8U,market_program,32U);key[40]=1U;
    CHECK(lxp_state_root(kernel,root)==LXP_OK && memcmp(root,receipt->resulting_state_root,32U)==0);
    CHECK(lxp_state_proof_build(kernel,9U,(lxp_byte_span){key,sizeof(key)},proof)==LXP_OK);
    CHECK(lxp_state_proof_verify(proof,root)==LXP_OK && proof->value_length==38U);
    manifest=market_blob(kernel,proof->value+6U);CHECK(manifest!=NULL && manifest->length>=6U);
    CHECK(lxp_state_proof_encode(proof,encoded,LXP_STATE_WITNESS_MAX_BYTES,&length)==LXP_OK);
    CHECK(snprintf(name,sizeof(name),"%s-%s.head",market_capture_name,market_capture_role)>0);
    CHECK(write_evidence_file(market_directory,name,encoded,length)==0);
    CHECK(fprintf(market_manifest,"\"%s_namespace\":{\"head\":\"%s\",",market_capture_role,name)>0);
    CHECK(snprintf(name,sizeof(name),"%s-%s.manifest",market_capture_name,market_capture_role)>0);
    CHECK(write_evidence_file(market_directory,name,manifest->bytes,manifest->length)==0);
    CHECK(fprintf(market_manifest,"\"manifest\":\"%s\",\"value_blobs\":[",name)>0);
    for(size_t i=0;i<market_u32(manifest->bytes+2U);++i) {
        uint16_t key_length; const lxp_module_blob *value;
        CHECK(cursor+2U<=manifest->length);
        key_length=(uint16_t)(((uint16_t)manifest->bytes[cursor]<<8U)|manifest->bytes[cursor+1U]);cursor+=2U;
        CHECK(cursor+key_length+36U<=manifest->length);
        value=market_blob(kernel,manifest->bytes+cursor+key_length);CHECK(value!=NULL);
        CHECK(snprintf(name,sizeof(name),"%s-%s-value-%zu",market_capture_name,market_capture_role,i)>0);
        CHECK(write_evidence_file(market_directory,name,value->bytes,value->length)==0);
        CHECK(fprintf(market_manifest,"%s{\"sha256\":\"",i?",":"")>0);
        CHECK(market_hex(market_manifest,value->key,32U)==0);
        CHECK(fprintf(market_manifest,"\",\"path\":\"%s\"}",name)>0);
        cursor+=key_length+36U;
    }
    CHECK(cursor==manifest->length && fprintf(market_manifest,"]},")>0);
    free(proof);free(encoded);return 0;
}
static const lxp_receipt *market_receipts(const lxp_kernel_prepared_batch *batch)
{
    const lxp_receipt *receipts=lxp_kernel_prepared_batch_receipts(batch);
    if(market_capture && batch==transport_prepared) {
        const lxp_kernel *settled=lxp_kernel_prepared_batch_settled_kernel(batch);
        market_capture=0;
        if(receipts==NULL || settled==NULL || lxp_kernel_prepared_batch_count(batch)!=1U ||
            receipts[0].result_code!=LXP_OK || market_namespace(settled,receipts)!=0) market_capture_error=1;
    }
    return receipts;
}
static int market_sign(lxp_activity *activity,const uint8_t seed[32],uint8_t signature[64])
{
    uint8_t preimage[32];size_t length=64U;
    EVP_PKEY *key=EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519,NULL,seed,32U);
    EVP_MD_CTX *ctx=EVP_MD_CTX_new();
    CHECK(key!=NULL && ctx!=NULL && lxp_activity_signing_preimage(activity,preimage)==LXP_OK);
    CHECK(EVP_DigestSignInit(ctx,NULL,NULL,NULL,key)==1 && EVP_DigestSign(ctx,signature,&length,preimage,32U)==1 && length==64U);
    EVP_MD_CTX_free(ctx);EVP_PKEY_free(key);activity->signature=(lxp_byte_span){signature,64U};return 0;
}
static int market_publish(maintenance_fixture *f,unsigned actor,uint32_t type,const uint8_t *payload,size_t length)
{
    lxp_activity activity;uint8_t signature[64];
    const uint8_t *did=actor?tenant_did:maintenance_did;
    size_t did_length=actor?sizeof(tenant_did)-1U:sizeof(maintenance_did)-1U;
    f->identity=market_identities[actor];f->actor=market_accounts[actor];
    memcpy(f->actor_public_key,market_public_keys[actor],32U);
    fill_activity(&activity,type,payload,length,did,did_length,f->actor_public_key);
    activity.protocol_version=3U;activity.network_id=7U;
    activity.account_sequence=f->identity->next_sequence;
    write_u64(activity.idempotency_key,activity.account_sequence+1U);
    activity.fee_limit=type==LX_PROGRAMS_CALL?(lxp_u128){0U,1000000000U}:(lxp_u128){0U,0U};
    CHECK(market_sign(&activity,actor?tenant_seed:maintenance_actor_seed,signature)==0);
    f->input_activity=&activity;
    ++market_batch;
    CHECK(replay_publish(f,type,payload,length,1U,market_batch,LXP_OK,true)==0);
    f->input_activity=NULL;CHECK(!market_capture_error);return 0;
}
static int market_destination_compare(const void *a,const void *b) { return memcmp(a,b,32U); }
static size_t market_interface(uint8_t *out,const uint8_t hash[32])
{
    size_t n=interface_payload(out,hash,LX_PROGRAMS_GUEST_ABI_V2_VERSION,INTERFACE_CAPABILITIES_NONE)-6U;
    size_t type_bound=sizeof("LayerX/program-interface/v1")+32U+2U+2U+2U+11U+4U+2U;
    write_u32(out+type_bound,1048576U);write_u32(out+type_bound+6U,1048576U);
    write_u16(out+n,9U);n+=2U;out[n++]=2U;out[n++]=3U;out[n++]=4U;
    for(size_t i=0;i<6U;++i){out[n++]=6U;memcpy(out+n,(uint8_t[32]){9U},32U);n+=32U;memcpy(out+n,market_destinations[i],32U);n+=32U;memset(out+n,0,16U);write_u64(out+n+8U,1000000000U);n+=16U;}
    write_u16(out+n,0U);n+=2U;write_u16(out+n,0U);return n+2U;
}
static int market_deploy(maintenance_fixture *f,const uint8_t program[32],const char *path,bool market)
{
    uint8_t *wasm,*payload=malloc(LXP_MAX_ACTIVITY_BYTES),hash[32];size_t wasm_length,n,iface;
    CHECK(payload!=NULL && market_read(path,&wasm,&wasm_length)==0);
    n=deploy_payload(payload,program,market_identities[0]->did_id,wasm,wasm_length,hash,LX_PROGRAMS_GUEST_ABI_V2_VERSION,INTERFACE_CAPABILITIES_NONE);
    if(market){iface=market_interface(payload+DEPLOY_FIXED_BYTES,hash);write_u32(payload+104U,(uint32_t)iface);memcpy(payload+DEPLOY_FIXED_BYTES+iface,wasm,wasm_length);n=DEPLOY_FIXED_BYTES+iface+wasm_length;}
    CHECK(market_publish(f,0U,LX_PROGRAMS_DEPLOY,payload,n)==0);
    free(wasm);free(payload);return 0;
}
static int market_call(maintenance_fixture *f,unsigned actor,const uint8_t *data,size_t length,const uint8_t *destination)
{
    static const uint8_t absent[]="LayerX/programs/access-declaration/v1\0";
    uint8_t capabilities[128],*payload=malloc(LXP_MAX_ACTIVITY_BYTES);size_t n=2U,payload_length;
    CHECK(payload!=NULL);write_u16(capabilities,destination?4U:3U);capabilities[n++]=3U;
    if(destination){capabilities[n++]=5U;memcpy(capabilities+n,f->asset.asset_id,32U);n+=32U;memcpy(capabilities+n,destination,32U);n+=32U;memset(capabilities+n,0,16U);write_u64(capabilities+n+8U,1000000000U);n+=16U;}
    capabilities[n++]=7U;capabilities[n++]=8U;
    payload_length=call_payload_with_data(payload,market_program,capabilities,n,absent,sizeof(absent),data,length);
    write_u16(payload+32U,LX_PROGRAMS_GUEST_ABI_V2_VERSION);
    CHECK(market_publish(f,actor,LX_PROGRAMS_CALL,payload,payload_length)==0);free(payload);return 0;
}
static int market_policy_commitment(const lxp_kernel *kernel,const uint8_t lease[32],uint8_t output[32])
{
    uint8_t key[41];const lxp_module_blob *manifest,*value=NULL;const uint8_t *head=NULL;size_t cursor=6U;
    memcpy(key,"progstor",8U);memcpy(key+8U,market_program,32U);key[40]=1U;
    for(size_t i=0;i<kernel->module_kv_count;++i)if(kernel->module_kv[i].module_id==9U && kernel->module_kv[i].key_length==41U && memcmp(kernel->module_kv[i].key,key,41U)==0)head=kernel->module_kv[i].value;
    CHECK(head!=NULL);manifest=market_blob(kernel,head+6U);CHECK(manifest!=NULL);
    for(size_t i=0;i<market_u32(manifest->bytes+2U);++i){uint16_t k=(uint16_t)((manifest->bytes[cursor]<<8U)|manifest->bytes[cursor+1U]);cursor+=2U;CHECK(cursor+k+36U<=manifest->length);if(k==sizeof("lx.market.attesters/")-1U+32U && memcmp(manifest->bytes+cursor,"lx.market.attesters/",sizeof("lx.market.attesters/")-1U)==0 && memcmp(manifest->bytes+cursor+sizeof("lx.market.attesters/")-1U,lease,32U)==0)value=market_blob(kernel,manifest->bytes+cursor+k);cursor+=k+36U;}
    CHECK(value!=NULL && value->length>250U && value->bytes[73U]==1U && value->bytes[218U]==1U);
    memcpy(output,value->bytes+219U,32U);return 0;
}
int main(int argc,char **argv)
{
    maintenance_fixture *f=calloc(1,sizeof(*f));
    uint8_t data[4096],lease[3][32],offer[3][32],claim[3][32],sandbox[3][32],escrow[3][32],stake[3][32];
    char path[4096],line[64];FILE *setup;size_t n;uint8_t id[32];
    static const uint8_t tenant_name[]="agent:did:lxp:market-tenant:main";
    CHECK(argc==3 && f!=NULL);market_directory=argv[1];CHECK(maintenance_fixture_open(f)==0);
    CHECK(lxp_state_store_bind_accounts(&f->state,&f->accounts)==LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel,lxp_governance_module_iface())==LXP_OK);
    market_identities[0]=f->identity;market_accounts[0]=f->actor;memcpy(market_public_keys[0],f->actor_public_key,32U);
    CHECK(executed_public_key(tenant_seed,market_public_keys[1])==0);
    CHECK(lxp_identity_register(&f->identities,tenant_did,sizeof(tenant_did)-1U,market_public_keys[1],&market_identities[1])==LXP_OK);
    CHECK(lx_account_id_from_string(tenant_name,sizeof(tenant_name)-1U,id)==LXP_OK);
    CHECK(lx_account_open(&f->accounts,tenant_name,sizeof(tenant_name)-1U,id,4U,LX_ACCOUNT_OPEN_GENESIS,NULL,&market_accounts[1])==LXP_OK);
    CHECK(lxp_ledger_bootstrap_balance(market_accounts[1],f->asset.asset_id,(lxp_u128){0U,UINT64_C(1000000000000)},1U)==LXP_OK);
    market_accounts[1]->has_authority_key=true;memcpy(market_accounts[1]->authority_key,market_public_keys[1],32U);
    CHECK(lxp_state_root(&f->kernel,f->kernel.current_state_root)==LXP_OK);
    f->kernel.execution_prestate_capture_enabled=true;f->evidence.arbiter_admission_prestate_enabled=true;
    f->evidence_directory=market_directory;f->arbiter_export=true;f->arbiter_first_capture=true;
    transport_store=&f->evidence;transport_arena=&f->arena;
    CHECK(snprintf(path,sizeof(path),"%s/inputs.json",market_directory)>0);f->arbiter_manifest=fopen(path,"wbx");CHECK(f->arbiter_manifest!=NULL);
    CHECK(fprintf(f->arbiter_manifest,"{\"network_id\":7,\"sequencer_public_key\":\"")>0 && market_hex(f->arbiter_manifest,f->authorization.public_key,32U)==0);
    CHECK(fprintf(f->arbiter_manifest,"\",\"sequencer_id\":\"")>0 && market_hex(f->arbiter_manifest,f->authorization.sequencer_id,32U)==0);
    CHECK(fprintf(f->arbiter_manifest,"\",\"first_batch_number\":1,\"last_batch_number\":100,\"captures\":[")>0);
    for(unsigned actor=0;actor<2U;++actor){memset(data,0,68U);data[0]=0x71U;data[1]=1U;data[3]=2U;memcpy(data+4U,market_identities[actor]->did_id,32U);memcpy(data+36U,market_public_keys[actor],32U);CHECK(market_publish(f,actor,UINT32_C(0x00070001),data,68U)==0);}
    for(size_t i=0;i<3U;++i){memset(lease[i],0x21+(int)i,32U);memset(offer[i],0x31+(int)i,32U);memset(claim[i],0x41+(int)i,32U);memset(sandbox[i],0,32U);sandbox[i][0]=0x71U+(uint8_t)i;CHECK(lxp_programs_account_derive(market_program,offer[i],32U,stake[i])==LXP_OK);CHECK(lxp_programs_account_derive(market_program,lease[i],32U,escrow[i])==LXP_OK);memcpy(market_destinations[2U*i],stake[i],32U);memcpy(market_destinations[2U*i+1U],escrow[i],32U);}
    qsort(market_destinations,6U,32U,market_destination_compare);CHECK(market_deploy(f,market_program,argv[2],true)==0);
    for(size_t i=0;i<3U;++i){CHECK(snprintf(path,sizeof(path),"%s/%s.wasm",market_directory,market_names[i])>0);CHECK(market_deploy(f,sandbox[i],path,false)==0);
        n=0U;data[n++]=1U;data[n++]=1U;memcpy(data+n,offer[i],32U);n+=32U;memcpy(data+n,market_identities[0]->did_id,32U);n+=32U;memcpy(data+n,market_accounts[0]->id,32U);n+=32U;memcpy(data+n,f->asset.asset_id,32U);n+=32U;memcpy(data+n,stake[i],32U);n+=32U;write_u16(data+n,32U);n+=2U;memcpy(data+n,offer[i],32U);n+=32U;memset(data+n,0,16U);write_u64(data+n+8U,10000U);n+=16U;memset(data+n,0,16U);write_u64(data+n+8U,1U);n+=16U;write_u64(data+n,10000000000U);n+=8U;write_u64(data+n,1U);n+=8U;write_u64(data+n,1000000000U);n+=8U;write_u64(data+n,1000U);n+=8U;data[n++]=3U;CHECK(market_call(f,0U,data,n,stake[i])==0);
        n=0U;data[n++]=1U;data[n++]=2U;memcpy(data+n,lease[i],32U);n+=32U;memcpy(data+n,offer[i],32U);n+=32U;memcpy(data+n,market_identities[1]->did_id,32U);n+=32U;memcpy(data+n,market_accounts[1]->id,32U);n+=32U;memcpy(data+n,escrow[i],32U);n+=32U;write_u16(data+n,32U);n+=2U;memcpy(data+n,lease[i],32U);n+=32U;write_u64(data+n,1000000000U);n+=8U;memset(data+n,0,16U);write_u64(data+n+8U,1000000000U);n+=16U;write_u64(data+n,900U);n+=8U;CHECK(market_call(f,1U,data,n,escrow[i])==0);
        n=0U;data[n++]=1U;data[n++]=6U;memcpy(data+n,lease[i],32U);n+=32U;write_u64(data+n,1U);n+=8U;data[n++]=1U;data[n++]=1U;data[n++]='a';memcpy(data+n,market_public_keys[0],32U);n+=32U;CHECK(market_call(f,1U,data,n,NULL)==0);
        data[0]=1U;data[1]=8U;memcpy(data+2U,lease[i],32U);CHECK(market_call(f,1U,data,34U,NULL)==0);CHECK(market_policy_commitment(&f->kernel,lease[i],market_commitments[i])==0);
    }
    CHECK(snprintf(path,sizeof(path),"%s/setup.json",market_directory)>0);setup=fopen(path,"wbx");CHECK(setup!=NULL);
    CHECK(fprintf(setup,"{\"network_id\":7,\"market_program\":\"")>0 && market_hex(setup,market_program,32U)==0);
    CHECK(fprintf(setup,"\",\"provider\":\"")>0 && market_hex(setup,market_identities[0]->did_id,32U)==0);
    CHECK(fprintf(setup,"\",\"tenant\":\"")>0 && market_hex(setup,market_identities[1]->did_id,32U)==0);
    CHECK(fprintf(setup,"\",\"payment_account\":\"")>0 && market_hex(setup,market_accounts[1]->id,32U)==0 && fprintf(setup,"\",\"cases\":[")>0);
    for(size_t i=0;i<3U;++i){CHECK(fprintf(setup,"%s{\"name\":\"%s\",\"sandbox_program\":\"",i?",":"",market_names[i])>0 && market_hex(setup,sandbox[i],32U)==0);CHECK(fprintf(setup,"\",\"offer_id\":\"")>0 && market_hex(setup,offer[i],32U)==0);CHECK(fprintf(setup,"\",\"lease_id\":\"")>0 && market_hex(setup,lease[i],32U)==0);CHECK(fprintf(setup,"\",\"claim_id\":\"")>0 && market_hex(setup,claim[i],32U)==0);CHECK(fprintf(setup,"\",\"attested_input_commitment\":\"")>0 && market_hex(setup,market_commitments[i],32U)==0);CHECK(fprintf(setup,"\",\"interval_start\":%llu,\"interval_end\":%llu,\"billing_height\":%llu,\"response_deadline\":%llu}",(unsigned long long)(market_batch+2U*i+1U),(unsigned long long)(market_batch+2U*i+2U),(unsigned long long)(market_batch+2U*i+2U),(unsigned long long)(market_batch+2U*i+3U))>0);}
    CHECK(fprintf(setup,"]}\n")>0 && fclose(setup)==0);puts("MARKET_CAPTURE_READY setup");CHECK(fflush(stdout)==0 && fgets(line,sizeof(line),stdin)!=NULL && strcmp(line,"continue\n")==0);
    CHECK(snprintf(path,sizeof(path),"%s/market-native.json",market_directory)>0);market_manifest=fopen(path,"wbx");CHECK(market_manifest!=NULL);
    CHECK(fprintf(market_manifest,"{\"admission_inputs\":\"inputs.json\",\"market_program\":\"")>0 && market_hex(market_manifest,market_program,32U)==0 && fprintf(market_manifest,"\",\"cases\":[")>0);
    for(size_t i=0;i<3U;++i){uint8_t *bytes;size_t length;CHECK(fprintf(market_manifest,"%s{\"name\":\"%s\",",i?",":"",market_names[i])>0);market_capture_name=market_names[i];
        CHECK(snprintf(path,sizeof(path),"%s/%s-profile.bin",market_directory,market_names[i])>0 && market_read(path,&bytes,&length)==0);data[0]=1U;data[1]=13U;CHECK(length+2U<=sizeof(data));memcpy(data+2U,bytes,length);free(bytes);market_capture_role="tenant";market_capture=1;CHECK(market_call(f,1U,data,length+2U,NULL)==0);CHECK(fprintf(market_manifest,"\"tenant_receipt\":\"batch-%llu-0.receipt\",",(unsigned long long)market_batch)>0);
        CHECK(snprintf(path,sizeof(path),"%s/%s-billing.bin",market_directory,market_names[i])>0 && market_read(path,&bytes,&length)==0);data[0]=1U;data[1]=14U;memcpy(data+2U,lease[i],32U);CHECK(length+34U<=sizeof(data));memcpy(data+34U,bytes,length);free(bytes);market_capture_role="provider";market_capture=1;CHECK(market_call(f,0U,data,length+34U,NULL)==0);CHECK(fprintf(market_manifest,"\"provider_receipt\":\"batch-%llu-0.receipt\",\"baseline_namespace\":null}",(unsigned long long)market_batch)>0);
    }
    CHECK(fprintf(market_manifest,"]}\n")>0 && fclose(market_manifest)==0);CHECK(fprintf(f->arbiter_manifest,"]}\n")>0 && fclose(f->arbiter_manifest)==0);CHECK(transport_reopen(f)==0);CHECK(arbiter_close_fixture(f)==0);puts("MARKET_SANDBOX_CAPTURE genuine-tenant-provider-guest-namespace-runtime");return 0;
}
