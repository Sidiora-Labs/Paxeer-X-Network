import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/evidence/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.evidence.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.evidence.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            evidence: import("../../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: GenesisState) => {
            evidence: import("../../../../types/google/protobuf").Any[];
        };
    };
};
