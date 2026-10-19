import { Query as cosmos_accesscontrol_x } from "./cosmos/accesscontrol_x/query";
import { Query as cosmos_auth_v1beta1 } from "./cosmos/auth/v1beta1/query";
import { Query as cosmos_authz_v1beta1 } from "./cosmos/authz/v1beta1/query";
import { Query as cosmos_bank_v1beta1 } from "./cosmos/bank/v1beta1/query";
import { Query as cosmos_distribution_v1beta1 } from "./cosmos/distribution/v1beta1/query";
import { Query as cosmos_evidence_v1beta1 } from "./cosmos/evidence/v1beta1/query";
import { Query as cosmos_feegrant_v1beta1 } from "./cosmos/feegrant/v1beta1/query";
import { Query as cosmos_gov_v1beta1 } from "./cosmos/gov/v1beta1/query";
import { Query as cosmos_mint_v1beta1 } from "./cosmos/mint/v1beta1/query";
import { Query as cosmos_params_v1beta1 } from "./cosmos/params/v1beta1/query";
import { Query as cosmos_slashing_v1beta1 } from "./cosmos/slashing/v1beta1/query";
import { Query as cosmos_staking_v1beta1 } from "./cosmos/staking/v1beta1/query";
import { Query as cosmos_upgrade_v1beta1 } from "./cosmos/upgrade/v1beta1/query";
import { Query as epoch } from "./epoch/query";
import { Query as evm } from "./evm/query";
import { Query as mint_v1beta1 } from "./mint/v1beta1/query";
import { Query as oracle } from "./oracle/query";
import { Query as tokenfactory } from "./tokenfactory/query";
export const Querier = {
    cosmos: {
        accesscontrol_x: cosmos_accesscontrol_x,
        auth: { v1beta1: cosmos_auth_v1beta1 },
        authz: { v1beta1: cosmos_authz_v1beta1 },
        bank: { v1beta1: cosmos_bank_v1beta1 },
        distribution: { v1beta1: cosmos_distribution_v1beta1 },
        evidence: { v1beta1: cosmos_evidence_v1beta1 },
        feegrant: { v1beta1: cosmos_feegrant_v1beta1 },
        gov: { v1beta1: cosmos_gov_v1beta1 },
        mint: { v1beta1: cosmos_mint_v1beta1 },
        params: { v1beta1: cosmos_params_v1beta1 },
        slashing: { v1beta1: cosmos_slashing_v1beta1 },
        staking: { v1beta1: cosmos_staking_v1beta1 },
        upgrade: { v1beta1: cosmos_upgrade_v1beta1 }
    },
    epoch: epoch,
    evm: evm,
    mint: { v1beta1: mint_v1beta1 },
    oracle: oracle,
    tokenfactory: tokenfactory
};
