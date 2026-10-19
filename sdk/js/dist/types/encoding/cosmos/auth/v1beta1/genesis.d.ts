import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/auth/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.auth.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.auth.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../../../types/cosmos/auth/v1beta1").Params;
            accounts: import("../../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../../../types/cosmos/auth/v1beta1").Params;
            accounts: import("../../../../types/google/protobuf").Any[];
        };
    };
};
