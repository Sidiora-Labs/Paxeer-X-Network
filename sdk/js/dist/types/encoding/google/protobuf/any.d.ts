import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Any as Any_type } from "../../../types/google/protobuf";
import type { MessageFns } from "../../common";
export interface Any extends Any_type {
}
export declare const Any: MessageFns<Any, "google.protobuf.Any">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.protobuf.Any": {
        aminoType: string;
        toAmino: (message: Any) => {
            type_url: string;
            value: Uint8Array;
        };
        fromAmino: (object: Any) => {
            type_url: string;
            value: Uint8Array;
        };
    };
};
