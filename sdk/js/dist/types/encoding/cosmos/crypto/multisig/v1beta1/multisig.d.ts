import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CompactBitArray as CompactBitArray_type, MultiSignature as MultiSignature_type } from "../../../../../types/cosmos/crypto/multisig/v1beta1";
import type { MessageFns } from "../../../../common";
export interface MultiSignature extends MultiSignature_type {
}
export interface CompactBitArray extends CompactBitArray_type {
}
export declare const MultiSignature: MessageFns<MultiSignature, "cosmos.crypto.multisig.v1beta1.MultiSignature">;
export declare const CompactBitArray: MessageFns<CompactBitArray, "cosmos.crypto.multisig.v1beta1.CompactBitArray">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crypto.multisig.v1beta1.MultiSignature": {
        aminoType: string;
        toAmino: (message: MultiSignature) => {
            signatures: Uint8Array[];
        };
        fromAmino: (object: MultiSignature) => {
            signatures: Uint8Array[];
        };
    };
    "/cosmos.crypto.multisig.v1beta1.CompactBitArray": {
        aminoType: string;
        toAmino: (message: CompactBitArray) => {
            extra_bits_stored: number;
            elems: Uint8Array;
        };
        fromAmino: (object: CompactBitArray) => {
            extra_bits_stored: number;
            elems: Uint8Array;
        };
    };
};
