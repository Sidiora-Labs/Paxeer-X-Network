import { registry as confio_proofs_registry } from "./confio/proofs";
import { registry as cosmos_accesscontrol_accesscontrol_registry } from "./cosmos/accesscontrol/accesscontrol";
import { registry as cosmos_accesscontrol_legacy_registry } from "./cosmos/accesscontrol/legacy";
import { registry as cosmos_accesscontrol_x_genesis_registry } from "./cosmos/accesscontrol_x/genesis";
import { registry as cosmos_accesscontrol_x_gov_registry } from "./cosmos/accesscontrol_x/gov";
import { registry as cosmos_accesscontrol_x_query_registry } from "./cosmos/accesscontrol_x/query";
import { registry as cosmos_accesscontrol_x_tx_registry } from "./cosmos/accesscontrol_x/tx";
import { registry as cosmos_auth_v1beta1_auth_registry } from "./cosmos/auth/v1beta1/auth";
import { registry as cosmos_auth_v1beta1_genesis_registry } from "./cosmos/auth/v1beta1/genesis";
import { registry as cosmos_auth_v1beta1_query_registry } from "./cosmos/auth/v1beta1/query";
import { registry as cosmos_authz_v1beta1_authz_registry } from "./cosmos/authz/v1beta1/authz";
import { registry as cosmos_authz_v1beta1_event_registry } from "./cosmos/authz/v1beta1/event";
import { registry as cosmos_authz_v1beta1_genesis_registry } from "./cosmos/authz/v1beta1/genesis";
import { registry as cosmos_authz_v1beta1_query_registry } from "./cosmos/authz/v1beta1/query";
import { registry as cosmos_authz_v1beta1_tx_registry } from "./cosmos/authz/v1beta1/tx";
import { registry as cosmos_bank_v1beta1_authz_registry } from "./cosmos/bank/v1beta1/authz";
import { registry as cosmos_bank_v1beta1_bank_registry } from "./cosmos/bank/v1beta1/bank";
import { registry as cosmos_bank_v1beta1_genesis_registry } from "./cosmos/bank/v1beta1/genesis";
import { registry as cosmos_bank_v1beta1_query_registry } from "./cosmos/bank/v1beta1/query";
import { registry as cosmos_bank_v1beta1_tx_registry } from "./cosmos/bank/v1beta1/tx";
import { registry as cosmos_base_abci_v1beta1_abci_registry } from "./cosmos/base/abci/v1beta1/abci";
import { registry as cosmos_base_kv_v1beta1_kv_registry } from "./cosmos/base/kv/v1beta1/kv";
import { registry as cosmos_base_query_v1beta1_pagination_registry } from "./cosmos/base/query/v1beta1/pagination";
import { registry as cosmos_base_reflection_v1beta1_reflection_registry } from "./cosmos/base/reflection/v1beta1/reflection";
import { registry as cosmos_base_reflection_v2alpha1_reflection_registry } from "./cosmos/base/reflection/v2alpha1/reflection";
import { registry as cosmos_base_snapshots_v1beta1_snapshot_registry } from "./cosmos/base/snapshots/v1beta1/snapshot";
import { registry as cosmos_base_store_v1beta1_commit_info_registry } from "./cosmos/base/store/v1beta1/commit_info";
import { registry as cosmos_base_store_v1beta1_listening_registry } from "./cosmos/base/store/v1beta1/listening";
import { registry as cosmos_base_tendermint_v1beta1_query_registry } from "./cosmos/base/tendermint/v1beta1/query";
import { registry as cosmos_base_v1beta1_coin_registry } from "./cosmos/base/v1beta1/coin";
import { registry as cosmos_capability_v1beta1_capability_registry } from "./cosmos/capability/v1beta1/capability";
import { registry as cosmos_capability_v1beta1_genesis_registry } from "./cosmos/capability/v1beta1/genesis";
import { registry as cosmos_crisis_v1beta1_genesis_registry } from "./cosmos/crisis/v1beta1/genesis";
import { registry as cosmos_crisis_v1beta1_tx_registry } from "./cosmos/crisis/v1beta1/tx";
import { registry as cosmos_crypto_ed25519_keys_registry } from "./cosmos/crypto/ed25519/keys";
import { registry as cosmos_crypto_multisig_keys_registry } from "./cosmos/crypto/multisig/keys";
import { registry as cosmos_crypto_multisig_v1beta1_multisig_registry } from "./cosmos/crypto/multisig/v1beta1/multisig";
import { registry as cosmos_crypto_secp256k1_keys_registry } from "./cosmos/crypto/secp256k1/keys";
import { registry as cosmos_crypto_secp256r1_keys_registry } from "./cosmos/crypto/secp256r1/keys";
import { registry as cosmos_crypto_sr25519_keys_registry } from "./cosmos/crypto/sr25519/keys";
import { registry as cosmos_distribution_v1beta1_distribution_registry } from "./cosmos/distribution/v1beta1/distribution";
import { registry as cosmos_distribution_v1beta1_genesis_registry } from "./cosmos/distribution/v1beta1/genesis";
import { registry as cosmos_distribution_v1beta1_query_registry } from "./cosmos/distribution/v1beta1/query";
import { registry as cosmos_distribution_v1beta1_tx_registry } from "./cosmos/distribution/v1beta1/tx";
import { registry as cosmos_evidence_v1beta1_evidence_registry } from "./cosmos/evidence/v1beta1/evidence";
import { registry as cosmos_evidence_v1beta1_genesis_registry } from "./cosmos/evidence/v1beta1/genesis";
import { registry as cosmos_evidence_v1beta1_query_registry } from "./cosmos/evidence/v1beta1/query";
import { registry as cosmos_evidence_v1beta1_tx_registry } from "./cosmos/evidence/v1beta1/tx";
import { registry as cosmos_feegrant_v1beta1_feegrant_registry } from "./cosmos/feegrant/v1beta1/feegrant";
import { registry as cosmos_feegrant_v1beta1_genesis_registry } from "./cosmos/feegrant/v1beta1/genesis";
import { registry as cosmos_feegrant_v1beta1_query_registry } from "./cosmos/feegrant/v1beta1/query";
import { registry as cosmos_feegrant_v1beta1_tx_registry } from "./cosmos/feegrant/v1beta1/tx";
import { registry as cosmos_genutil_v1beta1_genesis_registry } from "./cosmos/genutil/v1beta1/genesis";
import { registry as cosmos_gov_v1beta1_genesis_registry } from "./cosmos/gov/v1beta1/genesis";
import { registry as cosmos_gov_v1beta1_gov_registry } from "./cosmos/gov/v1beta1/gov";
import { registry as cosmos_gov_v1beta1_query_registry } from "./cosmos/gov/v1beta1/query";
import { registry as cosmos_gov_v1beta1_tx_registry } from "./cosmos/gov/v1beta1/tx";
import { registry as cosmos_mint_v1beta1_genesis_registry } from "./cosmos/mint/v1beta1/genesis";
import { registry as cosmos_mint_v1beta1_mint_registry } from "./cosmos/mint/v1beta1/mint";
import { registry as cosmos_mint_v1beta1_query_registry } from "./cosmos/mint/v1beta1/query";
import { registry as cosmos_params_types_types_registry } from "./cosmos/params/types/types";
import { registry as cosmos_params_v1beta1_params_registry } from "./cosmos/params/v1beta1/params";
import { registry as cosmos_params_v1beta1_query_registry } from "./cosmos/params/v1beta1/query";
import { registry as cosmos_slashing_v1beta1_genesis_registry } from "./cosmos/slashing/v1beta1/genesis";
import { registry as cosmos_slashing_v1beta1_query_registry } from "./cosmos/slashing/v1beta1/query";
import { registry as cosmos_slashing_v1beta1_slashing_registry } from "./cosmos/slashing/v1beta1/slashing";
import { registry as cosmos_slashing_v1beta1_tx_registry } from "./cosmos/slashing/v1beta1/tx";
import { registry as cosmos_staking_v1beta1_authz_registry } from "./cosmos/staking/v1beta1/authz";
import { registry as cosmos_staking_v1beta1_genesis_registry } from "./cosmos/staking/v1beta1/genesis";
import { registry as cosmos_staking_v1beta1_query_registry } from "./cosmos/staking/v1beta1/query";
import { registry as cosmos_staking_v1beta1_staking_registry } from "./cosmos/staking/v1beta1/staking";
import { registry as cosmos_staking_v1beta1_tx_registry } from "./cosmos/staking/v1beta1/tx";
import { registry as cosmos_tx_signing_v1beta1_signing_registry } from "./cosmos/tx/signing/v1beta1/signing";
import { registry as cosmos_tx_v1beta1_service_registry } from "./cosmos/tx/v1beta1/service";
import { registry as cosmos_tx_v1beta1_tx_registry } from "./cosmos/tx/v1beta1/tx";
import { registry as cosmos_upgrade_v1beta1_query_registry } from "./cosmos/upgrade/v1beta1/query";
import { registry as cosmos_upgrade_v1beta1_upgrade_registry } from "./cosmos/upgrade/v1beta1/upgrade";
import { registry as cosmos_vesting_v1beta1_tx_registry } from "./cosmos/vesting/v1beta1/tx";
import { registry as cosmos_vesting_v1beta1_vesting_registry } from "./cosmos/vesting/v1beta1/vesting";
import { registry as epoch_epoch_registry } from "./epoch/epoch";
import { registry as epoch_genesis_registry } from "./epoch/genesis";
import { registry as epoch_params_registry } from "./epoch/params";
import { registry as epoch_query_registry } from "./epoch/query";
import { registry as eth_tx_registry } from "./eth/tx";
import { registry as evm_config_registry } from "./evm/config";
import { registry as evm_genesis_registry } from "./evm/genesis";
import { registry as evm_gov_registry } from "./evm/gov";
import { registry as evm_params_registry } from "./evm/params";
import { registry as evm_query_registry } from "./evm/query";
import { registry as evm_receipt_registry } from "./evm/receipt";
import { registry as evm_tx_registry } from "./evm/tx";
import { registry as evm_types_registry } from "./evm/types";
import { registry as google_api_http_registry } from "./google/api/http";
import { registry as google_api_httpbody_registry } from "./google/api/httpbody";
import { registry as google_protobuf_any_registry } from "./google/protobuf/any";
import { registry as google_protobuf_descriptor_registry } from "./google/protobuf/descriptor";
import { registry as google_protobuf_duration_registry } from "./google/protobuf/duration";
import { registry as google_protobuf_timestamp_registry } from "./google/protobuf/timestamp";
import { registry as mint_v1beta1_genesis_registry } from "./mint/v1beta1/genesis";
import { registry as mint_v1beta1_gov_registry } from "./mint/v1beta1/gov";
import { registry as mint_v1beta1_mint_registry } from "./mint/v1beta1/mint";
import { registry as mint_v1beta1_query_registry } from "./mint/v1beta1/query";
import { registry as oracle_genesis_registry } from "./oracle/genesis";
import { registry as oracle_oracle_registry } from "./oracle/oracle";
import { registry as oracle_query_registry } from "./oracle/query";
import { registry as oracle_tx_registry } from "./oracle/tx";
import { registry as tendermint_abci_types_registry } from "./tendermint/abci/types";
import { registry as tendermint_crypto_keys_registry } from "./tendermint/crypto/keys";
import { registry as tendermint_crypto_proof_registry } from "./tendermint/crypto/proof";
import { registry as tendermint_libs_bits_types_registry } from "./tendermint/libs/bits/types";
import { registry as tendermint_p2p_types_registry } from "./tendermint/p2p/types";
import { registry as tendermint_types_block_registry } from "./tendermint/types/block";
import { registry as tendermint_types_evidence_registry } from "./tendermint/types/evidence";
import { registry as tendermint_types_params_registry } from "./tendermint/types/params";
import { registry as tendermint_types_types_registry } from "./tendermint/types/types";
import { registry as tendermint_types_validator_registry } from "./tendermint/types/validator";
import { registry as tendermint_version_types_registry } from "./tendermint/version/types";
import { registry as tokenfactory_authorityMetadata_registry } from "./tokenfactory/authorityMetadata";
import { registry as tokenfactory_genesis_registry } from "./tokenfactory/genesis";
import { registry as tokenfactory_params_registry } from "./tokenfactory/params";
import { registry as tokenfactory_query_registry } from "./tokenfactory/query";
import { registry as tokenfactory_tx_registry } from "./tokenfactory/tx";
export const paxProtoRegistry = [
    ...confio_proofs_registry,
    ...cosmos_accesscontrol_x_genesis_registry,
    ...cosmos_accesscontrol_x_gov_registry,
    ...cosmos_accesscontrol_x_query_registry,
    ...cosmos_accesscontrol_x_tx_registry,
    ...cosmos_accesscontrol_accesscontrol_registry,
    ...cosmos_accesscontrol_legacy_registry,
    ...cosmos_auth_v1beta1_auth_registry,
    ...cosmos_auth_v1beta1_genesis_registry,
    ...cosmos_auth_v1beta1_query_registry,
    ...cosmos_authz_v1beta1_authz_registry,
    ...cosmos_authz_v1beta1_event_registry,
    ...cosmos_authz_v1beta1_genesis_registry,
    ...cosmos_authz_v1beta1_query_registry,
    ...cosmos_authz_v1beta1_tx_registry,
    ...cosmos_bank_v1beta1_authz_registry,
    ...cosmos_bank_v1beta1_bank_registry,
    ...cosmos_bank_v1beta1_genesis_registry,
    ...cosmos_bank_v1beta1_query_registry,
    ...cosmos_bank_v1beta1_tx_registry,
    ...cosmos_base_abci_v1beta1_abci_registry,
    ...cosmos_base_kv_v1beta1_kv_registry,
    ...cosmos_base_query_v1beta1_pagination_registry,
    ...cosmos_base_reflection_v1beta1_reflection_registry,
    ...cosmos_base_reflection_v2alpha1_reflection_registry,
    ...cosmos_base_snapshots_v1beta1_snapshot_registry,
    ...cosmos_base_store_v1beta1_commit_info_registry,
    ...cosmos_base_store_v1beta1_listening_registry,
    ...cosmos_base_tendermint_v1beta1_query_registry,
    ...cosmos_base_v1beta1_coin_registry,
    ...cosmos_capability_v1beta1_capability_registry,
    ...cosmos_capability_v1beta1_genesis_registry,
    ...cosmos_crisis_v1beta1_genesis_registry,
    ...cosmos_crisis_v1beta1_tx_registry,
    ...cosmos_crypto_ed25519_keys_registry,
    ...cosmos_crypto_multisig_keys_registry,
    ...cosmos_crypto_multisig_v1beta1_multisig_registry,
    ...cosmos_crypto_secp256k1_keys_registry,
    ...cosmos_crypto_secp256r1_keys_registry,
    ...cosmos_crypto_sr25519_keys_registry,
    ...cosmos_distribution_v1beta1_distribution_registry,
    ...cosmos_distribution_v1beta1_genesis_registry,
    ...cosmos_distribution_v1beta1_query_registry,
    ...cosmos_distribution_v1beta1_tx_registry,
    ...cosmos_evidence_v1beta1_evidence_registry,
    ...cosmos_evidence_v1beta1_genesis_registry,
    ...cosmos_evidence_v1beta1_query_registry,
    ...cosmos_evidence_v1beta1_tx_registry,
    ...cosmos_feegrant_v1beta1_feegrant_registry,
    ...cosmos_feegrant_v1beta1_genesis_registry,
    ...cosmos_feegrant_v1beta1_query_registry,
    ...cosmos_feegrant_v1beta1_tx_registry,
    ...cosmos_genutil_v1beta1_genesis_registry,
    ...cosmos_gov_v1beta1_genesis_registry,
    ...cosmos_gov_v1beta1_gov_registry,
    ...cosmos_gov_v1beta1_query_registry,
    ...cosmos_gov_v1beta1_tx_registry,
    ...cosmos_mint_v1beta1_genesis_registry,
    ...cosmos_mint_v1beta1_mint_registry,
    ...cosmos_mint_v1beta1_query_registry,
    ...cosmos_params_types_types_registry,
    ...cosmos_params_v1beta1_params_registry,
    ...cosmos_params_v1beta1_query_registry,
    ...cosmos_slashing_v1beta1_genesis_registry,
    ...cosmos_slashing_v1beta1_query_registry,
    ...cosmos_slashing_v1beta1_slashing_registry,
    ...cosmos_slashing_v1beta1_tx_registry,
    ...cosmos_staking_v1beta1_authz_registry,
    ...cosmos_staking_v1beta1_genesis_registry,
    ...cosmos_staking_v1beta1_query_registry,
    ...cosmos_staking_v1beta1_staking_registry,
    ...cosmos_staking_v1beta1_tx_registry,
    ...cosmos_tx_signing_v1beta1_signing_registry,
    ...cosmos_tx_v1beta1_service_registry,
    ...cosmos_tx_v1beta1_tx_registry,
    ...cosmos_upgrade_v1beta1_query_registry,
    ...cosmos_upgrade_v1beta1_upgrade_registry,
    ...cosmos_vesting_v1beta1_tx_registry,
    ...cosmos_vesting_v1beta1_vesting_registry,
    ...epoch_epoch_registry,
    ...epoch_genesis_registry,
    ...epoch_params_registry,
    ...epoch_query_registry,
    ...eth_tx_registry,
    ...evm_config_registry,
    ...evm_genesis_registry,
    ...evm_gov_registry,
    ...evm_params_registry,
    ...evm_query_registry,
    ...evm_receipt_registry,
    ...evm_tx_registry,
    ...evm_types_registry,
    ...google_api_http_registry,
    ...google_api_httpbody_registry,
    ...google_protobuf_any_registry,
    ...google_protobuf_descriptor_registry,
    ...google_protobuf_duration_registry,
    ...google_protobuf_timestamp_registry,
    ...mint_v1beta1_genesis_registry,
    ...mint_v1beta1_gov_registry,
    ...mint_v1beta1_mint_registry,
    ...mint_v1beta1_query_registry,
    ...oracle_genesis_registry,
    ...oracle_oracle_registry,
    ...oracle_query_registry,
    ...oracle_tx_registry,
    ...tendermint_abci_types_registry,
    ...tendermint_crypto_keys_registry,
    ...tendermint_crypto_proof_registry,
    ...tendermint_libs_bits_types_registry,
    ...tendermint_p2p_types_registry,
    ...tendermint_types_block_registry,
    ...tendermint_types_evidence_registry,
    ...tendermint_types_params_registry,
    ...tendermint_types_types_registry,
    ...tendermint_types_validator_registry,
    ...tendermint_version_types_registry,
    ...tokenfactory_authorityMetadata_registry,
    ...tokenfactory_genesis_registry,
    ...tokenfactory_params_registry,
    ...tokenfactory_query_registry,
    ...tokenfactory_tx_registry
];
