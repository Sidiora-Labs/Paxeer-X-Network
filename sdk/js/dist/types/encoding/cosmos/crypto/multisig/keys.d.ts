import type { GeneratedType } from "@cosmjs/proto-signing";
import type { LegacyAminoPubKey as LegacyAminoPubKey_type } from "../../../../types/cosmos/crypto/multisig";
import type { MessageFns } from "../../../common";
export interface LegacyAminoPubKey extends LegacyAminoPubKey_type {
}
export declare const LegacyAminoPubKey: MessageFns<LegacyAminoPubKey, "cosmos.crypto.multisig.LegacyAminoPubKey">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crypto.multisig.LegacyAminoPubKey": {
        aminoType: string;
        toAmino: (message: LegacyAminoPubKey) => {
            threshold: number;
            public_keys: import("../../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: LegacyAminoPubKey) => {
            threshold: number;
            public_keys: import("../../../../types/google/protobuf").Any[];
        };
    };
};
