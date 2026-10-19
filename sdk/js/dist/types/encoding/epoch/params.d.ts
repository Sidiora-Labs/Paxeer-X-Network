import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Params as Params_type } from "../../types/epoch";
import type { MessageFns } from "../common";
export interface Params extends Params_type {
}
export declare const Params: MessageFns<Params, "paxprotocol.paxchain.epoch.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.epoch.Params": {
        aminoType: string;
        toAmino: (message: Params) => {};
        fromAmino: (object: Params) => {};
    };
};
