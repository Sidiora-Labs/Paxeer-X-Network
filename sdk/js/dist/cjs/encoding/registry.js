"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.paxProtoRegistry = void 0;
const proofs_1 = require("./confio/proofs");
const accesscontrol_1 = require("./cosmos/accesscontrol/accesscontrol");
const legacy_1 = require("./cosmos/accesscontrol/legacy");
const genesis_1 = require("./cosmos/accesscontrol_x/genesis");
const gov_1 = require("./cosmos/accesscontrol_x/gov");
const query_1 = require("./cosmos/accesscontrol_x/query");
const tx_1 = require("./cosmos/accesscontrol_x/tx");
const auth_1 = require("./cosmos/auth/v1beta1/auth");
const genesis_2 = require("./cosmos/auth/v1beta1/genesis");
const query_2 = require("./cosmos/auth/v1beta1/query");
const authz_1 = require("./cosmos/authz/v1beta1/authz");
const event_1 = require("./cosmos/authz/v1beta1/event");
const genesis_3 = require("./cosmos/authz/v1beta1/genesis");
const query_3 = require("./cosmos/authz/v1beta1/query");
const tx_2 = require("./cosmos/authz/v1beta1/tx");
const authz_2 = require("./cosmos/bank/v1beta1/authz");
const bank_1 = require("./cosmos/bank/v1beta1/bank");
const genesis_4 = require("./cosmos/bank/v1beta1/genesis");
const query_4 = require("./cosmos/bank/v1beta1/query");
const tx_3 = require("./cosmos/bank/v1beta1/tx");
const abci_1 = require("./cosmos/base/abci/v1beta1/abci");
const kv_1 = require("./cosmos/base/kv/v1beta1/kv");
const pagination_1 = require("./cosmos/base/query/v1beta1/pagination");
const reflection_1 = require("./cosmos/base/reflection/v1beta1/reflection");
const reflection_2 = require("./cosmos/base/reflection/v2alpha1/reflection");
const snapshot_1 = require("./cosmos/base/snapshots/v1beta1/snapshot");
const commit_info_1 = require("./cosmos/base/store/v1beta1/commit_info");
const listening_1 = require("./cosmos/base/store/v1beta1/listening");
const query_5 = require("./cosmos/base/tendermint/v1beta1/query");
const coin_1 = require("./cosmos/base/v1beta1/coin");
const capability_1 = require("./cosmos/capability/v1beta1/capability");
const genesis_5 = require("./cosmos/capability/v1beta1/genesis");
const genesis_6 = require("./cosmos/crisis/v1beta1/genesis");
const tx_4 = require("./cosmos/crisis/v1beta1/tx");
const keys_1 = require("./cosmos/crypto/ed25519/keys");
const keys_2 = require("./cosmos/crypto/multisig/keys");
const multisig_1 = require("./cosmos/crypto/multisig/v1beta1/multisig");
const keys_3 = require("./cosmos/crypto/secp256k1/keys");
const keys_4 = require("./cosmos/crypto/secp256r1/keys");
const keys_5 = require("./cosmos/crypto/sr25519/keys");
const distribution_1 = require("./cosmos/distribution/v1beta1/distribution");
const genesis_7 = require("./cosmos/distribution/v1beta1/genesis");
const query_6 = require("./cosmos/distribution/v1beta1/query");
const tx_5 = require("./cosmos/distribution/v1beta1/tx");
const evidence_1 = require("./cosmos/evidence/v1beta1/evidence");
const genesis_8 = require("./cosmos/evidence/v1beta1/genesis");
const query_7 = require("./cosmos/evidence/v1beta1/query");
const tx_6 = require("./cosmos/evidence/v1beta1/tx");
const feegrant_1 = require("./cosmos/feegrant/v1beta1/feegrant");
const genesis_9 = require("./cosmos/feegrant/v1beta1/genesis");
const query_8 = require("./cosmos/feegrant/v1beta1/query");
const tx_7 = require("./cosmos/feegrant/v1beta1/tx");
const genesis_10 = require("./cosmos/genutil/v1beta1/genesis");
const genesis_11 = require("./cosmos/gov/v1beta1/genesis");
const gov_2 = require("./cosmos/gov/v1beta1/gov");
const query_9 = require("./cosmos/gov/v1beta1/query");
const tx_8 = require("./cosmos/gov/v1beta1/tx");
const genesis_12 = require("./cosmos/mint/v1beta1/genesis");
const mint_1 = require("./cosmos/mint/v1beta1/mint");
const query_10 = require("./cosmos/mint/v1beta1/query");
const types_1 = require("./cosmos/params/types/types");
const params_1 = require("./cosmos/params/v1beta1/params");
const query_11 = require("./cosmos/params/v1beta1/query");
const genesis_13 = require("./cosmos/slashing/v1beta1/genesis");
const query_12 = require("./cosmos/slashing/v1beta1/query");
const slashing_1 = require("./cosmos/slashing/v1beta1/slashing");
const tx_9 = require("./cosmos/slashing/v1beta1/tx");
const authz_3 = require("./cosmos/staking/v1beta1/authz");
const genesis_14 = require("./cosmos/staking/v1beta1/genesis");
const query_13 = require("./cosmos/staking/v1beta1/query");
const staking_1 = require("./cosmos/staking/v1beta1/staking");
const tx_10 = require("./cosmos/staking/v1beta1/tx");
const signing_1 = require("./cosmos/tx/signing/v1beta1/signing");
const service_1 = require("./cosmos/tx/v1beta1/service");
const tx_11 = require("./cosmos/tx/v1beta1/tx");
const query_14 = require("./cosmos/upgrade/v1beta1/query");
const upgrade_1 = require("./cosmos/upgrade/v1beta1/upgrade");
const tx_12 = require("./cosmos/vesting/v1beta1/tx");
const vesting_1 = require("./cosmos/vesting/v1beta1/vesting");
const epoch_1 = require("./epoch/epoch");
const genesis_15 = require("./epoch/genesis");
const params_2 = require("./epoch/params");
const query_15 = require("./epoch/query");
const tx_13 = require("./eth/tx");
const config_1 = require("./evm/config");
const genesis_16 = require("./evm/genesis");
const gov_3 = require("./evm/gov");
const params_3 = require("./evm/params");
const query_16 = require("./evm/query");
const receipt_1 = require("./evm/receipt");
const tx_14 = require("./evm/tx");
const types_2 = require("./evm/types");
const http_1 = require("./google/api/http");
const httpbody_1 = require("./google/api/httpbody");
const any_1 = require("./google/protobuf/any");
const descriptor_1 = require("./google/protobuf/descriptor");
const duration_1 = require("./google/protobuf/duration");
const timestamp_1 = require("./google/protobuf/timestamp");
const genesis_17 = require("./mint/v1beta1/genesis");
const gov_4 = require("./mint/v1beta1/gov");
const mint_2 = require("./mint/v1beta1/mint");
const query_17 = require("./mint/v1beta1/query");
const genesis_18 = require("./oracle/genesis");
const oracle_1 = require("./oracle/oracle");
const query_18 = require("./oracle/query");
const tx_15 = require("./oracle/tx");
const types_3 = require("./tendermint/abci/types");
const keys_6 = require("./tendermint/crypto/keys");
const proof_1 = require("./tendermint/crypto/proof");
const types_4 = require("./tendermint/libs/bits/types");
const types_5 = require("./tendermint/p2p/types");
const block_1 = require("./tendermint/types/block");
const evidence_2 = require("./tendermint/types/evidence");
const params_4 = require("./tendermint/types/params");
const types_6 = require("./tendermint/types/types");
const validator_1 = require("./tendermint/types/validator");
const types_7 = require("./tendermint/version/types");
const authorityMetadata_1 = require("./tokenfactory/authorityMetadata");
const genesis_19 = require("./tokenfactory/genesis");
const params_5 = require("./tokenfactory/params");
const query_19 = require("./tokenfactory/query");
const tx_16 = require("./tokenfactory/tx");
exports.paxProtoRegistry = [
    ...proofs_1.registry,
    ...genesis_1.registry,
    ...gov_1.registry,
    ...query_1.registry,
    ...tx_1.registry,
    ...accesscontrol_1.registry,
    ...legacy_1.registry,
    ...auth_1.registry,
    ...genesis_2.registry,
    ...query_2.registry,
    ...authz_1.registry,
    ...event_1.registry,
    ...genesis_3.registry,
    ...query_3.registry,
    ...tx_2.registry,
    ...authz_2.registry,
    ...bank_1.registry,
    ...genesis_4.registry,
    ...query_4.registry,
    ...tx_3.registry,
    ...abci_1.registry,
    ...kv_1.registry,
    ...pagination_1.registry,
    ...reflection_1.registry,
    ...reflection_2.registry,
    ...snapshot_1.registry,
    ...commit_info_1.registry,
    ...listening_1.registry,
    ...query_5.registry,
    ...coin_1.registry,
    ...capability_1.registry,
    ...genesis_5.registry,
    ...genesis_6.registry,
    ...tx_4.registry,
    ...keys_1.registry,
    ...keys_2.registry,
    ...multisig_1.registry,
    ...keys_3.registry,
    ...keys_4.registry,
    ...keys_5.registry,
    ...distribution_1.registry,
    ...genesis_7.registry,
    ...query_6.registry,
    ...tx_5.registry,
    ...evidence_1.registry,
    ...genesis_8.registry,
    ...query_7.registry,
    ...tx_6.registry,
    ...feegrant_1.registry,
    ...genesis_9.registry,
    ...query_8.registry,
    ...tx_7.registry,
    ...genesis_10.registry,
    ...genesis_11.registry,
    ...gov_2.registry,
    ...query_9.registry,
    ...tx_8.registry,
    ...genesis_12.registry,
    ...mint_1.registry,
    ...query_10.registry,
    ...types_1.registry,
    ...params_1.registry,
    ...query_11.registry,
    ...genesis_13.registry,
    ...query_12.registry,
    ...slashing_1.registry,
    ...tx_9.registry,
    ...authz_3.registry,
    ...genesis_14.registry,
    ...query_13.registry,
    ...staking_1.registry,
    ...tx_10.registry,
    ...signing_1.registry,
    ...service_1.registry,
    ...tx_11.registry,
    ...query_14.registry,
    ...upgrade_1.registry,
    ...tx_12.registry,
    ...vesting_1.registry,
    ...epoch_1.registry,
    ...genesis_15.registry,
    ...params_2.registry,
    ...query_15.registry,
    ...tx_13.registry,
    ...config_1.registry,
    ...genesis_16.registry,
    ...gov_3.registry,
    ...params_3.registry,
    ...query_16.registry,
    ...receipt_1.registry,
    ...tx_14.registry,
    ...types_2.registry,
    ...http_1.registry,
    ...httpbody_1.registry,
    ...any_1.registry,
    ...descriptor_1.registry,
    ...duration_1.registry,
    ...timestamp_1.registry,
    ...genesis_17.registry,
    ...gov_4.registry,
    ...mint_2.registry,
    ...query_17.registry,
    ...genesis_18.registry,
    ...oracle_1.registry,
    ...query_18.registry,
    ...tx_15.registry,
    ...types_3.registry,
    ...keys_6.registry,
    ...proof_1.registry,
    ...types_4.registry,
    ...types_5.registry,
    ...block_1.registry,
    ...evidence_2.registry,
    ...params_4.registry,
    ...types_6.registry,
    ...validator_1.registry,
    ...types_7.registry,
    ...authorityMetadata_1.registry,
    ...genesis_19.registry,
    ...params_5.registry,
    ...query_19.registry,
    ...tx_16.registry
];
