import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryMinterRequest as QueryMinterRequest_type, QueryMinterResponse as QueryMinterResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../../types/mint/v1beta1";
import type { MessageFns } from "../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryMinterRequest extends QueryMinterRequest_type {
}
export interface QueryMinterResponse extends QueryMinterResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "paxprotocol.paxchain.mint.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "paxprotocol.paxchain.mint.QueryParamsResponse">;
export declare const QueryMinterRequest: MessageFns<QueryMinterRequest, "paxprotocol.paxchain.mint.QueryMinterRequest">;
export declare const QueryMinterResponse: MessageFns<QueryMinterResponse, "paxprotocol.paxchain.mint.QueryMinterResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.mint.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/paxprotocol.paxchain.mint.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../types/mint/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../types/mint/v1beta1").Params;
        };
    };
    "/paxprotocol.paxchain.mint.QueryMinterRequest": {
        aminoType: string;
        toAmino: (message: QueryMinterRequest) => {};
        fromAmino: (object: QueryMinterRequest) => {};
    };
    "/paxprotocol.paxchain.mint.QueryMinterResponse": {
        aminoType: string;
        toAmino: (message: QueryMinterResponse) => {
            start_date: string;
            end_date: string;
            denom: string;
            total_mint_amount: number;
            remaining_mint_amount: number;
            last_mint_amount: number;
            last_mint_date: string;
            last_mint_height: number;
        };
        fromAmino: (object: QueryMinterResponse) => {
            start_date: string;
            end_date: string;
            denom: string;
            total_mint_amount: number;
            remaining_mint_amount: number;
            last_mint_amount: number;
            last_mint_date: string;
            last_mint_height: number;
        };
    };
};
