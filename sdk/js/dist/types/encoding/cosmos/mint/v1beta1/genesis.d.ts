import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/mint/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.mint.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.mint.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            minter?: import("../../../../types/cosmos/mint/v1beta1").Minter;
            params?: import("../../../../types/cosmos/mint/v1beta1").Params;
        };
        fromAmino: (object: GenesisState) => {
            minter?: import("../../../../types/cosmos/mint/v1beta1").Minter;
            params?: import("../../../../types/cosmos/mint/v1beta1").Params;
        };
    };
};
