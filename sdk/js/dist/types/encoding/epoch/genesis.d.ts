import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../types/epoch";
import type { MessageFns } from "../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "paxprotocol.paxchain.epoch.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.epoch.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../types/epoch").Params;
            epoch?: import("../../types/epoch").Epoch;
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../types/epoch").Params;
            epoch?: import("../../types/epoch").Epoch;
        };
    };
};
