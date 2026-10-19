import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAccountRequest as QueryAccountRequest_type, QueryAccountResponse as QueryAccountResponse_type, QueryAccountsRequest as QueryAccountsRequest_type, QueryAccountsResponse as QueryAccountsResponse_type, QueryNextAccountNumberRequest as QueryNextAccountNumberRequest_type, QueryNextAccountNumberResponse as QueryNextAccountNumberResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../../../types/cosmos/auth/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryAccountsRequest extends QueryAccountsRequest_type {
}
export interface QueryAccountsResponse extends QueryAccountsResponse_type {
}
export interface QueryAccountRequest extends QueryAccountRequest_type {
}
export interface QueryAccountResponse extends QueryAccountResponse_type {
}
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryNextAccountNumberRequest extends QueryNextAccountNumberRequest_type {
}
export interface QueryNextAccountNumberResponse extends QueryNextAccountNumberResponse_type {
}
export declare const QueryAccountsRequest: MessageFns<QueryAccountsRequest, "cosmos.auth.v1beta1.QueryAccountsRequest">;
export declare const QueryAccountsResponse: MessageFns<QueryAccountsResponse, "cosmos.auth.v1beta1.QueryAccountsResponse">;
export declare const QueryAccountRequest: MessageFns<QueryAccountRequest, "cosmos.auth.v1beta1.QueryAccountRequest">;
export declare const QueryAccountResponse: MessageFns<QueryAccountResponse, "cosmos.auth.v1beta1.QueryAccountResponse">;
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.auth.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.auth.v1beta1.QueryParamsResponse">;
export declare const QueryNextAccountNumberRequest: MessageFns<QueryNextAccountNumberRequest, "cosmos.auth.v1beta1.QueryNextAccountNumberRequest">;
export declare const QueryNextAccountNumberResponse: MessageFns<QueryNextAccountNumberResponse, "cosmos.auth.v1beta1.QueryNextAccountNumberResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.auth.v1beta1.QueryAccountsRequest": {
        aminoType: string;
        toAmino: (message: QueryAccountsRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryAccountsRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.auth.v1beta1.QueryAccountsResponse": {
        aminoType: string;
        toAmino: (message: QueryAccountsResponse) => {
            accounts: import("../../../../types/google/protobuf").Any[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryAccountsResponse) => {
            accounts: import("../../../../types/google/protobuf").Any[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.auth.v1beta1.QueryAccountRequest": {
        aminoType: string;
        toAmino: (message: QueryAccountRequest) => {
            address: string;
        };
        fromAmino: (object: QueryAccountRequest) => {
            address: string;
        };
    };
    "/cosmos.auth.v1beta1.QueryAccountResponse": {
        aminoType: string;
        toAmino: (message: QueryAccountResponse) => {
            account?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: QueryAccountResponse) => {
            account?: import("../../../../types/google/protobuf").Any;
        };
    };
    "/cosmos.auth.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.auth.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/auth/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/auth/v1beta1").Params;
        };
    };
    "/cosmos.auth.v1beta1.QueryNextAccountNumberRequest": {
        aminoType: string;
        toAmino: (message: QueryNextAccountNumberRequest) => {};
        fromAmino: (object: QueryNextAccountNumberRequest) => {};
    };
    "/cosmos.auth.v1beta1.QueryNextAccountNumberResponse": {
        aminoType: string;
        toAmino: (message: QueryNextAccountNumberResponse) => {
            count: number;
        };
        fromAmino: (object: QueryNextAccountNumberResponse) => {
            count: number;
        };
    };
};
