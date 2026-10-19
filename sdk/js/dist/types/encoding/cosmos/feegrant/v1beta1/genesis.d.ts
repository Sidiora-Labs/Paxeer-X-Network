import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/feegrant/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.feegrant.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.feegrant.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
        };
        fromAmino: (object: GenesisState) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
        };
    };
};
