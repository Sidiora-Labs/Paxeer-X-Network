import type { GeneratedType } from "@cosmjs/proto-signing";
import type { SimpleValidator as SimpleValidator_type, ValidatorSet as ValidatorSet_type, Validator as Validator_type } from "../../../types/tendermint/types";
import type { MessageFns } from "../../common";
export interface ValidatorSet extends ValidatorSet_type {
}
export interface Validator extends Validator_type {
}
export interface SimpleValidator extends SimpleValidator_type {
}
export declare const ValidatorSet: MessageFns<ValidatorSet, "tendermint.types.ValidatorSet">;
export declare const Validator: MessageFns<Validator, "tendermint.types.Validator">;
export declare const SimpleValidator: MessageFns<SimpleValidator, "tendermint.types.SimpleValidator">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.types.ValidatorSet": {
        aminoType: string;
        toAmino: (message: ValidatorSet) => {
            validators: Validator_type[];
            proposer?: Validator_type;
            total_voting_power: number;
        };
        fromAmino: (object: ValidatorSet) => {
            validators: Validator_type[];
            proposer?: Validator_type;
            total_voting_power: number;
        };
    };
    "/tendermint.types.Validator": {
        aminoType: string;
        toAmino: (message: Validator) => {
            address: Uint8Array;
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            voting_power: number;
            proposer_priority: number;
        };
        fromAmino: (object: Validator) => {
            address: Uint8Array;
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            voting_power: number;
            proposer_priority: number;
        };
    };
    "/tendermint.types.SimpleValidator": {
        aminoType: string;
        toAmino: (message: SimpleValidator) => {
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            voting_power: number;
        };
        fromAmino: (object: SimpleValidator) => {
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            voting_power: number;
        };
    };
};
