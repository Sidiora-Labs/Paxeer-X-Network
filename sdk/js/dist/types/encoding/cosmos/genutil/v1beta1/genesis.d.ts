import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/genutil/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.genutil.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.genutil.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            gen_txs: Uint8Array[];
        };
        fromAmino: (object: GenesisState) => {
            gen_txs: Uint8Array[];
        };
    };
};
