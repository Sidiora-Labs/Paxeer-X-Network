import type { GeneratedType } from "@cosmjs/proto-signing";
import type { HttpBody as HttpBody_type } from "../../../types/google/api";
import type { MessageFns } from "../../common";
export interface HttpBody extends HttpBody_type {
}
export declare const HttpBody: MessageFns<HttpBody, "google.api.HttpBody">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.api.HttpBody": {
        aminoType: string;
        toAmino: (message: HttpBody) => {
            content_type: string;
            data: Uint8Array;
            extensions: import("../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: HttpBody) => {
            content_type: string;
            data: Uint8Array;
            extensions: import("../../../types/google/protobuf").Any[];
        };
    };
};
