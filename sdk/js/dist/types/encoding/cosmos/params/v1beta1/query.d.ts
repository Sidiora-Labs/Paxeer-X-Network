import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../../../types/cosmos/params/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.params.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.params.v1beta1.QueryParamsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.params.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {
            subspace: string;
            key: string;
        };
        fromAmino: (object: QueryParamsRequest) => {
            subspace: string;
            key: string;
        };
    };
    "/cosmos.params.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            param?: import("../../../../types/cosmos/params/v1beta1").ParamChange;
        };
        fromAmino: (object: QueryParamsResponse) => {
            param?: import("../../../../types/cosmos/params/v1beta1").ParamChange;
        };
    };
};
