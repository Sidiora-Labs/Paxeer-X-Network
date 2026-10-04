#define _POSIX_C_SOURCE 200809L
#include "layerx/lxp_daemon.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_receipt.h"
#include "layerx/programs.h"
#include <dirent.h>
#include <errno.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

#define CHECK(x) do { if (!(x)) { fprintf(stderr,"native authority:%d: %s\n",__LINE__,#x); return 1; } } while(0)
extern lxp_result layerx_programs_call_arbiter_authority_check(uint64_t token,uint64_t d0,uint64_t d1,uint64_t d2,uint64_t d3,uint64_t batch);
static pthread_mutex_t native_observer_mutex=PTHREAD_MUTEX_INITIALIZER;
static uint64_t native_u64(const uint8_t *p) { uint64_t n=0U; for(size_t i=0U;i<8U;++i)n=(n<<8U)|p[i]; return n; }
static int native_path(char *p,size_t cap,const char *dir,const char *phase,uint64_t batch,uint64_t sequence,const char *suffix) {
 int n=snprintf(p,cap,"%s/%s-%llu-%llu.%s",dir,phase,(unsigned long long)batch,(unsigned long long)sequence,suffix);
 return n>0 && (size_t)n<cap ? 0:1;
}
static int native_target(uint8_t digest[32],uint64_t *batch) {
 const char *name=getenv("LAYERX_NATIVE_AUTHORITY_TARGET_RECEIPT"); uint8_t bytes[40];
 if(name==NULL)return 1;
 FILE *f=fopen(name,"rb"); if(f==NULL)return 1;
 int ok=fread(bytes,1U,sizeof(bytes),f)==sizeof(bytes) && fgetc(f)==EOF && !ferror(f);
 if(fclose(f)!=0)ok=0;
 if(!ok)return 1;
 *batch=native_u64(bytes); memcpy(digest,bytes+8U,32U); return *batch==0U||lxp_ct_is_zero(digest,32U)?1:0;
}
static lxp_result native_begin(uint64_t token,const uint8_t digest[32],uint64_t batch) {
 return layerx_programs_call_arbiter_authority_begin(token,native_u64(digest),native_u64(digest+8U),native_u64(digest+16U),native_u64(digest+24U),batch);
}
static int native_record(uint64_t token,uint64_t batch,uint64_t sequence) {
 const char *dir=getenv("LAYERX_NATIVE_AUTHORITY_OUTPUT"),*phase=getenv("LAYERX_NATIVE_AUTHORITY_PHASE");
 uint8_t digest[32],wrong[32],view[247]; uint64_t target_batch; char path[4096];
 CHECK(dir!=NULL && phase!=NULL && (strcmp(phase,"before")==0||strcmp(phase,"after")==0));
 CHECK(native_target(digest,&target_batch)==0);
 CHECK(native_begin(token,digest,target_batch)==LXP_OK);
 CHECK(layerx_programs_call_receipt_view_begin(token,native_u64(digest),native_u64(digest+8U),native_u64(digest+16U),native_u64(digest+24U))==LXP_OK);
 for(uint32_t i=0U;i<32U;++i)CHECK(layerx_programs_call_receipt_view_byte(token,0U,i)==(lxp_result)digest[i]);
 CHECK(layerx_programs_call_receipt_view_byte(token,0U,32U)==LXP_ERR_TRUNCATED);
 CHECK(layerx_programs_call_arbiter_authority_byte(token,247U)==LXP_ERR_TRUNCATED);
 CHECK(native_begin(token,digest,target_batch+1U)==LXP_ERR_ROOT_MISMATCH);
 CHECK(layerx_programs_call_arbiter_authority_byte(token,0U)==LXP_ERR_UNKNOWN_FIELD);
 memcpy(wrong,digest,sizeof(wrong)); wrong[0]^=1U;
 CHECK(native_begin(token,wrong,target_batch)!=LXP_OK);
 CHECK(native_begin(token,digest,target_batch)==LXP_OK);
 CHECK(layerx_programs_call_arbiter_authority_check(token,native_u64(digest),native_u64(digest+8U),native_u64(digest+16U),native_u64(digest+24U),target_batch)==LXP_OK);
 for(uint32_t i=0U;i<247U;++i){lxp_result value=layerx_programs_call_arbiter_authority_byte(token,i);CHECK(value>=0&&value<=255);view[i]=(uint8_t)value;}
 CHECK(view[0]==0U&&view[1]==1U&&memcmp(view+55U,digest,32U)==0 && native_u64(view+15U)==target_batch);
 CHECK(native_path(path,sizeof(path),dir,phase,batch,sequence,"view")==0);
 FILE *f=fopen(path,"wbx");
 if(f==NULL && errno==EEXIST) {
  uint8_t old[247];f=fopen(path,"rb"); CHECK(f!=NULL&&fread(old,1U,sizeof(old),f)==sizeof(old)&&fgetc(f)==EOF&&fclose(f)==0&&memcmp(old,view,sizeof(view))==0);
 } else CHECK(f!=NULL&&fwrite(view,1U,sizeof(view),f)==sizeof(view)&&fclose(f)==0);
 return 0;
}
static void native_observe(uint64_t token,uint64_t batch,uint64_t sequence) {
 if(getenv("LAYERX_NATIVE_AUTHORITY_OUTPUT")==NULL)return;
 if(pthread_mutex_lock(&native_observer_mutex)!=0)return;
 if(native_record(token,batch,sequence)!=0) {
  const char *dir=getenv("LAYERX_NATIVE_AUTHORITY_OUTPUT"),*phase=getenv("LAYERX_NATIVE_AUTHORITY_PHASE");char path[4096];
  if(dir!=NULL && phase!=NULL && native_path(path,sizeof(path),dir,phase,batch,sequence,"error")==0){FILE *f=fopen(path,"a");if(f!=NULL){fputs("native callback checks failed\n",f);(void)fclose(f);}}
 }
 (void)pthread_mutex_unlock(&native_observer_mutex);
}
static void native_observe_result(uint64_t batch,uint64_t sequence,lxp_result result) {
 const char *dir=getenv("LAYERX_NATIVE_AUTHORITY_OUTPUT"),*phase=getenv("LAYERX_NATIVE_AUTHORITY_PHASE");char path[4096];
 if(dir!=NULL && phase!=NULL && native_path(path,sizeof(path),dir,phase,batch,sequence,"result")==0){FILE *f=fopen(path,"a");if(f!=NULL){fprintf(f,"%d\n",result);(void)fclose(f);}}
}
lxp_result __real_layerx_programs_call_begin(
    uint64_t token, uint64_t occupancy_token,
    uint64_t p0, uint64_t p1, uint64_t p2, uint64_t p3,
    uint64_t r0, uint64_t r1, uint64_t r2, uint64_t r3,
    uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3,
    uint64_t h0, uint64_t h1, uint64_t h2, uint64_t h3,
    uint64_t b0, uint64_t b1, uint64_t b2, uint64_t b3,
    uint64_t signed_fee_hi, uint64_t signed_fee_lo,
    uint64_t available_fee_hi, uint64_t available_fee_lo,
    uint32_t fee_schedule_version, uint32_t metering_schedule_version,
    uint32_t parameter_version,
    uint64_t meter_base, uint64_t meter_entity, uint64_t meter_load,
    uint64_t meter_store, uint64_t meter_call,
    uint64_t meter_branch_kept_per_fuel,
    uint64_t meter_func_locals_per_fuel,
    uint64_t meter_memory_bytes_per_fuel,
    uint64_t meter_table_elements_per_fuel,
    uint64_t fee_cpu, uint64_t fee_memory_byte,
    uint64_t fee_storage_read_byte, uint64_t fee_storage_write_byte,
    uint64_t fee_output_value, uint64_t fee_output_byte,
    uint64_t fee_occupancy_byte_batch, uint64_t batch_number,
    uint64_t activity_sequence,
    uint16_t protocol_version,
    uint16_t abi_version, uint16_t entrypoint_length,
    uint32_t wasm_length, uint32_t calldata_length, uint16_t capabilities_length,
    uint32_t access_declaration_length, uint32_t response_capacity,
    uint64_t cpu_fuel, uint64_t memory_bytes,
    uint64_t storage_read_bytes, uint64_t storage_write_bytes,
    uint64_t output_values, uint64_t output_bytes,
    uint64_t table_elements);
lxp_result __wrap_layerx_programs_call_begin(
    uint64_t token, uint64_t occupancy_token,
    uint64_t p0, uint64_t p1, uint64_t p2, uint64_t p3,
    uint64_t r0, uint64_t r1, uint64_t r2, uint64_t r3,
    uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3,
    uint64_t h0, uint64_t h1, uint64_t h2, uint64_t h3,
    uint64_t b0, uint64_t b1, uint64_t b2, uint64_t b3,
    uint64_t signed_fee_hi, uint64_t signed_fee_lo,
    uint64_t available_fee_hi, uint64_t available_fee_lo,
    uint32_t fee_schedule_version, uint32_t metering_schedule_version,
    uint32_t parameter_version,
    uint64_t meter_base, uint64_t meter_entity, uint64_t meter_load,
    uint64_t meter_store, uint64_t meter_call,
    uint64_t meter_branch_kept_per_fuel,
    uint64_t meter_func_locals_per_fuel,
    uint64_t meter_memory_bytes_per_fuel,
    uint64_t meter_table_elements_per_fuel,
    uint64_t fee_cpu, uint64_t fee_memory_byte,
    uint64_t fee_storage_read_byte, uint64_t fee_storage_write_byte,
    uint64_t fee_output_value, uint64_t fee_output_byte,
    uint64_t fee_occupancy_byte_batch, uint64_t batch_number,
    uint64_t activity_sequence,
    uint16_t protocol_version,
    uint16_t abi_version, uint16_t entrypoint_length,
    uint32_t wasm_length, uint32_t calldata_length, uint16_t capabilities_length,
    uint32_t access_declaration_length, uint32_t response_capacity,
    uint64_t cpu_fuel, uint64_t memory_bytes,
    uint64_t storage_read_bytes, uint64_t storage_write_bytes,
    uint64_t output_values, uint64_t output_bytes,
    uint64_t table_elements)
{
    native_observe(token, batch_number, activity_sequence);
    lxp_result actual = __real_layerx_programs_call_begin(token, occupancy_token, p0, p1, p2, p3, r0, r1, r2, r3, a0, a1, a2, a3, h0, h1, h2, h3, b0, b1, b2, b3, signed_fee_hi, signed_fee_lo, available_fee_hi, available_fee_lo, fee_schedule_version, metering_schedule_version, parameter_version, meter_base, meter_entity, meter_load, meter_store, meter_call, meter_branch_kept_per_fuel, meter_func_locals_per_fuel, meter_memory_bytes_per_fuel, meter_table_elements_per_fuel, fee_cpu, fee_memory_byte, fee_storage_read_byte, fee_storage_write_byte, fee_output_value, fee_output_byte, fee_occupancy_byte_batch, batch_number, activity_sequence, protocol_version, abi_version, entrypoint_length, wasm_length, calldata_length, capabilities_length, access_declaration_length, response_capacity, cpu_fuel, memory_bytes, storage_read_bytes, storage_write_bytes, output_values, output_bytes, table_elements);
    native_observe_result(batch_number, activity_sequence, actual);
    return actual;
}

static int native_owner_cases(const lxp_daemon_protocol_owner *owner,const uint8_t digest[32],lxp_verified_receipt_authority_facts *facts) {
 uint8_t *memory=malloc(4U*LXP_MAX_ACTIVITY_BYTES);lxp_arena arena;lxp_daemon_receipt_evidence evidence;
 lxp_verified_receipt_index *fresh=calloc(1U,sizeof(*fresh));lxp_receipt receipt;lxp_sequencer_authorization auth;lxp_batch_header header;
 CHECK(memory!=NULL&&fresh!=NULL&&owner!=NULL&&owner->attached&&owner->network_id==7U&&owner->receipt_authority!=NULL&&owner->verified_receipts!=NULL);
 CHECK(lxp_arena_init(&arena,memory,4U*LXP_MAX_ACTIVITY_BYTES)==LXP_OK);
 CHECK(lxp_verified_receipt_index_lookup_authority(owner->verified_receipts,digest,facts)==LXP_OK);
 CHECK(facts->version==1U&&facts->network_id==7U&&facts->epoch==1U&&facts->authorization_last_batch_number<UINT64_MAX);
 CHECK(lxp_daemon_receipt_authority_lookup(owner->receipt_authority,digest,&arena,&evidence)==LXP_OK);
 CHECK(lxp_batch_header_decode(evidence.canonical_header.bytes,evidence.canonical_header.length,&header)==LXP_OK);
 CHECK(lxp_daemon_receipt_authority_header_authorization(owner->receipt_authority,&header,&auth)==LXP_OK);
 CHECK(lxp_verified_receipt_index_init(fresh)==LXP_OK);
 CHECK(lxp_verified_receipt_index_add_authority(fresh,evidence.canonical_receipt,evidence.canonical_header,evidence.header_signature,&evidence.receipt_proof,&auth,&arena)==LXP_OK);
 lxp_verified_receipt_authority_facts copied;
 CHECK(lxp_verified_receipt_index_lookup_authority(fresh,digest,&copied)==LXP_OK && memcmp(copied.receipt_digest,facts->receipt_digest,32U)==0 && memcmp(copied.previous_state_root,facts->previous_state_root,32U)==0 && memcmp(copied.resulting_state_root,facts->resulting_state_root,32U)==0);
 CHECK(lxp_verified_receipt_index_init(fresh)==LXP_OK);
 CHECK(lxp_receipt_decode(evidence.canonical_receipt.bytes,evidence.canonical_receipt.length,true,&receipt)==LXP_OK);
 CHECK(lxp_verified_receipt_index_add(fresh,&receipt,auth.public_key,&arena)==LXP_OK);
 lxp_verified_receipt_facts legacy;
 CHECK(lxp_verified_receipt_index_lookup(fresh,digest,&legacy)==LXP_OK && memcmp(legacy.receipt_digest,digest,32U)==0 && memcmp(legacy.resulting_state_root,facts->resulting_state_root,32U)==0);
 CHECK(lxp_verified_receipt_index_lookup_authority(fresh,digest,&copied)!=LXP_OK);
 CHECK(lxp_verified_receipt_index_init(fresh)==LXP_OK);
 CHECK(owner->verified_receipts->fallback!=NULL);
 CHECK(lxp_verified_receipt_index_bind_fallback(fresh,owner->verified_receipts->fallback,owner->verified_receipts->fallback_context)==LXP_OK);
 CHECK(lxp_verified_receipt_index_lookup(fresh,digest,&legacy)==LXP_OK && memcmp(legacy.receipt_digest,digest,32U)==0 && memcmp(legacy.resulting_state_root,facts->resulting_state_root,32U)==0);
 CHECK(lxp_verified_receipt_index_lookup_authority(fresh,digest,&copied)!=LXP_OK);
 uint8_t signature[64]; memcpy(signature,evidence.header_signature,sizeof(signature));signature[0]^=1U;
 CHECK(lxp_verified_receipt_index_add_authority(fresh,evidence.canonical_receipt,evidence.canonical_header,signature,&evidence.receipt_proof,&auth,&arena)!=LXP_OK);
 uint8_t encoded[LXP_BATCH_HEADER_ENCODED_SIZE]; lxp_byte_span changed;
 lxp_batch_header altered=header; altered.network_id=77U;
 CHECK(lxp_batch_header_encode(&altered,&arena,&changed)==LXP_OK&&changed.length==sizeof(encoded));memcpy(encoded,changed.bytes,sizeof(encoded));
 CHECK(lxp_verified_receipt_index_add_authority(fresh,evidence.canonical_receipt,(lxp_byte_span){encoded,sizeof(encoded)},evidence.header_signature,&evidence.receipt_proof,&auth,&arena)!=LXP_OK);
 lxp_merkle_proof wrong=evidence.receipt_proof; CHECK(wrong.depth>0U); wrong.siblings[0][0]^=1U;
 CHECK(lxp_verified_receipt_index_add_authority(fresh,evidence.canonical_receipt,evidence.canonical_header,evidence.header_signature,&wrong,&auth,&arena)!=LXP_OK);
 free(fresh);free(memory);return 0;
}
static int native_manifest(const char *directory,const lxp_verified_receipt_authority_facts *facts) {
 struct entry { char name[256],phase[8];uint64_t batch,sequence; } entries[128];size_t count=0U;
 DIR *dir=opendir(directory);CHECK(dir!=NULL);struct dirent *item;char path[4096];bool before=false,after=false,serial=false,scheduled=false;
 while((item=readdir(dir))!=NULL) {
  size_t length=strlen(item->d_name);
  CHECK(length<sizeof(entries[0].name));
  if(length>6U&&strcmp(item->d_name+length-6U,".error")==0){(void)closedir(dir);CHECK(false);}
  if(length<5U||strcmp(item->d_name+length-5U,".view")!=0)continue;
  CHECK(count<128U);unsigned long long batch,sequence;char phase[8],tail;
  CHECK(sscanf(item->d_name,"%7[^-]-%llu-%llu.view%c",phase,&batch,&sequence,&tail)==3);
  CHECK(strcmp(phase,"before")==0||strcmp(phase,"after")==0);
  int n=snprintf(path,sizeof(path),"%s/%s",directory,item->d_name);CHECK(n>0&&(size_t)n<sizeof(path));
  uint8_t bytes[247];FILE *f=fopen(path,"rb");CHECK(f!=NULL&&fread(bytes,1U,sizeof(bytes),f)==sizeof(bytes)&&fgetc(f)==EOF&&fclose(f)==0);
  CHECK(bytes[0]==0U&&bytes[1]==1U&&bytes[2]==0U&&bytes[3]==0U&&bytes[4]==0U&&bytes[5]==7U);
  CHECK(memcmp(bytes+55U,facts->receipt_digest,32U)==0&&memcmp(bytes+87U,facts->activity_id,32U)==0&&memcmp(bytes+119U,facts->previous_state_root,32U)==0&&memcmp(bytes+151U,facts->resulting_state_root,32U)==0&&memcmp(bytes+183U,facts->sequencer_id,32U)==0&&memcmp(bytes+215U,facts->sequencer_public_key,32U)==0);
  CHECK(native_u64(bytes+15U)==facts->batch_number&&native_u64(bytes+23U)==facts->epoch&&native_u64(bytes+31U)==facts->global_sequence&&native_u64(bytes+39U)==facts->authorization_first_batch_number&&native_u64(bytes+47U)>=facts->batch_number);
  CHECK((((uint16_t)bytes[6U]<<8U)|bytes[7U])==facts->protocol_version && (((uint16_t)bytes[8U]<<8U)|bytes[9U])==facts->module_id && bytes[10U]==facts->operation);
  uint32_t encoded_result=((uint32_t)bytes[11U]<<24U)|((uint32_t)bytes[12U]<<16U)|((uint32_t)bytes[13U]<<8U)|bytes[14U];
  CHECK(encoded_result==(uint32_t)facts->result_code);
  if(strcmp(phase,"after")==0)CHECK(native_u64(bytes+47U)==facts->authorization_last_batch_number);
  CHECK(native_u64(bytes+15U)<(uint64_t)batch&&native_u64(bytes+31U)<(uint64_t)sequence);
  CHECK(native_path(path,sizeof(path),directory,phase,(uint64_t)batch,(uint64_t)sequence,"result")==0);
  f=fopen(path,"r");int actual;CHECK(f!=NULL&&fscanf(f,"%d",&actual)==1&&actual==LXP_OK);
  while(fscanf(f,"%d",&actual)==1)CHECK(actual==LXP_OK);
  CHECK(feof(f)&&!ferror(f)&&fclose(f)==0);
  memcpy(entries[count].name,item->d_name,length+1U);memcpy(entries[count].phase,phase,strlen(phase)+1U);entries[count].batch=(uint64_t)batch;entries[count].sequence=(uint64_t)sequence;++count;
  before|=strcmp(phase,"before")==0;after|=strcmp(phase,"after")==0;
 }
 CHECK(closedir(dir)==0&&before&&after);
 for(size_t i=0U;i<count;++i){size_t same=0U;for(size_t j=0U;j<count;++j)if(entries[i].batch==entries[j].batch&&strcmp(entries[i].phase,entries[j].phase)==0)++same;serial|=same==1U;scheduled|=same>=2U;}
 CHECK(serial&&scheduled);
 int n=snprintf(path,sizeof(path),"%s/native-authority.json",directory);CHECK(n>0&&(size_t)n<sizeof(path));FILE *f=fopen(path,"wx");CHECK(f!=NULL);
 fputs("{\"network_id\":7,\"view_bytes\":247,\"cases\":[\"owner-publication\",\"serial\",\"scheduled\",\"historical-handover\",\"reopen\",\"new-index-missing\",\"wrong-network\",\"wrong-batch\",\"wrong-digest\",\"wrong-root\",\"bad-header-signature\",\"inactive-view\",\"view-bounds\",\"old-lookup-fallback\",\"old-receipt-view\"],\"views\":[",f);
 for(size_t i=0U;i<count;++i){fprintf(f,"%s{\"view\":\"%s\",\"phase\":\"%s\",\"frame_batch\":%llu,\"frame_sequence\":%llu,\"receipt_digest\":\"",i==0U?"":",",entries[i].name,entries[i].phase,(unsigned long long)entries[i].batch,(unsigned long long)entries[i].sequence);for(size_t j=0U;j<32U;++j)fprintf(f,"%02x",facts->receipt_digest[j]);fputs("\"}",f);}
 fputs("]}\n",f);CHECK(!ferror(f)&&fclose(f)==0);return 0;
}
int main(int argc,char **argv) {
 if(argc==3 && strcmp(argv[1],"--serve")==0) return lxp_daemon_main(argc,argv)==LXP_OK?0:1;
 CHECK(argc==3&&argv[1][0]=='/'&&argv[2][0]=='/');uint8_t digest[32];uint64_t batch;CHECK(native_target(digest,&batch)==0);
 lxp_daemon_process *process=NULL;lxp_verified_receipt_authority_facts before,after;
 CHECK(lxp_daemon_process_owner(NULL)==NULL);
 CHECK(lxp_daemon_process_open(argv[1],&process)==LXP_OK);
 CHECK(native_owner_cases(lxp_daemon_process_owner(process),digest,&before)==0&&before.batch_number==batch);
 lxp_daemon_process_close(process);process=NULL;
 CHECK(lxp_daemon_process_open(argv[1],&process)==LXP_OK);
 CHECK(native_owner_cases(lxp_daemon_process_owner(process),digest,&after)==0&&after.batch_number==batch);
 CHECK(memcmp(before.receipt_digest,after.receipt_digest,32U)==0&&memcmp(before.previous_state_root,after.previous_state_root,32U)==0&&memcmp(before.resulting_state_root,after.resulting_state_root,32U)==0&&before.authorization_first_batch_number==after.authorization_first_batch_number&&before.authorization_last_batch_number==after.authorization_last_batch_number);
 lxp_daemon_process_close(process);
 CHECK(native_manifest(argv[2],&after)==0);
 puts("ARBITER_NATIVE_AUTHORITY real-signed-owner-index-call-frame-247-reopen-refusal");return 0;
}
