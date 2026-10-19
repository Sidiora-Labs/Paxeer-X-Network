import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisDenom as GenesisDenom_type, GenesisState as GenesisState_type } from "../../types/tokenfactory";
import type { MessageFns } from "../common";
export interface GenesisState extends GenesisState_type {
}
export interface GenesisDenom extends GenesisDenom_type {
}
export declare const GenesisState: MessageFns<GenesisState, "paxprotocol.paxchain.tokenfactory.GenesisState">;
export declare const GenesisDenom: MessageFns<GenesisDenom, "paxprotocol.paxchain.tokenfactory.GenesisDenom">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.tokenfactory.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../types/tokenfactory").Params;
            factory_denoms: GenesisDenom_type[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../types/tokenfactory").Params;
            factory_denoms: GenesisDenom_type[];
        };
    };
    "/paxprotocol.paxchain.tokenfactory.GenesisDenom": {
        aminoType: string;
        toAmino: (message: GenesisDenom) => {
            denom: string;
            authority_metadata?: import("../../types/tokenfactory").DenomAuthorityMetadata;
        };
        fromAmino: (object: GenesisDenom) => {
            denom: string;
            authority_metadata?: import("../../types/tokenfactory").DenomAuthorityMetadata;
        };
    };
};
