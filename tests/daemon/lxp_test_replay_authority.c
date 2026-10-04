#define _POSIX_C_SOURCE 200809L
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_state_proof.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
struct maintenance_fixture;
static int replay_authority_resolve(struct maintenance_fixture *f,
    const lxp_activity *activity, lxp_kernel_execution *execution, size_t index);
static void replay_fixture_destroy(lxp_kernel_prepared_batch *batch);
#define lxp_kernel_prepared_batch_destroy replay_fixture_destroy
#include "replay-authority-fixture-base.inc"
#undef lxp_kernel_prepared_batch_destroy
static const char *replay_directory;
static unsigned replay_records;
static uint64_t replay_batch;
static FILE *replay_manifest;
static lxp_receipt replay_receipts[4];
static uint8_t replay_receipt_digests[4][32];
static int replay_error;

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

static int replay_write(unsigned index, const char *suffix, const uint8_t *bytes, size_t length)
{
    char name[128];
    CHECK(snprintf(name,sizeof(name),"replay-%u.%s",index,suffix)>0);
    CHECK(write_evidence_file(replay_directory,name,bytes,length)==0);
    return 0;
}

static void replay_fixture_destroy(lxp_kernel_prepared_batch *batch)
{
    const lxp_receipt *receipts = lxp_kernel_prepared_batch_receipts(batch);
    if (batch != transport_prepared) {
        lxp_kernel_prepared_batch_destroy(batch);
        return;
    }
    for (size_t i=0; i<lxp_kernel_prepared_batch_count(batch); ++i) {
        lxp_byte_span bytes=lxp_kernel_prepared_batch_replay_witness(batch,i);
        lxp_byte_span encoded=lxp_kernel_prepared_batch_replay_metadata_proof(batch,i);
        lxp_state_witness *proof;
        uint8_t digest[32],key[LXP_PROGRAMS_REPLAY_KEY_BYTES];
        if (bytes.length==0U) continue;
        proof=calloc(1,sizeof(*proof));
        lxp_programs_replay_record_key(receipts[i].activity_id,key);
        if (proof==NULL || encoded.length==0U || replay_records>=4U ||
            lxp_state_proof_decode(encoded.bytes,encoded.length,proof)!=LXP_OK ||
            lxp_state_proof_verify(proof,receipts[i].resulting_state_root)!=LXP_OK ||
            proof->module_id!=LXP_MODULE_PROGRAMS || proof->value_length!=394U ||
            proof->key_length!=sizeof(key) || memcmp(proof->key,key,sizeof(key))!=0 ||
            lxp_hash_sha256(bytes.bytes,bytes.length,digest)!=LXP_OK ||
            memcmp(digest,proof->value+proof->value_length-32U,32U)!=0) replay_error=1;
        if (!replay_error) {
            size_t mark=lxp_arena_mark(transport_arena);
            replay_receipts[replay_records]=receipts[i];
            if (lxp_receipt_digest(&receipts[i],transport_arena,replay_receipt_digests[replay_records])!=LXP_OK ||
                lxp_arena_reset(transport_arena,mark)!=LXP_OK) replay_error=1;
            ++replay_records;
            if (replay_write(replay_records,"proof",encoded.bytes,encoded.length)!=0 ||
                replay_write(replay_records,"witness",bytes.bytes,bytes.length)!=0 ||
                replay_write(replay_records,"root",receipts[i].resulting_state_root,32U)!=0 ||
                fprintf(replay_manifest,"%s{\"index\":%u,\"batch\":%llu,\"receipt_index\":%zu,\"metadata_proof\":\"replay-%u.proof\",\"witness\":\"replay-%u.witness\",\"root\":\"replay-%u.root\",\"receipt\":\"batch-%llu-%zu.receipt\"}",
                    replay_records==1U?"":",",replay_records,(unsigned long long)replay_batch,i,
                    replay_records,replay_records,replay_records,(unsigned long long)replay_batch,i)<=0)
                replay_error=1;
        }
        free(proof);
    }
    transport_prepared=NULL;
    lxp_kernel_prepared_batch_destroy(batch);
}
static size_t signed_profile(uint8_t *out,const uint8_t *call,size_t length)
{
    static const uint8_t domain[]="LXP/program-replay-profile/v1";
    size_t cursor=34U;
    memset(out,0,34U); memcpy(out+cursor,domain,sizeof(domain)); cursor+=sizeof(domain);
    out[cursor++]=0;out[cursor++]=1;
    write_u32(out+cursor,128U);cursor+=4U;
    write_u32(out+cursor,1048576U);cursor+=4U;
    write_u32(out+cursor,(uint32_t)length);cursor+=4U;
    memcpy(out+cursor,call,length);return cursor+length;
}
static int reopen_proofs(void)
{
    char path[4096];
    uint8_t root[32],digest[32];
    uint8_t *encoded=malloc(LXP_STATE_WITNESS_MAX_BYTES),*bytes=malloc(LXP_KERNEL_MAX_BLOB_BYTES);
    lxp_state_witness *proof=calloc(1,sizeof(*proof));
    CHECK(encoded!=NULL&&bytes!=NULL&&proof!=NULL);
    for(unsigned i=1;i<=replay_records;++i) {
        FILE *file;size_t length,count;
        CHECK(snprintf(path,sizeof(path),"%s/replay-%u.proof",replay_directory,i)>0);
        file=fopen(path,"rb");CHECK(file!=NULL);length=fread(encoded,1,LXP_STATE_WITNESS_MAX_BYTES,file);CHECK(!ferror(file));CHECK(fclose(file)==0);
        CHECK(lxp_state_proof_decode(encoded,length,proof)==LXP_OK);
        CHECK(snprintf(path,sizeof(path),"%s/replay-%u.root",replay_directory,i)>0);
        file=fopen(path,"rb");CHECK(file!=NULL&&fread(root,1,32,file)==32);CHECK(fclose(file)==0);
        CHECK(lxp_state_proof_verify(proof,root)==LXP_OK);
        CHECK(snprintf(path,sizeof(path),"%s/replay-%u.witness",replay_directory,i)>0);
        file=fopen(path,"rb");CHECK(file!=NULL);count=fread(bytes,1,LXP_KERNEL_MAX_BLOB_BYTES,file);CHECK(!ferror(file));CHECK(fclose(file)==0);
        CHECK(count>0U&&count<=LXP_PROGRAMS_REPLAY_MAX_BYTES);
        CHECK(lxp_hash_sha256(bytes,count,digest)==LXP_OK&&memcmp(digest,proof->value+362U,32U)==0);
        root[0]^=1U;CHECK(lxp_state_proof_verify(proof,root)!=LXP_OK);root[0]^=1U;
        proof->key[0]^=1U;CHECK(lxp_state_proof_verify(proof,root)!=LXP_OK);proof->key[0]^=1U;
        bytes[0]^=1U;CHECK(lxp_hash_sha256(bytes,count,digest)==LXP_OK&&memcmp(digest,proof->value+362U,32U)!=0);
        CHECK(lxp_state_proof_decode(encoded,length-1U,proof)!=LXP_OK);
    }
    free(encoded);free(bytes);free(proof);return 0;
}
static int replay_reopen_authority(maintenance_fixture *f)
{
    lxp_daemon_receipt_authority_store reopened;
    CHECK(lxp_log_close(&f->authority_log)==LXP_OK);
    CHECK(lxp_log_open_or_create(&f->authority_log,f->authority_path,LXP_MAX_BATCH_BODY_BYTES)==LXP_OK);
    CHECK(lxp_daemon_receipt_authority_open(&reopened,&f->authority_log,&f->authorization)==LXP_OK);
    CHECK(reopened.last_global_sequence==f->receipt_authority.last_global_sequence);
    CHECK(transport_reopen(f)==0);
    for(unsigned i=0;i<replay_records;++i) {
        lxp_daemon_receipt_evidence evidence,maintenance;
        lxp_receipt receipt;
        uint8_t digest[32];bool present=false;
        size_t mark=lxp_arena_mark(&f->arena);
        memcpy(digest,replay_receipt_digests[i],32U);
        CHECK(lxp_daemon_receipt_authority_lookup(&reopened,digest,&f->arena,&evidence)==LXP_OK);
        CHECK(lxp_receipt_decode(evidence.canonical_receipt.bytes,evidence.canonical_receipt.length,true,&receipt)==LXP_OK);
        CHECK(lxp_receipt_verify(&receipt,f->authorization.public_key,&f->arena)==LXP_OK);
        CHECK(receipt.global_sequence==replay_receipts[i].global_sequence &&
            memcmp(receipt.activity_id,replay_receipts[i].activity_id,32U)==0 &&
            memcmp(receipt.resulting_state_root,replay_receipts[i].resulting_state_root,32U)==0);
        CHECK(lxp_daemon_receipt_authority_batch_maintenance(&reopened,&evidence,&f->arena,&maintenance,&present)==LXP_OK&&present);
        CHECK(maintenance.receipt_proof.leaf_count==evidence.receipt_proof.leaf_count);
        CHECK(lxp_arena_reset(&f->arena,mark)==LXP_OK);
    }
    return 0;
}
int main(int argc,char **argv)
{
    static const uint8_t entry[]={0x41U,0U,0x0bU},trap[]={0U,0x0bU};
    maintenance_fixture *f=calloc(1,sizeof(*f));
    uint8_t wasm[512],payload[2048],call[STAGED_CALL_FIXTURE_BYTES],profile[2048],program[32]={0x31U},hash[32];
    char path[4096];size_t length,wasm_length;
    CHECK(argc==2&&argv[1][0]=='/'&&f!=NULL);replay_directory=argv[1];
    CHECK(maintenance_fixture_open(f)==0);
    CHECK(lxp_state_store_bind_accounts(&f->state, &f->accounts)==LXP_OK);
    CHECK(lxp_kernel_register_module(&f->kernel, lxp_governance_module_iface())==LXP_OK);
    CHECK(lxp_state_root(&f->kernel, f->kernel.current_state_root)==LXP_OK);
    f->kernel.execution_prestate_capture_enabled=true;
    f->evidence.arbiter_admission_prestate_enabled=true;
    f->evidence_directory=replay_directory;f->arbiter_export=true;f->arbiter_first_capture=true;
    transport_store=&f->evidence;transport_arena=&f->arena;
    CHECK(snprintf(path,sizeof(path),"%s/inputs.json",replay_directory)>0);
    f->arbiter_manifest=fopen(path,"wbx");CHECK(f->arbiter_manifest!=NULL);
    CHECK(fprintf(f->arbiter_manifest,"{\"network_id\":7,\"sequencer_public_key\":\"")>0);
    for(size_t i=0;i<32U;++i)CHECK(fprintf(f->arbiter_manifest,"%02x",f->authorization.public_key[i])>0);
    CHECK(fprintf(f->arbiter_manifest,"\",\"sequencer_id\":\"")>0);
    for(size_t i=0;i<32U;++i)CHECK(fprintf(f->arbiter_manifest,"%02x",f->authorization.sequencer_id[i])>0);
    CHECK(fprintf(f->arbiter_manifest,"\",\"first_batch_number\":1,\"last_batch_number\":100,\"captures\":[")>0);
    CHECK(snprintf(path,sizeof(path),"%s/replay-inputs.json",replay_directory)>0);
    replay_manifest=fopen(path,"wbx");CHECK(replay_manifest!=NULL);
    CHECK(fprintf(replay_manifest,"{\"admission_inputs\":\"inputs.json\",\"captures\":[")>0);
    memset(payload, 0, 68U);
    payload[0]=0x71U;payload[1]=1U;payload[3]=2U;
    memcpy(payload+4U, f->identity->did_id, 32U);
    memcpy(payload+36U, f->actor_public_key, 32U);
    replay_batch=1U;
    CHECK(replay_publish(f, UINT32_C(0x00070001), payload, 68U, 1U,
        replay_batch, LXP_OK, true)==0);
    CHECK(lxp_governance_identity_refresh(&f->kernel, f->identity)==LXP_OK);
    CHECK(f->identity->revocation_sequence==1U && replay_records==0U && !replay_error);
    memcpy(f->authority.principal, f->identity->did_id, 32U);
    wasm_length=candidate_module(wasm,entry,sizeof(entry));
    length=deploy_payload(payload,program,f->authority.principal,wasm,wasm_length,hash,LX_PROGRAMS_GUEST_ABI_V2_VERSION,INTERFACE_CAPABILITIES_NONE);
    replay_batch=2U;CHECK(replay_publish(f,LX_PROGRAMS_DEPLOY,payload,length,1U,replay_batch,LXP_OK,true)==0);
    length=staged_call_payload(call,program);write_u16(call+32U,LX_PROGRAMS_GUEST_ABI_V2_VERSION);
    replay_batch=3U;CHECK(replay_publish(f,LX_PROGRAMS_CALL,call,length,1U,replay_batch,LXP_OK,true)==0);
    CHECK(replay_records==0U&&!replay_error);
    length=signed_profile(profile,call,length);
    replay_batch=4U;CHECK(replay_publish(f,LX_PROGRAMS_CALL,profile,length,1U,replay_batch,LXP_OK,true)==0);
    CHECK(replay_records==1U&&!replay_error);
    replay_batch=5U;CHECK(replay_publish(f,LX_PROGRAMS_CALL,profile,length,2U,replay_batch,LXP_OK,false)==0);
    CHECK(replay_records==3U&&!replay_error);
    program[0]=0x32U;wasm_length=candidate_module(wasm,trap,sizeof(trap));
    length=deploy_payload(payload,program,f->authority.principal,wasm,wasm_length,hash,LX_PROGRAMS_GUEST_ABI_V2_VERSION,INTERFACE_CAPABILITIES_NONE);
    replay_batch=6U;CHECK(replay_publish(f,LX_PROGRAMS_DEPLOY,payload,length,1U,replay_batch,LXP_OK,true)==0);
    length=staged_call_payload(call,program);write_u16(call+32U,LX_PROGRAMS_GUEST_ABI_V2_VERSION);length=signed_profile(profile,call,length);
    replay_batch=7U;CHECK(replay_publish(f,LX_PROGRAMS_CALL,profile,length,1U,replay_batch,LXP_ERR_PROGRAM_REFUSED,true)==0);
    CHECK(replay_records==4U&&!replay_error);
    write_u32(profile+34U+sizeof("LXP/program-replay-profile/v1")+2U,0U);
    replay_batch=8U;CHECK(replay_publish(f,LX_PROGRAMS_CALL,profile,length,1U,replay_batch,LXP_ERR_NON_CANONICAL,true)==0);
    CHECK(replay_records==4U&&!replay_error);
    CHECK(fprintf(f->arbiter_manifest,"]}\n")>0&&fclose(f->arbiter_manifest)==0);
    CHECK(fprintf(replay_manifest,"]}\n")>0&&fclose(replay_manifest)==0);
    CHECK(reopen_proofs()==0&&replay_reopen_authority(f)==0);
    CHECK(arbiter_close_fixture(f)==0);
    puts("REPLAY_AUTHORITY real-canonical-owner-signed-serial-scheduled-trap-proof-reopen-refusal");return 0;
}
