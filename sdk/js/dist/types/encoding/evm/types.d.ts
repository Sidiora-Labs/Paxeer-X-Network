import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DeferredInfo as DeferredInfo_type, Whitelist as Whitelist_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface Whitelist extends Whitelist_type {
}
export interface DeferredInfo extends DeferredInfo_type {
}
export declare const Whitelist: MessageFns<Whitelist, "paxprotocol.paxchain.evm.Whitelist">;
export declare const DeferredInfo: MessageFns<DeferredInfo, "paxprotocol.paxchain.evm.DeferredInfo">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.Whitelist": {
        aminoType: string;
        toAmino: (message: Whitelist) => {
            hashes: string[];
        };
        fromAmino: (object: Whitelist) => {
            hashes: string[];
        };
    };
    "/paxprotocol.paxchain.evm.DeferredInfo": {
        aminoType: string;
        toAmino: (message: DeferredInfo) => {
            tx_index: number;
            tx_hash: Uint8Array;
            tx_bloom: Uint8Array;
            surplus: string;
            error: string;
        };
        fromAmino: (object: DeferredInfo) => {
            tx_index: number;
            tx_hash: Uint8Array;
            tx_bloom: Uint8Array;
            surplus: string;
            error: string;
        };
    };
};
