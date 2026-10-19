import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type, LastValidatorPower as LastValidatorPower_type } from "../../../../types/cosmos/staking/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export interface LastValidatorPower extends LastValidatorPower_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.staking.v1beta1.GenesisState">;
export declare const LastValidatorPower: MessageFns<LastValidatorPower, "cosmos.staking.v1beta1.LastValidatorPower">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.staking.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../../../types/cosmos/staking/v1beta1").Params;
            last_total_power: Uint8Array;
            last_validator_powers: LastValidatorPower_type[];
            validators: import("../../../../types/cosmos/staking/v1beta1").Validator[];
            delegations: import("../../../../types/cosmos/staking/v1beta1").Delegation[];
            unbonding_delegations: import("../../../../types/cosmos/staking/v1beta1").UnbondingDelegation[];
            redelegations: import("../../../../types/cosmos/staking/v1beta1").Redelegation[];
            exported: boolean;
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../../../types/cosmos/staking/v1beta1").Params;
            last_total_power: Uint8Array;
            last_validator_powers: LastValidatorPower_type[];
            validators: import("../../../../types/cosmos/staking/v1beta1").Validator[];
            delegations: import("../../../../types/cosmos/staking/v1beta1").Delegation[];
            unbonding_delegations: import("../../../../types/cosmos/staking/v1beta1").UnbondingDelegation[];
            redelegations: import("../../../../types/cosmos/staking/v1beta1").Redelegation[];
            exported: boolean;
        };
    };
    "/cosmos.staking.v1beta1.LastValidatorPower": {
        aminoType: string;
        toAmino: (message: LastValidatorPower) => {
            address: string;
            power: number;
        };
        fromAmino: (object: LastValidatorPower) => {
            address: string;
            power: number;
        };
    };
};
