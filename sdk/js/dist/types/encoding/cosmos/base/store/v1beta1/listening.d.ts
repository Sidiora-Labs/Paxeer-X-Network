import type { GeneratedType } from "@cosmjs/proto-signing";
import type { StoreKVPair as StoreKVPair_type } from "../../../../../types/cosmos/base/store/v1beta1";
import type { MessageFns } from "../../../../common";
export interface StoreKVPair extends StoreKVPair_type {
}
export declare const StoreKVPair: MessageFns<StoreKVPair, "cosmos.base.store.v1beta1.StoreKVPair">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.store.v1beta1.StoreKVPair": {
        aminoType: string;
        toAmino: (message: StoreKVPair) => {
            store_key: string;
            delete: boolean;
            key: Uint8Array;
            value: Uint8Array;
        };
        fromAmino: (object: StoreKVPair) => {
            store_key: string;
            delete: boolean;
            key: Uint8Array;
            value: Uint8Array;
        };
    };
};
