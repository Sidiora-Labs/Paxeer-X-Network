import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BitArray as BitArray_type } from "../../../../types/tendermint/libs/bits";
import type { MessageFns } from "../../../common";
export interface BitArray extends BitArray_type {
}
export declare const BitArray: MessageFns<BitArray, "tendermint.libs.bits.BitArray">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.libs.bits.BitArray": {
        aminoType: string;
        toAmino: (message: BitArray) => {
            bits: number;
            elems: number[];
        };
        fromAmino: (object: BitArray) => {
            bits: number;
            elems: number[];
        };
    };
};
