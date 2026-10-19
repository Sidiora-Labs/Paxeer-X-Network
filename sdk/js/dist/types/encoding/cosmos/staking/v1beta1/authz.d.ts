import type { GeneratedType } from "@cosmjs/proto-signing";
import type { StakeAuthorizationValidators as StakeAuthorizationValidators_type, StakeAuthorization as StakeAuthorization_type } from "../../../../types/cosmos/staking/v1beta1";
import { AuthorizationType } from "../../../../types/cosmos/staking/v1beta1";
import type { MessageFns } from "../../../common";
export interface StakeAuthorization extends StakeAuthorization_type {
}
export interface StakeAuthorizationValidators extends StakeAuthorizationValidators_type {
}
export declare const StakeAuthorization: MessageFns<StakeAuthorization, "cosmos.staking.v1beta1.StakeAuthorization">;
export declare const StakeAuthorizationValidators: MessageFns<StakeAuthorizationValidators, "cosmos.staking.v1beta1.StakeAuthorization.Validators">;
export declare function authorizationTypeFromJSON(object: any): AuthorizationType;
export declare function authorizationTypeToJSON(object: AuthorizationType): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.staking.v1beta1.StakeAuthorization": {
        aminoType: string;
        toAmino: (message: StakeAuthorization) => {
            max_tokens?: import("../../../../types/cosmos/base/v1beta1").Coin;
            allow_list?: StakeAuthorizationValidators_type;
            deny_list?: StakeAuthorizationValidators_type;
            authorization_type: AuthorizationType;
        };
        fromAmino: (object: StakeAuthorization) => {
            max_tokens?: import("../../../../types/cosmos/base/v1beta1").Coin;
            allow_list?: StakeAuthorizationValidators_type;
            deny_list?: StakeAuthorizationValidators_type;
            authorization_type: AuthorizationType;
        };
    };
    "/cosmos.staking.v1beta1.StakeAuthorization.Validators": {
        aminoType: string;
        toAmino: (message: StakeAuthorizationValidators) => {
            address: string[];
        };
        fromAmino: (object: StakeAuthorizationValidators) => {
            address: string[];
        };
    };
};
