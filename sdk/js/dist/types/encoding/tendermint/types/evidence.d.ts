import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DuplicateVoteEvidence as DuplicateVoteEvidence_type, EvidenceList as EvidenceList_type, Evidence as Evidence_type, LightClientAttackEvidence as LightClientAttackEvidence_type } from "../../../types/tendermint/types";
import type { MessageFns } from "../../common";
export interface Evidence extends Evidence_type {
}
export interface DuplicateVoteEvidence extends DuplicateVoteEvidence_type {
}
export interface LightClientAttackEvidence extends LightClientAttackEvidence_type {
}
export interface EvidenceList extends EvidenceList_type {
}
export declare const Evidence: MessageFns<Evidence, "tendermint.types.Evidence">;
export declare const DuplicateVoteEvidence: MessageFns<DuplicateVoteEvidence, "tendermint.types.DuplicateVoteEvidence">;
export declare const LightClientAttackEvidence: MessageFns<LightClientAttackEvidence, "tendermint.types.LightClientAttackEvidence">;
export declare const EvidenceList: MessageFns<EvidenceList, "tendermint.types.EvidenceList">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.types.Evidence": {
        aminoType: string;
        toAmino: (message: Evidence) => {
            duplicate_vote_evidence?: DuplicateVoteEvidence_type;
            light_client_attack_evidence?: LightClientAttackEvidence_type;
        };
        fromAmino: (object: Evidence) => {
            duplicate_vote_evidence?: DuplicateVoteEvidence_type;
            light_client_attack_evidence?: LightClientAttackEvidence_type;
        };
    };
    "/tendermint.types.DuplicateVoteEvidence": {
        aminoType: string;
        toAmino: (message: DuplicateVoteEvidence) => {
            vote_a?: import("../../../types/tendermint/types").Vote;
            vote_b?: import("../../../types/tendermint/types").Vote;
            total_voting_power: number;
            validator_power: number;
            timestamp?: Date;
        };
        fromAmino: (object: DuplicateVoteEvidence) => {
            vote_a?: import("../../../types/tendermint/types").Vote;
            vote_b?: import("../../../types/tendermint/types").Vote;
            total_voting_power: number;
            validator_power: number;
            timestamp?: Date;
        };
    };
    "/tendermint.types.LightClientAttackEvidence": {
        aminoType: string;
        toAmino: (message: LightClientAttackEvidence) => {
            conflicting_block?: import("../../../types/tendermint/types").LightBlock;
            common_height: number;
            byzantine_validators: import("../../../types/tendermint/types").Validator[];
            total_voting_power: number;
            timestamp?: Date;
        };
        fromAmino: (object: LightClientAttackEvidence) => {
            conflicting_block?: import("../../../types/tendermint/types").LightBlock;
            common_height: number;
            byzantine_validators: import("../../../types/tendermint/types").Validator[];
            total_voting_power: number;
            timestamp?: Date;
        };
    };
    "/tendermint.types.EvidenceList": {
        aminoType: string;
        toAmino: (message: EvidenceList) => {
            evidence: Evidence_type[];
        };
        fromAmino: (object: EvidenceList) => {
            evidence: Evidence_type[];
        };
    };
};
