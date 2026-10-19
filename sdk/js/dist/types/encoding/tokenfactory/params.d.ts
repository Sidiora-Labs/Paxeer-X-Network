import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Params as Params_type } from "../../types/tokenfactory";
import type { MessageFns } from "../common";
export interface Params extends Params_type {
}
export declare const Params: MessageFns<Params, "paxprotocol.paxchain.tokenfactory.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.tokenfactory.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            denom_allowlist_max_size: number;
        };
        fromAmino: (object: Params) => {
            denom_allowlist_max_size: number;
        };
    };
};
