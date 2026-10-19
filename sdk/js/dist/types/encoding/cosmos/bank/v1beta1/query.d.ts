import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAllBalancesRequest as QueryAllBalancesRequest_type, QueryAllBalancesResponse as QueryAllBalancesResponse_type, QueryBalanceRequest as QueryBalanceRequest_type, QueryBalanceResponse as QueryBalanceResponse_type, QueryDenomMetadataRequest as QueryDenomMetadataRequest_type, QueryDenomMetadataResponse as QueryDenomMetadataResponse_type, QueryDenomsMetadataRequest as QueryDenomsMetadataRequest_type, QueryDenomsMetadataResponse as QueryDenomsMetadataResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, QuerySpendableBalancesRequest as QuerySpendableBalancesRequest_type, QuerySpendableBalancesResponse as QuerySpendableBalancesResponse_type, QuerySupplyOfRequest as QuerySupplyOfRequest_type, QuerySupplyOfResponse as QuerySupplyOfResponse_type, QueryTotalSupplyRequest as QueryTotalSupplyRequest_type, QueryTotalSupplyResponse as QueryTotalSupplyResponse_type } from "../../../../types/cosmos/bank/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryBalanceRequest extends QueryBalanceRequest_type {
}
export interface QueryBalanceResponse extends QueryBalanceResponse_type {
}
export interface QueryAllBalancesRequest extends QueryAllBalancesRequest_type {
}
export interface QueryAllBalancesResponse extends QueryAllBalancesResponse_type {
}
export interface QuerySpendableBalancesRequest extends QuerySpendableBalancesRequest_type {
}
export interface QuerySpendableBalancesResponse extends QuerySpendableBalancesResponse_type {
}
export interface QueryTotalSupplyRequest extends QueryTotalSupplyRequest_type {
}
export interface QueryTotalSupplyResponse extends QueryTotalSupplyResponse_type {
}
export interface QuerySupplyOfRequest extends QuerySupplyOfRequest_type {
}
export interface QuerySupplyOfResponse extends QuerySupplyOfResponse_type {
}
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryDenomsMetadataRequest extends QueryDenomsMetadataRequest_type {
}
export interface QueryDenomsMetadataResponse extends QueryDenomsMetadataResponse_type {
}
export interface QueryDenomMetadataRequest extends QueryDenomMetadataRequest_type {
}
export interface QueryDenomMetadataResponse extends QueryDenomMetadataResponse_type {
}
export declare const QueryBalanceRequest: MessageFns<QueryBalanceRequest, "cosmos.bank.v1beta1.QueryBalanceRequest">;
export declare const QueryBalanceResponse: MessageFns<QueryBalanceResponse, "cosmos.bank.v1beta1.QueryBalanceResponse">;
export declare const QueryAllBalancesRequest: MessageFns<QueryAllBalancesRequest, "cosmos.bank.v1beta1.QueryAllBalancesRequest">;
export declare const QueryAllBalancesResponse: MessageFns<QueryAllBalancesResponse, "cosmos.bank.v1beta1.QueryAllBalancesResponse">;
export declare const QuerySpendableBalancesRequest: MessageFns<QuerySpendableBalancesRequest, "cosmos.bank.v1beta1.QuerySpendableBalancesRequest">;
export declare const QuerySpendableBalancesResponse: MessageFns<QuerySpendableBalancesResponse, "cosmos.bank.v1beta1.QuerySpendableBalancesResponse">;
export declare const QueryTotalSupplyRequest: MessageFns<QueryTotalSupplyRequest, "cosmos.bank.v1beta1.QueryTotalSupplyRequest">;
export declare const QueryTotalSupplyResponse: MessageFns<QueryTotalSupplyResponse, "cosmos.bank.v1beta1.QueryTotalSupplyResponse">;
export declare const QuerySupplyOfRequest: MessageFns<QuerySupplyOfRequest, "cosmos.bank.v1beta1.QuerySupplyOfRequest">;
export declare const QuerySupplyOfResponse: MessageFns<QuerySupplyOfResponse, "cosmos.bank.v1beta1.QuerySupplyOfResponse">;
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.bank.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.bank.v1beta1.QueryParamsResponse">;
export declare const QueryDenomsMetadataRequest: MessageFns<QueryDenomsMetadataRequest, "cosmos.bank.v1beta1.QueryDenomsMetadataRequest">;
export declare const QueryDenomsMetadataResponse: MessageFns<QueryDenomsMetadataResponse, "cosmos.bank.v1beta1.QueryDenomsMetadataResponse">;
export declare const QueryDenomMetadataRequest: MessageFns<QueryDenomMetadataRequest, "cosmos.bank.v1beta1.QueryDenomMetadataRequest">;
export declare const QueryDenomMetadataResponse: MessageFns<QueryDenomMetadataResponse, "cosmos.bank.v1beta1.QueryDenomMetadataResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.bank.v1beta1.QueryBalanceRequest": {
        aminoType: string;
        toAmino: (message: QueryBalanceRequest) => {
            address: string;
            denom: string;
        };
        fromAmino: (object: QueryBalanceRequest) => {
            address: string;
            denom: string;
        };
    };
    "/cosmos.bank.v1beta1.QueryBalanceResponse": {
        aminoType: string;
        toAmino: (message: QueryBalanceResponse) => {
            balance?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: QueryBalanceResponse) => {
            balance?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.bank.v1beta1.QueryAllBalancesRequest": {
        aminoType: string;
        toAmino: (message: QueryAllBalancesRequest) => {
            address: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryAllBalancesRequest) => {
            address: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.bank.v1beta1.QueryAllBalancesResponse": {
        aminoType: string;
        toAmino: (message: QueryAllBalancesResponse) => {
            balances: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryAllBalancesResponse) => {
            balances: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.bank.v1beta1.QuerySpendableBalancesRequest": {
        aminoType: string;
        toAmino: (message: QuerySpendableBalancesRequest) => {
            address: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QuerySpendableBalancesRequest) => {
            address: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.bank.v1beta1.QuerySpendableBalancesResponse": {
        aminoType: string;
        toAmino: (message: QuerySpendableBalancesResponse) => {
            balances: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QuerySpendableBalancesResponse) => {
            balances: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.bank.v1beta1.QueryTotalSupplyRequest": {
        aminoType: string;
        toAmino: (message: QueryTotalSupplyRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryTotalSupplyRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.bank.v1beta1.QueryTotalSupplyResponse": {
        aminoType: string;
        toAmino: (message: QueryTotalSupplyResponse) => {
            supply: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryTotalSupplyResponse) => {
            supply: import("../../../../types/cosmos/base/v1beta1").Coin[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.bank.v1beta1.QuerySupplyOfRequest": {
        aminoType: string;
        toAmino: (message: QuerySupplyOfRequest) => {
            denom: string;
        };
        fromAmino: (object: QuerySupplyOfRequest) => {
            denom: string;
        };
    };
    "/cosmos.bank.v1beta1.QuerySupplyOfResponse": {
        aminoType: string;
        toAmino: (message: QuerySupplyOfResponse) => {
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: QuerySupplyOfResponse) => {
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.bank.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.bank.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/bank/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/bank/v1beta1").Params;
        };
    };
    "/cosmos.bank.v1beta1.QueryDenomsMetadataRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomsMetadataRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryDenomsMetadataRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.bank.v1beta1.QueryDenomsMetadataResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomsMetadataResponse) => {
            metadatas: import("../../../../types/cosmos/bank/v1beta1").Metadata[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryDenomsMetadataResponse) => {
            metadatas: import("../../../../types/cosmos/bank/v1beta1").Metadata[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.bank.v1beta1.QueryDenomMetadataRequest": {
        aminoType: string;
        toAmino: (message: QueryDenomMetadataRequest) => {
            denom: string;
        };
        fromAmino: (object: QueryDenomMetadataRequest) => {
            denom: string;
        };
    };
    "/cosmos.bank.v1beta1.QueryDenomMetadataResponse": {
        aminoType: string;
        toAmino: (message: QueryDenomMetadataResponse) => {
            metadata?: import("../../../../types/cosmos/bank/v1beta1").Metadata;
        };
        fromAmino: (object: QueryDenomMetadataResponse) => {
            metadata?: import("../../../../types/cosmos/bank/v1beta1").Metadata;
        };
    };
};
