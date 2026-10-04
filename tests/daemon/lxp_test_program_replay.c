#include "layerx/lxp_kernel.h"
#include "layerx/lxp_state_proof.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static void replay_fixture_destroy(lxp_kernel_prepared_batch *batch);
#define lxp_kernel_prepared_batch_destroy replay_fixture_destroy
#include "program-replay-fixture-base.inc"
#undef lxp_kernel_prepared_batch_destroy
static const char *replay_directory;
static unsigned replay_records;
static int replay_error;

static void replay_fixture_destroy(lxp_kernel_prepared_batch *batch)
{
    const lxp_receipt *receipts = lxp_kernel_prepared_batch_receipts(batch);
    for (size_t i=0; i<lxp_kernel_prepared_batch_count(batch); ++i) {
        lxp_byte_span bytes=lxp_kernel_prepared_batch_replay_witness(batch,i);
        lxp_byte_span encoded=lxp_kernel_prepared_batch_replay_metadata_proof(batch,i);
        lxp_state_witness *proof;
        uint8_t digest[32];
        char path[4096];
        FILE *file;
        if (bytes.length==0U) continue;
        proof=calloc(1,sizeof(*proof));
        if (proof==NULL || encoded.length==0U ||
            lxp_state_proof_decode(encoded.bytes,encoded.length,proof)!=LXP_OK ||
            lxp_state_proof_verify(proof,receipts[i].resulting_state_root)!=LXP_OK ||
            proof->module_id!=LXP_MODULE_PROGRAMS || proof->value_length!=394U ||
            lxp_hash_sha256(bytes.bytes,bytes.length,digest)!=LXP_OK ||
            memcmp(digest,proof->value+proof->value_length-32U,32U)!=0) replay_error=1;
        if (!replay_error) {
            ++replay_records;
            if (snprintf(path,sizeof(path),"%s/replay-%u.proof",replay_directory,replay_records)<=0 ||
                (file=fopen(path,"wbx"))==NULL) replay_error=1;
            else { if(fwrite(encoded.bytes,1,encoded.length,file)!=encoded.length) replay_error=1; fclose(file); }
            if (snprintf(path,sizeof(path),"%s/replay-%u.witness",replay_directory,replay_records)<=0 ||
                (file=fopen(path,"wbx"))==NULL) replay_error=1;
            else { if(fwrite(bytes.bytes,1,bytes.length,file)!=bytes.length) replay_error=1; fclose(file); }
            if (snprintf(path,sizeof(path),"%s/replay-%u.root",replay_directory,replay_records)<=0 ||
                (file=fopen(path,"wbx"))==NULL) replay_error=1;
            else { if(fwrite(receipts[i].resulting_state_root,1,32,file)!=32) replay_error=1; fclose(file); }
        }
        free(proof);
    }
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
        CHECK(lxp_hash_sha256(bytes,count,digest)==LXP_OK&&memcmp(digest,proof->value+362U,32U)==0);
        root[0]^=1U;CHECK(lxp_state_proof_verify(proof,root)!=LXP_OK);
        bytes[0]^=1U;CHECK(lxp_hash_sha256(bytes,count,digest)==LXP_OK&&memcmp(digest,proof->value+362U,32U)!=0);
    }
    free(encoded);free(bytes);free(proof);return 0;
}
int main(int argc,char **argv)
{
    static const uint8_t entry[]={0x41U,0U,0x0bU},trap[]={0U,0x0bU};
    maintenance_fixture *f=calloc(1,sizeof(*f));
    uint8_t wasm[512],payload[2048],call[STAGED_CALL_FIXTURE_BYTES],profile[2048],program[32]={0x31U},hash[32];
    size_t length,wasm_length;
    CHECK(argc==2&&argv[1][0]=='/'&&f!=NULL);replay_directory=argv[1];
    CHECK(maintenance_fixture_open(f)==0);
    f->evidence.arbiter_admission_prestate_enabled=true;
    transport_store=&f->evidence;transport_arena=&f->arena;
    wasm_length=candidate_module(wasm,entry,sizeof(entry));
    length=deploy_payload(payload,program,f->authority.principal,wasm,wasm_length,hash,LX_PROGRAMS_ABI_VERSION,INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f,LX_PROGRAMS_DEPLOY,payload,length,1U,1U)==0);
    length=staged_call_payload(call,program);length=signed_profile(profile,call,length);
    CHECK(maintenance_publish(f,LX_PROGRAMS_CALL,profile,length,1U,2U)==0);
    CHECK(replay_records==1U&&!replay_error);
    CHECK(maintenance_publish(f,LX_PROGRAMS_CALL,profile,length,2U,3U)==0);
    CHECK(replay_records==3U&&!replay_error);
    program[0]=0x32U;wasm_length=candidate_module(wasm,trap,sizeof(trap));
    length=deploy_payload(payload,program,f->authority.principal,wasm,wasm_length,hash,LX_PROGRAMS_ABI_VERSION,INTERFACE_CAPABILITIES_NONE);
    CHECK(maintenance_publish(f,LX_PROGRAMS_DEPLOY,payload,length,1U,4U)==0);
    length=staged_call_payload(call,program);length=signed_profile(profile,call,length);
    CHECK(maintenance_publish(f,LX_PROGRAMS_CALL,profile,length,1U,5U)==0);
    CHECK(replay_records==4U&&!replay_error);
    CHECK(reopen_proofs()==0);
    CHECK(arbiter_close_fixture(f)==0);free(f);
    puts("PROGRAM_REPLAY real-signed-serial-scheduled-trap-proof-reopen-refusal");return 0;
}
