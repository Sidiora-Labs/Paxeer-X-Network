import type { GeneratedType } from "@cosmjs/proto-signing";
import type { PubKey as PubKey_type } from "../../../../types/cosmos/crypto/sr25519";
import type { MessageFns } from "../../../common";
export interface PubKey extends PubKey_type {
}
export declare const PubKey: MessageFns<PubKey, "cosmos.crypto.sr25519.PubKey">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crypto.sr25519.PubKey": {
        aminoType: string;
        toAmino: (message: PubKey) => {
            key: Uint8Array;
        };
        fromAmino: (object: PubKey) => {
            key: Uint8Array;
        };
    };
};
