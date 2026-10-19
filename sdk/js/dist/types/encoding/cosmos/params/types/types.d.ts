import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CosmosGasParams as CosmosGasParams_type, FeesParams as FeesParams_type, GenesisState as GenesisState_type } from "../../../../types/cosmos/params/types";
import type { MessageFns } from "../../../common";
export interface FeesParams extends FeesParams_type {
}
export interface CosmosGasParams extends CosmosGasParams_type {
}
export interface GenesisState extends GenesisState_type {
}
export declare const FeesParams: MessageFns<FeesParams, "cosmos.params.v1beta1.FeesParams">;
export declare const CosmosGasParams: MessageFns<CosmosGasParams, "cosmos.params.v1beta1.CosmosGasParams">;
export declare const GenesisState: MessageFns<GenesisState, "cosmos.params.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.params.v1beta1.FeesParams": {
        aminoType: string;
        toAmino: (message: FeesParams) => {
            global_minimum_gas_prices: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            allowed_fee_denoms: string[];
        };
        fromAmino: (object: FeesParams) => {
            global_minimum_gas_prices: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            allowed_fee_denoms: string[];
        };
    };
    "/cosmos.params.v1beta1.CosmosGasParams": {
        aminoType: string;
        toAmino: (message: CosmosGasParams) => {
            cosmos_gas_multiplier_numerator: number;
            cosmos_gas_multiplier_denominator: number;
        };
        fromAmino: (object: CosmosGasParams) => {
            cosmos_gas_multiplier_numerator: number;
            cosmos_gas_multiplier_denominator: number;
        };
    };
    "/cosmos.params.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            fees_params?: FeesParams_type;
            cosmos_gas_params?: CosmosGasParams_type;
        };
        fromAmino: (object: GenesisState) => {
            fees_params?: FeesParams_type;
            cosmos_gas_params?: CosmosGasParams_type;
        };
    };
};
