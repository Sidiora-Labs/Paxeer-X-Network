import type { GeneratedType } from "@cosmjs/proto-signing";
import type { PageRequest as PageRequest_type, PageResponse as PageResponse_type } from "../../../../../types/cosmos/base/query/v1beta1";
import type { MessageFns } from "../../../../common";
export interface PageRequest extends PageRequest_type {
}
export interface PageResponse extends PageResponse_type {
}
export declare const PageRequest: MessageFns<PageRequest, "cosmos.base.query.v1beta1.PageRequest">;
export declare const PageResponse: MessageFns<PageResponse, "cosmos.base.query.v1beta1.PageResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.query.v1beta1.PageRequest": {
        aminoType: string;
        toAmino: (message: PageRequest) => {
            key: Uint8Array;
            offset: number;
            limit: number;
            count_total: boolean;
            reverse: boolean;
        };
        fromAmino: (object: PageRequest) => {
            key: Uint8Array;
            offset: number;
            limit: number;
            count_total: boolean;
            reverse: boolean;
        };
    };
    "/cosmos.base.query.v1beta1.PageResponse": {
        aminoType: string;
        toAmino: (message: PageResponse) => {
            next_key: Uint8Array;
            total: number;
        };
        fromAmino: (object: PageResponse) => {
            next_key: Uint8Array;
            total: number;
        };
    };
};
