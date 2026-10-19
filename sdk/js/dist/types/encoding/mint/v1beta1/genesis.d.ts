import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../types/mint/v1beta1";
import type { MessageFns } from "../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "paxprotocol.paxchain.mint.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.mint.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            minter?: import("../../../types/mint/v1beta1").Minter;
            params?: import("../../../types/mint/v1beta1").Params;
        };
        fromAmino: (object: GenesisState) => {
            minter?: import("../../../types/mint/v1beta1").Minter;
            params?: import("../../../types/mint/v1beta1").Params;
        };
    };
};
