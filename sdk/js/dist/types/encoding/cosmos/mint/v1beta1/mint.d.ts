import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Minter as Minter_type, Params as Params_type } from "../../../../types/cosmos/mint/v1beta1";
import type { MessageFns } from "../../../common";
export interface Minter extends Minter_type {
}
export interface Params extends Params_type {
}
export declare const Minter: MessageFns<Minter, "cosmos.mint.v1beta1.Minter">;
export declare const Params: MessageFns<Params, "cosmos.mint.v1beta1.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.mint.v1beta1.Minter": {
        aminoType: string;
        toAmino: (message: Minter) => {
            inflation: string;
            annual_provisions: string;
        };
        fromAmino: (object: Minter) => {
            inflation: string;
            annual_provisions: string;
        };
    };
    "/cosmos.mint.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            mint_denom: string;
            inflation_rate_change: string;
            inflation_max: string;
            inflation_min: string;
            goal_bonded: string;
            blocks_per_year: number;
        };
        fromAmino: (object: Params) => {
            mint_denom: string;
            inflation_rate_change: string;
            inflation_max: string;
            inflation_min: string;
            goal_bonded: string;
            blocks_per_year: number;
        };
    };
};
