import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/crisis/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.crisis.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crisis.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            constant_fee?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: GenesisState) => {
            constant_fee?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
};
