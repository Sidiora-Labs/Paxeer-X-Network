import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryGranteeGrantsRequest as QueryGranteeGrantsRequest_type, QueryGranteeGrantsResponse as QueryGranteeGrantsResponse_type, QueryGranterGrantsRequest as QueryGranterGrantsRequest_type, QueryGranterGrantsResponse as QueryGranterGrantsResponse_type, QueryGrantsRequest as QueryGrantsRequest_type, QueryGrantsResponse as QueryGrantsResponse_type } from "../../../../types/cosmos/authz/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryGrantsRequest extends QueryGrantsRequest_type {
}
export interface QueryGrantsResponse extends QueryGrantsResponse_type {
}
export interface QueryGranterGrantsRequest extends QueryGranterGrantsRequest_type {
}
export interface QueryGranterGrantsResponse extends QueryGranterGrantsResponse_type {
}
export interface QueryGranteeGrantsRequest extends QueryGranteeGrantsRequest_type {
}
export interface QueryGranteeGrantsResponse extends QueryGranteeGrantsResponse_type {
}
export declare const QueryGrantsRequest: MessageFns<QueryGrantsRequest, "cosmos.authz.v1beta1.QueryGrantsRequest">;
export declare const QueryGrantsResponse: MessageFns<QueryGrantsResponse, "cosmos.authz.v1beta1.QueryGrantsResponse">;
export declare const QueryGranterGrantsRequest: MessageFns<QueryGranterGrantsRequest, "cosmos.authz.v1beta1.QueryGranterGrantsRequest">;
export declare const QueryGranterGrantsResponse: MessageFns<QueryGranterGrantsResponse, "cosmos.authz.v1beta1.QueryGranterGrantsResponse">;
export declare const QueryGranteeGrantsRequest: MessageFns<QueryGranteeGrantsRequest, "cosmos.authz.v1beta1.QueryGranteeGrantsRequest">;
export declare const QueryGranteeGrantsResponse: MessageFns<QueryGranteeGrantsResponse, "cosmos.authz.v1beta1.QueryGranteeGrantsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.authz.v1beta1.QueryGrantsRequest": {
        aminoType: string;
        toAmino: (message: QueryGrantsRequest) => {
            granter: string;
            grantee: string;
            msg_type_url: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryGrantsRequest) => {
            granter: string;
            grantee: string;
            msg_type_url: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.authz.v1beta1.QueryGrantsResponse": {
        aminoType: string;
        toAmino: (message: QueryGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.authz.v1beta1.QueryGranterGrantsRequest": {
        aminoType: string;
        toAmino: (message: QueryGranterGrantsRequest) => {
            granter: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryGranterGrantsRequest) => {
            granter: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.authz.v1beta1.QueryGranterGrantsResponse": {
        aminoType: string;
        toAmino: (message: QueryGranterGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").GrantAuthorization[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryGranterGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").GrantAuthorization[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.authz.v1beta1.QueryGranteeGrantsRequest": {
        aminoType: string;
        toAmino: (message: QueryGranteeGrantsRequest) => {
            grantee: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryGranteeGrantsRequest) => {
            grantee: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.authz.v1beta1.QueryGranteeGrantsResponse": {
        aminoType: string;
        toAmino: (message: QueryGranteeGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").GrantAuthorization[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryGranteeGrantsResponse) => {
            grants: import("../../../../types/cosmos/authz/v1beta1").GrantAuthorization[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
};
