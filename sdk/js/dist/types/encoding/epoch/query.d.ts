import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryEpochRequest as QueryEpochRequest_type, QueryEpochResponse as QueryEpochResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type } from "../../types/epoch";
import type { MessageFns } from "../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryEpochRequest extends QueryEpochRequest_type {
}
export interface QueryEpochResponse extends QueryEpochResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "paxprotocol.paxchain.epoch.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "paxprotocol.paxchain.epoch.QueryParamsResponse">;
export declare const QueryEpochRequest: MessageFns<QueryEpochRequest, "paxprotocol.paxchain.epoch.QueryEpochRequest">;
export declare const QueryEpochResponse: MessageFns<QueryEpochResponse, "paxprotocol.paxchain.epoch.QueryEpochResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.epoch.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/paxprotocol.paxchain.epoch.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../types/epoch").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../types/epoch").Params;
        };
    };
    "/paxprotocol.paxchain.epoch.QueryEpochRequest": {
        aminoType: string;
        toAmino: (message: QueryEpochRequest) => {};
        fromAmino: (object: QueryEpochRequest) => {};
    };
    "/paxprotocol.paxchain.epoch.QueryEpochResponse": {
        aminoType: string;
        toAmino: (message: QueryEpochResponse) => {
            epoch?: import("../../types/epoch").Epoch;
        };
        fromAmino: (object: QueryEpochResponse) => {
            epoch?: import("../../types/epoch").Epoch;
        };
    };
};
