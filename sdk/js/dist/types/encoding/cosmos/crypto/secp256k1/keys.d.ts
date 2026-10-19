import type { GeneratedType } from "@cosmjs/proto-signing";
import type { PrivKey as PrivKey_type, PubKey as PubKey_type } from "../../../../types/cosmos/crypto/secp256k1";
import type { MessageFns } from "../../../common";
export interface PubKey extends PubKey_type {
}
export interface PrivKey extends PrivKey_type {
}
export declare const PubKey: MessageFns<PubKey, "cosmos.crypto.secp256k1.PubKey">;
export declare const PrivKey: MessageFns<PrivKey, "cosmos.crypto.secp256k1.PrivKey">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crypto.secp256k1.PubKey": {
        aminoType: string;
        toAmino: (message: PubKey) => {
            key: Uint8Array;
        };
        fromAmino: (object: PubKey) => {
            key: Uint8Array;
        };
    };
    "/cosmos.crypto.secp256k1.PrivKey": {
        aminoType: string;
        toAmino: (message: PrivKey) => {
            key: Uint8Array;
        };
        fromAmino: (object: PrivKey) => {
            key: Uint8Array;
        };
    };
};
