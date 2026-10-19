import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryDenomAllowListRequest as QueryDenomAllowListRequest_type, QueryDenomAllowListResponse as QueryDenomAllowListResponse_type, QueryDenomAuthorityMetadataRequest as QueryDenomAuthorityMetadataRequest_type, QueryDenomAuthorityMetadataResponse as QueryDenomAuthorityMetadataResponse_type, QueryDenomMetadataRequest as QueryDenomMetadataRequest_type, QueryDenomMetadataResponse as QueryDenomMetadataResponse_type, QueryDenomsFromCreatorRequest as QueryDenomsFromCreatorRequest_type, QueryDenomsFromCreatorResponse as QueryDenomsFromCreatorResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../types/tokenfactory";
import type { MessageFns } from "../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryDenomAuthorityMetadataRequest extends QueryDenomAuthorityMetadataRequest_type {
}
export interface QueryDenomAuthorityMetadataResponse extends QueryDenomAuthorityMetadataResponse_type {
}
export interface QueryDenomsFromCreatorRequest extends QueryDenomsFromCreatorRequest_type {
}
export interface QueryDenomsFromCreatorResponse extends QueryDenomsFromCreatorResponse_type {
}
export interface QueryDenomMetadataRequest extends QueryDenomMetadataRequest_type {
}
export interface QueryDenomMetadataResponse extends QueryDenomMetadataResponse_type {
}
export interface QueryDenomAllowListRequest extends QueryDenomAllowListRequest_type {
}
export interface QueryDenomAllowListResponse extends QueryDenomAllowListResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "paxprotocol.paxchain.tokenfactory.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "paxprotocol.paxchain.tokenfactory.QueryParamsResponse">;
export declare const QueryDenomAuthorityMetadataRequest: MessageFns<QueryDenomAuthorityMetadataRequest, "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest">;
export declare const QueryDenomAuthorityMetadataResponse: MessageFns<QueryDenomAuthorityMetadataResponse, "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse">;
export declare const QueryDenomsFromCreatorRequest: MessageFns<QueryDenomsFromCreatorRequest, "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest">;
export declare const QueryDenomsFromCreatorResponse: MessageFns<QueryDenomsFromCreatorResponse, "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse">;
export declare const QueryDenomMetadataRequest: MessageFns<QueryDenomMetadataRequest, "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest">;
export declare const QueryDenomMetadataResponse: MessageFns<QueryDenomMetadataResponse, "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse">;
export declare const QueryDenomAllowListRequest: MessageFns<QueryDenomAllowListRequest, "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest">;
export declare const QueryDenomAllowListResponse: MessageFns<QueryDenomAllowListResponse, "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.tokenfactory.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/paxprotocol.paxchain.tokenfactory.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../types/tokenfactory").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../types/tokenfactory").Params;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomAuthorityMetadataRequest) => {
            denom: string;
        };
        fromAmino: (object: QueryDenomAuthorityMetadataRequest) => {
            denom: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomAuthorityMetadataResponse) => {
            authority_metadata?: import("../../types/tokenfactory").DenomAuthorityMetadata;
        };
        fromAmino: (object: QueryDenomAuthorityMetadataResponse) => {
            authority_metadata?: import("../../types/tokenfactory").DenomAuthorityMetadata;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomsFromCreatorRequest) => {
            creator: string;
        };
        fromAmino: (object: QueryDenomsFromCreatorRequest) => {
            creator: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomsFromCreatorResponse) => {
            denoms: string[];
        };
        fromAmino: (object: QueryDenomsFromCreatorResponse) => {
            denoms: string[];
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomMetadataRequest) => {
            denom: string;
        };
        fromAmino: (object: QueryDenomMetadataRequest) => {
            denom: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomMetadataResponse) => {
            metadata?: import("../../types/cosmos/bank/v1beta1").Metadata;
        };
        fromAmino: (object: QueryDenomMetadataResponse) => {
            metadata?: import("../../types/cosmos/bank/v1beta1").Metadata;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomAllowListRequest) => {
            denom: string;
        };
        fromAmino: (object: QueryDenomAllowListRequest) => {
            denom: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomAllowListResponse) => {
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
        fromAmino: (object: QueryDenomAllowListResponse) => {
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
    };
};
