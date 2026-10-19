"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
var __exportStar = (this && this.__exportStar) || function(m, exports) {
    for (var p in m) if (p !== "default" && !Object.prototype.hasOwnProperty.call(exports, p)) __createBinding(exports, m, p);
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.Encoder = void 0;
const confio = __importStar(require("./confio/index"));
const cosmos_accesscontrol = __importStar(require("./cosmos/accesscontrol/index"));
const cosmos_accesscontrol_x = __importStar(require("./cosmos/accesscontrol_x/index"));
const cosmos_auth_v1beta1 = __importStar(require("./cosmos/auth/v1beta1/index"));
const cosmos_authz_v1beta1 = __importStar(require("./cosmos/authz/v1beta1/index"));
const cosmos_bank_v1beta1 = __importStar(require("./cosmos/bank/v1beta1/index"));
const cosmos_base_abci_v1beta1 = __importStar(require("./cosmos/base/abci/v1beta1/index"));
const cosmos_base_kv_v1beta1 = __importStar(require("./cosmos/base/kv/v1beta1/index"));
const cosmos_base_query_v1beta1 = __importStar(require("./cosmos/base/query/v1beta1/index"));
const cosmos_base_reflection_v1beta1 = __importStar(require("./cosmos/base/reflection/v1beta1/index"));
const cosmos_base_reflection_v2alpha1 = __importStar(require("./cosmos/base/reflection/v2alpha1/index"));
const cosmos_base_snapshots_v1beta1 = __importStar(require("./cosmos/base/snapshots/v1beta1/index"));
const cosmos_base_store_v1beta1 = __importStar(require("./cosmos/base/store/v1beta1/index"));
const cosmos_base_tendermint_v1beta1 = __importStar(require("./cosmos/base/tendermint/v1beta1/index"));
const cosmos_base_v1beta1 = __importStar(require("./cosmos/base/v1beta1/index"));
const cosmos_capability_v1beta1 = __importStar(require("./cosmos/capability/v1beta1/index"));
const cosmos_crisis_v1beta1 = __importStar(require("./cosmos/crisis/v1beta1/index"));
const cosmos_crypto_ed25519 = __importStar(require("./cosmos/crypto/ed25519/index"));
const cosmos_crypto_multisig = __importStar(require("./cosmos/crypto/multisig/index"));
const cosmos_crypto_secp256k1 = __importStar(require("./cosmos/crypto/secp256k1/index"));
const cosmos_crypto_secp256r1 = __importStar(require("./cosmos/crypto/secp256r1/index"));
const cosmos_crypto_sr25519 = __importStar(require("./cosmos/crypto/sr25519/index"));
const cosmos_distribution_v1beta1 = __importStar(require("./cosmos/distribution/v1beta1/index"));
const cosmos_evidence_v1beta1 = __importStar(require("./cosmos/evidence/v1beta1/index"));
const cosmos_feegrant_v1beta1 = __importStar(require("./cosmos/feegrant/v1beta1/index"));
const cosmos_genutil_v1beta1 = __importStar(require("./cosmos/genutil/v1beta1/index"));
const cosmos_gov_v1beta1 = __importStar(require("./cosmos/gov/v1beta1/index"));
const cosmos_mint_v1beta1 = __importStar(require("./cosmos/mint/v1beta1/index"));
const cosmos_params_types = __importStar(require("./cosmos/params/types/index"));
const cosmos_params_v1beta1 = __importStar(require("./cosmos/params/v1beta1/index"));
const cosmos_slashing_v1beta1 = __importStar(require("./cosmos/slashing/v1beta1/index"));
const cosmos_staking_v1beta1 = __importStar(require("./cosmos/staking/v1beta1/index"));
const cosmos_tx_signing_v1beta1 = __importStar(require("./cosmos/tx/signing/v1beta1/index"));
const cosmos_tx_v1beta1 = __importStar(require("./cosmos/tx/v1beta1/index"));
const cosmos_upgrade_v1beta1 = __importStar(require("./cosmos/upgrade/v1beta1/index"));
const cosmos_vesting_v1beta1 = __importStar(require("./cosmos/vesting/v1beta1/index"));
const epoch = __importStar(require("./epoch/index"));
const eth = __importStar(require("./eth/index"));
const evm = __importStar(require("./evm/index"));
const google_api = __importStar(require("./google/api/index"));
const google_protobuf = __importStar(require("./google/protobuf/index"));
const mint_v1beta1 = __importStar(require("./mint/v1beta1/index"));
const oracle = __importStar(require("./oracle/index"));
const tendermint_abci = __importStar(require("./tendermint/abci/index"));
const tendermint_crypto = __importStar(require("./tendermint/crypto/index"));
const tendermint_libs_bits = __importStar(require("./tendermint/libs/bits/index"));
const tendermint_p2p = __importStar(require("./tendermint/p2p/index"));
const tendermint_types = __importStar(require("./tendermint/types/index"));
const tendermint_version = __importStar(require("./tendermint/version/index"));
const tokenfactory = __importStar(require("./tokenfactory/index"));
__exportStar(require("./amino"), exports);
__exportStar(require("./registry"), exports);
exports.Encoder = {
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
