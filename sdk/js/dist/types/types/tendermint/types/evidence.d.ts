import type { LightBlock, Vote } from "./types";
import type { Validator } from "./validator";
export interface Evidence {
    duplicate_vote_evidence?: DuplicateVoteEvidence;
    light_client_attack_evidence?: LightClientAttackEvidence;
}
export interface DuplicateVoteEvidence {
    vote_a?: Vote;
    vote_b?: Vote;
    total_voting_power: number;
    validator_power: number;
    timestamp?: Date;
}
export interface LightClientAttackEvidence {
    conflicting_block?: LightBlock;
    common_height: number;
    byzantine_validators: Validator[];
    total_voting_power: number;
    timestamp?: Date;
}
export interface EvidenceList {
    evidence: Evidence[];
}
