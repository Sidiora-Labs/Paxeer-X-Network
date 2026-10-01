#include "layerx/lx_oracle.h"
#include "layerx/lx_perps.h"
#include "layerx/lxp_crypto.h"
#include "layerx/lxp_kernel.h"
#include <stdio.h>
#include <string.h>
#define CHECK(c) do { ++checks; if (!(c)) { fprintf(stderr,"oracle transport:%d: %s\n",__LINE__,#c);return 1; } } while(0)
int main(void)
{
    unsigned checks=0U;
    const uint8_t seed[32]={44U},other[32]={45U};
    static const uint8_t did[]="did:key:oracle";
    lx_oracle_observation observation={0};observation.market_id[0]=1U;observation.observation_sequence=1U;
    observation.price.lo=100U;observation.observed_at=1000U;observation.source_identifier=42U;
    uint8_t bytes[138],legacy[72];size_t length;
    CHECK(lx_oracle_observation_sign(&observation,seed)==LXP_OK);
    CHECK(lx_oracle_observation_encode(&observation,legacy,sizeof(legacy),&length)==LXP_OK && length==72U);
    CHECK(lx_oracle_transport_encode(&observation,bytes)==LXP_OK);
    CHECK(bytes[0]==1U && memcmp(bytes+1U,legacy,72U)==0 && memcmp(bytes+73U,observation.signature,64U)==0);
    lx_perps_oracle_command command;
    CHECK(lx_perps_oracle_command_decode(legacy,72U,&command)==LXP_OK && command.transport_version==0U);
    CHECK(lx_perps_oracle_command_decode(bytes,137U,&command)==LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_oracle_transport_decode(legacy,72U,&command)==LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_oracle_transport_decode(bytes,137U,&command)==LXP_OK && command.transport_version==1U && memcmp(command.signature,observation.signature,64U)==0);
    const size_t lengths[]={0U,71U,72U,73U,136U,138U};
    for(size_t i=0U;i<sizeof(lengths)/sizeof(lengths[0]);++i)CHECK(lx_perps_oracle_transport_decode(bytes,lengths[i],&command)==LXP_ERR_NON_CANONICAL);
    for(unsigned version=0U;version<256U;++version){if(version==1U)continue;bytes[0]=(uint8_t)version;CHECK(lx_perps_oracle_transport_decode(bytes,137U,&command)==LXP_ERR_NON_CANONICAL);}bytes[0]=1U;
    const size_t zero_at[]={1U,33U,41U,57U,65U},zero_length[]={32U,8U,16U,8U,8U};
    for(size_t i=0U;i<5U;++i){uint8_t bad[137];memcpy(bad,bytes,137U);memset(bad+zero_at[i],0,zero_length[i]);CHECK(lx_perps_oracle_transport_decode(bad,sizeof(bad),&command)!=LXP_OK);}
    lx_oracle_market market={0};memcpy(market.market_id,observation.market_id,32U);market.permitted_key_count=1U;memcpy(market.permitted_keys[0],observation.oracle_public_key,32U);
    CHECK(lx_oracle_key_set_check(&market,&observation,legacy,72U)==LXP_OK);
    observation.signature[0]^=1U;CHECK(lx_oracle_key_set_check(&market,&observation,legacy,72U)==LXP_ERR_UNAUTHORIZED_ORACLE);observation.signature[0]^=1U;
    lx_oracle_adapter_config config={0};memcpy(config.oracle_private_key,seed,32U);config.network_id=77U;config.protocol_version=3U;config.transport_version=1U;config.actor_did=did;config.actor_did_length=sizeof(did)-1U;config.fee_limit.lo=100U;config.not_before=1000U;config.not_after=2000U;
    static uint8_t memory[2U*LXP_MAX_ACTIVITY_BYTES];lxp_arena arena;lxp_byte_span encoded;lxp_activity activity;
    CHECK(lxp_arena_init(&arena,memory,sizeof(memory))==LXP_OK);
    CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)==LXP_OK && lxp_activity_decode(encoded.bytes,encoded.length,&activity)==LXP_OK && lxp_activity_verify_signature(&activity)==LXP_OK);
    CHECK(memcmp(activity.signature.bytes,observation.signature,64U)!=0);
    lxp_byte_span signature=activity.signature;activity.signature=(lxp_byte_span){observation.signature,64U};CHECK(lxp_activity_verify_signature(&activity)==LXP_ERR_BAD_SIGNATURE);activity.signature=signature;
    memcpy(observation.signature,signature.bytes,64U);CHECK(lx_oracle_key_set_check(&market,&observation,legacy,72U)==LXP_ERR_UNAUTHORIZED_ORACLE);CHECK(lx_oracle_observation_sign(&observation,seed)==LXP_OK);
    config.protocol_version=2U;CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)!=LXP_OK);config.protocol_version=3U;
    config.transport_version=0U;CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)!=LXP_OK);config.transport_version=1U;
    config.not_after=999U;CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)!=LXP_OK);
    config.not_after=301001U;CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)!=LXP_OK);
    config.not_after=2000U;config.fee_limit.lo=0U;CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)!=LXP_OK);config.fee_limit.lo=100U;
    memcpy(config.oracle_private_key,other,32U);CHECK(lx_oracle_activity_encode_signed(&observation,&config,&arena,&encoded)==LXP_ERR_BAD_SIGNATURE);
    CHECK(lx_perps_module_iface()->abi_version==1U && lx_perps_oracle_transport_module_iface()->abi_version==2U);
    printf("ORACLE_TRANSPORT checks=%u failures=0\n",checks);return 0;
}
