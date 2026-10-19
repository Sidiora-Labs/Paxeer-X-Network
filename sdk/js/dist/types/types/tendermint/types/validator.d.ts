import type { PublicKey } from "../crypto/keys";
export interface ValidatorSet {
    validators: Validator[];
    proposer?: Validator;
    total_voting_power: number;
}
export interface Validator {
    address: Uint8Array;
    pub_key?: PublicKey;
    voting_power: number;
    proposer_priority: number;
}
export interface SimpleValidator {
    pub_key?: PublicKey;
    voting_power: number;
}
