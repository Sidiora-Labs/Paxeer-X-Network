import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Pair as Pair_type, Pairs as Pairs_type } from "../../../../../types/cosmos/base/kv/v1beta1";
import type { MessageFns } from "../../../../common";
export interface Pairs extends Pairs_type {
}
export interface Pair extends Pair_type {
}
export declare const Pairs: MessageFns<Pairs, "cosmos.base.kv.v1beta1.Pairs">;
export declare const Pair: MessageFns<Pair, "cosmos.base.kv.v1beta1.Pair">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.kv.v1beta1.Pairs": {
        aminoType: string;
        toAmino: (message: Pairs) => {
            pairs: Pair_type[];
        };
        fromAmino: (object: Pairs) => {
            pairs: Pair_type[];
        };
    };
    "/cosmos.base.kv.v1beta1.Pair": {
        aminoType: string;
        toAmino: (message: Pair) => {
            key: Uint8Array;
            value: Uint8Array;
        };
        fromAmino: (object: Pair) => {
            key: Uint8Array;
            value: Uint8Array;
        };
    };
};
