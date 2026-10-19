import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAllowanceRequest as QueryAllowanceRequest_type, QueryAllowanceResponse as QueryAllowanceResponse_type, QueryAllowancesByGranterRequest as QueryAllowancesByGranterRequest_type, QueryAllowancesByGranterResponse as QueryAllowancesByGranterResponse_type, QueryAllowancesRequest as QueryAllowancesRequest_type, QueryAllowancesResponse as QueryAllowancesResponse_type } from "../../../../types/cosmos/feegrant/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryAllowanceRequest extends QueryAllowanceRequest_type {
}
export interface QueryAllowanceResponse extends QueryAllowanceResponse_type {
}
export interface QueryAllowancesRequest extends QueryAllowancesRequest_type {
}
export interface QueryAllowancesResponse extends QueryAllowancesResponse_type {
}
export interface QueryAllowancesByGranterRequest extends QueryAllowancesByGranterRequest_type {
}
export interface QueryAllowancesByGranterResponse extends QueryAllowancesByGranterResponse_type {
}
export declare const QueryAllowanceRequest: MessageFns<QueryAllowanceRequest, "cosmos.feegrant.v1beta1.QueryAllowanceRequest">;
export declare const QueryAllowanceResponse: MessageFns<QueryAllowanceResponse, "cosmos.feegrant.v1beta1.QueryAllowanceResponse">;
export declare const QueryAllowancesRequest: MessageFns<QueryAllowancesRequest, "cosmos.feegrant.v1beta1.QueryAllowancesRequest">;
export declare const QueryAllowancesResponse: MessageFns<QueryAllowancesResponse, "cosmos.feegrant.v1beta1.QueryAllowancesResponse">;
export declare const QueryAllowancesByGranterRequest: MessageFns<QueryAllowancesByGranterRequest, "cosmos.feegrant.v1beta1.QueryAllowancesByGranterRequest">;
export declare const QueryAllowancesByGranterResponse: MessageFns<QueryAllowancesByGranterResponse, "cosmos.feegrant.v1beta1.QueryAllowancesByGranterResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.feegrant.v1beta1.QueryAllowanceRequest": {
        aminoType: string;
        toAmino: (message: QueryAllowanceRequest) => {
            granter: string;
            grantee: string;
        };
        fromAmino: (object: QueryAllowanceRequest) => {
            granter: string;
            grantee: string;
        };
    };
    "/cosmos.feegrant.v1beta1.QueryAllowanceResponse": {
        aminoType: string;
        toAmino: (message: QueryAllowanceResponse) => {
            allowance?: import("../../../../types/cosmos/feegrant/v1beta1").Grant;
        };
        fromAmino: (object: QueryAllowanceResponse) => {
            allowance?: import("../../../../types/cosmos/feegrant/v1beta1").Grant;
        };
    };
    "/cosmos.feegrant.v1beta1.QueryAllowancesRequest": {
        aminoType: string;
        toAmino: (message: QueryAllowancesRequest) => {
            grantee: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryAllowancesRequest) => {
            grantee: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.feegrant.v1beta1.QueryAllowancesResponse": {
        aminoType: string;
        toAmino: (message: QueryAllowancesResponse) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryAllowancesResponse) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.feegrant.v1beta1.QueryAllowancesByGranterRequest": {
        aminoType: string;
        toAmino: (message: QueryAllowancesByGranterRequest) => {
            granter: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryAllowancesByGranterRequest) => {
            granter: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.feegrant.v1beta1.QueryAllowancesByGranterResponse": {
        aminoType: string;
        toAmino: (message: QueryAllowancesByGranterResponse) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryAllowancesByGranterResponse) => {
            allowances: import("../../../../types/cosmos/feegrant/v1beta1").Grant[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
};
