import * as confio from "./confio/index";
import * as cosmos_accesscontrol from "./cosmos/accesscontrol/index";
import * as cosmos_accesscontrol_x from "./cosmos/accesscontrol_x/index";
import * as cosmos_auth_v1beta1 from "./cosmos/auth/v1beta1/index";
import * as cosmos_authz_v1beta1 from "./cosmos/authz/v1beta1/index";
import * as cosmos_bank_v1beta1 from "./cosmos/bank/v1beta1/index";
import * as cosmos_base_abci_v1beta1 from "./cosmos/base/abci/v1beta1/index";
import * as cosmos_base_kv_v1beta1 from "./cosmos/base/kv/v1beta1/index";
import * as cosmos_base_query_v1beta1 from "./cosmos/base/query/v1beta1/index";
import * as cosmos_base_reflection_v1beta1 from "./cosmos/base/reflection/v1beta1/index";
import * as cosmos_base_reflection_v2alpha1 from "./cosmos/base/reflection/v2alpha1/index";
import * as cosmos_base_snapshots_v1beta1 from "./cosmos/base/snapshots/v1beta1/index";
import * as cosmos_base_store_v1beta1 from "./cosmos/base/store/v1beta1/index";
import * as cosmos_base_tendermint_v1beta1 from "./cosmos/base/tendermint/v1beta1/index";
import * as cosmos_base_v1beta1 from "./cosmos/base/v1beta1/index";
import * as cosmos_capability_v1beta1 from "./cosmos/capability/v1beta1/index";
import * as cosmos_crisis_v1beta1 from "./cosmos/crisis/v1beta1/index";
import * as cosmos_crypto_ed25519 from "./cosmos/crypto/ed25519/index";
import * as cosmos_crypto_multisig from "./cosmos/crypto/multisig/index";
import * as cosmos_crypto_secp256k1 from "./cosmos/crypto/secp256k1/index";
import * as cosmos_crypto_secp256r1 from "./cosmos/crypto/secp256r1/index";
import * as cosmos_crypto_sr25519 from "./cosmos/crypto/sr25519/index";
import * as cosmos_distribution_v1beta1 from "./cosmos/distribution/v1beta1/index";
import * as cosmos_evidence_v1beta1 from "./cosmos/evidence/v1beta1/index";
import * as cosmos_feegrant_v1beta1 from "./cosmos/feegrant/v1beta1/index";
import * as cosmos_genutil_v1beta1 from "./cosmos/genutil/v1beta1/index";
import * as cosmos_gov_v1beta1 from "./cosmos/gov/v1beta1/index";
import * as cosmos_mint_v1beta1 from "./cosmos/mint/v1beta1/index";
import * as cosmos_params_types from "./cosmos/params/types/index";
import * as cosmos_params_v1beta1 from "./cosmos/params/v1beta1/index";
import * as cosmos_slashing_v1beta1 from "./cosmos/slashing/v1beta1/index";
import * as cosmos_staking_v1beta1 from "./cosmos/staking/v1beta1/index";
import * as cosmos_tx_signing_v1beta1 from "./cosmos/tx/signing/v1beta1/index";
import * as cosmos_tx_v1beta1 from "./cosmos/tx/v1beta1/index";
import * as cosmos_upgrade_v1beta1 from "./cosmos/upgrade/v1beta1/index";
import * as cosmos_vesting_v1beta1 from "./cosmos/vesting/v1beta1/index";
import * as epoch from "./epoch/index";
import * as eth from "./eth/index";
import * as evm from "./evm/index";
import * as google_api from "./google/api/index";
import * as google_protobuf from "./google/protobuf/index";
import * as mint_v1beta1 from "./mint/v1beta1/index";
import * as oracle from "./oracle/index";
import * as tendermint_abci from "./tendermint/abci/index";
import * as tendermint_crypto from "./tendermint/crypto/index";
import * as tendermint_libs_bits from "./tendermint/libs/bits/index";
import * as tendermint_p2p from "./tendermint/p2p/index";
import * as tendermint_types from "./tendermint/types/index";
import * as tendermint_version from "./tendermint/version/index";
import * as tokenfactory from "./tokenfactory/index";
export * from "./amino";
export * from "./registry";
export const Encoder = {
    confio: confio,
    cosmos: {
        accesscontrol: cosmos_accesscontrol,
        accesscontrol_x: cosmos_accesscontrol_x,
        auth: { v1beta1: cosmos_auth_v1beta1 },
        authz: { v1beta1: cosmos_authz_v1beta1 },
        bank: { v1beta1: cosmos_bank_v1beta1 },
        base: {
            abci: { v1beta1: cosmos_base_abci_v1beta1 },
            kv: { v1beta1: cosmos_base_kv_v1beta1 },
            query: { v1beta1: cosmos_base_query_v1beta1 },
            reflection: { v1beta1: cosmos_base_reflection_v1beta1, v2alpha1: cosmos_base_reflection_v2alpha1 },
            snapshots: { v1beta1: cosmos_base_snapshots_v1beta1 },
            store: { v1beta1: cosmos_base_store_v1beta1 },
            tendermint: { v1beta1: cosmos_base_tendermint_v1beta1 },
            v1beta1: cosmos_base_v1beta1
        },
        capability: { v1beta1: cosmos_capability_v1beta1 },
        crisis: { v1beta1: cosmos_crisis_v1beta1 },
        crypto: {
            ed25519: cosmos_crypto_ed25519,
            multisig: cosmos_crypto_multisig,
            secp256k1: cosmos_crypto_secp256k1,
            secp256r1: cosmos_crypto_secp256r1,
            sr25519: cosmos_crypto_sr25519
        },
        distribution: { v1beta1: cosmos_distribution_v1beta1 },
        evidence: { v1beta1: cosmos_evidence_v1beta1 },
        feegrant: { v1beta1: cosmos_feegrant_v1beta1 },
        genutil: { v1beta1: cosmos_genutil_v1beta1 },
        gov: { v1beta1: cosmos_gov_v1beta1 },
        mint: { v1beta1: cosmos_mint_v1beta1 },
        params: { types: cosmos_params_types, v1beta1: cosmos_params_v1beta1 },
        slashing: { v1beta1: cosmos_slashing_v1beta1 },
        staking: { v1beta1: cosmos_staking_v1beta1 },
        tx: { signing: { v1beta1: cosmos_tx_signing_v1beta1 }, v1beta1: cosmos_tx_v1beta1 },
        upgrade: { v1beta1: cosmos_upgrade_v1beta1 },
        vesting: { v1beta1: cosmos_vesting_v1beta1 }
    },
    epoch: epoch,
    eth: eth,
    evm: evm,
    google: { api: google_api, protobuf: google_protobuf },
    mint: { v1beta1: mint_v1beta1 },
    oracle: oracle,
    tendermint: {
        abci: tendermint_abci,
        crypto: tendermint_crypto,
        libs: { bits: tendermint_libs_bits },
        p2p: tendermint_p2p,
        types: tendermint_types,
        version: tendermint_version
    },
    tokenfactory: tokenfactory
};
