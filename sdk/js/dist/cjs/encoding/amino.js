"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = void 0;
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
exports.aminoConverters = {
    ...proofs_1.aminoConverters,
    ...genesis_1.aminoConverters,
    ...gov_1.aminoConverters,
    ...query_1.aminoConverters,
    ...tx_1.aminoConverters,
    ...accesscontrol_1.aminoConverters,
    ...legacy_1.aminoConverters,
    ...auth_1.aminoConverters,
    ...genesis_2.aminoConverters,
    ...query_2.aminoConverters,
    ...authz_1.aminoConverters,
    ...event_1.aminoConverters,
    ...genesis_3.aminoConverters,
    ...query_3.aminoConverters,
    ...tx_2.aminoConverters,
    ...authz_2.aminoConverters,
    ...bank_1.aminoConverters,
    ...genesis_4.aminoConverters,
    ...query_4.aminoConverters,
    ...tx_3.aminoConverters,
    ...abci_1.aminoConverters,
    ...kv_1.aminoConverters,
    ...pagination_1.aminoConverters,
    ...reflection_1.aminoConverters,
    ...reflection_2.aminoConverters,
    ...snapshot_1.aminoConverters,
    ...commit_info_1.aminoConverters,
    ...listening_1.aminoConverters,
    ...query_5.aminoConverters,
    ...coin_1.aminoConverters,
    ...capability_1.aminoConverters,
    ...genesis_5.aminoConverters,
    ...genesis_6.aminoConverters,
    ...tx_4.aminoConverters,
    ...keys_1.aminoConverters,
    ...keys_2.aminoConverters,
    ...multisig_1.aminoConverters,
    ...keys_3.aminoConverters,
    ...keys_4.aminoConverters,
    ...keys_5.aminoConverters,
    ...distribution_1.aminoConverters,
    ...genesis_7.aminoConverters,
    ...query_6.aminoConverters,
    ...tx_5.aminoConverters,
    ...evidence_1.aminoConverters,
    ...genesis_8.aminoConverters,
    ...query_7.aminoConverters,
    ...tx_6.aminoConverters,
    ...feegrant_1.aminoConverters,
    ...genesis_9.aminoConverters,
    ...query_8.aminoConverters,
    ...tx_7.aminoConverters,
    ...genesis_10.aminoConverters,
    ...genesis_11.aminoConverters,
    ...gov_2.aminoConverters,
    ...query_9.aminoConverters,
    ...tx_8.aminoConverters,
    ...genesis_12.aminoConverters,
    ...mint_1.aminoConverters,
    ...query_10.aminoConverters,
    ...types_1.aminoConverters,
    ...params_1.aminoConverters,
    ...query_11.aminoConverters,
    ...genesis_13.aminoConverters,
    ...query_12.aminoConverters,
    ...slashing_1.aminoConverters,
    ...tx_9.aminoConverters,
    ...authz_3.aminoConverters,
    ...genesis_14.aminoConverters,
    ...query_13.aminoConverters,
    ...staking_1.aminoConverters,
    ...tx_10.aminoConverters,
    ...signing_1.aminoConverters,
    ...service_1.aminoConverters,
    ...tx_11.aminoConverters,
    ...query_14.aminoConverters,
    ...upgrade_1.aminoConverters,
    ...tx_12.aminoConverters,
    ...vesting_1.aminoConverters,
    ...epoch_1.aminoConverters,
    ...genesis_15.aminoConverters,
    ...params_2.aminoConverters,
    ...query_15.aminoConverters,
    ...tx_13.aminoConverters,
    ...config_1.aminoConverters,
    ...genesis_16.aminoConverters,
    ...gov_3.aminoConverters,
    ...params_3.aminoConverters,
    ...query_16.aminoConverters,
    ...receipt_1.aminoConverters,
    ...tx_14.aminoConverters,
    ...types_2.aminoConverters,
    ...http_1.aminoConverters,
    ...httpbody_1.aminoConverters,
    ...any_1.aminoConverters,
    ...descriptor_1.aminoConverters,
    ...duration_1.aminoConverters,
    ...timestamp_1.aminoConverters,
    ...genesis_17.aminoConverters,
    ...gov_4.aminoConverters,
    ...mint_2.aminoConverters,
    ...query_17.aminoConverters,
    ...genesis_18.aminoConverters,
    ...oracle_1.aminoConverters,
    ...query_18.aminoConverters,
    ...tx_15.aminoConverters,
    ...types_3.aminoConverters,
    ...keys_6.aminoConverters,
    ...proof_1.aminoConverters,
    ...types_4.aminoConverters,
    ...types_5.aminoConverters,
    ...block_1.aminoConverters,
    ...evidence_2.aminoConverters,
    ...params_4.aminoConverters,
    ...types_6.aminoConverters,
    ...validator_1.aminoConverters,
    ...types_7.aminoConverters,
    ...authorityMetadata_1.aminoConverters,
    ...genesis_19.aminoConverters,
    ...params_5.aminoConverters,
    ...query_19.aminoConverters,
    ...tx_16.aminoConverters
};
