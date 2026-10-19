import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, QuerySigningInfoRequest as QuerySigningInfoRequest_type, QuerySigningInfoResponse as QuerySigningInfoResponse_type, QuerySigningInfosRequest as QuerySigningInfosRequest_type, QuerySigningInfosResponse as QuerySigningInfosResponse_type } from "../../../../types/cosmos/slashing/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QuerySigningInfoRequest extends QuerySigningInfoRequest_type {
}
export interface QuerySigningInfoResponse extends QuerySigningInfoResponse_type {
}
export interface QuerySigningInfosRequest extends QuerySigningInfosRequest_type {
}
export interface QuerySigningInfosResponse extends QuerySigningInfosResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.slashing.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.slashing.v1beta1.QueryParamsResponse">;
export declare const QuerySigningInfoRequest: MessageFns<QuerySigningInfoRequest, "cosmos.slashing.v1beta1.QuerySigningInfoRequest">;
export declare const QuerySigningInfoResponse: MessageFns<QuerySigningInfoResponse, "cosmos.slashing.v1beta1.QuerySigningInfoResponse">;
export declare const QuerySigningInfosRequest: MessageFns<QuerySigningInfosRequest, "cosmos.slashing.v1beta1.QuerySigningInfosRequest">;
export declare const QuerySigningInfosResponse: MessageFns<QuerySigningInfosResponse, "cosmos.slashing.v1beta1.QuerySigningInfosResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.slashing.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.slashing.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
        };
    };
    "/cosmos.slashing.v1beta1.QuerySigningInfoRequest": {
        aminoType: string;
        toAmino: (message: QuerySigningInfoRequest) => {
            cons_address: string;
        };
        fromAmino: (object: QuerySigningInfoRequest) => {
            cons_address: string;
        };
    };
    "/cosmos.slashing.v1beta1.QuerySigningInfoResponse": {
        aminoType: string;
        toAmino: (message: QuerySigningInfoResponse) => {
            val_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo;
        };
        fromAmino: (object: QuerySigningInfoResponse) => {
            val_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo;
        };
    };
    "/cosmos.slashing.v1beta1.QuerySigningInfosRequest": {
        aminoType: string;
        toAmino: (message: QuerySigningInfosRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QuerySigningInfosRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.slashing.v1beta1.QuerySigningInfosResponse": {
        aminoType: string;
        toAmino: (message: QuerySigningInfosResponse) => {
            info: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QuerySigningInfosResponse) => {
            info: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
};
