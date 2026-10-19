import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAnnualProvisionsRequest as QueryAnnualProvisionsRequest_type, QueryAnnualProvisionsResponse as QueryAnnualProvisionsResponse_type, QueryInflationRequest as QueryInflationRequest_type, QueryInflationResponse as QueryInflationResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../../../types/cosmos/mint/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryInflationRequest extends QueryInflationRequest_type {
}
export interface QueryInflationResponse extends QueryInflationResponse_type {
}
export interface QueryAnnualProvisionsRequest extends QueryAnnualProvisionsRequest_type {
}
export interface QueryAnnualProvisionsResponse extends QueryAnnualProvisionsResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.mint.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.mint.v1beta1.QueryParamsResponse">;
export declare const QueryInflationRequest: MessageFns<QueryInflationRequest, "cosmos.mint.v1beta1.QueryInflationRequest">;
export declare const QueryInflationResponse: MessageFns<QueryInflationResponse, "cosmos.mint.v1beta1.QueryInflationResponse">;
export declare const QueryAnnualProvisionsRequest: MessageFns<QueryAnnualProvisionsRequest, "cosmos.mint.v1beta1.QueryAnnualProvisionsRequest">;
export declare const QueryAnnualProvisionsResponse: MessageFns<QueryAnnualProvisionsResponse, "cosmos.mint.v1beta1.QueryAnnualProvisionsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.mint.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.mint.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/mint/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/mint/v1beta1").Params;
        };
    };
    "/cosmos.mint.v1beta1.QueryInflationRequest": {
        aminoType: string;
        toAmino: (message: QueryInflationRequest) => {};
        fromAmino: (object: QueryInflationRequest) => {};
    };
    "/cosmos.mint.v1beta1.QueryInflationResponse": {
        aminoType: string;
        toAmino: (message: QueryInflationResponse) => {
            inflation: Uint8Array;
        };
        fromAmino: (object: QueryInflationResponse) => {
            inflation: Uint8Array;
        };
    };
    "/cosmos.mint.v1beta1.QueryAnnualProvisionsRequest": {
        aminoType: string;
        toAmino: (message: QueryAnnualProvisionsRequest) => {};
        fromAmino: (object: QueryAnnualProvisionsRequest) => {};
    };
    "/cosmos.mint.v1beta1.QueryAnnualProvisionsResponse": {
        aminoType: string;
        toAmino: (message: QueryAnnualProvisionsResponse) => {
            annual_provisions: Uint8Array;
        };
        fromAmino: (object: QueryAnnualProvisionsResponse) => {
            annual_provisions: Uint8Array;
        };
    };
};
