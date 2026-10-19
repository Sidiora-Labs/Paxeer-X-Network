import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DenomAuthorityMetadata as DenomAuthorityMetadata_type } from "../../types/tokenfactory";
import type { MessageFns } from "../common";
export interface DenomAuthorityMetadata extends DenomAuthorityMetadata_type {
}
export declare const DenomAuthorityMetadata: MessageFns<DenomAuthorityMetadata, "paxprotocol.paxchain.tokenfactory.DenomAuthorityMetadata">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.tokenfactory.DenomAuthorityMetadata": {
        aminoType: string;
        toAmino: (message: DenomAuthorityMetadata) => {
            admin: string;
        };
        fromAmino: (object: DenomAuthorityMetadata) => {
            admin: string;
        };
    };
};
