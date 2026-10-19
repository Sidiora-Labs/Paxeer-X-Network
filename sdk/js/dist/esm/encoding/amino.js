import { aminoConverters as confio_proofs_amino } from "./confio/proofs";
import { aminoConverters as cosmos_accesscontrol_accesscontrol_amino } from "./cosmos/accesscontrol/accesscontrol";
import { aminoConverters as cosmos_accesscontrol_legacy_amino } from "./cosmos/accesscontrol/legacy";
import { aminoConverters as cosmos_accesscontrol_x_genesis_amino } from "./cosmos/accesscontrol_x/genesis";
import { aminoConverters as cosmos_accesscontrol_x_gov_amino } from "./cosmos/accesscontrol_x/gov";
import { aminoConverters as cosmos_accesscontrol_x_query_amino } from "./cosmos/accesscontrol_x/query";
import { aminoConverters as cosmos_accesscontrol_x_tx_amino } from "./cosmos/accesscontrol_x/tx";
import { aminoConverters as cosmos_auth_v1beta1_auth_amino } from "./cosmos/auth/v1beta1/auth";
import { aminoConverters as cosmos_auth_v1beta1_genesis_amino } from "./cosmos/auth/v1beta1/genesis";
import { aminoConverters as cosmos_auth_v1beta1_query_amino } from "./cosmos/auth/v1beta1/query";
import { aminoConverters as cosmos_authz_v1beta1_authz_amino } from "./cosmos/authz/v1beta1/authz";
import { aminoConverters as cosmos_authz_v1beta1_event_amino } from "./cosmos/authz/v1beta1/event";
import { aminoConverters as cosmos_authz_v1beta1_genesis_amino } from "./cosmos/authz/v1beta1/genesis";
import { aminoConverters as cosmos_authz_v1beta1_query_amino } from "./cosmos/authz/v1beta1/query";
import { aminoConverters as cosmos_authz_v1beta1_tx_amino } from "./cosmos/authz/v1beta1/tx";
import { aminoConverters as cosmos_bank_v1beta1_authz_amino } from "./cosmos/bank/v1beta1/authz";
import { aminoConverters as cosmos_bank_v1beta1_bank_amino } from "./cosmos/bank/v1beta1/bank";
import { aminoConverters as cosmos_bank_v1beta1_genesis_amino } from "./cosmos/bank/v1beta1/genesis";
import { aminoConverters as cosmos_bank_v1beta1_query_amino } from "./cosmos/bank/v1beta1/query";
import { aminoConverters as cosmos_bank_v1beta1_tx_amino } from "./cosmos/bank/v1beta1/tx";
import { aminoConverters as cosmos_base_abci_v1beta1_abci_amino } from "./cosmos/base/abci/v1beta1/abci";
import { aminoConverters as cosmos_base_kv_v1beta1_kv_amino } from "./cosmos/base/kv/v1beta1/kv";
import { aminoConverters as cosmos_base_query_v1beta1_pagination_amino } from "./cosmos/base/query/v1beta1/pagination";
import { aminoConverters as cosmos_base_reflection_v1beta1_reflection_amino } from "./cosmos/base/reflection/v1beta1/reflection";
import { aminoConverters as cosmos_base_reflection_v2alpha1_reflection_amino } from "./cosmos/base/reflection/v2alpha1/reflection";
import { aminoConverters as cosmos_base_snapshots_v1beta1_snapshot_amino } from "./cosmos/base/snapshots/v1beta1/snapshot";
import { aminoConverters as cosmos_base_store_v1beta1_commit_info_amino } from "./cosmos/base/store/v1beta1/commit_info";
import { aminoConverters as cosmos_base_store_v1beta1_listening_amino } from "./cosmos/base/store/v1beta1/listening";
import { aminoConverters as cosmos_base_tendermint_v1beta1_query_amino } from "./cosmos/base/tendermint/v1beta1/query";
import { aminoConverters as cosmos_base_v1beta1_coin_amino } from "./cosmos/base/v1beta1/coin";
import { aminoConverters as cosmos_capability_v1beta1_capability_amino } from "./cosmos/capability/v1beta1/capability";
import { aminoConverters as cosmos_capability_v1beta1_genesis_amino } from "./cosmos/capability/v1beta1/genesis";
import { aminoConverters as cosmos_crisis_v1beta1_genesis_amino } from "./cosmos/crisis/v1beta1/genesis";
import { aminoConverters as cosmos_crisis_v1beta1_tx_amino } from "./cosmos/crisis/v1beta1/tx";
import { aminoConverters as cosmos_crypto_ed25519_keys_amino } from "./cosmos/crypto/ed25519/keys";
import { aminoConverters as cosmos_crypto_multisig_keys_amino } from "./cosmos/crypto/multisig/keys";
import { aminoConverters as cosmos_crypto_multisig_v1beta1_multisig_amino } from "./cosmos/crypto/multisig/v1beta1/multisig";
import { aminoConverters as cosmos_crypto_secp256k1_keys_amino } from "./cosmos/crypto/secp256k1/keys";
import { aminoConverters as cosmos_crypto_secp256r1_keys_amino } from "./cosmos/crypto/secp256r1/keys";
import { aminoConverters as cosmos_crypto_sr25519_keys_amino } from "./cosmos/crypto/sr25519/keys";
import { aminoConverters as cosmos_distribution_v1beta1_distribution_amino } from "./cosmos/distribution/v1beta1/distribution";
import { aminoConverters as cosmos_distribution_v1beta1_genesis_amino } from "./cosmos/distribution/v1beta1/genesis";
import { aminoConverters as cosmos_distribution_v1beta1_query_amino } from "./cosmos/distribution/v1beta1/query";
import { aminoConverters as cosmos_distribution_v1beta1_tx_amino } from "./cosmos/distribution/v1beta1/tx";
import { aminoConverters as cosmos_evidence_v1beta1_evidence_amino } from "./cosmos/evidence/v1beta1/evidence";
import { aminoConverters as cosmos_evidence_v1beta1_genesis_amino } from "./cosmos/evidence/v1beta1/genesis";
import { aminoConverters as cosmos_evidence_v1beta1_query_amino } from "./cosmos/evidence/v1beta1/query";
import { aminoConverters as cosmos_evidence_v1beta1_tx_amino } from "./cosmos/evidence/v1beta1/tx";
import { aminoConverters as cosmos_feegrant_v1beta1_feegrant_amino } from "./cosmos/feegrant/v1beta1/feegrant";
import { aminoConverters as cosmos_feegrant_v1beta1_genesis_amino } from "./cosmos/feegrant/v1beta1/genesis";
import { aminoConverters as cosmos_feegrant_v1beta1_query_amino } from "./cosmos/feegrant/v1beta1/query";
import { aminoConverters as cosmos_feegrant_v1beta1_tx_amino } from "./cosmos/feegrant/v1beta1/tx";
import { aminoConverters as cosmos_genutil_v1beta1_genesis_amino } from "./cosmos/genutil/v1beta1/genesis";
import { aminoConverters as cosmos_gov_v1beta1_genesis_amino } from "./cosmos/gov/v1beta1/genesis";
import { aminoConverters as cosmos_gov_v1beta1_gov_amino } from "./cosmos/gov/v1beta1/gov";
import { aminoConverters as cosmos_gov_v1beta1_query_amino } from "./cosmos/gov/v1beta1/query";
import { aminoConverters as cosmos_gov_v1beta1_tx_amino } from "./cosmos/gov/v1beta1/tx";
import { aminoConverters as cosmos_mint_v1beta1_genesis_amino } from "./cosmos/mint/v1beta1/genesis";
import { aminoConverters as cosmos_mint_v1beta1_mint_amino } from "./cosmos/mint/v1beta1/mint";
import { aminoConverters as cosmos_mint_v1beta1_query_amino } from "./cosmos/mint/v1beta1/query";
import { aminoConverters as cosmos_params_types_types_amino } from "./cosmos/params/types/types";
import { aminoConverters as cosmos_params_v1beta1_params_amino } from "./cosmos/params/v1beta1/params";
import { aminoConverters as cosmos_params_v1beta1_query_amino } from "./cosmos/params/v1beta1/query";
import { aminoConverters as cosmos_slashing_v1beta1_genesis_amino } from "./cosmos/slashing/v1beta1/genesis";
import { aminoConverters as cosmos_slashing_v1beta1_query_amino } from "./cosmos/slashing/v1beta1/query";
import { aminoConverters as cosmos_slashing_v1beta1_slashing_amino } from "./cosmos/slashing/v1beta1/slashing";
import { aminoConverters as cosmos_slashing_v1beta1_tx_amino } from "./cosmos/slashing/v1beta1/tx";
import { aminoConverters as cosmos_staking_v1beta1_authz_amino } from "./cosmos/staking/v1beta1/authz";
import { aminoConverters as cosmos_staking_v1beta1_genesis_amino } from "./cosmos/staking/v1beta1/genesis";
import { aminoConverters as cosmos_staking_v1beta1_query_amino } from "./cosmos/staking/v1beta1/query";
import { aminoConverters as cosmos_staking_v1beta1_staking_amino } from "./cosmos/staking/v1beta1/staking";
import { aminoConverters as cosmos_staking_v1beta1_tx_amino } from "./cosmos/staking/v1beta1/tx";
import { aminoConverters as cosmos_tx_signing_v1beta1_signing_amino } from "./cosmos/tx/signing/v1beta1/signing";
import { aminoConverters as cosmos_tx_v1beta1_service_amino } from "./cosmos/tx/v1beta1/service";
import { aminoConverters as cosmos_tx_v1beta1_tx_amino } from "./cosmos/tx/v1beta1/tx";
import { aminoConverters as cosmos_upgrade_v1beta1_query_amino } from "./cosmos/upgrade/v1beta1/query";
import { aminoConverters as cosmos_upgrade_v1beta1_upgrade_amino } from "./cosmos/upgrade/v1beta1/upgrade";
import { aminoConverters as cosmos_vesting_v1beta1_tx_amino } from "./cosmos/vesting/v1beta1/tx";
import { aminoConverters as cosmos_vesting_v1beta1_vesting_amino } from "./cosmos/vesting/v1beta1/vesting";
import { aminoConverters as epoch_epoch_amino } from "./epoch/epoch";
import { aminoConverters as epoch_genesis_amino } from "./epoch/genesis";
import { aminoConverters as epoch_params_amino } from "./epoch/params";
import { aminoConverters as epoch_query_amino } from "./epoch/query";
import { aminoConverters as eth_tx_amino } from "./eth/tx";
import { aminoConverters as evm_config_amino } from "./evm/config";
import { aminoConverters as evm_genesis_amino } from "./evm/genesis";
import { aminoConverters as evm_gov_amino } from "./evm/gov";
import { aminoConverters as evm_params_amino } from "./evm/params";
import { aminoConverters as evm_query_amino } from "./evm/query";
import { aminoConverters as evm_receipt_amino } from "./evm/receipt";
import { aminoConverters as evm_tx_amino } from "./evm/tx";
import { aminoConverters as evm_types_amino } from "./evm/types";
import { aminoConverters as google_api_http_amino } from "./google/api/http";
import { aminoConverters as google_api_httpbody_amino } from "./google/api/httpbody";
import { aminoConverters as google_protobuf_any_amino } from "./google/protobuf/any";
import { aminoConverters as google_protobuf_descriptor_amino } from "./google/protobuf/descriptor";
import { aminoConverters as google_protobuf_duration_amino } from "./google/protobuf/duration";
import { aminoConverters as google_protobuf_timestamp_amino } from "./google/protobuf/timestamp";
import { aminoConverters as mint_v1beta1_genesis_amino } from "./mint/v1beta1/genesis";
import { aminoConverters as mint_v1beta1_gov_amino } from "./mint/v1beta1/gov";
import { aminoConverters as mint_v1beta1_mint_amino } from "./mint/v1beta1/mint";
import { aminoConverters as mint_v1beta1_query_amino } from "./mint/v1beta1/query";
import { aminoConverters as oracle_genesis_amino } from "./oracle/genesis";
import { aminoConverters as oracle_oracle_amino } from "./oracle/oracle";
import { aminoConverters as oracle_query_amino } from "./oracle/query";
import { aminoConverters as oracle_tx_amino } from "./oracle/tx";
import { aminoConverters as tendermint_abci_types_amino } from "./tendermint/abci/types";
import { aminoConverters as tendermint_crypto_keys_amino } from "./tendermint/crypto/keys";
import { aminoConverters as tendermint_crypto_proof_amino } from "./tendermint/crypto/proof";
import { aminoConverters as tendermint_libs_bits_types_amino } from "./tendermint/libs/bits/types";
import { aminoConverters as tendermint_p2p_types_amino } from "./tendermint/p2p/types";
import { aminoConverters as tendermint_types_block_amino } from "./tendermint/types/block";
import { aminoConverters as tendermint_types_evidence_amino } from "./tendermint/types/evidence";
import { aminoConverters as tendermint_types_params_amino } from "./tendermint/types/params";
import { aminoConverters as tendermint_types_types_amino } from "./tendermint/types/types";
import { aminoConverters as tendermint_types_validator_amino } from "./tendermint/types/validator";
import { aminoConverters as tendermint_version_types_amino } from "./tendermint/version/types";
import { aminoConverters as tokenfactory_authorityMetadata_amino } from "./tokenfactory/authorityMetadata";
import { aminoConverters as tokenfactory_genesis_amino } from "./tokenfactory/genesis";
import { aminoConverters as tokenfactory_params_amino } from "./tokenfactory/params";
import { aminoConverters as tokenfactory_query_amino } from "./tokenfactory/query";
import { aminoConverters as tokenfactory_tx_amino } from "./tokenfactory/tx";
export const aminoConverters = {
    ...confio_proofs_amino,
    ...cosmos_accesscontrol_x_genesis_amino,
    ...cosmos_accesscontrol_x_gov_amino,
    ...cosmos_accesscontrol_x_query_amino,
    ...cosmos_accesscontrol_x_tx_amino,
    ...cosmos_accesscontrol_accesscontrol_amino,
    ...cosmos_accesscontrol_legacy_amino,
    ...cosmos_auth_v1beta1_auth_amino,
    ...cosmos_auth_v1beta1_genesis_amino,
    ...cosmos_auth_v1beta1_query_amino,
    ...cosmos_authz_v1beta1_authz_amino,
    ...cosmos_authz_v1beta1_event_amino,
    ...cosmos_authz_v1beta1_genesis_amino,
    ...cosmos_authz_v1beta1_query_amino,
    ...cosmos_authz_v1beta1_tx_amino,
    ...cosmos_bank_v1beta1_authz_amino,
    ...cosmos_bank_v1beta1_bank_amino,
    ...cosmos_bank_v1beta1_genesis_amino,
    ...cosmos_bank_v1beta1_query_amino,
    ...cosmos_bank_v1beta1_tx_amino,
    ...cosmos_base_abci_v1beta1_abci_amino,
    ...cosmos_base_kv_v1beta1_kv_amino,
    ...cosmos_base_query_v1beta1_pagination_amino,
    ...cosmos_base_reflection_v1beta1_reflection_amino,
    ...cosmos_base_reflection_v2alpha1_reflection_amino,
    ...cosmos_base_snapshots_v1beta1_snapshot_amino,
    ...cosmos_base_store_v1beta1_commit_info_amino,
    ...cosmos_base_store_v1beta1_listening_amino,
    ...cosmos_base_tendermint_v1beta1_query_amino,
    ...cosmos_base_v1beta1_coin_amino,
    ...cosmos_capability_v1beta1_capability_amino,
    ...cosmos_capability_v1beta1_genesis_amino,
    ...cosmos_crisis_v1beta1_genesis_amino,
    ...cosmos_crisis_v1beta1_tx_amino,
    ...cosmos_crypto_ed25519_keys_amino,
    ...cosmos_crypto_multisig_keys_amino,
    ...cosmos_crypto_multisig_v1beta1_multisig_amino,
    ...cosmos_crypto_secp256k1_keys_amino,
    ...cosmos_crypto_secp256r1_keys_amino,
    ...cosmos_crypto_sr25519_keys_amino,
    ...cosmos_distribution_v1beta1_distribution_amino,
    ...cosmos_distribution_v1beta1_genesis_amino,
    ...cosmos_distribution_v1beta1_query_amino,
    ...cosmos_distribution_v1beta1_tx_amino,
    ...cosmos_evidence_v1beta1_evidence_amino,
    ...cosmos_evidence_v1beta1_genesis_amino,
    ...cosmos_evidence_v1beta1_query_amino,
    ...cosmos_evidence_v1beta1_tx_amino,
    ...cosmos_feegrant_v1beta1_feegrant_amino,
    ...cosmos_feegrant_v1beta1_genesis_amino,
    ...cosmos_feegrant_v1beta1_query_amino,
    ...cosmos_feegrant_v1beta1_tx_amino,
    ...cosmos_genutil_v1beta1_genesis_amino,
    ...cosmos_gov_v1beta1_genesis_amino,
    ...cosmos_gov_v1beta1_gov_amino,
    ...cosmos_gov_v1beta1_query_amino,
    ...cosmos_gov_v1beta1_tx_amino,
    ...cosmos_mint_v1beta1_genesis_amino,
    ...cosmos_mint_v1beta1_mint_amino,
    ...cosmos_mint_v1beta1_query_amino,
    ...cosmos_params_types_types_amino,
    ...cosmos_params_v1beta1_params_amino,
    ...cosmos_params_v1beta1_query_amino,
    ...cosmos_slashing_v1beta1_genesis_amino,
    ...cosmos_slashing_v1beta1_query_amino,
    ...cosmos_slashing_v1beta1_slashing_amino,
    ...cosmos_slashing_v1beta1_tx_amino,
    ...cosmos_staking_v1beta1_authz_amino,
    ...cosmos_staking_v1beta1_genesis_amino,
    ...cosmos_staking_v1beta1_query_amino,
    ...cosmos_staking_v1beta1_staking_amino,
    ...cosmos_staking_v1beta1_tx_amino,
    ...cosmos_tx_signing_v1beta1_signing_amino,
    ...cosmos_tx_v1beta1_service_amino,
    ...cosmos_tx_v1beta1_tx_amino,
    ...cosmos_upgrade_v1beta1_query_amino,
    ...cosmos_upgrade_v1beta1_upgrade_amino,
    ...cosmos_vesting_v1beta1_tx_amino,
    ...cosmos_vesting_v1beta1_vesting_amino,
    ...epoch_epoch_amino,
    ...epoch_genesis_amino,
    ...epoch_params_amino,
    ...epoch_query_amino,
    ...eth_tx_amino,
    ...evm_config_amino,
    ...evm_genesis_amino,
    ...evm_gov_amino,
    ...evm_params_amino,
    ...evm_query_amino,
    ...evm_receipt_amino,
    ...evm_tx_amino,
    ...evm_types_amino,
    ...google_api_http_amino,
    ...google_api_httpbody_amino,
    ...google_protobuf_any_amino,
    ...google_protobuf_descriptor_amino,
    ...google_protobuf_duration_amino,
    ...google_protobuf_timestamp_amino,
    ...mint_v1beta1_genesis_amino,
    ...mint_v1beta1_gov_amino,
    ...mint_v1beta1_mint_amino,
    ...mint_v1beta1_query_amino,
    ...oracle_genesis_amino,
    ...oracle_oracle_amino,
    ...oracle_query_amino,
    ...oracle_tx_amino,
    ...tendermint_abci_types_amino,
    ...tendermint_crypto_keys_amino,
    ...tendermint_crypto_proof_amino,
    ...tendermint_libs_bits_types_amino,
    ...tendermint_p2p_types_amino,
    ...tendermint_types_block_amino,
    ...tendermint_types_evidence_amino,
    ...tendermint_types_params_amino,
    ...tendermint_types_types_amino,
    ...tendermint_types_validator_amino,
    ...tendermint_version_types_amino,
    ...tokenfactory_authorityMetadata_amino,
    ...tokenfactory_genesis_amino,
    ...tokenfactory_params_amino,
    ...tokenfactory_query_amino,
    ...tokenfactory_tx_amino
};
