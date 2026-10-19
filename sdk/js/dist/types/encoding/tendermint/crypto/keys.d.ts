import type { GeneratedType } from "@cosmjs/proto-signing";
import type { PublicKey as PublicKey_type } from "../../../types/tendermint/crypto";
import type { MessageFns } from "../../common";
export interface PublicKey extends PublicKey_type {
}
export declare const PublicKey: MessageFns<PublicKey, "tendermint.crypto.PublicKey">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.crypto.PublicKey": {
        aminoType: string;
        toAmino: (message: PublicKey) => {
            ed25519?: Uint8Array;
            secp256k1?: Uint8Array;
            sr25519?: Uint8Array;
        };
        fromAmino: (object: PublicKey) => {
            ed25519?: Uint8Array;
            secp256k1?: Uint8Array;
            sr25519?: Uint8Array;
        };
    };
};
