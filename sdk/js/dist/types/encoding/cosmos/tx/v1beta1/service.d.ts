import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BroadcastTxRequest as BroadcastTxRequest_type, BroadcastTxResponse as BroadcastTxResponse_type, GetBlockWithTxsRequest as GetBlockWithTxsRequest_type, GetBlockWithTxsResponse as GetBlockWithTxsResponse_type, GetTxRequest as GetTxRequest_type, GetTxResponse as GetTxResponse_type, GetTxsEventRequest as GetTxsEventRequest_type, GetTxsEventResponse as GetTxsEventResponse_type, SimulateRequest as SimulateRequest_type, SimulateResponse as SimulateResponse_type } from "../../../../types/cosmos/tx/v1beta1";
import { BroadcastMode, OrderBy } from "../../../../types/cosmos/tx/v1beta1";
import type { MessageFns } from "../../../common";
export interface GetTxsEventRequest extends GetTxsEventRequest_type {
}
export interface GetTxsEventResponse extends GetTxsEventResponse_type {
}
export interface BroadcastTxRequest extends BroadcastTxRequest_type {
}
export interface BroadcastTxResponse extends BroadcastTxResponse_type {
}
export interface SimulateRequest extends SimulateRequest_type {
}
export interface SimulateResponse extends SimulateResponse_type {
}
export interface GetTxRequest extends GetTxRequest_type {
}
export interface GetTxResponse extends GetTxResponse_type {
}
export interface GetBlockWithTxsRequest extends GetBlockWithTxsRequest_type {
}
export interface GetBlockWithTxsResponse extends GetBlockWithTxsResponse_type {
}
export declare const GetTxsEventRequest: MessageFns<GetTxsEventRequest, "cosmos.tx.v1beta1.GetTxsEventRequest">;
export declare const GetTxsEventResponse: MessageFns<GetTxsEventResponse, "cosmos.tx.v1beta1.GetTxsEventResponse">;
export declare const BroadcastTxRequest: MessageFns<BroadcastTxRequest, "cosmos.tx.v1beta1.BroadcastTxRequest">;
export declare const BroadcastTxResponse: MessageFns<BroadcastTxResponse, "cosmos.tx.v1beta1.BroadcastTxResponse">;
export declare const SimulateRequest: MessageFns<SimulateRequest, "cosmos.tx.v1beta1.SimulateRequest">;
export declare const SimulateResponse: MessageFns<SimulateResponse, "cosmos.tx.v1beta1.SimulateResponse">;
export declare const GetTxRequest: MessageFns<GetTxRequest, "cosmos.tx.v1beta1.GetTxRequest">;
export declare const GetTxResponse: MessageFns<GetTxResponse, "cosmos.tx.v1beta1.GetTxResponse">;
export declare const GetBlockWithTxsRequest: MessageFns<GetBlockWithTxsRequest, "cosmos.tx.v1beta1.GetBlockWithTxsRequest">;
export declare const GetBlockWithTxsResponse: MessageFns<GetBlockWithTxsResponse, "cosmos.tx.v1beta1.GetBlockWithTxsResponse">;
export declare function orderByFromJSON(object: any): OrderBy;
export declare function orderByToJSON(object: OrderBy): string;
export declare function broadcastModeFromJSON(object: any): BroadcastMode;
export declare function broadcastModeToJSON(object: BroadcastMode): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.tx.v1beta1.GetTxsEventRequest": {
        aminoType: string;
        toAmino: (message: GetTxsEventRequest) => {
            events: string[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
            order_by: OrderBy;
        };
        fromAmino: (object: GetTxsEventRequest) => {
            events: string[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
            order_by: OrderBy;
        };
    };
    "/cosmos.tx.v1beta1.GetTxsEventResponse": {
        aminoType: string;
        toAmino: (message: GetTxsEventResponse) => {
            txs: import("../../../../types/cosmos/tx/v1beta1").Tx[];
            tx_responses: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: GetTxsEventResponse) => {
            txs: import("../../../../types/cosmos/tx/v1beta1").Tx[];
            tx_responses: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.tx.v1beta1.BroadcastTxRequest": {
        aminoType: string;
        toAmino: (message: BroadcastTxRequest) => {
            tx_bytes: Uint8Array;
            mode: BroadcastMode;
        };
        fromAmino: (object: BroadcastTxRequest) => {
            tx_bytes: Uint8Array;
            mode: BroadcastMode;
        };
    };
    "/cosmos.tx.v1beta1.BroadcastTxResponse": {
        aminoType: string;
        toAmino: (message: BroadcastTxResponse) => {
            tx_response?: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse;
        };
        fromAmino: (object: BroadcastTxResponse) => {
            tx_response?: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse;
        };
    };
    "/cosmos.tx.v1beta1.SimulateRequest": {
        aminoType: string;
        toAmino: (message: SimulateRequest) => {
            tx?: import("../../../../types/cosmos/tx/v1beta1").Tx;
            tx_bytes: Uint8Array;
        };
        fromAmino: (object: SimulateRequest) => {
            tx?: import("../../../../types/cosmos/tx/v1beta1").Tx;
            tx_bytes: Uint8Array;
        };
    };
    "/cosmos.tx.v1beta1.SimulateResponse": {
        aminoType: string;
        toAmino: (message: SimulateResponse) => {
            gas_info?: import("../../../../types/cosmos/base/abci/v1beta1").GasInfo;
            result?: import("../../../../types/cosmos/base/abci/v1beta1").Result;
        };
        fromAmino: (object: SimulateResponse) => {
            gas_info?: import("../../../../types/cosmos/base/abci/v1beta1").GasInfo;
            result?: import("../../../../types/cosmos/base/abci/v1beta1").Result;
        };
    };
    "/cosmos.tx.v1beta1.GetTxRequest": {
        aminoType: string;
        toAmino: (message: GetTxRequest) => {
            hash: string;
        };
        fromAmino: (object: GetTxRequest) => {
            hash: string;
        };
    };
    "/cosmos.tx.v1beta1.GetTxResponse": {
        aminoType: string;
        toAmino: (message: GetTxResponse) => {
            tx?: import("../../../../types/cosmos/tx/v1beta1").Tx;
            tx_response?: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse;
        };
        fromAmino: (object: GetTxResponse) => {
            tx?: import("../../../../types/cosmos/tx/v1beta1").Tx;
            tx_response?: import("../../../../types/cosmos/base/abci/v1beta1").TxResponse;
        };
    };
    "/cosmos.tx.v1beta1.GetBlockWithTxsRequest": {
        aminoType: string;
        toAmino: (message: GetBlockWithTxsRequest) => {
            height: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: GetBlockWithTxsRequest) => {
            height: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.tx.v1beta1.GetBlockWithTxsResponse": {
        aminoType: string;
        toAmino: (message: GetBlockWithTxsResponse) => {
            txs: import("../../../../types/cosmos/tx/v1beta1").Tx[];
            block_id?: import("../../../../types/tendermint/types").BlockID;
            block?: import("../../../../types/tendermint/types").Block;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: GetBlockWithTxsResponse) => {
            txs: import("../../../../types/cosmos/tx/v1beta1").Tx[];
            block_id?: import("../../../../types/tendermint/types").BlockID;
            block?: import("../../../../types/tendermint/types").Block;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
};
